package value

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

// Number is a number as a JSON number writes it, reduced to a sign, its significant digits and
// the decimal exponent of the last of them. Reading and comparing numbers goes through Number
// and nothing else, so that the work never depends on the size of an exponent: 10^exponent is
// never built, and a text like 1e999999999999 costs what its length costs.
type Number struct {
	Neg bool
	// Digits are the significant digits, with no leading or trailing zero; empty for zero.
	Digits string
	// Exp is the exponent: the value is Digits × 10^Exp.
	Exp *big.Int
}

var numberPattern = regexp.MustCompile(`^(-?)(0|[1-9][0-9]*)(?:\.([0-9]+))?(?:[eE]([+-]?[0-9]+))?$`)

// ParseNumber reads a number written as RFC 8259 writes a JSON number.
func ParseNumber(s string) (Number, error) {
	m := numberPattern.FindStringSubmatch(s)
	if m == nil {
		return Number{}, fmt.Errorf("%q is not a JSON number", s)
	}
	exp := new(big.Int)
	if m[4] != "" {
		exp.SetString(strings.TrimPrefix(m[4], "+"), 10)
	}
	digits := m[2] + m[3]
	exp.Sub(exp, big.NewInt(int64(len(m[3]))))
	trimmed := strings.TrimRight(digits, "0")
	exp.Add(exp, big.NewInt(int64(len(digits)-len(trimmed))))
	trimmed = strings.TrimLeft(trimmed, "0")
	if trimmed == "" {
		return Number{Exp: new(big.Int)}, nil
	}
	return Number{Neg: m[1] == "-", Digits: trimmed, Exp: exp}, nil
}

// IsZero reports whether the number is zero, whatever its sign.
func (n Number) IsZero() bool { return n.Digits == "" }

// Equal reports whether two numbers have the same value; zero equals zero whatever the sign.
func (n Number) Equal(o Number) bool {
	if n.IsZero() || o.IsZero() {
		return n.IsZero() && o.IsZero()
	}
	return n.Neg == o.Neg && n.Digits == o.Digits && n.Exp.Cmp(o.Exp) == 0
}
