// Package value is the portable value model: the types a decoder's result, an encoder's input and
// an issue's metadata can have, how a value of each type is written as an observation, and when two
// values are the same.
package value

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Kind is the kind of a type.
type Kind int

// The kinds of type. See spec/value-model.md. The zero Kind is Invalid, so that a Type nobody set
// is never mistaken for a type.
const (
	Invalid Kind = iota
	Bool
	Int32
	Int64
	Float32
	Float64
	Decimal
	String
	Symbol
	Instant
	Date
	Time
	DateTime
	OffsetDateTime
	UUID
	URI
	List
	Set
	Map
	Product
	Presence
	Optional
	Nullable
	Record
	JSON
	Issues
	Param
)

var kindNames = map[Kind]string{
	Bool: "bool", Int32: "int32", Int64: "int64", Float32: "float32", Float64: "float64",
	Decimal: "decimal", String: "string", Symbol: "symbol", Instant: "instant", Date: "date",
	Time: "time", DateTime: "datetime", OffsetDateTime: "offset_datetime", UUID: "uuid", URI: "uri",
	List: "list", Set: "set", Map: "map", Product: "product", Presence: "presence",
	Optional: "optional", Nullable: "nullable", Record: "record", JSON: "json", Issues: "issues",
}

var kindsByName = func() map[string]Kind {
	m := map[string]Kind{}
	for k, n := range kindNames {
		m[n] = k
	}
	return m
}()

// arity is the number of type arguments a kind takes; -1 is any number of at least one.
var arity = map[Kind]int{
	List: 1, Set: 1, Map: 1, Presence: 1, Optional: 1, Nullable: 1, Product: -1, Record: -1,
}

// Type is a type of the value model.
type Type struct {
	Kind Kind
	// Args are the type arguments: the element type of a List, Set, Map (whose keys are strings),
	// Presence, Optional or Nullable, the element types of a Product, the field types of a Record.
	Args []Type
	// Fields are the field names of a Record, in the order of Args.
	Fields []string
	// Name is the name of a Param.
	Name string
	// Symbols are the alternatives of a Symbol; a well-formed Symbol has at least one.
	Symbols []string
}

// SymbolOf returns the symbol type with the given alternatives.
func SymbolOf(alternatives ...string) Type {
	if alternatives == nil {
		alternatives = []string{}
	}
	return Type{Kind: Symbol, Symbols: alternatives}
}

// Of returns the type of a kind that takes no arguments.
func Of(k Kind) Type { return Type{Kind: k} }

// ListOf returns list<elem>.
func ListOf(elem Type) Type { return Type{Kind: List, Args: []Type{elem}} }

// ProductOf returns product<elems...>.
func ProductOf(elems ...Type) Type { return Type{Kind: Product, Args: elems} }

// String is the name of a kind, as a type is written: int32, list, symbol.
func (k Kind) String() string { return kindNames[k] }

func (t Type) String() string {
	if t.Kind == Invalid {
		return "<invalid>"
	}
	if t.Kind == Param {
		return t.Name
	}
	name := kindNames[t.Kind]
	if t.Kind == Symbol {
		quoted := make([]string, len(t.Symbols))
		for i, s := range t.Symbols {
			quoted[i] = strconv.Quote(s)
		}
		return name + "<" + strings.Join(quoted, ",") + ">"
	}
	if len(t.Args) == 0 {
		return name
	}
	parts := make([]string, len(t.Args))
	for i, a := range t.Args {
		if t.Kind == Record {
			parts[i] = t.Fields[i] + ":" + a.String()
		} else {
			parts[i] = a.String()
		}
	}
	return name + "<" + strings.Join(parts, ",") + ">"
}

// Same reports whether two types are the same type.
func (t Type) Same(u Type) bool { return t.String() == u.String() }

// IsNumeric reports whether values of the type are numbers.
func (t Type) IsNumeric() bool {
	switch t.Kind {
	case Int32, Int64, Float32, Float64, Decimal:
		return true
	}
	return false
}

// IsTemporal reports whether values of the type are points or dates in time.
func (t Type) IsTemporal() bool {
	switch t.Kind {
	case Instant, Date, Time, DateTime, OffsetDateTime:
		return true
	}
	return false
}

// symbols reads the alternatives of a symbol type, <"A","B">; a symbol type always lists them.
func (p *typeParser) symbols() (Type, error) {
	t := Type{Kind: Symbol}
	if p.pos >= len(p.s) || p.s[p.pos] != '<' {
		return Type{}, fmt.Errorf(`a symbol type lists its alternatives, as symbol<"A","B">`)
	}
	p.pos++
	t.Symbols = []string{}
	for {
		q, err := strconv.QuotedPrefix(p.s[p.pos:])
		if err != nil {
			return Type{}, fmt.Errorf("expected a quoted alternative at %d", p.pos)
		}
		s, _ := strconv.Unquote(q)
		t.Symbols = append(t.Symbols, s)
		p.pos += len(q)
		if p.pos < len(p.s) && p.s[p.pos] == ',' {
			p.pos++
			continue
		}
		if p.pos < len(p.s) && p.s[p.pos] == '>' {
			p.pos++
			return t, nil
		}
		return Type{}, fmt.Errorf("unterminated alternatives")
	}
}

// Params returns the names of the type parameters t mentions.
func (t Type) Params() []string {
	var names []string
	var walk func(Type)
	walk = func(t Type) {
		if t.Kind == Param {
			for _, n := range names {
				if n == t.Name {
					return
				}
			}
			names = append(names, t.Name)
		}
		for _, a := range t.Args {
			walk(a)
		}
	}
	walk(t)
	return names
}

// Subst replaces the type parameters of t with the types args gives them.
func (t Type) Subst(args map[string]Type) Type {
	if t.Kind == Param {
		if a, ok := args[t.Name]; ok {
			return a
		}
		return t
	}
	if len(t.Args) == 0 {
		return t
	}
	out := Type{Kind: t.Kind, Fields: t.Fields, Symbols: t.Symbols, Args: make([]Type, len(t.Args))}
	for i, a := range t.Args {
		out.Args[i] = a.Subst(args)
	}
	return out
}

// Unify matches a pattern that may mention type parameters against a concrete type, adding to
// bound the parameters it fixes. It reports whether they match.
func Unify(pattern, t Type, bound map[string]Type) bool {
	if pattern.Kind == Param {
		if b, ok := bound[pattern.Name]; ok {
			return b.Same(t)
		}
		bound[pattern.Name] = t
		return true
	}
	if pattern.Kind != t.Kind || len(pattern.Args) != len(t.Args) {
		return false
	}
	if pattern.Kind == Symbol && !pattern.Same(t) {
		return false
	}
	for i := range pattern.Args {
		if pattern.Kind == Record && pattern.Fields[i] != t.Fields[i] {
			return false
		}
		if !Unify(pattern.Args[i], t.Args[i], bound) {
			return false
		}
	}
	return true
}

// ParseType reads a type written as in spec/value-model.md: "int32", "list<float32>",
// "product<string,int32>", "record<candidate:int32,issues:json>". A single upper-case letter is a
// type parameter.
func ParseType(s string) (Type, error) {
	p := typeParser{s: s}
	t, err := p.parse()
	if err != nil {
		return Type{}, fmt.Errorf("type %q: %w", s, err)
	}
	if p.pos != len(s) {
		return Type{}, fmt.Errorf("type %q: unexpected %q", s, s[p.pos:])
	}
	if err := WellFormed(t); err != nil {
		return Type{}, fmt.Errorf("type %q: %w", s, err)
	}
	return t, nil
}

// nullObservable reports whether JSON null is an observation of some value of the type.
func nullObservable(t Type) bool {
	switch t.Kind {
	case Optional, Nullable, JSON:
		return true
	}
	return false
}

// Concrete checks that a type is one values have: it mentions no type parameter and is well
// formed. A type built by substituting parameters is checked again, since a parameter may stand for
// a type that makes it ill-formed, such as nullable<string> in optional<T>.
func (t Type) Concrete() error {
	if ps := t.Params(); len(ps) > 0 {
		return fmt.Errorf("cannot tell what %s is", strings.Join(ps, ", "))
	}
	return WellFormed(t)
}

// WellFormed checks that every value of the type has an observation no other value has. An
// optional or nullable value observes its absence as null, so what it holds must not observe
// anything as null: optional<nullable<T>> could not tell an empty optional from a null in it.
// Types built by substituting type parameters are checked again, since a parameter may stand for
// such a type.
func WellFormed(t Type) error {
	if t.Kind == Invalid {
		return fmt.Errorf("the type is missing")
	}
	if t.Kind == Symbol {
		if len(t.Symbols) == 0 {
			return fmt.Errorf("a symbol type has at least one alternative")
		}
		for i, s := range t.Symbols {
			if slices.Contains(t.Symbols[:i], s) {
				return fmt.Errorf("%s lists %q twice", t, s)
			}
		}
	}
	if (t.Kind == Optional || t.Kind == Nullable) && nullObservable(t.Args[0]) {
		return fmt.Errorf("%s cannot tell its own null from a null of %s", t, t.Args[0])
	}
	for _, a := range t.Args {
		if err := WellFormed(a); err != nil {
			return err
		}
	}
	return nil
}

// MustParseType is ParseType for types written in code.
func MustParseType(s string) Type {
	t, err := ParseType(s)
	if err != nil {
		panic(err)
	}
	return t
}

type typeParser struct {
	s   string
	pos int
}

func (p *typeParser) ident() string {
	start := p.pos
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if c == '_' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9') {
			p.pos++
			continue
		}
		break
	}
	return p.s[start:p.pos]
}

func (p *typeParser) parse() (Type, error) {
	name := p.ident()
	if name == "" {
		return Type{}, fmt.Errorf("expected a type name at %d", p.pos)
	}
	if len(name) == 1 && 'A' <= name[0] && name[0] <= 'Z' {
		return Type{Kind: Param, Name: name}, nil
	}
	k, ok := kindsByName[name]
	if !ok {
		return Type{}, fmt.Errorf("unknown type %q", name)
	}
	t := Type{Kind: k}
	if k == Symbol {
		return p.symbols()
	}
	want, generic := arity[k]
	if p.pos >= len(p.s) || p.s[p.pos] != '<' {
		if generic {
			return Type{}, fmt.Errorf("%s needs type arguments", name)
		}
		return t, nil
	}
	if !generic {
		return Type{}, fmt.Errorf("%s takes no type arguments", name)
	}
	p.pos++
	for {
		if k == Record {
			field := p.ident()
			if field == "" || p.pos >= len(p.s) || p.s[p.pos] != ':' {
				return Type{}, fmt.Errorf("expected field:type at %d", p.pos)
			}
			p.pos++
			t.Fields = append(t.Fields, field)
		}
		a, err := p.parse()
		if err != nil {
			return Type{}, err
		}
		t.Args = append(t.Args, a)
		if p.pos >= len(p.s) {
			return Type{}, fmt.Errorf("unterminated type arguments")
		}
		if p.s[p.pos] == ',' {
			p.pos++
			continue
		}
		if p.s[p.pos] == '>' {
			p.pos++
			break
		}
		return Type{}, fmt.Errorf("unexpected %q at %d", p.s[p.pos], p.pos)
	}
	if want > 0 && len(t.Args) != want {
		return Type{}, fmt.Errorf("%s takes %d type argument(s)", name, want)
	}
	return t, nil
}
