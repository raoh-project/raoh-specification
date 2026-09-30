package compare

import (
	"strings"
	"testing"

	"github.com/raoh-project/raoh-specification/internal/catalog"
	"github.com/raoh-project/raoh-specification/internal/dsl"
	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/schemas"
	"github.com/raoh-project/raoh-specification/internal/suite"
)

func oneCase(t *testing.T, text string) *suite.Case {
	t.Helper()
	cat, err := catalog.Load("../..", schemasFor(t))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := dsl.Load("../..", schemasFor(t))
	if err != nil {
		t.Fatal(err)
	}
	profile := "core"
	if strings.Contains(text, `"encoder"`) {
		profile = "encode"
	}
	cases, problems := suite.ParseFile("suite/"+profile+"/t.json", profile, []byte("["+text+"]"), &dsl.Checker{Registry: reg, Catalog: cat})
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	return cases[0]
}

func outcome(t *testing.T, text string) suite.Outcome {
	t.Helper()
	o, err := Parse(jsontext.MustParse(text))
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func matches(t *testing.T, c *suite.Case, observed string) {
	t.Helper()
	if ok, why := Expected(c, outcome(t, observed)); !ok {
		t.Errorf("%s: %s", observed, why)
	}
}

func differs(t *testing.T, c *suite.Case, observed, why string) {
	t.Helper()
	ok, got := Expected(c, outcome(t, observed))
	if ok {
		t.Errorf("%s matched", observed)
	} else if !strings.Contains(got, why) {
		t.Errorf("%s: %q, want it to say %q", observed, got, why)
	}
}

func TestOkComparesTypedValues(t *testing.T) {
	c := oneCase(t, `{"id": "R000001", "title": "t", "decoder": ["double"], "input": 2, "ok": 2.0}`)
	matches(t, c, `{"ok": 2}`)
	matches(t, c, `{"ok": 2e0}`)
	differs(t, c, `{"ok": {"float": "-0"}}`, "expected ok")
	differs(t, c, `{"ok": "2"}`, "not a float64")
	differs(t, c, `{"issues": [{"path": "", "code": "required", "message_key": "required", "message": "is required", "meta": {}}]}`, "observed issues")
}

func TestPathsAreEscapedAsJSONPointers(t *testing.T) {
	c := oneCase(t, `{"id": "R000001", "title": "t", "decoder": ["object", [["field", "a/b", ["int"]], ["field", "~c", ["int"]]]], "input": {},
		"issues": [
			{"path": "/a~1b", "code": "required", "message_key": "required", "meta": {}},
			{"path": "/~0c", "code": "required", "message_key": "required", "meta": {}}]}`)
	matches(t, c, `{"issues": [
		{"path": "/a~1b", "code": "required", "message_key": "required", "message": "is required", "meta": {}},
		{"path": "/~0c", "code": "required", "message_key": "required", "message": "is required", "meta": {}}]}`)
	differs(t, c, `{"issues": [
		{"path": "/a/b", "code": "required", "message_key": "required", "message": "is required", "meta": {}},
		{"path": "/~0c", "code": "required", "message_key": "required", "message": "is required", "meta": {}}]}`, "path")
	// Declared order matters when nothing reports in input order.
	differs(t, c, `{"issues": [
		{"path": "/~0c", "code": "required", "message_key": "required", "message": "is required", "meta": {}},
		{"path": "/a~1b", "code": "required", "message_key": "required", "message": "is required", "meta": {}}]}`, "issue 0")
}

func TestDerivedMessagesAndTypedMeta(t *testing.T) {
	c := oneCase(t, `{"id": "R000001", "title": "t", "decoder": ["double", ["min", 1e7]], "input": 1,
		"issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum", "meta": {"min": 1e7, "actual": 1}}]}`)
	matches(t, c, `{"issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum",
		"message": "must be at least 1.0E7", "meta": {"actual": 1.0, "min": 10000000}}]}`)
	differs(t, c, `{"issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum",
		"message": "must be at least 10000000", "meta": {"actual": 1.0, "min": 10000000}}]}`, "message")
	differs(t, c, `{"issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum",
		"meta": {"actual": 1.0, "min": 10000000}}]}`, "no message")
	differs(t, c, `{"issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum",
		"message": "must be at least 1.0E7", "meta": {"actual": 2, "min": 10000000}}]}`, "meta actual")
	differs(t, c, `{"issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum",
		"message": "must be at least 1.0E7", "meta": {"min": 10000000}}]}`, "metadata")
}

func TestNestedTypeParametersInMeta(t *testing.T) {
	c := oneCase(t, `{"id": "R000001", "title": "t", "decoder": ["float", ["oneOf", [2.0, 1.0]]], "input": 0.1,
		"issues": [{"path": "", "code": "not_allowed", "message_key": "not_allowed", "meta": {"allowed": [1.0, 2.0], "actual": 0.1}}]}`)
	matches(t, c, `{"issues": [{"path": "", "code": "not_allowed", "message_key": "not_allowed",
		"message": "must be one of [1.0, 2.0]", "meta": {"allowed": [1, 2], "actual": 0.1}}]}`)
	differs(t, c, `{"issues": [{"path": "", "code": "not_allowed", "message_key": "not_allowed",
		"message": "must be one of [1.0, 2.0]", "meta": {"allowed": [1, 2], "actual": 0.10000000149011612}}]}`, "meta actual")
}

func TestInputOrderedIssuesAreAMultiset(t *testing.T) {
	c := oneCase(t, `{"id": "R000001", "title": "t", "decoder": ["strictObject", [["field", "a", ["int"]]]], "input": {"a": 1, "z": 1, "y": 1, "z2": 1},
		"issues": [
			{"path": "/z", "code": "unknown_field", "message_key": "unknown_field", "meta": {"field": "z"}},
			{"path": "/y", "code": "unknown_field", "message_key": "unknown_field", "meta": {"field": "y"}},
			{"path": "/y", "code": "unknown_field", "message_key": "unknown_field", "meta": {"field": "y"}}]}`)
	issue := func(f string) string {
		return `{"path": "/` + f + `", "code": "unknown_field", "message_key": "unknown_field", "message": "unknown field", "meta": {"field": "` + f + `"}}`
	}
	matches(t, c, `{"issues": [`+issue("y")+`,`+issue("z")+`,`+issue("y")+`]}`)
	// The same issues, but y once and z twice: a set comparison would accept it.
	differs(t, c, `{"issues": [`+issue("y")+`,`+issue("z")+`,`+issue("z")+`]}`, "no match")
}

func TestSameComparesDeclaredOutcomes(t *testing.T) {
	c := oneCase(t, `{"id": "R000001", "title": "t", "decoder": ["double"], "input": 2, "ok": 2.0}`)
	if ok, why := Same(c, outcome(t, `{"ok": 3}`), outcome(t, `{"ok": 3.0}`)); !ok {
		t.Error(why)
	}
	declared := `{"issues": [{"path": "", "code": "x", "message_key": "x", "message": "m", "meta": {"n": 1}}]}`
	if ok, _ := Same(c, outcome(t, declared), outcome(t, declared)); !ok {
		t.Error("an outcome differs from itself")
	}
	if ok, _ := Same(c, outcome(t, declared), outcome(t, strings.Replace(declared, `"m"`, `"n"`, 1))); ok {
		t.Error("different messages are the same")
	}
}

func TestEncodingOutcomesAreJSON(t *testing.T) {
	c := oneCase(t, `{"id": "R000001", "title": "t", "encoder": ["object", [["propertyWithDefault", "value", "identity", ["string"], "default"]]], "value": null, "ok": {"value": "default"}}`)
	matches(t, c, `{"ok": {"value": "default"}}`)
	differs(t, c, `{"ok": {"value": null}}`, "expected")
}

func TestCatalog(t *testing.T) {
	got := Catalog(map[string]string{"a": "x", "b": "y", "c": "z"}, map[string]string{"a": "x", "b": "Y", "d": "w"})
	want := map[string]string{"a": "", "b": `expected "y", observed "Y"`, "c": "missing", "d": "not in the catalogue"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
}

func schemasFor(t *testing.T) *schemas.Set {
	t.Helper()
	sch, err := schemas.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

// Only the issues of one unordered group may come in any order; everything else keeps its place,
// and the group keeps its place among the others.
func TestUnorderedGroupsKeepTheirPlace(t *testing.T) {
	c := oneCase(t, `{"id": "R000001", "title": "t", "decoder": ["strict", ["discriminate", "kind", {
		"rect": ["strict", ["object", [["field", "w", ["int"]], ["field", "h", ["int"]]], ["map", "area"]], ["kind", "w", "h"]]}],
		["kind", "w", "h"]],
		"input": {"kind": "rect", "w": "2", "extra": 1, "more": 2, "h": 3},
		"issues": [
			{"path": "/w", "code": "type_mismatch", "message_key": "type_mismatch", "meta": {"expected": "integer", "actual": "string"}},
			{"path": "/extra", "code": "unknown_field", "message_key": "unknown_field", "meta": {"field": "extra"}},
			{"path": "/more", "code": "unknown_field", "message_key": "unknown_field", "meta": {"field": "more"}}]}`)
	w := `{"path": "/w", "code": "type_mismatch", "message_key": "type_mismatch", "message": "expected integer", "meta": {"expected": "integer", "actual": "string"}}`
	unknown := func(f string) string {
		return `{"path": "/` + f + `", "code": "unknown_field", "message_key": "unknown_field", "message": "unknown field", "meta": {"field": "` + f + `"}}`
	}
	matches(t, c, `{"issues": [`+w+`,`+unknown("extra")+`,`+unknown("more")+`]}`)
	matches(t, c, `{"issues": [`+w+`,`+unknown("more")+`,`+unknown("extra")+`]}`)
	differs(t, c, `{"issues": [`+unknown("extra")+`,`+w+`,`+unknown("more")+`]}`, "issue 0")
	differs(t, c, `{"issues": [`+unknown("extra")+`,`+unknown("more")+`,`+w+`]}`, "issue 0")
}

// The issues a oneOf's candidates report are typed by each candidate's decoder, so a float's
// sign is compared as the value model compares it.
func TestCandidatesAreTyped(t *testing.T) {
	c := oneCase(t, `{"id": "R000001", "title": "t", "decoder": ["oneOf", [["double", ["positive"]], ["double", ["oneOf", [1]]]]], "input": -0.0,
		"issues": [{"path": "", "code": "one_of_failed", "message_key": "one_of_failed", "meta": {"candidates": [
			{"candidate": 0, "issues": [{"path": "", "code": "out_of_range", "message": "must be positive", "meta": {"min": 0, "actual": {"float": "-0"}}}]},
			{"candidate": 1, "issues": [{"path": "", "code": "not_allowed", "message": "must be one of [1.0]", "meta": {"allowed": [1], "actual": {"float": "-0"}}}]}]}}]}`)
	obs := func(actual string) string {
		return `{"issues": [{"path": "", "code": "one_of_failed", "message_key": "one_of_failed", "message": "no variant matched", "meta": {"candidates": [
			{"candidate": 1, "issues": [{"path": "", "code": "not_allowed", "message": "must be one of [1.0]", "meta": {"allowed": [1.0], "actual": {"float": "-0"}}}]},
			{"candidate": 0, "issues": [{"path": "", "code": "out_of_range", "message": "must be positive", "meta": {"min": 0.0, "actual": ` + actual + `}}]}]}}]}`
	}
	matches(t, c, obs(`{"float": "-0"}`))
	differs(t, c, obs(`0`), "candidate 0")
}
