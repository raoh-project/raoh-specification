# Issues

A decoder that fails gives one or more issues. An issue has five parts:

| Part | Meaning |
|------|---------|
| `path` | Where in the input the problem is, as [path.md](path.md) describes |
| `code` | The class of problem, such as `out_of_range` |
| `message_key` | The kind of problem within its class, such as `out_of_range.minimum`; it starts with the code |
| `meta` | Named values that describe the problem, such as the bound that was not met |
| `message` | A human-readable description |

The message key identifies the variant. `catalog/issues.json` lists every variant the operations of
the decoder language produce, with its code and the type of each metadata entry. A variant may
have type parameters: `out_of_range.minimum` has `min` and `actual` of type `T`, and `T` is the
type of the value being checked (`int32` for `int().min(1)`, `float64` for `double().min(0.5)`).
`catalog/operations.json` says, for each operation, which variants it produces and what their type
parameters are. Some variants leave some metadata out in some contexts; `optional_meta` lists those
entries. Every other entry is always present.

An issue's metadata has exactly the entries of its variant, and each is compared as a value of its
instantiated type.

## Messages

The message of an issue is either derived or given.

A derived message is the English template of the issue's message key, from
`catalog/messages/en.properties`, with every placeholder `{name}` replaced by the message form
(below) of the metadata entry `name`. A placeholder with no entry of that name stays as it is
written. If the catalogue has no template for the message key, the template of the code is used.

A given message is one the user of the library supplied, such as the `"bad"` of
`string().toInt("bad")`, or the message of an issue a fixture creates. It is the message, whatever
the catalogue says. An implementation's message resolver, applied later by its user, leaves a given
message alone.

A case writes an issue without `message` when the message is derived, and with it when the message
is given. A runner always writes the message its implementation gave. The verifier derives the
message the case leaves out and compares it with what the runner wrote.

### Message forms

A metadata value appears in a message in its message form. Version 0.8.0 takes these forms from what
raoh-java 0.8.0 writes, which is `String.valueOf` of the value:

| Type | Message form |
|------|--------------|
| `bool` | `true` or `false` |
| `int32`, `int64` | the integer in decimal |
| `float32`, `float64` | the shortest decimal that reads back as the float, written plainly with at least one digit after the point when 10⁻³ ≤ \|v\| < 10⁷ (`0.5`, `100.0`), and as a mantissa with at least one digit after the point, `E` and the exponent otherwise (`1.0E7`, `1.0E-4`); zeros are `0.0` and `-0.0`, the others `NaN`, `Infinity` and `-Infinity` |
| `decimal` | the coefficient with the point placed by the scale when the scale is not negative and the adjusted exponent (the exponent of the first digit) is at least -6 (`0.00010`, `10`); otherwise the first digit, the rest after a point, `E`, a sign and the adjusted exponent (`1E+3`, `1E-7`) |
| `string`, `symbol`, `uuid`, `uri` | the text |
| `date` | `yyyy-mm-dd` |
| `time` | `hh:mm`, followed by `:ss` when the seconds or the fraction are not zero, followed by the fraction in three, six or nine digits when it is not zero |
| `datetime` | the date, `T`, the time |
| `offset_datetime` | the date-time, then `Z` for a zero offset or `±hh:mm` |
| `instant` | the date, `T`, the time with the seconds always written, `Z` |
| `list<T>` | `[`, the message forms of the elements separated by `, `, `]` |

Other types have no message form, and no template refers to metadata of those types.

Whether the float and decimal forms should stay as raoh-java writes them is an open question for a
later version.

## Order

Issues are compared in the order the decoder gives them, unless the decoder contains a form whose
issues come in the order of the input's members (the unknown fields `strictObject` and `strict`
report). `catalog/operations.json` marks such forms with `"issue_order": "input"`. The issues of a
decoder containing one are compared as a multiset: the same issues, each the same number of times,
in any order.

## Issues from fixtures

A fixture that fails, such as a `refine` whose predicate does not hold, creates its issue itself,
as the user code it stands for would. `catalog/fixtures.json` declares the issue each fixture
creates: its code, message key, metadata types and message. Such an issue is not one of the
variants of `catalog/issues.json`, even when it has the same code.
