package verify

import (
	"encoding/json"
	"errors"
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
   "issues": [{"path": "", "code": "invalid_format", "message_key": "invalid_format.cuid", "meta": {}}]}
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
	defaultJSON = `{"ok": {"value": "default"}}`
)

var allFeatures = []string{"decoder.int", "decoder.string", "operation.int32.min", "operation.string.cuid",
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
	if !rep.Conformant() || rep.Profiles["core"].Matched != 3 || rep.Profiles["messages-en"].Matched != len(s.Catalog.Messages["en"]) {
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
