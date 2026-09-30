package value

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/raoh-project/raoh-specification/jsontext"
)

// Observe reads the observation n of a value of type t. See spec/observation.md.
func Observe(t Type, n *jsontext.Node) (Value, error) {
	v, err := observe(t, n)
	if err != nil {
		return Value{}, fmt.Errorf("%s: %s is not a %s observation: %w", t, abbreviate(n.Raw), t, err)
	}
	return v, nil
}

func abbreviate(raw []byte) string {
	s := string(raw)
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}

func observe(t Type, n *jsontext.Node) (Value, error) {
	v := Value{Type: t}
	want := func(k jsontext.Kind) error {
		if n.Kind != k {
			return fmt.Errorf("expected %s, found %s", k, n.Kind)
		}
		return nil
	}
	switch t.Kind {
	case Bool:
		if err := want(jsontext.Bool); err != nil {
			return v, err
		}
		v.Bool = n.Bool
	case Int32, Int64:
		if err := want(jsontext.Number); err != nil {
			return v, err
		}
		i, ok := new(big.Int).SetString(n.Text, 10)
		if !ok {
			return v, fmt.Errorf("an integer is written without a fraction or an exponent")
		}
		bits := 32
		if t.Kind == Int64 {
			bits = 64
		}
		if i.BitLen() > bits-1 && !(i.Sign() < 0 && new(big.Int).Neg(i).Cmp(new(big.Int).Lsh(big.NewInt(1), uint(bits-1))) == 0) {
			return v, fmt.Errorf("outside the %s range", t)
		}
		v.Int = i
	case Float32, Float64:
		f, err := observeFloat(t, n)
		if err != nil {
			return v, err
		}
		v.Float = f
	case Decimal:
		if err := want(jsontext.String); err != nil {
			return v, err
		}
		d, err := ParseDecimal(n.Text)
		if err != nil {
			return v, err
		}
		v.Dec = d
	case String, Symbol, URI:
		if err := want(jsontext.String); err != nil {
			return v, err
		}
		v.Str = n.Text
	case UUID:
		if err := want(jsontext.String); err != nil {
			return v, err
		}
		if !uuidPattern.MatchString(n.Text) {
			return v, fmt.Errorf("a UUID is written in lower case as 8-4-4-4-12 hexadecimal digits")
		}
		v.Str = n.Text
	case Instant, Date, Time, DateTime, OffsetDateTime:
		if err := want(jsontext.String); err != nil {
			return v, err
		}
		tm, err := ParseTemporal(t.Kind, n.Text)
		if err != nil {
			return v, err
		}
		v.Time = tm
	case List, Set, Product:
		if err := want(jsontext.Array); err != nil {
			return v, err
		}
		if t.Kind == Product && len(n.Elems) != len(t.Args) {
			return v, fmt.Errorf("a %s has %d elements, found %d", t, len(t.Args), len(n.Elems))
		}
		v.Elems = make([]Value, len(n.Elems))
		for i, e := range n.Elems {
			et := t.Args[0]
			if t.Kind == Product {
				et = t.Args[i]
			}
			ev, err := observe(et, e)
			if err != nil {
				return v, fmt.Errorf("element %d: %w", i, err)
			}
			v.Elems[i] = ev
		}
		if t.Kind == Set {
			for i := range v.Elems {
				for j := range i {
					if Equal(v.Elems[i], v.Elems[j]) {
						return v, fmt.Errorf("elements %d and %d of a set are the same", j, i)
					}
				}
			}
		}
	case Map:
		if err := want(jsontext.Object); err != nil {
			return v, err
		}
		for _, m := range n.Members {
			ev, err := observe(t.Args[0], m.Value)
			if err != nil {
				return v, fmt.Errorf("member %q: %w", m.Name, err)
			}
			v.Keys = append(v.Keys, m.Name)
			v.Elems = append(v.Elems, ev)
		}
	case Record:
		if err := want(jsontext.Object); err != nil {
			return v, err
		}
		if len(n.Members) != len(t.Fields) {
			return v, fmt.Errorf("a %s has the fields %s, found %s", t, strings.Join(t.Fields, ","), strings.Join(n.Names(), ","))
		}
		v.Elems = make([]Value, len(t.Fields))
		for i, f := range t.Fields {
			m, ok := n.Get(f)
			if !ok {
				return v, fmt.Errorf("field %q is missing", f)
			}
			ev, err := observe(t.Args[i], m)
			if err != nil {
				return v, fmt.Errorf("field %q: %w", f, err)
			}
			v.Elems[i] = ev
		}
	case Presence:
		switch {
		case n.Kind == jsontext.String && n.Text == "absent":
			v.State = Absent
		case n.Kind == jsontext.String && n.Text == "null":
			v.State = Null
		case n.Kind == jsontext.Object && len(n.Members) == 1 && n.Members[0].Name == "present":
			ev, err := observe(t.Args[0], n.Members[0].Value)
			if err != nil {
				return v, err
			}
			v.Elems = []Value{ev}
		default:
			return v, fmt.Errorf(`expected "absent", "null" or {"present": value}`)
		}
	case Optional, Nullable:
		if n.Kind == jsontext.Null {
			v.State = Null
			break
		}
		ev, err := observe(t.Args[0], n)
		if err != nil {
			return v, err
		}
		v.Elems = []Value{ev}
	case JSON:
		v.Node = n
	default:
		return v, fmt.Errorf("a %s has no observation", t)
	}
	return v, nil
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// observeFloat reads a float observation: a JSON number whose value is the shortest decimal that
// reads back as the float it rounds to, or a tag for what a JSON number cannot carry reliably.
func observeFloat(t Type, n *jsontext.Node) (float64, error) {
	bits := 64
	if t.Kind == Float32 {
		bits = 32
	}
	if n.Kind == jsontext.Object {
		tag, ok := n.Get("float")
		if !ok || len(n.Members) != 1 || tag.Kind != jsontext.String {
			return 0, fmt.Errorf(`expected a number or {"float": tag}`)
		}
		switch tag.Text {
		case "-0":
			return math.Copysign(0, -1), nil
		case "NaN":
			return math.NaN(), nil
		case "+Infinity":
			return math.Inf(1), nil
		case "-Infinity":
			return math.Inf(-1), nil
		}
		return 0, fmt.Errorf(`the float tags are "-0", "NaN", "+Infinity" and "-Infinity"`)
	}
	if n.Kind != jsontext.Number {
		return 0, fmt.Errorf("expected number, found %s", n.Kind)
	}
	f, err := strconv.ParseFloat(n.Text, bits)
	if err != nil {
		return 0, fmt.Errorf("outside the %s range", t)
	}
	if f == 0 && strings.HasPrefix(n.Text, "-") {
		return 0, fmt.Errorf(`negative zero is written {"float": "-0"}`)
	}
	written, _ := new(big.Rat).SetString(n.Text)
	shortest, _ := new(big.Rat).SetString(strconv.FormatFloat(f, 'e', -1, bits))
	if written.Cmp(shortest) != 0 {
		return 0, fmt.Errorf("%s rounds to the %s %s", n.Text, t, strconv.FormatFloat(f, 'g', -1, bits))
	}
	return f, nil
}

var decimalPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// ParseDecimal reads a decimal written as a JSON number is, keeping its scale: "1.50" has scale 2,
// "1E+3" has scale -3.
func ParseDecimal(s string) (Dec, error) {
	if !decimalPattern.MatchString(s) {
		return Dec{}, fmt.Errorf("%q is not a decimal number", s)
	}
	mantissa, exponent := s, int64(0)
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		mantissa = s[:i]
		e, err := strconv.ParseInt(s[i+1:], 10, 32)
		if err != nil {
			return Dec{}, fmt.Errorf("%q has an exponent out of range", s)
		}
		exponent = e
	}
	scale := int64(0)
	if i := strings.IndexByte(mantissa, '.'); i >= 0 {
		scale = int64(len(mantissa) - i - 1)
		mantissa = mantissa[:i] + mantissa[i+1:]
	}
	scale -= exponent
	if scale < math.MinInt32 || scale > math.MaxInt32 {
		return Dec{}, fmt.Errorf("%q has a scale out of range", s)
	}
	u, _ := new(big.Int).SetString(mantissa, 10)
	return Dec{Unscaled: u, Scale: int32(scale)}, nil
}

var (
	datePattern   = `(-?[0-9]{4,})-([0-9]{2})-([0-9]{2})`
	timePattern   = `([0-9]{2}):([0-9]{2})(?::([0-9]{2})(?:\.([0-9]{1,9}))?)?`
	offsetPattern = `(Z|[+-][0-9]{2}:[0-9]{2}(?::[0-9]{2})?)`
	temporalRe    = map[Kind]*regexp.Regexp{
		Date:           regexp.MustCompile(`^` + datePattern + `$`),
		Time:           regexp.MustCompile(`^` + timePattern + `$`),
		DateTime:       regexp.MustCompile(`^` + datePattern + `T` + timePattern + `$`),
		OffsetDateTime: regexp.MustCompile(`^` + datePattern + `T` + timePattern + offsetPattern + `$`),
		Instant:        regexp.MustCompile(`^` + datePattern + `T` + timePattern + `Z$`),
	}
)

// ParseTemporal reads a temporal value written in ISO 8601 as spec/observation.md describes.
func ParseTemporal(k Kind, s string) (Temporal, error) {
	m := temporalRe[k].FindStringSubmatch(s)
	if m == nil {
		return Temporal{}, fmt.Errorf("%q is not a %s", s, kindNames[k])
	}
	var tm Temporal
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	rest := m[1:]
	if k != Time {
		tm.Year, tm.Month, tm.Day = atoi(rest[0]), atoi(rest[1]), atoi(rest[2])
		d := time.Date(tm.Year, time.Month(tm.Month), tm.Day, 0, 0, 0, 0, time.UTC)
		if d.Year() != tm.Year || int(d.Month()) != tm.Month || d.Day() != tm.Day {
			return Temporal{}, fmt.Errorf("%q is not a date", s)
		}
		rest = rest[3:]
	}
	if k != Date {
		tm.Hour, tm.Minute, tm.Second = atoi(rest[0]), atoi(rest[1]), atoi(rest[2])
		if rest[3] != "" {
			tm.Nano = atoi((rest[3] + "00000000")[:9])
		}
		if tm.Hour > 23 || tm.Minute > 59 || tm.Second > 59 {
			return Temporal{}, fmt.Errorf("%q is not a time of day", s)
		}
		rest = rest[4:]
	}
	if k == OffsetDateTime && rest[0] != "Z" {
		o := rest[0]
		sign := 1
		if o[0] == '-' {
			sign = -1
		}
		secs := atoi(o[1:3])*3600 + atoi(o[4:6])*60
		if len(o) > 6 {
			secs += atoi(o[7:9])
		}
		if secs > 18*3600 {
			return Temporal{}, fmt.Errorf("%q has an offset beyond 18 hours", s)
		}
		tm.Offset = sign * secs
	}
	if k == Instant {
		tm.Epoch = time.Date(tm.Year, time.Month(tm.Month), tm.Day, tm.Hour, tm.Minute, tm.Second, 0, time.UTC).Unix()
	}
	return tm, nil
}
