package value

import "testing"

func TestFormatForMessage(t *testing.T) {
	for _, c := range []struct{ typ, obs, want string }{
		// Double.toString and Float.toString.
		{"float64", "1e7", "1.0E7"},
		{"float64", "1e-4", "1.0E-4"},
		{"float64", "0.001", "0.001"},
		{"float64", "9999999", "9999999.0"},
		{"float64", "0.5", "0.5"},
		{"float64", "100", "100.0"},
		{"float64", "-1.25e-7", "-1.25E-7"},
		{"float64", "1.7976931348623157e308", "1.7976931348623157E308"},
		{"float64", `{"float": "-0"}`, "-0.0"},
		{"float64", `{"float": "+Infinity"}`, "Infinity"},
		{"float64", `{"float": "NaN"}`, "NaN"},
		{"float32", "0.1", "0.1"},
		{"float32", "16777216", "1.6777216E7"},
		{"float32", "3.4028235e38", "3.4028235E38"},
		// BigDecimal.toString.
		{"decimal", `"0.0005"`, "0.0005"},
		{"decimal", `"0.00010"`, "0.00010"},
		{"decimal", `"10"`, "10"},
		{"decimal", `"1E+3"`, "1E+3"},
		{"decimal", `"0.0000001"`, "1E-7"},
		{"decimal", `"-12.5E-10"`, "-1.25E-9"},
		{"decimal", `"0E-10"`, "0E-10"},
		// LocalDate, LocalTime, LocalDateTime, OffsetDateTime and Instant toString.
		{"time", `"09:00:00"`, "09:00"},
		{"time", `"09:00:01"`, "09:00:01"},
		{"time", `"09:00:00.5"`, "09:00:00.500"},
		{"time", `"09:00:00.0005"`, "09:00:00.000500"},
		{"datetime", `"2024-01-01T00:00:00"`, "2024-01-01T00:00"},
		{"offset_datetime", `"2024-01-01T00:00:00+09:00"`, "2024-01-01T00:00+09:00"},
		{"offset_datetime", `"2024-01-01T00:00:00+00:00"`, "2024-01-01T00:00Z"},
		{"instant", `"2024-01-01T00:00Z"`, "2024-01-01T00:00:00Z"},
		{"date", `"2024-12-31"`, "2024-12-31"},
		{"date", `"10000-01-01"`, "+10000-01-01"},
		// AbstractCollection.toString.
		{"list<int32>", "[1, 3, 3]", "[1, 3, 3]"},
		{"list<string>", `["rect", "square"]`, "[rect, square]"},
		{"list<int32>", "[]", "[]"},
		{"bool", "true", "true"},
	} {
		got, err := FormatForMessage(obs(t, c.typ, c.obs))
		if err != nil {
			t.Errorf("%s %s: %v", c.typ, c.obs, err)
		} else if got != c.want {
			t.Errorf("%s %s: %q, want %q", c.typ, c.obs, got, c.want)
		}
	}
	if _, err := FormatForMessage(obs(t, "map<int32>", `{"a": 1}`)); err == nil {
		t.Error("a map has a message form")
	}
}
