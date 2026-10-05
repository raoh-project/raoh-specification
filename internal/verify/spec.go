// Package verify classifies what a runner observed against the suite and the implementation's
// declaration, and writes the report. See spec/conformance.md.
package verify

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/raoh-project/raoh-specification/internal/artifacts"
	"github.com/raoh-project/raoh-specification/internal/catalog"
	"github.com/raoh-project/raoh-specification/internal/dsl"
	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/manifest"
	"github.com/raoh-project/raoh-specification/internal/schemas"
	"github.com/raoh-project/raoh-specification/internal/suite"
)

// Profiles are every profile, in the order reports list them.
var Profiles = []string{"core", "encode", "messages-en", "messages-ja"}

// Spec is a revision of the specification, loaded and checked.
type Spec struct {
	Root     string
	Version  string
	Digest   string
	Schemas  *schemas.Set
	Catalog  *catalog.Catalog
	Checker  *dsl.Checker
	Suite    *suite.Suite
	Features map[string]bool
	// FacetOf maps each facet to the feature it is a facet of.
	FacetOf map[string]string
}

// InvalidError reports input that makes a comparison untrustworthy. raoh-verify exits with status
// 2 for it.
type InvalidError struct {
	Problems []string
}

func (e *InvalidError) Error() string {
	return "invalid input:\n  " + strings.Join(e.Problems, "\n  ")
}

func invalid(format string, args ...any) *InvalidError {
	return &InvalidError{Problems: []string{fmt.Sprintf(format, args...)}}
}

// Load reads and checks the specification under root: what raoh-verify check-suite does. Every
// artifact goes through its schema before it is parsed.
func Load(root string) (*Spec, error) {
	if _, err := artifacts.List(root); err != nil {
		return nil, err
	}
	s := &Spec{Root: root, Features: map[string]bool{}}
	var err error
	if s.Schemas, err = schemas.Load(root); err != nil {
		return nil, err
	}
	text, err := os.ReadFile(filepath.Join(root, "specification.json"))
	if err != nil {
		return nil, err
	}
	n, err := jsontext.Parse(text)
	if err != nil {
		return nil, fmt.Errorf("specification.json: %w", err)
	}
	if s.Version, err = n.String("version"); err != nil || s.Version == "" {
		return nil, fmt.Errorf("specification.json: version must be a non-empty string")
	}
	if s.Catalog, err = catalog.Load(root, s.Schemas); err != nil {
		return nil, err
	}
	reg, err := dsl.Load(root, s.Schemas)
	if err != nil {
		return nil, err
	}
	if s.Checker, err = dsl.NewChecker(reg, s.Catalog); err != nil {
		return nil, fmt.Errorf("catalog/operations.json: %w", err)
	}
	for _, f := range reg.Features() {
		s.Features[f] = true
	}
	s.FacetOf = map[string]string{}
	for _, f := range reg.Facets() {
		s.FacetOf[f.ID] = f.Parent
	}
	if s.Suite, err = suite.Load(root, s.Checker, s.Schemas); err != nil {
		return nil, err
	}
	if s.Digest, err = manifest.Digest(root); err != nil {
		return nil, err
	}
	return s, nil
}

// Validate checks a document against one of the specification's schemas.
func (s *Spec) Validate(schema string, text []byte) error {
	return s.Schemas.Validate(schema, text)
}

// UnpinnedOptional lists the optional metadata entries no case leaves out, as issue.entry. An
// entry in optional_meta says that an issue may or may not have it where a form leaves it open; a
// case that leaves it out is the evidence that it may, as a case that needs a feature is the
// evidence for a feature. Without one, the entry is always there, and optional_meta only lets a
// case drop it unnoticed.
func (s *Spec) UnpinnedOptional() []string {
	open := map[string]bool{}
	for _, f := range s.Checker.Registry().Forms() {
		for _, ref := range f.Issues {
			v, ok := s.Catalog.Variants[ref.Key]
			if !ok {
				continue
			}
			for _, o := range v.Optional {
				if !ref.Gives(o) && !slices.Contains(ref.Omit, o) {
					open[ref.Key+"."+o] = true
				}
			}
		}
	}
	left := map[string]bool{}
	var walk func(issues []suite.TypedIssue)
	walk = func(issues []suite.TypedIssue) {
		for _, is := range issues {
			for _, o := range is.Slot.Optional {
				if _, ok := is.Meta[o]; !ok {
					left[is.Slot.Key+"."+o] = true
				}
			}
			for _, i := range slices.Sorted(maps.Keys(is.Candidates)) {
				walk(is.Candidates[i])
			}
		}
	}
	for _, c := range s.Suite.Cases {
		walk(c.Issues)
	}
	var out []string
	for _, entry := range slices.Sorted(maps.Keys(open)) {
		if !left[entry] {
			out = append(out, entry)
		}
	}
	return out
}

// UnpinnedCandidatePaths lists the issues that list candidates, such as one_of_failed, for which no
// case has the issue below the root and a candidate's issue below it too. A candidate's path is
// read from the root of the input, not from where the issue is, and the two readings give the same
// path wherever the issue is at the root: a case there cannot tell them apart.
func (s *Spec) UnpinnedCandidatePaths() []string {
	open := map[string]bool{}
	for _, f := range s.Checker.Registry().Forms() {
		if c, ok := candidatesOf(f.Flow); ok {
			open[f.Issues[c.Issue.Index()].Key] = true
		}
	}
	pinned := map[string]bool{}
	var walk func(issues []suite.TypedIssue)
	walk = func(issues []suite.TypedIssue) {
		for _, is := range issues {
			for _, i := range slices.Sorted(maps.Keys(is.Candidates)) {
				for _, c := range is.Candidates[i] {
					if len(is.Slot.Path) > 0 && len(c.Slot.Path) > 0 {
						pinned[is.Slot.Key] = true
					}
				}
				walk(is.Candidates[i])
			}
		}
	}
	for _, c := range s.Suite.Cases {
		walk(c.Issues)
	}
	var out []string
	for _, key := range slices.Sorted(maps.Keys(open)) {
		if !pinned[key] {
			out = append(out, key)
		}
	}
	return out
}

// candidatesOf finds the issue that lists candidates in a flow, if it has one.
func candidatesOf(e dsl.Expr) (dsl.ExprCandidates, bool) {
	switch e := e.(type) {
	case dsl.ExprCandidates:
		return e, true
	case dsl.ExprCat:
		return firstCandidates(e.Items)
	case dsl.ExprChain:
		return firstCandidates(e.Items)
	case dsl.ExprEach:
		return candidatesOf(e.Body)
	case dsl.ExprAt:
		return candidatesOf(e.Body)
	case dsl.ExprUnknown:
		return candidatesOf(e.After)
	}
	return dsl.ExprCandidates{}, false
}

func firstCandidates(items []dsl.Expr) (dsl.ExprCandidates, bool) {
	for _, it := range items {
		if c, ok := candidatesOf(it); ok {
			return c, true
		}
	}
	return dsl.ExprCandidates{}, false
}

// UngivenMessages lists, as facet: issue, every issue a form that takes a message declares that no
// case expects with the message given. A given message is the message of every issue its form
// declares, so each needs a case: one per facet would leave toInt's type_mismatch.numeric_range
// unchecked.
func (s *Spec) UngivenMessages() []string {
	want := map[string]bool{}
	for _, facet := range s.Checker.Registry().Facets() {
		for _, ref := range facet.Form.Issues {
			want[facet.ID+": "+ref.Key] = true
		}
	}
	given := map[string]bool{}
	var walk func(issues []suite.TypedIssue)
	walk = func(issues []suite.TypedIssue) {
		for _, is := range issues {
			if is.Slot.MessageArg {
				given[dsl.MessageFacetID(is.Slot.Form)+": "+is.Slot.Key] = true
			}
			for _, i := range slices.Sorted(maps.Keys(is.Candidates)) {
				walk(is.Candidates[i])
			}
		}
	}
	for _, c := range s.Suite.Cases {
		walk(c.Issues)
	}
	var out []string
	for _, w := range slices.Sorted(maps.Keys(want)) {
		if !given[w] {
			out = append(out, w)
		}
	}
	return out
}

// Uncovered lists the features no case needs. A feature is in the registry only if a case pins
// it: a case is the least a specified feature has, though one case does not specify it all.
func (s *Spec) Uncovered() []string {
	used := map[string]bool{}
	for _, c := range s.Suite.Cases {
		for _, f := range c.Features() {
			used[f] = true
		}
	}
	var out []string
	for _, f := range s.Checker.Registry().Features() {
		if !used[f] {
			out = append(out, f)
		}
	}
	return out
}
