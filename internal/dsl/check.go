package dsl

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/raoh-project/raoh-specification/internal/catalog"
	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/value"
)

// Checked is what type-checking a decoder or encoder form finds.
type Checked struct {
	// Result is the result type of a decoder, and the input type of an encoder.
	Result value.Type
	// Features are the feature IDs the form needs, sorted.
	Features []string
	// Flow is the issue lists a decoder can give; Success for an encoder.
	Flow Flow
}

// Checker type-checks forms against a registry and an issue catalogue that NewChecker has checked
// against each other.
type Checker struct {
	registry *Registry
	catalog  *catalog.Catalog
}

// NewChecker checks a registry against the issue catalogue (see Validate) and returns a checker for
// them. A Checker exists only for a registry and a catalogue that agree, so checking a form never
// finds the catalogues wrong.
func NewChecker(r *Registry, c *catalog.Catalog) (*Checker, error) {
	if err := Validate(r, c); err != nil {
		return nil, err
	}
	return &Checker{registry: r, catalog: c}, nil
}

// Registry is the registry the checker checks forms against.
func (c *Checker) Registry() *Registry { return c.registry }

// Catalog is the issue catalogue the checker checks forms against.
func (c *Checker) Catalog() *catalog.Catalog { return c.catalog }

type state struct {
	*Checker
	features map[string]bool
	// unordered counts the Unordered flows made, to give each its own ID.
	unordered int
}

func (c *Checker) newState() *state {
	return &state{Checker: c, features: map[string]bool{}}
}

func (s *state) done(result value.Type, flow Flow) *Checked {
	out := &Checked{Result: result, Flow: flow}
	for f := range s.features {
		out.Features = append(out.Features, f)
	}
	sort.Strings(out.Features)
	return out
}

// CheckDecoder type-checks a decoder form.
func (c *Checker) CheckDecoder(n *jsontext.Node) (*Checked, error) {
	s := c.newState()
	t, flow, err := s.decoder(n)
	if err != nil {
		return nil, err
	}
	return s.done(t, flow), nil
}

// CheckEncoder type-checks an encoder form.
func (c *Checker) CheckEncoder(n *jsontext.Node) (*Checked, error) {
	s := c.newState()
	t, err := s.encoder(n)
	if err != nil {
		return nil, err
	}
	return s.done(t, Success), nil
}

func head(n *jsontext.Node, what string) (string, error) {
	if n.Kind != jsontext.Array || len(n.Elems) == 0 || n.Elems[0].Kind != jsontext.String {
		return "", fmt.Errorf("%s: expected [name, ...], found %s", what, abbreviate(n))
	}
	return n.Elems[0].Text, nil
}

func abbreviate(n *jsontext.Node) string {
	s := string(n.Raw)
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}

func concrete(t value.Type, what string) error {
	if ps := t.Params(); len(ps) > 0 {
		return fmt.Errorf("%s: cannot tell what %s is", what, strings.Join(ps, ", "))
	}
	if err := value.WellFormed(t); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if t.Kind == value.Symbol && t.Symbols == nil {
		return fmt.Errorf("%s: cannot tell the alternatives of the symbol", what)
	}
	return nil
}

// checkedArgs is what checking a form's arguments gives besides the types it binds, by the
// index of the argument.
type checkedArgs struct {
	// values are the values of every value argument, the default of one left out.
	values map[ArgRef]value.Value
	// flows are the flows of a decoder argument (one), a decoders argument or a variants
	// argument (one for each decoder).
	flows map[ArgRef][]Flow
	// fields are the fields of a fields argument.
	fields map[ArgRef][]field
	// product is the types of the fields, in order.
	product []value.Type
	// keys are the variant names of a variants argument.
	keys     map[ArgRef][]string
	fixtures map[ArgRef]checkedFixture
	// message is the message the form's message argument gives, or nil when the form has none or
	// it is left out; an empty message is a message.
	message *string
}

// checkedFixture is the fixture a fixture argument names, with the types its parameters take where
// it is used.
type checkedFixture struct {
	*Fixture
	bound map[string]value.Type
}

// field is a checked field: its flow, and the member it reads, if it names one.
type field struct {
	flow   Flow
	member string
	named  bool
}

func (s *state) decoder(n *jsontext.Node) (value.Type, Flow, error) {
	name, err := head(n, "decoder")
	if err != nil {
		return value.Type{}, nil, err
	}
	f, ok := s.registry.Constructors[name]
	if !ok {
		return value.Type{}, nil, fmt.Errorf("unknown constructor %q", name)
	}
	s.features["decoder."+name] = true
	if len(n.Elems)-1 < len(f.Args) {
		return value.Type{}, nil, fmt.Errorf("%s takes %d argument(s), found %d", name, len(f.Args), len(n.Elems)-1)
	}
	bound := map[string]value.Type{}
	ca, err := s.args(f, n.Elems[1:1+len(f.Args)], bound)
	if err != nil {
		return value.Type{}, nil, fmt.Errorf("%s: %w", name, err)
	}
	result := f.Result.Subst(bound)
	if f.Result.Kind == value.Product && len(f.Result.Args) == 0 {
		result = value.ProductOf(ca.product...)
	}
	if f.SymbolsFrom != NoArg {
		var alternatives []string
		for _, e := range ca.values[f.SymbolsFrom].Elems {
			alternatives = append(alternatives, e.Str)
		}
		result = value.SymbolOf(alternatives...)
	}
	if err := concrete(result, name); err != nil {
		return value.Type{}, nil, err
	}
	flow, err := s.build(name, f, f.Flow, bound, ca)
	if err != nil {
		return value.Type{}, nil, err
	}
	// Each operation runs only if everything before it succeeded.
	for _, step := range n.Elems[1+len(f.Args):] {
		var stepFlow Flow
		if result, stepFlow, err = s.operation(step, result); err != nil {
			return value.Type{}, nil, err
		}
		flow = &Chain{Items: []Flow{flow, stepFlow}}
	}
	return result, flow, nil
}

// build turns a form's flow expression into the flow it gives for these arguments.
func (s *state) build(owner string, f *Form, x Expr, bound map[string]value.Type, ca checkedArgs) (Flow, error) {
	all := func(xs []Expr) ([]Flow, error) {
		var out []Flow
		for _, e := range xs {
			fl, err := s.build(owner, f, e, bound, ca)
			if err != nil {
				return nil, err
			}
			out = append(out, fl)
		}
		return out, nil
	}
	switch x := x.(type) {
	case *ExprOwn:
		items := []Flow{Success}
		for _, i := range x.Issues {
			site, err := s.site(f.Issues[i.Index()], bound, ca)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", owner, err)
			}
			items = append(items, site)
		}
		return &Alt{Items: items}, nil
	case ExprNone, ExprDiscard:
		return Success, nil
	case ExprArg:
		switch f.Args[x.Arg.Index()].Kind {
		case "decoder":
			return ca.flows[x.Arg][0], nil
		case "variants":
			// One variant runs, whichever the tag names.
			return &Alt{Items: ca.flows[x.Arg]}, nil
		case "fields":
			var items []Flow
			for _, fl := range ca.fields[x.Arg] {
				items = append(items, fl.flow)
			}
			return &Cat{Items: items}, nil
		}
		return nil, fmt.Errorf("%s: argument %s gives no flow", owner, f.Args[x.Arg.Index()].Name)
	case ExprCat:
		items, err := all(x.Items)
		return &Cat{Items: items}, err
	case ExprChain:
		items, err := all(x.Items)
		return &Chain{Items: items}, err
	case ExprEach:
		body, err := s.build(owner, f, x.Body, bound, ca)
		return &Repeat{Over: x.Over, Body: body}, err
	case ExprAt:
		body, err := s.build(owner, f, x.Body, bound, ca)
		return &At{Name: ca.values[x.Member].Str, Body: body}, err
	case ExprUnknown:
		var known []string
		if x.KnownArg != NoArg {
			for _, e := range ca.values[x.KnownArg].Elems {
				known = append(known, e.Str)
			}
		} else {
			for _, fl := range ca.fields[x.KnownFields] {
				if !fl.named {
					return nil, fmt.Errorf("%s: a field that reads the whole input leaves the members it knows unknown", owner)
				}
				known = append(known, fl.member)
			}
		}
		site, err := s.site(f.Issues[x.Issue.Index()], bound, ca)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", owner, err)
		}
		s.unordered++
		return &Unordered{ID: s.unordered, Known: known, Site: site}, nil
	case ExprCandidates:
		site, err := s.site(f.Issues[x.Issue.Index()], bound, ca)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", owner, err)
		}
		site.Candidates = &CandidateList{Meta: x.Meta, Flows: ca.flows[x.Decoders]}
		return &Candidates{Site: site}, nil
	case ExprFixture:
		fx := ca.fixtures[x.Arg]
		meta := map[string]value.Type{}
		for name, t := range fx.Issue.Meta {
			meta[name] = t.Subst(fx.bound)
			if err := concrete(meta[name], "fixture "+fx.Name+" meta "+name); err != nil {
				return nil, fmt.Errorf("%s: %w", owner, err)
			}
		}
		message := fx.Issue.Message
		var fl Flow = &Site{Key: fx.Issue.Key, Code: fx.Issue.Code, Meta: meta, Message: &message}
		for i := len(fx.Issue.Path) - 1; i >= 0; i-- {
			fl = &At{Name: fx.Issue.Path[i], Body: fl}
		}
		return &Alt{Items: []Flow{Success, fl}}, nil
	}
	return nil, fmt.Errorf("%s: an unknown flow expression", owner)
}

// field checks a field form. A field whose flow is at a member reads that member.
func (s *state) field(n *jsontext.Node) (value.Type, field, error) {
	name, err := head(n, "field")
	if err != nil {
		return value.Type{}, field{}, err
	}
	f, ok := s.registry.Fields[name]
	if !ok {
		return value.Type{}, field{}, fmt.Errorf("unknown field kind %q", name)
	}
	s.features["field."+name] = true
	if len(n.Elems)-1 != len(f.Args) {
		return value.Type{}, field{}, fmt.Errorf("%s takes %d argument(s), found %d", name, len(f.Args), len(n.Elems)-1)
	}
	bound := map[string]value.Type{}
	ca, err := s.args(f, n.Elems[1:], bound)
	if err != nil {
		return value.Type{}, field{}, fmt.Errorf("%s: %w", name, err)
	}
	result := f.Result.Subst(bound)
	if err := concrete(result, name); err != nil {
		return value.Type{}, field{}, err
	}
	flow, err := s.build(name, f, f.Flow, bound, ca)
	if err != nil {
		return value.Type{}, field{}, err
	}
	out := field{flow: flow}
	if at, ok := f.Flow.(ExprAt); ok {
		out.member, out.named = ca.values[at.Member].Str, true
	}
	return result, out, nil
}

func (s *state) operation(n *jsontext.Node, receiver value.Type) (value.Type, Flow, error) {
	name, err := head(n, "operation")
	if err != nil {
		return value.Type{}, nil, err
	}
	overloads, ok := s.registry.Operations[name]
	if !ok {
		return value.Type{}, nil, fmt.Errorf("unknown operation %q", name)
	}
	// An operation applies to every type, or has at most one overload for the receiver's kind.
	o, ok := overloads[AnyReceiver]
	if !ok {
		o, ok = overloads[ReceiverKey(value.Type{Kind: receiver.Kind}.String())]
	}
	bound := map[string]value.Type{}
	if !ok || (o.Receiver.Pattern != nil && !value.Unify(*o.Receiver.Pattern, receiver, bound)) {
		return value.Type{}, nil, fmt.Errorf("operation %s does not apply to %s", name, receiver)
	}
	f := o.Form
	s.features[operationFeature(o.Receiver.Key, name)] = true
	bound["R"] = receiver
	given := n.Elems[1:]
	required := 0
	for _, a := range f.Args {
		if !a.Optional {
			required++
		}
	}
	if len(given) < required || len(given) > len(f.Args) {
		return value.Type{}, nil, fmt.Errorf("%s takes %d to %d argument(s), found %d", name, required, len(f.Args), len(given))
	}
	ca, err := s.args(f, given, bound)
	if err != nil {
		return value.Type{}, nil, fmt.Errorf("%s: %w", name, err)
	}
	result := f.Result.Subst(bound)
	if err := concrete(result, name); err != nil {
		return value.Type{}, nil, err
	}
	flow, err := s.build(name, f, f.Flow, bound, ca)
	if err != nil {
		return value.Type{}, nil, err
	}
	return result, flow, nil
}

func (s *state) encoder(n *jsontext.Node) (value.Type, error) {
	name, err := head(n, "encoder")
	if err != nil {
		return value.Type{}, err
	}
	f, ok := s.registry.Encoders[name]
	if !ok {
		return value.Type{}, fmt.Errorf("unknown encoder %q", name)
	}
	s.features["encoder."+name] = true
	if len(n.Elems)-1 != len(f.Args) {
		return value.Type{}, fmt.Errorf("%s takes %d argument(s), found %d", name, len(f.Args), len(n.Elems)-1)
	}
	bound := map[string]value.Type{}
	if _, err := s.args(f, n.Elems[1:], bound); err != nil {
		return value.Type{}, fmt.Errorf("%s: %w", name, err)
	}
	input := f.Input.Subst(bound)
	return input, concrete(input, name)
}

func (s *state) property(n *jsontext.Node) (value.Type, error) {
	name, err := head(n, "property")
	if err != nil {
		return value.Type{}, err
	}
	f, ok := s.registry.Properties[name]
	if !ok {
		return value.Type{}, fmt.Errorf("unknown property kind %q", name)
	}
	s.features["property."+name] = true
	if len(n.Elems)-1 != len(f.Args) {
		return value.Type{}, fmt.Errorf("%s takes %d argument(s), found %d", name, len(f.Args), len(n.Elems)-1)
	}
	bound := map[string]value.Type{}
	if _, err := s.args(f, n.Elems[1:], bound); err != nil {
		return value.Type{}, fmt.Errorf("%s: %w", name, err)
	}
	input := f.Input.Subst(bound)
	return input, concrete(input, name)
}

// args checks the arguments given, the first of the form's, in three passes, so that the types
// that decoders and encoders fix are known when values are read and fixtures are matched, and
// then the conditions the form requires of them. A value argument left out stands for its
// default, so every value argument has a value in what args gives; a message argument left out
// gives no message.
func (s *state) args(f *Form, nodes []*jsontext.Node, bound map[string]value.Type) (checkedArgs, error) {
	ca := checkedArgs{values: map[ArgRef]value.Value{}, flows: map[ArgRef][]Flow{}, fields: map[ArgRef][]field{},
		keys: map[ArgRef][]string{}, fixtures: map[ArgRef]checkedFixture{}}
	decoder := func(r ArgRef, a Arg, n *jsontext.Node) error {
		t, flow, err := s.decoder(n)
		if err != nil {
			return err
		}
		if !value.Unify(a.Type, t, bound) {
			return fmt.Errorf("%s: expected a decoder of %s, found one of %s", a.Name, a.Type.Subst(bound), t)
		}
		ca.flows[r] = append(ca.flows[r], flow)
		return nil
	}
	for pass := 0; pass < 3; pass++ {
		for i, a := range f.Args {
			ref := argRef(i)
			if i >= len(nodes) {
				if pass == 1 && a.Kind == "value" {
					ca.values[ref] = *a.Default
				}
				continue
			}
			v := nodes[i]
			var err error
			switch {
			case pass == 0 && a.Kind == "decoder":
				err = decoder(ref, a, v)
			case pass == 0 && a.Kind == "decoders":
				if v.Kind != jsontext.Array || len(v.Elems) == 0 {
					err = fmt.Errorf("%s must be a non-empty array of decoders", a.Name)
				}
				for j := 0; err == nil && j < len(v.Elems); j++ {
					err = decoder(ref, a, v.Elems[j])
				}
			case pass == 0 && a.Kind == "variants":
				if v.Kind != jsontext.Object || len(v.Members) == 0 {
					err = fmt.Errorf("%s must be a non-empty object of decoders", a.Name)
				}
				for j := 0; err == nil && j < len(v.Members); j++ {
					err = decoder(ref, a, v.Members[j].Value)
					ca.keys[ref] = append(ca.keys[ref], v.Members[j].Name)
				}
			case pass == 0 && a.Kind == "fields":
				if v.Kind != jsontext.Array || len(v.Elems) == 0 {
					err = fmt.Errorf("fields must be a non-empty array")
				}
				for j := 0; err == nil && j < len(v.Elems); j++ {
					var t value.Type
					var fl field
					if t, fl, err = s.field(v.Elems[j]); err == nil {
						ca.product = append(ca.product, t)
						ca.fields[ref] = append(ca.fields[ref], fl)
					}
				}
			case pass == 0 && a.Kind == "encoder":
				var t value.Type
				if t, err = s.encoder(v); err == nil && !value.Unify(a.Type, t, bound) {
					err = fmt.Errorf("%s: expected an encoder of %s, found one of %s", a.Name, a.Type.Subst(bound), t)
				}
			case pass == 0 && a.Kind == "properties":
				if v.Kind != jsontext.Array || len(v.Elems) == 0 {
					err = fmt.Errorf("properties must be a non-empty array")
				}
				for j := 0; err == nil && j < len(v.Elems); j++ {
					var t value.Type
					if t, err = s.property(v.Elems[j]); err == nil && !value.Unify(a.Type, t, bound) {
						err = fmt.Errorf("%s: expected properties that read %s, found one that reads %s", a.Name, a.Type.Subst(bound), t)
					}
				}
			case pass == 1 && a.Kind == "value":
				ca.values[ref], err = s.literal(a, v, bound)
			case pass == 1 && ref == f.Message:
				if v.Kind != jsontext.String {
					err = fmt.Errorf("%s must be a string", a.Name)
				}
				message := v.Text
				ca.message = &message
			case pass == 2 && a.Kind == "fixture":
				ca.fixtures[ref], err = s.fixture(a, v, bound)
			}
			if err != nil {
				return ca, err
			}
		}
	}
	for _, r := range f.Requires {
		if err := meets(f, r, ca.values); err != nil {
			return ca, err
		}
	}
	return ca, nil
}

// meets checks a condition on argument values.
func meets(f *Form, r Require, values map[ArgRef]value.Value) error {
	var vs []value.Value
	for _, i := range r.Args {
		vs = append(vs, values[i])
	}
	name := func(k int) string { return f.Args[r.Args[k].Index()].Name }
	switch r.Check {
	case "ordered":
		c, err := value.Compare(vs[0], vs[1])
		if err != nil {
			return err
		}
		if c > 0 {
			return fmt.Errorf("%s must not be after %s", name(0), name(1))
		}
	case "nonzero":
		if value.IsZero(vs[0]) {
			return fmt.Errorf("%s must not be zero", name(0))
		}
	case "nonempty":
		if len(vs[0].Elems) == 0 {
			return fmt.Errorf("%s must not be empty", name(0))
		}
	case "distinct_ascii_fold":
		seen := map[string]string{}
		for _, e := range vs[0].Elems {
			folded := asciiLower(e.Str)
			if other, dup := seen[folded]; dup {
				return fmt.Errorf("%s: %q and %q are the same under ASCII case folding", name(0), other, e.Str)
			}
			seen[folded] = e.Str
		}
	}
	return nil
}

func asciiLower(s string) string {
	return strings.Map(func(c rune) rune {
		if 'A' <= c && c <= 'Z' {
			return c - 'A' + 'a'
		}
		return c
	}, s)
}

func (s *state) literal(a Arg, n *jsontext.Node, bound map[string]value.Type) (value.Value, error) {
	t := a.Type.Subst(bound)
	if err := concrete(t, a.Name); err != nil {
		return value.Value{}, err
	}
	v, err := value.Observe(t, n)
	if err != nil {
		return value.Value{}, fmt.Errorf("%s: %w", a.Name, err)
	}
	if len(a.OneOf) > 0 && !slices.Contains(a.OneOf, v.Str) {
		return value.Value{}, fmt.Errorf("%s must be one of %s", a.Name, strings.Join(a.OneOf, ", "))
	}
	return v, nil
}

// fixture checks a fixture argument: the fixture it names, and the types its parameters take there.
func (s *state) fixture(a Arg, n *jsontext.Node, bound map[string]value.Type) (checkedFixture, error) {
	if n.Kind != jsontext.String {
		return checkedFixture{}, fmt.Errorf("%s must name a fixture", a.Name)
	}
	fx, ok := s.registry.Fixtures[n.Text]
	if !ok {
		return checkedFixture{}, fmt.Errorf("unknown fixture %q", n.Text)
	}
	if fx.Kind != a.FixtureKind {
		return checkedFixture{}, fmt.Errorf("%s needs a %s fixture, and %s is a %s fixture", a.Name, a.FixtureKind, fx.Name, fx.Kind)
	}
	s.features["fixture."+fx.Name] = true
	fb := map[string]value.Type{}
	in := a.FixtureInput.Subst(bound)
	inKnown := len(in.Params()) == 0
	if inKnown && !value.Unify(fx.Input, in, fb) {
		return checkedFixture{}, fmt.Errorf("fixture %s takes %s, not %s", fx.Name, fx.Input, in)
	}
	if a.FixtureOutput != nil {
		out := a.FixtureOutput.Subst(bound)
		if len(out.Params()) == 0 {
			if !value.Unify(*fx.Output, out, fb) {
				return checkedFixture{}, fmt.Errorf("fixture %s gives %s, not %s", fx.Name, *fx.Output, out)
			}
		} else {
			o := fx.Output.Subst(fb)
			if err := concrete(o, "fixture "+fx.Name); err != nil {
				return checkedFixture{}, err
			}
			if !value.Unify(out, o, bound) {
				return checkedFixture{}, fmt.Errorf("fixture %s gives %s, not %s", fx.Name, o, out)
			}
		}
	}
	if !inKnown {
		i := fx.Input.Subst(fb)
		if err := concrete(i, "fixture "+fx.Name); err != nil {
			return checkedFixture{}, err
		}
		if !value.Unify(in, i, bound) {
			return checkedFixture{}, fmt.Errorf("fixture %s takes %s, not %s", fx.Name, i, in)
		}
	}
	return checkedFixture{Fixture: fx, bound: fb}, nil
}

// site instantiates an issue reference with the types bound gives the form's parameters and the
// values its arguments give its metadata. NewChecker has checked that the catalogue has the
// variant, and that the reference binds its parameters and names only metadata it has.
func (s *state) site(ref IssueRef, bound map[string]value.Type, ca checkedArgs) (*Site, error) {
	v := s.catalog.Variants[ref.Key]
	args := map[string]value.Type{}
	for _, p := range v.Params {
		args[p] = ref.Bind[p].Subst(bound)
		if err := concrete(args[p], "issue "+ref.Key); err != nil {
			return nil, err
		}
	}
	meta, err := v.Instantiate(args)
	if err != nil {
		return nil, err
	}
	site := &Site{Key: v.Key, Code: v.Code, Meta: meta, Values: map[string]value.Value{}, Message: ca.message}
	for _, o := range ref.Omit {
		delete(meta, o)
	}
	for name, src := range ref.Meta {
		t := meta[name]
		if src.Kind == "member" {
			if t.Kind != value.String {
				return nil, fmt.Errorf("issue %s: %s is a member name, and a %s", ref.Key, name, t)
			}
			site.MemberMeta = append(site.MemberMeta, name)
			continue
		}
		val, err := metaValue(src, t, ca)
		if err != nil {
			return nil, fmt.Errorf("issue %s meta %s: %w", ref.Key, name, err)
		}
		site.Values[name] = val
	}
	for _, o := range v.Optional {
		if _, ok := meta[o]; ok {
			site.Optional = append(site.Optional, o)
		}
	}
	return site, nil
}

// candidatesType is the type of the metadata entry that lists the candidates of an issue.
var candidatesType = value.MustParseType("list<record<candidate:int32,issues:issues>>")

// Validate checks a registry against the issue catalogue: every issue a form declares is in it,
// with its parameters bound and nothing else, and every metadata entry the form omits or gives a
// source is one the variant has. An issue that lists candidates lists them in an entry of exactly
// the candidates' type that every such issue has.
func Validate(r *Registry, c *catalog.Catalog) error {
	var problems []string
	check := func(owner string, f *Form) {
		for _, ref := range f.Issues {
			v, ok := c.Variants[ref.Key]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: issue %s is not in the catalogue", owner, ref.Key))
				continue
			}
			for _, p := range v.Params {
				if _, ok := ref.Bind[p]; !ok {
					problems = append(problems, fmt.Sprintf("%s: issue %s does not bind %s", owner, ref.Key, p))
				}
			}
			for p := range ref.Bind {
				if !slices.Contains(v.Params, p) {
					problems = append(problems, fmt.Sprintf("%s: issue %s has no parameter %s to bind", owner, ref.Key, p))
				}
			}
			for name, src := range ref.Meta {
				t, ok := v.Meta[name]
				if !ok {
					problems = append(problems, fmt.Sprintf("%s: issue %s has no metadata %s to give a source", owner, ref.Key, name))
					continue
				}
				if err := sourceFits(f, src, t.Subst(ref.Bind)); err != nil {
					problems = append(problems, fmt.Sprintf("%s: issue %s meta %s: %v", owner, ref.Key, name, err))
				}
			}
			for _, name := range ref.Omit {
				if _, ok := v.Meta[name]; !ok {
					problems = append(problems, fmt.Sprintf("%s: issue %s has no metadata %s to omit", owner, ref.Key, name))
				}
			}
		}
		walkExpr(f.Flow, func(e Expr) {
			x, ok := e.(ExprCandidates)
			if !ok {
				return
			}
			ref := f.Issues[x.Issue.Index()]
			v, ok := c.Variants[ref.Key]
			if !ok {
				return
			}
			switch t, ok := v.Meta[x.Meta]; {
			case !ok:
				problems = append(problems, fmt.Sprintf("%s: issue %s has no metadata %s to list the candidates", owner, ref.Key, x.Meta))
			case !t.Same(candidatesType):
				problems = append(problems, fmt.Sprintf("%s: issue %s lists the candidates in %s, which is a %s, not a %s", owner, ref.Key, x.Meta, t, candidatesType))
			case slices.Contains(v.Optional, x.Meta):
				problems = append(problems, fmt.Sprintf("%s: issue %s may leave out %s, and an issue that lists candidates lists them all", owner, ref.Key, x.Meta))
			}
		})
	}
	for name, f := range r.Constructors {
		check("constructor "+name, f)
	}
	for name, f := range r.Fields {
		check("field "+name, f)
	}
	for name, overloads := range r.Operations {
		seen := map[*Form]bool{}
		for _, o := range overloads {
			if !seen[o.Form] {
				seen[o.Form] = true
				check("operation "+name, o.Form)
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	return nil
}

// sourceFits checks what of a metadata source the forms alone decide: a constant is an
// observation of the entry's type, and an argument has the entry's type, wherever neither
// mentions a type parameter a decoder binds.
func sourceFits(f *Form, src MetaSource, t value.Type) error {
	concrete := len(t.Params()) == 0
	switch src.Kind {
	case "const":
		if concrete {
			if _, err := value.Observe(t, src.Const); err != nil {
				return err
			}
		}
	case "const_by_type":
		for name, n := range src.ByType {
			bt, err := value.ParseType(name)
			if err != nil {
				return err
			}
			if _, err := value.Observe(bt, n); err != nil {
				return fmt.Errorf("the value for %s: %w", name, err)
			}
		}
		if concrete {
			if _, ok := src.ByType[t.String()]; !ok {
				return fmt.Errorf("const_by_type has no value for %s", t)
			}
		}
	case "arg", "sorted", "ascii_lower_sorted":
		at := f.Args[src.Arg.Index()].Type
		if concrete && len(at.Params()) == 0 && !at.Same(t) {
			return fmt.Errorf("argument %s is a %s, and the entry a %s", f.Args[src.Arg.Index()].Name, at, t)
		}
	case "sorted_keys":
		if t.Kind != value.List || t.Args[0].Kind != value.String {
			return fmt.Errorf("sorted_keys gives a list<string>, and the entry is a %s", t)
		}
	case "member":
		if concrete && t.Kind != value.String {
			return fmt.Errorf("the name of a member is a string, and the entry a %s", t)
		}
	}
	return nil
}

// metaValue computes the value a metadata source decides.
func metaValue(src MetaSource, t value.Type, ca checkedArgs) (value.Value, error) {
	var v value.Value
	var err error
	switch src.Kind {
	case "const":
		v, err = value.Observe(t, src.Const)
	case "const_by_type":
		n, ok := src.ByType[t.String()]
		if !ok {
			return value.Value{}, fmt.Errorf("const_by_type has no value for %s", t)
		}
		v, err = value.Observe(t, n)
	case "arg", "sorted", "ascii_lower_sorted":
		a := ca.values[src.Arg]
		v = a
		if src.Kind == "ascii_lower_sorted" {
			v = asciiLowered(a)
			v.Type = t
		}
		if src.Kind != "arg" {
			if err := sortElems(&v); err != nil {
				return value.Value{}, err
			}
		}
		if !v.Type.Same(t) {
			return value.Value{}, fmt.Errorf("the source is a %s, and the entry a %s", v.Type, t)
		}
	case "sorted_keys":
		if t.Kind != value.List || t.Args[0].Kind != value.String {
			return value.Value{}, fmt.Errorf("sorted_keys gives a list<string>, and the entry is a %s", t)
		}
		v = value.Value{Type: t}
		for _, k := range ca.keys[src.Arg] {
			v.Elems = append(v.Elems, value.Value{Type: t.Args[0], Str: k})
		}
		if err := sortElems(&v); err != nil {
			return value.Value{}, err
		}
	default:
		return value.Value{}, fmt.Errorf("%s is not a metadata source", src.Kind)
	}
	if err != nil {
		return value.Value{}, err
	}
	return v, nil
}

// asciiLowered returns a list of strings with A-Z read as a-z.
func asciiLowered(v value.Value) value.Value {
	out := value.Value{Type: v.Type, Elems: make([]value.Value, len(v.Elems))}
	for i, e := range v.Elems {
		out.Elems[i] = e
		out.Elems[i].Str = asciiLower(e.Str)
	}
	return out
}

// sortElems sorts a list in ascending order: strings by code point, numbers and temporal values as
// Compare orders them.
func sortElems(v *value.Value) error {
	if v.Type.Kind != value.List && v.Type.Kind != value.Set {
		return fmt.Errorf("only a list can be sorted, not a %s", v.Type)
	}
	elems := slices.Clone(v.Elems)
	var err error
	slices.SortStableFunc(elems, func(a, b value.Value) int {
		if a.Type.Kind == value.String {
			return strings.Compare(a.Str, b.Str)
		}
		c, e := value.Compare(a, b)
		if e != nil {
			err = e
		}
		return c
	})
	v.Elems = elems
	return err
}
