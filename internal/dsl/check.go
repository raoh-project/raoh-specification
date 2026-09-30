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
	// Flow is the issues a decoder can give; empty for an encoder.
	Flow Flow
	// Sites are the sites of Flow, in order.
	Sites []Located
}

// Checker type-checks forms against a registry and an issue catalogue.
type Checker struct {
	Registry *Registry
	Catalog  *catalog.Catalog
}

type state struct {
	*Checker
	features map[string]bool
}

func (c *Checker) newState() *state {
	return &state{Checker: c, features: map[string]bool{}}
}

func (s *state) done(result value.Type, flow Flow) *Checked {
	out := &Checked{Result: result, Flow: flow, Sites: Sites(flow)}
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
	return s.done(t, Seq{}), nil
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

// checkedArgs is what checking a form's arguments gives besides the types it binds.
type checkedArgs struct {
	values map[string]value.Value
	// flows are the flows of the decoder arguments, by argument name, in the order of the
	// decoders within the argument.
	flows map[string][]Flow
	// fixtures are the fixtures the arguments name.
	fixtures []*Fixture
	// fields are the flows of an object's fields and product their types.
	fields  []Flow
	product []value.Type
	message string
}

func (s *state) decoder(n *jsontext.Node) (value.Type, Flow, error) {
	name, err := head(n, "decoder")
	if err != nil {
		return value.Type{}, nil, err
	}
	f, ok := s.Registry.Constructors[name]
	if !ok {
		return value.Type{}, nil, fmt.Errorf("unknown constructor %q", name)
	}
	s.features["decoder."+name] = true
	if len(n.Elems)-1 < len(f.Args) {
		return value.Type{}, nil, fmt.Errorf("%s takes %d argument(s), found %d", name, len(f.Args), len(n.Elems)-1)
	}
	bound := map[string]value.Type{}
	ca, err := s.args(f, f.Args, n.Elems[1:1+len(f.Args)], bound)
	if err != nil {
		return value.Type{}, nil, fmt.Errorf("%s: %w", name, err)
	}
	result := f.Result.Subst(bound)
	if f.Result.Kind == value.Product && len(f.Result.Args) == 0 {
		result = value.ProductOf(ca.product...)
	}
	if f.SymbolsFrom != "" {
		var alternatives []string
		for _, e := range ca.values[f.SymbolsFrom].Elems {
			alternatives = append(alternatives, e.Str)
		}
		result = value.SymbolOf(alternatives...)
	}
	if err := concrete(result, name); err != nil {
		return value.Type{}, nil, err
	}
	flow, err := s.formFlow(name, f, bound, ca)
	if err != nil {
		return value.Type{}, nil, err
	}
	for _, step := range n.Elems[1+len(f.Args):] {
		var stepFlow Flow
		if result, stepFlow, err = s.operation(step, result); err != nil {
			return value.Type{}, nil, err
		}
		flow = Seq{Items: []Flow{flow, stepFlow}}
	}
	return result, flow, nil
}

// formFlow builds the flow of a constructor or operation: the flows of its decoder arguments, as
// each argument's placement says, then its own issues, then the issues of the fixtures it names.
func (s *state) formFlow(owner string, f *Form, bound map[string]value.Type, ca checkedArgs) (Flow, error) {
	var items []Flow
	var candidates []Flow
	items = append(items, ca.fields...)
	for _, a := range f.Args {
		for _, fl := range ca.flows[a.Name] {
			switch a.Flows {
			case FlowsHere:
				items = append(items, fl)
			case FlowsEachElement:
				items = append(items, Repeat{Body: fl})
			case FlowsCandidates:
				candidates = append(candidates, fl)
			case FlowsNone:
			}
		}
	}
	var members []Flow
	for _, ref := range f.Issues {
		site, err := s.site(ref, bound)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", owner, err)
		}
		site.Message = ca.message
		if ref.Key == "one_of_failed" {
			site.Candidates = candidates
		}
		switch ref.At {
		case PlaceHere:
			items = append(items, site)
		case PlaceTag:
			tag, ok := ca.values["field"]
			if !ok {
				return nil, fmt.Errorf("%s: an issue at the tag field needs the argument field", owner)
			}
			items = append(items, At{Name: tag.Str, Body: site})
		case PlaceMember:
			members = append(members, Repeat{Body: site})
		}
	}
	if len(members) > 0 {
		var m Flow = Seq{Items: members}
		if f.InputOrder {
			m = Unordered{Body: m}
		}
		items = append(items, m)
	}
	for _, fx := range ca.fixtures {
		if fx.Issue == nil {
			continue
		}
		var fl Flow = &Site{Key: fx.Issue.Key, Code: fx.Issue.Code, Meta: fx.Issue.Meta, Message: fx.Issue.Message}
		for i := len(fx.Issue.Path) - 1; i >= 0; i-- {
			fl = At{Name: fx.Issue.Path[i], Body: fl}
		}
		items = append(items, fl)
	}
	return Seq{Items: items}, nil
}

func (s *state) field(n *jsontext.Node) (value.Type, Flow, error) {
	name, err := head(n, "field")
	if err != nil {
		return value.Type{}, nil, err
	}
	f, ok := s.Registry.Fields[name]
	if !ok {
		return value.Type{}, nil, fmt.Errorf("unknown field kind %q", name)
	}
	s.features["field."+name] = true
	if len(n.Elems)-1 != len(f.Args) {
		return value.Type{}, nil, fmt.Errorf("%s takes %d argument(s), found %d", name, len(f.Args), len(n.Elems)-1)
	}
	bound := map[string]value.Type{}
	ca, err := s.args(f, f.Args, n.Elems[1:], bound)
	if err != nil {
		return value.Type{}, nil, fmt.Errorf("%s: %w", name, err)
	}
	result := f.Result.Subst(bound)
	if err := concrete(result, name); err != nil {
		return value.Type{}, nil, err
	}
	flow, err := s.formFlow(name, f, bound, ca)
	if err != nil {
		return value.Type{}, nil, err
	}
	// A field that names a member gives all its issues at that member's path.
	if member, ok := ca.values["name"]; ok {
		flow = At{Name: member.Str, Body: flow}
	}
	return result, flow, nil
}

func (s *state) operation(n *jsontext.Node, receiver value.Type) (value.Type, Flow, error) {
	name, err := head(n, "operation")
	if err != nil {
		return value.Type{}, nil, err
	}
	forms, ok := s.Registry.Operations[name]
	if !ok {
		return value.Type{}, nil, fmt.Errorf("unknown operation %q", name)
	}
	var f *Form
	var pattern string
	bound := map[string]value.Type{}
	for _, cand := range forms {
		for _, rc := range cand.Receivers {
			b := map[string]value.Type{}
			if rc == "*" || value.Unify(value.MustParseType(rc), receiver, b) {
				f, pattern, bound = cand, rc, b
				break
			}
		}
		if f != nil {
			break
		}
	}
	if f == nil {
		return value.Type{}, nil, fmt.Errorf("operation %s does not apply to %s", name, receiver)
	}
	s.features[operationFeature(pattern, name)] = true
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
	ca, err := s.args(f, f.Args[:len(given)], given, bound)
	if err != nil {
		return value.Type{}, nil, fmt.Errorf("%s: %w", name, err)
	}
	result := f.Result.Subst(bound)
	if err := concrete(result, name); err != nil {
		return value.Type{}, nil, err
	}
	flow, err := s.formFlow(name, f, bound, ca)
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
	f, ok := s.Registry.Encoders[name]
	if !ok {
		return value.Type{}, fmt.Errorf("unknown encoder %q", name)
	}
	s.features["encoder."+name] = true
	if len(n.Elems)-1 != len(f.Args) {
		return value.Type{}, fmt.Errorf("%s takes %d argument(s), found %d", name, len(f.Args), len(n.Elems)-1)
	}
	bound := map[string]value.Type{}
	if _, err := s.args(f, f.Args, n.Elems[1:], bound); err != nil {
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
	f, ok := s.Registry.Properties[name]
	if !ok {
		return value.Type{}, fmt.Errorf("unknown property kind %q", name)
	}
	s.features["property."+name] = true
	if len(n.Elems)-1 != len(f.Args) {
		return value.Type{}, fmt.Errorf("%s takes %d argument(s), found %d", name, len(f.Args), len(n.Elems)-1)
	}
	bound := map[string]value.Type{}
	if _, err := s.args(f, f.Args, n.Elems[1:], bound); err != nil {
		return value.Type{}, fmt.Errorf("%s: %w", name, err)
	}
	t, ok := bound["T"]
	if !ok {
		return value.Type{}, fmt.Errorf("%s: cannot tell what the property reads", name)
	}
	return t, nil
}

// args checks arguments in three passes, so that the types that decoders and encoders fix are
// known when values are read and fixtures are matched, and then the conditions the form
// requires of them.
func (s *state) args(f *Form, args []Arg, nodes []*jsontext.Node, bound map[string]value.Type) (checkedArgs, error) {
	ca := checkedArgs{values: map[string]value.Value{}, flows: map[string][]Flow{}}
	decoder := func(a Arg, n *jsontext.Node) error {
		t, flow, err := s.decoder(n)
		if err != nil {
			return err
		}
		if !value.Unify(a.Type, t, bound) {
			return fmt.Errorf("%s: expected a decoder of %s, found one of %s", a.Name, a.Type.Subst(bound), t)
		}
		ca.flows[a.Name] = append(ca.flows[a.Name], flow)
		return nil
	}
	for pass := 0; pass < 3; pass++ {
		for i, a := range args {
			v := nodes[i]
			var err error
			switch {
			case pass == 0 && a.Kind == "decoder":
				err = decoder(a, v)
			case pass == 0 && a.Kind == "decoders":
				if v.Kind != jsontext.Array || len(v.Elems) == 0 {
					err = fmt.Errorf("%s must be a non-empty array of decoders", a.Name)
				}
				for j := 0; err == nil && j < len(v.Elems); j++ {
					err = decoder(a, v.Elems[j])
				}
			case pass == 0 && a.Kind == "variants":
				if v.Kind != jsontext.Object || len(v.Members) == 0 {
					err = fmt.Errorf("%s must be a non-empty object of decoders", a.Name)
				}
				for j := 0; err == nil && j < len(v.Members); j++ {
					err = decoder(a, v.Members[j].Value)
				}
			case pass == 0 && a.Kind == "fields":
				if v.Kind != jsontext.Array || len(v.Elems) == 0 {
					err = fmt.Errorf("fields must be a non-empty array")
				}
				for j := 0; err == nil && j < len(v.Elems); j++ {
					var t value.Type
					var flow Flow
					if t, flow, err = s.field(v.Elems[j]); err == nil {
						ca.product = append(ca.product, t)
						ca.fields = append(ca.fields, flow)
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
					if t, err = s.property(v.Elems[j]); err == nil && !value.Unify(value.Type{Kind: value.Param, Name: "T"}, t, bound) {
						err = fmt.Errorf("the properties read values of different types: %s and %s", bound["T"], t)
					}
				}
			case pass == 1 && a.Kind == "value":
				ca.values[a.Name], err = s.literal(a, v, bound)
			case pass == 1 && a.Kind == "message":
				if v.Kind != jsontext.String {
					err = fmt.Errorf("%s must be a string", a.Name)
				}
				ca.message = v.Text
			case pass == 2 && a.Kind == "fixture":
				var fx *Fixture
				if fx, err = s.fixture(a, v, bound); err == nil {
					ca.fixtures = append(ca.fixtures, fx)
				}
			}
			if err != nil {
				return ca, err
			}
		}
	}
	for _, r := range f.Requires {
		if err := meets(r, ca.values); err != nil {
			return ca, err
		}
	}
	return ca, nil
}

// meets checks a condition on argument values. An argument left out is not checked.
func meets(r Require, values map[string]value.Value) error {
	var vs []value.Value
	for _, name := range r.Args {
		v, ok := values[name]
		if !ok {
			return nil
		}
		vs = append(vs, v)
	}
	switch r.Check {
	case "ordered":
		c, err := value.Compare(vs[0], vs[1])
		if err != nil {
			return err
		}
		if c > 0 {
			return fmt.Errorf("%s must not be after %s", r.Args[0], r.Args[1])
		}
	case "nonzero":
		if value.IsZero(vs[0]) {
			return fmt.Errorf("%s must not be zero", r.Args[0])
		}
	case "nonempty":
		if len(vs[0].Elems) == 0 {
			return fmt.Errorf("%s must not be empty", r.Args[0])
		}
	case "distinct_ascii_fold":
		seen := map[string]string{}
		for _, e := range vs[0].Elems {
			folded := strings.Map(func(c rune) rune {
				if 'A' <= c && c <= 'Z' {
					return c - 'A' + 'a'
				}
				return c
			}, e.Str)
			if other, dup := seen[folded]; dup {
				return fmt.Errorf("%s: %q and %q are the same under ASCII case folding", r.Args[0], other, e.Str)
			}
			seen[folded] = e.Str
		}
	}
	return nil
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

func (s *state) fixture(a Arg, n *jsontext.Node, bound map[string]value.Type) (*Fixture, error) {
	if n.Kind != jsontext.String {
		return nil, fmt.Errorf("%s must name a fixture", a.Name)
	}
	fx, ok := s.Registry.Fixtures[n.Text]
	if !ok {
		return nil, fmt.Errorf("unknown fixture %q", n.Text)
	}
	if fx.Kind != a.FixtureKind {
		return nil, fmt.Errorf("%s needs a %s fixture, and %s is a %s fixture", a.Name, a.FixtureKind, fx.Name, fx.Kind)
	}
	s.features["fixture."+fx.Name] = true
	fb := map[string]value.Type{}
	in := a.FixtureInput.Subst(bound)
	inKnown := len(in.Params()) == 0
	if inKnown && !value.Unify(fx.Input, in, fb) {
		return nil, fmt.Errorf("fixture %s takes %s, not %s", fx.Name, fx.Input, in)
	}
	if a.FixtureOutput != nil {
		out := a.FixtureOutput.Subst(bound)
		if len(out.Params()) == 0 {
			if !value.Unify(*fx.Output, out, fb) {
				return nil, fmt.Errorf("fixture %s gives %s, not %s", fx.Name, *fx.Output, out)
			}
		} else {
			o := fx.Output.Subst(fb)
			if err := concrete(o, "fixture "+fx.Name); err != nil {
				return nil, err
			}
			if !value.Unify(out, o, bound) {
				return nil, fmt.Errorf("fixture %s gives %s, not %s", fx.Name, o, out)
			}
		}
	}
	if !inKnown {
		i := fx.Input.Subst(fb)
		if err := concrete(i, "fixture "+fx.Name); err != nil {
			return nil, err
		}
		if !value.Unify(in, i, bound) {
			return nil, fmt.Errorf("fixture %s takes %s, not %s", fx.Name, i, in)
		}
	}
	return fx, nil
}

// site instantiates an issue reference with the types bound gives the form's parameters.
func (s *state) site(ref IssueRef, bound map[string]value.Type) (*Site, error) {
	v, ok := s.Catalog.Variants[ref.Key]
	if !ok {
		return nil, fmt.Errorf("issue %s is not in the catalogue", ref.Key)
	}
	args := map[string]value.Type{}
	for _, p := range v.Params {
		b, ok := ref.Bind[p]
		if !ok {
			return nil, fmt.Errorf("issue %s: %s is not bound", ref.Key, p)
		}
		args[p] = b.Subst(bound)
		if err := concrete(args[p], "issue "+ref.Key); err != nil {
			return nil, err
		}
	}
	meta, err := v.Instantiate(args)
	if err != nil {
		return nil, err
	}
	site := &Site{Key: v.Key, Code: v.Code, Meta: meta, Fixed: ref.Fixed}
	for name, n := range ref.Fixed {
		t, ok := meta[name]
		if !ok {
			return nil, fmt.Errorf("issue %s has no metadata %s to fix", ref.Key, name)
		}
		if _, err := value.Observe(t, n); err != nil {
			return nil, fmt.Errorf("issue %s: fixed %s: %w", ref.Key, name, err)
		}
	}
	for _, o := range ref.Omit {
		if _, ok := meta[o]; !ok {
			return nil, fmt.Errorf("issue %s has no metadata %s to omit", ref.Key, o)
		}
		delete(meta, o)
	}
	for _, o := range v.Optional {
		if _, ok := meta[o]; ok {
			site.Optional = append(site.Optional, o)
		}
	}
	return site, nil
}

// Validate checks that every issue a form declares is in the catalogue with every parameter
// bound, and that an issue placed at the tag field belongs to a form with a field argument.
func (c *Checker) Validate() error {
	var problems []string
	check := func(owner string, f *Form) {
		for _, ref := range f.Issues {
			v, ok := c.Catalog.Variants[ref.Key]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: issue %s is not in the catalogue", owner, ref.Key))
				continue
			}
			for _, p := range v.Params {
				if _, ok := ref.Bind[p]; !ok {
					problems = append(problems, fmt.Sprintf("%s: issue %s does not bind %s", owner, ref.Key, p))
				}
			}
			if ref.At == PlaceTag && !slices.ContainsFunc(f.Args, func(a Arg) bool { return a.Name == "field" && a.Kind == "value" }) {
				problems = append(problems, fmt.Sprintf("%s: issue %s is at the tag field, and the form has no field argument", owner, ref.Key))
			}
		}
	}
	for name, f := range c.Registry.Constructors {
		check("constructor "+name, f)
	}
	for name, f := range c.Registry.Fields {
		check("field "+name, f)
	}
	for name, forms := range c.Registry.Operations {
		for _, f := range forms {
			check("operation "+name, f)
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	return nil
}
