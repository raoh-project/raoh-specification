package dsl

import (
	"slices"
	"strings"
	"testing"

	"github.com/raoh-project/raoh-specification/catalog"
	"github.com/raoh-project/raoh-specification/jsontext"
)

func checker(t *testing.T) *Checker {
	t.Helper()
	cat, err := catalog.Load("..")
	if err != nil {
		t.Fatal(err)
	}
	reg, err := Load("..")
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

func issue(t *testing.T, checked *Checked, key string) Possible {
	t.Helper()
	for _, p := range checked.Issues {
		if p.Key == key {
			return p
		}
	}
	t.Fatalf("no possible issue %s among %v", key, checked.Issues)
	return Possible{}
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
		`["enum", ["RED", "GREEN"], ["string", ["trim"]]]`:                                                       "symbol",
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
	rejected(t, c, `["int", ["min", 0.5]]`, "without a fraction")
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
	if len(d.Issues) != 0 {
		t.Errorf("recover can give %v", d.Issues)
	}
	d = decoder(t, c, `["oneOf", [["int", ["map", "decimal_string"]], ["string", ["minLength", 3]]]]`)
	if len(d.Issues) != 1 || d.Issues[0].Key != "one_of_failed" {
		t.Errorf("oneOf can give %v", d.Issues)
	}
}

func TestInputOrder(t *testing.T) {
	c := checker(t)
	if decoder(t, c, `["object", [["field", "a", ["int"]]]]`).InputOrder {
		t.Error("object is input-ordered")
	}
	if !decoder(t, c, `["list", ["strictObject", [["field", "a", ["int"]]]]]`).InputOrder {
		t.Error("strictObject inside a list is not input-ordered")
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
