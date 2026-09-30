// Package suite reads the conformance cases and checks each against the catalogues and the
// decoder language.
package suite

import (
	"fmt"
	"slices"
	"strings"

	"github.com/raoh-project/raoh-specification/internal/jsontext"
)

// Issue is an issue as a case or a runner writes it: with a message key, which is never empty.
type Issue struct {
	Path string
	Code string
	Key  string
	// Message is nil when a case leaves the message to be derived; an empty message is a message.
	Message *string
	Meta    *jsontext.Node
}

// NestedIssue is an issue a candidate of a oneOf reported, as one_of_failed lists it: with a
// message, and no message key.
type NestedIssue struct {
	Path    string
	Code    string
	Message string
	Meta    *jsontext.Node
}

// surface is what is written of an issue of either kind; key is nil for a nested issue, which has
// none, and message nil for a case's issue whose message is derived.
type surface struct {
	path, code string
	key        *string
	message    *string
	meta       *jsontext.Node
}

func (is Issue) surface() surface {
	key := is.Key
	return surface{path: is.Path, code: is.Code, key: &key, message: is.Message, meta: is.Meta}
}

func (is NestedIssue) surface() surface {
	message := is.Message
	return surface{path: is.Path, code: is.Code, message: &message, meta: is.Meta}
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

// ParseNestedIssues reads the issues a candidate of a oneOf reported, as its one_of_failed lists
// them: each with path, code, message and meta, and no message key.
func ParseNestedIssues(n *jsontext.Node) ([]NestedIssue, error) {
	if n.Kind != jsontext.Array || len(n.Elems) == 0 {
		return nil, fmt.Errorf("a candidate's issues must be a non-empty array")
	}
	var out []NestedIssue
	for i, e := range n.Elems {
		if e.Kind != jsontext.Object {
			return nil, fmt.Errorf("issue %d: expected an object", i)
		}
		for _, name := range e.Names() {
			if !slices.Contains([]string{"path", "code", "message", "meta"}, name) {
				return nil, fmt.Errorf("issue %d: unknown member %q", i, name)
			}
		}
		var is NestedIssue
		var err error
		if is.Path, err = e.String("path"); err != nil {
			return nil, err
		}
		if _, err := SplitPath(is.Path); err != nil {
			return nil, fmt.Errorf("issue %d: %w", i, err)
		}
		if is.Code, err = e.String("code"); err != nil || is.Code == "" {
			return nil, fmt.Errorf("issue %d: code must be a non-empty string", i)
		}
		if is.Message, err = e.String("message"); err != nil {
			return nil, err
		}
		if is.Meta, err = e.Member("meta"); err != nil || is.Meta.Kind != jsontext.Object {
			return nil, fmt.Errorf("issue %d: meta must be an object", i)
		}
		out = append(out, is)
	}
	return out, nil
}

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
	if is.Code, err = text("code"); err != nil || is.Code == "" {
		return is, fmt.Errorf("code must be a non-empty string")
	}
	if is.Key, err = text("message_key"); err != nil || is.Key == "" {
		return is, fmt.Errorf("message_key must be a non-empty string")
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
