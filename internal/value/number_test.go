package value

import (
	"runtime"
	"testing"
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
// exponent: 10^exponent is never built. The allocations are counted, not the time, so the test
// means the same on every machine.
func TestComparingHugeExponentsBuildsNoPower(t *testing.T) {
	one, _ := ParseNumber("1")
	for _, s := range []string{"1e999999999999999999999999999999", "1e-999999999999999999999999999999", "9e2147483648"} {
		var n Number
		allocs := testing.AllocsPerRun(10, func() {
			var err error
			if n, err = ParseNumber(s); err != nil {
				t.Fatal(err)
			}
			if n.Equal(one) {
				t.Errorf("%s equals 1", s)
			}
			if n.Cmp(one) == 0 {
				t.Errorf("%s compares equal to 1", s)
			}
		})
		if allocs > 64 {
			t.Errorf("comparing %s allocates %v times", s, allocs)
		}
	}
	var bytes uint64
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	invalid(t, "float64", "1e-99999999999", "rounds to")
	runtime.ReadMemStats(&after)
	if bytes = after.TotalAlloc - before.TotalAlloc; bytes > 1<<20 {
		t.Errorf("observing 1e-99999999999 as a float64 allocates %d bytes", bytes)
	}
}
