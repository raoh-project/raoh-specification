package dsl

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/raoh-project/raoh-specification/internal/catalog"
	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/schemas"
	"github.com/raoh-project/raoh-specification/internal/value"
)

func checker(t *testing.T) *Checker {
	t.Helper()
	cat, err := catalog.Load("../..", schemasFor(t))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := Load("../..", schemasFor(t))
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewChecker(reg, cat)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func decoder(t *testing.T, c *Checker, form string) *Checked {
	t.Helper()
	checked, err := c.CheckDecoder(jsontext.MustParse(form))
	if err != nil {
		t.Fatalf("%s: %v", form, err)
	}
	return checked
}

func rejected(t *testing.T, c *Checker, form, why string) {
	t.Helper()
	_, err := c.CheckDecoder(jsontext.MustParse(form))
	if err == nil {
		t.Errorf("%s accepted", form)
	} else if !strings.Contains(err.Error(), why) {
		t.Errorf("%s: %v, want it to say %q", form, err, why)
	}
}

func issue(t *testing.T, checked *Checked, key string) *Site {
	t.Helper()
	var found *Site
	var walk func(f Flow)
	walk = func(f Flow) {
		switch f := f.(type) {
		case *Site:
			if f.Key == key && found == nil {
				found = f
			}
		case *Alt:
			for _, i := range f.Items {
				walk(i)
			}
		case *Cat:
			for _, i := range f.Items {
				walk(i)
			}
		case *Chain:
			for _, i := range f.Items {
				walk(i)
			}
		case *Repeat:
			walk(f.Body)
		case *At:
			walk(f.Body)
		case *Unordered:
			walk(f.Site)
		case *Candidates:
			walk(f.Site)
		}
	}
	walk(checked.Flow)
	if found == nil {
		t.Fatalf("no site %s in %s", key, Describe(checked.Flow))
	}
	return found
}

// gives reports whether a decoder can give a list of issues, each written "path key", for an
// input, reading an issue at a site by its path and key alone.
func gives(t *testing.T, c *Checker, form, input string, issues ...string) bool {
	t.Helper()
	d := decoder(t, c, form)
	fit := func(i int, slot Slot) (string, bool) {
		want := JoinPath(slot.Path) + " " + slot.Key
		return fmt.Sprintf("%p", slot.Group), issues[i] == want
	}
	return len(ParseIssues(d.Flow, jsontext.MustParse(input), nil, len(issues), fit)) > 0
}

func TestResultTypes(t *testing.T) {
	c := checker(t)
	for form, want := range map[string]string{
		`["string", ["minLength", 3]]`:                                                                           "string",
		`["string", ["toDecimal"], ["min", "0.5"]]`:                                                              "decimal",
		`["list", ["int"], ["toSet"]]`:                                                                           "set<int32>",
		`["object", [["field", "name", ["string"]], ["optionalField", "nick", ["int"]]]]`:                        "product<string,optional<int32>>",
		`["object", [["optionalNullableField", "n", ["long"]]]]`:                                                 "product<presence<int64>>",
		`["dict", ["nullable", ["bool"]]]`:                                                                       "map<nullable<bool>>",
		`["enum", ["RED", "GREEN"], ["string", ["trim"]]]`:                                                       `symbol<"RED","GREEN">`,
		`["oneOf", [["int", ["map", "decimal_string"]], ["string", ["minLength", 3]]]]`:                          "string",
		`["object", [["field", "w", ["int"]], ["field", "h", ["int"]]], ["map", "area"]]`:                        "int32",
		`["object", [["field", "k", ["string"]]], ["map", "first"]]`:                                             "string",
		`["string", ["iso8601"], ["after", "2024-01-01T00:00:00Z"]]`:                                             "instant",
		`["withDefault", ["int"], 0]`:                                                                            "int32",
		`["recoverWith", ["int"], "issue_count_plus_10"]`:                                                        "int32",
		`["discriminate", "kind", {"square": ["object", [["field", "side", ["int"]]], ["map", "square_side"]]}]`: "int32",
	} {
		if got := decoder(t, c, form).Result.String(); got != want {
			t.Errorf("%s: %s, want %s", form, got, want)
		}
	}
}

func TestLiteralsAreReadAtTheTypeTheyAreUsedAt(t *testing.T) {
	c := checker(t)
	decoder(t, c, `["float", ["min", 0.1]]`)
	decoder(t, c, `["double", ["oneOf", [2.0, {"float": "-0"}]]]`)
	rejected(t, c, `["int", ["min", 0.5]]`, "no fraction")
	rejected(t, c, `["float", ["min", 16777217]]`, "rounds to")
	rejected(t, c, `["decimal", ["min", 0.5]]`, "expected string")
	rejected(t, c, `["list", ["int"], ["contains", "2"]]`, "expected number")
	rejected(t, c, `["string", ["normalize", "NFX"]]`, "must be one of")
	rejected(t, c, `["withDefault", ["int"], "0"]`, "expected number")
}

func TestFormsThatDoNotTypeCheck(t *testing.T) {
	c := checker(t)
	rejected(t, c, `["strng"]`, "unknown constructor")
	rejected(t, c, `["string", ["minLenght", 3]]`, "unknown operation")
	rejected(t, c, `["int", ["minLength", 3]]`, "does not apply to int32")
	rejected(t, c, `["list"]`, "takes 1 argument")
	rejected(t, c, `["string", ["minLength"]]`, "takes 1 to 1")
	rejected(t, c, `["oneOf", [["int"], ["string"]]]`, "expected a decoder of int32")
	rejected(t, c, `["int", ["map", "area"]]`, "takes product<int32,int32>, not int32")
	rejected(t, c, `["int", ["map", "no_such_fixture"]]`, "unknown fixture")
	rejected(t, c, `["int", ["map", "even"]]`, "needs a map fixture")
	rejected(t, c, `["object", [["field", "a"]]]`, "takes 2 argument")
	rejected(t, c, `"string"`, "expected [name, ...]")
}

func TestFeatures(t *testing.T) {
	c := checker(t)
	got := decoder(t, c, `["list", ["int", ["min", 1], ["refine", "even"]], ["nonempty"]]`).Features
	want := []string{"decoder.int", "decoder.list", "fixture.even", "operation.any.refine", "operation.int32.min", "operation.list.nonempty"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	all := c.Registry().Features()
	for _, f := range got {
		if !slices.Contains(all, f) {
			t.Errorf("%s is not among the registry's features", f)
		}
	}
	if !slices.Contains(all, "operation.map.nonempty") || !slices.Contains(all, "operation.decimal.scale") {
		t.Errorf("registry features incomplete: %v", all)
	}
}

func TestPossibleIssuesAreInstantiated(t *testing.T) {
	c := checker(t)
	p := issue(t, decoder(t, c, `["float", ["oneOf", [1.0, 2.0]]]`), "not_allowed")
	if p.Meta["allowed"].String() != "list<float32>" || p.Meta["actual"].String() != "float32" {
		t.Errorf("got %v", p.Meta)
	}
	d := decoder(t, c, `["discriminate", "kind", {"a": ["int"]}]`)
	p = issue(t, d, "not_allowed")
	if _, ok := p.Meta["actual"]; ok || p.Meta["allowed"].String() != "list<string>" {
		t.Errorf("discriminate not_allowed %v", p.Meta)
	}
	p = issue(t, decoder(t, c, `["list", ["string"], ["unique"]]`), "duplicate_element")
	if p.Meta["duplicates"].String() != "list<string>" {
		t.Errorf("got %v", p.Meta)
	}
	p = issue(t, decoder(t, c, `["int", ["refine", "even"]]`), "must_be_even")
	if p.Message == nil || *p.Message != "must be even" || p.Meta["actual"].String() != "int32" {
		t.Errorf("got %+v", p)
	}
}

func TestReplacedIssuesAreNotPossible(t *testing.T) {
	c := checker(t)
	if got := Describe(decoder(t, c, `["recover", ["int", ["min", 1]], 1]`).Flow); got != "alt()" {
		t.Errorf("recover gives %s", got)
	}
	d := decoder(t, c, `["oneOf", [["int", ["map", "decimal_string"]], ["string", ["minLength", 3]]]]`)
	one := issue(t, d, "one_of_failed")
	if one.Candidates.Meta != "candidates" || len(one.Candidates.Flows) != 2 {
		t.Errorf("oneOf lists %d candidates in %s", len(one.Candidates.Flows), one.Candidates.Meta)
	}
	if got := Describe(one.Candidates.Flows[1]); got != "chain(alt(alt(), required, type_mismatch), alt(alt(), too_short))" {
		t.Errorf("the second candidate: %s", got)
	}
}

// A flow follows the expressions operations.json declares: a form's own issues exclude each
// other, an operation runs only if what comes before it succeeded, fields aggregate, variants
// exclude each other, and a strict form reports every member it does not know.
func TestFlowsFollowTheDeclaredExpressions(t *testing.T) {
	c := checker(t)
	for form, want := range map[string]string{
		`["int"]`: "alt(alt(), required, type_mismatch, type_mismatch.numeric_range)",
		`["string", ["minLength", 3], ["email"]]`: "chain(chain(alt(alt(), required, type_mismatch), alt(alt(), too_short)), alt(alt(), invalid_format.email))",
		`["list", ["int"]]`:                       "chain(alt(alt(), required, type_mismatch), each_elements(alt(alt(), required, type_mismatch, type_mismatch.numeric_range)))",
		`["strict", ["strict", ["object", [["field", "a", ["int"]]]], ["a", "x"]], ["a", "y"]]`: "" +
			"cat(cat(cat(at(a, chain(alt(alt(), type_mismatch), alt(alt(), required, type_mismatch, type_mismatch.numeric_range)))), unknown#1(except a,x: unknown_field)), unknown#2(except a,y: unknown_field))",
		`["recover", ["int"], 1]`: "alt()",
	} {
		if got := Describe(decoder(t, c, form).Flow); got != want {
			t.Errorf("%s:\n got %s\nwant %s", form, got, want)
		}
	}
	rejected(t, c, `["strictObject", [["flat", ["object", [["field", "a", ["int"]]]]]]]`, "reads the whole input")
}

// The flow is the language of the issue lists a decoder can give: nothing more.
func TestFlowsAreExactLanguages(t *testing.T) {
	c := checker(t)
	for _, x := range []struct {
		form, input string
		issues      []string
		ok          bool
	}{
		{`["int"]`, `null`, []string{" required"}, true},
		{`["int"]`, `null`, []string{" required", " type_mismatch"}, false},
		{`["int"]`, `null`, nil, true},
		{`["string", ["minLength", 3], ["email"]]`, `"a"`, []string{" too_short"}, true},
		{`["string", ["minLength", 3], ["email"]]`, `"a"`, []string{" invalid_format.email"}, true},
		{`["string", ["minLength", 3], ["email"]]`, `"a"`, []string{" too_short", " invalid_format.email"}, false},
		{`["object", [["field", "a", ["int"]], ["field", "b", ["int"]]]]`, `{}`, []string{"/a required", "/b required"}, true},
		{`["object", [["field", "a", ["int"]], ["field", "b", ["int"]]]]`, `{}`, []string{"/b required", "/a required"}, false},
		{`["discriminate", "kind", {"a": ["object", [["field", "x", ["int"]]]], "b": ["object", [["field", "y", ["int"]]]]}]`, `{"kind": "a"}`, []string{"/x required"}, true},
		{`["discriminate", "kind", {"a": ["object", [["field", "x", ["int"]]]], "b": ["object", [["field", "y", ["int"]]]]}]`, `{"kind": "a"}`, []string{"/x required", "/y required"}, false},
		{`["list", ["int", ["min", 1]]]`, `[0, 0]`, []string{"/0 out_of_range.minimum", "/1 out_of_range.minimum"}, true},
		{`["list", ["int"]]`, `[1]`, []string{" type_mismatch", "/0 required"}, false},
		// Every member a strict form does not know is reported, in any order within its group.
		{`["strictObject", [["field", "a", ["int"]]]]`, `{"a": 1, "b": 1, "c": 1}`, []string{"/c unknown_field", "/b unknown_field"}, true},
		{`["strictObject", [["field", "a", ["int"]]]]`, `{"a": 1, "b": 1, "c": 1}`, []string{"/b unknown_field"}, false},
		{`["strictObject", [["field", "a", ["int"]]]]`, `{"a": 1, "b": 1}`, nil, false},
		// Two strict forms on one object are two groups, the inner first.
		{`["strict", ["strict", ["object", [["field", "a", ["int"]]]], ["a", "x"]], ["a", "y"]]`, `{"a": 1, "x": 1, "y": 1}`, []string{"/y unknown_field", "/x unknown_field"}, true},
		{`["strict", ["strict", ["object", [["field", "a", ["int"]]]], ["a", "x"]], ["a", "y"]]`, `{"a": 1, "x": 1, "y": 1}`, []string{"/x unknown_field", "/y unknown_field"}, false},
	} {
		if got := gives(t, c, x.form, x.input, x.issues...); got != x.ok {
			t.Errorf("%s on %s gives %v: %v, want %v", x.form, x.input, x.issues, got, x.ok)
		}
	}
}

// The parser keeps, for each node, path and position, where its parses end, so a decoder with
// many alternatives over many elements is parsed quickly.
func TestParsingDoesNotExplode(t *testing.T) {
	c := checker(t)
	var elems, issues []string
	for i := 0; i < 200; i++ {
		elems = append(elems, "0")
		issues = append(issues, "/"+strconv.Itoa(i)+" out_of_range.minimum")
	}
	start := time.Now()
	if !gives(t, c, `["list", ["int", ["min", 1], ["max", 5], ["multipleOf", 2]]]`, "["+strings.Join(elems, ",")+"]", issues...) {
		t.Error("200 failing elements are not a list the decoder gives")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestEncoders(t *testing.T) {
	c := checker(t)
	e, err := c.CheckEncoder(jsontext.MustParse(`["object", [["propertyWithDefault", "value", "identity", ["string"], "default"]]]`))
	if err != nil {
		t.Fatal(err)
	}
	if e.Result.String() != "nullable<string>" {
		t.Errorf("input %s", e.Result)
	}
	if !slices.Equal(e.Features, []string{"encoder.object", "encoder.string", "fixture.identity", "property.propertyWithDefault"}) {
		t.Errorf("features %v", e.Features)
	}
	if _, err := c.CheckEncoder(jsontext.MustParse(`["object", [["propertyWithDefault", "value", "identity", ["string"], 1]]]`)); err == nil {
		t.Error("a default of the wrong type accepted")
	}
}

// A form raoh-java refuses to construct is not a decoder.
func TestArgumentsMeetWhatTheFormRequires(t *testing.T) {
	c := checker(t)
	rejected(t, c, `["int", ["range", 5, 1]]`, "must not be after")
	decoder(t, c, `["int", ["range", 1, 1]]`)
	rejected(t, c, `["double", ["range", 0, {"float": "-0"}]]`, "must not be after")
	decoder(t, c, `["double", ["range", {"float": "-0"}, 0]]`)
	rejected(t, c, `["decimal", ["range", "10", "9.99"]]`, "must not be after")
	rejected(t, c, `["string", ["date"], ["between", "2024-12-31", "2024-01-01"]]`, "must not be after")
	rejected(t, c, `["int", ["multipleOf", 0]]`, "must not be zero")
	rejected(t, c, `["decimal", ["multipleOf", "0.00"]]`, "must not be zero")
	rejected(t, c, `["list", ["int"], ["containsAll", []]]`, "must not be empty")
	rejected(t, c, `["enum", ["Red", "RED"], ["string"]]`, "ASCII case folding")
	decoder(t, c, `["enum", ["RED", "GREEN"], ["string"]]`)
	rejected(t, c, `["decimal", ["oneOf", ["1"]]]`, "does not apply to decimal")
}

func TestResultTypesMustBeWellFormed(t *testing.T) {
	c := checker(t)
	rejected(t, c, `["nullable", ["nullable", ["int"]]]`, "cannot tell its own null")
	rejected(t, c, `["object", [["optionalField", "a", ["nullable", ["int"]]]]]`, "cannot tell its own null")
	decoder(t, c, `["nullable", ["list", ["nullable", ["int"]]]]`)
}

func schemasFor(t *testing.T) *schemas.Set {
	t.Helper()
	sch, err := schemas.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

// A section's required type left out of the file is an error, in the schema and in the parser,
// never a zero Type.
func TestMissingTypesAreErrors(t *testing.T) {
	ops, err := os.ReadFile("../../catalog/operations.json")
	if err != nil {
		t.Fatal(err)
	}
	fx, _ := os.ReadFile("../../catalog/fixtures.json")
	for name, member := range map[string][3]string{
		"constructor result": {"constructors", "string", "result"},
		"field result":       {"fields", "field", "result"},
		"encoder input":      {"encoders", "string", "input"},
	} {
		var doc map[string]any
		if err := json.Unmarshal(ops, &doc); err != nil {
			t.Fatal(err)
		}
		form := doc[member[0]].(map[string]any)[member[1]].(map[string]any)
		if _, ok := form[member[2]]; !ok {
			t.Fatalf("%s: nothing to remove", name)
		}
		delete(form, member[2])
		broken, _ := json.Marshal(doc)
		if err := schemasFor(t).Validate("operations", broken); err == nil {
			t.Errorf("%s: the schema accepts it", name)
		}
		if _, err := Parse(broken, fx); err == nil {
			t.Errorf("%s: the parser accepts it", name)
		}
	}
}

func TestEnumsGiveTheirSymbols(t *testing.T) {
	c := checker(t)
	if got := decoder(t, c, `["enum", ["RED", "GREEN"], ["string"]]`).Result.String(); got != `symbol<"RED","GREEN">` {
		t.Errorf("enum gives %s", got)
	}
}

// Every reference in operations.json is resolved when the registry is loaded, and a reference
// that does not resolve is an error there, never a meaning the checker falls back to.
func TestReferencesResolveWhenLoaded(t *testing.T) {
	ops, err := os.ReadFile("../../catalog/operations.json")
	if err != nil {
		t.Fatal(err)
	}
	fx, _ := os.ReadFile("../../catalog/fixtures.json")
	operation := func(doc map[string]any, name, receiver string) map[string]any {
		for _, o := range doc["operations"].([]any) {
			op := o.(map[string]any)
			if op["name"] == name && slices.Contains(op["receivers"].([]any), any(receiver)) {
				return op
			}
		}
		t.Fatalf("no operation %s on %s", name, receiver)
		return nil
	}
	for name, x := range map[string]struct {
		edit func(doc map[string]any)
		why  string
	}{
		"a metadata source naming no argument": {func(doc map[string]any) {
			op := operation(doc, "min", "int32")
			op["issues"].([]any)[0].(map[string]any)["meta"] = map[string]any{"min": map[string]any{"arg": "mni"}}
		}, "reads mni"},
		"a flow naming no argument": {func(doc map[string]any) {
			doc["constructors"].(map[string]any)["strict"].(map[string]any)["flow"] = map[string]any{"cat": []any{
				map[string]any{"arg": "inner"}, map[string]any{"unknown_members": map[string]any{"known": map[string]any{"arg": "knwon"}, "issue": "unknown_field"}}}}
		}, "knwon"},
		"a decoder argument the flow leaves out": {func(doc map[string]any) {
			doc["constructors"].(map[string]any)["nullable"].(map[string]any)["flow"] = "none"
		}, "places the issues of argument inner 0 times"},
		"an issue the flow never gives": {func(doc map[string]any) {
			doc["constructors"].(map[string]any)["enum"].(map[string]any)["flow"] = map[string]any{"arg": "string"}
		}, "gives none of the issues"},
		"a member name outside unknown members": {func(doc map[string]any) {
			op := operation(doc, "min", "int32")
			op["issues"].([]any)[0].(map[string]any)["meta"] = map[string]any{"min": map[string]any{"member": true}}
		}, "only for an issue given at members"},
		"lower-casing a list that is not of strings": {func(doc map[string]any) {
			op := operation(doc, "oneOf", "int32")
			op["issues"].([]any)[0].(map[string]any)["meta"] = map[string]any{"allowed": map[string]any{"ascii_lower_sorted": "allowed"}}
		}, "not a list<string>"},
	} {
		var doc map[string]any
		if err := json.Unmarshal(ops, &doc); err != nil {
			t.Fatal(err)
		}
		x.edit(doc)
		broken, _ := json.Marshal(doc)
		if _, err := Parse(broken, fx); err == nil || !strings.Contains(err.Error(), x.why) {
			t.Errorf("%s: %v, want it to say %q", name, err, x.why)
		}
	}
}

// There is no alt in the flow language: a form cannot declare that it may skip a decoder it runs.
func TestTheFlowLanguageHasNoAlt(t *testing.T) {
	ops, _ := os.ReadFile("../../catalog/operations.json")
	fx, _ := os.ReadFile("../../catalog/fixtures.json")
	var doc map[string]any
	if err := json.Unmarshal(ops, &doc); err != nil {
		t.Fatal(err)
	}
	doc["constructors"].(map[string]any)["list"].(map[string]any)["flow"] = map[string]any{"alt": []any{"own", map[string]any{"each_element": map[string]any{"arg": "element"}}}}
	broken, _ := json.Marshal(doc)
	if err := schemasFor(t).Validate("operations", broken); err == nil {
		t.Error("the schema accepts alt")
	}
	if _, err := Parse(broken, fx); err == nil || !strings.Contains(err.Error(), `"alt" is not a flow`) {
		t.Errorf("the parser: %v", err)
	}
	if err := json.Unmarshal(ops, &doc); err != nil {
		t.Fatal(err)
	}
	for _, o := range doc["operations"].([]any) {
		if op := o.(map[string]any); op["name"] == "min" {
			op["issues"].([]any)[0].(map[string]any)["meta"] = map[string]any{"min": map[string]any{"arg": ""}}
		}
	}
	broken, _ = json.Marshal(doc)
	if _, err := Parse(broken, fx); err == nil || !strings.Contains(err.Error(), "reads , which is not") {
		t.Errorf("a metadata source naming the empty argument: %v", err)
	}
}

// An operation's arguments are complete once read: a value argument left out stands for its
// default, a message argument left out gives no message, and nothing else can be left out.
func TestLeftOutArgumentsHaveMeanings(t *testing.T) {
	c := checker(t)
	f := c.Registry().Operations["normalize"]["string"].Form
	ca, err := c.newState().args(f, nil, map[string]value.Type{"R": value.Of(value.String)})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := ca.values[argRef(0)]; !ok || v.Str != "NFC" {
		t.Errorf("normalize without a form reads %v (%v)", v, ok)
	}
	f = c.Registry().Operations["toInt"]["string"].Form
	ca, err = c.newState().args(f, nil, map[string]value.Type{"R": value.Of(value.String)})
	if err != nil || ca.message != nil {
		t.Errorf("toInt without a message gives the message %v (%v)", ca.message, err)
	}
	decoder(t, c, `["string", ["normalize"]]`)
	rejected(t, c, `["int", ["refine"]]`, "refine takes 1 to 1 argument(s), found 0")
}

// checkerWith is the checker for the catalogues with fixtures added.
func checkerWith(t *testing.T, fixtures doc) *Checker {
	t.Helper()
	return checkerWithOps(t, nil, fixtures)
}

// checkerWithOps is the checker for the catalogues with operations, fixtures and issue variants
// added.
func checkerWithOps(t *testing.T, operations []doc, fixtures doc, variants ...doc) *Checker {
	t.Helper()
	fx := readDoc(t, "../../catalog/fixtures.json")
	for name, f := range fixtures {
		fx[name] = f
	}
	od := readDoc(t, "../../catalog/operations.json")
	for _, o := range operations {
		od["operations"] = append(od["operations"].([]any), o)
	}
	ops := encode(t, od)
	reg, err := Parse(ops, encode(t, fx))
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load("../..", schemasFor(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(variants) > 0 {
		is := readDoc(t, "../../catalog/issues.json")
		for _, v := range variants {
			for k, e := range v {
				is[k] = e
			}
		}
		if cat.Variants, err = catalog.ParseVariants(encode(t, is)); err != nil {
			t.Fatal(err)
		}
	}
	c, err := NewChecker(reg, cat)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// A generic fixture's issue has the types its parameters take where it is used.
func TestFixtureIssuesAreInstantiated(t *testing.T) {
	c := checkerWith(t, doc{
		"nonzero_any": doc{"kind": "refine", "doc": "x", "input": "T",
			"issue": doc{"code": "invalid_value", "message_key": "invalid_value", "message": "", "meta": doc{"actual": "list<T>"}}},
	})
	for form, want := range map[string]string{
		`["int", ["refine", "nonzero_any"]]`:              "list<int32>",
		`["list", ["string"], ["refine", "nonzero_any"]]`: "list<list<string>>",
	} {
		site := issue(t, decoder(t, c, form), "invalid_value")
		if got := site.Meta["actual"].String(); got != want {
			t.Errorf("%s: actual is a %s, want %s", form, got, want)
		}
		if site.Message == nil || *site.Message != "" {
			t.Errorf("%s: the fixture gives the empty message, not %v", form, site.Message)
		}
	}
}

// A property reads the type its declared input takes, and the properties of an object encoder
// the type of their argument: no parameter name means anything by itself.
func TestPropertiesReadTheirDeclaredInput(t *testing.T) {
	checked, err := checker(t).CheckEncoder(jsontext.MustParse(`["object", [["propertyWithDefault", "value", "identity", ["string"], "default"]]]`))
	if err != nil {
		t.Fatal(err)
	}
	if got := checked.Result.String(); got != "nullable<string>" {
		t.Errorf("the object encodes a %s", got)
	}
}

// The overload of an operation is the one for the receiver's kind, whatever order
// operations.json lists them in, and the feature is that overload's.
func TestOverloadsAreChosenByKind(t *testing.T) {
	c := checker(t)
	for form, feature := range map[string]string{
		`["int", ["min", 1]]`:               "operation.int32.min",
		`["decimal", ["min", "1"]]`:         "operation.decimal.min",
		`["list", ["int"], ["minSize", 1]]`: "operation.list.minSize",
		`["string", ["minLength", 1]]`:      "operation.string.minLength",
		`["int", ["refine", "even"]]`:       "operation.any.refine",
	} {
		if d := decoder(t, c, form); !slices.Contains(d.Features, feature) {
			t.Errorf("%s needs %v, not %s", form, d.Features, feature)
		}
	}
}

// A fixture binds the types it takes and gives before the values of a form are read, and fixtures
// bind each other's types whatever order the form lists them in.
func TestFixturesBindTypesInAnyOrder(t *testing.T) {
	mapArg := func(name, in, out string) doc {
		return doc{"name": name, "kind": "fixture", "fixture": "map", "input": in, "output": out}
	}
	value := func(name, typ string) doc { return doc{"name": name, "kind": "value", "type": typ} }
	op := func(name, result string, args ...doc) doc {
		as := []any{}
		for _, a := range args {
			as = append(as, a)
		}
		return doc{"name": name, "doc": "x", "receivers": []any{"*"}, "result": result, "args": as, "issues": []any{}, "flow": "none"}
	}
	c := checkerWithOps(t, []doc{
		op("expectOutput", "R", mapArg("function", "R", "U"), value("expected", "U")),
		op("pipeAB", "Z", mapArg("a", "X", "Y"), mapArg("b", "Y", "Z"), value("x", "X")),
		op("pipeBA", "Z", mapArg("b", "Y", "Z"), mapArg("a", "X", "Y"), value("x", "X")),
	}, doc{
		"same":   doc{"kind": "map", "doc": "x", "input": "T", "output": "T"},
		"length": doc{"kind": "map", "doc": "x", "input": "string", "output": "int32"},
	})
	decoder(t, c, `["int", ["expectOutput", "decimal_string", "123"]]`)
	rejected(t, c, `["int", ["expectOutput", "decimal_string", 123]]`, "expected")
	for _, form := range []string{
		`["int", ["pipeAB", "same", "length", "abc"]]`,
		`["int", ["pipeBA", "length", "same", "abc"]]`,
	} {
		if got := decoder(t, c, form).Result.String(); got != "int32" {
			t.Errorf("%s gives %s", form, got)
		}
	}
	rejected(t, c, `["int", ["pipeAB", "same", "same", "abc"]]`, "cannot tell the types of fixture")
	if got := decoder(t, c, `["enum", ["A", "B"], ["string"], ["map", "same"]]`).Result.String(); got != `symbol<"A","B">` {
		t.Errorf("a generic fixture gives %s for a symbol", got)
	}
}

// Fixtures are one set of constraints: a type is known once anything fixes it, whether the types
// on both sides of a match mention parameters or two fixtures each fix part of it.
func TestFixturesSolveTogether(t *testing.T) {
	fixture := func(name, in, out string) doc {
		return doc{"name": name, "kind": "fixture", "fixture": "map", "input": in, "output": out}
	}
	op := func(name, result string, args ...any) doc {
		return doc{"name": name, "doc": "x", "receivers": []any{"*"}, "result": result, "args": args, "issues": []any{}, "flow": "none"}
	}
	c := checkerWithOps(t, []doc{
		op("infer", "Y", fixture("function", "product<X,string>", "Y")),
		op("joinAB", "product<X,Y>", fixture("a", "product<X,Y>", "Z"), fixture("b", "product<X,Y>", "W")),
		op("joinBA", "product<X,Y>", fixture("b", "product<X,Y>", "W"), fixture("a", "product<X,Y>", "Z")),
	}, doc{
		"pair":  doc{"kind": "map", "doc": "x", "input": "product<int32,T>", "output": "T"},
		"left":  doc{"kind": "map", "doc": "x", "input": "product<int32,T>", "output": "T"},
		"right": doc{"kind": "map", "doc": "x", "input": "product<U,string>", "output": "U"},
	})
	for form, want := range map[string]string{
		`["int", ["infer", "pair"]]`:           "string",
		`["int", ["joinAB", "left", "right"]]`: "product<int32,string>",
		`["int", ["joinBA", "right", "left"]]`: "product<int32,string>",
	} {
		if got := decoder(t, c, form).Result.String(); got != want {
			t.Errorf("%s gives %s, want %s", form, got, want)
		}
	}
	rejected(t, c, `["int", ["joinAB", "left", "left"]]`, "cannot tell the types of fixture")
	rejected(t, c, `["int", ["infer", "decimal_string"]]`, "fixture decimal_string takes int32, not product<X,string>")
}

// A type written with parameters is checked once they are bound, wherever it is used: an issue's
// metadata as well as a result.
func TestInstantiatedTypesAreWellFormed(t *testing.T) {
	c := checkerWithOps(t, []doc{{"name": "probe", "doc": "x", "receivers": []any{"*"}, "result": "R",
		"issues": []any{doc{"key": "probe", "T": "nullable<R>"}}, "flow": "own"}}, nil,
		doc{"probe": doc{"code": "probe", "params": []any{"T"}, "meta": doc{"x": "optional<T>"}}})
	rejected(t, c, `["string", ["probe"]]`, "optional<nullable<string>> cannot tell its own null")
}
