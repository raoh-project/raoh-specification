package catalog

import (
	"github.com/raoh-project/raoh-specification/internal/schemas"
	"strings"
	"testing"

	"github.com/raoh-project/raoh-specification/internal/value"
)

func TestTheCataloguesAgree(t *testing.T) {
	c, err := Load("../..", schemasFor(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Variants) < 50 {
		t.Errorf("only %d variants", len(c.Variants))
	}
	if got := c.Messages["en"]["too_short"]; got != "must be at least {min} characters" {
		t.Errorf("en too_short = %q", got)
	}
	if got := c.Messages["ja"]["required"]; got != "必須です" {
		t.Errorf("ja required = %q", got)
	}
}

func TestInstantiateBindsNestedParameters(t *testing.T) {
	c, err := Load("../..", schemasFor(t))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := c.Variants["not_allowed"].Instantiate(map[string]value.Type{"T": value.Of(value.Float32)})
	if err != nil {
		t.Fatal(err)
	}
	if meta["allowed"].String() != "list<float32>" || meta["actual"].String() != "float32" {
		t.Errorf("got %v", meta)
	}
	if _, err := c.Variants["not_allowed"].Instantiate(nil); err == nil {
		t.Error("instantiated without T")
	}
}

func TestCheckFindsDisagreements(t *testing.T) {
	variants, err := ParseVariants([]byte(`{
		"too_short": {"code": "too_short", "meta": {"min": "int32"}},
		"not_allowed": {"code": "not_allowed", "params": ["T"], "meta": {"allowed": "list<T>", "actual": "T"}, "optional_meta": ["actual"]},
		"blank": {"code": "blank", "meta": {}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	c := &Catalog{Variants: variants, Messages: map[string]map[string]string{
		"en": {"too_short": "at least {minimum}", "not_allowed": "{actual} is not allowed", "extra": "x"},
		"ja": {"too_short": "{min}", "not_allowed": "{allowed}", "blank": "空白"},
	}}
	err = c.Check()
	if err == nil {
		t.Fatal("no disagreement found")
	}
	for _, want := range []string{
		"en: no template for blank",
		"en: the template for too_short refers to {minimum}",
		"en: the template for not_allowed refers to {actual}, which the variant may leave out",
		"en: the template for extra belongs to no variant",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "ja:") {
		t.Errorf("ja reported:\n%v", err)
	}
}

func TestParseVariantRejects(t *testing.T) {
	for _, text := range []string{
		`{"x": {"code": "y", "meta": {}}}`,
		`{"x": {"code": "x", "meta": {"a": "T"}}}`,
		`{"x": {"code": "x", "params": ["T"], "meta": {}}}`,
		`{"x": {"code": "x", "meta": {}, "optional_meta": ["a"]}}`,
		`{"x": {"code": "x", "meta": {"a": "list"}}}`,
		`{"x": {"code": "x", "meta": {}, "colour": 1}}`,
	} {
		if _, err := ParseVariants([]byte(text)); err == nil {
			t.Errorf("%s accepted", text)
		}
	}
}

func TestParseProperties(t *testing.T) {
	props, err := ParseProperties("# comment\n! comment\n  a = 1\nb:2\nc 3\nd=x\\\n   y\ne=\\u65e5\\t\\=\nf\\ g=h\n")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a": "1", "b": "2", "c": "3", "d": "xy", "e": "日\t=", "f g": "h"}
	for k, v := range want {
		if props[k] != v {
			t.Errorf("%q = %q, want %q", k, props[k], v)
		}
	}
	if len(props) != len(want) {
		t.Errorf("got %v", props)
	}
	if _, err := ParseProperties("a=1\na=2\n"); err == nil {
		t.Error("a repeated key accepted")
	}
}

func schemasFor(t *testing.T) *schemas.Set {
	t.Helper()
	sch, err := schemas.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	return sch
}
