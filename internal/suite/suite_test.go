package suite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raoh-project/raoh-specification/internal/catalog"
	"github.com/raoh-project/raoh-specification/internal/dsl"
)

func checker(t *testing.T) *dsl.Checker {
	t.Helper()
	cat, err := catalog.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	reg, err := dsl.Load("../..")
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
		{"id": "double.negative_zero", "decoder": ["double"], "input": -0.0, "ok": {"float": "-0"}},
		{"id": "long.too_big", "decoder": ["long"], "input": 12345678901234567890,
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
	rejected(t, "core", `[{"id": "a.b", "decoder": ["dict", ["int"]], "input": {"a": 1, "a": 2}, "ok": {"a": 2}}]`, "more than once")
}

func TestExpectedOutcomesAreTyped(t *testing.T) {
	rejected(t, "core", `[{"id": "a.b", "decoder": ["int"], "input": 1, "ok": "1"}]`, "expected number")
	rejected(t, "core", `[{"id": "a.b", "decoder": ["float"], "input": 16777217, "ok": 16777217}]`, "rounds to")
	rejected(t, "core", `[{"id": "a.b", "decoder": ["int"], "input": 1, "ok": 1, "issues": []}]`, "either ok or issues")
	rejected(t, "core", `[{"id": "a.b", "decoder": ["int"], "input": "x",
		"issues": [{"path": "", "code": "too_short", "message_key": "too_short", "meta": {"min": 1, "actual": 0}}]}]`, "cannot give too_short")
	rejected(t, "core", `[{"id": "a.b", "decoder": ["int", ["min", 1]], "input": 0,
		"issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum", "meta": {"min": 1}}]}]`, "needs metadata actual")
	rejected(t, "core", `[{"id": "a.b", "decoder": ["double", ["min", 1]], "input": 0,
		"issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum", "meta": {"min": 1, "actual": 0, "max": 2}}]}]`, "no metadata max")
	rejected(t, "core", `[{"id": "a.b", "decoder": ["int", ["refine", "even"]], "input": 3,
		"issues": [{"path": "", "code": "must_be_even", "message_key": "must_be_even", "meta": {"actual": 3}}]}]`, "has to write")
	rejected(t, "core", `[{"id": "a.b", "decoder": ["int"], "input": "x",
		"issues": [{"path": "x", "code": "type_mismatch", "message_key": "type_mismatch", "meta": {"expected": "integer"}}]}]`, "JSON Pointer")
}

func TestMessagesAreDerivedOrGiven(t *testing.T) {
	cases := accepted(t, "core", `[
		{"id": "a.derived", "decoder": ["double", ["min", 1e7]], "input": 1,
		 "issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum", "meta": {"min": 1e7, "actual": 1}}]},
		{"id": "a.given", "decoder": ["string", ["toInt", "bad"]], "input": "x",
		 "issues": [{"path": "", "code": "type_mismatch", "message_key": "type_mismatch", "message": "bad", "meta": {"expected": "integer"}}]},
		{"id": "a.fixture", "decoder": ["int", ["refine", "even"]], "input": 3,
		 "issues": [{"path": "", "code": "must_be_even", "message_key": "must_be_even", "message": "must be even", "meta": {"actual": 3}}]}
	]`)
	for i, want := range []string{"must be at least 1.0E7", "bad", "must be even"} {
		if got := cases[i].Issues[0].Message; got != want {
			t.Errorf("%s: %q, want %q", cases[i].ID, got, want)
		}
	}
}

func TestCaseShape(t *testing.T) {
	rejected(t, "core", `[{"id": "A.b", "decoder": ["int"], "input": 1, "ok": 1}]`, "lower snake case")
	rejected(t, "core", `[{"id": "single", "decoder": ["int"], "input": 1, "ok": 1}]`, "lower snake case")
	rejected(t, "core", `[{"id": "a.b", "decoder": ["int"], "ok": 1}]`, "needs an input")
	rejected(t, "core", `[{"id": "a.b", "decoder": ["int"], "input": 1, "ok": 1, "note": ""}]`, "unknown member")
	rejected(t, "core", `[{"id": "a.b", "encoder": ["string"], "value": "x", "ok": "x"}]`, "suite/encode")
	accepted(t, "encode", `[{"id": "a.b", "encoder": ["object", [["propertyWithDefault", "value", "identity", ["string"], "default"]]], "value": null, "ok": {"value": "default"}}]`)
	rejected(t, "encode", `[{"id": "a.b", "encoder": ["string"], "value": 1, "ok": "x"}]`, "value")
}

func TestLoadRejectsRepeatedIDs(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"catalog/messages", "suite/core"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("suite/core/a.json", `[{"id": "int.one", "decoder": ["int"], "input": 1, "ok": 1}]`)
	write("suite/core/b.json", `[{"id": "int.one", "decoder": ["int"], "input": 2, "ok": 2}]`)
	_, err := Load(root, checker(t))
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
