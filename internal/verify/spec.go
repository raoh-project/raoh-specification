// Package verify classifies what a runner observed against the suite and the implementation's
// declaration, and writes the report. See spec/conformance.md.
package verify

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/raoh-project/raoh-specification/internal/catalog"
	"github.com/raoh-project/raoh-specification/internal/dsl"
	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/manifest"
	"github.com/raoh-project/raoh-specification/internal/suite"
)

// Profiles are every profile, in the order reports list them.
var Profiles = []string{"core", "encode", "messages-en", "messages-ja"}

// Spec is a revision of the specification, loaded and checked.
type Spec struct {
	Root     string
	Version  string
	Digest   string
	Catalog  *catalog.Catalog
	Checker  *dsl.Checker
	Suite    *suite.Suite
	Features map[string]bool
	schemas  map[string]*jsonschema.Schema
}

// InvalidError reports input that makes a comparison untrustworthy. raoh-verify exits with status
// 2 for it.
type InvalidError struct {
	Problems []string
}

func (e *InvalidError) Error() string {
	return "invalid input:\n  " + strings.Join(e.Problems, "\n  ")
}

func invalid(format string, args ...any) *InvalidError {
	return &InvalidError{Problems: []string{fmt.Sprintf(format, args...)}}
}

const schemaBase = "https://github.com/raoh-project/raoh-specification/schema/"

// Load reads and checks the specification under root: what raoh-verify check-suite does.
func Load(root string) (*Spec, error) {
	s := &Spec{Root: root, Features: map[string]bool{}, schemas: map[string]*jsonschema.Schema{}}
	text, err := os.ReadFile(filepath.Join(root, "specification.json"))
	if err != nil {
		return nil, err
	}
	n, err := jsontext.Parse(text)
	if err != nil {
		return nil, fmt.Errorf("specification.json: %w", err)
	}
	v, ok := n.Get("version")
	if !ok || v.Kind != jsontext.String || v.Text == "" {
		return nil, fmt.Errorf("specification.json: version must be a non-empty string")
	}
	s.Version = v.Text
	if s.Catalog, err = catalog.Load(root); err != nil {
		return nil, err
	}
	reg, err := dsl.Load(root)
	if err != nil {
		return nil, err
	}
	s.Checker = &dsl.Checker{Registry: reg, Catalog: s.Catalog}
	if err := s.Checker.Validate(); err != nil {
		return nil, fmt.Errorf("catalog/operations.json: %w", err)
	}
	for _, f := range reg.Features() {
		s.Features[f] = true
	}
	if s.Suite, err = suite.Load(root, s.Checker); err != nil {
		return nil, err
	}
	if s.Digest, err = manifest.Digest(root); err != nil {
		return nil, err
	}
	if err := s.compileSchemas(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Spec) compileSchemas() error {
	c := jsonschema.NewCompiler()
	names := []string{"case", "runner-result", "conformance", "report"}
	for _, name := range names {
		text, err := os.ReadFile(filepath.Join(s.Root, "schema", name+".schema.json"))
		if err != nil {
			return err
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(text))
		if err != nil {
			return fmt.Errorf("schema/%s.schema.json: %w", name, err)
		}
		if err := c.AddResource(schemaBase+name+".schema.json", doc); err != nil {
			return err
		}
	}
	for _, name := range names {
		sch, err := c.Compile(schemaBase + name + ".schema.json")
		if err != nil {
			return fmt.Errorf("schema/%s.schema.json: %w", name, err)
		}
		s.schemas[name] = sch
	}
	return nil
}

// Validate checks a document against one of the specification's schemas.
func (s *Spec) Validate(schema string, text []byte) error {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(text))
	if err != nil {
		return err
	}
	return s.schemas[schema].Validate(doc)
}

// CheckSuiteFiles checks every case file against the case schema, as check-suite does.
func (s *Spec) CheckSuiteFiles() error {
	files := map[string]bool{}
	for _, c := range s.Suite.Cases {
		files[c.File] = true
	}
	for f := range files {
		text, err := os.ReadFile(filepath.Join(s.Root, filepath.FromSlash(f)))
		if err != nil {
			return err
		}
		if err := s.Validate("case", text); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
	}
	return nil
}
