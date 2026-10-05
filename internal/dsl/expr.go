package dsl

import (
	"fmt"
	"maps"
	"slices"

	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/value"
)

// ArgRef refers to an argument of a form: its index plus one, so that the zero ArgRef refers to
// nothing and a reference nobody set is never taken for the first argument.
type ArgRef int

// NoArg is the zero ArgRef, which refers to no argument.
const NoArg ArgRef = 0

func argRef(index int) ArgRef { return ArgRef(index + 1) }

// Index is the index of the argument; it panics for NoArg.
func (r ArgRef) Index() int {
	if r == NoArg {
		panic("NoArg has no index")
	}
	return int(r) - 1
}

// IssueIndex refers to one of a form's own issues, as ArgRef refers to an argument.
type IssueIndex int

func issueIndex(index int) IssueIndex { return IssueIndex(index + 1) }

// Index is the index of the issue.
func (r IssueIndex) Index() int {
	if r == 0 {
		panic("the zero IssueIndex has no index")
	}
	return int(r) - 1
}

// Expr is a form's flow expression, as catalog/operations.json declares it, with every reference
// to an argument or an issue resolved to its index when the registry is loaded. See
// spec/issues.md for what each means.
type Expr interface{ isExpr() }

// ExprOwn is the form's own issues that no other part of its expression names: at most one of
// them, or none.
type ExprOwn struct{ Issues []IssueIndex }

// ExprNone gives no issue.
type ExprNone struct{}

// ExprArg is the flow of a decoder, variants or fields argument.
type ExprArg struct{ Arg ArgRef }

// ExprCat is its parts' issues one after the other.
type ExprCat struct{ Items []Expr }

// ExprChain is its parts' issues, stopping after the first part that gives any.
type ExprChain struct{ Items []Expr }

// ExprEach is its body for each element (Over Elements) or member (Over Members) of the input.
type ExprEach struct {
	Over Over
	Body Expr
}

// ExprAt is its body at the member a string value argument names.
type ExprAt struct {
	Member ArgRef
	Body   Expr
}

// ExprUnknown is After's issues, then one issue for each member of an object input the form does
// not know and After has not already reported unknown, in any order. The
// known members are the strings of a value argument (KnownArg) or the members a fields argument
// reads (KnownFields); the other is NoArg.
type ExprUnknown struct {
	After       Expr
	KnownArg    ArgRef
	KnownFields ArgRef
	Issue       IssueIndex
}

// ExprCandidates is an issue that lists, for every decoder of a decoders argument, the issues it
// gave, in the metadata entry Meta.
type ExprCandidates struct {
	Decoders ArgRef
	Issue    IssueIndex
	Meta     string
}

// ExprForm is the flow of a decoder form written in the catalogue itself, such as the string decoder
// discriminate reads its tag with. The form is part of the meaning of the form that embeds it, not
// a feature a case uses; NewChecker checks that it type-checks and that no form embeds itself.
type ExprForm struct{ Form *jsontext.Node }

// ExprFixture is the issue the fixture a fixture argument names declares.
type ExprFixture struct{ Arg ArgRef }

// ExprDiscard gives no issue, and says that the issues of the arguments listed never
// reach the form's caller.
type ExprDiscard struct{ Args []ArgRef }

func (ExprOwn) isExpr()        {}
func (ExprNone) isExpr()       {}
func (ExprArg) isExpr()        {}
func (ExprCat) isExpr()        {}
func (ExprChain) isExpr()      {}
func (ExprEach) isExpr()       {}
func (ExprAt) isExpr()         {}
func (ExprUnknown) isExpr()    {}
func (ExprCandidates) isExpr() {}
func (ExprFixture) isExpr()    {}
func (ExprForm) isExpr()       {}
func (ExprDiscard) isExpr()    {}

// exprResolver resolves the names a form's expression uses.
type exprResolver struct {
	f *Form
	// named are the own issues the expression names; flowRefs counts references to each argument
	// whose issues flow; own counts the "own" the expression uses.
	named    map[int]bool
	flowRefs map[ArgRef]int
	own      int
}

func (r *exprResolver) arg(n *jsontext.Node, kinds ...string) (ArgRef, error) {
	return r.argWhere(n, fmt.Sprintf("of kind %v", kinds), func(a Arg) bool { return slices.Contains(kinds, a.Kind) })
}

// argWhere resolves the argument a flow names, which has to be one that is what describes.
func (r *exprResolver) argWhere(n *jsontext.Node, describes string, is func(Arg) bool) (ArgRef, error) {
	if n.Kind != jsontext.String {
		return NoArg, fmt.Errorf("the flow names an argument with a string")
	}
	i, ok := r.f.arg(n.Text)
	if !ok {
		return NoArg, fmt.Errorf("the flow names %s, which is not an argument", n.Text)
	}
	if a := r.f.Args[i.Index()]; !is(a) {
		return NoArg, fmt.Errorf("argument %s is a %s argument, and the flow needs one %s", a.Name, a.Kind, describes)
	}
	return i, nil
}

func (r *exprResolver) issue(n *jsontext.Node) (IssueIndex, error) {
	if n.Kind != jsontext.String {
		return 0, fmt.Errorf("the flow names an issue with a string")
	}
	key := n.Text
	for i, ref := range r.f.Issues {
		if ref.Key == key {
			if r.named[i] {
				return 0, fmt.Errorf("the flow names issue %s twice", key)
			}
			r.named[i] = true
			return issueIndex(i), nil
		}
	}
	return 0, fmt.Errorf("the flow names issue %s, which is not among the form's issues", key)
}

func (r *exprResolver) parse(n *jsontext.Node) (Expr, error) {
	if n.Kind == jsontext.String {
		switch n.Text {
		case "own":
			r.own++
			return &ExprOwn{}, nil
		case "none":
			return ExprNone{}, nil
		}
		return nil, fmt.Errorf("%q is not a flow", n.Text)
	}
	if n.Kind != jsontext.Object || len(n.Members) != 1 {
		return nil, fmt.Errorf("a flow is \"own\", \"none\" or an object with one member")
	}
	m := n.Members[0]
	v := m.Value
	switch m.Name {
	case "arg":
		i, err := r.argWhere(v, "whose issues {\"arg\": ...} places", func(a Arg) bool { return argKinds[a.Kind].FlowArg })
		if err != nil {
			return nil, err
		}
		r.flowRefs[i]++
		return ExprArg{Arg: i}, nil
	case "cat", "chain":
		// There is no alt: a form cannot skip a decoder it runs. What excludes each other is a
		// form's own issues, and the variants of a variants argument.
		if v.Kind != jsontext.Array || len(v.Elems) < 2 {
			return nil, fmt.Errorf("%s is an array of two flows or more", m.Name)
		}
		var items []Expr
		for _, e := range v.Elems {
			x, err := r.parse(e)
			if err != nil {
				return nil, err
			}
			items = append(items, x)
		}
		if m.Name == "cat" {
			return ExprCat{Items: items}, nil
		}
		return ExprChain{Items: items}, nil
	case "each_element", "each_member":
		body, err := r.parse(v)
		if err != nil {
			return nil, err
		}
		over := Elements
		if m.Name == "each_member" {
			over = Members
		}
		return ExprEach{Over: over, Body: body}, nil
	case "at":
		if err := object(v, "at", "member", "flow"); err != nil {
			return nil, err
		}
		member, err := v.Member("member")
		if err != nil {
			return nil, err
		}
		i, err := r.arg(member, "value")
		if err != nil {
			return nil, err
		}
		if t := r.f.Args[i.Index()].Type; t.Kind != value.String {
			return nil, fmt.Errorf("at names %s, which is a %s, not a string", member.Text, t)
		}
		body, err := v.Member("flow")
		if err != nil {
			return nil, err
		}
		b, err := r.parse(body)
		if err != nil {
			return nil, err
		}
		return ExprAt{Member: i, Body: b}, nil
	case "unknown_members":
		if err := object(v, "unknown_members", "after", "known", "issue"); err != nil {
			return nil, err
		}
		x := ExprUnknown{}
		after, err := v.Member("after")
		if err != nil {
			return nil, err
		}
		if x.After, err = r.parse(after); err != nil {
			return nil, err
		}
		known, err := v.Member("known")
		if err != nil {
			return nil, err
		}
		if known.Kind != jsontext.Object || len(known.Members) != 1 {
			return nil, fmt.Errorf("the known members come from an arg or from fields")
		}
		switch k := known.Members[0]; k.Name {
		case "arg":
			if x.KnownArg, err = r.arg(k.Value, "value"); err != nil {
				return nil, err
			}
			if t := r.f.Args[x.KnownArg.Index()].Type; t.Kind != value.List || t.Args[0].Kind != value.String {
				return nil, fmt.Errorf("the known members come from %s, which is a %s, not a list<string>", k.Value.Text, t)
			}
		case "fields":
			if x.KnownFields, err = r.arg(k.Value, "fields"); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("the known members come from an arg or from fields")
		}
		key, err := v.Member("issue")
		if err != nil {
			return nil, err
		}
		if x.Issue, err = r.issue(key); err != nil {
			return nil, err
		}
		return x, nil
	case "candidates":
		if err := object(v, "candidates", "decoders", "issue", "meta"); err != nil {
			return nil, err
		}
		x := ExprCandidates{}
		decoders, err := v.Member("decoders")
		if err != nil {
			return nil, err
		}
		if x.Decoders, err = r.arg(decoders, "decoders"); err != nil {
			return nil, err
		}
		r.flowRefs[x.Decoders]++
		key, err := v.Member("issue")
		if err != nil {
			return nil, err
		}
		if x.Issue, err = r.issue(key); err != nil {
			return nil, err
		}
		if x.Meta, err = v.String("meta"); err != nil {
			return nil, err
		}
		// The form does not decide the candidates, and cannot leave them out.
		ref := r.f.Issues[x.Issue.Index()]
		if _, ok := ref.Meta[x.Meta]; ok || slices.Contains(ref.Omit, x.Meta) {
			return nil, fmt.Errorf("issue %s lists the candidates in %s, which the form gives a source or omits", ref.Key, x.Meta)
		}
		return x, nil
	case "form":
		if v.Kind != jsontext.Array || len(v.Elems) == 0 || v.Elems[0].Kind != jsontext.String {
			return nil, fmt.Errorf("form is a decoder form, [name, ...]")
		}
		return ExprForm{Form: v}, nil
	case "fixture":
		i, err := r.arg(v, "fixture")
		if err != nil {
			return nil, err
		}
		if a := r.f.Args[i.Index()]; !producesIssues(a) {
			return nil, fmt.Errorf("fixture %s is a %s fixture, which gives no issue", a.Name, a.FixtureKind)
		}
		r.flowRefs[i]++
		return ExprFixture{Arg: i}, nil
	case "discard":
		if v.Kind != jsontext.Array || len(v.Elems) == 0 {
			return nil, fmt.Errorf("discard is a non-empty array of arguments")
		}
		x := ExprDiscard{}
		for _, e := range v.Elems {
			i, err := r.argWhere(e, "that gives issues", producesIssues)
			if err != nil {
				return nil, err
			}
			r.flowRefs[i]++
			x.Args = append(x.Args, i)
		}
		return x, nil
	}
	return nil, fmt.Errorf("%q is not a flow", m.Name)
}

// resolveFlow reads a form's flow expression and checks that it accounts for the form: every
// argument that gives issues is placed or discarded exactly once, every own issue is
// given by exactly one part, and a metadata source that reads a member name belongs to an issue
// given at members.
func resolveFlow(f *Form, n *jsontext.Node) (Expr, error) {
	r := &exprResolver{f: f, named: map[int]bool{}, flowRefs: map[ArgRef]int{}}
	x, err := r.parse(n)
	if err != nil {
		return nil, err
	}
	for i, a := range f.Args {
		if producesIssues(a) && r.flowRefs[argRef(i)] != 1 {
			return nil, fmt.Errorf("the flow places the issues of argument %s %d times, not once", a.Name, r.flowRefs[argRef(i)])
		}
	}
	var rest []IssueIndex
	for i := range f.Issues {
		if !r.named[i] {
			rest = append(rest, issueIndex(i))
		}
	}
	switch {
	case r.own > 1:
		return nil, fmt.Errorf("the flow uses own %d times", r.own)
	case r.own == 0 && len(rest) > 0:
		return nil, fmt.Errorf("the flow gives none of the issues %v", issueKeys(f, rest))
	}
	setOwn(x, rest)
	unknown := map[int]bool{}
	walkExpr(x, func(e Expr) {
		if u, ok := e.(ExprUnknown); ok {
			unknown[u.Issue.Index()] = true
		}
	})
	for i, ref := range f.Issues {
		for _, name := range slices.Sorted(maps.Keys(ref.Meta)) {
			src := ref.Meta[name]
			if src.Kind == "member" && !unknown[i] {
				return nil, fmt.Errorf("issue %s: %s is the name of a member only for an issue given at members", ref.Key, name)
			}
		}
	}
	return x, nil
}

func issueKeys(f *Form, idx []IssueIndex) []string {
	var keys []string
	for _, i := range idx {
		keys = append(keys, f.Issues[i.Index()].Key)
	}
	return keys
}

func setOwn(x Expr, rest []IssueIndex) {
	walkExpr(x, func(e Expr) {
		if o, ok := e.(*ExprOwn); ok {
			o.Issues = rest
		}
	})
}

func walkExpr(x Expr, visit func(Expr)) {
	visit(x)
	switch x := x.(type) {
	case ExprCat:
		for _, i := range x.Items {
			walkExpr(i, visit)
		}
	case ExprChain:
		for _, i := range x.Items {
			walkExpr(i, visit)
		}
	case ExprEach:
		walkExpr(x.Body, visit)
	case ExprAt:
		walkExpr(x.Body, visit)
	case ExprUnknown:
		walkExpr(x.After, visit)
	}
}
