# Observation

An observation is a value of the [value model](value-model.md) written as JSON, so that a runner
can report what a decoder gave and a case can say what it expects. Observations are typed: the
same JSON can be the observation of different values, and which value it is depends on the type
the decoder's result has. The verifier reads every observation against its type and compares the
values, never the JSON text.

Each type has an observation language: the JSON texts this document lists for it, and nothing
else. A text outside it is not an observation, even when it would denote the same value. Most
values have one spelling; where a type allows several (a float written `1` or `1.0`, a time with
or without seconds), this document lists them.

## Scalars

| Type | Observation |
|------|-------------|
| `bool` | `true` or `false` |
| `int32`, `int64` | a JSON number written as an integer, with no fraction, no exponent and no minus sign on zero |
| `float32`, `float64` | a JSON number, or a tag (below) |
| `decimal` | a JSON string holding a decimal number written as a JSON number is, with no minus sign on zero; its scale is the number of digits after the point, less the exponent: `"1.50"` has scale 2, `"1E+3"` and `"1e3"` scale -3 |
| `string`, `symbol`, `uri` | a JSON string |
| `uuid` | a JSON string of 32 lower-case hexadecimal digits grouped 8-4-4-4-12 |

A finite float is written as a JSON number whose value is the value of the float's canonical
decimal (below), the decimal its message form writes too ([issues.md](issues.md#message-forms)). `0.1` is an
observation of the float32 nearest to 0.1, and `1`, `1.0` and `1e0` are all observations of 1.
`16777217` is not an observation of any float32: the float32 it rounds to is 16777216, whose
canonical decimal is `16777216`. `5e-324` is not an observation of the least positive float64,
whose canonical decimal is `4.9e-324`. A number beyond the range of the type is not an observation
of it either.

### Floats

The canonical decimal of a finite non-zero float m is chosen as follows. A decimal is c × 10^q for
integers c and q where c is not a multiple of 10, and its length is the number of digits of c. Let R
be the set of decimals that round to m under IEEE 754 round to nearest, ties to even, at the
float's width, and p the least length of a decimal in R. Let T be the decimals in R of length p
when p ≥ 2, and of length 1 or 2 when p is 1. The canonical decimal is the one in T closest to m,
and of two equally close, the one with the even c.

Allowing length 2 when one digit would do keeps the decimal close to m where every one-digit
decimal is far from it: the least positive float64 is `4.9e-324`, not `5e-324`, and the least
positive float32 `1.4e-45`. A float is ±0, an infinity or NaN otherwise, which the tags below
write.

What a JSON number cannot carry reliably through every JSON library is written as a tag:
`{"float": "-0"}`, `{"float": "NaN"}`, `{"float": "+Infinity"}` or `{"float": "-Infinity"}`. A JSON
number with a minus sign and the value zero is not an observation; negative zero is written only as
the tag.

## Temporal values

Temporal values are written as JSON strings in ISO 8601:

| Type | Observation |
|------|-------------|
| `date` | the year, `-`, two digits of month, `-`, two digits of day. A year from 0 to 9999 is four digits (`0000`, `2024`); a negative year is `-` and its absolute value, with leading zeros up to four digits (`-0001`) and none past them (`-10000`); a year above 9999 is `+` and its digits (`+10000`). No other spelling of a year is an observation. |
| `time` | `hh:mm`, `hh:mm:ss` or `hh:mm:ss.f` with one to nine digits of fraction |
| `datetime` | a date, `T`, and a time |
| `offset_datetime` | a date-time followed by the offset: `Z` for zero, otherwise `+` or `-`, `hh:mm`, and `:ss` only when the seconds are not zero. Hours are at most 18, minutes and seconds at most 59, and the offset at most 18 hours. |
| `instant` | a date-time in UTC followed by `Z` |

The forms of a time of day are alternatives: `09:00` and `09:00:00.000` are the same time.

## Structures

| Type | Observation |
|------|-------------|
| `list<T>` | a JSON array of the observations of the elements, in order |
| `set<T>` | a JSON array of the observations of the elements, in any order, with no element twice |
| `map<T>` | a JSON object whose members are the keys and the observations of their values |
| `product<T1,...,Tn>` | a JSON array of n observations, the i-th of type `Ti` |
| `presence<T>` | `"absent"`, `"null"`, or `{"present": v}` with `v` the observation of the value |
| `optional<T>`, `nullable<T>` | `null`, or the observation of the value |
| `record<...>` | a JSON object with exactly the fields of the type |
| `json` | the value itself |
| `issues` | a JSON array of issues, as [issues.md](issues.md) describes the ones `one_of_failed` lists |

Because an empty optional and a null are observed as `null`, `T` in `optional<T>` and
`nullable<T>` must not be a type that observes anything as `null` (`optional`, `nullable`,
`json`). A type such as `optional<nullable<int32>>` is not well formed: its observation could not
tell an empty optional from one holding null. The type checker rejects every form whose result
would be such a type.

## Outcomes

A case's expected outcome, and a runner's observed outcome, is one of:

```json
{"ok": <observation of the result>}
{"issues": [<issue>, ...]}
```

An issue is written as [issues.md](issues.md) describes.

An encoder's outcome is always `{"ok": <JSON value>}`, and the JSON value is compared as a `json`
observation is.

## Materialisation

Encoding cases give the encoder's input as an observation. A runner materialises it: it builds the
value of its own language that the observation denotes. Every type above except `json` and `issues`
can be materialised. A value no observation can denote, such as a domain object that a decoder's
`map` built, is out of reach of encoding cases.
