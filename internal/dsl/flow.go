package dsl

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/value"
)

// Flow is the issues a decoder can give: where each can arise, what it is, and in what order they
// come. It keeps the structure of the decoder, so that the path, the typing, the origin of the
// message and the ordering of every issue follow from one description. A flow is a grammar: for
// a given input, Instantiate lists the places issues can arise at, in order.
type Flow interface{ isFlow() }

// Site is one issue that can arise where the flow runs.
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
	// creates it; empty when the message is derived from the catalogue.
	Message string
	// Candidates are, for one_of_failed, the flows of the candidates it reports, by index.
	Candidates []Flow
}

// Seq is flows whose issues come one after the other, in this order. Of flows that exclude each
// other (the variants of a discriminate), at most one gives issues.
type Seq struct{ Items []Flow }

// Unordered is a flow whose issues come in the order of the input's members, which the
// specification leaves to the implementation: the issues of one instance of it are compared as a
// multiset. Each Unordered of a decoder has its own ID, so that two of them on the same object
// are two groups.
type Unordered struct {
	ID   int
	Body Flow
}

// Over is what a Repeat repeats over.
type Over int

// The things a Repeat repeats over.
const (
	// Elements are the elements of an array input.
	Elements Over = iota + 1
	// Members are the members of an object input.
	Members
)

// Repeat is a flow given for each element of an array or each member of an object, in order, at
// the path of the element or member. Members named in Except are skipped: a strict form reports
// only the members it does not know.
type Repeat struct {
	Over   Over
	Except []string
	Body   Flow
}

// At is a flow given at the path of a named member.
type At struct {
	Name string
	Body Flow
}

func (*Site) isFlow()      {}
func (Seq) isFlow()        {}
func (*Unordered) isFlow() {}
func (Repeat) isFlow()     {}
func (At) isFlow()         {}

// Slot is a place an issue can arise at for a given input: a site, the concrete path, the input
// there, and the unordered group instance it belongs to, if any.
type Slot struct {
	*Site
	Path  []string
	Input *jsontext.Node
	// Group identifies the instance of the Unordered the slot is in; empty when it is ordered.
	Group string
}

// Instantiate lists the slots of a flow for an input given at a path, in the order the flow gives
// its issues. The input may be nil for an absent value.
func Instantiate(f Flow, input *jsontext.Node, path []string) []Slot {
	var out []Slot
	var walk func(f Flow, in *jsontext.Node, at []string, group string)
	walk = func(f Flow, in *jsontext.Node, at []string, group string) {
		switch f := f.(type) {
		case *Site:
			out = append(out, Slot{Site: f, Path: at, Input: in, Group: group})
		case Seq:
			for _, item := range f.Items {
				walk(item, in, at, group)
			}
		case *Unordered:
			walk(f.Body, in, at, fmt.Sprintf("unordered#%d at %q", f.ID, JoinPath(at)))
		case Repeat:
			switch {
			case f.Over == Elements && in != nil && in.Kind == jsontext.Array:
				for i, e := range in.Elems {
					walk(f.Body, e, appendPath(at, strconv.Itoa(i)), group)
				}
			case f.Over == Members && in != nil && in.Kind == jsontext.Object:
				for _, m := range in.Members {
					if !slices.Contains(f.Except, m.Name) {
						walk(f.Body, m.Value, appendPath(at, m.Name), group)
					}
				}
			}
		case At:
			var child *jsontext.Node
			if in != nil && in.Kind == jsontext.Object {
				child, _ = in.Get(f.Name)
			}
			walk(f.Body, child, appendPath(at, f.Name), group)
		}
	}
	walk(f, input, append([]string{}, path...), "")
	return out
}

func appendPath(p []string, seg string) []string {
	return append(append([]string{}, p...), seg)
}

// JoinPath writes path segments as a JSON Pointer.
func JoinPath(segs []string) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString("/" + strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1"))
	}
	return b.String()
}

// Describe writes a flow's sites with their path patterns, for tests and messages: "*" stands
// for any element or member, and a site in an Unordered is marked with its ID.
func Describe(f Flow) []string {
	var out []string
	var walk func(f Flow, at string, group string)
	walk = func(f Flow, at string, group string) {
		switch f := f.(type) {
		case *Site:
			line := at + " " + f.Key
			if group != "" {
				line += " (" + group + ")"
			}
			out = append(out, line)
		case Seq:
			for _, item := range f.Items {
				walk(item, at, group)
			}
		case *Unordered:
			walk(f.Body, at, fmt.Sprintf("unordered#%d", f.ID))
		case Repeat:
			seg := "/*"
			if len(f.Except) > 0 {
				seg = "/*-{" + strings.Join(f.Except, ",") + "}"
			}
			walk(f.Body, at+seg, group)
		case At:
			walk(f.Body, at+JoinPath([]string{f.Name}), group)
		}
	}
	walk(f, "", "")
	return out
}
