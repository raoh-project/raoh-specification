package dsl

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/raoh-project/raoh-specification/internal/catalog"
	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/schemas"
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
	c := &Checker{Registry: reg, Catalog: cat}
	if err := c.Validate(); err != nil {
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
	var all []*Site
	var walk func(f Flow)
	walk = func(f Flow) {
		switch f := f.(type) {
		case *Site:
			all = append(all, f)
		case Seq:
			for _, i := range f.Items {
				walk(i)
			}
		case *Unordered:
			walk(f.Body)
		case Repeat:
			walk(f.Body)
		case At:
			walk(f.Body)
		}
	}
	walk(checked.Flow)
	for _, p := range all {
		if p.Key == key {
			return p
		}
	}
	t.Fatalf("no site %s among %v", key, Describe(checked.Flow))
	return nil
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
	all := c.Registry.Features()
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
	if p.Message != "must be even" || p.Meta["actual"].String() != "int32" {
		t.Errorf("got %+v", p)
	}
}

func TestReplacedIssuesAreNotPossible(t *testing.T) {
	c := checker(t)
	d := decoder(t, c, `["recover", ["int", ["min", 1]], 1]`)
	if got := Describe(d.Flow); len(got) != 0 {
		t.Errorf("recover can give %v", got)
	}
	d = decoder(t, c, `["oneOf", [["int", ["map", "decimal_string"]], ["string", ["minLength", 3]]]]`)
	one := issue(t, d, "one_of_failed")
	if got := Describe(d.Flow); len(got) != 1 || len(one.Candidates) != 2 {
		t.Errorf("oneOf can give %v", got)
	}
	if got := strings.Join(Describe(one.Candidates[1]), "|"); got != " required| type_mismatch| too_short" {
		t.Errorf("the second candidate's sites: %s", got)
	}
}

// A flow keeps where each issue arises and which issues come in the input's order, so that
// neither is lost to the rest of the decoder. Within a form, the flows of its arguments come
// first and its own issues after them; each Unordered has its own ID.
func TestFlowsKeepPathsAndOrder(t *testing.T) {
	c := checker(t)
	for form, want := range map[string]string{
		`["object", [["field", "name", ["string"]], ["optionalField", "nick", ["int", ["min", 1]]]]]`: "" +
			"/name required|/name type_mismatch|/name type_mismatch|/nick required|/nick type_mismatch|/nick type_mismatch.numeric_range|/nick out_of_range.minimum",
		`["list", ["object", [["field", "q", ["int", ["positive"]]]]], ["nonempty"]]`: "" +
			"/*/q required|/*/q type_mismatch|/*/q type_mismatch.numeric_range|/*/q out_of_range.positive|/*/q type_mismatch|" +
			" required| type_mismatch| too_small.nonempty",
		`["strict", ["object", [["field", "w", ["int"]]]], ["kind", "w"]]`: "" +
			"/w required|/w type_mismatch|/w type_mismatch.numeric_range|/w type_mismatch|/*-{kind,w} unknown_field (unordered#1)",
		`["strict", ["strict", ["object", [["field", "a", ["int"]]]], ["a", "x"]], ["a", "y"]]`: "" +
			"/a required|/a type_mismatch|/a type_mismatch.numeric_range|/a type_mismatch|" +
			"/*-{a,x} unknown_field (unordered#1)|/*-{a,y} unknown_field (unordered#2)",
		`["discriminate", "kind", {"a": ["strictObject", [["field", "x", ["int"]]]]}]`: "" +
			"/x required|/x type_mismatch|/x type_mismatch.numeric_range|/x type_mismatch|/*-{x} unknown_field (unordered#1)|" +
			"/kind required|/kind type_mismatch|/kind not_allowed",
		`["object", [["field", "p", ["object", [["field", "s", ["int"]], ["field", "e", ["int"]]], ["flatMap", "ordered_period"]]]]]`: "" +
			"/p/s required|/p/s type_mismatch|/p/s type_mismatch.numeric_range|/p/s type_mismatch|" +
			"/p/e required|/p/e type_mismatch|/p/e type_mismatch.numeric_range|/p/e type_mismatch|/p/end invalid_value|/p type_mismatch",
	} {
		if got := strings.Join(Describe(decoder(t, c, form).Flow), "|"); got != want {
			t.Errorf("%s:\n got %s\nwant %s", form, got, want)
		}
	}
	rejected(t, c, `["strictObject", [["flat", ["object", [["field", "a", ["int"]]]]]]]`, "flat field")
}

// For an input, a flow's repeats expand over its elements and members, and a strict form's
// unknown members are the ones it does not know.
func TestInstantiate(t *testing.T) {
	c := checker(t)
	d := decoder(t, c, `["strict", ["strict", ["object", [["field", "a", ["int"]]]], ["a", "x"]], ["a", "y"]]`)
	var got []string
	for _, s := range Instantiate(d.Flow, jsontext.MustParse(`{"a": 1, "x": 1, "y": 1, "z": 1}`), nil) {
		if s.Key == "unknown_field" {
			got = append(got, JoinPath(s.Path)+" "+s.Group)
		}
	}
	want := []string{`/y unordered#1 at ""`, `/z unordered#1 at ""`, `/x unordered#2 at ""`, `/z unordered#2 at ""`}
	if !slices.Equal(got, want) {
		t.Errorf("got %v", got)
	}
	d = decoder(t, c, `["list", ["int"]]`)
	if n := len(Instantiate(d.Flow, jsontext.MustParse(`[1, 2, 3]`), nil)); n != 3*3+2 {
		t.Errorf("a list of three gives %d slots", n)
	}
	if n := len(Instantiate(d.Flow, jsontext.MustParse(`{"a": 1}`), nil)); n != 2 {
		t.Errorf("a list decoder given an object gives %d slots", n)
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
