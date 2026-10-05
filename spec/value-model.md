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
| `decimal` | a coefficient (an integer) and a scale (an integer from -2147483647 to 2147483647): coefficient × 10^-scale | coefficient and scale both equal |
| `string` | sequences of Unicode scalar values | equal |
| `symbol<"A","B",...>` | one of the listed alternatives; the alternatives are part of the type, so `symbol<"RED","GREEN">` and `symbol<"YES","NO">` are different types, and a symbol type always lists at least one. `enum` gives the symbol type of the names it lists. | equal |
| `uuid` | 128-bit UUIDs | equal |
| `uri` | RFC 3986 URIs | equal as written |

The domain of `uri` is the `URI` production of RFC 3986 section 3, not `URI-reference`: every value
has a scheme, and a relative reference is not a `uri`. It is the set the `uri` operation accepts.
RFC 3986 is read as updated (RFC 7320 and RFC 8820, which do not change the syntax), and without
the IPv6 zone identifier RFC 6874 added, since RFC 9844 removed it. An observation of a `uri` is a
JSON string, and the verifier checks that it is in the domain.

Two floats are the same when they are the same IEEE 754 value, with two exceptions to what the
IEEE 754 comparison says: +0 and -0 are different values, and every NaN is the same value as every
other NaN. A float type therefore has one NaN and two zeros.

The operations that bound or order floats (`min`, `max`, `range`, `positive`, `negative`,
`nonNegative`, `nonPositive`, and sorting the `allowed` of `oneOf`) use the float order, a total
order on these values, from least to greatest: -∞, the negative finite values in increasing
numerical order, -0, +0, the positive finite values in increasing numerical order, +∞, and last
NaN (-∞ < … < -2 < -1 < -0 < +0 < 1 < 2 < … < +∞ < NaN). So -0 is less than +0, `negative` accepts -0 and
`nonNegative` rejects it, and every value is comparable with every other, NaN included. The order
is total because each of these operations needs one answer for every value; it is not IEEE 754's
comparison, in which -0 equals +0 and NaN is unordered.

A decimal keeps its scale: 1.5 and 1.50 are different decimals. An operation compares decimals by
value only where it says so.

## Temporal values

| Type | Values | Same when |
|------|--------|-----------|
| `date` | a year, month and day of the proleptic Gregorian calendar | all three equal |
| `time` | a time of day to the nanosecond | equal |
| `datetime` | a date and a time of day, with no offset | both equal |
| `offset_datetime` | a date, a time of day and an offset from UTC | all three equal |
| `instant` | a point on the UTC time-line to the nanosecond, from -1000000000-01-01T00:00:00Z to +1000000000-12-31T23:59:59.999999999Z | equal |

Two `offset_datetime` values at different offsets are different values even when they denote the
same instant. The operations that compare temporal values (`before`, `after`, `between`) compare
offset date-times chronologically, by the instant alone. That comparison is not an order on the
values, since two different values can compare equal: `09:00Z` and `10:00+01:00` are different
values and neither is before the other. Sameness and chronology are two relations, as sameness and
numeric comparison are for decimals of different scales.

The offset is a number of seconds. `Z`, `+00:00` and `-00:00` all give the offset zero. RFC 9557,
which updates RFC 3339, gives `Z` and `-00:00` one meaning, that the time in UTC is known and the
local offset is not, and `+00:00` another, that UTC is the preferred reference point; an
`offset_datetime` does not represent that distinction.

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

`optional<T>` is the type of an optional field's value: empty when the field is absent, the decoded
value otherwise. `nullable<T>` is the type of a nullable decoder's result: null when the input is
null, the inner decoder's value otherwise. The two are observed the same way and are
different types.

## Types for metadata and fixtures

| Type | Values | Same when |
|------|--------|-----------|
| `record<f1:T1,...,fn:Tn>` | a value of each named field | the same value for each field |
| `json` | any JSON value | the same kind; numbers with the same value, strings, booleans and arrays equal element by element, objects with the same members in any order |
| `issues` | a non-empty list of issues, as a failed decoder gives them | issue by issue, as [issues.md](issues.md) compares them |

`record` appears only in issue metadata, where a variant carries structured data. `issues` appears
in the candidates `one_of_failed` lists, where each candidate's issues are typed by that
candidate's decoder, and as the input of a fixture that recovers from a failure. `json` is the
output of an encoder.
