package value

import (
	"math"
	"math/big"

	"github.com/raoh-project/raoh-specification/jsontext"
)

// Equal reports whether two values of the same type are the same value. See spec/value-model.md.
func Equal(a, b Value) bool {
	if !a.Type.Same(b.Type) {
		return false
	}
	switch a.Type.Kind {
	case Bool:
		return a.Bool == b.Bool
	case Int32, Int64:
		return a.Int.Cmp(b.Int) == 0
	case Float32, Float64:
		// As Double.equals: every NaN is the same, and +0 and -0 differ.
		if math.IsNaN(a.Float) || math.IsNaN(b.Float) {
			return math.IsNaN(a.Float) && math.IsNaN(b.Float)
		}
		return math.Float64bits(a.Float) == math.Float64bits(b.Float)
	case Decimal:
		return a.Dec.Scale == b.Dec.Scale && a.Dec.Unscaled.Cmp(b.Dec.Unscaled) == 0
	case String, Symbol, UUID, URI:
		return a.Str == b.Str
	case Instant:
		return a.Time.Epoch == b.Time.Epoch && a.Time.Nano == b.Time.Nano
	case Date, Time, DateTime, OffsetDateTime:
		return a.Time == b.Time
	case List, Product, Record:
		return equalSeq(a.Elems, b.Elems)
	case Set:
		return equalBag(a.Elems, b.Elems)
	case Map:
		if len(a.Keys) != len(b.Keys) {
			return false
		}
		for i, k := range a.Keys {
			j := indexOf(b.Keys, k)
			if j < 0 || !Equal(a.Elems[i], b.Elems[j]) {
				return false
			}
		}
		return true
	case Presence, Optional, Nullable:
		return a.State == b.State && equalSeq(a.Elems, b.Elems)
	case JSON:
		return EqualJSON(a.Node, b.Node)
	}
	return false
}

func indexOf(keys []string, k string) int {
	for i, key := range keys {
		if key == k {
			return i
		}
	}
	return -1
}

func equalSeq(a, b []Value) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

// equalBag reports whether a and b hold the same values the same number of times, in any order.
func equalBag(a, b []Value) bool {
	if len(a) != len(b) {
		return false
	}
	used := make([]bool, len(b))
next:
	for _, x := range a {
		for j, y := range b {
			if !used[j] && Equal(x, y) {
				used[j] = true
				continue next
			}
		}
		return false
	}
	return true
}

// EqualJSON reports whether two JSON values are the same: numbers by their value, objects by their
// members in any order.
func EqualJSON(a, b *jsontext.Node) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case jsontext.Null:
		return true
	case jsontext.Bool:
		return a.Bool == b.Bool
	case jsontext.String:
		return a.Text == b.Text
	case jsontext.Number:
		x, _ := new(big.Rat).SetString(a.Text)
		y, _ := new(big.Rat).SetString(b.Text)
		return x.Cmp(y) == 0
	case jsontext.Array:
		if len(a.Elems) != len(b.Elems) {
			return false
		}
		for i := range a.Elems {
			if !EqualJSON(a.Elems[i], b.Elems[i]) {
				return false
			}
		}
		return true
	case jsontext.Object:
		if len(a.Members) != len(b.Members) {
			return false
		}
		for _, m := range a.Members {
			o, ok := b.Get(m.Name)
			if !ok || !EqualJSON(m.Value, o) {
				return false
			}
		}
		return true
	}
	return false
}
