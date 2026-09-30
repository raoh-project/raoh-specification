// Package compare decides whether an outcome a runner observed is the one a case expects, or the
// one a declaration says the implementation gives instead. An implementation's issues are read
// against the decoder's flow exactly as the case's are, and the typed readings are compared.
package compare

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/suite"
	"github.com/raoh-project/raoh-specification/internal/value"
)

// Expected reports whether an observed outcome is the one the case expects, and if not, how it
// differs.
func Expected(c *suite.Case, observed suite.Outcome) (bool, string) {
	if c.Encoder {
		if observed.Failed() {
			return false, "the encoder failed"
		}
		if !value.EqualJSON(c.OK, observed.OK) {
			return false, fmt.Sprintf("expected %s, observed %s", c.OK.Raw, observed.OK.Raw)
		}
		return true, ""
	}
	if c.OK != nil {
		if observed.Failed() {
			return false, fmt.Sprintf("expected ok %s, observed issues %s", c.OK.Raw, describe(observed.Issues))
		}
		v, err := value.Observe(c.Checked.Result, observed.OK)
		if err != nil {
			return false, fmt.Sprintf("the observation is not a %s: %v", c.Checked.Result, err)
		}
		if !value.Equal(c.OKValue, v) {
			return false, fmt.Sprintf("expected ok %s, observed ok %s", c.OK.Raw, observed.OK.Raw)
		}
		return true, ""
	}
	if !observed.Failed() {
		return false, fmt.Sprintf("expected issues, observed ok %s", observed.OK.Raw)
	}
	readings, err := suite.ReadIssues(observed.Issues, c.Checked.Flow, c.Input, nil, c.Catalog, suite.ObservedIssues)
	if err != nil {
		return false, fmt.Sprintf("the implementation's issues %s: %v", describe(observed.Issues), err)
	}
	var why string
	for _, r := range readings {
		if why = Lists(c.Issues, r, againstPlace); why == "" {
			return true, ""
		}
	}
	return false, why
}

// Observation checks that an outcome is one a runner could write for the case: an ok that is an
// observation of the decoder's result type, or issues. It does not say whether it is right.
func Observation(c *suite.Case, o suite.Outcome) error {
	if c.Encoder || o.Failed() {
		return nil
	}
	if _, err := value.Observe(c.Checked.Result, o.OK); err != nil {
		return fmt.Errorf("its outcome is not an observation of %s: %w", c.Checked.Result, err)
	}
	return nil
}

// Same reports whether two observed outcomes of a case are the same: a runner's and the one a
// divergence declares. Issues that fit the decoder's flow are read and compared as the case's are;
// a divergence's issues need not fit it, and those are compared as written, in order.
func Same(c *suite.Case, declared, observed suite.Outcome) (bool, string) {
	if declared.Failed() != observed.Failed() {
		return false, "one outcome succeeded and the other failed"
	}
	if !declared.Failed() {
		if c.Encoder {
			if !value.EqualJSON(declared.OK, observed.OK) {
				return false, fmt.Sprintf("declared %s, observed %s", declared.OK.Raw, observed.OK.Raw)
			}
			return true, ""
		}
		a, err := value.Observe(c.Checked.Result, declared.OK)
		if err != nil {
			return false, fmt.Sprintf("the declared outcome is not a %s: %v", c.Checked.Result, err)
		}
		b, err := value.Observe(c.Checked.Result, observed.OK)
		if err != nil {
			return false, fmt.Sprintf("the observation is not a %s: %v", c.Checked.Result, err)
		}
		if !value.Equal(a, b) {
			return false, fmt.Sprintf("declared ok %s, observed ok %s", declared.OK.Raw, observed.OK.Raw)
		}
		return true, ""
	}
	d, derr := suite.ReadIssues(declared.Issues, c.Checked.Flow, c.Input, nil, c.Catalog, suite.ObservedIssues)
	o, oerr := suite.ReadIssues(observed.Issues, c.Checked.Flow, c.Input, nil, c.Catalog, suite.ObservedIssues)
	if derr == nil && oerr == nil {
		var why string
		for _, dr := range d {
			for _, or := range o {
				if why = Lists(dr, or, asWritten); why == "" {
					return true, ""
				}
			}
		}
		return false, why
	}
	if len(declared.Issues) != len(observed.Issues) {
		return false, fmt.Sprintf("declared %s, observed %s", describe(declared.Issues), describe(observed.Issues))
	}
	for i := range declared.Issues {
		if why := asWrittenRaw(declared.Issues[i], observed.Issues[i]); why != "" {
			return false, fmt.Sprintf("issue %d: %s", i, why)
		}
	}
	return true, ""
}

// Messages says what an observed issue's message is compared with.
type Messages int

const (
	// againstPlace compares it with the message its place gives.
	againstPlace Messages = iota
	// asWritten compares it with the message the other issue wrote.
	asWritten
)

// Lists compares two readings of issue lists: in order, except that consecutive issues of one
// unordered group in the first are compared with the same number of issues of the second as a
// multiset.
func Lists(want, got []suite.TypedIssue, m Messages) string {
	if len(want) != len(got) {
		return fmt.Sprintf("%d issue(s) where %d were expected: expected %s, observed %s", len(got), len(want), describeTyped(want), describeTyped(got))
	}
	for i := 0; i < len(want); {
		k := i + 1
		if want[i].Group != "" {
			for k < len(want) && want[k].Group == want[i].Group {
				k++
			}
		}
		used := make([]bool, k-i)
	next:
		for a := i; a < k; a++ {
			for b := i; b < k; b++ {
				if !used[b-i] && sameIssue(want[a], got[b], m) == "" {
					used[b-i] = true
					continue next
				}
			}
			if k-i == 1 {
				return fmt.Sprintf("issue %d: %s", i, sameIssue(want[i], got[i], m))
			}
			return fmt.Sprintf("issue %d has no match among issues %d to %d, which may come in any order: expected %s, observed %s", a, i, k-1, describeTyped(want), describeTyped(got))
		}
		i = k
	}
	return ""
}

// sameIssue compares two typed issues: place, key, message, metadata by type, and candidates.
func sameIssue(e, o suite.TypedIssue, m Messages) string {
	if !slices.Equal(e.Slot.Path, o.Slot.Path) || e.Slot.Site != o.Slot.Site || e.Group != o.Group {
		return fmt.Sprintf("%s at %q, observed %s at %q", e.Slot.Key, e.Path, o.Slot.Key, o.Path)
	}
	if e.Key != o.Key {
		return fmt.Sprintf("message key %q, observed %q", e.Key, o.Key)
	}
	want := e.Message
	if m == asWritten && e.Issue.Message != nil {
		want = *e.Issue.Message
	}
	if o.Issue.Message == nil || *o.Issue.Message != want {
		got := "none"
		if o.Issue.Message != nil {
			got = fmt.Sprintf("%q", *o.Issue.Message)
		}
		return fmt.Sprintf("message %q, observed %s", want, got)
	}
	if len(e.Meta) != len(o.Meta) || e.CandidatesMeta != o.CandidatesMeta {
		return fmt.Sprintf("metadata %s, observed %s", metaNames(e), metaNames(o))
	}
	for name, v := range e.Meta {
		w, ok := o.Meta[name]
		if !ok {
			return fmt.Sprintf("metadata %s, observed %s", metaNames(e), metaNames(o))
		}
		if !value.Equal(v, w) {
			return fmt.Sprintf("meta %s: observed %s", name, rawMeta(o, name))
		}
	}
	if len(e.Candidates) != len(o.Candidates) {
		return fmt.Sprintf("meta %s: %d candidates, observed %d", e.CandidatesMeta, len(e.Candidates), len(o.Candidates))
	}
	for k, list := range e.Candidates {
		other, ok := o.Candidates[k]
		if !ok {
			return fmt.Sprintf("meta %s: candidate %d is missing", e.CandidatesMeta, k)
		}
		if why := Lists(list, other, m); why != "" {
			return fmt.Sprintf("meta %s candidate %d: %s", e.CandidatesMeta, k, why)
		}
	}
	return ""
}

func rawMeta(o suite.TypedIssue, name string) string {
	if n, ok := o.Issue.Meta.Get(name); ok {
		return string(n.Raw)
	}
	return "nothing"
}

func metaNames(t suite.TypedIssue) string {
	var names []string
	for k := range t.Meta {
		names = append(names, k)
	}
	if t.CandidatesMeta != "" {
		names = append(names, t.CandidatesMeta)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// asWrittenRaw compares two issues as written, for a divergence whose issues do not fit the flow.
func asWrittenRaw(d, o suite.Issue) string {
	if d.Path != o.Path || d.Code != o.Code || d.Key != o.Key {
		return fmt.Sprintf("%s (%s) at %q, observed %s (%s) at %q", d.Key, d.Code, d.Path, o.Key, o.Code, o.Path)
	}
	if d.Message == nil || o.Message == nil || *d.Message != *o.Message {
		return "the messages differ"
	}
	if !value.EqualJSON(d.Meta, o.Meta) {
		return fmt.Sprintf("meta %s, observed %s", d.Meta.Raw, o.Meta.Raw)
	}
	return ""
}

func describeTyped(issues []suite.TypedIssue) string {
	var parts []string
	for _, is := range issues {
		parts = append(parts, fmt.Sprintf("%s at %q", is.Slot.Key, is.Path))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func describe(issues []suite.Issue) string {
	var parts []string
	for _, is := range issues {
		parts = append(parts, fmt.Sprintf("%s at %q", is.Key, is.Path))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// Catalog compares the templates an implementation ships for a locale with the catalogue's, and
// gives, for each message key, whether it matched and how it differs.
func Catalog(want, got map[string]string) map[string]string {
	out := map[string]string{}
	for key, w := range want {
		g, ok := got[key]
		switch {
		case !ok:
			out[key] = "missing"
		case g != w:
			out[key] = fmt.Sprintf("expected %q, observed %q", w, g)
		default:
			out[key] = ""
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			out[key] = "not in the catalogue"
		}
	}
	return out
}

// Parse reads an outcome a runner or a declaration wrote.
func Parse(n *jsontext.Node) (suite.Outcome, error) { return suite.ParseOutcome(n) }
