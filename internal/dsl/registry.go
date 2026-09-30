// Package dsl reads and type-checks the decoder language: the JSON forms by which a case names
// the decoder or encoder it exercises. See spec/decoder-language.md.
package dsl

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/schemas"
	"github.com/raoh-project/raoh-specification/internal/value"
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
	// Default is what an optional value argument left out stands for, if the form says.
	Default *value.Value
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
	// Meta says where the values of metadata entries come from; an entry it does not list is
	// known only when a decoder runs.
	Meta map[string]MetaSource
}

// MetaSource says where the value of a metadata entry comes from, when the form alone decides it.
type MetaSource struct {
	// Kind is const, const_by_type, arg, sorted, ascii_lower_sorted, sorted_keys or member.
	Kind string
	// Const is the value of a const, as an observation of the entry's type.
	Const *jsontext.Node
	// ByType is the value of a const_by_type, by the type the entry has.
	ByType map[string]*jsontext.Node
	// Arg is the argument an arg, sorted, ascii_lower_sorted or sorted_keys source reads, by name
	// as the file writes it and by index once resolved.
	ArgName string
	Arg     int
}

func parseMetaSource(n *jsontext.Node) (MetaSource, error) {
	if n.Kind != jsontext.Object || len(n.Members) != 1 {
		return MetaSource{}, fmt.Errorf("a metadata source is an object with one member")
	}
	m := n.Members[0]
	src := MetaSource{Kind: m.Name}
	switch m.Name {
	case "const":
		src.Const = m.Value
	case "const_by_type":
		if m.Value.Kind != jsontext.Object {
			return src, fmt.Errorf("const_by_type must be an object of values by type")
		}
		src.ByType = map[string]*jsontext.Node{}
		for _, e := range m.Value.Members {
			src.ByType[e.Name] = e.Value
		}
	case "arg", "sorted", "ascii_lower_sorted", "sorted_keys":
		if m.Value.Kind != jsontext.String {
			return src, fmt.Errorf("%s must name an argument", m.Name)
		}
		src.ArgName = m.Value.Text
	case "member":
		if m.Value.Kind != jsontext.Bool || !m.Value.Bool {
			return src, fmt.Errorf("member must be true")
		}
	default:
		return src, fmt.Errorf("%q is not a metadata source", m.Name)
	}
	return src, nil
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
	// Flow is how the form gives its issues.
	Flow Expr
	// SymbolsFrom is the index of the argument whose strings are the alternatives of the symbol
	// type the form gives, or -1.
	SymbolsFrom int
	// Requires are the conditions its arguments have to meet for the form to exist at all, as
	// raoh-java refuses to construct the decoder otherwise.
	Requires []Require
}

// Require is a condition on the values of some of a form's arguments.
type Require struct {
	// Check is ordered (the first is not after the second, as Compare orders them), nonzero,
	// nonempty (a list with an element) or distinct_ascii_fold (strings that stay distinct when
	// A-Z are read as a-z).
	Check string
	Args  []int
}

var requireArity = map[string]int{"ordered": 2, "nonzero": 1, "nonempty": 1, "distinct_ascii_fold": 1}

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

// Load reads the registry under root, each file checked against its schema first.
func Load(root string, sch *schemas.Set) (*Registry, error) {
	ops, err := os.ReadFile(filepath.Join(root, "catalog", "operations.json"))
	if err != nil {
		return nil, err
	}
	if err := sch.Validate("operations", ops); err != nil {
		return nil, fmt.Errorf("catalog/operations.json: %w", err)
	}
	fixtures, err := os.ReadFile(filepath.Join(root, "catalog", "fixtures.json"))
	if err != nil {
		return nil, err
	}
	if err := sch.Validate("fixtures", fixtures); err != nil {
		return nil, fmt.Errorf("catalog/fixtures.json: %w", err)
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
	for name, f := range r.Constructors {
		if err := f.needs(name, "constructor", true, false); err != nil {
			return nil, err
		}
	}
	for name, f := range r.Fields {
		if err := f.needs(name, "field", true, false); err != nil {
			return nil, err
		}
	}
	for name, f := range r.Encoders {
		if err := f.needs(name, "encoder", false, true); err != nil {
			return nil, err
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
		if err := f.needs(name.Text, "operation", true, false); err != nil {
			return nil, err
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
	f := &Form{Name: name, SymbolsFrom: -1}
	var err error
	if f.Doc, err = n.String("doc"); err != nil {
		return nil, err
	}
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
	if args, ok := n.Get("args"); ok {
		for _, a := range args.Elems {
			arg, err := parseArg(a)
			if err != nil {
				return nil, err
			}
			f.Args = append(f.Args, arg)
		}
	}
	if from, ok := str(n, "symbols_from"); ok {
		i := f.argIndex(from)
		if f.Result.Kind != value.Symbol || i < 0 || f.Args[i].Kind != "value" || f.Args[i].Type.String() != "list<string>" {
			return nil, fmt.Errorf("symbols_from %s needs a symbol result and a list<string> value argument %s", from, from)
		}
		f.SymbolsFrom = i
	}
	for i, a := range f.Args {
		if !a.Optional && i > 0 && f.Args[i-1].Optional {
			return nil, fmt.Errorf("argument %s follows an optional argument", a.Name)
		}
	}
	if reqs, ok := n.Get("requires"); ok {
		for _, e := range reqs.Elems {
			var r Require
			r.Check, _ = str(e, "check")
			args, err := strs(e, "args")
			if err != nil {
				return nil, err
			}
			if want, ok := requireArity[r.Check]; !ok || len(args) != want {
				return nil, fmt.Errorf("requires %q with %d argument(s) is not a condition", r.Check, len(args))
			}
			for _, name := range args {
				i := f.argIndex(name)
				if i < 0 || f.Args[i].Kind != "value" {
					return nil, fmt.Errorf("requires %s of %s, which is not a value argument", r.Check, name)
				}
				r.Args = append(r.Args, i)
			}
			f.Requires = append(f.Requires, r)
		}
	}
	if issues, ok := n.Get("issues"); ok {
		for _, e := range issues.Elems {
			ref, err := parseIssueRef(e)
			if err != nil {
				return nil, err
			}
			for name, src := range ref.Meta {
				if src.ArgName == "" {
					continue
				}
				i := f.argIndex(src.ArgName)
				kind := "value"
				if src.Kind == "sorted_keys" {
					kind = "variants"
				}
				if i < 0 || f.Args[i].Kind != kind {
					return nil, fmt.Errorf("issue %s meta %s reads %s, which is not a %s argument", ref.Key, name, src.ArgName, kind)
				}
				if src.Kind == "ascii_lower_sorted" && f.Args[i].Type.String() != "list<string>" {
					return nil, fmt.Errorf("issue %s meta %s lower-cases %s, which is not a list<string>", ref.Key, name, src.ArgName)
				}
				if src.Kind == "sorted" && f.Args[i].Type.Kind != value.List {
					return nil, fmt.Errorf("issue %s meta %s sorts %s, which is not a list", ref.Key, name, src.ArgName)
				}
				src.Arg = i
				ref.Meta[name] = src
			}
			f.Issues = append(f.Issues, ref)
		}
	}
	if fl, ok := n.Get("flow"); ok {
		if f.Flow, err = resolveFlow(f, fl); err != nil {
			return nil, fmt.Errorf("flow: %w", err)
		}
	}
	return f, nil
}

// argIndex is the index of the argument with a name, or -1.
func (f *Form) argIndex(name string) int {
	for i, a := range f.Args {
		if a.Name == name {
			return i
		}
	}
	return -1
}

// needs checks that a form has the types its section requires, so that a type the file leaves
// out is an error and never the zero Type.
func (f *Form) needs(name, section string, result, input bool) error {
	if result && f.Result.Kind == value.Invalid {
		return fmt.Errorf("operations.json: %s %s has no result type", section, name)
	}
	if result && f.Flow == nil {
		return fmt.Errorf("operations.json: %s %s has no flow", section, name)
	}
	if input && f.Input.Kind == value.Invalid {
		return fmt.Errorf("operations.json: %s %s has no input type", section, name)
	}
	for _, a := range f.Args {
		if slices.Contains([]string{"decoder", "decoders", "variants", "value", "encoder"}, a.Kind) && a.Type.Kind == value.Invalid {
			return fmt.Errorf("operations.json: %s %s: argument %s has no type", section, name, a.Name)
		}
		if a.Kind == "fixture" && a.FixtureInput.Kind == value.Invalid {
			return fmt.Errorf("operations.json: %s %s: argument %s has no fixture input type", section, name, a.Name)
		}
	}
	return nil
}

func parseArg(n *jsontext.Node) (Arg, error) {
	var a Arg
	var err error
	if a.Name, err = n.String("name"); err != nil {
		return a, err
	}
	if a.Kind, err = n.String("kind"); err != nil {
		return a, err
	}
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
	if d, ok := n.Get("default"); ok {
		v, err := value.Observe(a.Type, d)
		if err != nil {
			return a, fmt.Errorf("argument %s: default: %w", a.Name, err)
		}
		a.Default = &v
	}
	if a.OneOf, err = strs(n, "one_of"); err != nil {
		return a, err
	}
	if a.Kind == "fixture" {
		if a.FixtureKind, err = n.String("fixture"); err != nil {
			return a, err
		}
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
		case m.Name == "meta":
			if m.Value.Kind != jsontext.Object {
				return ref, fmt.Errorf("meta must be an object")
			}
			ref.Meta = map[string]MetaSource{}
			for _, f := range m.Value.Members {
				src, err := parseMetaSource(f.Value)
				if err != nil {
					return ref, fmt.Errorf("meta %s: %w", f.Name, err)
				}
				ref.Meta[f.Name] = src
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
	var err error
	if f.Kind, err = n.String("kind"); err != nil {
		return nil, err
	}
	if !slices.Contains([]string{"map", "refine", "flatMap", "recover", "getter"}, f.Kind) {
		return nil, fmt.Errorf("unknown kind %q", f.Kind)
	}
	if f.Doc, err = n.String("doc"); err != nil {
		return nil, err
	}
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
		if fi.Code, err = is.String("code"); err != nil {
			return nil, err
		}
		if fi.Key, err = is.String("message_key"); err != nil {
			return nil, err
		}
		if fi.Message, err = is.String("message"); err != nil {
			return nil, err
		}
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
		if values, ok := is.Get("values"); ok {
			for _, m := range values.Members {
				if _, ok := fi.Meta[m.Name]; !ok {
					return nil, fmt.Errorf("values gives %s, which meta does not have", m.Name)
				}
				if m.Value.Kind != jsontext.String || m.Value.Text != "input" {
					return nil, fmt.Errorf(`values: the only source of a value is "input"`)
				}
			}
		}
		if len(fi.Path) > 0 && f.Kind != "flatMap" {
			return nil, fmt.Errorf("only a flatMap fixture gives its issue a path")
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
