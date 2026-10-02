package pattern

import (
	_ "embed"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// derivedGeneralCategory is extracted/DerivedGeneralCategory.txt of Unicode 18.0.0, byte for byte
// as Unicode publishes it at https://www.unicode.org/Public/18.0.0/ucd/extracted/. Its SHA-256 is
// derivedGeneralCategorySHA256, which 199x-notation records for the same file.
//
// The characters a backslash is not kept before are read from it, never from Go's unicode package,
// whose data is the Unicode version of the Go that builds the verifier.
//
//go:embed ucd/18.0.0/DerivedGeneralCategory.txt
var derivedGeneralCategory string

const derivedGeneralCategorySHA256 = "d6b151d2d40ee9b1876d26f417980f45ffae47b6055ccf7203cb31f07a030f94"

// letterAndDigitCategories are General_Category L, written in the file as its five values, and
// Nd (spec/pattern.md). The file has no line for L itself.
var letterAndDigitCategories = []string{"Lu", "Ll", "Lt", "Lm", "Lo", "Nd"}

// span is the code points from lo to hi, both included.
type span struct{ lo, hi rune }

// lettersAndDigits are the characters of letterAndDigitCategories, ordered and apart.
var lettersAndDigits = readCategories(derivedGeneralCategory, letterAndDigitCategories)

// readCategories reads the code points of the given General_Category values from a file of
// DerivedGeneralCategory.txt's format: lines of a code point or a range, a semicolon and a value,
// each optionally followed by a comment.
func readCategories(file string, categories []string) []span {
	var spans []span
	for n, line := range strings.Split(file, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		points, value, ok := strings.Cut(line, ";")
		if !ok {
			panic(fmt.Sprintf("DerivedGeneralCategory.txt line %d is not a code point and a value", n+1))
		}
		if !slices.Contains(categories, strings.TrimSpace(value)) {
			continue
		}
		lo, hi, isRange := strings.Cut(strings.TrimSpace(points), "..")
		if !isRange {
			hi = lo
		}
		spans = append(spans, span{codePoint(lo, n), codePoint(hi, n)})
	}
	slices.SortFunc(spans, func(a, b span) int { return int(a.lo - b.lo) })
	return spans
}

func codePoint(hex string, n int) rune {
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		panic(fmt.Sprintf("DerivedGeneralCategory.txt line %d: %v", n+1, err))
	}
	return rune(v)
}

// isLetterOrDigit tells whether a character is a letter or a decimal digit as spec/pattern.md
// means them: of General_Category L or Nd in Unicode 18.0.0.
func isLetterOrDigit(c rune) bool {
	_, found := slices.BinarySearchFunc(lettersAndDigits, c, func(s span, c rune) int {
		switch {
		case s.hi < c:
			return -1
		case s.lo > c:
			return 1
		}
		return 0
	})
	return found
}
