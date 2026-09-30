# Observation

An observation is a value of the [value model](value-model.md) written as JSON, so that a runner
can report what a decoder gave and a case can say what it expects. Observations are typed: the
same JSON can be the observation of different values, and which value it is depends on the type
the decoder's result has. The verifier reads every observation against its type and compares the
values, never the JSON text.

## Scalars

| Type | Observation |
|------|-------------|
| `bool` | `true` or `false` |
| `int32`, `int64` | a JSON number written as an integer, with no fraction and no exponent |
| `float32`, `float64` | a JSON number, or a tag (below) |
| `decimal` | a JSON string holding a decimal number written as a JSON number is; its scale is the number of digits after the point, less the exponent: `"1.50"` has scale 2, `"1E+3"` scale -3 |
| `string`, `symbol`, `uri` | a JSON string |
| `uuid` | a JSON string of 32 lower-case hexadecimal digits grouped 8-4-4-4-12 |

A finite float is written as a JSON number whose value is the value of the shortest decimal that
reads back as the float. `0.1` is an observation of the float32 nearest to 0.1, and `1`, `1.0` and
`1e0` are all observations of 1. `16777217` is not an observation of any float32: the float32 it
rounds to is 16777216, whose shortest decimal is `16777216`. A number beyond the range of the type
is not an observation of it either.

What a JSON number cannot carry reliably through every JSON library is written as a tag:
`{"float": "-0"}`, `{"float": "NaN"}`, `{"float": "+Infinity"}` or `{"float": "-Infinity"}`. A JSON
number with a minus sign and the value zero is not an observation; negative zero is written only as
the tag.

## Temporal values

Temporal values are written as JSON strings in ISO 8601:

| Type | Observation |
|------|-------------|
| `date` | `yyyy-mm-dd` |
| `time` | `hh:mm`, `hh:mm:ss` or `hh:mm:ss.f` with one to nine digits of fraction |
| `datetime` | a date, `T`, and a time |
| `offset_datetime` | a date-time followed by `Z` or an offset `±hh:mm` (or `±hh:mm:ss`) |
| `instant` | a date-time in UTC followed by `Z` |

Any of the forms a type allows denotes the same value when the fields are the same: `09:00` and
`09:00:00.000` are the same time.

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
`map` built, is out of reach of encoding cases; a case that needs one uses a constructor fixture
from `catalog/fixtures.json`.
