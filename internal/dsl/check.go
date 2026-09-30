package dsl

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/raoh-project/raoh-specification/internal/catalog"
	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/pattern"
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
	chk := &Checker{registry: r, catalog: c}
	if err := chk.checkEmbedded(); err != nil {
		return nil, err
	}
	return chk, nil
}

// checkEmbedded type-checks every form a flow embeds, so that building a flow never finds one
// wrong, and rejects a form that embeds itself, directly or through others, whose flow would never
// end.
func (c *Checker) checkEmbedded() error {
	var problems []string
	each := func(owner string, f *Form) {
		walkExpr(f.Flow, func(e Expr) {
			if x, ok := e.(ExprForm); ok {
				if _, _, err := c.newState().embedded(x); err != nil {
					problems = append(problems, fmt.Sprintf("%s: the form it embeds: %v", owner, err))
				}
			}
		})
	}
	for _, name := range slices.Sorted(maps.Keys(c.registry.Constructors)) {
		each("constructor "+name, c.registry.Constructors[name])
	}
	for _, name := range slices.Sorted(maps.Keys(c.registry.Fields)) {
		each("field "+name, c.registry.Fields[name])
	}
	for _, name := range slices.Sorted(maps.Keys(c.registry.Operations)) {
		seen := map[*Form]bool{}
		for _, key := range slices.Sorted(maps.Keys(c.registry.Operations[name])) {
			if f := c.registry.Operations[name][key].Form; !seen[f] {
				seen[f] = true
				each("operation "+name, f)
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	return nil
}

// embedded checks a form a flow embeds, recording none of its features: it is part of the meaning
// of the form that embeds it, not something a case uses.
func (s *state) embedded(x ExprForm) (value.Type, Flow, error) {
	if slices.Contains(s.embedding, x.Form) {
		return value.Type{}, nil, fmt.Errorf("%s embeds itself", abbreviate(x.Form))
	}
	features := s.features
	s.features, s.embedding = map[string]bool{}, append(s.embedding, x.Form)
	defer func() { s.features, s.embedding = features, s.embedding[:len(s.embedding)-1] }()
	return s.decoder(x.Form)
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
	// embedding are the forms embedded in a flow (ExprForm) being checked, innermost last.
	embedding []*jsontext.Node
}

func (c *Checker) newState() *state {
	return &state{Checker: c, features: map[string]bool{}}
}

func (s *state) done(result value.Type, flow Flow) *Checked {
	out := &Checked{Result: result, Flow: flow}
	for _, f := range slices.Sorted(maps.Keys(s.features)) {
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

// concrete checks that a type the checker computed is one values have (see value.Type.Concrete).
func concrete(t value.Type, what string) error {
	if err := t.Concrete(); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

// instantiate is the type of a value that a type written with parameters has for the bindings
// (see value.Type.Instantiate).
func instantiate(t value.Type, bound map[string]value.Type, what string) (value.Type, error) {
	u, err := t.Instantiate(bound)
	if err != nil {
		return u, fmt.Errorf("%s: %w", what, err)
	}
	return u, nil
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
	// members are the members the properties of a properties argument write, in order.
	members map[ArgRef][]string
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

// field is a checked field: its type, its flow, and the member it reads, if it names one.
type field struct {
	typ    value.Type
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
	result, err := resultOf(name, f.Result, bound, ca)
	if err != nil {
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
		panic("registry invariant: argument " + f.Args[x.Arg.Index()].Name + " of " + owner + " gives no flow")
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
					panic("registry invariant: " + owner + " takes its known members from fields it does not require to be named")
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
	case ExprForm:
		// NewChecker checks every embedded form through here, and refuses the catalogue when one
		// fails; once it has, none does.
		_, fl, err := s.embedded(x)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", owner, err)
		}
		return fl, nil
	case ExprFixture:
		fx := ca.fixtures[x.Arg]
		meta := map[string]value.Type{}
		for _, name := range slices.Sorted(maps.Keys(fx.Issue.Meta)) {
			t, err := instantiate(fx.Issue.Meta[name], fx.bound, "fixture "+fx.Name+" meta "+name)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", owner, err)
			}
			meta[name] = t
		}
		message := fx.Issue.Message
		var fl Flow = &Site{Key: fx.Issue.Key, Code: fx.Issue.Code, Meta: meta, Message: &message}
		for i := len(fx.Issue.Path) - 1; i >= 0; i-- {
			fl = &At{Name: fx.Issue.Path[i], Body: fl}
		}
		return &Alt{Items: []Flow{Success, fl}}, nil
	}
	panic(fmt.Sprintf("registry invariant: %s has a flow expression %T", owner, x))
}

// field checks a field form. A field whose flow is at a member reads that member.
func (s *state) field(n *jsontext.Node) (field, error) {
	name, err := head(n, "field")
	if err != nil {
		return field{}, err
	}
	f, ok := s.registry.Fields[name]
	if !ok {
		return field{}, fmt.Errorf("unknown field kind %q", name)
	}
	s.features["field."+name] = true
	if len(n.Elems)-1 != len(f.Args) {
		return field{}, fmt.Errorf("%s takes %d argument(s), found %d", name, len(f.Args), len(n.Elems)-1)
	}
	bound := map[string]value.Type{}
	ca, err := s.args(f, n.Elems[1:], bound)
	if err != nil {
		return field{}, fmt.Errorf("%s: %w", name, err)
	}
	result, err := resultOf(name, f.Result, bound, ca)
	if err != nil {
		return field{}, err
	}
	flow, err := s.build(name, f, f.Flow, bound, ca)
	if err != nil {
		return field{}, err
	}
	out := field{typ: result, flow: flow}
	if at, ok := f.Flow.(ExprAt); ok {
		out.member, out.named = ca.values[at.Member].Str, true
	}
	return out, nil
}

// resultOf is a form's result type for the types and values its arguments gave.
func resultOf(name string, r Result, bound map[string]value.Type, ca checkedArgs) (value.Type, error) {
	var t value.Type
	switch r := r.(type) {
	case ResultType:
		return instantiate(r.Type, bound, name)
	case ResultProduct:
		var types []value.Type
		for _, fl := range ca.fields[r.Fields] {
			types = append(types, fl.typ)
		}
		t = value.ProductOf(types...)
	case ResultSymbols:
		var alternatives []string
		for _, e := range ca.values[r.Symbols].Elems {
			alternatives = append(alternatives, e.Str)
		}
		t = value.SymbolOf(alternatives...)
	}
	return t, concrete(t, name)
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
		o, ok = overloads[ReceiverKey(receiver.Kind.String())]
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
	result, err := resultOf(name, f.Result, bound, ca)
	if err != nil {
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
	return instantiate(f.Input, bound, name)
}

// property checks a property form: the type it reads, and the member it writes.
func (s *state) property(n *jsontext.Node) (value.Type, string, error) {
	name, err := head(n, "property")
	if err != nil {
		return value.Type{}, "", err
	}
	f, ok := s.registry.Properties[name]
	if !ok {
		return value.Type{}, "", fmt.Errorf("unknown property kind %q", name)
	}
	s.features["property."+name] = true
	if len(n.Elems)-1 != len(f.Args) {
		return value.Type{}, "", fmt.Errorf("%s takes %d argument(s), found %d", name, len(f.Args), len(n.Elems)-1)
	}
	bound := map[string]value.Type{}
	ca, err := s.args(f, n.Elems[1:], bound)
	if err != nil {
		return value.Type{}, "", fmt.Errorf("%s: %w", name, err)
	}
	t, err := instantiate(f.Input, bound, name)
	return t, ca.values[f.Member].Str, err
}

// args checks the arguments given, the first of the form's, in the order their types are bound:
// first the arguments whose types a case's forms bind (decoders, fields, encoders, properties),
// then the fixtures, which bind each other's types until none is left to bind, then the values and
// the message, whose types have to be known by then, and last the conditions the form requires of
// them. A value argument left out stands for its default, so every value argument has a value in
// what args gives; a message argument left out gives no message.
func (s *state) args(f *Form, nodes []*jsontext.Node, bound map[string]value.Type) (checkedArgs, error) {
	ca := checkedArgs{values: map[ArgRef]value.Value{}, flows: map[ArgRef][]Flow{}, fields: map[ArgRef][]field{},
		keys: map[ArgRef][]string{}, members: map[ArgRef][]string{}, fixtures: map[ArgRef]checkedFixture{}}
	given := func(i int) bool { return i < len(nodes) }
	for i, a := range f.Args {
		if !given(i) || argKinds[a.Kind].Binding != bindsByForms {
			continue
		}
		if err := s.binder(a, argRef(i), nodes[i], bound, &ca); err != nil {
			return ca, err
		}
	}
	if err := s.fixtures(f, nodes, bound, &ca); err != nil {
		return ca, err
	}
	for i, a := range f.Args {
		ref := argRef(i)
		switch {
		case a.Kind == "value" && !given(i):
			ca.values[ref] = *a.Default
		case a.Kind == "value":
			v, err := s.literal(a, nodes[i], bound)
			if err != nil {
				return ca, err
			}
			ca.values[ref] = v
		case ref == f.Message && given(i):
			if nodes[i].Kind != jsontext.String {
				return ca, fmt.Errorf("%s must be a string", a.Name)
			}
			message := nodes[i].Text
			ca.message = &message
		}
	}
	for _, r := range f.Requires {
		if err := meets(f, r, ca); err != nil {
			return ca, err
		}
	}
	return ca, nil
}

// binder checks an argument whose type a case's forms bind: a decoder, decoders, variants,
// fields, encoder or properties argument. Other arguments bind nothing here.
func (s *state) binder(a Arg, ref ArgRef, v *jsontext.Node, bound map[string]value.Type, ca *checkedArgs) error {
	decoder := func(n *jsontext.Node) error {
		t, flow, err := s.decoder(n)
		if err != nil {
			return err
		}
		if !value.Unify(a.Type, t, bound) {
			return fmt.Errorf("%s: expected a decoder of %s, found one of %s", a.Name, a.Type.Subst(bound), t)
		}
		ca.flows[ref] = append(ca.flows[ref], flow)
		return nil
	}
	switch a.Kind {
	case "decoder":
		return decoder(v)
	case "decoders":
		if v.Kind != jsontext.Array || len(v.Elems) == 0 {
			return fmt.Errorf("%s must be a non-empty array of decoders", a.Name)
		}
		for _, e := range v.Elems {
			if err := decoder(e); err != nil {
				return err
			}
		}
	case "variants":
		if v.Kind != jsontext.Object || len(v.Members) == 0 {
			return fmt.Errorf("%s must be a non-empty object of decoders", a.Name)
		}
		for _, m := range v.Members {
			if err := decoder(m.Value); err != nil {
				return err
			}
			ca.keys[ref] = append(ca.keys[ref], m.Name)
		}
	case "fields":
		if v.Kind != jsontext.Array || len(v.Elems) == 0 {
			return fmt.Errorf("fields must be a non-empty array")
		}
		for _, e := range v.Elems {
			fl, err := s.field(e)
			if err != nil {
				return err
			}
			ca.fields[ref] = append(ca.fields[ref], fl)
		}
	case "encoder":
		t, err := s.encoder(v)
		if err != nil {
			return err
		}
		if !value.Unify(a.Type, t, bound) {
			return fmt.Errorf("%s: expected an encoder of %s, found one of %s", a.Name, a.Type.Subst(bound), t)
		}
	case "properties":
		if v.Kind != jsontext.Array || len(v.Elems) == 0 {
			return fmt.Errorf("properties must be a non-empty array")
		}
		for _, e := range v.Elems {
			t, member, err := s.property(e)
			if err != nil {
				return err
			}
			ca.members[ref] = append(ca.members[ref], member)
			if !value.Unify(a.Type, t, bound) {
				return fmt.Errorf("%s: expected properties that read %s, found one that reads %s", a.Name, a.Type.Subst(bound), t)
			}
		}
	}
	return nil
}

// fixtures resolves the fixture arguments given as one set of constraints: each type a fixture
// argument declares, in the form's bindings, has to match the fixture's own type, in that
// fixture's bindings. Every binding a match derives is kept, even when its fixture is not resolved
// yet, since another fixture may need it; the matches repeat until none adds a binding. Then every
// type on both sides has to be known, or the case does not give enough to tell them.
func (s *state) fixtures(f *Form, nodes []*jsontext.Node, bound map[string]value.Type, ca *checkedArgs) error {
	type port struct {
		what      string
		form, fix value.Type
	}
	type use struct {
		ref   ArgRef
		fx    *Fixture
		local map[string]value.Type
		ports []port
	}
	var uses []use
	for i, a := range f.Args {
		if argKinds[a.Kind].Binding != bindsByFixture || i >= len(nodes) {
			continue
		}
		fx, err := s.fixture(a, nodes[i])
		if err != nil {
			return err
		}
		// A fixture argument and the fixture have the same signature (see fixtureSignatures).
		u := use{ref: argRef(i), fx: fx, local: map[string]value.Type{}, ports: []port{{"takes", a.FixtureInput, fx.Input}}}
		if a.FixtureOutput != nil {
			u.ports = append(u.ports, port{"gives", *a.FixtureOutput, *fx.Output})
		}
		uses = append(uses, u)
	}
	for progress := true; progress; {
		progress = false
		for _, u := range uses {
			for _, p := range u.ports {
				r, added := value.Match(p.form, bound, p.fix, u.local)
				if r == value.Mismatch {
					return fmt.Errorf("fixture %s %s %s, not %s", u.fx.Name, p.what, p.fix.Subst(u.local), p.form.Subst(bound))
				}
				progress = progress || added
			}
		}
	}
	for _, u := range uses {
		for _, p := range u.ports {
			formT, fixT := p.form.Subst(bound), p.fix.Subst(u.local)
			if len(formT.Params()) > 0 || len(fixT.Params()) > 0 {
				return fmt.Errorf("%s: cannot tell the types of fixture %s", f.Args[u.ref.Index()].Name, u.fx.Name)
			}
			if _, err := p.form.Instantiate(bound); err != nil {
				return fmt.Errorf("fixture %s: %w", u.fx.Name, err)
			}
		}
		ca.fixtures[u.ref] = checkedFixture{Fixture: u.fx, bound: u.local}
	}
	return nil
}

// meets checks a condition on argument values.
func meets(f *Form, r Require, ca checkedArgs) error {
	var vs []value.Value
	for _, i := range r.Args {
		vs = append(vs, ca.values[i])
	}
	name := func(k int) string { return f.Args[r.Args[k].Index()].Name }
	switch r.Check {
	case "distinct":
		for i, e := range vs[0].Elems {
			for _, o := range vs[0].Elems[:i] {
				if value.Equal(e, o) {
					return fmt.Errorf("%s lists the same value twice", name(0))
				}
			}
		}
	case "pattern":
		if err := pattern.Read(vs[0].Str); err != nil {
			return fmt.Errorf("%s is not a pattern spec/pattern.md admits: %w", name(0), err)
		}
	case "named_fields":
		for _, fl := range ca.fields[r.Args[0]] {
			if !fl.named {
				return fmt.Errorf("%s: a field that reads the whole input leaves the members this form knows unknown", name(0))
			}
		}
	case "distinct_members":
		members := ca.members[r.Args[0]]
		for i, m := range members {
			if slices.Contains(members[:i], m) {
				return fmt.Errorf("%s: two properties write the member %q", name(0), m)
			}
		}
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
	t, err := instantiate(a.Type, bound, a.Name)
	if err != nil {
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

// fixture reads the fixture a fixture argument names, of the argument's kind.
func (s *state) fixture(a Arg, n *jsontext.Node) (*Fixture, error) {
	if n.Kind != jsontext.String {
		return nil, fmt.Errorf("%s must name a fixture", a.Name)
	}
	fx, ok := s.registry.Fixtures[n.Text]
	if !ok {
		return nil, fmt.Errorf("unknown fixture %q", n.Text)
	}
	if fx.Kind != a.FixtureKind {
		return nil, fmt.Errorf("%s needs a %s fixture, and %s is a %s fixture", a.Name, a.FixtureKind, fx.Name, fx.Kind)
	}
	s.features["fixture."+fx.Name] = true
	return fx, nil
}

// site instantiates an issue reference with the types bound gives the form's parameters and the
// values its arguments give its metadata. NewChecker has checked that the catalogue has the
// variant, that the reference binds its parameters and names only metadata it has, and that every
// source fits its entry.
func (s *state) site(ref IssueRef, bound map[string]value.Type, ca checkedArgs) (*Site, error) {
	v := s.catalog.Variants[ref.Key]
	args := map[string]value.Type{}
	for _, p := range v.Params {
		t, err := instantiate(ref.Bind[p], bound, "issue "+ref.Key)
		if err != nil {
			return nil, err
		}
		args[p] = t
	}
	meta, err := v.Instantiate(args)
	if err != nil {
		return nil, fmt.Errorf("issue %s: %w", ref.Key, err)
	}
	for _, name := range s.catalog.Written(ref.Key) {
		if t, ok := meta[name]; ok && !t.HasMessageForm() {
			return nil, fmt.Errorf("issue %s writes %s, a %s, into its message, and a message cannot write a %s", ref.Key, name, t, t)
		}
	}
	site := &Site{Key: v.Key, Code: v.Code, Meta: meta, Values: map[string]value.Value{}, Message: ca.message}
	for _, o := range ref.Omit {
		delete(meta, o)
	}
	for _, name := range slices.Sorted(maps.Keys(ref.Meta)) {
		if src := ref.Meta[name]; src.Kind == "member" {
			site.MemberMeta = append(site.MemberMeta, name)
		} else {
			site.Values[name] = metaValue(src, meta[name], ca)
		}
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

// Validate checks a registry against the issue catalogue, so that checking a form never finds
// them wrong. What a form decides by itself is checked once (checkIssues): every issue it declares
// is in the catalogue, with its parameters bound and nothing else, every metadata entry it omits
// or gives a source is one the variant has, and an issue that lists candidates lists them in an
// entry of exactly the candidates' type that it cannot leave out. What depends on the receiver an
// operation applies to is checked in the context of each receiver pattern (checkSources).
func Validate(r *Registry, c *catalog.Catalog) error {
	var problems []string
	form := func(owner string, f *Form, contexts []context) {
		if p := checkIssues(owner, f, c); len(p) > 0 {
			problems = append(problems, p...)
			return
		}
		problems = append(problems, checkSources(owner, f, c, contexts)...)
	}
	for _, name := range slices.Sorted(maps.Keys(r.Constructors)) {
		form("constructor "+name, r.Constructors[name], []context{{where: "constructor " + name}})
	}
	for _, name := range slices.Sorted(maps.Keys(r.Fields)) {
		form("field "+name, r.Fields[name], []context{{where: "field " + name}})
	}
	for _, name := range slices.Sorted(maps.Keys(r.Operations)) {
		overloads := r.Operations[name]
		var forms []*Form
		contexts := map[*Form][]context{}
		for _, key := range slices.Sorted(maps.Keys(overloads)) {
			o := overloads[key]
			if _, ok := contexts[o.Form]; !ok {
				forms = append(forms, o.Form)
			}
			contexts[o.Form] = append(contexts[o.Form], receiverContext("operation "+name, o.Receiver))
		}
		for _, f := range forms {
			form("operation "+name, f, contexts[f])
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	return nil
}

// context is what the form's parameters are known to be before a case applies it: R bound to an
// operation's receiver pattern, which may itself mention parameters (list<E>), or nothing; where
// names it in a problem.
type context struct {
	where string
	bound map[string]value.Type
}

func receiverContext(owner string, rc Receiver) context {
	if rc.Pattern == nil {
		return context{where: owner + " on any type"}
	}
	return context{where: owner + " on " + rc.Pattern.String(), bound: map[string]value.Type{"R": *rc.Pattern}}
}

// checkIssues checks what a form decides of its issues by itself.
func checkIssues(owner string, f *Form, c *catalog.Catalog) []string {
	var problems []string
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
		for _, p := range slices.Sorted(maps.Keys(ref.Bind)) {
			if !slices.Contains(v.Params, p) {
				problems = append(problems, fmt.Sprintf("%s: issue %s has no parameter %s to bind", owner, ref.Key, p))
			}
		}
		for _, name := range slices.Sorted(maps.Keys(ref.Meta)) {
			if _, ok := v.Meta[name]; !ok {
				problems = append(problems, fmt.Sprintf("%s: issue %s has no metadata %s to give a source", owner, ref.Key, name))
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
	return problems
}

// checkSources checks, in each context the form can be used in, the types of its issues'
// bindings and metadata and the metadata sources it gives. A type the context fixes has to be one
// values have. A source has to fit its entry for every type a case can give it: where the context
// fixes the entry's type, it is checked for that type, and where the type is left to the case, a
// source is accepted only if it fits whatever the case gives (an argument of the same type), never
// one that needs the type (a constant, a constant by type, a sort).
func checkSources(owner string, f *Form, c *catalog.Catalog, contexts []context) []string {
	var problems []string
	for _, ref := range f.Issues {
		v := c.Variants[ref.Key]
		byType := map[string][]string{}
		for _, ctx := range contexts {
			bad := func(format string, args ...any) {
				problems = append(problems, fmt.Sprintf("%s: issue %s ", ctx.where, ref.Key)+fmt.Sprintf(format, args...))
			}
			for _, p := range v.Params {
				if t := ref.Bind[p].Subst(ctx.bound); len(t.Params()) == 0 {
					if err := t.Concrete(); err != nil {
						bad("binds %s to %s: %v", p, t, err)
					}
				}
			}
			for _, name := range slices.Sorted(maps.Keys(v.Meta)) {
				t := v.Meta[name].Subst(ref.Bind).Subst(ctx.bound)
				known := len(t.Params()) == 0
				if known {
					if err := t.Concrete(); err != nil {
						bad("meta %s is a %s: %v", name, t, err)
						continue
					}
					if slices.Contains(c.Written(ref.Key), name) && !t.HasMessageForm() {
						bad("writes meta %s, a %s, into its message, and a message cannot write a %s", name, t, t)
					}
				}
				src, ok := ref.Meta[name]
				if !ok {
					continue
				}
				if err := sourceFits(f, src, t, ctx); err != nil {
					bad("meta %s: %v", name, err)
				}
				if src.Kind == "const_by_type" && known {
					byType[name] = append(byType[name], t.String())
				}
			}
		}
		for _, name := range slices.Sorted(maps.Keys(byType)) {
			reached := byType[name]
			keys := slices.Sorted(maps.Keys(ref.Meta[name].ByType))
			slices.Sort(reached)
			reached = slices.Compact(reached)
			if !slices.Equal(keys, reached) {
				problems = append(problems, fmt.Sprintf("%s: issue %s meta %s: const_by_type gives values for %v, and the entry can be %v", owner, ref.Key, name, keys, reached))
			}
		}
	}
	return problems
}

// sourceFits checks a metadata source against the type its entry has in a context.
func sourceFits(f *Form, src MetaSource, t value.Type, ctx context) error {
	known := len(t.Params()) == 0
	switch src.Kind {
	case "const":
		if !known {
			return fmt.Errorf("a constant for a %s, which a case decides", t)
		}
		if _, err := value.Observe(t, src.Const); err != nil {
			return err
		}
	case "const_by_type":
		if !known {
			return fmt.Errorf("const_by_type for a %s, which a case decides", t)
		}
		n, ok := src.ByType[t.String()]
		if !ok {
			return fmt.Errorf("const_by_type has no value for %s", t)
		}
		if _, err := value.Observe(t, n); err != nil {
			return fmt.Errorf("the value for %s: %w", t, err)
		}
	case "arg", "sorted", "ascii_lower_sorted":
		a := f.Args[src.Arg.Index()]
		if at := a.Type.Subst(ctx.bound); !at.Same(t) {
			return fmt.Errorf("argument %s is a %s, and the entry a %s", a.Name, at, t)
		}
		if src.Kind == "sorted" {
			if e := t.Args[0]; len(e.Params()) > 0 || !(e.Kind == value.String || e.IsNumeric() || e.IsTemporal()) {
				return fmt.Errorf("sorts a %s, whose elements have no order it can tell", t)
			}
		}
	case "sorted_keys":
		if !t.Same(value.MustParseType("list<string>")) {
			return fmt.Errorf("sorted_keys gives a list<string>, and the entry is a %s", t)
		}
	case "member":
		if !t.Same(value.Of(value.String)) {
			return fmt.Errorf("the name of a member is a string, and the entry a %s", t)
		}
	}
	return nil
}

// metaValue computes the value a metadata source decides. Validate has checked that the source fits
// the entry for every type a case can give it, so nothing here can fail.
func metaValue(src MetaSource, t value.Type, ca checkedArgs) value.Value {
	var v value.Value
	var err error
	switch src.Kind {
	case "const":
		v, err = value.Observe(t, src.Const)
	case "const_by_type":
		v, err = value.Observe(t, src.ByType[t.String()])
	case "arg":
		v = ca.values[src.Arg]
	case "sorted":
		v = sortElems(ca.values[src.Arg])
	case "ascii_lower_sorted":
		v = sortElems(asciiLowered(ca.values[src.Arg]))
	case "sorted_keys":
		v = value.Value{Type: t}
		for _, k := range ca.keys[src.Arg] {
			v.Elems = append(v.Elems, value.Value{Type: t.Args[0], Str: k})
		}
		v = sortElems(v)
	default:
		panic("registry invariant: metadata source " + src.Kind)
	}
	if err != nil || !v.Type.Same(t) {
		panic(fmt.Sprintf("registry invariant: the %s source of a %s gives %s (%v)", src.Kind, t, v.Type, err))
	}
	return v
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
// Compare orders them. Validate has checked that the elements have that order.
func sortElems(v value.Value) value.Value {
	elems := slices.Clone(v.Elems)
	slices.SortStableFunc(elems, func(a, b value.Value) int {
		if a.Type.Kind == value.String {
			return strings.Compare(a.Str, b.Str)
		}
		c, err := value.Compare(a, b)
		if err != nil {
			panic("registry invariant: " + err.Error())
		}
		return c
	})
	v.Elems = elems
	return v
}
