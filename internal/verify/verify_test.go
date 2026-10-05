package verify

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/raoh-project/raoh-specification/internal/artifacts/artifactstest"
)

const miniCore = `[
  {"id": "R000001", "title": "int accepts 1", "decoder": ["int"], "input": 1, "ok": 1},
  {"id": "R000002", "title": "int.min(1) rejects 0", "decoder": ["int", ["min", 1]], "input": 0,
   "issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum", "meta": {"min": 1, "actual": 0}}]},
  {"id": "R000003", "title": "string.cuid() rejects x", "decoder": ["string", ["cuid"]], "input": "x",
   "issues": [{"path": "", "code": "invalid_format", "message_key": "invalid_format.cuid", "meta": {}}]},
  {"id": "R000005", "title": "int.min(1, \"low\") rejects 0", "decoder": ["int", ["min", 1, "low"]], "input": 0,
   "issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum", "message": "low", "meta": {"min": 1, "actual": 0}}]}
]`

const miniEncode = `[
  {"id": "R000004", "title": "null encodes to the default",
   "encoder": ["object", [["propertyWithDefault", "value", "identity", ["string"], "default"]]],
   "value": null, "ok": {"value": "default"}}
]`

// miniSpec builds a specification with the real catalogues and schemas and a small suite.
func miniSpec(t *testing.T) *Spec {
	t.Helper()
	root := artifactstest.Copy(t, "../..", map[string]string{
		"suite/core/mini.json":   miniCore,
		"suite/encode/mini.json": miniEncode,
	})
	s, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const (
	okInt       = `{"ok": 1}`
	minIssue    = `{"issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum", "message": "must be at least 1", "meta": {"min": 1, "actual": 0}}]}`
	cuidIssue   = `{"issues": [{"path": "", "code": "invalid_format", "message_key": "invalid_format.cuid", "message": "not a valid CUID", "meta": {}}]}`
	lowIssue    = `{"issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum", "message": "low", "meta": {"min": 1, "actual": 0}}]}`
	defaultJSON = `{"ok": {"value": "default"}}`
)

var allFeatures = []string{"decoder.int", "decoder.string", "operation.int32.min", "operation.int32.min.message", "operation.string.cuid",
	"encoder.object", "encoder.string", "property.propertyWithDefault", "fixture.identity"}

type run struct {
	digest   string
	version  string
	bound    []string
	results  map[string]string
	catalogs map[string]map[string]string
}

func (s *Spec) run() *run {
	return &run{
		digest:  s.Digest,
		version: s.Version,
		bound:   allFeatures,
		results: map[string]string{
			"R000001": okInt,
			"R000002": minIssue,
			"R000003": cuidIssue,
			"R000004": defaultJSON,
			"R000005": lowIssue,
		},
		catalogs: map[string]map[string]string{"en": s.Catalog.Messages["en"], "ja": s.Catalog.Messages["ja"]},
	}
}

func (r *run) json(t *testing.T) []byte {
	t.Helper()
	results := map[string]any{}
	for id, o := range r.results {
		results[id] = map[string]json.RawMessage{"observed": json.RawMessage(o)}
	}
	out, err := json.Marshal(map[string]any{
		"format":         "raoh-runner-result/v1",
		"specification":  map[string]string{"version": r.version, "revision": "abc", "manifest_digest": r.digest},
		"implementation": map[string]string{"name": "raoh-test", "version": "1.0.0", "revision": "def"},
		"environment":    map[string]string{"language": "test"},
		"bound_features": r.bound,
		"results":        results,
		"catalogs":       r.catalogs,
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func declare(version, extra string) []byte {
	return []byte(`{"implementation": "raoh-test", "specification": "` + version + `",
		"profiles": ["core", "encode", "messages-en", "messages-ja"]` + extra + `}`)
}

func verify(t *testing.T, s *Spec, r *run, decl []byte) (*Report, error) {
	t.Helper()
	return Verify(s, r.json(t), decl, "test")
}

func outcome(rep *Report, id string) CaseResult {
	for _, c := range rep.Cases {
		if c.ID == id {
			return c
		}
	}
	return CaseResult{}
}

func TestClassification(t *testing.T) {
	s := miniSpec(t)
	v := s.Version
	divergentMin := `, "divergences": {"R000002": {"category": "design", "reason": "r",
		"observed": {"issues": [{"path": "", "code": "out_of_range", "message_key": "out_of_range.minimum", "message": "too small", "meta": {"min": 1, "actual": 0}}]}}}`
	for _, c := range []struct {
		name     string
		change   func(*run)
		decl     []byte
		invalid  string
		statuses map[string]string
		outcomes map[string]string
	}{
		{
			name:     "everything matches",
			decl:     declare(v, ""),
			statuses: map[string]string{"core": Conformant, "encode": Conformant, "messages-en": Conformant, "messages-ja": Conformant},
		},
		{
			name: "a declared divergence",
			change: func(r *run) {
				r.results["R000002"] = strings.Replace(minIssue, "must be at least 1", "too small", 1)
			},
			decl:     declare(v, divergentMin),
			statuses: map[string]string{"core": PartiallyConformant},
			outcomes: map[string]string{"R000002": Divergent},
		},
		{
			name: "the implementation fails",
			change: func(r *run) {
				r.results["R000002"] = `{"error": "ArithmeticException: BigInteger would overflow supported range"}`
			},
			decl:     declare(v, ""),
			statuses: map[string]string{"core": NonConformant},
			outcomes: map[string]string{"R000002": Failed},
		},
		{
			name: "a failure no divergence excuses",
			change: func(r *run) {
				r.results["R000002"] = `{"error": "threw"}`
			},
			decl:     declare(v, divergentMin),
			statuses: map[string]string{"core": NonConformant},
			outcomes: map[string]string{"R000002": Failed},
		},
		{
			name:     "a stale divergence",
			decl:     declare(v, divergentMin),
			statuses: map[string]string{"core": NonConformant},
			outcomes: map[string]string{"R000002": Failed},
		},
		{
			name: "an outcome that is neither expected nor declared",
			change: func(r *run) {
				r.results["R000002"] = strings.Replace(minIssue, "must be at least 1", "other", 1)
			},
			decl:     declare(v, divergentMin),
			statuses: map[string]string{"core": NonConformant},
			outcomes: map[string]string{"R000002": Failed},
		},
		{
			name: "a declared unsupported feature",
			change: func(r *run) {
				r.bound = without(r.bound, "operation.string.cuid")
				delete(r.results, "R000003")
			},
			decl:     declare(v, `, "unsupported_features": {"operation.string.cuid": {"reason": "no CUID library"}}`),
			statuses: map[string]string{"core": PartiallyConformant, "encode": Conformant},
			outcomes: map[string]string{"R000003": Unsupported},
		},
		{
			name: "a facet unsupported with its parent",
			change: func(r *run) {
				r.bound = without(without(r.bound, "operation.int32.min"), "operation.int32.min.message")
				delete(r.results, "R000002")
				delete(r.results, "R000005")
			},
			decl:     declare(v, `, "unsupported_features": {"operation.int32.min": {"reason": "r"}}`),
			statuses: map[string]string{"core": PartiallyConformant},
			outcomes: map[string]string{"R000002": Unsupported, "R000005": Unsupported},
		},
		{
			name: "a facet unsupported alone",
			change: func(r *run) {
				r.bound = without(r.bound, "operation.int32.min.message")
				delete(r.results, "R000005")
			},
			decl:     declare(v, `, "unsupported_features": {"operation.int32.min.message": {"reason": "no message overload"}}`),
			statuses: map[string]string{"core": PartiallyConformant},
			outcomes: map[string]string{"R000002": Matched, "R000005": Unsupported},
		},
		{
			name: "a facet bound without its parent",
			change: func(r *run) {
				r.bound = without(r.bound, "operation.int32.min")
				delete(r.results, "R000002")
				delete(r.results, "R000005")
			},
			decl:    declare(v, `, "unsupported_features": {"operation.int32.min": {"reason": "r"}}`),
			invalid: "binds operation.int32.min.message, and not operation.int32.min, which it is a facet of",
		},
		{
			name: "an undeclared unbound feature",
			change: func(r *run) {
				r.bound = without(r.bound, "operation.string.cuid")
				delete(r.results, "R000003")
			},
			decl:     declare(v, ""),
			statuses: map[string]string{"core": NonConformant},
			outcomes: map[string]string{"R000003": Failed},
		},
		{
			name:     "a missing result",
			change:   func(r *run) { delete(r.results, "R000001") },
			decl:     declare(v, ""),
			statuses: map[string]string{"core": NonConformant},
			outcomes: map[string]string{"R000001": Failed},
		},
		{
			name: "a mismatched catalogue",
			change: func(r *run) {
				en := map[string]string{}
				for k, t := range r.catalogs["en"] {
					en[k] = t
				}
				en["required"] = "is needed"
				delete(en, "blank")
				r.catalogs["en"] = en
			},
			decl:     declare(v, ""),
			statuses: map[string]string{"messages-en": NonConformant, "messages-ja": Conformant, "core": Conformant},
			outcomes: map[string]string{"required": Failed, "blank": Failed},
		},
		{
			name:    "a bound feature declared unsupported",
			decl:    declare(v, `, "unsupported_features": {"operation.string.cuid": {"reason": "r"}}`),
			invalid: "declared unsupported, and the runner binds it",
		},
		{
			name: "a divergence for a case that cannot run",
			change: func(r *run) {
				r.bound = without(r.bound, "operation.int32.min")
				delete(r.results, "R000002")
			},
			decl:    declare(v, strings.Replace(divergentMin, `}}}`, `}}}, "unsupported_features": {"operation.int32.min": {"reason": "r"}}`, 1)),
			invalid: "which the runner does not bind",
		},
		{
			name:    "a manifest mismatch",
			change:  func(r *run) { r.digest = "sha256:" + strings.Repeat("0", 64) },
			decl:    declare(v, ""),
			invalid: "manifest digest",
		},
		{
			name:    "a known bug declared as a divergence",
			decl:    declare(v, strings.Replace(divergentMin, `"design"`, `"bug"`, 1)),
			invalid: "declaration",
		},
		{
			name:    "a declaration for another version",
			decl:    declare("0.0.1", ""),
			invalid: "the declaration is for specification 0.0.1",
		},
		{
			name:    "a result for an unknown case",
			change:  func(r *run) { r.results["R999999"] = okInt },
			decl:    declare(v, ""),
			invalid: "which is not a case",
		},
		{
			name: "a result for a case whose feature is not bound",
			change: func(r *run) {
				r.bound = without(r.bound, "operation.string.cuid")
			},
			decl:    declare(v, `, "unsupported_features": {"operation.string.cuid": {"reason": "r"}}`),
			invalid: "has a result for R000003",
		},
		{
			name:    "a declaration of another implementation",
			decl:    []byte(strings.Replace(string(declare(v, "")), `"implementation": "raoh-test"`, `"implementation": "raoh-rust"`, 1)),
			invalid: "the declaration is raoh-rust's, and the runner result is raoh-test's",
		},
		{
			name:    "a divergence whose outcome is not an observation",
			decl:    declare(v, `, "divergences": {"R000001": {"category": "design", "reason": "r", "observed": {"ok": "1"}}}`),
			invalid: "is not an observation",
		},
		{
			name:    "a divergence that gives what the case expects",
			decl:    declare(v, strings.Replace(divergentMin, "too small", "must be at least 1", 1)),
			invalid: "gives what the case expects",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := s.run()
			if c.change != nil {
				c.change(r)
			}
			rep, err := verify(t, s, r, c.decl)
			if c.invalid != "" {
				var inv *InvalidError
				if !errors.As(err, &inv) {
					t.Fatalf("got %v, want an invalid input error", err)
				}
				if !strings.Contains(err.Error(), c.invalid) {
					t.Errorf("%v, want it to say %q", err, c.invalid)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for profile, want := range c.statuses {
				if got := rep.Profiles[profile].Status; got != want {
					t.Errorf("%s: %s, want %s (%+v)", profile, got, want, rep.Cases)
				}
			}
			for id, want := range c.outcomes {
				if got := outcome(rep, id); got.Outcome != want {
					t.Errorf("%s: %+v, want %s", id, got, want)
				}
			}
		})
	}
}

func TestReportMatchesItsSchema(t *testing.T) {
	s := miniSpec(t)
	rep, err := verify(t, s, s.run(), declare(s.Version, ""))
	if err != nil {
		t.Fatal(err)
	}
	text, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate("report", text); err != nil {
		t.Error(err)
	}
	if !rep.Conformant() || rep.Profiles["core"].Matched != 4 || rep.Profiles["messages-en"].Matched != len(s.Catalog.Messages["en"]) {
		t.Errorf("%+v", rep.Profiles)
	}
}

func without(list []string, drop string) []string {
	var out []string
	for _, s := range list {
		if s != drop {
			out = append(out, s)
		}
	}
	return out
}

func TestUncoveredFeatures(t *testing.T) {
	got := miniSpec(t).Uncovered()
	for _, f := range []string{"decoder.list", "operation.string.email", "fixture.area"} {
		if !slices.Contains(got, f) {
			t.Errorf("%s is not reported uncovered", f)
		}
	}
	for _, f := range []string{"decoder.int", "operation.int32.min", "fixture.identity"} {
		if slices.Contains(got, f) {
			t.Errorf("%s is reported uncovered", f)
		}
	}
}

// An optional_meta entry needs a case that leaves it out, as a feature needs a case that uses it.
func TestOptionalMetaNeedsACaseThatLeavesItOut(t *testing.T) {
	issues, err := os.ReadFile("../../catalog/issues.json")
	if err != nil {
		t.Fatal(err)
	}
	optional := strings.Replace(string(issues),
		`"meta": {"allowed": "list<T>", "actual": "T"}`,
		`"meta": {"allowed": "list<T>", "actual": "T"}, "optional_meta": ["actual"]`, 1)
	if optional == string(issues) {
		t.Fatal("not_allowed is not where the test expects it")
	}
	withActual := `{"id": "R000001", "title": "t", "decoder": ["string", ["oneOf", ["a"]]], "input": "b",
	  "issues": [{"path": "", "code": "not_allowed", "message_key": "not_allowed", "meta": {"allowed": ["a"], "actual": "b"}}]}`
	withoutActual := `{"id": "R000002", "title": "t", "decoder": ["string", ["oneOf", ["a"]]], "input": "c",
	  "issues": [{"path": "", "code": "not_allowed", "message_key": "not_allowed", "meta": {"allowed": ["a"]}}]}`
	for _, tc := range []struct {
		cases string
		want  []string
	}{
		{"[" + withActual + "]", []string{"not_allowed.actual"}},
		{"[" + withActual + "," + withoutActual + "]", nil},
	} {
		root := artifactstest.Copy(t, "../..", map[string]string{
			"catalog/issues.json":  optional,
			"suite/core/mini.json": tc.cases,
		})
		s, err := Load(root)
		if err != nil {
			t.Fatal(err)
		}
		if got := s.UnpinnedOptional(); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.cases, got, tc.want)
		}
	}
	// The catalogue as it is: every optional entry a form leaves open has a case that leaves it out.
	if got := miniSpec(t).UnpinnedOptional(); len(got) > 0 {
		t.Errorf("the catalogue has optional entries that are always there: %v", got)
	}
}

// Every issue a form that takes a message declares needs a case that gives it the message.
func TestEveryIssueOfAFormThatTakesAMessageIsGivenOne(t *testing.T) {
	got := miniSpec(t).UngivenMessages()
	if slices.Contains(got, "operation.int32.min.message: out_of_range.minimum") {
		t.Error("R000005 gives min's issue a message, and it is reported ungiven")
	}
	for _, want := range []string{"operation.string.toInt.message: type_mismatch.numeric_range", "decoder.literal.message: invalid_format.literal"} {
		if !slices.Contains(got, want) {
			t.Errorf("%s is not reported ungiven", want)
		}
	}
	if got := func() []string {
		s, err := Load("../..")
		if err != nil {
			t.Fatal(err)
		}
		return s.UngivenMessages()
	}(); len(got) > 0 {
		t.Errorf("the suite leaves issues without a given message: %v", got)
	}
}

// A candidate's path is read from the root of the input. A case with the oneOf at the root cannot
// tell that from a path read from the oneOf, so one_of_failed needs a case with the oneOf below it.
func TestCandidatePathsNeedACaseBelowTheRoot(t *testing.T) {
	root := artifactstest.Copy(t, "../..", map[string]string{
		"suite/core/one_of.json": `[{"id": "R000271", "title": "t", "decoder": ["oneOf", [["int", ["map", "decimal_string"]], ["string", ["minLength", 3]]]], "input": "ab",
		  "issues": [{"path": "", "code": "one_of_failed", "message_key": "one_of_failed", "meta": {"candidates": [
		    {"candidate": 0, "issues": [{"code": "type_mismatch", "message": "expected integer", "meta": {"actual": "string", "expected": "integer"}, "path": ""}]},
		    {"candidate": 1, "issues": [{"code": "too_short", "message": "must be at least 3 characters", "meta": {"actual": 2, "min": 3}, "path": ""}]}]}}]}]`,
	})
	s, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.UnpinnedCandidatePaths(); !slices.Equal(got, []string{"one_of_failed"}) {
		t.Errorf("a suite with the oneOf at the root only: %v, want one_of_failed", got)
	}
	s, err = Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.UnpinnedCandidatePaths(); len(got) > 0 {
		t.Errorf("the suite leaves candidate paths unpinned: %v", got)
	}
}

// A constructor that declares required as an issue of its own gives it for null and for an absent
// input, and each of the two needs a case: a case for one does not show the other.
func TestRequiredNeedsACaseForNullAndForAbsent(t *testing.T) {
	required := `{"path": "", "code": "required", "message_key": "required", "meta": {}}`
	null := `{"id": "R000001", "title": "t", "decoder": ["bool"], "input": null, "issues": [` + required + `]}`
	absent := `{"id": "R000002", "title": "t", "decoder": ["object", [["field", "a", ["bool"]]]], "input": {},
	  "issues": [{"path": "/a", "code": "required", "message_key": "required", "meta": {}}]}`
	for _, tc := range []struct {
		cases      string
		has, hasNo string
	}{
		{"[" + null + "]", "decoder.bool: absent", "decoder.bool: null"},
		{"[" + absent + "]", "decoder.bool: null", "decoder.bool: absent"},
		{"[" + null + "," + absent + "]", "", "decoder.bool: null"},
	} {
		root := artifactstest.Copy(t, "../..", map[string]string{"suite/core/mini.json": tc.cases})
		s, err := Load(root)
		if err != nil {
			t.Fatal(err)
		}
		got := s.UnpinnedRequired()
		if tc.has != "" && !slices.Contains(got, tc.has) {
			t.Errorf("%s: %v, want %s among them", tc.cases, got, tc.has)
		}
		if slices.Contains(got, tc.hasNo) {
			t.Errorf("%s: %v, want no %s", tc.cases, got, tc.hasNo)
		}
		if tc.has == "" && slices.Contains(got, "decoder.bool: absent") {
			t.Errorf("%s: %v, want no decoder.bool: absent", tc.cases, got)
		}
	}
	s, err := Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.UnpinnedRequired(); len(got) > 0 {
		t.Errorf("the suite leaves required unpinned: %v", got)
	}
}
