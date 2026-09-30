package dsl

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/raoh-project/raoh-specification/catalog"
	"github.com/raoh-project/raoh-specification/jsontext"
	"github.com/raoh-project/raoh-specification/value"
)

// Possible is an issue a decoder can give.
type Possible struct {
	Key  string
	Code string
	// Meta is the type of each metadata entry the issue can have.
	Meta map[string]value.Type
	// Optional are the entries it may leave out.
	Optional []string
	// Message is the message a fixture gives its issue; empty when the message is derived or given
	// by a message argument.
	Message string
}

// Checked is what type-checking a decoder or encoder form finds.
type Checked struct {
	// Result is the result type of a decoder, and the input type of an encoder.
	Result value.Type
	// Features are the feature IDs the form needs, sorted.
	Features []string
	// Issues are the issues a decoder can give, one per distinct variant and metadata typing.
	Issues []Possible
	// InputOrder is set when the decoder contains a form whose issues come in the order of the
	// input's members, so that its issues are compared as a multiset.
	InputOrder bool
}

// Checker type-checks forms against a registry and an issue catalogue.
type Checker struct {
	Registry *Registry
	Catalog  *catalog.Catalog
}

type state struct {
	*Checker
	features   map[string]bool
	issues     []Possible
	inputOrder bool
}

func (c *Checker) newState() *state {
	return &state{Checker: c, features: map[string]bool{}}
}

func (s *state) done(result value.Type) *Checked {
	out := &Checked{Result: result, Issues: s.issues, InputOrder: s.inputOrder}
	for f := range s.features {
		out.Features = append(out.Features, f)
	}
	sort.Strings(out.Features)
	return out
}

// CheckDecoder type-checks a decoder form.
func (c *Checker) CheckDecoder(n *jsontext.Node) (*Checked, error) {
	s := c.newState()
	t, err := s.decoder(n)
	if err != nil {
		return nil, err
	}
	return s.done(t), nil
}

// CheckEncoder type-checks an encoder form.
func (c *Checker) CheckEncoder(n *jsontext.Node) (*Checked, error) {
	s := c.newState()
	t, err := s.encoder(n)
	if err != nil {
		return nil, err
	}
	return s.done(t), nil
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
	return nil
}

func (s *state) decoder(n *jsontext.Node) (value.Type, error) {
	name, err := head(n, "decoder")
	if err != nil {
		return value.Type{}, err
	}
	f, ok := s.Registry.Constructors[name]
	if !ok {
		return value.Type{}, fmt.Errorf("unknown constructor %q", name)
	}
	s.features["decoder."+name] = true
	if len(n.Elems)-1 < len(f.Args) {
		return value.Type{}, fmt.Errorf("%s takes %d argument(s), found %d", name, len(f.Args), len(n.Elems)-1)
	}
	mark := len(s.issues)
	bound := map[string]value.Type{}
	var product []value.Type
	err = s.args(name, f.Args, n.Elems[1:1+len(f.Args)], bound, func(a Arg, v *jsontext.Node) error {
		if a.Kind != "fields" {
			return fmt.Errorf("unexpected argument kind %s", a.Kind)
		}
		if v.Kind != jsontext.Array || len(v.Elems) == 0 {
			return fmt.Errorf("fields must be a non-empty array")
		}
		for _, fn := range v.Elems {
			t, err := s.field(fn)
			if err != nil {
				return err
			}
			product = append(product, t)
		}
		return nil
	})
	if err != nil {
		return value.Type{}, fmt.Errorf("%s: %w", name, err)
	}
	if f.ReplacesIssues {
		s.issues = s.issues[:mark]
	}
	if f.InputOrder {
		s.inputOrder = true
	}
	result := f.Result.Subst(bound)
	if f.Result.Kind == value.Product && len(f.Result.Args) == 0 {
		result = value.ProductOf(product...)
	}
	if err := concrete(result, name); err != nil {
		return value.Type{}, err
	}
	if err := s.addIssues(name, f.Issues, bound); err != nil {
		return value.Type{}, err
	}
	for _, step := range n.Elems[1+len(f.Args):] {
		if result, err = s.operation(step, result); err != nil {
			return value.Type{}, err
		}
	}
	return result, nil
}

func (s *state) field(n *jsontext.Node) (value.Type, error) {
	name, err := head(n, "field")
	if err != nil {
		return value.Type{}, err
	}
	f, ok := s.Registry.Fields[name]
	if !ok {
		return value.Type{}, fmt.Errorf("unknown field kind %q", name)
	}
	s.features["field."+name] = true
	if len(n.Elems)-1 != len(f.Args) {
		return value.Type{}, fmt.Errorf("%s takes %d argument(s), found %d", name, len(f.Args), len(n.Elems)-1)
	}
	bound := map[string]value.Type{}
	if err := s.args(name, f.Args, n.Elems[1:], bound, nil); err != nil {
		return value.Type{}, fmt.Errorf("%s: %w", name, err)
	}
	result := f.Result.Subst(bound)
	return result, concrete(result, name)
}

func (s *state) operation(n *jsontext.Node, receiver value.Type) (value.Type, error) {
	name, err := head(n, "operation")
	if err != nil {
		return value.Type{}, err
	}
	forms, ok := s.Registry.Operations[name]
	if !ok {
		return value.Type{}, fmt.Errorf("unknown operation %q", name)
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
		return value.Type{}, fmt.Errorf("operation %s does not apply to %s", name, receiver)
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
		return value.Type{}, fmt.Errorf("%s takes %d to %d argument(s), found %d", name, required, len(f.Args), len(given))
	}
	if err := s.args(name, f.Args[:len(given)], given, bound, nil); err != nil {
		return value.Type{}, fmt.Errorf("%s: %w", name, err)
	}
	result := f.Result.Subst(bound)
	if err := concrete(result, name); err != nil {
		return value.Type{}, err
	}
	if err := s.addIssues(name, f.Issues, bound); err != nil {
		return value.Type{}, err
	}
	return result, nil
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
	err = s.args(name, f.Args, n.Elems[1:], bound, func(a Arg, v *jsontext.Node) error {
		if a.Kind != "properties" {
			return fmt.Errorf("unexpected argument kind %s", a.Kind)
		}
		if v.Kind != jsontext.Array || len(v.Elems) == 0 {
			return fmt.Errorf("properties must be a non-empty array")
		}
		for _, pn := range v.Elems {
			t, err := s.property(pn)
			if err != nil {
				return err
			}
			if !value.Unify(value.Type{Kind: value.Param, Name: "T"}, t, bound) {
				return fmt.Errorf("the properties read values of different types: %s and %s", bound["T"], t)
			}
		}
		return nil
	})
	if err != nil {
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
	if err := s.args(name, f.Args, n.Elems[1:], bound, nil); err != nil {
		return value.Type{}, fmt.Errorf("%s: %w", name, err)
	}
	t, ok := bound["T"]
	if !ok {
		return value.Type{}, fmt.Errorf("%s: cannot tell what the property reads", name)
	}
	return t, nil
}

// args checks arguments in three passes, so that the types that decoders and encoders fix are
// known when values are read and fixtures are matched.
func (s *state) args(owner string, args []Arg, nodes []*jsontext.Node, bound map[string]value.Type, other func(Arg, *jsontext.Node) error) error {
	for pass := 0; pass < 3; pass++ {
		for i, a := range args {
			v := nodes[i]
			var err error
			switch {
			case pass == 0 && a.Kind == "decoder":
				err = s.bindDecoder(a.Type, v, bound)
			case pass == 0 && a.Kind == "decoders":
				if v.Kind != jsontext.Array || len(v.Elems) == 0 {
					err = fmt.Errorf("%s must be a non-empty array of decoders", a.Name)
				}
				for j := 0; err == nil && j < len(v.Elems); j++ {
					err = s.bindDecoder(a.Type, v.Elems[j], bound)
				}
			case pass == 0 && a.Kind == "variants":
				if v.Kind != jsontext.Object || len(v.Members) == 0 {
					err = fmt.Errorf("%s must be a non-empty object of decoders", a.Name)
				}
				for j := 0; err == nil && j < len(v.Members); j++ {
					err = s.bindDecoder(a.Type, v.Members[j].Value, bound)
				}
			case pass == 0 && a.Kind == "encoder":
				var t value.Type
				if t, err = s.encoder(v); err == nil && !value.Unify(a.Type, t, bound) {
					err = fmt.Errorf("%s: expected an encoder of %s, found one of %s", a.Name, a.Type.Subst(bound), t)
				}
			case pass == 0 && (a.Kind == "fields" || a.Kind == "properties"):
				err = other(a, v)
			case pass == 1 && a.Kind == "value":
				err = s.literal(a, v, bound)
			case pass == 1 && a.Kind == "message":
				if v.Kind != jsontext.String {
					err = fmt.Errorf("%s must be a string", a.Name)
				}
			case pass == 2 && a.Kind == "fixture":
				err = s.fixture(a, v, bound)
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *state) bindDecoder(want value.Type, n *jsontext.Node, bound map[string]value.Type) error {
	t, err := s.decoder(n)
	if err != nil {
		return err
	}
	if !value.Unify(want, t, bound) {
		return fmt.Errorf("expected a decoder of %s, found one of %s", want.Subst(bound), t)
	}
	return nil
}

func (s *state) literal(a Arg, n *jsontext.Node, bound map[string]value.Type) error {
	t := a.Type.Subst(bound)
	if err := concrete(t, a.Name); err != nil {
		return err
	}
	v, err := value.Observe(t, n)
	if err != nil {
		return fmt.Errorf("%s: %w", a.Name, err)
	}
	if len(a.OneOf) > 0 && !slices.Contains(a.OneOf, v.Str) {
		return fmt.Errorf("%s must be one of %s", a.Name, strings.Join(a.OneOf, ", "))
	}
	return nil
}

func (s *state) fixture(a Arg, n *jsontext.Node, bound map[string]value.Type) error {
	if n.Kind != jsontext.String {
		return fmt.Errorf("%s must name a fixture", a.Name)
	}
	fx, ok := s.Registry.Fixtures[n.Text]
	if !ok {
		return fmt.Errorf("unknown fixture %q", n.Text)
	}
	if fx.Kind != a.FixtureKind {
		return fmt.Errorf("%s needs a %s fixture, and %s is a %s fixture", a.Name, a.FixtureKind, fx.Name, fx.Kind)
	}
	s.features["fixture."+fx.Name] = true
	fb := map[string]value.Type{}
	in := a.FixtureInput.Subst(bound)
	inKnown := len(in.Params()) == 0
	if inKnown && !value.Unify(fx.Input, in, fb) {
		return fmt.Errorf("fixture %s takes %s, not %s", fx.Name, fx.Input, in)
	}
	if a.FixtureOutput != nil {
		out := a.FixtureOutput.Subst(bound)
		if len(out.Params()) == 0 {
			if !value.Unify(*fx.Output, out, fb) {
				return fmt.Errorf("fixture %s gives %s, not %s", fx.Name, *fx.Output, out)
			}
		} else {
			o := fx.Output.Subst(fb)
			if err := concrete(o, "fixture "+fx.Name); err != nil {
				return err
			}
			if !value.Unify(out, o, bound) {
				return fmt.Errorf("fixture %s gives %s, not %s", fx.Name, o, out)
			}
		}
	}
	if !inKnown {
		i := fx.Input.Subst(fb)
		if err := concrete(i, "fixture "+fx.Name); err != nil {
			return err
		}
		if !value.Unify(in, i, bound) {
			return fmt.Errorf("fixture %s takes %s, not %s", fx.Name, i, in)
		}
	}
	if fx.Issue != nil {
		s.add(Possible{Key: fx.Issue.Key, Code: fx.Issue.Code, Meta: fx.Issue.Meta, Message: fx.Issue.Message})
	}
	return nil
}

func (s *state) addIssues(owner string, refs []IssueRef, bound map[string]value.Type) error {
	for _, ref := range refs {
		p, err := s.Possible(ref, bound)
		if err != nil {
			return fmt.Errorf("%s: %w", owner, err)
		}
		s.add(p)
	}
	return nil
}

// Possible instantiates an issue reference with the types bound gives the form's parameters.
func (c *Checker) Possible(ref IssueRef, bound map[string]value.Type) (Possible, error) {
	v, ok := c.Catalog.Variants[ref.Key]
	if !ok {
		return Possible{}, fmt.Errorf("issue %s is not in the catalogue", ref.Key)
	}
	args := map[string]value.Type{}
	for _, p := range v.Params {
		b, ok := ref.Bind[p]
		if !ok {
			return Possible{}, fmt.Errorf("issue %s: %s is not bound", ref.Key, p)
		}
		args[p] = b.Subst(bound)
		if err := concrete(args[p], "issue "+ref.Key); err != nil {
			return Possible{}, err
		}
	}
	meta, err := v.Instantiate(args)
	if err != nil {
		return Possible{}, err
	}
	p := Possible{Key: v.Key, Code: v.Code, Meta: meta}
	for _, o := range ref.Omit {
		if _, ok := meta[o]; !ok {
			return Possible{}, fmt.Errorf("issue %s has no metadata %s to omit", ref.Key, o)
		}
		delete(meta, o)
	}
	for _, o := range v.Optional {
		if _, ok := meta[o]; ok {
			p.Optional = append(p.Optional, o)
		}
	}
	return p, nil
}

func (s *state) add(p Possible) {
	for _, q := range s.issues {
		if q.Key == p.Key && q.Message == p.Message && sameMeta(q.Meta, p.Meta) && slices.Equal(q.Optional, p.Optional) {
			return
		}
	}
	s.issues = append(s.issues, p)
}

func sameMeta(a, b map[string]value.Type) bool {
	if len(a) != len(b) {
		return false
	}
	for k, t := range a {
		u, ok := b[k]
		if !ok || !t.Same(u) {
			return false
		}
	}
	return true
}

// Validate checks that every issue a form declares is in the catalogue with every parameter
// bound, and that every fixture's issue is well-typed.
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
		}
	}
	for name, f := range c.Registry.Constructors {
		check("constructor "+name, f)
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
