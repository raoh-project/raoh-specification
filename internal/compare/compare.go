// Package compare decides whether an outcome a runner observed is the one a case expects, or the
// one a declaration says the implementation gives instead.
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
			return false, fmt.Sprintf("expected ok %s, observed issues %s", c.OK.Raw, describeIssues(observed.Issues))
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
		return false, fmt.Sprintf("expected issues %s, observed ok %s", describeExpected(c.Issues), observed.OK.Raw)
	}
	return matchIssues(len(c.Issues), len(observed.Issues), c.Checked.InputOrder,
		func(i, j int) string { return matchExpected(c.Issues[i], observed.Issues[j]) },
		func() string {
			return fmt.Sprintf("expected %s, observed %s", describeExpected(c.Issues), describeIssues(observed.Issues))
		})
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
// divergence declares.
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
	return matchIssues(len(declared.Issues), len(observed.Issues), c.Checked.InputOrder,
		func(i, j int) string { return matchObserved(declared.Issues[i], observed.Issues[j]) },
		func() string {
			return fmt.Sprintf("declared %s, observed %s", describeIssues(declared.Issues), describeIssues(observed.Issues))
		})
}

// matchIssues compares two lists of issues in order, or as multisets when unordered.
func matchIssues(n, m int, unordered bool, match func(i, j int) string, describe func() string) (bool, string) {
	if n != m {
		return false, fmt.Sprintf("%d issue(s) where %d were expected: %s", m, n, describe())
	}
	if !unordered {
		for i := range n {
			if why := match(i, i); why != "" {
				return false, fmt.Sprintf("issue %d: %s", i, why)
			}
		}
		return true, ""
	}
	used := make([]bool, m)
next:
	for i := range n {
		for j := range m {
			if !used[j] && match(i, j) == "" {
				used[j] = true
				continue next
			}
		}
		return false, fmt.Sprintf("issue %d has no match in any order: %s", i, describe())
	}
	return true, ""
}

func samePath(a, b string) bool {
	x, errx := suite.SplitPath(a)
	y, erry := suite.SplitPath(b)
	return errx == nil && erry == nil && slices.Equal(x, y)
}

func matchExpected(e suite.ExpectedIssue, o suite.Issue) string {
	if !samePath(e.Path, o.Path) {
		return fmt.Sprintf("path %q, observed %q", e.Path, o.Path)
	}
	if e.Code != o.Code || e.Key != o.Key {
		return fmt.Sprintf("%s (%s), observed %s (%s)", e.Key, e.Code, o.Key, o.Code)
	}
	if o.Message == nil {
		return "the runner wrote no message"
	}
	if *o.Message != e.Message {
		return fmt.Sprintf("message %q, observed %q", e.Message, *o.Message)
	}
	names := o.Meta.Names()
	if len(names) != len(e.Meta) {
		return fmt.Sprintf("metadata %s, observed %s", metaNames(e.Meta), strings.Join(sorted(names), ","))
	}
	for _, m := range o.Meta.Members {
		want, ok := e.Meta[m.Name]
		if !ok {
			return fmt.Sprintf("metadata %s, observed %s", metaNames(e.Meta), strings.Join(sorted(names), ","))
		}
		got, err := value.Observe(want.Type, m.Value)
		if err != nil {
			return fmt.Sprintf("meta %s: %v", m.Name, err)
		}
		if !value.Equal(want, got) {
			return fmt.Sprintf("meta %s: observed %s", m.Name, m.Value.Raw)
		}
	}
	return ""
}

func matchObserved(d, o suite.Issue) string {
	if !samePath(d.Path, o.Path) {
		return fmt.Sprintf("path %q, observed %q", d.Path, o.Path)
	}
	if d.Code != o.Code || d.Key != o.Key {
		return fmt.Sprintf("%s (%s), observed %s (%s)", d.Key, d.Code, o.Key, o.Code)
	}
	if d.Message == nil || o.Message == nil || *d.Message != *o.Message {
		return "the messages differ"
	}
	if !value.EqualJSON(d.Meta, o.Meta) {
		return fmt.Sprintf("meta %s, observed %s", d.Meta.Raw, o.Meta.Raw)
	}
	return ""
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	sort.Strings(s)
	return s
}

func metaNames(m map[string]value.Value) string {
	var names []string
	for k := range m {
		names = append(names, k)
	}
	return strings.Join(sorted(names), ",")
}

func describeExpected(issues []suite.ExpectedIssue) string {
	var parts []string
	for _, is := range issues {
		parts = append(parts, fmt.Sprintf("%s at %q", is.Key, is.Path))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func describeIssues(issues []suite.Issue) string {
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
