package jsontext

import "testing"

// The canonical text keeps lexemes and member order and drops whitespace and escapes.
func TestCanonical(t *testing.T) {
	same := [][2]string{
		{`[1, "a", {"x": null, "y": true}]`, `[1,"a",{"x":null,"y":true}]`},
	}
	for _, p := range same {
		if MustParse(p[0]).Canonical() != MustParse(p[1]).Canonical() {
			t.Errorf("%s and %s differ", p[0], p[1])
		}
	}
	differ := [][2]string{{`1`, `1.0`}, {`-0`, `0`}, {`{"a":1,"b":2}`, `{"b":2,"a":1}`}, {`"1"`, `1`}}
	for _, p := range differ {
		if MustParse(p[0]).Canonical() == MustParse(p[1]).Canonical() {
			t.Errorf("%s and %s are the same", p[0], p[1])
		}
	}
}
