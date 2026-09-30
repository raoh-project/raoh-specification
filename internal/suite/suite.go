package suite

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/raoh-project/raoh-specification/internal/artifacts"
	"github.com/raoh-project/raoh-specification/internal/catalog"
	"github.com/raoh-project/raoh-specification/internal/dsl"
	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/schemas"
	"github.com/raoh-project/raoh-specification/internal/value"
)

// ExpectedIssue is an issue a case expects, with what the verifier needs to compare it.
type ExpectedIssue struct {
	Issue
	// Site is where in the decoder it arises.
	Site dsl.Located
	// Meta holds the metadata values, typed.
	Meta map[string]value.Value
	// Message is the message: the one the case gives, or the one derived from the catalogue.
	Message string
	// Group identifies, for an issue whose site is unordered, the object whose members order it;
	// empty for an ordered issue.
	Group string
	// Candidates are, for one_of_failed, the issues each candidate gave, by candidate index, and
	// CandidatesMeta the metadata entry that lists them.
	Candidates     map[int][]ExpectedIssue
	CandidatesMeta string
}

// Case is one conformance case.
type Case struct {
	ID string
	// Title says what the case checks.
	Title   string
	Profile string
	File    string
	// Encoder is set for an encoding case.
	Encoder bool
	Form    *jsontext.Node
	Checked *dsl.Checked
	// Input is a decoding case's input, or an encoding case's value observation.
	Input *jsontext.Node
	// OK is the observation of the result a case expects, when it expects success.
	OK *jsontext.Node
	// OKValue is OK read as a value of the result type; unset for an encoding case, whose output
	// is JSON.
	OKValue value.Value
	// Issues are the issues a case expects, when it expects failure.
	Issues []ExpectedIssue
}

// Features returns the features the case needs.
func (c *Case) Features() []string { return c.Checked.Features }

// IDPattern is what a case ID looks like: R and six digits, meaning nothing.
var IDPattern = regexp.MustCompile(`^R[0-9]{6}$`)

// Suite is every case, in the order of their files and positions, and the IDs retired from it.
type Suite struct {
	Cases   []*Case
	ByID    map[string]*Case
	Retired []string
}

// Load reads every case file of the specification under root, each checked against the case
// schema first.
func Load(root string, chk *dsl.Checker, sch *schemas.Set) (*Suite, error) {
	list, err := artifacts.List(root)
	if err != nil {
		return nil, err
	}
	s := &Suite{ByID: map[string]*Case{}}
	var problems []string
	retired, err := os.ReadFile(filepath.Join(root, "suite", "retired.json"))
	if err != nil {
		return nil, err
	}
	if err := sch.Validate("retired", retired); err != nil {
		return nil, fmt.Errorf("suite/retired.json: %w", err)
	}
	ids, err := jsontext.Parse(retired)
	if err != nil {
		return nil, fmt.Errorf("suite/retired.json: %w", err)
	}
	for _, e := range ids.Elems {
		s.Retired = append(s.Retired, e.Text)
	}
	for _, a := range artifacts.OfKind(list, artifacts.CaseFile) {
		text, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(a.Path)))
		if err != nil {
			return nil, err
		}
		if err := sch.Validate("case", text); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", a.Path, err))
			continue
		}
		cases, errs := ParseFile(a.Path, a.Profile, text, chk)
		problems = append(problems, errs...)
		for _, c := range cases {
			if slices.Contains(s.Retired, c.ID) {
				problems = append(problems, fmt.Sprintf("%s: %s is retired and cannot be used again", a.Path, c.ID))
				continue
			}
			if other, dup := s.ByID[c.ID]; dup {
				problems = append(problems, fmt.Sprintf("%s: %s: the ID is also used in %s", a.Path, c.ID, other.File))
				continue
			}
			s.ByID[c.ID] = c
			s.Cases = append(s.Cases, c)
		}
	}
	if len(problems) > 0 {
		if len(problems) > 50 {
			problems = append(problems[:50], fmt.Sprintf("... and %d more", len(problems)-50))
		}
		return nil, fmt.Errorf("the suite has problems:\n  %s", strings.Join(problems, "\n  "))
	}
	return s, nil
}

// ParseFile reads a file of cases: a JSON array of case objects.
func ParseFile(name, profile string, text []byte, chk *dsl.Checker) ([]*Case, []string) {
	root, err := jsontext.Parse(text)
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: %v", name, err)}
	}
	if root.Kind != jsontext.Array {
		return nil, []string{fmt.Sprintf("%s: expected an array of cases", name)}
	}
	var cases []*Case
	var problems []string
	for i, n := range root.Elems {
		c, err := parseCase(name, profile, n, chk)
		if err != nil {
			id := fmt.Sprintf("case %d", i)
			if v, ok := n.Get("id"); ok && v.Kind == jsontext.String {
				id = v.Text
			}
			problems = append(problems, fmt.Sprintf("%s: %s: %v", name, id, err))
			continue
		}
		cases = append(cases, c)
	}
	return cases, problems
}

var (
	decoderMembers = []string{"id", "title", "decoder", "input", "ok", "issues"}
	encoderMembers = []string{"id", "title", "encoder", "value", "ok"}
)

func parseCase(file, profile string, n *jsontext.Node, chk *dsl.Checker) (*Case, error) {
	if n.Kind != jsontext.Object {
		return nil, fmt.Errorf("expected an object")
	}
	c := &Case{File: file, Profile: profile}
	id, ok := n.Get("id")
	if !ok || id.Kind != jsontext.String || !IDPattern.MatchString(id.Text) {
		return nil, fmt.Errorf("id must be R and six digits, such as R000123")
	}
	c.ID = id.Text
	title, err := n.String("title")
	if err != nil || strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("a case needs a title saying what it checks")
	}
	c.Title = title
	_, c.Encoder = n.Get("encoder")
	allowed := decoderMembers
	if c.Encoder {
		allowed = encoderMembers
	}
	for _, name := range n.Names() {
		if !slices.Contains(allowed, name) {
			return nil, fmt.Errorf("unknown member %q", name)
		}
	}
	if c.Encoder != (profile == "encode") {
		return nil, fmt.Errorf("decoding cases belong in suite/core and encoding cases in suite/encode")
	}
	if c.Encoder {
		if c.Form, err = n.Member("encoder"); err != nil {
			return nil, err
		}
		if c.Checked, err = chk.CheckEncoder(c.Form); err != nil {
			return nil, err
		}
		var ok bool
		if c.Input, ok = n.Get("value"); !ok {
			return nil, fmt.Errorf("an encoding case needs a value")
		}
		if _, err := value.Observe(c.Checked.Result, c.Input); err != nil {
			return nil, fmt.Errorf("value: %w", err)
		}
		if c.OK, ok = n.Get("ok"); !ok {
			return nil, fmt.Errorf("an encoding case needs ok")
		}
		return c, nil
	}
	if c.Form, err = n.Member("decoder"); err != nil {
		return nil, fmt.Errorf("a case needs a decoder or an encoder")
	}
	if c.Checked, err = chk.CheckDecoder(c.Form); err != nil {
		return nil, err
	}
	if c.Input, ok = n.Get("input"); !ok {
		return nil, fmt.Errorf("a decoding case needs an input")
	}
	okNode, hasOK := n.Get("ok")
	issues, hasIssues := n.Get("issues")
	if hasOK == hasIssues {
		return nil, fmt.Errorf("a decoding case expects either ok or issues")
	}
	if hasOK {
		c.OK = okNode
		if c.OKValue, err = value.Observe(c.Checked.Result, okNode); err != nil {
			return nil, fmt.Errorf("ok: %w", err)
		}
		return c, nil
	}
	list, err := ParseIssues(issues)
	if err != nil {
		return nil, err
	}
	for i, is := range list {
		e, err := Expect(is, c.Checked, chk.Catalog)
		if err != nil {
			return nil, fmt.Errorf("issue %d: %w", i, err)
		}
		c.Issues = append(c.Issues, e)
	}
	return c, nil
}

// Expect finds the site in the decoder's flow an issue arises at, types its metadata, and
// settles its message. The site is found by the issue's path, message key and code; an issue
// that two sites fit, with different types, messages or ordering, is ambiguous, and so is a case
// that expects it.
func Expect(is Issue, checked *dsl.Checked, cat *catalog.Catalog) (ExpectedIssue, error) {
	return expectAmong(is, checked.Sites, nil, cat)
}

// expectAmong finds an issue among sites. base is the path the sites are relative to.
func expectAmong(is Issue, sites []dsl.Located, base []string, cat *catalog.Catalog) (ExpectedIssue, error) {
	path, err := SplitPath(is.Path)
	if err != nil {
		return ExpectedIssue{}, err
	}
	if len(path) < len(base) || !slices.Equal(path[:len(base)], base) {
		return ExpectedIssue{}, fmt.Errorf("%s at %q is not below %q", is.Key, is.Path, JoinPath(base))
	}
	rel := path[len(base):]
	var fits []ExpectedIssue
	var tried []string
	for _, site := range sites {
		if site.Code != is.Code || (is.Key != "" && site.Key != is.Key) || !dsl.Matches(site.Path, rel) {
			continue
		}
		e, err := instance(is, site, path, cat)
		if err != nil {
			tried = append(tried, err.Error())
			continue
		}
		fits = append(fits, e)
	}
	if len(fits) == 0 {
		if len(tried) == 0 {
			return ExpectedIssue{}, fmt.Errorf("the decoder gives no %s at %q", describeKey(is), is.Path)
		}
		return ExpectedIssue{}, fmt.Errorf("%s", strings.Join(tried, "; "))
	}
	for _, other := range fits[1:] {
		if other.Message != fits[0].Message || !sameTypes(other.Meta, fits[0].Meta) || other.Group != fits[0].Group {
			return ExpectedIssue{}, fmt.Errorf("%s at %q is ambiguous: sites %s and %s of the decoder can give it differently", describeKey(is), is.Path, dsl.PathString(fits[0].Site.Path), dsl.PathString(other.Site.Path))
		}
	}
	return fits[0], nil
}

func describeKey(is Issue) string {
	if is.Key == "" {
		return is.Code
	}
	return is.Key
}

func sameTypes(a, b map[string]value.Value) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		w, ok := b[k]
		if !ok || !v.Type.Same(w.Type) {
			return false
		}
	}
	return true
}

// GroupOf identifies the object whose members order an issue at a site, or is empty.
func GroupOf(site dsl.Located, path []string) string {
	if site.Group == nil {
		return ""
	}
	return "unordered " + JoinPath(path[:len(site.Group)])
}

func instance(is Issue, site dsl.Located, path []string, cat *catalog.Catalog) (ExpectedIssue, error) {
	e := ExpectedIssue{Issue: is, Site: site, Meta: map[string]value.Value{}, Group: GroupOf(site, path)}
	for _, m := range is.Meta.Members {
		t, ok := site.Meta[m.Name]
		if !ok {
			return e, fmt.Errorf("%s has no metadata %s", site.Key, m.Name)
		}
		if t.Kind == value.List && t.Args[0].Kind == value.Record && site.Candidates != nil {
			e.CandidatesMeta = m.Name
			if err := e.candidates(m.Value, site, path, cat); err != nil {
				return e, fmt.Errorf("%s meta %s: %w", site.Key, m.Name, err)
			}
			continue
		}
		v, err := value.Observe(t, m.Value)
		if err != nil {
			return e, fmt.Errorf("%s meta %s: %w", site.Key, m.Name, err)
		}
		e.Meta[m.Name] = v
	}
	for name, want := range site.Fixed {
		got, ok := is.Meta.Get(name)
		if !ok || !value.EqualJSON(want, got) {
			return e, fmt.Errorf("%s from this form always has %s %s", site.Key, name, want.Raw)
		}
	}
	for name := range site.Meta {
		_, typed := e.Meta[name]
		_, nested := is.Meta.Get(name)
		if !typed && !nested && !slices.Contains(site.Optional, name) {
			return e, fmt.Errorf("%s needs metadata %s", site.Key, name)
		}
	}
	switch {
	case is.Key == "":
		// An issue a candidate reported has no message key; its message is whatever the
		// candidate wrote, which the case gives.
		if is.Message == nil {
			return e, fmt.Errorf("an issue a candidate reported has to give its message")
		}
		e.Message = *is.Message
	case site.Message != "":
		if is.Message == nil || *is.Message != site.Message {
			return e, fmt.Errorf("%s is given the message %q, which the case has to write", site.Key, site.Message)
		}
		e.Message = site.Message
	case is.Message != nil:
		return e, fmt.Errorf("the message of %s is derived from the catalogue; leave it out", site.Key)
	default:
		m, err := Derive(site.Key, site.Code, e.Meta, cat)
		if err != nil {
			return e, err
		}
		e.Message = m
	}
	return e, nil
}

// candidates reads the candidates of a one_of_failed: for each, its index and the issues it
// gave, each typed by the flow of that candidate.
func (e *ExpectedIssue) candidates(n *jsontext.Node, site dsl.Located, path []string, cat *catalog.Catalog) error {
	if n.Kind != jsontext.Array {
		return fmt.Errorf("expected an array of candidates")
	}
	e.Candidates = map[int][]ExpectedIssue{}
	for _, c := range n.Elems {
		idx, err := c.Member("candidate")
		if err != nil {
			return err
		}
		i, err := value.Observe(value.Of(value.Int32), idx)
		if err != nil || i.Int.Sign() < 0 || i.Int.Int64() >= int64(len(site.Candidates)) {
			return fmt.Errorf("candidate %s is not an index of the %d candidates", idx.Raw, len(site.Candidates))
		}
		k := int(i.Int.Int64())
		if _, dup := e.Candidates[k]; dup {
			return fmt.Errorf("candidate %d appears twice", k)
		}
		list, err := c.Member("issues")
		if err != nil {
			return err
		}
		nested, err := ParseNestedIssues(list)
		if err != nil {
			return err
		}
		sites := dsl.Sites(site.Candidates[k])
		for j, is := range nested {
			ne, err := expectAmong(is, sites, path, cat)
			if err != nil {
				return fmt.Errorf("candidate %d issue %d: %w", k, j, err)
			}
			e.Candidates[k] = append(e.Candidates[k], ne)
		}
		if len(c.Members) != 2 {
			return fmt.Errorf("a candidate has candidate and issues, and nothing else")
		}
	}
	return nil
}

// Derive derives an issue's message from the English catalogue. See spec/issues.md.
func Derive(key, code string, meta map[string]value.Value, cat *catalog.Catalog) (string, error) {
	template, ok := cat.Messages["en"][key]
	if !ok {
		if template, ok = cat.Messages["en"][code]; !ok {
			return "", fmt.Errorf("the catalogue has no template for %s or %s", key, code)
		}
	}
	var err error
	out := placeholder.ReplaceAllStringFunc(template, func(ph string) string {
		v, ok := meta[ph[1:len(ph)-1]]
		if !ok {
			return ph
		}
		s, ferr := value.FormatForMessage(v)
		if ferr != nil {
			err = ferr
		}
		return s
	})
	return out, err
}

var placeholder = regexp.MustCompile(`\{[A-Za-z0-9_]+\}`)

// CheckIDs checks that a suite keeps the IDs of a base suite, the one a change starts from: every
// case ID of the base is still a case or has been retired, and every retired ID stays retired.
func CheckIDs(base, head *Suite) error {
	var problems []string
	for _, c := range base.Cases {
		if _, ok := head.ByID[c.ID]; !ok && !slices.Contains(head.Retired, c.ID) {
			problems = append(problems, fmt.Sprintf("%s was removed without being added to suite/retired.json", c.ID))
		}
	}
	for _, id := range base.Retired {
		if !slices.Contains(head.Retired, id) {
			problems = append(problems, fmt.Sprintf("%s was retired and is no longer listed in suite/retired.json", id))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("case IDs are not kept:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}
