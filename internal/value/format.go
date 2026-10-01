package value

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// FormatForMessage writes a value as a message template shows it. See spec/issues.md.
func FormatForMessage(v Value) (string, error) {
	switch v.Type.Kind {
	case Bool:
		return strconv.FormatBool(v.Bool), nil
	case Int32, Int64:
		return v.Int.String(), nil
	case Float32:
		return formatFloat(v.Float, 32), nil
	case Float64:
		return formatFloat(v.Float, 64), nil
	case Decimal:
		return formatDecimal(v.Dec), nil
	case String, Symbol, UUID, URI:
		return v.Str, nil
	case Date:
		return formatDate(v.Time), nil
	case Time:
		return formatTime(v.Time, false), nil
	case DateTime:
		return formatDate(v.Time) + "T" + formatTime(v.Time, false), nil
	case OffsetDateTime:
		return formatDate(v.Time) + "T" + formatTime(v.Time, false) + formatOffset(v.Time.Offset), nil
	case Instant:
		return formatDate(v.Time) + "T" + formatTime(v.Time, true) + "Z", nil
	case List:
		parts := make([]string, len(v.Elems))
		for i, e := range v.Elems {
			s, err := FormatForMessage(e)
			if err != nil {
				return "", err
			}
			parts[i] = s
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	}
	return "", fmt.Errorf("a %s has no message form", v.Type)
}

// formatFloat writes the message form of a float (spec/issues.md, Message forms): the canonical
// decimal of the float, written plainly when the exponent of its first digit is from -3 to 6, and as
// d.dddEn otherwise, with at least one digit after the point either way.
func formatFloat(f float64, bits int) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		if math.Signbit(f) {
			return "-0.0"
		}
		return "0.0"
	}
	sign := ""
	if f < 0 {
		sign = "-"
		f = -f
	}
	digits, e := canonicalDecimal(f, bits)
	if e >= -3 && e < 7 {
		if e >= 0 {
			for len(digits) < e+1 {
				digits += "0"
			}
			frac := digits[e+1:]
			if frac == "" {
				frac = "0"
			}
			return sign + digits[:e+1] + "." + frac
		}
		return sign + "0." + strings.Repeat("0", -e-1) + digits
	}
	frac := digits[1:]
	if frac == "" {
		frac = "0"
	}
	return sign + digits[:1] + "." + frac + "E" + strconv.Itoa(e)
}

// canonicalDecimal selects the canonical decimal of a positive finite float m of the given width,
// as spec/issues.md defines it. R is the set of decimals that round to m under round to nearest,
// ties to even; p is the least length of a decimal in R; T is the decimals in R of length p, or of
// length 1 or 2 when p is 1; the canonical decimal is the one in T closest to m, or of the two
// equally close, the one with the even significand. It returns the significand's digits, without
// trailing zeros, and the exponent of the first digit.
//
// Everything is exact rational arithmetic on m and the bounds of R, so the result depends on no
// library's choice among equally short decimals.
func canonicalDecimal(f float64, bits int) (string, int) {
	m := new(big.Rat).SetFloat64(f)
	low, high, closed := roundingInterval(f, bits)
	inR := func(d *big.Rat) bool {
		if closed {
			return d.Cmp(low) >= 0 && d.Cmp(high) <= 0
		}
		return d.Cmp(low) > 0 && d.Cmp(high) < 0
	}
	e10 := decimalExponent(m)
	// The decimals of length n nearest m on either side are c·10^s and (c+1)·10^s, where
	// s = e10-n+1 and c is m/10^s rounded down. R is an interval around m, so it holds a decimal of
	// length n exactly when it holds one of these two.
	grid := func(n int) (*big.Int, int) {
		s := e10 - n + 1
		q := new(big.Rat).Quo(m, pow10(s))
		return new(big.Int).Quo(q.Num(), q.Denom()), s
	}
	at := func(c *big.Int, s int) *big.Rat {
		return new(big.Rat).Mul(new(big.Rat).SetInt(c), pow10(s))
	}
	p := 1
	for ; ; p++ {
		c, s := grid(p)
		if inR(at(c, s)) || inR(at(new(big.Int).Add(c, big.NewInt(1)), s)) {
			break
		}
	}
	n := p
	if p == 1 {
		n = 2
	}
	c, s := grid(n)
	below, above := c, new(big.Int).Add(c, big.NewInt(1))
	var chosen *big.Int
	switch inBelow, inAbove := inR(at(below, s)), inR(at(above, s)); {
	case inBelow && inAbove:
		dBelow := new(big.Rat).Sub(m, at(below, s))
		dAbove := new(big.Rat).Sub(at(above, s), m)
		switch dBelow.Cmp(dAbove) {
		case -1:
			chosen = below
		case 1:
			chosen = above
		default:
			chosen = below
			if below.Bit(0) == 1 {
				chosen = above
			}
		}
	case inBelow:
		chosen = below
	default:
		chosen = above
	}
	ten := big.NewInt(10)
	for chosen.Sign() != 0 {
		q, r := new(big.Int).QuoRem(chosen, ten, new(big.Int))
		if r.Sign() != 0 {
			break
		}
		chosen, s = q, s+1
	}
	digits := chosen.String()
	return digits, s + len(digits) - 1
}

// canonicalText writes the canonical decimal of a finite float as a JSON number, d...de±x.
func canonicalText(f float64, bits int) string {
	if f == 0 {
		return "0"
	}
	sign := ""
	if f < 0 {
		sign, f = "-", -f
	}
	digits, e := canonicalDecimal(f, bits)
	return sign + digits + "e" + strconv.Itoa(e-len(digits)+1)
}

// roundingInterval gives the bounds of the decimals that round to the positive finite float f of
// the given width: the midpoints to its neighbours, included when f's significand is even, since a
// tie rounds to even. Past the greatest finite float the next value would be one gap further on.
func roundingInterval(f float64, bits int) (low, high *big.Rat, closed bool) {
	var prev, next float64
	if bits == 32 {
		f32 := float32(f)
		prev = float64(math.Nextafter32(f32, 0))
		next = float64(math.Nextafter32(f32, float32(math.Inf(1))))
		closed = math.Float32bits(f32)&1 == 0
	} else {
		prev = math.Nextafter(f, 0)
		next = math.Nextafter(f, math.Inf(1))
		closed = math.Float64bits(f)&1 == 0
	}
	m := new(big.Rat).SetFloat64(f)
	below := new(big.Rat).SetFloat64(prev)
	var above *big.Rat
	if math.IsInf(next, 1) || (bits == 32 && next > math.MaxFloat32) {
		above = new(big.Rat).Sub(new(big.Rat).Add(m, m), below)
	} else {
		above = new(big.Rat).SetFloat64(next)
	}
	half := big.NewRat(1, 2)
	low = new(big.Rat).Mul(new(big.Rat).Add(m, below), half)
	high = new(big.Rat).Mul(new(big.Rat).Add(m, above), half)
	return low, high, closed
}

// decimalExponent gives the e for which 10^e <= m < 10^(e+1).
func decimalExponent(m *big.Rat) int {
	f, _ := m.Float64()
	e := int(math.Floor(math.Log10(f)))
	for pow10(e).Cmp(m) > 0 {
		e--
	}
	for pow10(e+1).Cmp(m) <= 0 {
		e++
	}
	return e
}

// pow10 gives 10^e exactly.
func pow10(e int) *big.Rat {
	if e >= 0 {
		return new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(e)), nil))
	}
	return new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-e)), nil))
}

// formatDecimal writes the message form of a decimal (spec/issues.md, Message forms): the
// coefficient with the point placed by the scale when the scale is not negative and the adjusted
// exponent is at least -6, and scientific notation with the adjusted exponent otherwise.
func formatDecimal(d Dec) string {
	sign := ""
	coefficient := d.Unscaled.String()
	if strings.HasPrefix(coefficient, "-") {
		sign, coefficient = "-", coefficient[1:]
	}
	scale := int(d.Scale)
	adjusted := -scale + len(coefficient) - 1
	if scale >= 0 && adjusted >= -6 {
		switch {
		case scale == 0:
			return sign + coefficient
		case len(coefficient) > scale:
			return sign + coefficient[:len(coefficient)-scale] + "." + coefficient[len(coefficient)-scale:]
		default:
			return sign + "0." + strings.Repeat("0", scale-len(coefficient)) + coefficient
		}
	}
	s := sign + coefficient[:1]
	if len(coefficient) > 1 {
		s += "." + coefficient[1:]
	}
	s += "E"
	if adjusted >= 0 {
		s += "+"
	}
	return s + strconv.Itoa(adjusted)
}

func formatDate(t Temporal) string {
	return fmt.Sprintf("%s-%02d-%02d", formatYear(t.Year), t.Month, t.Day)
}

// formatYear writes a year as spec/issues.md says: four digits, with - when negative, and more
// digits only when needed, then always signed.
func formatYear(y int) string {
	switch {
	case y > 9999:
		return fmt.Sprintf("+%d", y)
	case y < 0:
		return fmt.Sprintf("-%04d", -y)
	}
	return fmt.Sprintf("%04d", y)
}

// formatTime writes a time of day as spec/issues.md says, leaving out seconds that are zero unless
// always is set, as an instant sets it; a fraction takes three, six or nine digits.
func formatTime(t Temporal, always bool) string {
	s := fmt.Sprintf("%02d:%02d", t.Hour, t.Minute)
	if !always && t.Second == 0 && t.Nano == 0 {
		return s
	}
	s += fmt.Sprintf(":%02d", t.Second)
	switch {
	case t.Nano == 0:
	case t.Nano%1_000_000 == 0:
		s += fmt.Sprintf(".%03d", t.Nano/1_000_000)
	case t.Nano%1_000 == 0:
		s += fmt.Sprintf(".%06d", t.Nano/1_000)
	default:
		s += fmt.Sprintf(".%09d", t.Nano)
	}
	return s
}

func formatOffset(secs int) string {
	if secs == 0 {
		return "Z"
	}
	sign := "+"
	if secs < 0 {
		sign, secs = "-", -secs
	}
	s := fmt.Sprintf("%s%02d:%02d", sign, secs/3600, secs/60%60)
	if secs%60 != 0 {
		s += fmt.Sprintf(":%02d", secs%60)
	}
	return s
}
