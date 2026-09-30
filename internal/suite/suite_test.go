package suite

import (
	"strings"
	"testing"

	"github.com/raoh-project/raoh-specification/internal/artifacts/artifactstest"
	"github.com/raoh-project/raoh-specification/internal/catalog"
	"github.com/raoh-project/raoh-specification/internal/dsl"
	"github.com/raoh-project/raoh-specification/internal/schemas"
)

func checker(t *testing.T) *dsl.Checker {
	t.Helper()
	cat, err := catalog.Load("../..", schemasFor(t))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := dsl.Load("../..", schemasFor(t))
	if err != nil {
		t.Fatal(err)
	}
	return &dsl.Checker{Registry: reg, Catalog: cat}
}

func parse(t *testing.T, profile, text string) ([]*Case, []string) {
	t.Helper()
	return ParseFile("suite/"+profile+"/test.json", profile, []byte(text), checker(t))
}

func accepted(t *testing.T, profile, text string) []*Case {
	t.Helper()
	cases, problems := parse(t, profile, text)
	if len(problems) > 0 {
		t.Fatalf("%v", problems)
	}
	return cases
}

func rejected(t *testing.T, profile, text, why string) {
	t.Helper()
	_, problems := parse(t, profile, text)
	if len(problems) == 0 {
		t.Errorf("%s accepted", text)
	} else if !strings.Contains(strings.Join(problems, "\n"), why) {
		t.Errorf("%s: %v, want it to say %q", text, problems, why)
	}
}

func TestInputsKeepTheirLexemes(t *testing.T) {
	cases := accepted(t, "core", `[
		{"id": "R000001", "title": "t", "decoder": ["double"], "input": -0.0, "ok": {"float": "-0"}},
		{"id": "R000002", "title": "t", "decoder": ["long"], "input": 12345678901234567890,
		 "issues": [{"path": "", "code": "type_mismatch", "message_key": "type_mismatch.numeric_range", "meta": {"expected": "long"}}]}
	]`)
	if string(cases[0].Input.Raw) != "-0.0" || string(cases[1].Input.Raw) != "12345678901234567890" {
		t.Errorf("inputs %s and %s", cases[0].Input.Raw, cases[1].Input.Raw)
	}
	if got := cases[1].Issues[0].Message; got != "value is outside the long range" {
		t.Errorf("derived message %q", got)
	}
}

func TestRepeatedMemberNamesInAnInputAreRejected(t *testing.T) {
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["dict", ["int"]], "input": {"a": 1, "a": 2}, "ok": {"a": 2}}]`, "more than once")
}

func TestExpectedOutcomesAreTyped(t *testing.T) {
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["int"], "input": 1, "ok": "1"}]`, "expected number")
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["float"], "input": 16777217, "ok": 16777217}]`, "rounds to")
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["int"], "input": 1, "ok": 1, "issues": []}]`, "either ok or issues")
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["int"], "input": "x",
		"issues": [{"path": "", "code": "too_short", "message_key": "too_short", "meta": {"min": 1, "actual": 0}}]}]`, "gives no too_short")
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["int", ["min", 1]], "input": 0,
		"issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum", "meta": {"min": 1}}]}]`, "needs metadata actual")
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["double", ["min", 1]], "input": 0,
		"issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum", "meta": {"min": 1, "actual": 0, "max": 2}}]}]`, "no metadata max")
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["int", ["refine", "even"]], "input": 3,
		"issues": [{"path": "", "code": "must_be_even", "message_key": "must_be_even", "meta": {"actual": 3}}]}]`, "has to write")
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["int"], "input": "x",
		"issues": [{"path": "x", "code": "type_mismatch", "message_key": "type_mismatch", "meta": {"expected": "integer"}}]}]`, "JSON Pointer")
}

func TestMessagesAreDerivedOrGiven(t *testing.T) {
	cases := accepted(t, "core", `[
		{"id": "R000001", "title": "t", "decoder": ["double", ["min", 1e7]], "input": 1,
		 "issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum", "meta": {"min": 1e7, "actual": 1}}]},
		{"id": "R000002", "title": "t", "decoder": ["string", ["toInt", "bad"]], "input": "x",
		 "issues": [{"path": "", "code": "type_mismatch", "message_key": "type_mismatch", "message": "bad", "meta": {"expected": "integer"}}]},
		{"id": "R000003", "title": "t", "decoder": ["int", ["refine", "even"]], "input": 3,
		 "issues": [{"path": "", "code": "must_be_even", "message_key": "must_be_even", "message": "must be even", "meta": {"actual": 3}}]}
	]`)
	for i, want := range []string{"must be at least 1.0E7", "bad", "must be even"} {
		if got := cases[i].Issues[0].Message; got != want {
			t.Errorf("%s: %q, want %q", cases[i].ID, got, want)
		}
	}
}

func TestCaseShape(t *testing.T) {
	rejected(t, "core", `[{"id": "int.accepts_1", "title": "t", "decoder": ["int"], "input": 1, "ok": 1}]`, "R and six digits")
	rejected(t, "core", `[{"id": "R12345", "title": "t", "decoder": ["int"], "input": 1, "ok": 1}]`, "R and six digits")
	rejected(t, "core", `[{"id": "R000001", "decoder": ["int"], "input": 1, "ok": 1}]`, "title")
	rejected(t, "core", `[{"id": "R000001", "title": " ", "decoder": ["int"], "input": 1, "ok": 1}]`, "title")
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["int"], "ok": 1}]`, "needs an input")
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["int"], "input": 1, "ok": 1, "note": ""}]`, "unknown member")
	rejected(t, "core", `[{"id": "R000001", "title": "t", "encoder": ["string"], "value": "x", "ok": "x"}]`, "suite/encode")
	accepted(t, "encode", `[{"id": "R000001", "title": "t", "encoder": ["object", [["propertyWithDefault", "value", "identity", ["string"], "default"]]], "value": null, "ok": {"value": "default"}}]`)
	rejected(t, "encode", `[{"id": "R000001", "title": "t", "encoder": ["string"], "value": 1, "ok": "x"}]`, "value")
}

func TestLoadRejectsRepeatedIDs(t *testing.T) {
	root := artifactstest.Copy(t, "../..", map[string]string{
		"suite/core/a.json": `[{"id": "R000001", "title": "t", "decoder": ["int"], "input": 1, "ok": 1}]`,
		"suite/core/b.json": `[{"id": "R000001", "title": "t", "decoder": ["int"], "input": 2, "ok": 2}]`,
	})
	_, err := Load(root, checker(t), schemasFor(t))
	if err == nil || !strings.Contains(err.Error(), "also used in suite/core/a.json") {
		t.Errorf("got %v", err)
	}
}

func TestPaths(t *testing.T) {
	segs, err := SplitPath("/a~1b/~0c/2")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(segs, "|") != "a/b|~c|2" {
		t.Errorf("segments %q", segs)
	}
	if got := JoinPath(segs); got != "/a~1b/~0c/2" {
		t.Errorf("joined %q", got)
	}
	for _, bad := range []string{"a", "/~", "/~2"} {
		if _, err := SplitPath(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestMessagesMustBeWhereTheyAreGiven(t *testing.T) {
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["int", ["min", 1]], "input": 0,
		"issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum", "message": "anything", "meta": {"min": 1, "actual": 0}}]}]`, "derived")
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["string", ["toInt", "bad"]], "input": "x",
		"issues": [{"path": "", "code": "type_mismatch", "message_key": "type_mismatch", "meta": {"expected": "integer"}}]}]`, `"bad"`)
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["string", ["toInt", "bad"]], "input": "x",
		"issues": [{"path": "", "code": "type_mismatch", "message_key": "type_mismatch", "message": "worse", "meta": {"expected": "integer"}}]}]`, `"bad"`)
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["int", ["refine", "even"]], "input": 3,
		"issues": [{"path": "", "code": "must_be_even", "message_key": "must_be_even", "message": "totally different", "meta": {"actual": 3}}]}]`, `"must be even"`)
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["object", [["field", "start", ["int"]], ["field", "end", ["int"]]], ["flatMap", "ordered_period"]],
		"input": {"start": 3, "end": 2},
		"issues": [{"path": "/zzz", "code": "invalid_value", "message_key": "invalid_value", "message": "end is before start", "meta": {}}]}]`, `no invalid_value at "/zzz"`)
	accepted(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["object", [["field", "id", ["int"]], ["field", "period",
		["object", [["field", "start", ["int"]], ["field", "end", ["int"]]], ["flatMap", "ordered_period"]]]]],
		"input": {"id": 1, "period": {"start": 3, "end": 2}},
		"issues": [{"path": "/period/end", "code": "invalid_value", "message_key": "invalid_value", "message": "end is before start", "meta": {}}]}]`)
}

// The path of an issue says which part of the decoder gave it, and so how its metadata is typed.
func TestIssuesAreTypedByWhereTheyArise(t *testing.T) {
	cases := accepted(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["object", [["field", "a", ["int", ["oneOf", [1, 2]]]], ["field", "b", ["double", ["oneOf", [1, 2]]]]]],
		"input": {"a": 1, "b": 3},
		"issues": [{"path": "/b", "code": "not_allowed", "message_key": "not_allowed", "meta": {"allowed": [1, 2], "actual": 3}}]}]`)
	e := cases[0].Issues[0]
	if e.Meta["actual"].Type.String() != "float64" || e.Message != "must be one of [1.0, 2.0]" {
		t.Errorf("typed %s, message %q", e.Meta["actual"].Type, e.Message)
	}
}

// Two flat fields that read the same member can give an issue at the same path with different
// types; which one gave it is not in the issue, so the case is rejected.
func TestAnIssueThatFitsTwoTypingsIsRejected(t *testing.T) {
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["object", [
		["flat", ["object", [["field", "x", ["int", ["oneOf", [1, 2]]]]]]],
		["flat", ["object", [["field", "x", ["double", ["oneOf", [1, 2]]]]]]]]],
		"input": {"x": 3},
		"issues": [{"path": "/x", "code": "not_allowed", "message_key": "not_allowed", "meta": {"allowed": [1, 2], "actual": 3}}]}]`, "two ways")
}

func schemasFor(t *testing.T) *schemas.Set {
	t.Helper()
	sch, err := schemas.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

func TestIDsAreKept(t *testing.T) {
	load := func(files map[string]string) *Suite {
		t.Helper()
		s, err := Load(artifactstest.Copy(t, "../..", files), checker(t), schemasFor(t))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	one := `{"id": "R000001", "title": "t", "decoder": ["int"], "input": 1, "ok": 1}`
	two := `{"id": "R000002", "title": "t", "decoder": ["int"], "input": 2, "ok": 2}`
	base := load(map[string]string{"suite/core/a.json": "[" + one + "," + two + "]"})
	if err := CheckIDs(base, load(map[string]string{"suite/core/a.json": "[" + one + "," + two + "]"})); err != nil {
		t.Error(err)
	}
	if err := CheckIDs(base, load(map[string]string{"suite/core/a.json": "[" + one + "]"})); err == nil || !strings.Contains(err.Error(), "R000002 was removed") {
		t.Errorf("a silently removed ID: %v", err)
	}
	retired := load(map[string]string{"suite/core/a.json": "[" + one + "]", "suite/retired.json": `["R000002"]`})
	if err := CheckIDs(base, retired); err != nil {
		t.Error(err)
	}
	if err := CheckIDs(retired, load(map[string]string{"suite/core/a.json": "[" + one + "]"})); err == nil || !strings.Contains(err.Error(), "no longer listed") {
		t.Errorf("an unretired ID: %v", err)
	}
	_, err := Load(artifactstest.Copy(t, "../..", map[string]string{"suite/core/a.json": "[" + two + "]", "suite/retired.json": `["R000002"]`}), checker(t), schemasFor(t))
	if err == nil || !strings.Contains(err.Error(), "is retired") {
		t.Errorf("a reused ID: %v", err)
	}
}

// Metadata the form decides has to be what the form decides; metadata known only when a decoder
// runs is typed and not recomputed.
func TestMetaTheFormDecides(t *testing.T) {
	issue := func(decoder, input, key, code, meta string) string {
		return `[{"id": "R000001", "title": "t", "decoder": ` + decoder + `, "input": ` + input + `,
			"issues": [{"path": "", "code": "` + code + `", "message_key": "` + key + `", "meta": ` + meta + `}]}]`
	}
	rejected(t, "core", issue(`["int", ["min", 1]]`, `0`, "out_of_range.minimum", "out_of_range", `{"min": 5, "actual": 0}`), "gives out_of_range.minimum the min 1")
	accepted(t, "core", issue(`["int", ["min", 1]]`, `0`, "out_of_range.minimum", "out_of_range", `{"min": 1, "actual": 7}`))
	rejected(t, "core", issue(`["string", ["oneOf", ["b", "a"]]]`, `"c"`, "not_allowed", "not_allowed", `{"allowed": ["b", "a"], "actual": "c"}`), "the allowed [a, b]")
	rejected(t, "core", issue(`["enum", ["RED", "GREEN"], ["string"]]`, `"x"`, "invalid_format.enum", "invalid_format", `{"allowed": ["red", "green"]}`), "the allowed [green, red]")
	rejected(t, "core", issue(`["long", ["positive"]]`, `0`, "out_of_range.positive", "out_of_range", `{"min": 0, "actual": 0}`), "the min 1")
	rejected(t, "core", issue(`["double", ["positive"]]`, `0`, "out_of_range.positive", "out_of_range", `{"min": 1, "actual": 0}`), "the min 0.0")
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": ["strictObject", [["field", "a", ["int"]]]], "input": {"a": 1, "b": 2},
		"issues": [{"path": "/b", "code": "unknown_field", "message_key": "unknown_field", "meta": {"field": "a"}}]}]`, "member's name")
}

// A oneOf fails only when every candidate fails, so its one_of_failed reports every candidate once,
// and each candidate's issues come from that candidate's flow, messages included.
func TestCandidatesAreComplete(t *testing.T) {
	c := func(candidates string) string {
		return `[{"id": "R000001", "title": "t", "decoder": ["oneOf", [["int", ["min", 1]], ["int", ["max", -1]]]], "input": 0,
			"issues": [{"path": "", "code": "one_of_failed", "message_key": "one_of_failed", "meta": {"candidates": [` + candidates + `]}}]}]`
	}
	zero := `{"candidate": 0, "issues": [{"path": "", "code": "out_of_range", "message": "must be at least 1", "meta": {"min": 1, "actual": 0}}]}`
	one := `{"candidate": 1, "issues": [{"path": "", "code": "out_of_range", "message": "must be at most -1", "meta": {"max": -1, "actual": 0}}]}`
	accepted(t, "core", c(one+","+zero))
	rejected(t, "core", c(zero), "candidate 1 is missing")
	rejected(t, "core", c(zero+","+zero+","+one), "appears twice")
	rejected(t, "core", c(strings.Replace(zero, "must be at least 1", "banana", 1)+","+one), `not "banana"`)
	rejected(t, "core", c(strings.Replace(zero, `"min": 1`, `"min": 2`, 1)+","+one), "the min 1")
}

// A case can only expect an issue list the decoder's flow gives: exclusive issues one at a time,
// nothing after an operation that failed, one variant's issues, and every member a strict form
// does not know.
func TestCasesExpectOnlyWhatTheFlowGives(t *testing.T) {
	issue := func(path, code, key, meta string) string {
		return `{"path": "` + path + `", "code": "` + code + `", "message_key": "` + key + `", "meta": ` + meta + `}`
	}
	c := func(decoder, input string, issues ...string) string {
		return `[{"id": "R000001", "title": "t", "decoder": ` + decoder + `, "input": ` + input + `, "issues": [` + strings.Join(issues, ", ") + `]}]`
	}
	required := issue("", "required", "required", `{}`)
	mismatch := issue("", "type_mismatch", "type_mismatch", `{"actual": "null", "expected": "integer"}`)
	rejected(t, "core", c(`["int"]`, `null`, required, mismatch), "together or in this order")
	accepted(t, "core", c(`["int"]`, `null`, required))
	short := issue("", "too_short", "too_short", `{"min": 3, "actual": 1}`)
	email := issue("", "invalid_format", "invalid_format.email", `{}`)
	rejected(t, "core", c(`["string", ["minLength", 3], ["email"]]`, `"a"`, short, email), "together or in this order")
	accepted(t, "core", c(`["string", ["minLength", 3], ["email"]]`, `"a"`, short))
	variants := `["discriminate", "kind", {"a": ["object", [["field", "x", ["int"]]]], "b": ["object", [["field", "y", ["int"]]]]}]`
	rejected(t, "core", c(variants, `{"kind": "a"}`, issue("/x", "required", "required", `{}`), issue("/y", "required", "required", `{}`)), "together or in this order")
	strict := `["strictObject", [["field", "a", ["int"]]]]`
	unknown := func(f string) string { return issue("/"+f, "unknown_field", "unknown_field", `{"field": "`+f+`"}`) }
	accepted(t, "core", c(strict, `{"a": 1, "b": 1, "c": 1}`, unknown("c"), unknown("b")))
	rejected(t, "core", c(strict, `{"a": 1, "b": 1, "c": 1}`, unknown("b")), "together or in this order")
	rejected(t, "core", `[{"id": "R000001", "title": "t", "decoder": `+strict+`, "input": {"a": 1, "b": 1}, "ok": [1]}]`, "gives issues for this input whatever it does")
}
