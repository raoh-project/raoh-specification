// Package jsontext reads JSON text into a tree that keeps what encoding/json discards: the lexeme
// of every number, the order of object members, and the exact bytes of every value. It accepts
// only what RFC 8259 allows, and in addition rejects an object that repeats a member name and a
// string holding an unpaired surrogate, neither of which the Raoh input model can represent.
package jsontext

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Kind is the kind of a JSON value.
type Kind int

// The kinds of JSON value.
const (
	Null Kind = iota
	Bool
	Number
	String
	Array
	Object
)

func (k Kind) String() string {
	return [...]string{"null", "boolean", "number", "string", "array", "object"}[k]
}

// Node is one JSON value.
type Node struct {
	Kind Kind
	// Bool is the value of a Bool.
	Bool bool
	// Text is the value of a String, and the lexeme of a Number.
	Text string
	// Elems are the elements of an Array.
	Elems []*Node
	// Members are the members of an Object, in the order the text gives them.
	Members []Member
	// Raw is the text of the value, without surrounding whitespace.
	Raw []byte
}

// Member is one member of an object.
type Member struct {
	Name  string
	Value *Node
}

// Get returns the value of the member named name.
func (n *Node) Get(name string) (*Node, bool) {
	for _, m := range n.Members {
		if m.Name == name {
			return m.Value, true
		}
	}
	return nil, false
}

// Names returns the member names of an object, in order.
func (n *Node) Names() []string {
	names := make([]string, len(n.Members))
	for i, m := range n.Members {
		names[i] = m.Name
	}
	return names
}

// SyntaxError reports where a text stops being acceptable.
type SyntaxError struct {
	Offset int
	Msg    string
}

func (e *SyntaxError) Error() string { return fmt.Sprintf("offset %d: %s", e.Offset, e.Msg) }

// Parse reads a JSON text holding exactly one value.
func Parse(text []byte) (*Node, error) {
	p := &parser{text: text}
	p.space()
	n, err := p.value()
	if err != nil {
		return nil, err
	}
	p.space()
	if p.pos != len(text) {
		return nil, p.errorf("unexpected data after the value")
	}
	return n, nil
}

// MustParse is Parse for texts written in code.
func MustParse(text string) *Node {
	n, err := Parse([]byte(text))
	if err != nil {
		panic(err)
	}
	return n
}

type parser struct {
	text  []byte
	pos   int
	depth int
}

const maxDepth = 1000

func (p *parser) errorf(format string, args ...any) error {
	return &SyntaxError{Offset: p.pos, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) space() {
	for p.pos < len(p.text) {
		switch p.text[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) value() (*Node, error) {
	if p.pos >= len(p.text) {
		return nil, p.errorf("unexpected end of text")
	}
	start := p.pos
	var n *Node
	var err error
	switch c := p.text[p.pos]; {
	case c == '{':
		n, err = p.object()
	case c == '[':
		n, err = p.array()
	case c == '"':
		var s string
		s, err = p.string()
		n = &Node{Kind: String, Text: s}
	case c == '-' || ('0' <= c && c <= '9'):
		var lexeme string
		lexeme, err = p.number()
		n = &Node{Kind: Number, Text: lexeme}
	case p.literal("true"):
		n = &Node{Kind: Bool, Bool: true}
	case p.literal("false"):
		n = &Node{Kind: Bool}
	case p.literal("null"):
		n = &Node{Kind: Null}
	default:
		return nil, p.errorf("unexpected character %q", c)
	}
	if err != nil {
		return nil, err
	}
	n.Raw = p.text[start:p.pos]
	return n, nil
}

func (p *parser) literal(word string) bool {
	if bytes.HasPrefix(p.text[p.pos:], []byte(word)) {
		p.pos += len(word)
		return true
	}
	return false
}

func (p *parser) enter() error {
	p.depth++
	if p.depth > maxDepth {
		return p.errorf("nested more than %d levels", maxDepth)
	}
	return nil
}

func (p *parser) object() (*Node, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	p.pos++
	n := &Node{Kind: Object, Members: []Member{}}
	seen := map[string]bool{}
	p.space()
	if p.pos < len(p.text) && p.text[p.pos] == '}' {
		p.pos++
		return n, nil
	}
	for {
		p.space()
		if p.pos >= len(p.text) || p.text[p.pos] != '"' {
			return nil, p.errorf("expected a member name")
		}
		at := p.pos
		name, err := p.string()
		if err != nil {
			return nil, err
		}
		if seen[name] {
			return nil, &SyntaxError{Offset: at, Msg: fmt.Sprintf("member %q appears more than once", name)}
		}
		seen[name] = true
		p.space()
		if p.pos >= len(p.text) || p.text[p.pos] != ':' {
			return nil, p.errorf("expected ':'")
		}
		p.pos++
		p.space()
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		n.Members = append(n.Members, Member{Name: name, Value: v})
		p.space()
		if p.pos >= len(p.text) {
			return nil, p.errorf("unexpected end of text in an object")
		}
		switch p.text[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return n, nil
		default:
			return nil, p.errorf("expected ',' or '}'")
		}
	}
}

func (p *parser) array() (*Node, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	p.pos++
	n := &Node{Kind: Array, Elems: []*Node{}}
	p.space()
	if p.pos < len(p.text) && p.text[p.pos] == ']' {
		p.pos++
		return n, nil
	}
	for {
		p.space()
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		n.Elems = append(n.Elems, v)
		p.space()
		if p.pos >= len(p.text) {
			return nil, p.errorf("unexpected end of text in an array")
		}
		switch p.text[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return n, nil
		default:
			return nil, p.errorf("expected ',' or ']'")
		}
	}
}

func (p *parser) number() (string, error) {
	start := p.pos
	if p.text[p.pos] == '-' {
		p.pos++
	}
	digits := func() int {
		n := 0
		for p.pos < len(p.text) && '0' <= p.text[p.pos] && p.text[p.pos] <= '9' {
			p.pos++
			n++
		}
		return n
	}
	if p.pos < len(p.text) && p.text[p.pos] == '0' {
		p.pos++
	} else if digits() == 0 {
		return "", p.errorf("expected a digit")
	}
	if p.pos < len(p.text) && p.text[p.pos] == '.' {
		p.pos++
		if digits() == 0 {
			return "", p.errorf("expected a digit after '.'")
		}
	}
	if p.pos < len(p.text) && (p.text[p.pos] == 'e' || p.text[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.text) && (p.text[p.pos] == '+' || p.text[p.pos] == '-') {
			p.pos++
		}
		if digits() == 0 {
			return "", p.errorf("expected a digit in the exponent")
		}
	}
	return string(p.text[start:p.pos]), nil
}

func (p *parser) string() (string, error) {
	p.pos++
	var b strings.Builder
	for {
		if p.pos >= len(p.text) {
			return "", p.errorf("unterminated string")
		}
		c := p.text[p.pos]
		switch {
		case c == '"':
			p.pos++
			return b.String(), nil
		case c == '\\':
			r, err := p.escape()
			if err != nil {
				return "", err
			}
			b.WriteRune(r)
		case c < 0x20:
			return "", p.errorf("control character in a string")
		default:
			r, size := utf8.DecodeRune(p.text[p.pos:])
			if r == utf8.RuneError && size == 1 {
				return "", p.errorf("invalid UTF-8")
			}
			b.WriteRune(r)
			p.pos += size
		}
	}
}

func (p *parser) escape() (rune, error) {
	p.pos++
	if p.pos >= len(p.text) {
		return 0, p.errorf("unterminated escape")
	}
	c := p.text[p.pos]
	p.pos++
	switch c {
	case '"', '\\', '/':
		return rune(c), nil
	case 'b':
		return '\b', nil
	case 'f':
		return '\f', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 't':
		return '\t', nil
	case 'u':
		r, err := p.hex4()
		if err != nil {
			return 0, err
		}
		if utf16.IsSurrogate(r) {
			if r >= 0xdc00 || !bytes.HasPrefix(p.text[p.pos:], []byte(`\u`)) {
				return 0, p.errorf("unpaired surrogate")
			}
			p.pos += 2
			low, err := p.hex4()
			if err != nil {
				return 0, err
			}
			r = utf16.DecodeRune(r, low)
			if r == utf8.RuneError {
				return 0, p.errorf("unpaired surrogate")
			}
		}
		return r, nil
	default:
		return 0, p.errorf("invalid escape '\\%c'", c)
	}
}

func (p *parser) hex4() (rune, error) {
	if p.pos+4 > len(p.text) {
		return 0, p.errorf("short \\u escape")
	}
	v, err := strconv.ParseUint(string(p.text[p.pos:p.pos+4]), 16, 32)
	if err != nil {
		return 0, p.errorf("invalid \\u escape")
	}
	p.pos += 4
	return rune(v), nil
}
