package pattern

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// isInadmissible checks that p is inadmissible as spec/pattern.md decides, refused or a pattern
// past a limit, and that this reader's reason contains why. Which of the two p is comes from
// spec/pattern.md. The reason is this reader's own diagnostic, checked so that it stays useful to
// whoever wrote the case; spec/pattern.md does not require it.
func isInadmissible(t *testing.T, p string, limit bool, why string) {
	t.Helper()
	want := "refused"
	if limit {
		want = "past a limit"
	}
	var in inadmissible
	switch err := Read(p); {
	case err == nil:
		t.Errorf("%.40q is read, want it %s", p, want)
	case !errors.As(err, &in):
		t.Errorf("%.40q: %v, want it %s", p, err, want)
	case in.limit != limit:
		t.Errorf("%.40q: %v, want it %s", p, err, want)
	case !strings.Contains(in.why, why):
		t.Errorf("%.40q: %v, want this reader to say %q", p, err, why)
	}
}

// The pattern language is Souther's (spec/pattern.md), and spec/pattern.md decides what a class, an
// anchor and an octal escape are where Souther's text leaves two readings.
func TestPatternsAreReadAsSoutherReadsThem(t *testing.T) {
	for _, p := range []string{
		`^[0-9]{3}$`, `[0-9]{3}`, `a|`, `|a`, `(?:)`, `()`, `[]]`, `[^]]`, `[\d-z]`, `[-a]`, `[a-]`,
		`\x{1F600}`, `\x41`, `😀`, `A`, `\0101`, `\.`, `\\`, `\-`, `a{2,}?`, `a{2}`,
		`a{0,3}`, `a+?`, `(^a|b$)`, `a^b`, `^abc$`, `.`, `\s\S\d\D\w\W`, `\t\n\r\f\a\e`, "a\x00b",
		`[\x{0}-\x{10FFFF}]`, `[^\x{0}-\x{10FFFF}]`, `😀+`, `(a)(b)`, `}`, `]`,
		`\😀`, `\Ⅻ`, `\²`, `[\Ⅻ]`,
		// In a class only its own syntax is special: every other character stands for itself, a ^
		// that is not first, and a - that is not between two characters.
		`[.]`, `[$]`, `[(]`, `[)]`, `[{]`, `[}]`, `[|]`, `[?]`, `[*]`, `[+]`, `[a^]`, `[a^b]`, `[^^]`, `[\^]`,
		`[a-\d]`, `[\d-z-\w]`, `[\d-\w]`, `[--a]`, `[+--]`, `[]a]`, `[^]a]`, `[@-\[]`, `[%-&]`, `[a&]`, `[&a]`,
		// \0 takes the longest run of at most three octal digits, up to 377.
		`\0377`, `\0123`, `\000`, `\0008`,
		// A $ with nothing after it that may take a character.
		`a$$`, `a$|b`, `(a$|c)`, `a$()`, `a$b{0}`,
	} {
		if err := Read(p); err != nil {
			t.Errorf("%q is refused: %v", p, err)
		}
	}
	for p, why := range map[string]string{
		`(?=a)`:   "a group the grammar does not have",
		`(?<n>a)`: "a group the grammar does not have",
		`(?i)a`:   "a group the grammar does not have",
		`\1`:      "a back reference",
		`\k<n>`:   "a back reference",
		`\p{L}`:   "a character property",
		`\b`:      "a boundary",
		`\z`:      "a boundary",
		`\Qa\E`:   "a quotation",
		`a++`:     "a possessive repetition",
		`a{2}+`:   "a possessive repetition",
		`[a&&b]`:  "a class of classes",
		`[[a]]`:   "a class of classes",
		`(a|)^b`:  "an anchor",
		`(^a)*`:   "an anchor",
		`a$b`:     "an anchor",
		`a$b?`:    "an anchor",
		`a$b*`:    "an anchor",
		`$b`:      "an anchor",
		`$(b|)`:   "an anchor",
		`a$(|b)`:  "an anchor",
		`(a$)b`:   "an anchor",
		`(a$|c)b`: "an anchor",
		`[a--]`:   "a count this cannot read",
		// A [ or && is refused wherever it stands in a class, an end of a run included.
		`[@-[]`:      "a class of classes",
		`[%-&&]`:     "a class of classes",
		`[^%-[]`:     "a class of classes",
		`[a-&&]`:     "a class of classes",
		`[[-a]`:      "a class of classes",
		`\0777`:      "an escape this does not read",
		`\uD800`:     "a character no string holds",
		`\x{DC00}`:   "a character no string holds",
		`\q`:         "an escape this does not read",
		`\é`:         "an escape this does not read",
		`\٣`:         "an escape this does not read",
		`[\٣]`:       "an escape this does not read",
		`\꟝`:         "an escape this does not read",
		`\x{110000}`: "an escape this does not read",
		`\x4`:        "an escape this does not read",
		`\x１２`:       "an escape this does not read",
		`\0400`:      "an escape this does not read",
		`{`:          "a count this cannot read",
		`a{,3}`:      "a count this cannot read",
		`a{3,2}`:     "a count this cannot read",
		`*a`:         "something is left open",
		`a**`:        "something is left open",
		`(a`:         "something is left open",
		`[a`:         "something is left open",
		`a)`:         "a bracket closes nothing",
		`[z-a]`:      "a count this cannot read",
		`\`:          "an escape this does not read",
	} {
		isInadmissible(t, p, false, why)
	}
	for p, why := range map[string]string{
		`a{999999999}`:  "a count past the limit",
		`a{249999}`:     "more than 250000 states",
		`(a{500}){500}`: "more than 250000 states",
		`a{134217727}`:  "more than 250000 states",
		strings.Repeat("(", Deepest+1) + "a" + strings.Repeat(")", Deepest+1): "nest deeper",
	} {
		isInadmissible(t, p, true, why)
	}
	// a{134217728} is past the count and the states. spec/pattern.md does not say which a reader
	// names; this one names the first it meets in the text.
	isInadmissible(t, `a{134217728}`, true, "a count past the limit")
	if err := Read(strings.Repeat("(", Deepest) + "a" + strings.Repeat(")", Deepest)); err != nil {
		t.Errorf("groups nested %d deep: %v", Deepest, err)
	}
}

// The states a pattern comes to are counted from what is written (spec/pattern.md). Each vector is
// one arm of a choice beside a filler, so an anchor in it stands at both ends of the string; the
// choice is one, each arm one more than itself, and the pattern one more, so the whole is
// 4 + the vector + the filler. At the limit it is read, one past it is not. 199x-notation's reader
// is held to the same vectors.
func TestStatesAreCountedFromWhatIsWritten(t *testing.T) {
	for p, n := range map[string]int{
		``: 0, `a`: 1, `abc`: 3, `.`: 1, `[a-z]`: 1, `[^a]`: 1, `\d`: 1, `\x{1F600}`: 1,
		`\uD83D\uDE00`: 1, "\U0001F600": 1, `()`: 0, `(?:a)`: 1, `a|b`: 5, `a|b|c`: 7,
		`(?:a|b)|c`: 9, `a|`: 4, `a?`: 2, `a??`: 2, `a*`: 2, `a+`: 3, `a{3}`: 4, `a{2,5}`: 6,
		`a{2,}`: 4, `a{0,0}`: 1, `(?:ab){3}`: 7, `(?:a{2}){3}`: 10, `(?:a|b)*`: 6,
		`(?:){134217727}`: 1, `^a`: 2, `a^b`: 3, `a$`: 2, `^a$`: 3,
	} {
		filler := MostStates - 4 - n
		at := fmt.Sprintf("(?:%s)|a{0,%d}", p, filler-1)
		over := fmt.Sprintf("(?:%s)|a{0,%d}", p, filler)
		if err := Read(at); err != nil {
			t.Errorf("%q at the limit is refused: %v", p, err)
		}
		isInadmissible(t, over, true, "states")
	}
	if err := Read(`a{249998}`); err != nil {
		t.Errorf("a{249998} is %d states and is read: %v", MostStates, err)
	}
}

// A limit is about a pattern, so text that is no pattern is refused whatever limit it also went
// past: the reading goes on past a count or a depth to the end, and places the anchors, before a
// limit is reported. 199x-notation's reader refuses the same text.
func TestTextThatIsNoPatternIsRefusedWhateverLimitItWentPast(t *testing.T) {
	past := strings.Repeat("(?:", Deepest+1)
	closed := strings.Repeat(")", Deepest+1)
	for p, why := range map[string]string{
		`a{134217728x}`:          "something is left open",
		`a{134217728}\p{L}`:      "a character property",
		`a{134217728}(`:          "something is left open",
		past + `a`:               "something is left open",
		past + `(a|)^b` + closed: "an anchor",
		past + `(?=a)` + closed:  "a group the grammar does not have",
		`a{200000000,150000000}`: "a count this cannot read",
	} {
		isInadmissible(t, p, false, why)
	}
	isInadmissible(t, `a{000134217728}`, true, "a count past the limit")
	// Past the count and the depth, in either order. spec/pattern.md does not say which a reader
	// names; this one names the first it meets in the text.
	isInadmissible(t, `a{134217728}`+past+`a`+closed, true, "a count past the limit")
	isInadmissible(t, past+`a{134217728}`+closed, true, "groups nest deeper")
	if err := Read(`a{0003,0005}`); err != nil {
		t.Errorf("a{0003,0005}: %v", err)
	}
}

// Text nested far past the depth limit is still read to its end, and its anchors placed, before
// the limit is reported: the reader and the placing of anchors keep stacks of their own, so how
// deep the text is never decides whether it is read. 199x-notation's reader is held to the same.
func TestTextFarPastTheDepthLimitIsReadToItsEnd(t *testing.T) {
	const deep = 1_000_000
	opened := strings.Repeat("(?:", deep)
	closed := strings.Repeat(")", deep)
	isInadmissible(t, opened+"a"+closed, true, "groups nest deeper")
	isInadmissible(t, opened+"a"+closed[1:], false, "something is left open")
	isInadmissible(t, opened+"a*^b"+closed, false, "an anchor")
	isInadmissible(t, opened+"^a"+strings.Repeat(")*", deep), false, "an anchor")
	isInadmissible(t, opened+"a"+strings.Repeat(")*", deep), true, "groups nest deeper")
}
