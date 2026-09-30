package dsl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/raoh-project/raoh-specification/internal/catalog"
)

// doc is a catalogue file decoded for editing, its numbers kept as written.
type doc = map[string]any

func readDoc(t *testing.T, path string) doc {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var out doc
	if err := d.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func encode(t *testing.T, d doc) []byte {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func operation(ops doc, name string) doc {
	for _, o := range ops["operations"].([]any) {
		if o.(doc)["name"] == name {
			return o.(doc)
		}
	}
	panic("no operation " + name)
}

func section(ops doc, sec, name string) doc { return ops[sec].(doc)[name].(doc) }

func arg(form doc, i int) doc { return form["args"].([]any)[i].(doc) }

// catalogs are the three files a mutation may edit.
type catalogs struct{ ops, fixtures, issues doc }

// TestRegistryInvariants checks that a catalogue breaking a condition the checker relies on is
// rejected when it is read, with a reason, and never later: by Parse when the condition is the
// registry's own, by NewChecker when it needs the issue catalogue. Parse does not rely on the
// schema; the schema rejects what it can say, which the table records.
func TestRegistryInvariants(t *testing.T) {
	sch := schemasFor(t)
	for _, tc := range []struct {
		name string
		edit func(c catalogs)
		// schema says whether operations.schema.json rejects the edit too.
		schema bool
		// checker says the rejection needs the issue catalogue.
		checker bool
		reason  string
	}{
		{name: "unchanged", edit: func(catalogs) {}},
		{"duplicate argument name", func(c catalogs) {
			f := operation(c.ops, "minLength")
			f["args"] = append(f["args"].([]any), doc{"name": "min", "kind": "value", "type": "int32"})
		}, false, false, "two arguments are named min"},
		{"optional constructor argument", func(c catalogs) {
			arg(section(c.ops, "constructors", "strict"), 1)["optional"] = true
		}, true, false, "only an operation has optional arguments"},
		{"optional field argument", func(c catalogs) {
			arg(section(c.ops, "fields", "field"), 0)["optional"] = true
		}, true, false, "only an operation has optional arguments"},
		{"optional decoder argument", func(c catalogs) {
			f := operation(c.ops, "minLength")
			f["args"] = append(f["args"].([]any), doc{"name": "inner", "kind": "decoder", "type": "string", "optional": true})
		}, true, false, "cannot be left out"},
		{"optional fixture argument", func(c catalogs) {
			arg(operation(c.ops, "refine"), 0)["optional"] = true
		}, true, false, "cannot be left out"},
		{"optional value without a default", func(c catalogs) {
			arg(operation(c.ops, "minLength"), 0)["optional"] = true
		}, true, false, "no default"},
		{"default on an argument that cannot be left out", func(c catalogs) {
			delete(arg(operation(c.ops, "normalize"), 0), "optional")
		}, true, false, "only an optional value argument has one"},
		{"default on a message", func(c catalogs) {
			arg(operation(c.ops, "toInt"), 0)["default"] = "x"
		}, true, false, "only an optional value argument has one"},
		{"default of the wrong type", func(c catalogs) {
			arg(operation(c.ops, "normalize"), 0)["default"] = json.Number("3")
		}, false, false, "default"},
		{"default outside one_of", func(c catalogs) {
			arg(operation(c.ops, "normalize"), 0)["default"] = "XYZ"
		}, false, false, "not one of its values"},
		{"one_of on a non-string", func(c catalogs) {
			arg(operation(c.ops, "minLength"), 0)["one_of"] = []any{"a"}
		}, true, false, "only a string value argument can"},
		{"two message arguments", func(c catalogs) {
			f := operation(c.ops, "toInt")
			f["args"] = append(f["args"].([]any), doc{"name": "again", "kind": "message", "optional": true})
		}, false, false, "are both messages"},
		{"a type on a message", func(c catalogs) {
			arg(operation(c.ops, "toInt"), 0)["type"] = "string"
		}, true, false, "has no type"},
		{"an unknown member", func(c catalogs) {
			arg(operation(c.ops, "minLength"), 0)["optinal"] = true
		}, true, false, `no member "optinal"`},
		{"duplicate issue", func(c catalogs) {
			f := operation(c.ops, "minLength")
			f["issues"] = append(f["issues"].([]any), "too_short")
		}, false, false, "declared twice"},
		{"omitted metadata with a source", func(c catalogs) {
			f := operation(c.ops, "minLength")
			f["issues"].([]any)[0].(doc)["omit"] = []any{"min"}
		}, false, false, "both omits min and gives it a source"},
		{"candidates without meta", func(c catalogs) {
			delete(section(c.ops, "constructors", "oneOf")["flow"].(doc)["candidates"].(doc), "meta")
		}, true, false, `"meta"`},
		{"candidates with an unknown member", func(c catalogs) {
			section(c.ops, "constructors", "oneOf")["flow"].(doc)["candidates"].(doc)["alt"] = "x"
		}, true, false, `no member "alt"`},
		{"candidates in metadata the issue does not have", func(c catalogs) {
			section(c.ops, "constructors", "oneOf")["flow"].(doc)["candidates"].(doc)["meta"] = "nope"
		}, false, true, "no metadata nope to list the candidates"},
		{"candidates of the wrong type", func(c catalogs) {
			c.issues["one_of_failed"].(doc)["meta"].(doc)["candidates"] = "json"
		}, false, true, "which is a json"},
		{"candidates with a metadata source", func(c catalogs) {
			section(c.ops, "constructors", "oneOf")["issues"] = []any{doc{"key": "one_of_failed", "meta": doc{"candidates": doc{"const": []any{}}}}}
		}, false, false, "gives a source or omits"},
		{"candidates that may be left out", func(c catalogs) {
			c.issues["one_of_failed"].(doc)["optional_meta"] = []any{"candidates"}
		}, false, true, "may leave out candidates"},
		{"a constant not of its entry's type", func(c catalogs) {
			section(c.ops, "constructors", "string")["issues"].([]any)[1].(doc)["meta"].(doc)["expected"].(doc)["const"] = json.Number("5")
		}, false, true, "meta expected"},
		{"an argument not of its entry's type", func(c catalogs) {
			arg(operation(c.ops, "minLength"), 0)["type"] = "string"
		}, false, true, "argument min is a string"},
		{"a parameter the issue does not have", func(c catalogs) {
			operation(c.ops, "minLength")["issues"].([]any)[0].(doc)["Z"] = "string"
		}, false, true, "no parameter Z"},
		{"omitting metadata the issue does not have", func(c catalogs) {
			operation(c.ops, "minLength")["issues"].([]any)[0].(doc)["omit"] = []any{"nope"}
		}, false, true, "no metadata nope to omit"},
		{"a cat of one flow", func(c catalogs) {
			section(c.ops, "constructors", "strict")["flow"] = doc{"cat": []any{doc{"arg": "inner"}}}
		}, true, false, "two flows or more"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := catalogs{readDoc(t, "../../catalog/operations.json"), readDoc(t, "../../catalog/fixtures.json"), readDoc(t, "../../catalog/issues.json")}
			tc.edit(c)
			ops, fixtures, issues := encode(t, c.ops), encode(t, c.fixtures), encode(t, c.issues)
			if err := sch.Validate("operations", ops); (err != nil) != tc.schema {
				t.Errorf("the schema rejects it: %v, want %v (%v)", err != nil, tc.schema, err)
			}
			err := read(t, ops, fixtures, issues)
			switch {
			case tc.reason == "" && err != nil:
				t.Fatalf("rejected: %v", err)
			case tc.reason == "":
			case err == nil:
				t.Fatalf("accepted")
			case !strings.Contains(err.Error(), tc.reason):
				t.Fatalf("rejected for another reason: %v", err)
			case tc.checker != strings.HasPrefix(err.Error(), "checker: "):
				t.Fatalf("rejected by the wrong stage: %v", err)
			}
		})
	}
}

// read reads the catalogues as the verifier does, without their schemas, and says which stage
// rejected them.
func read(t *testing.T, ops, fixtures, issues []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	reg, err := Parse(ops, fixtures)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	cat, err := catalog.Load("../..", schemasFor(t))
	if err != nil {
		return err
	}
	if cat.Variants, err = catalog.ParseVariants(issues); err != nil {
		return err
	}
	if _, err := NewChecker(reg, cat); err != nil {
		return fmt.Errorf("checker: %w", err)
	}
	return nil
}
