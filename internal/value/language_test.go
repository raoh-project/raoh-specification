package value

import (
	"testing"

	"github.com/raoh-project/raoh-specification/internal/jsontext"
)

// The observation language of each type is what spec/observation.md lists, and nothing that
// merely denotes the same value.
func TestOnlyTheListedObservationsAreAccepted(t *testing.T) {
	for _, c := range []struct {
		typ, text string
		ok        bool
	}{
		{"int32", "0", true},
		{"int32", "-0", false},
		{"int32", "-1", true},

		{"decimal", `"0.00"`, true},
		{"decimal", `"-0.00"`, false},
		{"decimal", `"-0"`, false},
		{"decimal", `"1E+3"`, true},

		{"date", `"2024-01-01"`, true},
		{"date", `"0000-01-01"`, true},
		{"date", `"-0001-01-01"`, true},
		{"date", `"+10000-01-01"`, true},
		{"date", `"-10000-01-01"`, true},
		{"date", `"10000-01-01"`, false},
		{"date", `"+2024-01-01"`, false},
		{"date", `"02024-01-01"`, false},
		{"date", `"+010000-01-01"`, false},
		{"date", `"-001-01-01"`, false},

		{"time", `"09:00"`, true},
		{"time", `"09:00:00"`, true},
		{"time", `"09:00:00.5"`, true},
		{"time", `"09:00:00.123456789"`, true},
		{"time", `"9:00"`, false},
		{"time", `"09:00:00."`, false},

		{"offset_datetime", `"2024-01-01T00:00Z"`, true},
		{"offset_datetime", `"2024-01-01T00:00+09:00"`, true},
		{"offset_datetime", `"2024-01-01T00:00-09:30"`, true},
		{"offset_datetime", `"2024-01-01T00:00+09:00:30"`, true},
		{"offset_datetime", `"2024-01-01T00:00+18:00"`, true},
		{"offset_datetime", `"2024-01-01T00:00+00:00"`, false},
		{"offset_datetime", `"2024-01-01T00:00-00:00"`, false},
		{"offset_datetime", `"2024-01-01T00:00+09:00:00"`, false},
		{"offset_datetime", `"2024-01-01T00:00+17:60"`, false},
		{"offset_datetime", `"2024-01-01T00:00+09:60"`, false},
		{"offset_datetime", `"2024-01-01T00:00+18:00:01"`, false},
		{"offset_datetime", `"2024-01-01T00:00+19:00"`, false},

		{"instant", `"+10000-01-01T00:00:00Z"`, true},
		{"instant", `"10000-01-01T00:00:00Z"`, false},
	} {
		_, err := Observe(MustParseType(c.typ), jsontext.MustParse(c.text))
		if (err == nil) != c.ok {
			t.Errorf("%s %s: accepted %v, want %v (%v)", c.typ, c.text, err == nil, c.ok, err)
		}
	}
}

// A type whose observation would not tell two of its values apart is not a type.
func TestTypesWhoseObservationIsAmbiguousAreRejected(t *testing.T) {
	for s, ok := range map[string]bool{
		"optional<nullable<int32>>":       false,
		"nullable<optional<int32>>":       false,
		"nullable<nullable<int32>>":       false,
		"nullable<json>":                  false,
		"nullable<list<nullable<int32>>>": true,
		"presence<nullable<int32>>":       true,
		"nullable<presence<int32>>":       true,
		"nullable<T>":                     true,
	} {
		_, err := ParseType(s)
		if (err == nil) != ok {
			t.Errorf("%s: accepted %v, want %v (%v)", s, err == nil, ok, err)
		}
	}
	if err := WellFormed(Type{Kind: Nullable, Args: []Type{{Kind: Optional, Args: []Type{Of(Int32)}}}}); err == nil {
		t.Error("a nullable optional built by substitution is well formed")
	}
}

func TestCompareOrdersAsTheOperationsDo(t *testing.T) {
	for _, c := range []struct {
		typ, a, b string
		want      int
	}{
		{"int64", "-5", "3", -1},
		{"float64", `{"float": "-0"}`, "0", -1},
		{"float64", "0", `{"float": "-0"}`, 1},
		{"float64", `{"float": "NaN"}`, `{"float": "+Infinity"}`, 1},
		{"float32", "0.1", "0.1", 0},
		{"decimal", `"1.50"`, `"1.5"`, 0},
		{"decimal", `"1E+3"`, `"999.999"`, 1},
		{"decimal", `"-2"`, `"-10"`, 1},
		{"date", `"2024-01-01"`, `"2023-12-31"`, 1},
		{"time", `"09:00"`, `"09:00:00.000000001"`, -1},
		{"instant", `"2024-01-01T00:00:00Z"`, `"2024-01-01T00:00:00.5Z"`, -1},
		// OffsetDateTime.compareTo: by instant, then by local date-time.
		{"offset_datetime", `"2024-01-01T09:00Z"`, `"2024-01-01T10:00+01:00"`, -1},
		{"offset_datetime", `"2024-01-01T09:00+09:00"`, `"2024-01-01T01:00Z"`, -1},
	} {
		got, err := Compare(obs(t, c.typ, c.a), obs(t, c.typ, c.b))
		if err != nil {
			t.Fatalf("%s %s %s: %v", c.typ, c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("%s: compare %s %s = %d, want %d", c.typ, c.a, c.b, got, c.want)
		}
	}
	if _, err := Compare(obs(t, "string", `"a"`), obs(t, "string", `"b"`)); err == nil {
		t.Error("strings are ordered")
	}
}
