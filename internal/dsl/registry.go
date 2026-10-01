// Package dsl reads and type-checks the decoder language: the JSON forms by which a case names
// the decoder or encoder it exercises. See spec/decoder-language.md.
package dsl

import (
	"fmt"
	"maps"
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
	// Optional marks a trailing value or message argument of an operation that may be left out.
	Optional bool
	// Default is what an optional value argument left out stands for; every optional value
	// argument has one. An optional message argument left out gives no message.
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

// Gives reports whether the reference gives the entry a source, so that the entry is always there.
func (r IssueRef) Gives(name string) bool {
	_, ok := r.Meta[name]
	return ok
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
	// as the file writes it and resolved once the form is read.
	ArgName string
	Arg     ArgRef
}

// readsArg reports whether a kind of metadata source reads an argument.
func readsArg(kind string) bool {
	return kind == "arg" || kind == "sorted" || kind == "ascii_lower_sorted" || kind == "sorted_keys"
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
	// Message is the form's message argument, or NoArg; a form has at most one.
	Message ArgRef
	// Member is, for a property, the string value argument that names the member it writes.
	Member ArgRef
	// Result is how the result type of a constructor, field or operation is found.
	Result Result
	// Receivers are the receivers an operation applies to.
	Receivers []Receiver
	// Input is the type an encoder encodes, or a property reads.
	Input  value.Type
	Issues []IssueRef
	// Flow is how the form gives its issues.
	Flow Expr
	// Requires are the conditions its arguments have to meet for the form to exist at all: an
	// implementation refuses to construct the decoder otherwise.
	Requires []Require
}

// ReceiverKey identifies the receivers an overload of an operation applies to: AnyReceiver, or
// the name of the outer kind of the receiver's type. It is what an operation's feature ID names, and
// an operation has at most one overload for each.
type ReceiverKey string

// AnyReceiver is the key of an operation that applies to every type; it has no other overload.
const AnyReceiver ReceiverKey = "*"

// Receiver is a receiver pattern of an operation: the key it applies under and, but for
// AnyReceiver, the type a receiver of that kind unifies with.
type Receiver struct {
	Key     ReceiverKey
	Pattern *value.Type
}

// Overload is the form an operation has for a receiver key, and its receiver pattern.
type Overload struct {
	Form     *Form
	Receiver Receiver
}

// Require is a condition on the values of some of a form's arguments.
type Require struct {
	// Check is a kind of condition (see requirements).
	Check string
	Args  []ArgRef
}

// requirement is what a kind of condition reads: how many arguments, of what kind, and, for a
// value argument, whether it is a list, and whether of strings.
type requirement struct {
	arity   int
	kind    string
	list    bool
	strings bool
	// text is a condition on one string value.
	text bool
}

// requirements are the kinds of condition. Each is a condition on what the arguments mean, which
// the specification refuses whether or not an implementation checks it, and never one that only a
// host language's API imposes (see spec/decoder-language.md).
var requirements = map[string]requirement{
	// ordered: the first value is not after the second, as Compare orders them.
	"ordered": {arity: 2, kind: "value"},
	// nonzero: the value is not zero.
	"nonzero": {arity: 1, kind: "value"},
	// nonempty: the list has an element.
	"nonempty": {arity: 1, kind: "value", list: true},
	// distinct: no two elements of the list are the same value (value-model sameness).
	"distinct": {arity: 1, kind: "value", list: true},
	// distinct_ascii_fold: the strings stay distinct when A-Z are read as a-z.
	"distinct_ascii_fold": {arity: 1, kind: "value", list: true, strings: true},
	// pattern: the string is a pattern of spec/pattern.md.
	"pattern": {arity: 1, kind: "value", text: true},
	// named_fields: every field names the member it reads; none is flat.
	"named_fields": {arity: 1, kind: "fields"},
	// distinct_members: no two properties write the same member.
	"distinct_members": {arity: 1, kind: "properties"},
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

// fixtureSignature is the shape every fixture of a kind has, and every fixture argument of that
// kind declares: whether it has an output type, whether it gives an issue, and whether that issue
// may be at a path of its own.
type fixtureSignature struct {
	Output, Issue, Path bool
}

// fixtureSignatures are the fixture kinds; fixtures, fixture arguments and flows read what a kind
// is from here alone.
var fixtureSignatures = map[string]fixtureSignature{
	"map":     {Output: true},
	"refine":  {Issue: true},
	"flatMap": {Output: true, Issue: true, Path: true},
	"recover": {Output: true},
	"getter":  {Output: true},
}

// Result is how a form's result type is found: ResultType, ResultProduct or ResultSymbols.
type Result interface{ isResult() }

// ResultType is a result type the form declares, which may mention its type parameters.
type ResultType struct{ Type value.Type }

// ResultProduct is the product of the types of the fields a fields argument reads, in order.
type ResultProduct struct{ Fields ArgRef }

// ResultSymbols is the symbol type whose alternatives are the strings of a list<string> value
// argument.
type ResultSymbols struct{ Symbols ArgRef }

func (ResultType) isResult()    {}
func (ResultProduct) isResult() {}
func (ResultSymbols) isResult() {}

// Registry is catalog/operations.json and catalog/fixtures.json. A Registry that Parse returns
// meets every condition the checker relies on: its argument names are unambiguous, every argument
// that may be left out says what leaving it out means, every reference resolves to exactly one
// argument or issue, and every member the files write means something. Only what needs the issue
// catalogue is left to Checker.
type Registry struct {
	Constructors map[string]*Form
	Fields       map[string]*Form
	// Operations are the overloads of each operation, by receiver key.
	Operations map[string]map[ReceiverKey]Overload
	Encoders   map[string]*Form
	Properties map[string]*Form
	Fixtures   map[string]*Fixture
}

// Forms returns every form that can give issues: the constructors, the fields and each distinct
// form of an operation's overloads.
func (r *Registry) Forms() []*Form {
	var out []*Form
	for _, name := range slices.Sorted(maps.Keys(r.Constructors)) {
		out = append(out, r.Constructors[name])
	}
	for _, name := range slices.Sorted(maps.Keys(r.Fields)) {
		out = append(out, r.Fields[name])
	}
	for _, name := range slices.Sorted(maps.Keys(r.Operations)) {
		overloads := r.Operations[name]
		for _, key := range slices.Sorted(maps.Keys(overloads)) {
			if f := overloads[key].Form; !slices.Contains(out, f) {
				out = append(out, f)
			}
		}
	}
	return out
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

// sectionMembers are the members a form of each section may have.
var sectionMembers = map[string][]string{
	"constructor": {"doc", "result", "args", "issues", "requires", "symbols_from", "flow"},
	"field":       {"doc", "result", "args", "issues", "requires", "symbols_from", "flow"},
	"operation":   {"name", "doc", "receivers", "result", "args", "issues", "requires", "symbols_from", "flow"},
	"encoder":     {"doc", "input", "args", "requires"},
	"property":    {"doc", "input", "args", "member"},
}

// Parse reads a registry from the text of operations.json and fixtures.json. It does not rely on
// the schemas: what it accepts without them means what it means with them.
func Parse(operations, fixtures []byte) (*Registry, error) {
	r := &Registry{
		Constructors: map[string]*Form{}, Fields: map[string]*Form{}, Operations: map[string]map[ReceiverKey]Overload{},
		Encoders: map[string]*Form{}, Properties: map[string]*Form{}, Fixtures: map[string]*Fixture{},
	}
	root, err := jsontext.Parse(operations)
	if err != nil {
		return nil, fmt.Errorf("operations.json: %w", err)
	}
	if err := object(root, "operations.json", "constructors", "fields", "operations", "encoders", "properties"); err != nil {
		return nil, err
	}
	for _, section := range []struct {
		name, form string
		into       map[string]*Form
	}{{"constructors", "constructor", r.Constructors}, {"fields", "field", r.Fields}, {"encoders", "encoder", r.Encoders}, {"properties", "property", r.Properties}} {
		n, ok := root.Get(section.name)
		if !ok || n.Kind != jsontext.Object {
			return nil, fmt.Errorf("operations.json: %s must be an object", section.name)
		}
		for _, m := range n.Members {
			f, err := parseForm(section.form, m.Name, m.Value)
			if err != nil {
				return nil, fmt.Errorf("operations.json: %s %s: %w", section.form, m.Name, err)
			}
			section.into[m.Name] = f
		}
	}
	list, ok := root.Get("operations")
	if !ok || list.Kind != jsontext.Array {
		return nil, fmt.Errorf("operations.json: operations must be an array")
	}
	for _, e := range list.Elems {
		if e.Kind != jsontext.Object {
			return nil, fmt.Errorf("operations.json: an operation must be an object")
		}
		name, ok, err := text(e, "name")
		if err != nil || !ok {
			return nil, fmt.Errorf("operations.json: an operation has no name")
		}
		f, err := parseForm("operation", name, e)
		if err != nil {
			return nil, fmt.Errorf("operations.json: operation %s: %w", name, err)
		}
		overloads := r.Operations[f.Name]
		if overloads == nil {
			overloads = map[ReceiverKey]Overload{}
			r.Operations[f.Name] = overloads
		}
		for _, rc := range f.Receivers {
			if _, dup := overloads[rc.Key]; dup {
				return nil, fmt.Errorf("operations.json: operation %s applies to %s twice", f.Name, rc.Key)
			}
			overloads[rc.Key] = Overload{Form: f, Receiver: rc}
		}
		if _, any := overloads[AnyReceiver]; any && len(overloads) > 1 {
			return nil, fmt.Errorf("operations.json: operation %s applies to every type, and to some types again", f.Name)
		}
	}
	fx, err := jsontext.Parse(fixtures)
	if err != nil {
		return nil, fmt.Errorf("fixtures.json: %w", err)
	}
	if fx.Kind != jsontext.Object {
		return nil, fmt.Errorf("fixtures.json must be an object")
	}
	for _, m := range fx.Members {
		f, err := parseFixture(m.Name, m.Value)
		if err != nil {
			return nil, fmt.Errorf("fixtures.json: %s: %w", m.Name, err)
		}
		r.Fixtures[m.Name] = f
	}
	// A feature ID names one thing: a facet's written ID cannot also be a form's.
	ids := r.Features()
	for i := 1; i < len(ids); i++ {
		if ids[i] == ids[i-1] {
			return nil, fmt.Errorf("operations.json: two features have the ID %s", ids[i])
		}
	}
	return r, nil
}

// object checks that n is an object whose members are among those listed, so that a member the
// language does not have is an error and never passed over.
func object(n *jsontext.Node, what string, allowed ...string) error {
	if n.Kind != jsontext.Object {
		return fmt.Errorf("%s must be an object", what)
	}
	for _, m := range n.Members {
		if !slices.Contains(allowed, m.Name) {
			return fmt.Errorf("%s has no member %q", what, m.Name)
		}
	}
	return nil
}

// text reads a string member: ok is false when it is absent, and anything but a string is an
// error.
func text(n *jsontext.Node, name string) (string, bool, error) {
	v, ok := n.Get(name)
	if !ok {
		return "", false, nil
	}
	if v.Kind != jsontext.String {
		return "", false, fmt.Errorf("%s must be a string", name)
	}
	return v.Text, true, nil
}

// elems reads an array member; absent, it has no elements.
func elems(n *jsontext.Node, name string) ([]*jsontext.Node, error) {
	v, ok := n.Get(name)
	if !ok {
		return nil, nil
	}
	if v.Kind != jsontext.Array {
		return nil, fmt.Errorf("%s must be an array", name)
	}
	return v.Elems, nil
}

func strs(n *jsontext.Node, name string) ([]string, error) {
	es, err := elems(n, name)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, e := range es {
		if e.Kind != jsontext.String {
			return nil, fmt.Errorf("%s must be an array of strings", name)
		}
		out = append(out, e.Text)
	}
	return out, nil
}

func typeOf(n *jsontext.Node, name string) (value.Type, bool, error) {
	s, ok, err := text(n, name)
	if err != nil || !ok {
		return value.Type{}, false, err
	}
	t, err := value.ParseType(s)
	return t, true, err
}

// binding says how the types of an argument's kind are bound when a form is checked.
type binding int

const (
	// bindsNothing is an argument whose type, if it has one, has to be known when it is read.
	bindsNothing binding = iota + 1
	// bindsByForms is an argument whose decoders, fields, encoder or properties a case writes: their
	// types bind the argument's.
	bindsByForms
	// bindsByFixture is a fixture argument: the fixture a case names binds its input and output.
	bindsByFixture
)

// argKind is what every argument of a kind is. The rules about arguments read it from here; what
// depends on the form or on the argument's type as well (only an operation leaves arguments out,
// only a string value has one_of) is checked where the form is read.
type argKind struct {
	// Typed is an argument that declares a type.
	Typed bool
	// Binding is how its types are bound.
	Binding binding
	// Issues is an argument whose decoders give issues the form places or discards; a fixture
	// argument gives issues when its fixture kind does (see producesIssues).
	Issues bool
	// FlowArg is an argument whose issues the flow places with {"arg": ...}.
	FlowArg bool
	// Omittable is an argument an operation may leave out.
	Omittable bool
	// Valued is an argument whose value the case writes, which one_of and default constrain.
	Valued bool
}

// argKinds are the kinds of argument.
var argKinds = map[string]argKind{
	"decoder":    {Typed: true, Binding: bindsByForms, Issues: true, FlowArg: true},
	"decoders":   {Typed: true, Binding: bindsByForms, Issues: true},
	"variants":   {Typed: true, Binding: bindsByForms, Issues: true, FlowArg: true},
	"fields":     {Binding: bindsByForms, Issues: true, FlowArg: true},
	"encoder":    {Typed: true, Binding: bindsByForms},
	"properties": {Typed: true, Binding: bindsByForms},
	"fixture":    {Binding: bindsByFixture},
	"value":      {Typed: true, Binding: bindsNothing, Omittable: true, Valued: true},
	"message":    {Binding: bindsNothing, Omittable: true},
}

// producesIssues reports whether an argument gives issues the form has to place or discard: the
// decoders of an argument whose kind gives issues, and a fixture whose kind gives an issue.
func producesIssues(a Arg) bool {
	if a.Kind == "fixture" {
		return fixtureSignatures[a.FixtureKind].Issue
	}
	return argKinds[a.Kind].Issues
}

func parseForm(section, name string, n *jsontext.Node) (*Form, error) {
	if err := object(n, section, sectionMembers[section]...); err != nil {
		return nil, err
	}
	f := &Form{Name: name}
	var err error
	if f.Doc, err = n.String("doc"); err != nil {
		return nil, err
	}
	result, hasResult, err := text(n, "result")
	if err != nil {
		return nil, err
	}
	if t, ok, err := typeOf(n, "input"); err != nil {
		return nil, err
	} else if ok {
		f.Input = t
	}
	receivers, err := strs(n, "receivers")
	if err != nil {
		return nil, err
	}
	for _, rc := range receivers {
		r, err := parseReceiver(rc)
		if err != nil {
			return nil, err
		}
		f.Receivers = append(f.Receivers, r)
	}
	if err := f.parseArgs(section, n); err != nil {
		return nil, err
	}
	if hasResult {
		if f.Result, err = f.parseResult(result, n); err != nil {
			return nil, err
		}
	} else if _, ok := n.Get("symbols_from"); ok {
		return nil, fmt.Errorf("symbols_from says the alternatives of a symbol result, and it has no result")
	}
	reqs, err := elems(n, "requires")
	if err != nil {
		return nil, err
	}
	for _, e := range reqs {
		if err := object(e, "a condition", "check", "args"); err != nil {
			return nil, err
		}
		var r Require
		if r.Check, err = e.String("check"); err != nil {
			return nil, err
		}
		args, err := strs(e, "args")
		if err != nil {
			return nil, err
		}
		req, ok := requirements[r.Check]
		if !ok || len(args) != req.arity {
			return nil, fmt.Errorf("requires %q with %d argument(s) is not a condition", r.Check, len(args))
		}
		for _, name := range args {
			i, ok := f.arg(name)
			if !ok || f.Args[i.Index()].Kind != req.kind {
				return nil, fmt.Errorf("requires %s of %s, which is not a %s argument", r.Check, name, req.kind)
			}
			t := f.Args[i.Index()].Type
			if req.list && (t.Kind != value.List || (req.strings && t.Args[0].Kind != value.String)) {
				return nil, fmt.Errorf("requires %s of %s, a %s, and it reads a list", r.Check, name, t)
			}
			if req.text && t.Kind != value.String {
				return nil, fmt.Errorf("requires %s of %s, a %s, and it reads a string", r.Check, name, t)
			}
			r.Args = append(r.Args, i)
		}
		f.Requires = append(f.Requires, r)
	}
	if member, ok, err := text(n, "member"); err != nil {
		return nil, err
	} else if ok {
		i, found := f.arg(member)
		if !found || f.Args[i.Index()].Kind != "value" || f.Args[i.Index()].Type.Kind != value.String {
			return nil, fmt.Errorf("member %s is not a string value argument", member)
		}
		f.Member = i
	} else if section == "property" {
		return nil, fmt.Errorf("it does not say which argument names the member it writes")
	}
	if err := f.parseIssues(n); err != nil {
		return nil, err
	}
	// An operation checks or converts a value, and the issues it declares are about that value, so
	// it takes a given message exactly when it declares an issue (spec/issues.md); it is optional,
	// and the operation's message facet is giving it. A field is structure, and takes none.
	switch hasMessage := f.Message != NoArg; {
	case section == "operation" && len(f.Issues) > 0 && !hasMessage:
		return nil, fmt.Errorf("it declares issues, and an operation that does takes an optional message")
	case section == "operation" && len(f.Issues) == 0 && hasMessage:
		return nil, fmt.Errorf("it takes a message, and declares no issue for it to be the message of")
	case section == "operation" && hasMessage && !f.Args[f.Message.Index()].Optional:
		return nil, fmt.Errorf("its message is required, and an operation's message is optional")
	case section == "field" && hasMessage:
		return nil, fmt.Errorf("a field takes no message: it is structure, whose messages a resolver gives")
	case section == "constructor" && convertsAString(name) && !hasMessage:
		return nil, fmt.Errorf("it converts a string, and takes an optional trailing message")
	case section == "constructor" && convertsAString(name) && !f.Args[f.Message.Index()].Optional:
		return nil, fmt.Errorf("its message is required, and a constructor's message is optional")
	case section == "constructor" && !convertsAString(name) && hasMessage:
		return nil, fmt.Errorf("a constructor that builds structure takes no message; only enum and literal, which convert a string, do")
	}
	if fl, ok := n.Get("flow"); ok {
		if f.Flow, err = resolveFlow(f, fl); err != nil {
			return nil, fmt.Errorf("flow: %w", err)
		}
		// Members a form knows by its fields are known only if every field names one.
		var problem error
		walkExpr(f.Flow, func(e Expr) {
			x, ok := e.(ExprUnknown)
			if !ok || x.KnownFields == NoArg {
				return
			}
			if !slices.ContainsFunc(f.Requires, func(r Require) bool {
				return r.Check == "named_fields" && r.Args[0] == x.KnownFields
			}) {
				problem = fmt.Errorf("flow: the members it knows come from %s, which it does not require to be named_fields", f.Args[x.KnownFields.Index()].Name)
			}
		})
		if problem != nil {
			return nil, problem
		}
	}
	switch section {
	case "constructor", "field", "operation":
		if f.Result == nil {
			return nil, fmt.Errorf("it has no result type")
		}
		if f.Flow == nil {
			return nil, fmt.Errorf("it has no flow")
		}
	case "encoder", "property":
		if f.Input.Kind == value.Invalid {
			return nil, fmt.Errorf("it has no input type")
		}
	}
	if section == "operation" && len(f.Receivers) == 0 {
		return nil, fmt.Errorf("it has no receivers")
	}
	if err := f.checkBindings(section); err != nil {
		return nil, err
	}
	return f, nil
}

// parseResult reads a form's result once its arguments are known: "product" is the product of its
// one fields argument, "symbol" the symbol whose alternatives symbols_from names, and anything
// else a type.
func (f *Form) parseResult(result string, n *jsontext.Node) (Result, error) {
	from, hasFrom, err := text(n, "symbols_from")
	if err != nil {
		return nil, err
	}
	switch result {
	case "product":
		fields := NoArg
		for i, a := range f.Args {
			if a.Kind == "fields" {
				if fields != NoArg {
					return nil, fmt.Errorf("its result is the product of its fields, and it has two fields arguments")
				}
				fields = argRef(i)
			}
		}
		if fields == NoArg {
			return nil, fmt.Errorf("its result is the product of its fields, and it has no fields argument")
		}
		if hasFrom {
			return nil, fmt.Errorf("symbols_from says the alternatives of a symbol result, and its result is a product")
		}
		return ResultProduct{Fields: fields}, nil
	case "symbol":
		if !hasFrom {
			return nil, fmt.Errorf("its result is a symbol, and symbols_from does not say its alternatives")
		}
		i, ok := f.arg(from)
		if !ok || f.Args[i.Index()].Kind != "value" || f.Args[i.Index()].Type.String() != "list<string>" {
			return nil, fmt.Errorf("symbols_from %s is not a list<string> value argument", from)
		}
		return ResultSymbols{Symbols: i}, nil
	}
	if hasFrom {
		return nil, fmt.Errorf("symbols_from says the alternatives of a symbol result, and its result is %s", result)
	}
	t, err := value.ParseType(result)
	if err != nil {
		return nil, err
	}
	return ResultType{Type: t}, nil
}

// parseReceiver reads a receiver pattern: "*", or a type whose outer kind is not a parameter and
// that does not mention R, which an operation binds to its receiver.
func parseReceiver(s string) (Receiver, error) {
	if s == string(AnyReceiver) {
		return Receiver{Key: AnyReceiver}, nil
	}
	t, err := value.ParseType(s)
	if err != nil {
		return Receiver{}, err
	}
	if t.Kind == value.Param {
		return Receiver{}, fmt.Errorf("receiver %s is a parameter, which has no kind to apply to", s)
	}
	if slices.Contains(t.Params(), "R") {
		return Receiver{}, fmt.Errorf("receiver %s mentions R, which stands for the whole receiver", s)
	}
	return Receiver{Key: ReceiverKey(t.Kind.String()), Pattern: &t}, nil
}

// checkBindings checks that every type parameter a form uses has a place where the types a case
// gives can bind it: the receiver of an operation (R, and the parameters of its pattern), the type
// of a decoder, decoders, variants, encoder or properties argument, and the input or output of a
// fixture argument. The result, the type of a value argument, the types an issue binds and the
// input of an encoder or a property use parameters; a parameter only they mention is one no case
// could ever bind. Whether a case binds a parameter it could is the case's matter.
func (f *Form) checkBindings(section string) error {
	bindable := map[string]bool{}
	bind := func(t value.Type) {
		for _, p := range t.Params() {
			bindable[p] = true
		}
	}
	if section == "operation" {
		bindable["R"] = true
		for _, rc := range f.Receivers {
			if rc.Pattern != nil {
				bind(*rc.Pattern)
			}
		}
	}
	for _, a := range f.Args {
		switch argKinds[a.Kind].Binding {
		case bindsByForms:
			bind(a.Type)
		case bindsByFixture:
			bind(a.FixtureInput)
			if a.FixtureOutput != nil {
				bind(*a.FixtureOutput)
			}
		}
	}
	use := func(what string, t value.Type) error {
		for _, p := range t.Params() {
			if !bindable[p] {
				return fmt.Errorf("%s mentions %s, which nothing a case gives can bind", what, p)
			}
		}
		return nil
	}
	if r, ok := f.Result.(ResultType); ok {
		if err := use("the result", r.Type); err != nil {
			return err
		}
	}
	if err := use("the input", f.Input); err != nil {
		return err
	}
	for _, a := range f.Args {
		if a.Kind == "value" {
			if err := use("argument "+a.Name, a.Type); err != nil {
				return err
			}
		}
	}
	for _, ref := range f.Issues {
		for _, p := range slices.Sorted(maps.Keys(ref.Bind)) {
			if err := use("issue "+ref.Key+" binding "+p, ref.Bind[p]); err != nil {
				return err
			}
		}
	}
	return nil
}

// parseArgs reads a form's arguments: each name once, at most one message argument, and optional
// arguments only at the end of an operation's.
func (f *Form) parseArgs(section string, n *jsontext.Node) error {
	nodes, err := elems(n, "args")
	if err != nil {
		return err
	}
	for _, e := range nodes {
		a, err := parseArg(section, e)
		if err != nil {
			return err
		}
		if _, dup := f.arg(a.Name); dup {
			return fmt.Errorf("two arguments are named %s", a.Name)
		}
		if len(f.Args) > 0 && f.Args[len(f.Args)-1].Optional && !a.Optional {
			return fmt.Errorf("argument %s follows an optional argument", a.Name)
		}
		f.Args = append(f.Args, a)
		if a.Kind == "message" {
			if f.Message != NoArg {
				return fmt.Errorf("arguments %s and %s are both messages", f.Args[f.Message.Index()].Name, a.Name)
			}
			f.Message = argRef(len(f.Args) - 1)
		}
	}
	return nil
}

// parseIssues reads the issues a form declares, each once, and resolves the arguments their
// metadata sources read.
func (f *Form) parseIssues(n *jsontext.Node) error {
	nodes, err := elems(n, "issues")
	if err != nil {
		return err
	}
	for _, e := range nodes {
		ref, err := parseIssueRef(e)
		if err != nil {
			return err
		}
		for _, other := range f.Issues {
			if other.Key == ref.Key {
				return fmt.Errorf("issue %s is declared twice", ref.Key)
			}
		}
		for _, name := range slices.Sorted(maps.Keys(ref.Meta)) {
			src := ref.Meta[name]
			if slices.Contains(ref.Omit, name) {
				return fmt.Errorf("issue %s both omits %s and gives it a source", ref.Key, name)
			}
			if !readsArg(src.Kind) {
				continue
			}
			i, ok := f.arg(src.ArgName)
			kind := "value"
			if src.Kind == "sorted_keys" {
				kind = "variants"
			}
			if !ok || f.Args[i.Index()].Kind != kind {
				return fmt.Errorf("issue %s meta %s reads %s, which is not a %s argument", ref.Key, name, src.ArgName, kind)
			}
			if src.Kind == "ascii_lower_sorted" && f.Args[i.Index()].Type.String() != "list<string>" {
				return fmt.Errorf("issue %s meta %s lower-cases %s, which is not a list<string>", ref.Key, name, src.ArgName)
			}
			if src.Kind == "sorted" && f.Args[i.Index()].Type.Kind != value.List {
				return fmt.Errorf("issue %s meta %s sorts %s, which is not a list", ref.Key, name, src.ArgName)
			}
			src.Arg = i
			ref.Meta[name] = src
		}
		f.Issues = append(f.Issues, ref)
	}
	return nil
}

// arg is the argument with a name; argument names are unique within a form.
func (f *Form) arg(name string) (ArgRef, bool) {
	for i, a := range f.Args {
		if a.Name == name {
			return argRef(i), true
		}
	}
	return NoArg, false
}

func parseArg(section string, n *jsontext.Node) (Arg, error) {
	var a Arg
	if err := object(n, "an argument", "name", "kind", "type", "optional", "default", "one_of", "fixture", "input", "output"); err != nil {
		return a, err
	}
	var err error
	if a.Name, err = n.String("name"); err != nil {
		return a, err
	}
	if a.Kind, err = n.String("kind"); err != nil {
		return a, err
	}
	kind, ok := argKinds[a.Kind]
	if !ok {
		return a, fmt.Errorf("argument %s has unknown kind %q", a.Name, a.Kind)
	}
	t, ok, err := typeOf(n, "type")
	if err != nil {
		return a, err
	}
	switch typed := kind.Typed; {
	case typed && !ok:
		return a, fmt.Errorf("argument %s needs a type", a.Name)
	case !typed && ok:
		return a, fmt.Errorf("argument %s is a %s argument, which has no type", a.Name, a.Kind)
	}
	a.Type = t
	if o, ok := n.Get("optional"); ok {
		if o.Kind != jsontext.Bool {
			return a, fmt.Errorf("optional must be true or false")
		}
		a.Optional = o.Bool
	}
	// A constructor's arguments are read by position before its operations, which are arrays; a
	// trailing message, a string, cannot be mistaken for one, and no other optional argument is
	// allowed there.
	if a.Optional && section != "operation" && !(section == "constructor" && a.Kind == "message") {
		return a, fmt.Errorf("argument %s is optional, and only an operation has optional arguments, or a constructor a trailing message", a.Name)
	}
	if a.Optional && !kind.Omittable {
		return a, fmt.Errorf("argument %s is a %s argument, which cannot be left out", a.Name, a.Kind)
	}
	if _, ok := n.Get("one_of"); ok {
		if !kind.Valued || a.Type.Kind != value.String {
			return a, fmt.Errorf("argument %s restricts its values, and only a string value argument can", a.Name)
		}
		if a.OneOf, err = strs(n, "one_of"); err != nil {
			return a, err
		}
		if len(a.OneOf) == 0 {
			return a, fmt.Errorf("argument %s allows no value", a.Name)
		}
	}
	if d, ok := n.Get("default"); ok {
		if !a.Optional || !kind.Valued {
			return a, fmt.Errorf("argument %s has a default, and only an optional value argument has one", a.Name)
		}
		if ps := a.Type.Params(); len(ps) > 0 {
			return a, fmt.Errorf("argument %s has a default, and its type %s is not concrete", a.Name, a.Type)
		}
		v, err := value.Observe(a.Type, d)
		if err != nil {
			return a, fmt.Errorf("argument %s: default: %w", a.Name, err)
		}
		if len(a.OneOf) > 0 && !slices.Contains(a.OneOf, v.Str) {
			return a, fmt.Errorf("argument %s: the default %s is not one of its values", a.Name, v.Str)
		}
		a.Default = &v
	} else if a.Optional && kind.Valued {
		return a, fmt.Errorf("argument %s may be left out, and has no default to stand for it", a.Name)
	}
	_, hasFixture := n.Get("fixture")
	_, hasInput := n.Get("input")
	_, hasOutput := n.Get("output")
	if a.Kind != "fixture" {
		if hasFixture || hasInput || hasOutput {
			return a, fmt.Errorf("argument %s is not a fixture argument, and has fixture, input or output", a.Name)
		}
		return a, nil
	}
	if a.FixtureKind, err = n.String("fixture"); err != nil {
		return a, err
	}
	sig, ok := fixtureSignatures[a.FixtureKind]
	if !ok {
		return a, fmt.Errorf("fixture argument %s has unknown fixture kind %q", a.Name, a.FixtureKind)
	}
	in, ok, err := typeOf(n, "input")
	if err != nil {
		return a, err
	}
	if !ok {
		return a, fmt.Errorf("fixture argument %s needs an input type", a.Name)
	}
	a.FixtureInput = in
	if out, ok, err := typeOf(n, "output"); err != nil {
		return a, err
	} else if ok {
		a.FixtureOutput = &out
	}
	if (a.FixtureOutput != nil) != sig.Output {
		return a, fmt.Errorf("fixture argument %s: %s", a.Name, sig.outputRule(a.FixtureKind))
	}
	return a, nil
}

// outputRule says whether fixtures of a kind have an output type.
func (sig fixtureSignature) outputRule(kind string) string {
	if sig.Output {
		return "a " + kind + " fixture has an output type"
	}
	return "a " + kind + " fixture has no output type"
}

func parseIssueRef(n *jsontext.Node) (IssueRef, error) {
	ref := IssueRef{Bind: map[string]value.Type{}}
	switch n.Kind {
	case jsontext.String:
		ref.Key = n.Text
	case jsontext.Object:
		if _, ok := n.Get("key"); !ok {
			return ref, fmt.Errorf("an issue reference has no key")
		}
	default:
		return ref, fmt.Errorf("an issue reference is a key or an object")
	}
	for _, m := range n.Members {
		switch {
		case m.Name == "key":
			if m.Value.Kind != jsontext.String {
				return ref, fmt.Errorf("key must be a string")
			}
			ref.Key = m.Value.Text
		case m.Name == "omit":
			omit, err := strs(n, "omit")
			if err != nil {
				return ref, err
			}
			ref.Omit = omit
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
		case len(m.Name) == 1 && m.Name[0] >= 'A' && m.Name[0] <= 'Z':
			if m.Value.Kind != jsontext.String {
				return ref, fmt.Errorf("%s must be bound to a type", m.Name)
			}
			t, err := value.ParseType(m.Value.Text)
			if err != nil {
				return ref, err
			}
			ref.Bind[m.Name] = t
		default:
			return ref, fmt.Errorf("an issue reference has no member %q", m.Name)
		}
	}
	if ref.Key == "" {
		return ref, fmt.Errorf("an issue reference has an empty key")
	}
	return ref, nil
}

func parseFixture(name string, n *jsontext.Node) (*Fixture, error) {
	if err := object(n, "a fixture", "kind", "doc", "input", "output", "issue"); err != nil {
		return nil, err
	}
	f := &Fixture{Name: name}
	var err error
	if f.Kind, err = n.String("kind"); err != nil {
		return nil, err
	}
	sig, ok := fixtureSignatures[f.Kind]
	if !ok {
		return nil, fmt.Errorf("unknown kind %q", f.Kind)
	}
	if f.Doc, err = n.String("doc"); err != nil {
		return nil, err
	}
	in, ok, err := typeOf(n, "input")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no input type")
	}
	f.Input = in
	if out, ok, err := typeOf(n, "output"); err != nil {
		return nil, err
	} else if ok {
		f.Output = &out
	}
	if f.Output != nil {
		if err := fixtureParams("the output", *f.Output, in); err != nil {
			return nil, err
		}
	}
	if (f.Output != nil) != sig.Output {
		return nil, fmt.Errorf("%s", sig.outputRule(f.Kind))
	}
	if is, ok := n.Get("issue"); ok {
		if err := object(is, "the issue", "code", "message_key", "message", "path", "meta", "values"); err != nil {
			return nil, err
		}
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
			if meta.Kind != jsontext.Object {
				return nil, fmt.Errorf("meta must be an object of types")
			}
			for _, m := range meta.Members {
				if m.Value.Kind != jsontext.String {
					return nil, fmt.Errorf("meta %s must be a type", m.Name)
				}
				t, err := value.ParseType(m.Value.Text)
				if err != nil {
					return nil, err
				}
				if err := fixtureParams("meta "+m.Name, t, in); err != nil {
					return nil, err
				}
				fi.Meta[m.Name] = t
			}
		}
		if fi.Code == "" || fi.Key == "" {
			return nil, fmt.Errorf("the issue needs a non-empty code and message_key")
		}
		if values, ok := is.Get("values"); ok {
			if values.Kind != jsontext.Object {
				return nil, fmt.Errorf("values must be an object")
			}
			for _, m := range values.Members {
				if _, ok := fi.Meta[m.Name]; !ok {
					return nil, fmt.Errorf("values gives %s, which meta does not have", m.Name)
				}
				if m.Value.Kind != jsontext.String || m.Value.Text != "input" {
					return nil, fmt.Errorf(`values: the only source of a value is "input"`)
				}
			}
		}
		if len(fi.Path) > 0 && !sig.Path {
			return nil, fmt.Errorf("a %s fixture gives its issue at the path it runs at", f.Kind)
		}
		f.Issue = fi
	}
	if (f.Issue != nil) != sig.Issue {
		return nil, fmt.Errorf("a %s fixture %s", f.Kind, map[bool]string{true: "declares the issue it gives", false: "gives no issue"}[sig.Issue])
	}
	return f, nil
}

// fixtureParams checks that a type of a fixture mentions only parameters of its input: a fixture
// takes one value, so once its input is known, its output and its issue's metadata are too.
func fixtureParams(what string, t, input value.Type) error {
	for _, p := range t.Params() {
		if !slices.Contains(input.Params(), p) {
			return fmt.Errorf("%s mentions %s, which the input does not", what, p)
		}
	}
	return nil
}

// convertsAString names the constructors whose own issue is about a value, the string their string
// decoder reads, and that take a message for it (spec/decoder-language.md): enum and literal. Every
// other constructor builds structure.
func convertsAString(constructor string) bool {
	return constructor == "enum" || constructor == "literal"
}

// MessageFacetID writes the ID of the message facet of a form's feature: operation.int32.min.message
// is giving min on an int32 a message. The suffix is how a facet is written, not how one is
// recognized: Facets says which IDs are facets and of what, and Parse refuses a facet ID that is
// also another feature's.
func MessageFacetID(feature string) string { return feature + ".message" }

// Facet is a feature that is a capability of a form rather than a form: giving Form its message.
// Parent is the form's feature.
type Facet struct {
	ID, Parent string
	Form       *Form
}

// Facets returns the message facet of every form that takes a message, by ID order of the parent.
func (r *Registry) Facets() []Facet {
	var out []Facet
	for _, name := range slices.Sorted(maps.Keys(r.Constructors)) {
		if f := r.Constructors[name]; f.Message != NoArg {
			out = append(out, Facet{ID: MessageFacetID("decoder." + name), Parent: "decoder." + name, Form: f})
		}
	}
	for _, name := range slices.Sorted(maps.Keys(r.Operations)) {
		overloads := r.Operations[name]
		for _, key := range slices.Sorted(maps.Keys(overloads)) {
			if f := overloads[key].Form; f.Message != NoArg {
				parent := operationFeature(key, name)
				out = append(out, Facet{ID: MessageFacetID(parent), Parent: parent, Form: f})
			}
		}
	}
	return out
}

// Features returns every feature ID the registry defines, sorted: each form's, and each facet.
func (r *Registry) Features() []string {
	var ids []string
	for _, name := range slices.Sorted(maps.Keys(r.Constructors)) {
		ids = append(ids, "decoder."+name)
	}
	for _, name := range slices.Sorted(maps.Keys(r.Fields)) {
		ids = append(ids, "field."+name)
	}
	for _, name := range slices.Sorted(maps.Keys(r.Operations)) {
		overloads := r.Operations[name]
		for _, key := range slices.Sorted(maps.Keys(overloads)) {
			ids = append(ids, operationFeature(key, name))
		}
	}
	for _, f := range r.Facets() {
		ids = append(ids, f.ID)
	}
	for _, name := range slices.Sorted(maps.Keys(r.Encoders)) {
		ids = append(ids, "encoder."+name)
	}
	for _, name := range slices.Sorted(maps.Keys(r.Properties)) {
		ids = append(ids, "property."+name)
	}
	for _, name := range slices.Sorted(maps.Keys(r.Fixtures)) {
		ids = append(ids, "fixture."+name)
	}
	sort.Strings(ids)
	return ids
}

// operationFeature names the feature of an operation's overload for a receiver key.
func operationFeature(key ReceiverKey, name string) string {
	if key == AnyReceiver {
		return "operation.any." + name
	}
	return "operation." + string(key) + "." + name
}
