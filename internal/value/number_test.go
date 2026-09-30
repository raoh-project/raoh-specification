package value

import (
	"testing"
	"time"
)

func TestNumbersEqualByValue(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"1.50", "1.5", true},
		{"100", "1e2", true},
		{"100e-2", "1", true},
		{"0", "-0.0", true},
		{"0.000", "0e999", true},
		{"-1", "1", false},
		{"1e10000000", "1", false},
		{"1e10000000", "10e9999999", true},
		{"1e-99999999", "0", false},
		{"12345678901234567890", "1.234567890123456789e19", true},
	} {
		a, err := ParseNumber(c.a)
		if err != nil {
			t.Fatal(err)
		}
		b, err := ParseNumber(c.b)
		if err != nil {
			t.Fatal(err)
		}
		if a.Equal(b) != c.same {
			t.Errorf("%s == %s: %v, want %v", c.a, c.b, a.Equal(b), c.same)
		}
	}
	for _, bad := range []string{"", "+1", "01", "1.", ".5", "1e", "--1", "NaN"} {
		if _, err := ParseNumber(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// The work a comparison takes depends on the length of the text, never on the size of the
// exponent: 10^exponent is never built.
func TestComparingHugeExponentsTakesNoTime(t *testing.T) {
	start := time.Now()
	for _, s := range []string{"1e999999999999999999999999999999", "1e-999999999999999999999999999999", "9e2147483648"} {
		n, err := ParseNumber(s)
		if err != nil {
			t.Fatal(err)
		}
		one, _ := ParseNumber("1")
		if n.Equal(one) {
			t.Errorf("%s equals 1", s)
		}
	}
	invalid(t, "float64", "1e-99999999999", "rounds to")
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v", d)
	}
}
