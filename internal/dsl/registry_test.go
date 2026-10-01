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

// positiveMin is the const_by_type that gives positive's minimum.
func positiveMin(c catalogs) doc {
	return operation(c.ops, "positive")["issues"].([]any)[0].(doc)["meta"].(doc)["min"].(doc)["const_by_type"].(doc)
}

// probe is an operation with the receivers given.
func probe(receivers ...string) doc {
	var rs []any
	for _, r := range receivers {
		rs = append(rs, r)
	}
	return doc{"name": "probe", "doc": "x", "receivers": rs, "result": "R", "issues": []any{}, "flow": "none"}
}

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
		{"an issue the catalogue does not have", func(c catalogs) {
			operation(c.ops, "minLength")["issues"] = []any{"no_such_issue"}
		}, false, true, "issue no_such_issue is not in the catalogue"},
		{"omitting an entry the message writes", func(c catalogs) {
			operation(c.ops, "minLength")["issues"] = []any{doc{"key": "too_short", "omit": []any{"min"}}}
		}, false, true, "omits min, which its derived message writes"},
		{"an optional entry every form gives a source", func(c catalogs) {
			c.issues["invalid_format"].(doc)["optional_meta"] = []any{"pattern"}
		}, false, true, "every form that gives it omits pattern or gives it a source"},
		{"an optional entry every form omits", func(c catalogs) {
			c.issues["invalid_format.cuid"] = doc{"code": "invalid_format", "meta": doc{"x": "string"}, "optional_meta": []any{"x"}}
			operation(c.ops, "cuid")["issues"] = []any{doc{"key": "invalid_format.cuid", "omit": []any{"x"}}}
		}, false, true, "every form that gives it omits x or gives it a source"},
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
		{"any type and string in two forms", func(c catalogs) {
			c.ops["operations"] = append(c.ops["operations"].([]any), probe("*"), probe("string"))
		}, false, false, "applies to every type, and to some types again"},
		{"any type and string in one form", func(c catalogs) {
			c.ops["operations"] = append(c.ops["operations"].([]any), probe("*", "string"))
		}, false, false, "applies to every type, and to some types again"},
		{"two patterns of one kind", func(c catalogs) {
			c.ops["operations"] = append(c.ops["operations"].([]any), probe("list<E>"), probe("list<string>"))
		}, false, false, "applies to list twice"},
		{"a parameter as the receiver", func(c catalogs) {
			c.ops["operations"] = append(c.ops["operations"].([]any), probe("E"))
		}, false, false, "is a parameter"},
		{"R inside a receiver", func(c catalogs) {
			c.ops["operations"] = append(c.ops["operations"].([]any), probe("list<R>"))
		}, false, false, "mentions R"},
		{"a result nothing binds", func(c catalogs) {
			operation(c.ops, "minLength")["result"] = "list<U>"
		}, false, false, "the result mentions U"},
		{"a value argument nothing binds", func(c catalogs) {
			arg(operation(c.ops, "minLength"), 0)["type"] = "U"
		}, false, false, "argument min mentions U"},
		{"an issue binding nothing binds", func(c catalogs) {
			operation(c.ops, "min")["issues"].([]any)[0].(doc)["T"] = "U"
		}, false, false, "binding T mentions U"},
		{"R outside an operation", func(c catalogs) {
			arg(section(c.ops, "properties", "propertyWithDefault"), 3)["type"] = "R"
		}, false, false, "argument default mentions R"},
		{"a symbol without alternatives", func(c catalogs) {
			delete(section(c.ops, "constructors", "enum"), "symbols_from")
		}, false, false, "does not say its alternatives"},
		{"symbols_from for a symbol with alternatives", func(c catalogs) {
			section(c.ops, "constructors", "enum")["result"] = `symbol<"A">`
		}, false, false, "symbols_from says the alternatives of a symbol result"},
		{"properties without a type", func(c catalogs) {
			delete(arg(section(c.ops, "encoders", "object"), 0), "type")
		}, true, false, "needs a type"},
		{"a property without an input", func(c catalogs) {
			delete(section(c.ops, "properties", "propertyWithDefault"), "input")
		}, true, false, "no input type"},
		{"a fixture output nothing binds", func(c catalogs) {
			c.fixtures["unbound"] = doc{"kind": "map", "doc": "x", "input": "int32", "output": "U"}
		}, false, false, "the output mentions U"},
		{"a fixture issue metadata nothing binds", func(c catalogs) {
			c.fixtures["unbound"] = doc{"kind": "refine", "doc": "x", "input": "T", "issue": doc{"code": "c", "message_key": "c", "message": "m", "meta": doc{"x": "list<U>"}}}
		}, false, false, "meta x mentions U"},
		{"a refine fixture argument with an output", func(c catalogs) {
			arg(operation(c.ops, "refine"), 0)["output"] = "R"
		}, true, false, "a refine fixture has no output type"},
		{"a map fixture argument without an output", func(c catalogs) {
			delete(arg(operation(c.ops, "map"), 0), "output")
		}, true, false, "a map fixture has an output type"},
		{"a getter fixture argument without an output", func(c catalogs) {
			delete(arg(section(c.ops, "properties", "propertyWithDefault"), 1), "output")
		}, true, false, "a getter fixture has an output type"},
		{"a refine fixture placed nowhere", func(c catalogs) {
			operation(c.ops, "refine")["flow"] = "none"
		}, false, false, "places the issues of argument predicate 0 times"},
		{"a map fixture placed", func(c catalogs) {
			operation(c.ops, "map")["flow"] = doc{"fixture": "function"}
		}, false, false, "gives no issue"},
		{"a map fixture discarded", func(c catalogs) {
			operation(c.ops, "map")["flow"] = doc{"discard": []any{"function"}}
		}, false, false, "the flow needs one that gives issues"},
		{"a symbol without alternatives in a result", func(c catalogs) {
			operation(c.ops, "minLength")["result"] = "list<symbol>"
		}, false, false, "a symbol type lists its alternatives"},
		{"a symbol without alternatives in a value argument", func(c catalogs) {
			arg(operation(c.ops, "minLength"), 0)["type"] = "nullable<symbol>"
		}, false, false, "a symbol type lists its alternatives"},
		{"a symbol without alternatives as a fixture output", func(c catalogs) {
			c.fixtures["sym"] = doc{"kind": "map", "doc": "x", "input": "int32", "output": "symbol"}
		}, false, false, "a symbol type lists its alternatives"},
		{"a symbol without alternatives in fixture metadata", func(c catalogs) {
			c.fixtures["sym"] = doc{"kind": "refine", "doc": "x", "input": "int32", "issue": doc{"code": "c", "message_key": "c", "message": "m", "meta": doc{"x": "list<symbol>"}}}
		}, false, false, "a symbol type lists its alternatives"},
		{"a symbol without alternatives in the issue catalogue", func(c catalogs) {
			c.issues["too_short"].(doc)["meta"].(doc)["min"] = "symbol"
		}, false, false, "a symbol type lists its alternatives"},
		{"a product of two fields arguments", func(c catalogs) {
			f := section(c.ops, "constructors", "object")
			f["args"] = append(f["args"].([]any), doc{"name": "more", "kind": "fields"})
		}, false, false, "two fields arguments"},
		{"symbols_from for a declared result", func(c catalogs) {
			section(c.ops, "constructors", "enum")["result"] = "string"
		}, false, false, "symbols_from says the alternatives of a symbol result, and its result is string"},
		{"const_by_type without a receiver's type", func(c catalogs) {
			delete(positiveMin(c), "decimal")
		}, false, true, "const_by_type gives values for [float32 float64 int32 int64], and the entry can be [decimal float32 float64 int32 int64]"},
		{"const_by_type for a type no receiver gives", func(c catalogs) {
			positiveMin(c)["string"] = "x"
		}, false, true, "const_by_type gives values for [decimal float32 float64 int32 int64 string]"},
		{"const_by_type of the wrong type", func(c catalogs) {
			positiveMin(c)["decimal"] = json.Number("0")
		}, false, true, "the value for decimal"},
		{"const_by_type for a type a case decides", func(c catalogs) {
			operation(c.ops, "contains")["issues"].([]any)[0].(doc)["meta"].(doc)["expected"] = doc{"const_by_type": doc{"int32": json.Number("1")}}
		}, false, true, "const_by_type for a E, which a case decides"},
		{"a constant for a type a case decides", func(c catalogs) {
			operation(c.ops, "contains")["issues"].([]any)[0].(doc)["meta"].(doc)["expected"] = doc{"const": json.Number("1")}
		}, false, true, "a constant for a E, which a case decides"},
		{"a sort of elements with no order", func(c catalogs) {
			f := operation(c.ops, "containsAll")
			f["issues"].([]any)[0].(doc)["meta"].(doc)["expected"] = doc{"sorted": "elements"}
		}, false, true, "sorts a list<E>, whose elements have no order it can tell"},
		{"an ill-formed metadata type in a receiver's context", func(c catalogs) {
			c.issues["probe"] = doc{"code": "probe", "params": []any{"T"}, "meta": doc{"x": "optional<T>"}}
			c.ops["operations"] = append(c.ops["operations"].([]any), doc{"name": "probe", "doc": "x", "receivers": []any{"string"}, "result": "R",
				"issues": []any{doc{"key": "probe", "T": "nullable<R>"}}, "flow": "own"})
		}, false, true, "operation probe on string: issue probe meta x is a optional<nullable<string>>"},
		{"members known by fields not required to be named", func(c catalogs) {
			delete(section(c.ops, "constructors", "strictObject"), "requires")
		}, false, false, "does not require to be named_fields"},
		{"a property that names no member", func(c catalogs) {
			delete(section(c.ops, "properties", "propertyWithDefault"), "member")
		}, true, false, "does not say which argument names the member"},
		{"a condition of the wrong kind of argument", func(c catalogs) {
			operation(c.ops, "minLength")["requires"] = []any{doc{"check": "distinct", "args": []any{"min"}}}
		}, false, false, "and it reads a list"},
		{name: "a fixture issue with an empty message", edit: func(c catalogs) {
			c.fixtures["even"].(doc)["issue"].(doc)["message"] = ""
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := catalogs{readDoc(t, "../../catalog/operations.json"), readDoc(t, "../../catalog/fixtures.json"), readDoc(t, "../../catalog/issues.json")}
			tc.edit(c)
			ops, fixtures, issues := encode(t, c.ops), encode(t, c.fixtures), encode(t, c.issues)
			err := sch.Validate("operations", ops)
			if err == nil {
				err = sch.Validate("fixtures", fixtures)
			}
			if (err != nil) != tc.schema {
				t.Errorf("the schemas reject it: %v, want %v (%v)", err != nil, tc.schema, err)
			}
			err = read(t, ops, fixtures, issues)
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
