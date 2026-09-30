// Package suite reads the conformance cases and checks each against the catalogues and the
// decoder language.
package suite

import (
	"fmt"
	"slices"
	"strings"

	"github.com/raoh-project/raoh-specification/internal/jsontext"
)

// Issue is an issue as a case or a runner writes it.
type Issue struct {
	Path string
	Code string
	Key  string
	// Message is nil when a case leaves the message to be derived.
	Message *string
	Meta    *jsontext.Node
}

// Outcome is what a decoder or encoder gave: a value, or issues.
type Outcome struct {
	OK     *jsontext.Node
	Issues []Issue
}

// Failed reports whether the outcome is a failure.
func (o Outcome) Failed() bool { return o.OK == nil }

// ParseOutcome reads {"ok": value} or {"issues": [...]}.
func ParseOutcome(n *jsontext.Node) (Outcome, error) {
	if n.Kind != jsontext.Object || len(n.Members) != 1 {
		return Outcome{}, fmt.Errorf(`an outcome is {"ok": value} or {"issues": [...]}`)
	}
	m := n.Members[0]
	switch m.Name {
	case "ok":
		return Outcome{OK: m.Value}, nil
	case "issues":
		issues, err := ParseIssues(m.Value)
		return Outcome{Issues: issues}, err
	}
	return Outcome{}, fmt.Errorf(`an outcome is {"ok": value} or {"issues": [...]}`)
}

// ParseIssues reads a non-empty array of issues.
func ParseIssues(n *jsontext.Node) ([]Issue, error) {
	if n.Kind != jsontext.Array || len(n.Elems) == 0 {
		return nil, fmt.Errorf("issues must be a non-empty array")
	}
	var out []Issue
	for i, e := range n.Elems {
		is, err := parseIssue(e)
		if err != nil {
			return nil, fmt.Errorf("issue %d: %w", i, err)
		}
		out = append(out, is)
	}
	return out, nil
}

var issueMembers = []string{"path", "code", "message_key", "message", "meta"}

func parseIssue(n *jsontext.Node) (Issue, error) {
	var is Issue
	if n.Kind != jsontext.Object {
		return is, fmt.Errorf("expected an object")
	}
	for _, name := range n.Names() {
		if !slices.Contains(issueMembers, name) {
			return is, fmt.Errorf("unknown member %q", name)
		}
	}
	text := func(name string) (string, error) {
		v, ok := n.Get(name)
		if !ok || v.Kind != jsontext.String {
			return "", fmt.Errorf("%s must be a string", name)
		}
		return v.Text, nil
	}
	var err error
	if is.Path, err = text("path"); err != nil {
		return is, err
	}
	if _, err := SplitPath(is.Path); err != nil {
		return is, err
	}
	if is.Code, err = text("code"); err != nil {
		return is, err
	}
	if is.Key, err = text("message_key"); err != nil {
		return is, err
	}
	if _, ok := n.Get("message"); ok {
		m, err := text("message")
		if err != nil {
			return is, err
		}
		is.Message = &m
	}
	meta, ok := n.Get("meta")
	if !ok || meta.Kind != jsontext.Object {
		return is, fmt.Errorf("meta must be an object")
	}
	is.Meta = meta
	return is, nil
}

// SplitPath reads a JSON Pointer into its segments.
func SplitPath(p string) ([]string, error) {
	if p == "" {
		return nil, nil
	}
	if !strings.HasPrefix(p, "/") {
		return nil, fmt.Errorf("path %q is not a JSON Pointer", p)
	}
	var segs []string
	for _, raw := range strings.Split(p[1:], "/") {
		for i := 0; i < len(raw); i++ {
			if raw[i] == '~' && (i+1 == len(raw) || (raw[i+1] != '0' && raw[i+1] != '1')) {
				return nil, fmt.Errorf("path %q has a ~ that is not ~0 or ~1", p)
			}
		}
		segs = append(segs, strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~"))
	}
	return segs, nil
}

// JoinPath writes segments as a JSON Pointer.
func JoinPath(segs []string) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteByte('/')
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1"))
	}
	return b.String()
}
