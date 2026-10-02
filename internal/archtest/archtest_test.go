// Package archtest holds tests of the verifier's source itself.
package archtest

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// exception is a place the source may do what a test otherwise rejects: the number of times it
// does, and why. The count makes an exception cover exactly the uses it was written for, so a new
// use in the same place is caught like any other.
type exception struct {
	count int
	why   string
}

// emptyComparisons are the comparisons of a struct field with "" that do not use the empty string
// to mean that something is absent.
var emptyComparisons = map[string]exception{
	"internal/value/number.go Number.IsZero: n.Digits":     {1, "no digits is the number zero, a value, not an absence"},
	"internal/catalog/catalog.go ParseVariant: code.Text":  {1, "rejects an empty code, which no issue has"},
	"internal/dsl/registry.go parseIssueRef: ref.Key":      {1, "rejects an empty message key, which no issue has"},
	"internal/dsl/registry.go parseFixture: fi.Code":       {1, "rejects an empty code, which no issue has"},
	"internal/dsl/registry.go parseFixture: fi.Key":        {1, "rejects an empty message key, which no issue has"},
	"internal/suite/outcome.go parseIssue: is.Code":        {1, "rejects an empty code, which no issue has"},
	"internal/suite/outcome.go parseIssue: is.Key":         {1, "rejects an empty message key, which no issue has"},
	"internal/suite/outcome.go ParseNestedIssues: is.Code": {1, "rejects an empty code, which no issue has"},
	"internal/verify/spec.go Load: s.Version":              {1, "rejects an empty version, which no specification has"},
}

// unorderedRanges are the ranges over a map whose order cannot reach anything the verifier gives.
// Any other range over a map iterates slices.Sorted(maps.Keys(m)): the verifier's problems,
// reports and the reasons in them are output, and Go gives map order at random.
var unorderedRanges = map[string]exception{
	"internal/compare/compare.go Catalog: want":               {1, "fills a map, whose contents do not depend on the order"},
	"internal/compare/compare.go Catalog: got":                {1, "fills a map, whose contents do not depend on the order"},
	"internal/value/type.go var: kindNames":                   {1, "builds the reverse map of kind names"},
	"internal/verify/verify.go Report.Conformant: r.Profiles": {1, "tells whether every profile is conformant"},
	"internal/verify/verify.go sortedKeys: m":                 {1, "collects the keys to sort them"},
}

// substCalls are the calls of value.Type.Subst whose result is not the type of a value. Every
// other type written with parameters becomes the type of a value through Instantiate, which checks
// that it is one.
var substCalls = map[string]exception{
	"internal/value/type.go Type.Subst":       {1, "is Subst, applied to the arguments of a type"},
	"internal/value/type.go Type.Instantiate": {1, "substitutes, then checks the result is a type values have"},
	"internal/value/type.go Match":            {2, "resolves the parameters bound so far on each side, to match what is left"},
	"internal/dsl/check.go state.binder":      {3, "writes the type an argument expects in a problem"},
	"internal/dsl/check.go state.fixtures":    {4, "matches and reports fixture types that may still be patterns; the types it keeps are instantiated"},
	"internal/dsl/check.go checkSources":      {3, "types a binding and an entry in a receiver's context, which may leave parameters to the case; Concrete checks those it fixes"},
	"internal/dsl/check.go sourceFits":        {1, "types an argument in a receiver's context to compare it with its entry's"},
}

// source is the production Go source of the module, parsed and type-checked once for every test
// of it.
type source struct {
	fset  *token.FileSet
	dirs  []string
	files map[string][]*ast.File
	info  map[string]*types.Info
}

// module is the import path of the module whose source the tests read.
const module = "github.com/raoh-project/raoh-specification"

// load parses and type-checks every production package of the module once, each after the module
// packages it imports; everything else is imported from the compiler's export data, which go list
// -export builds and caches.
func load(t *testing.T) source {
	t.Helper()
	src := source{fset: token.NewFileSet(), files: map[string][]*ast.File{}, info: map[string]*types.Info{}}
	out, err := exec.Command("go", "list", "-export", "-deps", "-f", "{{.ImportPath}}\t{{.Export}}", "../../...").Output()
	if err != nil {
		t.Fatal(err)
	}
	exports := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if path, file, ok := strings.Cut(line, "\t"); ok && file != "" {
			exports[path] = file
		}
	}
	lookup := func(path string) (io.ReadCloser, error) {
		file, ok := exports[path]
		if !ok {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(file)
	}
	m := &moduleImporter{src: &src, std: importer.ForCompiler(src.fset, "gc", lookup), pkgs: map[string]*types.Package{}}
	dirs, err := filepath.Glob("../../internal/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range append(dirs, "../../cmd/raoh-verify") {
		if _, err := m.check(dir); err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(src.dirs)
	return src
}

type moduleImporter struct {
	src  *source
	std  types.Importer
	pkgs map[string]*types.Package
}

func (m *moduleImporter) Import(path string) (*types.Package, error) {
	if rel, ok := strings.CutPrefix(path, module+"/"); ok {
		return m.check("../../" + rel)
	}
	return m.std.Import(path)
}

// check type-checks the production files of the package in dir, once.
func (m *moduleImporter) check(dir string) (*types.Package, error) {
	if pkg, ok := m.pkgs[dir]; ok {
		return pkg, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(m.src.fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	path := module + "/" + strings.TrimPrefix(filepath.ToSlash(dir), "../../")
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	pkg, err := (&types.Config{Importer: m}).Check(path, m.src.fset, files, info)
	if err != nil {
		return nil, err
	}
	m.pkgs[dir] = pkg
	if len(files) > 0 {
		m.src.dirs = append(m.src.dirs, dir)
		m.src.files[dir], m.src.info[dir] = files, info
	}
	return pkg, nil
}

// each calls visit for every node of the production source, with the key "file function" that
// names where it is.
func (src source) each(visit func(dir, at string, n ast.Node)) {
	for _, dir := range src.dirs {
		for _, f := range src.files[dir] {
			rel, _ := filepath.Rel("../..", src.fset.Position(f.Pos()).Filename)
			rel = filepath.ToSlash(rel)
			for _, d := range f.Decls {
				name := "var"
				if fn, ok := d.(*ast.FuncDecl); ok {
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

// tally checks what a test found, by key, against its exceptions: every key found is listed, as
// many times as it is found, and every listed key is still found.
func tally(t *testing.T, found map[string][]token.Position, exceptions map[string]exception, what, fix string) {
	t.Helper()
	for _, key := range slices.Sorted(maps.Keys(found)) {
		at := found[key]
		e, ok := exceptions[key]
		switch {
		case !ok:
			for _, p := range at {
				t.Errorf("%s: %s; %s, or list %q with why", p, what, fix, key)
			}
		case e.count != len(at):
			t.Errorf("%q is listed for %d, and the source has %d at %v; list only the uses the reason covers", key, e.count, len(at), at)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(exceptions)) {
		if _, ok := found[key]; !ok {
			t.Errorf("%s is listed and no longer in the source; remove it", key)
		}
	}
}

// TestSource runs the tests of the production source on one parse of it.
func TestSource(t *testing.T) {
	src := load(t)
	t.Run("NoAbsenceIsTheEmptyString", func(t *testing.T) { noAbsenceIsTheEmptyString(t, src) })
	t.Run("MapOrderNeverShows", func(t *testing.T) { mapOrderNeverShows(t, src) })
	t.Run("ValueTypesAreInstantiated", func(t *testing.T) { valueTypesAreInstantiated(t, src) })
	t.Run("NoUnicodeOfTheHost", func(t *testing.T) { noUnicodeOfTheHost(t, src) })
}

// A struct field compared with "" is usually a presence encoded in a value that a real value can
// have: an empty message, an empty argument name. An absence belongs in a pointer or a type.
func noAbsenceIsTheEmptyString(t *testing.T, src source) {
	found := map[string][]token.Position{}
	src.each(func(dir, at string, n ast.Node) {
		b, ok := n.(*ast.BinaryExpr)
		if !ok || (b.Op != token.EQL && b.Op != token.NEQ) {
			return
		}
		for _, pair := range [][2]ast.Expr{{b.X, b.Y}, {b.Y, b.X}} {
			lit, ok := pair[1].(*ast.BasicLit)
			if !ok || lit.Value != `""` {
				continue
			}
			if _, ok := pair[0].(*ast.SelectorExpr); !ok {
				if idx, ok := pair[0].(*ast.IndexExpr); !ok || !isSelector(idx.X) {
					continue
				}
			}
			key := at + ": " + exprString(pair[0])
			found[key] = append(found[key], src.fset.Position(b.Pos()))
		}
	})
	tally(t, found, emptyComparisons, "compares a field with \"\"", "if the empty string means absence, use a pointer or a type")
}

// Every range over a map is in sorted order, or listed with why its order cannot show.
func mapOrderNeverShows(t *testing.T, src source) {
	found := map[string][]token.Position{}
	src.each(func(dir, at string, n ast.Node) {
		r, ok := n.(*ast.RangeStmt)
		if !ok {
			return
		}
		if _, isMap := src.info[dir].TypeOf(r.X).Underlying().(*types.Map); !isMap {
			return
		}
		key := at + ": " + exprString(r.X)
		found[key] = append(found[key], src.fset.Position(r.Pos()))
	})
	tally(t, found, unorderedRanges, "ranges over a map", "iterate slices.Sorted(maps.Keys(...))")
}

// Subst is called only where the type it gives is a pattern or an intermediate step, never the
// type of a value.
func valueTypesAreInstantiated(t *testing.T, src source) {
	found := map[string][]token.Position{}
	src.each(func(dir, at string, n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Subst" {
			found[at] = append(found[at], src.fset.Position(call.Pos()))
		}
	})
	tally(t, found, substCalls, "calls Subst", "use Instantiate for the type of a value")
}

// The specification reads Unicode properties of version 18.0.0, and Go's unicode package has those
// of the Go that builds the verifier. A test of an answer cannot tell the two apart while the
// versions agree on it, so the source does not import the package at all; what it needs of Unicode
// it reads from the Unicode Character Database files it carries.
func noUnicodeOfTheHost(t *testing.T, src source) {
	for _, dir := range src.dirs {
		for _, f := range src.files[dir] {
			for _, spec := range f.Imports {
				if spec.Path.Value == `"unicode"` {
					t.Errorf("%s: imports unicode, whose properties are of Go's version of Unicode; read them from a file of the version the specification names", src.fset.Position(spec.Pos()))
				}
			}
		}
	}
}

// A test never judges by the clock: how long something takes depends on the machine, so a test
// counts the work or the allocations instead, and means the same everywhere.
func TestNoTestReadsTheClock(t *testing.T) {
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
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

func recvName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.StarExpr:
		return recvName(e.X)
	case *ast.Ident:
		return e.Name
	}
	return "?"
}

func isSelector(e ast.Expr) bool {
	_, ok := e.(*ast.SelectorExpr)
	return ok
}

func exprString(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprString(e.X) + "." + e.Sel.Name
	case *ast.IndexExpr:
		return exprString(e.X) + "[" + exprString(e.Index) + "]"
	case *ast.CallExpr:
		return exprString(e.Fun) + "(...)"
	}
	return fmt.Sprintf("%T", e)
}
