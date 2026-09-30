// Package schemas validates documents against the specification's JSON Schemas. It is the first
// of the three phases every document the verifier reads goes through: the schema says what shape
// the document has, a typed parser then reads it without defaulting anything that is missing, and
// the semantic checks join it with the other documents.
package schemas

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Names are the schemas, each at schema/<name>.schema.json.
var Names = []string{"issues", "operations", "fixtures", "case", "retired", "runner-result", "conformance", "report"}

const base = "https://github.com/raoh-project/raoh-specification/schema/"

// Set is the compiled schemas of a specification.
type Set struct {
	schemas map[string]*jsonschema.Schema
}

// Load compiles the schemas under root.
func Load(root string) (*Set, error) {
	c := jsonschema.NewCompiler()
	for _, name := range Names {
		text, err := os.ReadFile(filepath.Join(root, "schema", name+".schema.json"))
		if err != nil {
			return nil, err
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(text))
		if err != nil {
			return nil, fmt.Errorf("schema/%s.schema.json: %w", name, err)
		}
		if err := c.AddResource(base+name+".schema.json", doc); err != nil {
			return nil, err
		}
	}
	s := &Set{schemas: map[string]*jsonschema.Schema{}}
	for _, name := range Names {
		sch, err := c.Compile(base + name + ".schema.json")
		if err != nil {
			return nil, fmt.Errorf("schema/%s.schema.json: %w", name, err)
		}
		s.schemas[name] = sch
	}
	return s, nil
}

// Validate checks a document against a schema.
func (s *Set) Validate(name string, text []byte) error {
	sch, ok := s.schemas[name]
	if !ok {
		return fmt.Errorf("no schema %q", name)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(text))
	if err != nil {
		return err
	}
	return sch.Validate(doc)
}
