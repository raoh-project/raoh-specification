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
	Issues []TypedIssue
	// Catalog is the issue catalogue the case was read with, which reads an implementation's
	// issues for it too.
	Catalog *catalog.Catalog
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
	c := &Case{File: file, Profile: profile, Catalog: chk.Catalog()}
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
		if !Succeeds(c.Checked.Flow, c.Input) {
			return nil, fmt.Errorf("ok: the decoder gives issues for this input whatever it does")
		}
		return c, nil
	}
	list, err := ParseIssues(issues)
	if err != nil {
		return nil, err
	}
	if c.Issues, err = ReadIssues(list, c.Checked.Flow, c.Input, nil, chk.Catalog(), CaseIssues); err != nil {
		return nil, err
	}
	return c, nil
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
