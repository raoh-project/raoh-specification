package value

import (
	"fmt"
	"math"
	"math/big"
	"strings"
)

// Cmp compares two numbers by value: -1, 0 or 1.
func (n Number) Cmp(o Number) int {
	sign := func(x Number) int {
		switch {
		case x.IsZero():
			return 0
		case x.Neg:
			return -1
		}
		return 1
	}
	sn, so := sign(n), sign(o)
	if sn != so || sn == 0 {
		return cmpInt(sn, so)
	}
	mag := n.cmpMagnitude(o)
	if sn < 0 {
		return -mag
	}
	return mag
}

// cmpMagnitude compares the absolute values of two non-zero numbers, by the exponent of their
// first digit and then digit by digit.
func (n Number) cmpMagnitude(o Number) int {
	lead := func(x Number) *big.Int { return new(big.Int).Add(x.Exp, big.NewInt(int64(len(x.Digits)))) }
	if c := lead(n).Cmp(lead(o)); c != 0 {
		return c
	}
	a, b := n.Digits, o.Digits
	if len(a) < len(b) {
		a += strings.Repeat("0", len(b)-len(a))
	} else {
		b += strings.Repeat("0", len(a)-len(b))
	}
	return strings.Compare(a, b)
}

func cmpInt[T int | int64](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Compare orders two values of the same type as the operations that bound values order them
// (min, max, range, before, after, between, ...): integers and decimals by value; floats as
// Double.compare does, with -0 before +0 and NaN after everything; temporal values by their
// fields, and offset date-times as OffsetDateTime.compareTo does, by instant and then by local
// date-time. Other types have no order.
func Compare(a, b Value) (int, error) {
	if !a.Type.Same(b.Type) {
		return 0, fmt.Errorf("cannot compare a %s with a %s", a.Type, b.Type)
	}
	switch a.Type.Kind {
	case Int32, Int64:
		return a.Int.Cmp(b.Int), nil
	case Float32, Float64:
		return compareFloat(a.Float, b.Float), nil
	case Decimal:
		return decimalNumber(a.Dec).Cmp(decimalNumber(b.Dec)), nil
	case Date, Time, DateTime:
		return compareFields(a.Time, b.Time), nil
	case Instant:
		if c := cmpInt(a.Time.Epoch, b.Time.Epoch); c != 0 {
			return c, nil
		}
		return cmpInt(a.Time.Nano, b.Time.Nano), nil
	case OffsetDateTime:
		ia, ib := a.Time.Epoch-int64(a.Time.Offset), b.Time.Epoch-int64(b.Time.Offset)
		if c := cmpInt(ia, ib); c != 0 {
			return c, nil
		}
		if c := cmpInt(a.Time.Nano, b.Time.Nano); c != 0 {
			return c, nil
		}
		return compareFields(a.Time, b.Time), nil
	}
	return 0, fmt.Errorf("a %s has no order", a.Type)
}

// compareFloat orders as Double.compare: NaN is greatest, and -0 is less than +0.
func compareFloat(a, b float64) int {
	switch {
	case math.IsNaN(a) && math.IsNaN(b):
		return 0
	case math.IsNaN(a):
		return 1
	case math.IsNaN(b):
		return -1
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return cmpInt(boolInt(!math.Signbit(a)), boolInt(!math.Signbit(b)))
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// compareFields orders by year, month, day, hour, minute, second and nanosecond.
func compareFields(a, b Temporal) int {
	for _, p := range [][2]int{
		{a.Year, b.Year}, {a.Month, b.Month}, {a.Day, b.Day},
		{a.Hour, b.Hour}, {a.Minute, b.Minute}, {a.Second, b.Second}, {a.Nano, b.Nano},
	} {
		if c := cmpInt(p[0], p[1]); c != 0 {
			return c
		}
	}
	return 0
}

func decimalNumber(d Dec) Number {
	digits := d.Unscaled.String()
	neg := strings.HasPrefix(digits, "-")
	digits = strings.TrimPrefix(digits, "-")
	n, err := ParseNumber(digits + "e" + big.NewInt(-int64(d.Scale)).String())
	if err != nil {
		// A coefficient is digits and a scale an integer, so this is a JSON number.
		panic(err)
	}
	n.Neg = neg && !n.IsZero()
	return n
}

// IsZero reports whether a numeric value is zero, of either sign.
func IsZero(v Value) bool {
	switch v.Type.Kind {
	case Int32, Int64:
		return v.Int.Sign() == 0
	case Float32, Float64:
		return v.Float == 0
	case Decimal:
		return v.Dec.Unscaled.Sign() == 0
	}
	return false
}
