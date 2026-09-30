# Value model

The value model says which values are the same across implementations. A decoder's result, an
encoder's input and an issue's metadata are values of the types below; an implementation represents
them with whatever its language offers, and [observation.md](observation.md) says how each is
written down so that the verifier can compare them.

Types are written as in `catalog/operations.json` and `catalog/issues.json`: a name, followed by
type arguments in angle brackets. A single upper-case letter is a type parameter.

## Scalars

| Type | Values | Same when |
|------|--------|-----------|
| `bool` | true, false | equal |
| `int32` | integers from -2³¹ to 2³¹-1 | equal |
| `int64` | integers from -2⁶³ to 2⁶³-1 | equal |
| `float32` | IEEE 754 binary32 values: the finite ones, +0, -0, +∞, -∞ and NaN | see below |
| `float64` | IEEE 754 binary64 values, likewise | see below |
| `decimal` | a coefficient (an integer) and a scale (an integer): coefficient × 10^-scale | coefficient and scale both equal |
| `string` | sequences of Unicode scalar values | equal |
| `symbol` | the name of one of a fixed set of alternatives, as the decoder that produced it lists them | equal |
| `uuid` | 128-bit UUIDs | equal |
| `uri` | URI references | equal as written |

Two floats are the same when they are the same IEEE 754 value, with two exceptions to what the
IEEE 754 comparison says: +0 and -0 are different values, and every NaN is the same value as every
other NaN. This is how `Double.equals` compares, and how raoh-java 0.8.0 compares values against the
bounds of `min`, `oneOf` and the like.

A decimal keeps its scale: 1.5 and 1.50 are different decimals. An operation compares decimals by
value only where it says so.

## Temporal values

| Type | Values | Same when |
|------|--------|-----------|
| `date` | a year, month and day of the proleptic Gregorian calendar | all three equal |
| `time` | a time of day to the nanosecond | equal |
| `datetime` | a date and a time of day, with no offset | both equal |
| `offset_datetime` | a date, a time of day and an offset from UTC | all three equal |
| `instant` | a point on the UTC time-line to the nanosecond | equal |

Two `offset_datetime` values at different offsets are different values even when they denote the
same instant.

## Structures

| Type | Values | Same when |
|------|--------|-----------|
| `list<T>` | finite sequences of `T` | same length, and the same element at each position |
| `set<T>` | finite sets of `T` | the same elements, in any order |
| `map<T>` | finite maps from strings to `T` | the same keys, each with the same value, in any order |
| `product<T1,...,Tn>` | n-tuples | the same element at each position |
| `presence<T>` | absent, null, or present with a `T` | the same case, and the same value when present |
| `optional<T>` | empty, or a `T` | both empty, or the same value |
| `nullable<T>` | null, or a `T` | both null, or the same value |

A product is what an object decoder produces from its fields before any `map`: the value of each
field, in the order the fields are declared. Implementations represent it however they like, as a
tuple, a record, an array or a list; the specification needs only its elements.

`optional<T>` is what an optional field produces when the field is absent. `nullable<T>` is what a
nullable decoder produces when the input is null. The two are observed the same way and are
different types.

## Types for metadata and fixtures

| Type | Values | Same when |
|------|--------|-----------|
| `record<f1:T1,...,fn:Tn>` | a value of each named field | the same value for each field |
| `json` | any JSON value | the same kind; numbers with the same value, strings, booleans and arrays equal element by element, objects with the same members in any order |
| `issues` | a non-empty list of issues, as a failed decoder gives them | not observed |

`record` and `json` appear only in issue metadata, where a variant carries structured data (the
candidates `one_of_failed` lists, for instance). `issues` appears only as the input of a fixture
that recovers from a failure; it is never written down.
