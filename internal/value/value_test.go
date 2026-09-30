package value

import (
	"strings"
	"testing"

	"github.com/raoh-project/raoh-specification/internal/jsontext"
)

func obs(t *testing.T, typ, text string) Value {
	t.Helper()
	v, err := Observe(MustParseType(typ), jsontext.MustParse(text))
	if err != nil {
		t.Fatalf("%s %s: %v", typ, text, err)
	}
	return v
}

func invalid(t *testing.T, typ, text, why string) {
	t.Helper()
	_, err := Observe(MustParseType(typ), jsontext.MustParse(text))
	if err == nil {
		t.Errorf("%s %s accepted", typ, text)
	} else if !strings.Contains(err.Error(), why) {
		t.Errorf("%s %s: %v, want it to say %q", typ, text, err, why)
	}
}

func TestParseTypeRoundTrips(t *testing.T) {
	for _, s := range []string{
		"int32", "list<float32>", "product<string,int32,list<T>>", "presence<int32>",
		"record<candidate:int32,issues:json>", "map<set<decimal>>", "T",
	} {
		typ, err := ParseType(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		if typ.String() != s {
			t.Errorf("%s printed as %s", s, typ)
		}
	}
	for _, s := range []string{"list", "int32<string>", "list<int32,int32>", "foo", "list<int32", "record<int32>"} {
		if _, err := ParseType(s); err == nil {
			t.Errorf("%s accepted", s)
		}
	}
}

func TestSubstAndUnify(t *testing.T) {
	pattern := MustParseType("record<actual:T,allowed:list<T>>")
	bound := map[string]Type{}
	if !Unify(pattern, MustParseType("record<actual:float32,allowed:list<float32>>"), bound) {
		t.Fatal("did not unify")
	}
	if got := pattern.Subst(bound).String(); got != "record<actual:float32,allowed:list<float32>>" {
		t.Errorf("got %s", got)
	}
	if Unify(pattern, MustParseType("record<actual:float32,allowed:list<int32>>"), map[string]Type{}) {
		t.Error("unified T with two types")
	}
}

func TestFloatsCompareByValueNotLexeme(t *testing.T) {
	for _, typ := range []string{"float32", "float64"} {
		one := obs(t, typ, "1")
		for _, text := range []string{"1.0", "1e0", "10E-1"} {
			if !Equal(one, obs(t, typ, text)) {
				t.Errorf("%s: 1 and %s differ", typ, text)
			}
		}
	}
	if Equal(obs(t, "float32", "1"), obs(t, "float64", "1")) {
		t.Error("a float32 and a float64 are the same")
	}
}

func TestFloatObservationsAreTheShortestDecimal(t *testing.T) {
	obs(t, "float32", "0.1")
	obs(t, "float64", "0.1")
	obs(t, "float32", "16777216")
	obs(t, "float32", "3.4028235e38")
	invalid(t, "float32", "16777217", "rounds to")
	invalid(t, "float64", "0.10000000000000001", "rounds to")
	invalid(t, "float32", "3.4028236e38", "range")
	invalid(t, "float64", "1e400", "range")
}

func TestSignedZeroAndNonFiniteFloatsAreTagged(t *testing.T) {
	negZero := obs(t, "float64", `{"float": "-0"}`)
	if Equal(negZero, obs(t, "float64", "0")) {
		t.Error("-0 and 0 are the same")
	}
	if !Equal(negZero, obs(t, "float64", `{"float": "-0"}`)) {
		t.Error("-0 differs from itself")
	}
	if !Equal(obs(t, "float32", `{"float": "NaN"}`), obs(t, "float32", `{"float": "NaN"}`)) {
		t.Error("NaN differs from NaN")
	}
	if Equal(obs(t, "float64", `{"float": "+Infinity"}`), obs(t, "float64", `{"float": "-Infinity"}`)) {
		t.Error("the infinities are the same")
	}
	invalid(t, "float64", "-0", "negative zero")
	invalid(t, "float64", "-0.0", "negative zero")
	invalid(t, "float64", `{"float": "Infinity"}`, "tags")
}

func TestIntegers(t *testing.T) {
	obs(t, "int32", "-2147483648")
	invalid(t, "int32", "2147483648", "range")
	invalid(t, "int32", "-2147483649", "range")
	obs(t, "int64", "9223372036854775807")
	invalid(t, "int64", "9223372036854775808", "range")
	invalid(t, "int32", "1.0", "no fraction")
	invalid(t, "int32", `"1"`, "expected number")
}

func TestDecimalsKeepTheirScale(t *testing.T) {
	if Equal(obs(t, "decimal", `"1.5"`), obs(t, "decimal", `"1.50"`)) {
		t.Error("1.5 and 1.50 are the same decimal")
	}
	if !Equal(obs(t, "decimal", `"1E+3"`), obs(t, "decimal", `"1e3"`)) {
		t.Error("1E+3 and 1e3 differ")
	}
	d := obs(t, "decimal", `"-0.00010"`).Dec
	if d.Unscaled.String() != "-10" || d.Scale != 5 {
		t.Errorf("got %v scale %d", d.Unscaled, d.Scale)
	}
	invalid(t, "decimal", "1.5", "expected string")
	invalid(t, "decimal", `"1.5.0"`, "not a decimal")
}

func TestCollections(t *testing.T) {
	if !Equal(obs(t, "set<int32>", "[2, 1]"), obs(t, "set<int32>", "[1, 2]")) {
		t.Error("set order matters")
	}
	if Equal(obs(t, "list<int32>", "[2, 1]"), obs(t, "list<int32>", "[1, 2]")) {
		t.Error("list order does not matter")
	}
	invalid(t, "set<int32>", "[1, 1]", "are the same")
	if !Equal(obs(t, "map<int32>", `{"a":1,"b":2}`), obs(t, "map<int32>", `{"b":2,"a":1}`)) {
		t.Error("map key order matters")
	}
	obs(t, "product<string,int32>", `["Ken", 30]`)
	invalid(t, "product<string,int32>", `["Ken"]`, "has 2 elements")
	invalid(t, "product<string,int32>", `["Ken", "30"]`, "element 1")
}

func TestPresenceOptionalAndNullable(t *testing.T) {
	absent := obs(t, "presence<int32>", `"absent"`)
	null := obs(t, "presence<int32>", `"null"`)
	present := obs(t, "presence<int32>", `{"present": 3}`)
	if Equal(absent, null) || Equal(null, present) || !Equal(present, obs(t, "presence<int32>", `{"present": 3}`)) {
		t.Error("presence cases are confused")
	}
	if !Equal(obs(t, "optional<string>", "null"), obs(t, "optional<string>", "null")) {
		t.Error("empty optionals differ")
	}
	if Equal(obs(t, "nullable<string>", "null"), obs(t, "nullable<string>", `""`)) {
		t.Error("null and empty string are the same")
	}
}

func TestTemporalValuesCompareByValue(t *testing.T) {
	if !Equal(obs(t, "time", `"09:00"`), obs(t, "time", `"09:00:00"`)) {
		t.Error("09:00 and 09:00:00 differ")
	}
	if !Equal(obs(t, "datetime", `"2024-01-01T00:00"`), obs(t, "datetime", `"2024-01-01T00:00:00.000"`)) {
		t.Error("date-times differ")
	}
	if !Equal(obs(t, "instant", `"2024-01-01T00:00:00Z"`), obs(t, "instant", `"2024-01-01T00:00Z"`)) {
		t.Error("instants differ")
	}
	// As OffsetDateTime.equals: the same instant at another offset is another value.
	if Equal(obs(t, "offset_datetime", `"2024-01-01T09:00+09:00"`), obs(t, "offset_datetime", `"2024-01-01T00:00Z"`)) {
		t.Error("offset date-times at different offsets are the same")
	}
	obs(t, "instant", `"+1000000000-12-31T23:59:59.999999999Z"`)
	invalid(t, "date", `"2024-02-30"`, "not a date")
	invalid(t, "time", `"24:00"`, "not a time")
	invalid(t, "uuid", `"123E4567-E89B-12D3-A456-426614174000"`, "lower case")
}

func TestRecordsAndJSON(t *testing.T) {
	typ := "record<candidate:int32,issues:json>"
	a := obs(t, typ, `{"candidate": 0, "issues": [{"code": "x", "meta": {"min": 3, "actual": 2}}]}`)
	b := obs(t, typ, `{"issues": [{"meta": {"actual": 2.0, "min": 3}, "code": "x"}], "candidate": 0}`)
	if !Equal(a, b) {
		t.Error("records differ")
	}
	invalid(t, typ, `{"candidate": 0}`, "has the fields")
}
