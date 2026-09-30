// Package compare decides whether an outcome a runner observed is the one a case expects, or the
// one a declaration says the implementation gives instead. An implementation's issues are matched
// against the reading of the case's (or the divergence's) issues by the same reader, never read on
// their own.
package compare

import (
	"bytes"
	"encoding/json"
	"fmt"
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
			return false, fmt.Sprintf("expected %s, observed %s", compact(c.OK), compact(observed.OK))
		}
		return true, ""
	}
	if c.OK != nil {
		if observed.Failed() {
			return false, fmt.Sprintf("expected ok %s, observed issues %s", compact(c.OK), describe(observed.Issues))
		}
		v, err := value.Observe(c.Checked.Result, observed.OK)
		if err != nil {
			return false, fmt.Sprintf("the observation is not a %s: %v", c.Checked.Result, err)
		}
		if !value.Equal(c.OKValue, v) {
			return false, fmt.Sprintf("expected ok %s, observed ok %s", compact(c.OK), compact(observed.OK))
		}
		return true, ""
	}
	if !observed.Failed() {
		return false, fmt.Sprintf("expected issues, observed ok %s", compact(observed.OK))
	}
	if why := suite.Match(c.Issues, observed.Issues, c.Catalog); why != "" {
		return false, why
	}
	return true, ""
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
// divergence declares. A divergence's issues that fit the decoder's flow are read as a case's are,
// with their written messages, and the runner's are matched against that reading; a divergence's
// issues need not fit the flow, and those are compared as written, in order.
func Same(c *suite.Case, declared, observed suite.Outcome) (bool, string) {
	if declared.Failed() != observed.Failed() {
		return false, "one outcome succeeded and the other failed"
	}
	if !declared.Failed() {
		if c.Encoder {
			if !value.EqualJSON(declared.OK, observed.OK) {
				return false, fmt.Sprintf("declared %s, observed %s", compact(declared.OK), compact(observed.OK))
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
			return false, fmt.Sprintf("declared ok %s, observed ok %s", compact(declared.OK), compact(observed.OK))
		}
		return true, ""
	}
	if reading, err := suite.ReadIssues(declared.Issues, c.Checked.Flow, c.Input, nil, c.Catalog, suite.DeclaredIssues); err == nil {
		if why := suite.Match(reading, observed.Issues, c.Catalog); why != "" {
			return false, why
		}
		return true, ""
	}
	if len(declared.Issues) != len(observed.Issues) {
		return false, fmt.Sprintf("declared %s, observed %s", describe(declared.Issues), describe(observed.Issues))
	}
	for i := range declared.Issues {
		if why := asWritten(declared.Issues[i], observed.Issues[i]); why != "" {
			return false, fmt.Sprintf("issue %d: %s", i, why)
		}
	}
	return true, ""
}

// asWritten compares two issues as written, for a divergence whose issues do not fit the flow.
func asWritten(d, o suite.Issue) string {
	if d.Path != o.Path || d.Code != o.Code || d.Key != o.Key {
		return fmt.Sprintf("%s (%s) at %q, observed %s (%s) at %q", d.Key, d.Code, d.Path, o.Key, o.Code, o.Path)
	}
	if d.Message == nil || o.Message == nil || *d.Message != *o.Message {
		return "the messages differ"
	}
	if !value.EqualJSON(d.Meta, o.Meta) {
		return fmt.Sprintf("meta %s, observed %s", compact(d.Meta), compact(o.Meta))
	}
	return ""
}

// compact writes a JSON value on one line, whatever the whitespace it was written with, so that a
// reason reads the same whichever file the value came from.
func compact(n *jsontext.Node) string {
	var b bytes.Buffer
	if err := json.Compact(&b, n.Raw); err != nil {
		return string(n.Raw)
	}
	return b.String()
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
