// Package pattern reads the pattern language of spec/pattern.md: it tells a pattern from text that
// is not one, and an admissible pattern from one past the limits spec/pattern.md sets apart from
// what a pattern means. It is the language Souther's string patterns are written in, read the way
// Souther's compiler reads it, so that one pattern means one set of strings in both.
//
// The verifier never matches a pattern; a case says what its decoder gives. What it needs is to
// refuse, where a case is read, text whose meaning the specification does not define and a pattern
// that means a set of strings but is past a limit, since neither is an argument.
package pattern

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The limits on an admissible pattern (spec/pattern.md): the largest count a repetition may write,
// how deep groups may nest, and the most states a pattern may come to once its repetitions are
// written out.
const (
	MostCount  = 134217727
	Deepest    = 200
	MostStates = 250000
)

// noCeiling is the upper count of a repetition with none.
const noCeiling = -1

// written is a pattern as it is written: what is meant by a part, an anchor, parts in turn, a
// choice, or a repetition.
type written interface{ isWritten() }

type (
	// symbols is a set of characters, never empty as written.
	symbols struct{}
	// nothing is the empty string: an empty group or sequence.
	nothing  struct{}
	anchor   struct{ end bool }
	inTurn   struct{ parts []written }
	eitherOf struct{ arms []written }
	repeated struct {
		what        written
		least, most int
	}
)

func (symbols) isWritten()  {}
func (nothing) isWritten()  {}
func (anchor) isWritten()   {}
func (inTurn) isWritten()   {}
func (eitherOf) isWritten() {}
func (repeated) isWritten() {}

// refusal is why text is no admissible pattern, and the construct that stopped the reading.
type refusal struct {
	why       string
	construct string
}

func (r refusal) Error() string {
	return fmt.Sprintf("%s: %q", r.why, r.construct)
}

type reader struct {
	text      []rune
	at        int
	depth     int
	construct int
}

// Read reports whether text is an admissible pattern, and if it is not, why: it is no pattern, or
// a pattern past a limit.
func Read(text string) (err error) {
	if !utf8.ValidString(text) {
		return fmt.Errorf("a pattern is text")
	}
	r := &reader{text: []rune(text)}
	defer func() {
		if p := recover(); p != nil {
			ref, ok := p.(refusal)
			if !ok {
				panic(p)
			}
			err = ref
		}
	}()
	w := r.alternation()
	if !r.done() {
		r.construct = r.at
		r.refuse("a bracket closes nothing", r.at+1)
	}
	if !placed(w, yes, yes) {
		return refusal{"an anchor whose answer would turn on the string matched", text}
	}
	if plus(1, states(w)) > MostStates {
		return refusal{fmt.Sprintf("more than %d states once its repetitions are written out", MostStates), text}
	}
	return nil
}

// past is one more than MostStates: every count above the limit is this, so a count as large as a
// pattern writes is multiplied without overflowing.
const past = MostStates + 1

// states is what w comes to with its repetitions written out, before the one state the whole
// pattern adds, counted as spec/pattern.md counts it and never above past.
func states(w written) int64 {
	switch w := w.(type) {
	case symbols, anchor:
		return 1
	case nothing:
		return 0
	case inTurn:
		var sum int64
		for _, part := range w.parts {
			sum = plus(sum, states(part))
		}
		return sum
	case eitherOf:
		sum := int64(1)
		for _, arm := range w.arms {
			sum = plus(sum, plus(1, states(arm)))
		}
		return sum
	case repeated:
		copies := int64(w.most)
		if w.most == noCeiling {
			copies = int64(w.least) + 1
		}
		return plus(times(copies, states(w.what)), 1)
	}
	panic(fmt.Sprintf("a written pattern of no known shape: %T", w))
}

func plus(one, other int64) int64 { return min(past, one+other) }

func times(copies, body int64) int64 {
	if copies == 0 || body == 0 {
		return 0
	}
	if copies > past/body {
		return past
	}
	return min(past, copies*body)
}

func (r *reader) done() bool { return r.at >= len(r.text) }

func (r *reader) peek() rune {
	if r.done() {
		return 0
	}
	return r.text[r.at]
}

func (r *reader) take() rune {
	if r.done() {
		r.refuse("something is left open", r.at)
	}
	c := r.text[r.at]
	r.at++
	return c
}

func (r *reader) expect(c rune) {
	if r.peek() != c {
		r.construct = r.at
		r.refuse("something is left open", r.at)
	}
	r.take()
}

// refuse stops the reading, quoting the construct from where it began to to.
func (r *reader) refuse(why string, to int) {
	to = max(to, r.construct)
	to = min(to, len(r.text))
	panic(refusal{why, string(r.text[r.construct:to])})
}

func (r *reader) alternation() written {
	arms := []written{r.sequence()}
	for r.peek() == '|' {
		r.take()
		arms = append(arms, r.sequence())
	}
	if len(arms) == 1 {
		return arms[0]
	}
	return eitherOf{arms}
}

func (r *reader) sequence() written {
	var parts []written
	for !r.done() && r.peek() != '|' && r.peek() != ')' {
		if one := r.quantified(); one != (nothing{}) {
			parts = append(parts, one)
		}
	}
	switch len(parts) {
	case 0:
		return nothing{}
	case 1:
		return parts[0]
	}
	return inTurn{parts}
}

func (r *reader) quantified() written {
	one := r.atom()
	r.construct = r.at
	var least, most int
	switch r.peek() {
	case '?':
		r.take()
		least, most = 0, 1
	case '*':
		r.take()
		least, most = 0, noCeiling
	case '+':
		r.take()
		least, most = 1, noCeiling
	case '{':
		r.take()
		least = r.count()
		most = least
		if r.peek() == ',' {
			r.take()
			if r.peek() == '}' {
				most = noCeiling
			} else {
				most = r.count()
			}
		}
		r.expect('}')
		if most != noCeiling && most < least {
			r.refuse("a count this cannot read", r.at)
		}
	default:
		return one
	}
	// A reluctant count accepts what the greedy one does; a possessive one accepts what a matcher's
	// walk leaves, which the language does not describe.
	switch r.peek() {
	case '?':
		r.take()
	case '+':
		r.take()
		r.refuse("a possessive repetition", r.at)
	}
	return repeated{one, least, most}
}

func (r *reader) atom() written {
	r.construct = r.at
	switch c := r.peek(); c {
	case '(':
		return r.group()
	case '[':
		r.take()
		r.class()
		return symbols{}
	case '\\':
		r.take()
		r.escaped()
		return symbols{}
	case '.':
		r.take()
		return symbols{}
	case '^', '$':
		r.take()
		return anchor{end: c == '$'}
	case '{':
		r.take()
		r.refuse("a count this cannot read", r.at)
	case '*', '+', '?':
		r.take()
		r.refuse("something is left open", r.at)
	}
	// Any other character stands for itself, U+0000 included.
	r.take()
	return symbols{}
}

func (r *reader) group() written {
	r.expect('(')
	if r.peek() == '?' {
		r.take()
		if r.peek() != ':' {
			r.take()
			r.refuse("a group the grammar does not have", r.at)
		}
		r.take()
	}
	if r.depth++; r.depth > Deepest {
		panic(refusal{fmt.Sprintf("groups nest deeper than %d", Deepest), string(r.text)})
	}
	inside := r.alternation()
	r.depth--
	r.expect(')')
	return inside
}

// class reads what is between [ and ], the [ taken.
func (r *reader) class() {
	if r.peek() == '^' {
		r.take()
	}
	first := true
	for !r.done() && (r.peek() != ']' || first) {
		first = false
		r.construct = r.at
		if r.peek() == '[' {
			r.take()
			r.refuse("a class of classes", r.at)
		}
		if r.peek() == '&' && r.at+1 < len(r.text) && r.text[r.at+1] == '&' {
			r.at += 2
			r.refuse("a class of classes", r.at)
		}
		r.classMember()
	}
	r.expect(']')
}

// classMember reads a character, a run of them, or a shorthand. A run is read only where both of
// its ends are one character.
func (r *reader) classMember() {
	low, one := r.classAtom()
	if one && r.peek() == '-' && r.at+1 < len(r.text) && r.text[r.at+1] != ']' {
		r.take()
		high, one := r.classAtom()
		if !one {
			r.refuse("an escape this does not read", r.at)
		}
		if high < low {
			r.refuse("a count this cannot read", r.at)
		}
	}
}

// classAtom reads one member of a class: the character it is, if it is one.
func (r *reader) classAtom() (rune, bool) {
	if r.peek() == '\\' {
		r.take()
		return r.escaped()
	}
	return r.take(), true
}

// escaped reads what an escape stands for, the backslash taken: the one character it is, or false
// for a shorthand, which stands for many.
func (r *reader) escaped() (rune, bool) {
	if r.done() {
		r.refuse("an escape this does not read", r.at)
	}
	kind := r.take()
	switch kind {
	case 'd', 'D', 'w', 'W', 's', 'S':
		return 0, false
	case 'n':
		return '\n', true
	case 't':
		return '\t', true
	case 'r':
		return '\r', true
	case 'f':
		return '\f', true
	case 'a':
		return 0x07, true
	case 'e':
		return 0x1B, true
	case '0':
		return r.octal(), true
	case 'x':
		return r.spelled(r.hex()), true
	case 'u':
		return r.spelled(r.unicode()), true
	case 'p', 'P':
		r.refuse("a character property", r.at)
	case 'b', 'B', 'A', 'z', 'Z', 'G', 'R':
		r.refuse("a boundary", r.at)
	case 'Q', 'E':
		r.refuse("a quotation", r.at)
	case 'k', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		r.refuse("a back reference", r.at)
	}
	if unicode.IsLetter(kind) {
		r.refuse("an escape this does not read", r.at)
	}
	return kind, true
}

func (r *reader) octal() rune {
	value, digits := 0, 0
	for digits < 3 && !r.done() && r.peek() >= '0' && r.peek() <= '7' {
		value = value*8 + int(r.take()-'0')
		digits++
	}
	if digits == 0 || value > 0xFF {
		r.refuse("an escape this does not read", r.at)
	}
	return rune(value)
}

// spelled is the character a \x or \u escape spells; no half of a surrogate pair is one.
func (r *reader) spelled(c rune, ok bool) rune {
	if !ok {
		r.refuse("an escape this does not read", r.at)
	}
	if 0xD800 <= c && c <= 0xDFFF {
		r.refuse("a character no string holds", r.at)
	}
	return c
}

// hex reads what \x spells: two hex digits, or hex digits in braces up to 10FFFF.
func (r *reader) hex() (rune, bool) {
	if r.peek() != '{' {
		return r.fixedHex(2)
	}
	r.take()
	value, digits := 0, 0
	for !r.done() && r.peek() != '}' {
		d := hexDigit(r.take())
		if d < 0 {
			return 0, false
		}
		value = value*16 + d
		digits++
		if value > unicode.MaxRune {
			return 0, false
		}
	}
	if r.done() || digits == 0 {
		return 0, false
	}
	r.take()
	return rune(value), true
}

// unicode reads what \u spells: four hex digits, where a high surrogate's followed at once by a
// low one's is the one character the two encode.
func (r *reader) unicode() (rune, bool) {
	first, ok := r.fixedHex(4)
	if !ok {
		return 0, false
	}
	if 0xD800 <= first && first <= 0xDBFF && strings.HasPrefix(string(r.text[r.at:min(r.at+2, len(r.text))]), `\u`) {
		save := r.at
		r.at += 2
		if second, ok := r.fixedHex(4); ok && 0xDC00 <= second && second <= 0xDFFF {
			return (first-0xD800)<<10 + (second - 0xDC00) + 0x10000, true
		}
		r.at = save
	}
	return first, true
}

func (r *reader) fixedHex(digits int) (rune, bool) {
	if r.at+digits > len(r.text) {
		return 0, false
	}
	value := 0
	for i := 0; i < digits; i++ {
		d := hexDigit(r.text[r.at+i])
		if d < 0 {
			return 0, false
		}
		value = value*16 + d
	}
	r.at += digits
	return rune(value), true
}

// hexDigit is the value of an ASCII hex digit, or -1.
func hexDigit(c rune) int {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0')
	case 'a' <= c && c <= 'f':
		return int(c-'a') + 10
	case 'A' <= c && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

func (r *reader) count() int {
	value, digits := 0, 0
	for !r.done() && r.peek() >= '0' && r.peek() <= '9' {
		value = min(MostCount+1, value*10+int(r.take()-'0'))
		digits++
	}
	if digits == 0 {
		r.refuse("a count this cannot read", r.at)
	}
	if value > MostCount {
		r.refuse(fmt.Sprintf("a count past the limit of %d", MostCount), r.at)
	}
	return value
}

// where is whether an anchor stands at the end it asks about, as far as the pattern's shape says.
type where int

const (
	yes where = iota + 1
	no
	unsettled
)

// placed reports whether every anchor in w comes to something: an anchor at its end adds nothing,
// a ^ after something that must take a character leaves no string, and one whose answer would
// turn on the string matched is refused, as is a $ before something that must take a character.
func placed(w written, atStart, atEnd where) bool {
	switch w := w.(type) {
	case symbols, nothing:
		return true
	case anchor:
		at := atStart
		if w.end {
			at = atEnd
		}
		switch at {
		case yes:
			return true
		case no:
			return !w.end
		}
		return false
	case eitherOf:
		for _, arm := range w.arms {
			if !placed(arm, atStart, atEnd) {
				return false
			}
		}
		return true
	case inTurn:
		n := len(w.parts)
		mayBefore, mustBefore := make([]bool, n+1), make([]bool, n+1)
		mustBefore[0] = true
		for i, p := range w.parts {
			mayBefore[i+1] = mayBefore[i] || mayTake(p)
			mustBefore[i+1] = mustBefore[i] && mustTake(p)
		}
		mayAfter, mustAfter := make([]bool, n+1), make([]bool, n+1)
		mustAfter[n] = true
		for i := n - 1; i >= 0; i-- {
			mayAfter[i] = mayAfter[i+1] || mayTake(w.parts[i])
			mustAfter[i] = mustAfter[i+1] && mustTake(w.parts[i])
		}
		for i, p := range w.parts {
			if !placed(p, beyond(mayBefore[i], mustBefore[i], atStart), beyond(mayAfter[i+1], mustAfter[i+1], atEnd)) {
				return false
			}
		}
		return true
	case repeated:
		switch {
		case !holdsAnchor(w.what):
			return true
		case w.least == 1 && w.most == 1:
			return placed(w.what, atStart, atEnd)
		}
		return false
	}
	panic(fmt.Sprintf("a written pattern %T", w))
}

func beyond(anyTakes, allTake bool, outer where) where {
	switch {
	case !anyTakes:
		return outer
	case allTake:
		return no
	}
	return unsettled
}

// mayTake reports whether w accepts a string of one character or more.
func mayTake(w written) bool {
	switch w := w.(type) {
	case symbols:
		return true
	case nothing, anchor:
		return false
	case inTurn:
		for _, p := range w.parts {
			if mayTake(p) {
				return true
			}
		}
		return false
	case eitherOf:
		for _, a := range w.arms {
			if mayTake(a) {
				return true
			}
		}
		return false
	case repeated:
		return (w.most == noCeiling || w.most > 0) && mayTake(w.what)
	}
	panic(fmt.Sprintf("a written pattern %T", w))
}

// mustTake reports whether every string w accepts has a character.
func mustTake(w written) bool {
	switch w := w.(type) {
	case symbols:
		return true
	case nothing, anchor:
		return false
	case inTurn:
		for _, p := range w.parts {
			if mustTake(p) {
				return true
			}
		}
		return false
	case eitherOf:
		for _, a := range w.arms {
			if !mustTake(a) {
				return false
			}
		}
		return true
	case repeated:
		return w.least > 0 && mustTake(w.what)
	}
	panic(fmt.Sprintf("a written pattern %T", w))
}

func holdsAnchor(w written) bool {
	switch w := w.(type) {
	case anchor:
		return true
	case inTurn:
		for _, p := range w.parts {
			if holdsAnchor(p) {
				return true
			}
		}
	case eitherOf:
		for _, a := range w.arms {
			if holdsAnchor(a) {
				return true
			}
		}
	case repeated:
		return holdsAnchor(w.what)
	}
	return false
}
