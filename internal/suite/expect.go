package suite

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/raoh-project/raoh-specification/internal/catalog"
	"github.com/raoh-project/raoh-specification/internal/dsl"
	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/value"
)

// Mode says whose issues are read: a case's, which write a message only where the form gives it,
// or an implementation's, which always write the message they have.
type Mode int

// The modes of reading issues.
const (
	CaseIssues Mode = iota + 1
	ObservedIssues
)

// TypedIssue is an issue read against the decoder's flow: its place, its typed metadata and the
// message its place gives.
type TypedIssue struct {
	Issue
	// Slot is where in the decoder's flow, for the input, the issue arises.
	Slot dsl.Slot
	// Meta holds the metadata values, typed.
	Meta map[string]value.Value
	// Message is the message the issue's place gives: given by the form, or derived from the
	// catalogue. An observed issue's own message is Issue.Message.
	Message string
	// Group identifies the unordered group instance the issue belongs to; empty when it is
	// ordered.
	Group string
	// Candidates are, for an issue that lists what candidates gave, the issues each candidate
	// gave, by candidate index, and CandidatesMeta the metadata entry that lists them.
	Candidates     map[int][]TypedIssue
	CandidatesMeta string
}

// ReadIssues reads a list of issues as the flow gives them for an input at a path. It fails when
// the list is not one the flow gives, or, for a case, when it fits the flow in two ways that type,
// word or group an issue differently. An implementation's issues that fit in two ways give both
// readings, for the comparison to try.
func ReadIssues(issues []Issue, flow dsl.Flow, input *jsontext.Node, path []string, cat *catalog.Catalog, mode Mode) ([][]TypedIssue, error) {
	r := &reader{issues: issues, cat: cat, mode: mode, cache: map[string]fitResult{}}
	parses := dsl.ParseIssues(flow, input, path, len(issues), r.fit)
	if len(parses) == 0 {
		return nil, r.explain(dsl.Places(flow, input, path))
	}
	if mode == CaseIssues && len(parses) > 1 {
		return nil, fmt.Errorf("the issues fit the decoder's flow in two ways that type, word or group them differently")
	}
	var out [][]TypedIssue
	for _, p := range parses {
		typed := make([]TypedIssue, len(p))
		for i, slot := range p {
			typed[i] = r.cache[r.key(i, slot)].issue
		}
		out = append(out, typed)
	}
	return out, nil
}

// Succeeds reports whether the flow can give no issue for the input: whether the decoder can
// succeed at all.
func Succeeds(flow dsl.Flow, input *jsontext.Node) bool {
	return len(dsl.ParseIssues(flow, input, nil, 0, func(int, dsl.Slot) (string, bool) { return "", false })) > 0
}

type fitResult struct {
	issue TypedIssue
	sig   string
	err   error
}

type reader struct {
	issues []Issue
	cat    *catalog.Catalog
	mode   Mode
	cache  map[string]fitResult
}

func (r *reader) key(i int, slot dsl.Slot) string {
	return fmt.Sprintf("%d|%p|%s|%s", i, slot.Site, dsl.JoinPath(slot.Path), slot.Group)
}

// explain says why no parse was found: the first issue that fits no place the flow has for the
// input, with why it fits none of those with its path, key and code; or, when each fits some
// place, that the decoder does not give them together or in this order.
func (r *reader) explain(places []dsl.Slot) error {
	for i, is := range r.issues {
		var reasons []string
		fitted := false
		for _, slot := range places {
			if !fits(is, slot) {
				continue
			}
			if _, err := instance(is, slot, r.cat, r.mode); err != nil {
				if !slices.Contains(reasons, err.Error()) {
					reasons = append(reasons, err.Error())
				}
				continue
			}
			fitted = true
			break
		}
		switch {
		case fitted:
		case len(reasons) > 0:
			return fmt.Errorf("issue %d: %s", i, strings.Join(reasons, "; "))
		default:
			return fmt.Errorf("issue %d: the decoder gives no %s at %q for this input", i, describeKey(is), is.Path)
		}
	}
	return fmt.Errorf("each issue fits a place in the decoder's flow, and the decoder does not give them together or in this order: %s", describe(r.issues))
}

func describeKey(is Issue) string {
	if is.Key == "" {
		return is.Code
	}
	return is.Key
}

func describe(issues []Issue) string {
	var parts []string
	for _, is := range issues {
		parts = append(parts, fmt.Sprintf("%s at %q", describeKey(is), is.Path))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// fit reads issue i at a slot, once per slot.
func (r *reader) fit(i int, slot dsl.Slot) (string, bool) {
	k := r.key(i, slot)
	res, ok := r.cache[k]
	if !ok {
		is := r.issues[i]
		if fits(is, slot) {
			res.issue, res.err = instance(is, slot, r.cat, r.mode)
			if res.err == nil {
				res.sig = res.issue.Group + "\x00" + res.issue.Message + "\x00" + typesOf(res.issue.Meta)
			}
		} else {
			res.err = fmt.Errorf("not here")
		}
		r.cache[k] = res
	}
	return res.sig, res.err == nil
}

// fits reports whether an issue's path, key and code are the slot's.
func fits(is Issue, slot dsl.Slot) bool {
	path, err := SplitPath(is.Path)
	if err != nil || !slices.Equal(path, slot.Path) || is.Code != slot.Code {
		return false
	}
	return is.Key == "" || is.Key == slot.Key
}

func typesOf(m map[string]value.Value) string {
	var parts []string
	for k, v := range m {
		parts = append(parts, k+":"+v.Type.String())
	}
	sort.Strings(parts)
	return "{" + strings.Join(parts, ",") + "}"
}

// instance reads an issue at a slot: its metadata typed and checked against what the form decides,
// its message settled.
func instance(is Issue, slot dsl.Slot, cat *catalog.Catalog, mode Mode) (TypedIssue, error) {
	e := TypedIssue{Issue: is, Slot: slot, Meta: map[string]value.Value{}, Group: slot.Group}
	for _, m := range is.Meta.Members {
		t, ok := slot.Meta[m.Name]
		if !ok {
			return e, fmt.Errorf("%s has no metadata %s", slot.Key, m.Name)
		}
		if slot.Candidates != nil && t.Kind == value.List && t.Args[0].Kind == value.Record {
			e.CandidatesMeta = m.Name
			if err := e.candidates(m.Value, slot, cat, mode); err != nil {
				return e, fmt.Errorf("%s meta %s: %w", slot.Key, m.Name, err)
			}
			continue
		}
		v, err := value.Observe(t, m.Value)
		if err != nil {
			return e, fmt.Errorf("%s meta %s: %w", slot.Key, m.Name, err)
		}
		e.Meta[m.Name] = v
	}
	for name := range slot.Meta {
		_, typed := e.Meta[name]
		if !typed && name != e.CandidatesMeta && !slices.Contains(slot.Optional, name) {
			return e, fmt.Errorf("%s needs metadata %s", slot.Key, name)
		}
	}
	for name, want := range slot.Values {
		got, ok := e.Meta[name]
		if !ok || !value.Equal(want, got) {
			return e, fmt.Errorf("the form gives %s the %s %s", slot.Key, name, describeValue(want))
		}
	}
	for _, name := range slot.MemberMeta {
		got, ok := e.Meta[name]
		if !ok || len(slot.Path) == 0 || got.Str != slot.Path[len(slot.Path)-1] {
			return e, fmt.Errorf("%s at %q has the member's name as %s", slot.Key, is.Path, name)
		}
	}
	want := slot.Message
	if want == "" {
		var err error
		if want, err = Derive(slot.Key, slot.Code, e.Meta, cat); err != nil {
			return e, err
		}
	}
	e.Message = want
	switch {
	case mode == ObservedIssues:
		if is.Message == nil {
			return e, fmt.Errorf("an implementation writes the message of every issue")
		}
	case is.Key == "":
		// An issue a candidate reported, as raoh-java writes it, carries its message; a case may
		// write it, and then it has to be the one the place gives.
		if is.Message != nil && *is.Message != want {
			return e, fmt.Errorf("%s at %q has the message %q, not %q", slot.Key, is.Path, want, *is.Message)
		}
	case slot.Message != "":
		if is.Message == nil || *is.Message != want {
			return e, fmt.Errorf("%s is given the message %q, which the case has to write", slot.Key, want)
		}
	case is.Message != nil:
		return e, fmt.Errorf("the message of %s is derived from the catalogue; leave it out", slot.Key)
	}
	return e, nil
}

// candidates reads the candidates an issue lists: every candidate exactly once, each with the
// issues it gave, read by the flow of that candidate for the same input.
func (e *TypedIssue) candidates(n *jsontext.Node, slot dsl.Slot, cat *catalog.Catalog, mode Mode) error {
	if n.Kind != jsontext.Array {
		return fmt.Errorf("expected an array of candidates")
	}
	e.Candidates = map[int][]TypedIssue{}
	for _, c := range n.Elems {
		if c.Kind != jsontext.Object || len(c.Members) != 2 {
			return fmt.Errorf("a candidate has candidate and issues, and nothing else")
		}
		idx, err := c.Member("candidate")
		if err != nil {
			return err
		}
		i, err := value.Observe(value.Of(value.Int32), idx)
		if err != nil || i.Int.Sign() < 0 || i.Int.Int64() >= int64(len(slot.Candidates)) {
			return fmt.Errorf("candidate %s is not an index of the %d candidates", idx.Raw, len(slot.Candidates))
		}
		k := int(i.Int.Int64())
		if _, dup := e.Candidates[k]; dup {
			return fmt.Errorf("candidate %d appears twice", k)
		}
		list, err := c.Member("issues")
		if err != nil {
			return err
		}
		nested, err := ParseNestedIssues(list)
		if err != nil {
			return err
		}
		readings, err := ReadIssues(nested, slot.Candidates[k], slot.Input, slot.Path, cat, mode)
		if err != nil {
			return fmt.Errorf("candidate %d: %w", k, err)
		}
		e.Candidates[k] = readings[0]
	}
	for k := range slot.Candidates {
		if _, ok := e.Candidates[k]; !ok {
			return fmt.Errorf("candidate %d is missing: every candidate failed, and each reports its issues", k)
		}
	}
	return nil
}

// describeValue writes a value for a message, as its message form when it has one.
func describeValue(v value.Value) string {
	if s, err := value.FormatForMessage(v); err == nil {
		return s
	}
	return v.Type.String()
}
