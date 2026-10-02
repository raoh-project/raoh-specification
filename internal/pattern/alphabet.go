package pattern

import (
	_ "embed"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// derivedGeneralCategory is the Unicode Character Database's file of General_Category values, as
// Unicode publishes it, of the version spec/pattern.md names. Which characters keep a backslash is
// read from it and from nothing else: Go's unicode package has the version of the Go that builds
// the verifier, and that would move the answer with it.
//
//go:embed ucd/18.0.0/extracted/DerivedGeneralCategory.txt
var derivedGeneralCategory string

// unicodeVersion is the version of Unicode whose letters and decimal digits keep a backslash.
const unicodeVersion = "18.0.0"

// span is the characters from one scalar value to another, both included.
type span struct{ from, to rune }

// keepingBackslash are the characters of General_Category L or Nd, in order.
var keepingBackslash = readSpans(derivedGeneralCategory, unicodeVersion, "Lu", "Ll", "Lt", "Lm", "Lo", "Nd")

// keepsBackslash tells whether a backslash before c is kept for an escape of its own, so that the
// two do not stand for c: c is a letter or a decimal digit (spec/pattern.md).
func keepsBackslash(c rune) bool {
	_, found := slices.BinarySearchFunc(keepingBackslash, c, func(s span, c rune) int {
		switch {
		case s.to < c:
			return -1
		case s.from > c:
			return 1
		}
		return 0
	})
	return found
}

// readSpans reads the characters of the given categories from a DerivedGeneralCategory file of the
// given version. The file is part of the verifier, so one that is not what it should be is a fault
// of the build, not of anything the verifier reads.
func readSpans(file, version string, categories ...string) []span {
	first, _, _ := strings.Cut(file, "\n")
	if want := "# DerivedGeneralCategory-" + version + ".txt"; first != want {
		panic(fmt.Sprintf("the General_Category file begins %q, not %q", first, want))
	}
	var spans []span
	for line := range strings.Lines(file) {
		data, _, _ := strings.Cut(line, "#")
		values, category, ok := strings.Cut(data, ";")
		if !ok || !slices.Contains(categories, strings.TrimSpace(category)) {
			continue
		}
		low, high, ranged := strings.Cut(strings.TrimSpace(values), "..")
		if !ranged {
			high = low
		}
		spans = append(spans, span{scalar(low), scalar(high)})
	}
	slices.SortFunc(spans, func(a, b span) int { return int(a.from - b.from) })
	return spans
}

func scalar(hex string) rune {
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		panic(fmt.Sprintf("the General_Category file has %q for a scalar value", hex))
	}
	return rune(v)
}
