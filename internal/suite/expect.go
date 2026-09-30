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

// ExpectedIssue is an issue a case expects, with what the verifier needs to compare it.
type ExpectedIssue struct {
	Issue
	// Slot is where in the decoder's flow, for the case's input, the issue arises.
	Slot dsl.Slot
	// Meta holds the metadata values, typed.
	Meta map[string]value.Value
	// Message is the message the issue has: given by the form, or derived from the catalogue.
	Message string
	// Group identifies the unordered group instance the issue belongs to; empty when it is
	// ordered.
	Group string
	// Candidates are, for one_of_failed, the issues each candidate gave, by candidate index, and
	// CandidatesMeta the metadata entry that lists them.
	Candidates     map[int][]ExpectedIssue
	CandidatesMeta string
}

// searchLimit bounds the assignments Expect tries, so that a pathological case fails instead of
// running for ever.
const searchLimit = 1 << 20

// Expect reads a list of issues as the decoder's flow produces them for the case's input: each
// issue is assigned a slot, in the order of the slots, where only the issues of one unordered
// group instance may come in any order among themselves. Each issue is typed, its metadata checked
// against what the form decides, and its message settled, at its slot. The list is rejected if no
// assignment fits, or if two assignments fit that type an issue differently.
func Expect(issues []Issue, slots []dsl.Slot, cat *catalog.Catalog) ([]ExpectedIssue, error) {
	options := make([][]option, len(issues))
	for i, is := range issues {
		var tried []string
		for j, slot := range slots {
			if !fits(is, slot) {
				continue
			}
			e, err := instance(is, slot, cat)
			if err != nil {
				tried = append(tried, err.Error())
				continue
			}
			options[i] = append(options[i], option{slot: j, issue: e})
		}
		if len(options[i]) == 0 {
			if len(tried) == 0 {
				return nil, fmt.Errorf("issue %d: the decoder gives no %s at %q for this input", i, describeKey(is), is.Path)
			}
			return nil, fmt.Errorf("issue %d: %s", i, strings.Join(tried, "; "))
		}
	}
	pos := positions(slots)
	var found []ExpectedIssue
	var foundSig string
	visits := 0
	used := make([]bool, len(slots))
	chosen := make([]ExpectedIssue, len(issues))
	var ambiguous error
	var search func(k, last int, group string) bool
	search = func(k, last int, group string) bool {
		if visits++; visits > searchLimit {
			ambiguous = fmt.Errorf("the issues cannot be matched with the decoder's flow in reasonable time")
			return true
		}
		if k == len(issues) {
			sig := signature(chosen)
			if found == nil {
				found, foundSig = slices.Clone(chosen), sig
				return false
			}
			if sig != foundSig {
				ambiguous = fmt.Errorf("the issues fit the decoder's flow in two ways that type or order them differently")
				return true
			}
			return false
		}
		for _, o := range options[k] {
			slot := slots[o.slot]
			p := pos[o.slot]
			sameGroup := slot.Group != "" && slot.Group == group
			if used[o.slot] || (sameGroup && p != last) || (!sameGroup && p <= last) {
				continue
			}
			used[o.slot] = true
			chosen[k] = o.issue
			stop := search(k+1, p, slot.Group)
			used[o.slot] = false
			if stop {
				return true
			}
		}
		return false
	}
	search(0, -1, "")
	if ambiguous != nil {
		return nil, ambiguous
	}
	if found == nil {
		return nil, fmt.Errorf("the issues do not come in an order the decoder's flow gives them in")
	}
	return found, nil
}

type option struct {
	slot  int
	issue ExpectedIssue
}

// positions gives each slot its place in the order: its own index, or for a slot in an unordered
// group the index of the group's first slot, since the group's issues come in any order.
func positions(slots []dsl.Slot) []int {
	pos := make([]int, len(slots))
	first := map[string]int{}
	for i, s := range slots {
		pos[i] = i
		if s.Group == "" {
			continue
		}
		if f, ok := first[s.Group]; ok {
			pos[i] = f
		} else {
			first[s.Group] = i
		}
	}
	return pos
}

// signature is what the comparison of an assignment depends on: for each issue its group, message
// and metadata types.
func signature(issues []ExpectedIssue) string {
	var b strings.Builder
	for _, e := range issues {
		b.WriteString(e.Group + "\x00" + e.Message + "\x00" + typesOf(e.Meta) + "\x01")
	}
	return b.String()
}

// fits reports whether an issue's path, key and code are the slot's.
func fits(is Issue, slot dsl.Slot) bool {
	path, err := SplitPath(is.Path)
	if err != nil || !slices.Equal(path, slot.Path) || is.Code != slot.Code {
		return false
	}
	return is.Key == "" || is.Key == slot.Key
}

func describeKey(is Issue) string {
	if is.Key == "" {
		return is.Code
	}
	return is.Key
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
func instance(is Issue, slot dsl.Slot, cat *catalog.Catalog) (ExpectedIssue, error) {
	e := ExpectedIssue{Issue: is, Slot: slot, Meta: map[string]value.Value{}, Group: slot.Group}
	for _, m := range is.Meta.Members {
		t, ok := slot.Meta[m.Name]
		if !ok {
			return e, fmt.Errorf("%s has no metadata %s", slot.Key, m.Name)
		}
		if slot.Candidates != nil && t.Kind == value.List && t.Args[0].Kind == value.Record {
			e.CandidatesMeta = m.Name
			if err := e.candidates(m.Value, slot, cat); err != nil {
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
	switch {
	case is.Key == "":
		// An issue a candidate reported, as raoh-java writes it, carries its message; it has to
		// be the one the slot gives.
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
	e.Message = want
	return e, nil
}

// candidates reads the candidates of a one_of_failed: every candidate exactly once, each with the
// issues it gave, read by the flow of that candidate for the same input.
func (e *ExpectedIssue) candidates(n *jsontext.Node, slot dsl.Slot, cat *catalog.Catalog) error {
	if n.Kind != jsontext.Array {
		return fmt.Errorf("expected an array of candidates")
	}
	e.Candidates = map[int][]ExpectedIssue{}
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
		expected, err := Expect(nested, dsl.Instantiate(slot.Candidates[k], slot.Input, slot.Path), cat)
		if err != nil {
			return fmt.Errorf("candidate %d: %w", k, err)
		}
		e.Candidates[k] = expected
	}
	for k := range slot.Candidates {
		if _, ok := e.Candidates[k]; !ok {
			return fmt.Errorf("candidate %d is missing: every candidate failed, and each reports its issues", k)
		}
	}
	return nil
}

// Groups assigns the issues a declaration gives to the unordered groups of the case's flow, by
// path, key and code only, for comparing them with a runner's; an issue that no assignment in
// order places is ordered.
func Groups(issues []Issue, slots []dsl.Slot) []string {
	groups := make([]string, len(issues))
	pos := positions(slots)
	used := make([]bool, len(slots))
	var search func(k, last int, group string) bool
	search = func(k, last int, group string) bool {
		if k == len(issues) {
			return true
		}
		for j, slot := range slots {
			p := pos[j]
			sameGroup := slot.Group != "" && slot.Group == group
			if used[j] || !fits(issues[k], slot) || (sameGroup && p != last) || (!sameGroup && p <= last) {
				continue
			}
			used[j] = true
			groups[k] = slot.Group
			if search(k+1, p, slot.Group) {
				return true
			}
			used[j] = false
		}
		return false
	}
	if !search(0, -1, "") {
		return make([]string, len(issues))
	}
	return groups
}

// describeValue writes a value for a message, as its message form when it has one.
func describeValue(v value.Value) string {
	if s, err := value.FormatForMessage(v); err == nil {
		return s
	}
	return v.Type.String()
}
