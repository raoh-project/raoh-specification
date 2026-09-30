// Package archtest holds tests of the verifier's source itself.
package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// allowed are the comparisons of a struct field with "" that do not use the empty string to mean
// that something is absent, with why.
var allowed = map[string]string{
	"internal/value/number.go n.Digits":          "no digits is the number zero, a value, not an absence",
	"internal/catalog/catalog.go code.Text":      "rejects an empty code while validating the catalogue",
	"internal/dsl/registry.go ref.Key":           "rejects an issue reference without a key while reading",
	"internal/dsl/registry.go fi.Code":           "rejects a fixture issue without a code while reading",
	"internal/dsl/registry.go fi.Key":            "rejects a fixture issue without a message key while reading",
	"internal/dsl/registry.go fi.Message":        "rejects a fixture issue without a message while reading",
	"internal/suite/outcome.go is.Code":          "rejects an issue without a code while reading",
	"internal/suite/outcome.go is.Key":           "rejects an issue without a message key while reading",
	"internal/verify/spec.go s.Version":          "rejects a specification without a version while reading",
	"internal/suite/expect.go expected[i].Group": "a group is an ID the verifier makes, never empty; empty means the issue is in none",
}

// A struct field compared with "" is usually a presence encoded in a value that a real value can
// have: an empty message, an empty argument name. New comparisons have to be listed above, with
// why the empty string is not an absence there; an absence belongs in a pointer or a type.
func TestNoAbsenceIsTheEmptyString(t *testing.T) {
	root := "../.."
	seen := map[string]bool{}
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		ast.Inspect(file, func(n ast.Node) bool {
			b, ok := n.(*ast.BinaryExpr)
			if !ok || (b.Op != token.EQL && b.Op != token.NEQ) {
				return true
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
				key := rel + " " + exprString(pair[0])
				seen[key] = true
				if _, ok := allowed[key]; !ok {
					t.Errorf("%s: %s compared with \"\"; if the empty string means absence, use a pointer or a type; otherwise list it with why", fset.Position(b.Pos()), exprString(pair[0]))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for key := range allowed {
		if !seen[key] {
			t.Errorf("%s is listed and no longer in the source; remove it", key)
		}
	}
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
	}
	return "?"
}
