package jsontext

import (
	"strings"
	"testing"
)

func TestNumbersKeepTheirLexeme(t *testing.T) {
	for _, text := range []string{"-0", "-0.0", "1e400", "12345678901234567890", "1.50", "1E+3"} {
		n, err := Parse([]byte(text))
		if err != nil {
			t.Fatalf("%s: %v", text, err)
		}
		if n.Kind != Number || n.Text != text {
			t.Errorf("%s: got %v %q", text, n.Kind, n.Text)
		}
	}
}

func TestRawIsTheTextOfEachValue(t *testing.T) {
	n := MustParse(` {"input" : -0.0 , "list":[ 1 ,"a"]} `)
	in, _ := n.Get("input")
	if string(in.Raw) != "-0.0" {
		t.Errorf("input raw %q", in.Raw)
	}
	list, _ := n.Get("list")
	if string(list.Raw) != `[ 1 ,"a"]` {
		t.Errorf("list raw %q", list.Raw)
	}
	if got := n.Names(); strings.Join(got, ",") != "input,list" {
		t.Errorf("names %v", got)
	}
}

func TestRepeatedMemberNamesAreRejected(t *testing.T) {
	for _, text := range []string{`{"a":1,"a":2}`, `{"x":{"a":1,"a":2}}`} {
		if _, err := Parse([]byte(text)); err == nil || !strings.Contains(err.Error(), "more than once") {
			t.Errorf("%s: %v", text, err)
		}
	}
}

func TestStrings(t *testing.T) {
	n := MustParse(`"😀é\n\/"`)
	if n.Text != "😀é\n/" {
		t.Errorf("got %q", n.Text)
	}
	for _, text := range []string{`"\ud83d"`, `"\ude00"`, `"\ud83dx"`, "\"\x01\"", "\"\xff\""} {
		if _, err := Parse([]byte(text)); err == nil {
			t.Errorf("%q accepted", text)
		}
	}
}

func TestInvalidTexts(t *testing.T) {
	for _, text := range []string{"", "01", "1.", ".5", "+1", "1e", "[1,]", `{"a":1,}`, "nul", "1 2", "[", `{"a"}`, "NaN"} {
		if _, err := Parse([]byte(text)); err == nil {
			t.Errorf("%q accepted", text)
		}
	}
}

func TestDepthIsLimited(t *testing.T) {
	text := strings.Repeat("[", maxDepth+1) + strings.Repeat("]", maxDepth+1)
	if _, err := Parse([]byte(text)); err == nil {
		t.Error("accepted")
	}
}
