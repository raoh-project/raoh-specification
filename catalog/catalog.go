// Package catalog reads the issue catalogue and the message catalogues, and checks that they
// agree with each other.
package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/raoh-project/raoh-specification/jsontext"
	"github.com/raoh-project/raoh-specification/value"
)

// Locales are the locales the specification has message catalogues for.
var Locales = []string{"en", "ja"}

// KeyPrefix is prefixed to a message key to make its key in a properties file.
const KeyPrefix = "raoh."

// Variant is one kind of issue.
type Variant struct {
	// Key is the message key, which identifies the variant.
	Key string
	// Code is the code, which classifies it.
	Code string
	// Params are the type parameters its metadata types mention.
	Params []string
	// Meta is the type of each metadata entry.
	Meta map[string]value.Type
	// Optional are the metadata entries an issue of the variant may leave out.
	Optional []string
}

// Catalog is the issue catalogue and the message catalogues.
type Catalog struct {
	Variants map[string]*Variant
	// Messages holds, for each locale, the template of each message key.
	Messages map[string]map[string]string
}

var placeholder = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)

// Placeholders returns the names a template refers to.
func Placeholders(template string) []string {
	var names []string
	for _, m := range placeholder.FindAllStringSubmatch(template, -1) {
		names = append(names, m[1])
	}
	return names
}

// Load reads catalog/issues.json and catalog/messages/*.properties under root.
func Load(root string) (*Catalog, error) {
	text, err := os.ReadFile(filepath.Join(root, "catalog", "issues.json"))
	if err != nil {
		return nil, err
	}
	variants, err := ParseVariants(text)
	if err != nil {
		return nil, fmt.Errorf("catalog/issues.json: %w", err)
	}
	c := &Catalog{Variants: variants, Messages: map[string]map[string]string{}}
	for _, locale := range Locales {
		name := filepath.Join("catalog", "messages", locale+".properties")
		text, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return nil, err
		}
		messages, err := ParseMessages(string(text))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		c.Messages[locale] = messages
	}
	if err := c.Check(); err != nil {
		return nil, err
	}
	return c, nil
}

// ParseMessages reads a message catalogue: a properties file whose keys are message keys with
// KeyPrefix.
func ParseMessages(text string) (map[string]string, error) {
	props, err := ParseProperties(text)
	if err != nil {
		return nil, err
	}
	messages := map[string]string{}
	for k, v := range props {
		key, ok := strings.CutPrefix(k, KeyPrefix)
		if !ok {
			return nil, fmt.Errorf("key %q does not start with %q", k, KeyPrefix)
		}
		messages[key] = v
	}
	return messages, nil
}

// ParseVariants reads the issue catalogue.
func ParseVariants(text []byte) (map[string]*Variant, error) {
	root, err := jsontext.Parse(text)
	if err != nil {
		return nil, err
	}
	if root.Kind != jsontext.Object {
		return nil, fmt.Errorf("expected an object")
	}
	variants := map[string]*Variant{}
	for _, m := range root.Members {
		v, err := parseVariant(m.Name, m.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m.Name, err)
		}
		variants[m.Name] = v
	}
	return variants, nil
}

func parseVariant(key string, n *jsontext.Node) (*Variant, error) {
	return ParseVariant(key, n, nil)
}

// ParseVariant reads one variant written as in catalog/issues.json. allowed lists the members the
// object may have beyond those of a variant.
func ParseVariant(key string, n *jsontext.Node, allowed []string) (*Variant, error) {
	if n.Kind != jsontext.Object {
		return nil, fmt.Errorf("expected an object")
	}
	for _, name := range n.Names() {
		if !slices.Contains([]string{"code", "params", "meta", "optional_meta"}, name) && !slices.Contains(allowed, name) {
			return nil, fmt.Errorf("unknown member %q", name)
		}
	}
	v := &Variant{Key: key, Meta: map[string]value.Type{}}
	code, ok := n.Get("code")
	if !ok || code.Kind != jsontext.String || code.Text == "" {
		return nil, fmt.Errorf("code must be a non-empty string")
	}
	v.Code = code.Text
	if !strings.HasPrefix(key, v.Code) {
		return nil, fmt.Errorf("the message key does not start with the code %q", v.Code)
	}
	strs := func(name string) ([]string, error) {
		n, ok := n.Get(name)
		if !ok {
			return nil, nil
		}
		if n.Kind != jsontext.Array {
			return nil, fmt.Errorf("%s must be an array of strings", name)
		}
		var out []string
		for _, e := range n.Elems {
			if e.Kind != jsontext.String {
				return nil, fmt.Errorf("%s must be an array of strings", name)
			}
			out = append(out, e.Text)
		}
		return out, nil
	}
	var err error
	if v.Params, err = strs("params"); err != nil {
		return nil, err
	}
	if v.Optional, err = strs("optional_meta"); err != nil {
		return nil, err
	}
	meta, ok := n.Get("meta")
	if !ok || meta.Kind != jsontext.Object {
		return nil, fmt.Errorf("meta must be an object")
	}
	var mentioned []string
	for _, m := range meta.Members {
		if m.Value.Kind != jsontext.String {
			return nil, fmt.Errorf("meta %s: expected a type", m.Name)
		}
		t, err := value.ParseType(m.Value.Text)
		if err != nil {
			return nil, fmt.Errorf("meta %s: %w", m.Name, err)
		}
		for _, p := range t.Params() {
			if !slices.Contains(v.Params, p) {
				return nil, fmt.Errorf("meta %s mentions %s, which params does not list", m.Name, p)
			}
			if !slices.Contains(mentioned, p) {
				mentioned = append(mentioned, p)
			}
		}
		v.Meta[m.Name] = t
	}
	if len(mentioned) != len(v.Params) {
		return nil, fmt.Errorf("params lists a parameter no metadata type mentions")
	}
	for _, o := range v.Optional {
		if _, ok := v.Meta[o]; !ok {
			return nil, fmt.Errorf("optional_meta lists %q, which meta does not have", o)
		}
	}
	return v, nil
}

// Check checks that every variant has a template in every locale, that every template belongs to
// a variant, and that a template refers only to metadata its variant always has.
func (c *Catalog) Check() error {
	var problems []string
	for _, locale := range Locales {
		messages := c.Messages[locale]
		for key, v := range c.Variants {
			template, ok := messages[key]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: no template for %s", locale, key))
				continue
			}
			for _, name := range Placeholders(template) {
				if _, ok := v.Meta[name]; !ok {
					problems = append(problems, fmt.Sprintf("%s: the template for %s refers to {%s}, which the variant has no metadata for", locale, key, name))
				} else if slices.Contains(v.Optional, name) {
					problems = append(problems, fmt.Sprintf("%s: the template for %s refers to {%s}, which the variant may leave out", locale, key, name))
				}
			}
		}
		for key := range messages {
			if _, ok := c.Variants[key]; !ok {
				problems = append(problems, fmt.Sprintf("%s: the template for %s belongs to no variant", locale, key))
			}
		}
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return fmt.Errorf("the catalogues disagree:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

// Instantiate returns the metadata types of a variant with its type parameters bound to args.
func (v *Variant) Instantiate(args map[string]value.Type) (map[string]value.Type, error) {
	for _, p := range v.Params {
		if _, ok := args[p]; !ok {
			return nil, fmt.Errorf("%s: no type for %s", v.Key, p)
		}
	}
	out := map[string]value.Type{}
	for k, t := range v.Meta {
		out[k] = t.Subst(args)
	}
	return out, nil
}
