package value

import (
	"fmt"
	"math"
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

// formatFloat writes a float as Double.toString and Float.toString do: the shortest decimal that
// reads back as the float, plainly with at least one digit after the point when
// 10^-3 <= |v| < 10^7, and as d.dddEn otherwise.
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
	e := strconv.FormatFloat(f, 'e', -1, bits) // d.ddde±xx
	mantissa, exp, _ := strings.Cut(e, "e")
	digits := strings.Replace(mantissa, ".", "", 1)
	x, _ := strconv.Atoi(exp)
	if f >= 1e-3 && f < 1e7 {
		if x >= 0 {
			for len(digits) < x+1 {
				digits += "0"
			}
			frac := digits[x+1:]
			if frac == "" {
				frac = "0"
			}
			return sign + digits[:x+1] + "." + frac
		}
		return sign + "0." + strings.Repeat("0", -x-1) + digits
	}
	frac := digits[1:]
	if frac == "" {
		frac = "0"
	}
	return sign + digits[:1] + "." + frac + "E" + strconv.Itoa(x)
}

// formatDecimal writes a decimal as BigDecimal.toString does.
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

// formatYear writes a year as LocalDate.toString does: four digits, with - when negative, and
// more digits only when needed, then always signed.
func formatYear(y int) string {
	switch {
	case y > 9999:
		return fmt.Sprintf("+%d", y)
	case y < 0:
		return fmt.Sprintf("-%04d", -y)
	}
	return fmt.Sprintf("%04d", y)
}

// formatTime writes a time of day as LocalTime.toString does, leaving out seconds that are zero
// unless always is set, as Instant.toString sets it; a fraction takes three, six or nine digits.
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
