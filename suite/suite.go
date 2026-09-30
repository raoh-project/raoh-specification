package suite

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/raoh-project/raoh-specification/catalog"
	"github.com/raoh-project/raoh-specification/dsl"
	"github.com/raoh-project/raoh-specification/jsontext"
	"github.com/raoh-project/raoh-specification/value"
)

// Profiles whose cases are in suite/<profile>/.
var CaseProfiles = []string{"core", "encode"}

// ExpectedIssue is an issue a case expects, with what the verifier needs to compare it.
type ExpectedIssue struct {
	Issue
	// Possible is the issue of the decoder it is an instance of.
	Possible dsl.Possible
	// Meta holds the metadata values, typed.
	Meta map[string]value.Value
	// Message is the message: the one the case gives, or the one derived from the catalogue.
	Message string
}

// Case is one conformance case.
type Case struct {
	ID      string
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

var idPattern = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)+$`)

// Suite is every case, in the order of their files and positions.
type Suite struct {
	Cases []*Case
	ByID  map[string]*Case
}

// Load reads every case under root/suite.
func Load(root string, chk *dsl.Checker) (*Suite, error) {
	s := &Suite{ByID: map[string]*Case{}}
	var problems []string
	for _, profile := range CaseProfiles {
		dir := filepath.Join(root, "suite", profile)
		var files []string
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".json") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		sort.Strings(files)
		for _, path := range files {
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			text, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			cases, errs := ParseFile(rel, profile, text, chk)
			problems = append(problems, errs...)
			for _, c := range cases {
				if other, dup := s.ByID[c.ID]; dup {
					problems = append(problems, fmt.Sprintf("%s: %s: the ID is also used in %s", rel, c.ID, other.File))
					continue
				}
				s.ByID[c.ID] = c
				s.Cases = append(s.Cases, c)
			}
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
	decoderMembers = []string{"id", "decoder", "input", "ok", "issues"}
	encoderMembers = []string{"id", "encoder", "value", "ok"}
)

func parseCase(file, profile string, n *jsontext.Node, chk *dsl.Checker) (*Case, error) {
	if n.Kind != jsontext.Object {
		return nil, fmt.Errorf("expected an object")
	}
	c := &Case{File: file, Profile: profile}
	id, ok := n.Get("id")
	if !ok || id.Kind != jsontext.String || !idPattern.MatchString(id.Text) {
		return nil, fmt.Errorf("id must be dot-separated lower snake case, such as string.min_length.too_short")
	}
	c.ID = id.Text
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
	var err error
	if c.Encoder {
		c.Form, _ = n.Get("encoder")
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
	c.Form, _ = n.Get("decoder")
	if c.Form == nil {
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

// Expect finds the possible issue an issue is an instance of, types its metadata, and settles its
// message.
func Expect(is Issue, checked *dsl.Checked, cat *catalog.Catalog) (ExpectedIssue, error) {
	var tried []string
	for _, p := range checked.Issues {
		if p.Key != is.Key {
			continue
		}
		e, err := instance(is, p, cat)
		if err == nil {
			return e, nil
		}
		tried = append(tried, err.Error())
	}
	if len(tried) == 0 {
		return ExpectedIssue{}, fmt.Errorf("the decoder cannot give %s", is.Key)
	}
	return ExpectedIssue{}, fmt.Errorf("%s", strings.Join(tried, "; "))
}

func instance(is Issue, p dsl.Possible, cat *catalog.Catalog) (ExpectedIssue, error) {
	e := ExpectedIssue{Issue: is, Possible: p, Meta: map[string]value.Value{}}
	if is.Code != p.Code {
		return e, fmt.Errorf("%s has code %s, not %s", is.Key, p.Code, is.Code)
	}
	for _, m := range is.Meta.Members {
		t, ok := p.Meta[m.Name]
		if !ok {
			return e, fmt.Errorf("%s has no metadata %s", is.Key, m.Name)
		}
		v, err := value.Observe(t, m.Value)
		if err != nil {
			return e, fmt.Errorf("%s meta %s: %w", is.Key, m.Name, err)
		}
		e.Meta[m.Name] = v
	}
	for name := range p.Meta {
		if _, ok := e.Meta[name]; !ok && !slices.Contains(p.Optional, name) {
			return e, fmt.Errorf("%s needs metadata %s", is.Key, name)
		}
	}
	switch {
	case is.Message != nil:
		e.Message = *is.Message
	case p.Message != "":
		return e, fmt.Errorf("%s is given by a fixture, whose message the case has to write", is.Key)
	default:
		m, err := Derive(is.Key, is.Code, e.Meta, cat)
		if err != nil {
			return e, err
		}
		e.Message = m
	}
	return e, nil
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
