package pattern

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// The file is the one Unicode publishes for 18.0.0, unchanged.
func TestTheGeneralCategoriesAreUnicode18s(t *testing.T) {
	sum := sha256.Sum256([]byte(derivedGeneralCategory))
	if got := hex.EncodeToString(sum[:]); got != derivedGeneralCategorySHA256 {
		t.Fatalf("DerivedGeneralCategory.txt has SHA-256 %s, not %s", got, derivedGeneralCategorySHA256)
	}
	if !strings.HasPrefix(derivedGeneralCategory, "# DerivedGeneralCategory-18.0.0.txt\n") {
		t.Fatal("DerivedGeneralCategory.txt is not Unicode 18.0.0's")
	}
}

// A letter is one of General_Category L's five values, and a digit Nd; a character that only looks
// like either, of No or Nl, is neither.
func TestLettersAndDigitsAreLAndNd(t *testing.T) {
	for c, category := range map[rune]string{
		'A': "Lu", 'a': "Ll", 'ǅ': "Lt", 'ʰ': "Lm", 'א': "Lo", '0': "Nd", '٣': "Nd",
		'꟝': "Lu from Unicode 18.0.0",
	} {
		if !isLetterOrDigit(c) {
			t.Errorf("%U (%s) is not read as a letter or a digit", c, category)
		}
	}
	for c, category := range map[rune]string{
		'²': "No", '½': "No", 'Ⅰ': "Nl", '.': "Po", '_': "Pc", ' ': "Zs",
	} {
		if isLetterOrDigit(c) {
			t.Errorf("%U (%s) is read as a letter or a digit", c, category)
		}
	}
}

// A backslash is kept before a character that is neither a letter nor a digit, and refused before
// one that is, whatever Unicode version Go has.
func TestABackslashIsKeptBeforeNeitherALetterNorADigit(t *testing.T) {
	for _, p := range []string{`\.`, `\\`, `\²`, `\Ⅰ`} {
		if err := Read(p); err != nil {
			t.Errorf("%q is refused: %v", p, err)
		}
	}
	for _, p := range []string{`\٣`, "\\꟝", `\é`, `\q`} {
		if err := Read(p); err == nil || !strings.Contains(err.Error(), "an escape this does not read") {
			t.Errorf("%q: %v", p, err)
		}
	}
}
