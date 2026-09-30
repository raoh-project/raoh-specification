// Package dsl reads and type-checks the decoder language: the JSON forms by which a case names
// the decoder or encoder it exercises. See spec/decoder-language.md.
package dsl

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/raoh-project/raoh-specification/jsontext"
	"github.com/raoh-project/raoh-specification/value"
)

// Arg is one argument of a constructor, field, operation, encoder or property.
type Arg struct {
	Name string
	// Kind is decoder, decoders, fields, variants, value, message, fixture, encoder or properties.
	Kind string
	// Type is the type of a value, or the result type of a decoder, or the input type of an
	// encoder; it may mention type parameters.
	Type value.Type
	// Optional marks a trailing argument of an operation that may be left out.
	Optional bool
	// OneOf restricts a string value to the values listed.
	OneOf []string
	// FixtureKind, FixtureInput and FixtureOutput constrain a fixture argument.
	FixtureKind   string
	FixtureInput  value.Type
	FixtureOutput *value.Type
}

// IssueRef is an issue variant a form can give, with its type parameters bound to types that
// may mention the form's own parameters.
type IssueRef struct {
	Key  string
	Bind map[string]value.Type
	Omit []string
}

// Form describes a constructor, field, operation, encoder or property.
type Form struct {
	Name string
	Doc  string
	Args []Arg
	// Result is the result type of a constructor, field or operation; the product type of an
	// object is computed from its fields.
	Result value.Type
	// Receivers are the types an operation applies to; "*" is any type.
	Receivers []string
	// Input is the input type of an encoder.
	Input  value.Type
	Issues []IssueRef
	// InputOrder marks a form whose issues come in the order of the input's members.
	InputOrder bool
	// ReplacesIssues marks a form that gives no issue of its inner decoder.
	ReplacesIssues bool
}

// FixtureIssue is the issue a refine or flatMap fixture creates.
type FixtureIssue struct {
	Path    []string
	Code    string
	Key     string
	Message string
	Meta    map[string]value.Type
}

// Fixture is a named function the suite uses.
type Fixture struct {
	Name   string
	Kind   string
	Doc    string
	Input  value.Type
	Output *value.Type
	Issue  *FixtureIssue
}

// Registry is catalog/operations.json and catalog/fixtures.json.
type Registry struct {
	Constructors map[string]*Form
	Fields       map[string]*Form
	Operations   map[string][]*Form
	Encoders     map[string]*Form
	Properties   map[string]*Form
	Fixtures     map[string]*Fixture
}

// Load reads the registry under root.
func Load(root string) (*Registry, error) {
	ops, err := os.ReadFile(filepath.Join(root, "catalog", "operations.json"))
	if err != nil {
		return nil, err
	}
	fixtures, err := os.ReadFile(filepath.Join(root, "catalog", "fixtures.json"))
	if err != nil {
		return nil, err
	}
	return Parse(ops, fixtures)
}

// Parse reads a registry from the text of operations.json and fixtures.json.
func Parse(operations, fixtures []byte) (*Registry, error) {
	r := &Registry{
		Constructors: map[string]*Form{}, Fields: map[string]*Form{}, Operations: map[string][]*Form{},
		Encoders: map[string]*Form{}, Properties: map[string]*Form{}, Fixtures: map[string]*Fixture{},
	}
	root, err := jsontext.Parse(operations)
	if err != nil {
		return nil, fmt.Errorf("operations.json: %w", err)
	}
	for _, section := range []struct {
		name string
		into map[string]*Form
	}{{"constructors", r.Constructors}, {"fields", r.Fields}, {"encoders", r.Encoders}, {"properties", r.Properties}} {
		n, ok := root.Get(section.name)
		if !ok || n.Kind != jsontext.Object {
			return nil, fmt.Errorf("operations.json: %s must be an object", section.name)
		}
		for _, m := range n.Members {
			f, err := parseForm(m.Name, m.Value)
			if err != nil {
				return nil, fmt.Errorf("operations.json: %s %s: %w", section.name, m.Name, err)
			}
			section.into[m.Name] = f
		}
	}
	list, ok := root.Get("operations")
	if !ok || list.Kind != jsontext.Array {
		return nil, fmt.Errorf("operations.json: operations must be an array")
	}
	for _, e := range list.Elems {
		name, ok := e.Get("name")
		if !ok || name.Kind != jsontext.String {
			return nil, fmt.Errorf("operations.json: an operation has no name")
		}
		f, err := parseForm(name.Text, e)
		if err != nil {
			return nil, fmt.Errorf("operations.json: operation %s: %w", name.Text, err)
		}
		if len(f.Receivers) == 0 {
			return nil, fmt.Errorf("operations.json: operation %s has no receivers", name.Text)
		}
		for _, other := range r.Operations[f.Name] {
			for _, rc := range f.Receivers {
				if slices.Contains(other.Receivers, rc) {
					return nil, fmt.Errorf("operations.json: operation %s is defined twice for %s", f.Name, rc)
				}
			}
		}
		r.Operations[f.Name] = append(r.Operations[f.Name], f)
	}
	fx, err := jsontext.Parse(fixtures)
	if err != nil {
		return nil, fmt.Errorf("fixtures.json: %w", err)
	}
	for _, m := range fx.Members {
		f, err := parseFixture(m.Name, m.Value)
		if err != nil {
			return nil, fmt.Errorf("fixtures.json: %s: %w", m.Name, err)
		}
		r.Fixtures[m.Name] = f
	}
	return r, nil
}

func str(n *jsontext.Node, name string) (string, bool) {
	v, ok := n.Get(name)
	if !ok || v.Kind != jsontext.String {
		return "", false
	}
	return v.Text, true
}

func strs(n *jsontext.Node, name string) ([]string, error) {
	v, ok := n.Get(name)
	if !ok {
		return nil, nil
	}
	if v.Kind != jsontext.Array {
		return nil, fmt.Errorf("%s must be an array", name)
	}
	out := []string{}
	for _, e := range v.Elems {
		if e.Kind != jsontext.String {
			return nil, fmt.Errorf("%s must be an array of strings", name)
		}
		out = append(out, e.Text)
	}
	return out, nil
}

func typeOf(n *jsontext.Node, name string) (value.Type, bool, error) {
	s, ok := str(n, name)
	if !ok {
		return value.Type{}, false, nil
	}
	t, err := value.ParseType(s)
	return t, true, err
}

var argKinds = []string{"decoder", "decoders", "fields", "variants", "value", "message", "fixture", "encoder", "properties"}

func parseForm(name string, n *jsontext.Node) (*Form, error) {
	f := &Form{Name: name}
	f.Doc, _ = str(n, "doc")
	if f.Doc == "" {
		return nil, fmt.Errorf("no doc")
	}
	var err error
	if s, ok := str(n, "result"); ok {
		if s != "product" {
			if f.Result, err = value.ParseType(s); err != nil {
				return nil, err
			}
		} else {
			f.Result = value.Type{Kind: value.Product}
		}
	}
	if t, ok, err := typeOf(n, "input"); err != nil {
		return nil, err
	} else if ok {
		f.Input = t
	}
	if f.Receivers, err = strs(n, "receivers"); err != nil {
		return nil, err
	}
	for _, rc := range f.Receivers {
		if rc != "*" {
			if _, err := value.ParseType(rc); err != nil {
				return nil, err
			}
		}
	}
	if order, ok := str(n, "issue_order"); ok {
		if order != "input" {
			return nil, fmt.Errorf("issue_order must be \"input\"")
		}
		f.InputOrder = true
	}
	if rep, ok := n.Get("replaces_issues"); ok {
		f.ReplacesIssues = rep.Kind == jsontext.Bool && rep.Bool
	}
	if args, ok := n.Get("args"); ok {
		for _, a := range args.Elems {
			arg, err := parseArg(a)
			if err != nil {
				return nil, err
			}
			f.Args = append(f.Args, arg)
		}
	}
	for i, a := range f.Args {
		if !a.Optional && i > 0 && f.Args[i-1].Optional {
			return nil, fmt.Errorf("argument %s follows an optional argument", a.Name)
		}
	}
	if issues, ok := n.Get("issues"); ok {
		for _, e := range issues.Elems {
			ref, err := parseIssueRef(e)
			if err != nil {
				return nil, err
			}
			f.Issues = append(f.Issues, ref)
		}
	}
	return f, nil
}

func parseArg(n *jsontext.Node) (Arg, error) {
	var a Arg
	a.Name, _ = str(n, "name")
	a.Kind, _ = str(n, "kind")
	if !slices.Contains(argKinds, a.Kind) {
		return a, fmt.Errorf("argument %s has unknown kind %q", a.Name, a.Kind)
	}
	t, ok, err := typeOf(n, "type")
	if err != nil {
		return a, err
	}
	if ok {
		a.Type = t
	} else if slices.Contains([]string{"decoder", "decoders", "variants", "value", "encoder"}, a.Kind) {
		return a, fmt.Errorf("argument %s needs a type", a.Name)
	}
	if o, ok := n.Get("optional"); ok && o.Kind == jsontext.Bool {
		a.Optional = o.Bool
	}
	if a.OneOf, err = strs(n, "one_of"); err != nil {
		return a, err
	}
	if a.Kind == "fixture" {
		a.FixtureKind, _ = str(n, "fixture")
		in, ok, err := typeOf(n, "input")
		if err != nil || !ok {
			return a, fmt.Errorf("fixture argument %s needs an input type", a.Name)
		}
		a.FixtureInput = in
		if out, ok, err := typeOf(n, "output"); err != nil {
			return a, err
		} else if ok {
			a.FixtureOutput = &out
		}
	}
	return a, nil
}

func parseIssueRef(n *jsontext.Node) (IssueRef, error) {
	if n.Kind == jsontext.String {
		return IssueRef{Key: n.Text}, nil
	}
	ref := IssueRef{Bind: map[string]value.Type{}}
	for _, m := range n.Members {
		switch {
		case m.Name == "key":
			ref.Key = m.Value.Text
		case m.Name == "omit":
			for _, e := range m.Value.Elems {
				ref.Omit = append(ref.Omit, e.Text)
			}
		default:
			t, err := value.ParseType(m.Value.Text)
			if err != nil {
				return ref, err
			}
			ref.Bind[m.Name] = t
		}
	}
	if ref.Key == "" {
		return ref, fmt.Errorf("an issue reference has no key")
	}
	return ref, nil
}

func parseFixture(name string, n *jsontext.Node) (*Fixture, error) {
	f := &Fixture{Name: name}
	f.Kind, _ = str(n, "kind")
	if !slices.Contains([]string{"map", "refine", "flatMap", "recover", "getter"}, f.Kind) {
		return nil, fmt.Errorf("unknown kind %q", f.Kind)
	}
	f.Doc, _ = str(n, "doc")
	in, ok, err := typeOf(n, "input")
	if err != nil || !ok {
		return nil, fmt.Errorf("no input type")
	}
	f.Input = in
	if out, ok, err := typeOf(n, "output"); err != nil {
		return nil, err
	} else if ok {
		f.Output = &out
	}
	if (f.Output == nil) != (f.Kind == "refine") {
		return nil, fmt.Errorf("a %s fixture %s an output type", f.Kind, map[bool]string{true: "has no", false: "has"}[f.Kind == "refine"])
	}
	if is, ok := n.Get("issue"); ok {
		fi := &FixtureIssue{Meta: map[string]value.Type{}}
		fi.Code, _ = str(is, "code")
		fi.Key, _ = str(is, "message_key")
		fi.Message, _ = str(is, "message")
		if fi.Path, err = strs(is, "path"); err != nil {
			return nil, err
		}
		if meta, ok := is.Get("meta"); ok {
			for _, m := range meta.Members {
				t, err := value.ParseType(m.Value.Text)
				if err != nil {
					return nil, err
				}
				fi.Meta[m.Name] = t
			}
		}
		if fi.Code == "" || fi.Key == "" || fi.Message == "" {
			return nil, fmt.Errorf("the issue needs a code, a message_key and a message")
		}
		f.Issue = fi
	}
	if (f.Issue != nil) != (f.Kind == "refine" || f.Kind == "flatMap") {
		return nil, fmt.Errorf("only refine and flatMap fixtures declare an issue, and they must")
	}
	return f, nil
}

// Features returns every feature ID the registry defines, sorted.
func (r *Registry) Features() []string {
	var ids []string
	for name := range r.Constructors {
		ids = append(ids, "decoder."+name)
	}
	for name := range r.Fields {
		ids = append(ids, "field."+name)
	}
	for name, forms := range r.Operations {
		for _, f := range forms {
			for _, rc := range f.Receivers {
				ids = append(ids, operationFeature(rc, name))
			}
		}
	}
	for name := range r.Encoders {
		ids = append(ids, "encoder."+name)
	}
	for name := range r.Properties {
		ids = append(ids, "property."+name)
	}
	for name := range r.Fixtures {
		ids = append(ids, "fixture."+name)
	}
	sort.Strings(ids)
	return ids
}

// operationFeature names the feature of an operation on receivers of a pattern.
func operationFeature(receiver, name string) string {
	if receiver == "*" {
		return "operation.any." + name
	}
	t := value.MustParseType(receiver)
	return "operation." + value.Type{Kind: t.Kind}.String() + "." + name
}
