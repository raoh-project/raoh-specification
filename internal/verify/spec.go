// Package verify classifies what a runner observed against the suite and the implementation's
// declaration, and writes the report. See spec/conformance.md.
package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/raoh-project/raoh-specification/internal/artifacts"
	"github.com/raoh-project/raoh-specification/internal/catalog"
	"github.com/raoh-project/raoh-specification/internal/dsl"
	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/manifest"
	"github.com/raoh-project/raoh-specification/internal/schemas"
	"github.com/raoh-project/raoh-specification/internal/suite"
)

// Profiles are every profile, in the order reports list them.
var Profiles = []string{"core", "encode", "messages-en", "messages-ja"}

// Spec is a revision of the specification, loaded and checked.
type Spec struct {
	Root     string
	Version  string
	Digest   string
	Schemas  *schemas.Set
	Catalog  *catalog.Catalog
	Checker  *dsl.Checker
	Suite    *suite.Suite
	Features map[string]bool
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

// Load reads and checks the specification under root: what raoh-verify check-suite does. Every
// artifact goes through its schema before it is parsed.
func Load(root string) (*Spec, error) {
	if _, err := artifacts.List(root); err != nil {
		return nil, err
	}
	s := &Spec{Root: root, Features: map[string]bool{}}
	var err error
	if s.Schemas, err = schemas.Load(root); err != nil {
		return nil, err
	}
	text, err := os.ReadFile(filepath.Join(root, "specification.json"))
	if err != nil {
		return nil, err
	}
	n, err := jsontext.Parse(text)
	if err != nil {
		return nil, fmt.Errorf("specification.json: %w", err)
	}
	if s.Version, err = n.String("version"); err != nil || s.Version == "" {
		return nil, fmt.Errorf("specification.json: version must be a non-empty string")
	}
	if s.Catalog, err = catalog.Load(root, s.Schemas); err != nil {
		return nil, err
	}
	reg, err := dsl.Load(root, s.Schemas)
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
	if s.Suite, err = suite.Load(root, s.Checker, s.Schemas); err != nil {
		return nil, err
	}
	if s.Digest, err = manifest.Digest(root); err != nil {
		return nil, err
	}
	return s, nil
}

// Validate checks a document against one of the specification's schemas.
func (s *Spec) Validate(schema string, text []byte) error {
	return s.Schemas.Validate(schema, text)
}
