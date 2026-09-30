package dsl

import (
	"strings"

	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/value"
)

// Flow is the issues a decoder can give: where each can arise, what it is, and in what order they
// come. It keeps the structure of the decoder, so that the path, the typing, the origin of the
// message and the ordering of every issue follow from one description.
type Flow interface{ isFlow() }

// Site is one issue that can arise where the flow runs.
type Site struct {
	Key  string
	Code string
	// Meta is the type of each metadata entry the issue can have, and Optional the entries it may
	// leave out.
	Meta     map[string]value.Type
	Optional []string
	// Fixed are metadata values the form always gives.
	Fixed map[string]*jsontext.Node
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
// specification leaves to the implementation: they are compared as a multiset.
type Unordered struct{ Body Flow }

// Repeat is a flow given for each element of an array or each member of an object, in order, at
// the path of the element or member.
type Repeat struct{ Body Flow }

// At is a flow given at the path of a named member.
type At struct {
	Name string
	Body Flow
}

func (*Site) isFlow()     {}
func (Seq) isFlow()       {}
func (Unordered) isFlow() {}
func (Repeat) isFlow()    {}
func (At) isFlow()        {}

// Seg is one segment of a path pattern: a member name, or any member or index.
type Seg struct {
	Name string
	Any  bool
}

func (s Seg) String() string {
	if s.Any {
		return "*"
	}
	return strings.ReplaceAll(strings.ReplaceAll(s.Name, "~", "~0"), "/", "~1")
}

// Located is a site with the path pattern where it arises, relative to the decoder, and, when its
// issues are unordered, the path pattern of the object whose members order them.
type Located struct {
	*Site
	Path []Seg
	// Group is the path of the Unordered flow the site is in; nil when the site is ordered.
	Group []Seg
}

// PathString writes a path pattern.
func PathString(p []Seg) string {
	var b strings.Builder
	for _, s := range p {
		b.WriteString("/" + s.String())
	}
	return b.String()
}

// Sites lists the sites of a flow, in order, with their paths.
func Sites(f Flow) []Located {
	var out []Located
	var walk func(f Flow, at []Seg, group []Seg)
	walk = func(f Flow, at []Seg, group []Seg) {
		switch f := f.(type) {
		case *Site:
			out = append(out, Located{Site: f, Path: at, Group: group})
		case Seq:
			for _, item := range f.Items {
				walk(item, at, group)
			}
		case Unordered:
			walk(f.Body, at, clonePath(at))
		case Repeat:
			walk(f.Body, append(clonePath(at), Seg{Any: true}), group)
		case At:
			walk(f.Body, append(clonePath(at), Seg{Name: f.Name}), group)
		}
	}
	walk(f, []Seg{}, nil)
	return out
}

func clonePath(p []Seg) []Seg { return append([]Seg{}, p...) }

// Matches reports whether a concrete path, as segments, fits a path pattern.
func Matches(pattern []Seg, path []string) bool {
	if len(pattern) != len(path) {
		return false
	}
	for i, s := range pattern {
		if !s.Any && s.Name != path[i] {
			return false
		}
	}
	return true
}
