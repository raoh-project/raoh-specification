package suite

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/raoh-project/raoh-specification/internal/catalog"
	"github.com/raoh-project/raoh-specification/internal/dsl"
	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/value"
)

// Mode says whose issues are read, which decides only how their messages are treated.
type Mode int

// The modes of reading issues.
const (
	// CaseIssues are a case's: a derived message is left out, a given one written.
	CaseIssues Mode = iota + 1
	// DeclaredIssues are a divergence's: every message is written, and is what the
	// implementation is declared to give.
	DeclaredIssues
	// ObservedIssues are an implementation's: every message is written, and is compared with the
	// message of the reading it is matched against.
	ObservedIssues
)

// TypedIssue is an issue read against the decoder's flow: its place, its typed metadata and the
// message it is expected to have.
type TypedIssue struct {
	// Path, Code and Key are as written; Key is nil for an issue a candidate reported.
	Path string
	Code string
	Key  *string
	// Slot is where in the decoder's flow, for the input, the issue arises.
	Slot dsl.Slot
	// Meta holds the metadata values, typed.
	Meta map[string]value.Value
	// Message is the message the issue is expected to have: the one its place gives, or, for a
	// divergence, the one written.
	Message string
	// Group is the unordered group instance the issue belongs to; nil when it is in order.
	Group *dsl.Group
	// Candidates holds, for an issue that lists candidates, by candidate index, the issues each
	// gave, read from the metadata entry its slot names; nil otherwise.
	Candidates map[int][]TypedIssue
}

// ReadIssues reads a case's or a divergence's issues as the flow gives them for an input at a
// path: each takes its place in the flow, where it is typed and its metadata and message settled.
// It fails when the list is not one the flow gives, or when it fits the flow in two ways that
// type, word or group an issue differently.
func ReadIssues(issues []Issue, flow dsl.Flow, input *jsontext.Node, path []string, cat *catalog.Catalog, mode Mode) ([]TypedIssue, error) {
	surfaces := make([]surface, len(issues))
	for i, is := range issues {
		surfaces[i] = is.surface()
	}
	return read(surfaces, flow, input, path, cat, mode)
}

func read(issues []surface, flow dsl.Flow, input *jsontext.Node, path []string, cat *catalog.Catalog, mode Mode) ([]TypedIssue, error) {
	r := &reader{issues: issues, cat: cat, mode: mode, cache: map[string]fitResult{}}
	parses := dsl.ParseIssues(flow, input, path, len(issues), r.fit)
	switch len(parses) {
	case 0:
		return nil, r.explain(dsl.Places(flow, input, path))
	case 1:
	default:
		return nil, fmt.Errorf("the issues fit the decoder's flow in two ways that type, word or group them differently")
	}
	typed := make([]TypedIssue, len(parses[0]))
	for i, slot := range parses[0] {
		typed[i] = r.cache[r.key(i, slot)].issue
	}
	return typed, nil
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
	issues []surface
	cat    *catalog.Catalog
	mode   Mode
	cache  map[string]fitResult
}

func (r *reader) key(i int, slot dsl.Slot) string {
	return fmt.Sprintf("%d|%p|%s|%p", i, slot.Site, dsl.JoinPath(slot.Path), slot.Group)
}

// fit reads issue i at a slot, once per slot.
func (r *reader) fit(i int, slot dsl.Slot) (string, bool) {
	k := r.key(i, slot)
	res, ok := r.cache[k]
	if !ok {
		is := r.issues[i]
		if fits(is, slot) {
			res.issue, res.err = instance(is, slot, r.cat, r.mode, nil)
			if res.err == nil {
				res.sig = fmt.Sprintf("%p", res.issue.Group) + "\x00" + res.issue.Message + "\x00" + typesOf(res.issue.Meta)
			}
		} else {
			res.err = fmt.Errorf("not here")
		}
		r.cache[k] = res
	}
	return res.sig, res.err == nil
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
			if _, err := instance(is, slot, r.cat, r.mode, nil); err != nil {
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
			return fmt.Errorf("issue %d: the decoder gives no %s at %q for this input", i, is.describe(), is.path)
		}
	}
	return fmt.Errorf("each issue fits a place in the decoder's flow, and the decoder does not give them together or in this order: %s", describeSurfaces(r.issues))
}

func (is surface) describe() string {
	if is.key == nil {
		return is.code
	}
	return *is.key
}

func describeSurfaces(issues []surface) string {
	var parts []string
	for _, is := range issues {
		parts = append(parts, fmt.Sprintf("%s at %q", is.describe(), is.path))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// fits reports whether an issue's path, key and code are the slot's.
func fits(is surface, slot dsl.Slot) bool {
	path, err := SplitPath(is.path)
	if err != nil || !slices.Equal(path, slot.Path) || is.code != slot.Code {
		return false
	}
	return is.key == nil || *is.key == slot.Key
}

func typesOf(m map[string]value.Value) string {
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(m)) {
		v := m[k]
		parts = append(parts, k+":"+v.Type.String())
	}
	sort.Strings(parts)
	return "{" + strings.Join(parts, ",") + "}"
}

// instance reads an issue at a slot: its metadata typed and checked against what the form decides,
// its message settled. When against is given, the issue is an implementation's, read as the
// counterpart of that expected issue: the candidates it lists are matched against the expected
// ones.
func instance(is surface, slot dsl.Slot, cat *catalog.Catalog, mode Mode, against *TypedIssue) (TypedIssue, error) {
	e := TypedIssue{Path: is.path, Code: is.code, Key: is.key, Slot: slot, Meta: map[string]value.Value{}, Group: slot.Group}
	for _, m := range is.meta.Members {
		t, ok := slot.Meta[m.Name]
		if !ok {
			return e, fmt.Errorf("%s has no metadata %s", slot.Key, m.Name)
		}
		if slot.Candidates != nil && m.Name == slot.Candidates.Meta {
			if err := e.candidates(m.Value, slot, cat, mode, against); err != nil {
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
	for _, name := range slices.Sorted(maps.Keys(slot.Meta)) {
		_, typed := e.Meta[name]
		listed := e.Candidates != nil && name == slot.Candidates.Meta
		if !typed && !listed && !slices.Contains(slot.Optional, name) {
			return e, fmt.Errorf("%s needs metadata %s", slot.Key, name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(slot.Values)) {
		want := slot.Values[name]
		got, ok := e.Meta[name]
		if !ok || !value.Equal(want, got) {
			return e, fmt.Errorf("the form gives %s the %s %s", slot.Key, name, describeValue(want))
		}
	}
	for _, name := range slot.MemberMeta {
		got, ok := e.Meta[name]
		if !ok || len(slot.Path) == 0 || got.Str != slot.Path[len(slot.Path)-1] {
			return e, fmt.Errorf("%s at %q has the member's name as %s", slot.Key, is.path, name)
		}
	}
	var want string
	if slot.Message != nil {
		want = *slot.Message
	} else {
		var err error
		if want, err = Derive(slot.Key, slot.Code, e.Meta, cat); err != nil {
			return e, err
		}
	}
	e.Message = want
	switch {
	case mode != CaseIssues:
		if is.message == nil {
			return e, fmt.Errorf("an implementation writes the message of every issue")
		}
		if mode == DeclaredIssues {
			e.Message = *is.message
		}
	case is.key == nil:
		// An issue a candidate reported carries its message; it has to be the one its place gives.
		if *is.message != want {
			return e, fmt.Errorf("%s at %q has the message %q, not %q", slot.Key, is.path, want, *is.message)
		}
	case slot.Message != nil:
		if is.message == nil || *is.message != want {
			return e, fmt.Errorf("%s is given the message %q, which the case has to write", slot.Key, want)
		}
	case is.message != nil:
		return e, fmt.Errorf("the message of %s is derived from the catalogue; leave it out", slot.Key)
	}
	return e, nil
}

// candidates reads the candidates an issue lists: every candidate exactly once, each with the
// non-empty list of issues it gave, read by the flow of that candidate for the same input, or,
// for an implementation's issue, matched against the expected candidate's issues.
func (e *TypedIssue) candidates(n *jsontext.Node, slot dsl.Slot, cat *catalog.Catalog, mode Mode, against *TypedIssue) error {
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
		if err != nil || i.Int.Sign() < 0 || i.Int.Int64() >= int64(len(slot.Candidates.Flows)) {
			return fmt.Errorf("candidate %s is not an index of the %d candidates", idx.Raw, len(slot.Candidates.Flows))
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
		surfaces := make([]surface, len(nested))
		for j, is := range nested {
			surfaces[j] = is.surface()
		}
		if against == nil {
			typed, err := read(surfaces, slot.Candidates.Flows[k], slot.Input, slot.Path, cat, mode)
			if err != nil {
				return fmt.Errorf("candidate %d: %w", k, err)
			}
			e.Candidates[k] = typed
			continue
		}
		expected, ok := against.Candidates[k]
		if !ok {
			return fmt.Errorf("candidate %d is not expected", k)
		}
		typed, why := match(expected, surfaces, cat)
		if why != "" {
			return fmt.Errorf("candidate %d: %s", k, why)
		}
		e.Candidates[k] = typed
	}
	for k := range slot.Candidates.Flows {
		if _, ok := e.Candidates[k]; !ok {
			return fmt.Errorf("candidate %d is missing: every candidate failed, and each reports its issues", k)
		}
	}
	return nil
}

// Match matches an implementation's issues against the reading of a case's or a divergence's:
// in order, except that consecutive expected issues of one unordered group are matched with the
// same number of observed issues in any order. Each observed issue is read at the place of the
// expected issue it is matched with, by the same reading as the case's, and must have the same
// metadata and the expected message. It gives why they do not match, or "".
func Match(expected []TypedIssue, observed []Issue, cat *catalog.Catalog) string {
	surfaces := make([]surface, len(observed))
	for i, is := range observed {
		surfaces[i] = is.surface()
	}
	_, why := match(expected, surfaces, cat)
	return why
}

func match(expected []TypedIssue, observed []surface, cat *catalog.Catalog) ([]TypedIssue, string) {
	if len(expected) != len(observed) {
		return nil, fmt.Sprintf("%d issue(s) where %d were expected: expected %s, observed %s", len(observed), len(expected), describeTyped(expected), describeSurfaces(observed))
	}
	out := make([]TypedIssue, len(observed))
	for i := 0; i < len(expected); {
		k := i + 1
		if expected[i].Group != nil {
			for k < len(expected) && expected[k].Group == expected[i].Group {
				k++
			}
		}
		used := make([]bool, k-i)
	next:
		for a := i; a < k; a++ {
			var why string
			for b := i; b < k; b++ {
				if used[b-i] {
					continue
				}
				var t TypedIssue
				if t, why = matchOne(expected[a], observed[b], cat); why == "" {
					used[b-i] = true
					out[b] = t
					continue next
				}
			}
			if k-i == 1 {
				return nil, fmt.Sprintf("issue %d: %s", i, why)
			}
			return nil, fmt.Sprintf("issue %d has no match among issues %d to %d, which may come in any order: expected %s, observed %s", a, i, k-1, describeTyped(expected), describeSurfaces(observed))
		}
		i = k
	}
	return out, ""
}

// matchOne reads an observed issue at the place of an expected one and compares them.
func matchOne(e TypedIssue, o surface, cat *catalog.Catalog) (TypedIssue, string) {
	if !fits(o, e.Slot) || (o.key == nil) != (e.Key == nil) {
		return TypedIssue{}, fmt.Sprintf("%s at %q, observed %s at %q", e.Slot.Key, e.Path, o.describe(), o.path)
	}
	t, err := instance(o, e.Slot, cat, ObservedIssues, &e)
	if err != nil {
		return TypedIssue{}, err.Error()
	}
	if *o.message != e.Message {
		return TypedIssue{}, fmt.Sprintf("message %q, observed %q", e.Message, *o.message)
	}
	if len(t.Meta) != len(e.Meta) {
		return TypedIssue{}, fmt.Sprintf("metadata %s, observed %s", metaNames(e), metaNames(t))
	}
	for _, name := range slices.Sorted(maps.Keys(e.Meta)) {
		v := e.Meta[name]
		w, ok := t.Meta[name]
		if !ok {
			return TypedIssue{}, fmt.Sprintf("metadata %s, observed %s", metaNames(e), metaNames(t))
		}
		if !value.Equal(v, w) {
			observed := "a different value"
			if n, ok := o.meta.Get(name); ok {
				observed = string(n.Raw)
			}
			return TypedIssue{}, fmt.Sprintf("meta %s: observed %s", name, observed)
		}
	}
	return t, ""
}

func metaNames(t TypedIssue) string {
	var names []string
	for _, k := range slices.Sorted(maps.Keys(t.Meta)) {
		names = append(names, k)
	}
	if t.Candidates != nil {
		names = append(names, t.Slot.Candidates.Meta)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func describeTyped(issues []TypedIssue) string {
	var parts []string
	for _, is := range issues {
		parts = append(parts, fmt.Sprintf("%s at %q", is.Slot.Key, is.Path))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// describeValue writes a value for a message, as its message form when it has one.
func describeValue(v value.Value) string {
	if s, err := value.FormatForMessage(v); err == nil {
		return s
	}
	return v.Type.String()
}
