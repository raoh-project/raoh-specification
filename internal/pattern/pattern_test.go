package pattern

import (
	"strings"
	"testing"
)

// The pattern language reads what Souther's reads, and refuses what it refuses, each for its own
// reason (spec/pattern.md).
func TestPatternsAreReadAsSoutherReadsThem(t *testing.T) {
	for _, p := range []string{
		`^[0-9]{3}$`, `[0-9]{3}`, `a|`, `|a`, `(?:)`, `()`, `[]]`, `[^]]`, `[\d-z]`, `[-a]`, `[a-]`,
		`\x{1F600}`, `\x41`, `😀`, `A`, `\0101`, `\.`, `\\`, `\-`, `a{2,}?`, `a{2}`,
		`a{0,3}`, `a+?`, `(^a|b$)`, `a^b`, `^abc$`, `.`, `\s\S\d\D\w\W`, `\t\n\r\f\a\e`, "a\x00b",
		`[\x{0}-\x{10FFFF}]`, `[^\x{0}-\x{10FFFF}]`, `😀+`, `(a)(b)`, `}`, `]`,
	} {
		if err := Read(p); err != nil {
			t.Errorf("%q is refused: %v", p, err)
		}
	}
	for p, why := range map[string]string{
		`(?=a)`:        "a group the grammar does not have",
		`(?<n>a)`:      "a group the grammar does not have",
		`(?i)a`:        "a group the grammar does not have",
		`\1`:           "a back reference",
		`\k<n>`:        "a back reference",
		`\p{L}`:        "a character property",
		`\b`:           "a boundary",
		`\z`:           "a boundary",
		`\Qa\E`:        "a quotation",
		`a++`:          "a possessive repetition",
		`a{2}+`:        "a possessive repetition",
		`[a&&b]`:       "a class of classes",
		`[[a]]`:        "a class of classes",
		`(a|)^b`:       "an anchor",
		`(^a)*`:        "an anchor",
		`a$b`:          "an anchor",
		`\uD800`:       "a character no string holds",
		`\x{DC00}`:     "a character no string holds",
		`\q`:           "an escape this does not read",
		`\é`:           "an escape this does not read",
		`\x{110000}`:   "an escape this does not read",
		`\x4`:          "an escape this does not read",
		`\x１２`:         "an escape this does not read",
		`\0400`:        "an escape this does not read",
		`[\d-z-\w]`:    "an escape this does not read",
		`[a-\d]`:       "an escape this does not read",
		`{`:            "a count this cannot read",
		`a{,3}`:        "a count this cannot read",
		`a{3,2}`:       "a count this cannot read",
		`a{999999999}`: "a count this cannot read",
		`*a`:           "something is left open",
		`a**`:          "something is left open",
		`(a`:           "something is left open",
		`[a`:           "something is left open",
		`a)`:           "a bracket closes nothing",
		`[z-a]`:        "a count this cannot read",
		`\`:            "an escape this does not read",
	} {
		err := Read(p)
		switch {
		case why == "":
			if err != nil {
				t.Errorf("%q is refused: %v", p, err)
			}
		case err == nil:
			t.Errorf("%q is read", p)
		case !strings.Contains(err.Error(), why):
			t.Errorf("%q is refused for another reason: %v", p, err)
		}
	}
	deep := strings.Repeat("(", Deepest+1) + "a" + strings.Repeat(")", Deepest+1)
	if err := Read(deep); err == nil || !strings.Contains(err.Error(), "nest deeper") {
		t.Errorf("groups nested %d deep: %v", Deepest+1, err)
	}
	if err := Read(strings.Repeat("(", Deepest) + "a" + strings.Repeat(")", Deepest)); err != nil {
		t.Errorf("groups nested %d deep: %v", Deepest, err)
	}
}
