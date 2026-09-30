package value

import (
	"math/big"

	"github.com/raoh-project/raoh-specification/internal/jsontext"
)

// State says which case of a Presence, Optional or Nullable a value is.
type State int

// The cases of Presence, Optional and Nullable. Optional and Nullable have no Absent.
const (
	Present State = iota
	Null
	Absent
)

// Dec is a decimal number: Unscaled × 10^-Scale.
type Dec struct {
	Unscaled *big.Int
	Scale    int32
}

// Temporal holds the fields of a temporal value. Which fields mean something depends on the type:
// a Date has Year, Month and Day; a Time has Hour, Minute, Second and Nano; a DateTime has both;
// an OffsetDateTime adds Offset, in seconds east of UTC, and Epoch, its local date-time read as
// UTC; an Instant is Epoch seconds and Nano.
type Temporal struct {
	Year, Month, Day           int
	Hour, Minute, Second, Nano int
	Offset                     int
	Epoch                      int64
}

// Value is a value of the value model.
type Value struct {
	Type Type
	// Bool is the value of a Bool.
	Bool bool
	// Int is the value of an Int32 or Int64.
	Int *big.Int
	// Float is the value of a Float64, or of a Float32 widened to float64.
	Float float64
	// Dec is the value of a Decimal.
	Dec Dec
	// Str is the value of a String, Symbol, UUID or URI.
	Str string
	// Time is the value of a temporal type.
	Time Temporal
	// Elems are the elements of a List, Set or Product, the values of a Map, the fields of a
	// Record, and the one value of a Present Presence, Optional or Nullable.
	Elems []Value
	// Keys are the keys of a Map, in the order of Elems.
	Keys []string
	// State is the case of a Presence, Optional or Nullable.
	State State
	// Node is the value of a JSON.
	Node *jsontext.Node
}
