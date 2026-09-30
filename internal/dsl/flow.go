package dsl

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/value"
)

// Flow is the language of the issue lists a decoder can give, built from the flow expressions of
// its forms. The empty list is success. See spec/issues.md.
type Flow interface{ isFlow() }

// Site is exactly one issue.
type Site struct {
	Key  string
	Code string
	// Meta is the type of each metadata entry the issue can have, and Optional the entries it may
	// leave out.
	Meta     map[string]value.Type
	Optional []string
	// Values are the metadata values the form decides, and MemberMeta the entries whose value is
	// the name of the member the issue is at. Every other entry is known only when a decoder runs.
	Values     map[string]value.Value
	MemberMeta []string
	// Message is the message the issue is given, by a message argument or by the fixture that
	// creates it; nil when the message is derived from the catalogue. An empty message is given.
	Message *string
	// Candidates are, for an issue that lists what candidates gave, the metadata entry that lists
	// them and the flow of each candidate; nil for any other issue.
	Candidates *CandidateList
}

// CandidateList is what an issue that lists what candidates gave lists: in the metadata entry
// Meta, for the candidate at each index of Flows, the issues its flow gives.
type CandidateList struct {
	Meta  string
	Flows []Flow
}

// Alt is the issue lists of any one of its items. An Alt with no items gives only the empty list.
type Alt struct{ Items []Flow }

// Cat is the issue lists of its items one after the other.
type Cat struct{ Items []Flow }

// Chain is the issue lists of its items one after the other, stopping after the first item that
// gives a non-empty list.
type Chain struct{ Items []Flow }

// Over is what a Repeat repeats over.
type Over int

// The things a Repeat repeats over.
const (
	// Elements are the elements of an array input.
	Elements Over = iota + 1
	// Members are the members of an object input.
	Members
)

// Repeat is its body for each element of an array input, or each member of an object input, one
// after the other, at the path of the element or member. Members named in Except are skipped.
type Repeat struct {
	Over   Over
	Except []string
	Body   Flow
}

// At is its body at the path of a named member.
type At struct {
	Name string
	Body Flow
}

// Candidates is a oneOf: success when some candidate succeeds, or its Site, which lists for every
// candidate a non-empty list of issues that candidate gives. Its Site's Candidates are the
// candidates' flows.
type Candidates struct {
	Site *Site
}

// Unordered is exactly one issue, its Site, for each member of an object input not named in
// Known, in any order: the order of the input's members, which the specification leaves to the
// implementation. Each Unordered of a decoder has its own ID.
type Unordered struct {
	ID    int
	Known []string
	Site  *Site
}

func (*Site) isFlow()       {}
func (*Alt) isFlow()        {}
func (*Cat) isFlow()        {}
func (*Chain) isFlow()      {}
func (*Repeat) isFlow()     {}
func (*At) isFlow()         {}
func (*Unordered) isFlow()  {}
func (*Candidates) isFlow() {}

// Success is the flow that gives only the empty list.
var Success Flow = &Alt{}

// Slot is an issue's place in a parse: the site, the concrete path, the input there, and, for an
// issue of an Unordered, the group instance; nil for an issue in order.
type Slot struct {
	*Site
	Path  []string
	Input *jsontext.Node
	Group *Group
}

// Group is one instance of an Unordered: the issues it gives for the unknown members of the object
// at Path, which come in any order. A parse gives every slot of an instance the same *Group, so a
// slot is in a group by pointer identity.
type Group struct {
	Unordered int
	Path      string
}

// JoinPath writes path segments as a JSON Pointer.
func JoinPath(segs []string) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString("/" + strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1"))
	}
	return b.String()
}

// Fit reads issue i at a slot. It gives what the parse of that issue depends on, to tell two
// parses apart, or false when the issue cannot be at the slot.
type Fit func(i int, slot Slot) (signature string, ok bool)

// Assignment is one way the issues fit a flow: the slot of each.
type Assignment []Slot

// parseLimit bounds the distinct partial parses kept for each node, position and end, so that a
// case with many equivalent parses fails instead of running long; two are enough to tell that a
// case is ambiguous.
const parseLimit = 2

// ParseIssues parses n issues against a flow for an input at a path. It gives every parse whose
// signature differs from the others, up to two: none means the issues are not a list the flow
// gives, two that they fit it in two ways that differ. The parser keeps, for each node, path and
// issue position, the positions its parses can end at, so it parses each of them once: the work is
// bounded by the product of the flow's nodes, the input's paths and the issues, never by the number
// of their combinations.
func ParseIssues(f Flow, input *jsontext.Node, path []string, n int, fit Fit) []Assignment {
	out, _ := parseIssues(f, input, path, n, fit)
	return out
}

// parseIssues is ParseIssues, and the number of node, path and position parses it made: the work
// it did.
func parseIssues(f Flow, input *jsontext.Node, path []string, n int, fit Fit) ([]Assignment, int) {
	p := &parser{n: n, fit: fit, memo: map[memoKey]endSet{}, groups: groups{}}
	var out []Assignment
	for _, pp := range p.parse(f, input, path, 0).at(n) {
		out = append(out, pp.slots)
	}
	return out, len(p.memo)
}

type partial struct {
	slots Assignment
	sig   string
}

type memoKey struct {
	node  Flow
	path  string
	start int
}

type parser struct {
	n      int
	fit    Fit
	memo   map[memoKey]endSet
	groups groups
}

// groups gives each instance of an Unordered one *Group.
type groups map[Group]*Group

func (gs groups) of(f *Unordered, at []string) *Group {
	g := Group{Unordered: f.ID, Path: JoinPath(at)}
	if p, ok := gs[g]; ok {
		return p
	}
	gs[g] = &g
	return &g
}

// endSet is the partial parses of a node from one position, by the position they end at, in
// increasing order: the order is the structure's, so what a node keeps, and every reading and
// message built from it, never depends on the order of a map.
type endSet []endParses

type endParses struct {
	end    int
	parses []partial
}

func (es endSet) find(end int) (int, bool) {
	return slices.BinarySearchFunc(es, end, func(e endParses, end int) int { return e.end - end })
}

// at gives the partial parses that end at end.
func (es endSet) at(end int) []partial {
	if i, ok := es.find(end); ok {
		return es[i].parses
	}
	return nil
}

// add records a partial parse ending at end, unless one with the same signature is there or the
// limit is reached.
func (es *endSet) add(end int, pp partial) {
	i, ok := es.find(end)
	if !ok {
		*es = slices.Insert(*es, i, endParses{end: end})
	}
	e := &(*es)[i]
	for _, q := range e.parses {
		if q.sig == pp.sig {
			return
		}
	}
	if len(e.parses) < parseLimit {
		e.parses = append(e.parses, pp)
	}
}

func join(a, b partial) partial {
	return partial{slots: append(slices.Clone(a.slots), b.slots...), sig: a.sig + b.sig}
}

// parse gives, for each end position, the partial parses of issues start..end by the flow.
func (p *parser) parse(f Flow, in *jsontext.Node, at []string, start int) endSet {
	// Every node is a pointer, so a node is its own identity in the memo.
	key := memoKey{node: f, path: JoinPath(at), start: start}
	if r, ok := p.memo[key]; ok {
		return r
	}
	var ends endSet
	switch f := f.(type) {
	case *Site:
		if start < p.n {
			slot := Slot{Site: f, Path: at, Input: in}
			if sig, ok := p.fit(start, slot); ok {
				ends.add(start+1, partial{slots: Assignment{slot}, sig: sig + "\x01"})
			}
		}
	case *Alt:
		if len(f.Items) == 0 {
			ends.add(start, partial{})
		}
		for _, item := range f.Items {
			for _, ep := range p.parse(item, in, at, start) {
				end, pps := ep.end, ep.parses
				for _, pp := range pps {
					ends.add(end, pp)
				}
			}
		}
	case *Cat:
		ends = endSet{{end: start, parses: []partial{{}}}}
		for _, item := range f.Items {
			var next endSet
			for _, ep := range ends {
				mid, heads := ep.end, ep.parses
				for _, ep := range p.parse(item, in, at, mid) {
					end, tails := ep.end, ep.parses
					for _, h := range heads {
						for _, t := range tails {
							next.add(end, join(h, t))
						}
					}
				}
			}
			ends = next
		}
	case *Chain:
		ends = p.chain(f.Items, in, at, start)
	case *Repeat:
		var items []Flow
		var ats [][]string
		var inputs []*jsontext.Node
		switch {
		case f.Over == Elements && in != nil && in.Kind == jsontext.Array:
			for i, e := range in.Elems {
				items, ats, inputs = append(items, f.Body), append(ats, appendPath(at, strconv.Itoa(i))), append(inputs, e)
			}
		case f.Over == Members && in != nil && in.Kind == jsontext.Object:
			for _, m := range in.Members {
				if !slices.Contains(f.Except, m.Name) {
					items, ats, inputs = append(items, f.Body), append(ats, appendPath(at, m.Name)), append(inputs, m.Value)
				}
			}
		}
		ends = endSet{{end: start, parses: []partial{{}}}}
		for k, item := range items {
			var next endSet
			for _, ep := range ends {
				mid, heads := ep.end, ep.parses
				for _, ep := range p.parse(item, inputs[k], ats[k], mid) {
					end, tails := ep.end, ep.parses
					for _, h := range heads {
						for _, t := range tails {
							next.add(end, join(h, t))
						}
					}
				}
			}
			ends = next
		}
	case *At:
		var child *jsontext.Node
		if in != nil && in.Kind == jsontext.Object {
			child, _ = in.Get(f.Name)
		}
		ends = p.parse(f.Body, child, appendPath(at, f.Name), start)
	case *Unordered:
		ends = p.unordered(f, in, at, start)
	case *Candidates:
		for _, c := range f.Site.Candidates.Flows {
			if len(p.parse(c, in, at, start).at(start)) > 0 {
				ends.add(start, partial{})
				break
			}
		}
		if start < p.n {
			slot := Slot{Site: f.Site, Path: at, Input: in}
			if sig, ok := p.fit(start, slot); ok {
				ends.add(start+1, partial{slots: Assignment{slot}, sig: sig + "\x01"})
			}
		}
	}
	p.memo[key] = ends
	return ends
}

// chain parses items as a Chain: the first item's non-empty parses end the chain; its empty parse
// goes on with the rest.
func (p *parser) chain(items []Flow, in *jsontext.Node, at []string, start int) endSet {
	var ends endSet
	if len(items) == 0 {
		ends.add(start, partial{})
		return ends
	}
	for _, ep := range p.parse(items[0], in, at, start) {
		end, pps := ep.end, ep.parses
		if end == start {
			continue
		}
		for _, pp := range pps {
			ends.add(end, pp)
		}
	}
	for _, empty := range p.parse(items[0], in, at, start).at(start) {
		for _, ep := range p.chain(items[1:], in, at, start) {
			end, pps := ep.end, ep.parses
			for _, pp := range pps {
				ends.add(end, join(empty, pp))
			}
		}
	}
	return ends
}

// unordered parses one issue for each unknown member, in any order.
func (p *parser) unordered(f *Unordered, in *jsontext.Node, at []string, start int) endSet {
	var ends endSet
	var slots []Slot
	group := p.groups.of(f, at)
	if in != nil && in.Kind == jsontext.Object {
		for _, m := range in.Members {
			if !slices.Contains(f.Known, m.Name) {
				slots = append(slots, Slot{Site: f.Site, Path: appendPath(at, m.Name), Input: m.Value, Group: group})
			}
		}
	}
	k := len(slots)
	if start+k > p.n {
		return ends
	}
	// Match issues start..start+k to the slots one to one; each issue fits at most the slot of the
	// member its path names, so the matching is found issue by issue.
	used := make([]bool, k)
	pp := partial{slots: make(Assignment, k)}
	for i := 0; i < k; i++ {
		matched := false
		for j, slot := range slots {
			if used[j] {
				continue
			}
			if sig, ok := p.fit(start+i, slot); ok {
				used[j], matched = true, true
				pp.slots[i] = slot
				pp.sig += sig + "\x01"
				break
			}
		}
		if !matched {
			return ends
		}
	}
	ends.add(start+k, pp)
	return ends
}

func appendPath(p []string, seg string) []string {
	return append(append([]string{}, p...), seg)
}

// Describe writes a flow as an expression, for tests and messages.
func Describe(f Flow) string {
	switch f := f.(type) {
	case *Site:
		return f.Key
	case *Alt:
		return "alt(" + describeAll(f.Items) + ")"
	case *Cat:
		return "cat(" + describeAll(f.Items) + ")"
	case *Chain:
		return "chain(" + describeAll(f.Items) + ")"
	case *Repeat:
		over := "elements"
		if f.Over == Members {
			over = "members"
		}
		return "each_" + over + "(" + Describe(f.Body) + ")"
	case *At:
		return "at(" + f.Name + ", " + Describe(f.Body) + ")"
	case *Unordered:
		return fmt.Sprintf("unknown#%d(except %s: %s)", f.ID, strings.Join(f.Known, ","), f.Site.Key)
	case *Candidates:
		return "candidates(" + describeAll(f.Site.Candidates.Flows) + ": " + f.Site.Key + ")"
	}
	return "?"
}

func describeAll(items []Flow) string {
	var parts []string
	for _, i := range items {
		parts = append(parts, Describe(i))
	}
	return strings.Join(parts, ", ")
}

// Places lists every place an issue can arise at in a flow for an input, whatever the other
// issues: the flow's structure without its ordering and exclusions. It is for explaining why a
// list of issues is not one the flow gives.
func Places(f Flow, input *jsontext.Node, path []string) []Slot {
	var out []Slot
	gs := groups{}
	var walk func(f Flow, in *jsontext.Node, at []string)
	walk = func(f Flow, in *jsontext.Node, at []string) {
		switch f := f.(type) {
		case *Site:
			out = append(out, Slot{Site: f, Path: at, Input: in})
		case *Alt:
			for _, i := range f.Items {
				walk(i, in, at)
			}
		case *Cat:
			for _, i := range f.Items {
				walk(i, in, at)
			}
		case *Chain:
			for _, i := range f.Items {
				walk(i, in, at)
			}
		case *Repeat:
			switch {
			case f.Over == Elements && in != nil && in.Kind == jsontext.Array:
				for i, e := range in.Elems {
					walk(f.Body, e, appendPath(at, strconv.Itoa(i)))
				}
			case f.Over == Members && in != nil && in.Kind == jsontext.Object:
				for _, m := range in.Members {
					if !slices.Contains(f.Except, m.Name) {
						walk(f.Body, m.Value, appendPath(at, m.Name))
					}
				}
			}
		case *At:
			var child *jsontext.Node
			if in != nil && in.Kind == jsontext.Object {
				child, _ = in.Get(f.Name)
			}
			walk(f.Body, child, appendPath(at, f.Name))
		case *Candidates:
			out = append(out, Slot{Site: f.Site, Path: at, Input: in})
		case *Unordered:
			group := gs.of(f, at)
			if in != nil && in.Kind == jsontext.Object {
				for _, m := range in.Members {
					if !slices.Contains(f.Known, m.Name) {
						out = append(out, Slot{Site: f.Site, Path: appendPath(at, m.Name), Input: m.Value, Group: group})
					}
				}
			}
		}
	}
	walk(f, input, append([]string{}, path...))
	return out
}
