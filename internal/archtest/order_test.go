package archtest

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// unorderedRanges are the ranges over a map whose order cannot reach anything the verifier gives,
// with why. Any other range over a map iterates slices.Sorted(maps.Keys(m)): the verifier's
// problems, reports and the reasons in them are output, and Go gives map order at random.
var unorderedRanges = map[string]string{
	"internal/compare/compare.go Catalog: want":               "fills a map, whose contents do not depend on the order",
	"internal/compare/compare.go Catalog: got":                "fills a map, whose contents do not depend on the order",
	"internal/value/type.go var: kindNames":                   "builds the reverse map of kind names",
	"internal/verify/verify.go Report.Conformant: r.Profiles": "tells whether every profile is conformant",
	"internal/verify/verify.go sortedKeys: m":                 "collects the keys to sort them",
}

// substCalls are the functions that call value.Type.Subst, with why the type they get is not
// the type of a value. Every other type written with parameters becomes the type of a value
// through Instantiate, which checks that it is one.
var substCalls = map[string]string{
	"internal/value/type.go Type.Subst":       "is Subst, applied to the arguments of a type",
	"internal/value/type.go Type.Instantiate": "substitutes, then checks the result is a type values have",
	"internal/value/type.go Match":            "resolves the parameters bound so far, to match what is left",
	"internal/dsl/check.go state.binder":      "writes the type an argument expects in a problem",
	"internal/dsl/check.go state.fixtures":    "matches and reports fixture types that may still be patterns; the types it keeps are instantiated",
	"internal/dsl/check.go checkSources":      "types an entry in a receiver's context, which may leave parameters to the case; Concrete checks those it fixes",
	"internal/dsl/check.go sourceFits":        "types an argument in a receiver's context to compare it with its entry's",
}

// source is the production Go source of the module, parsed and type-checked, by package.
type source struct {
	fset  *token.FileSet
	files map[string][]*ast.File
	info  map[string]*types.Info
}

func load(t *testing.T) source {
	t.Helper()
	src := source{fset: token.NewFileSet(), files: map[string][]*ast.File{}, info: map[string]*types.Info{}}
	imp := importer.ForCompiler(src.fset, "source", nil)
	dirs, err := filepath.Glob("../../internal/*")
	if err != nil {
		t.Fatal(err)
	}
	dirs = append(dirs, "../../cmd/raoh-verify")
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var files []*ast.File
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			f, err := parser.ParseFile(src.fset, filepath.Join(dir, e.Name()), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
		}
		if len(files) == 0 {
			continue
		}
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
		if _, err := (&types.Config{Importer: imp}).Check(dir, src.fset, files, info); err != nil {
			t.Fatal(err)
		}
		src.files[dir], src.info[dir] = files, info
	}
	return src
}

// each calls visit for every node of the production source, with the key "file function" that
// names where it is.
func (src source) each(visit func(dir, at string, n ast.Node)) {
	for _, dir := range slices.Sorted(func(yield func(string) bool) {
		for d := range src.files {
			if !yield(d) {
				return
			}
		}
	}) {
		for _, f := range src.files[dir] {
			rel, _ := filepath.Rel("../..", src.fset.Position(f.Pos()).Filename)
			rel = filepath.ToSlash(rel)
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				name := "var"
				if ok {
					name = fn.Name.Name
					if fn.Recv != nil && len(fn.Recv.List) > 0 {
						name = recvName(fn.Recv.List[0].Type) + "." + name
					}
				}
				at := rel + " " + name
				ast.Inspect(d, func(n ast.Node) bool {
					if n != nil {
						visit(dir, at, n)
					}
					return true
				})
			}
		}
	}
}

func recvName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.StarExpr:
		return recvName(e.X)
	case *ast.Ident:
		return e.Name
	}
	return "?"
}

// Every range over a map is either in sorted order or listed with why its order cannot show.
func TestMapOrderNeverShows(t *testing.T) {
	src := load(t)
	seen := map[string]bool{}
	src.each(func(dir, at string, n ast.Node) {
		r, ok := n.(*ast.RangeStmt)
		if !ok {
			return
		}
		if _, isMap := src.info[dir].TypeOf(r.X).Underlying().(*types.Map); !isMap {
			return
		}
		key := at + ": " + exprString(r.X)
		seen[key] = true
		if _, ok := unorderedRanges[key]; !ok {
			t.Errorf("%s: ranges over the map %s; iterate slices.Sorted(maps.Keys(...)), or list %q with why its order cannot show", src.fset.Position(r.Pos()), exprString(r.X), key)
		}
	})
	for key := range unorderedRanges {
		if !seen[key] {
			t.Errorf("%s is listed and no longer in the source; remove it", key)
		}
	}
}

// Subst is called only where the type it gives is a pattern or an intermediate step, never the
// type of a value.
func TestValueTypesAreInstantiated(t *testing.T) {
	src := load(t)
	seen := map[string]bool{}
	src.each(func(dir, at string, n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Subst" {
			return
		}
		seen[at] = true
		if _, ok := substCalls[at]; !ok {
			t.Errorf("%s: calls Subst; use Instantiate for the type of a value, or list %q with why this is not one", src.fset.Position(call.Pos()), at)
		}
	})
	for key := range substCalls {
		if !seen[key] {
			t.Errorf("%s is listed and no longer calls Subst; remove it", key)
		}
	}
}

// A test never judges by the clock: how long something takes depends on the machine, so a test
// counts the work or the allocations instead, and means the same everywhere.
func TestNoTestReadsTheClock(t *testing.T) {
	root := "../.."
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == "time" && (sel.Sel.Name == "Now" || sel.Sel.Name == "Since") {
				t.Errorf("%s: a test reads the clock with time.%s; count the work instead", fset.Position(sel.Pos()), sel.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
