package verify

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/raoh-project/raoh-specification/internal/compare"
	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/suite"
)

// The outcomes of a case.
const (
	Matched     = "matched"
	Divergent   = "divergent"
	Unsupported = "unsupported"
	Failed      = "failed"
)

// The statuses of a profile.
const (
	Conformant          = "conformant"
	PartiallyConformant = "partially_conformant"
	NonConformant       = "non_conformant"
)

// Specification identifies the revision a result was produced from.
type Specification struct {
	Version        string `json:"version"`
	Revision       string `json:"revision"`
	ManifestDigest string `json:"manifest_digest"`
}

// Implementation identifies the implementation.
type Implementation struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Revision string `json:"revision"`
}

// ProfileResult is a profile's status and counts.
type ProfileResult struct {
	Status      string `json:"status"`
	Matched     int    `json:"matched"`
	Divergent   int    `json:"divergent"`
	Unsupported int    `json:"unsupported"`
	Failed      int    `json:"failed"`
}

// CaseResult is a case's outcome.
type CaseResult struct {
	ID      string `json:"id"`
	Profile string `json:"profile"`
	Outcome string `json:"outcome"`
	Detail  string `json:"detail,omitempty"`
}

// Report is a conformance report.
type Report struct {
	Format         string                    `json:"format"`
	Specification  Specification             `json:"specification"`
	Implementation Implementation            `json:"implementation"`
	Environment    map[string]string         `json:"environment"`
	Verifier       string                    `json:"verifier,omitempty"`
	Profiles       map[string]*ProfileResult `json:"profiles"`
	Cases          []CaseResult              `json:"cases"`
}

// Conformant reports whether no profile is non-conformant.
func (r *Report) Conformant() bool {
	for _, p := range r.Profiles {
		if p.Status == NonConformant {
			return false
		}
	}
	return true
}

type divergence struct {
	category, reason string
	observed         suite.Outcome
}

type runnerResult struct {
	spec     Specification
	impl     Implementation
	env      map[string]string
	bound    map[string]bool
	results  map[string]suite.Outcome
	catalogs map[string]map[string]string
}

type declaration struct {
	specification string
	profiles      []string
	unsupported   map[string]bool
	divergences   map[string]divergence
}

// Verify classifies a runner's result against the specification and the declaration. It returns
// an *InvalidError when the input cannot be trusted.
func Verify(s *Spec, resultText, declarationText []byte, verifier string) (*Report, error) {
	var problems []string
	if err := s.Validate("runner-result", resultText); err != nil {
		problems = append(problems, fmt.Sprintf("runner result: %v", err))
	}
	if err := s.Validate("conformance", declarationText); err != nil {
		problems = append(problems, fmt.Sprintf("declaration: %v", err))
	}
	if len(problems) > 0 {
		return nil, &InvalidError{Problems: problems}
	}
	res, err := parseResult(resultText)
	if err != nil {
		return nil, invalid("runner result: %v", err)
	}
	decl, err := parseDeclaration(declarationText)
	if err != nil {
		return nil, invalid("declaration: %v", err)
	}
	if problems := s.consistency(res, decl); len(problems) > 0 {
		return nil, &InvalidError{Problems: problems}
	}
	report := &Report{
		Format:         "raoh-conformance-report/v1",
		Specification:  res.spec,
		Implementation: res.impl,
		Environment:    res.env,
		Verifier:       verifier,
		Profiles:       map[string]*ProfileResult{},
	}
	for _, profile := range Profiles {
		if !slices.Contains(decl.profiles, profile) {
			continue
		}
		var results []CaseResult
		if locale, ok := strings.CutPrefix(profile, "messages-"); ok {
			results = classifyCatalog(profile, s.Catalog.Messages[locale], res.catalogs[locale])
		} else {
			for _, c := range s.Suite.Cases {
				if c.Profile == profile {
					results = append(results, classify(c, res, decl))
				}
			}
		}
		p := &ProfileResult{}
		for _, r := range results {
			switch r.Outcome {
			case Matched:
				p.Matched++
			case Divergent:
				p.Divergent++
			case Unsupported:
				p.Unsupported++
			case Failed:
				p.Failed++
			}
		}
		switch {
		case p.Failed > 0:
			p.Status = NonConformant
		case p.Divergent > 0 || p.Unsupported > 0:
			p.Status = PartiallyConformant
		default:
			p.Status = Conformant
		}
		report.Profiles[profile] = p
		report.Cases = append(report.Cases, results...)
	}
	return report, nil
}

// consistency lists what makes a result and a declaration untrustworthy together.
func (s *Spec) consistency(res *runnerResult, decl *declaration) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if res.spec.Version != s.Version {
		add("the runner read cases of specification %s, and the suite is %s", res.spec.Version, s.Version)
	}
	if res.spec.ManifestDigest != s.Digest {
		add("the runner read a suite whose manifest digest is %s, and this suite's is %s", res.spec.ManifestDigest, s.Digest)
	}
	if decl.specification != s.Version {
		add("the declaration is for specification %s, and the suite is %s", decl.specification, s.Version)
	}
	for _, f := range sortedKeys(res.bound) {
		if !s.Features[f] {
			add("the runner binds %s, which is not a feature", f)
		}
		if decl.unsupported[f] {
			add("%s is declared unsupported, and the runner binds it", f)
		}
	}
	for _, f := range sortedKeys(decl.unsupported) {
		if !s.Features[f] {
			add("%s is declared unsupported, and is not a feature", f)
		}
	}
	for _, id := range sortedKeys(res.results) {
		c, ok := s.Suite.ByID[id]
		if !ok {
			add("the runner has a result for %s, which is not a case", id)
			continue
		}
		if missing := unbound(c, res); len(missing) > 0 {
			add("the runner has a result for %s, which needs %s, which it does not bind", id, strings.Join(missing, ", "))
		}
	}
	for _, id := range sortedKeys(decl.divergences) {
		c, ok := s.Suite.ByID[id]
		if !ok {
			add("a divergence is declared for %s, which is not a case", id)
			continue
		}
		if !slices.Contains(decl.profiles, c.Profile) {
			add("a divergence is declared for %s, whose profile %s the declaration does not list", id, c.Profile)
		}
		if missing := unbound(c, res); len(missing) > 0 {
			add("a divergence is declared for %s, which needs %s, which the runner does not bind", id, strings.Join(missing, ", "))
		}
		if ok, _ := compare.Expected(c, decl.divergences[id].observed); ok {
			add("the divergence declared for %s gives what the case expects", id)
		}
	}
	return problems
}

func unbound(c *suite.Case, res *runnerResult) []string {
	var missing []string
	for _, f := range c.Features() {
		if !res.bound[f] {
			missing = append(missing, f)
		}
	}
	return missing
}

func classify(c *suite.Case, res *runnerResult, decl *declaration) CaseResult {
	r := CaseResult{ID: c.ID, Profile: c.Profile}
	if missing := unbound(c, res); len(missing) > 0 {
		var undeclared []string
		for _, f := range missing {
			if !decl.unsupported[f] {
				undeclared = append(undeclared, f)
			}
		}
		if len(undeclared) == 0 {
			r.Outcome, r.Detail = Unsupported, "needs "+strings.Join(missing, ", ")
		} else {
			r.Outcome, r.Detail = Failed, "needs "+strings.Join(undeclared, ", ")+", which the runner does not bind and the declaration does not declare unsupported"
		}
		return r
	}
	observed, ok := res.results[c.ID]
	if !ok {
		r.Outcome, r.Detail = Failed, "the runner has no result for the case"
		return r
	}
	div, declared := decl.divergences[c.ID]
	if ok, why := compare.Expected(c, observed); ok {
		if declared {
			r.Outcome, r.Detail = Failed, "the case is declared divergent, and the implementation gives what the case expects: the divergence is stale"
		} else {
			r.Outcome = Matched
		}
		return r
	} else if !declared {
		r.Outcome, r.Detail = Failed, why
		return r
	}
	if ok, why := compare.Same(c, div.observed, observed); ok {
		r.Outcome, r.Detail = Divergent, div.category+": "+div.reason
	} else {
		r.Outcome, r.Detail = Failed, "the outcome is neither the expected one nor the declared one: "+why
	}
	return r
}

func classifyCatalog(profile string, want, got map[string]string) []CaseResult {
	diffs := compare.Catalog(want, got)
	var results []CaseResult
	for _, key := range sortedKeys(diffs) {
		r := CaseResult{ID: key, Profile: profile, Outcome: Matched}
		if d := diffs[key]; d != "" {
			r.Outcome, r.Detail = Failed, d
		}
		results = append(results, r)
	}
	return results
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func text(n *jsontext.Node, name string) string {
	if v, ok := n.Get(name); ok && v.Kind == jsontext.String {
		return v.Text
	}
	return ""
}

func parseResult(t []byte) (*runnerResult, error) {
	n, err := jsontext.Parse(t)
	if err != nil {
		return nil, err
	}
	r := &runnerResult{env: map[string]string{}, bound: map[string]bool{}, results: map[string]suite.Outcome{}, catalogs: map[string]map[string]string{}}
	spec, _ := n.Get("specification")
	r.spec = Specification{Version: text(spec, "version"), Revision: text(spec, "revision"), ManifestDigest: text(spec, "manifest_digest")}
	impl, _ := n.Get("implementation")
	r.impl = Implementation{Name: text(impl, "name"), Version: text(impl, "version"), Revision: text(impl, "revision")}
	if env, ok := n.Get("environment"); ok {
		for _, m := range env.Members {
			r.env[m.Name] = m.Value.Text
		}
	}
	bound, _ := n.Get("bound_features")
	for _, e := range bound.Elems {
		r.bound[e.Text] = true
	}
	results, _ := n.Get("results")
	for _, m := range results.Members {
		obs, _ := m.Value.Get("observed")
		o, err := suite.ParseOutcome(obs)
		if err != nil {
			return nil, fmt.Errorf("result for %s: %w", m.Name, err)
		}
		r.results[m.Name] = o
	}
	if cats, ok := n.Get("catalogs"); ok {
		for _, m := range cats.Members {
			entries := map[string]string{}
			for _, e := range m.Value.Members {
				entries[e.Name] = e.Value.Text
			}
			r.catalogs[m.Name] = entries
		}
	}
	return r, nil
}

func parseDeclaration(t []byte) (*declaration, error) {
	n, err := jsontext.Parse(t)
	if err != nil {
		return nil, err
	}
	d := &declaration{specification: text(n, "specification"), unsupported: map[string]bool{}, divergences: map[string]divergence{}}
	profiles, _ := n.Get("profiles")
	for _, e := range profiles.Elems {
		d.profiles = append(d.profiles, e.Text)
	}
	if u, ok := n.Get("unsupported_features"); ok {
		for _, m := range u.Members {
			d.unsupported[m.Name] = true
		}
	}
	if divs, ok := n.Get("divergences"); ok {
		for _, m := range divs.Members {
			obs, _ := m.Value.Get("observed")
			o, err := suite.ParseOutcome(obs)
			if err != nil {
				return nil, fmt.Errorf("divergence for %s: %w", m.Name, err)
			}
			d.divergences[m.Name] = divergence{category: text(m.Value, "category"), reason: text(m.Value, "reason"), observed: o}
		}
	}
	return d, nil
}
