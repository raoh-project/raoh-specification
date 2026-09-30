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
`catalog/operations.json` says, for each operation, which variants it produces, what their type
parameters are, and which metadata values it always gives (the `expected` of the type mismatch
`int` gives is always `"integer"`). Some variants leave some metadata out in some contexts; `optional_meta` lists those
entries. Every other entry is always present.

An issue's metadata has exactly the entries of its variant, and each is compared as a value of its
instantiated type.

The form that gives an issue decides some of its metadata, and `catalog/operations.json` says
where each such value comes from: a constant (`{"const": "integer"}`), a constant for each type the
entry can have (`{"const_by_type": ...}`: `positive` gives `min` 1 for an integer and 0 otherwise),
an argument (`{"arg": "min"}`), an argument sorted in ascending order (`{"sorted": "allowed"}`:
strings by code point, other values as the bounding operations order them), an argument with A-Z
read as a-z and then sorted (`{"ascii_lower_sorted": "symbols"}`), the sorted names of a
`variants` argument (`{"sorted_keys": "variants"}`), or the name of the member the issue is at
(`{"member": true}`). A case's value for such an entry must be that value. An entry with no source,
such as the `actual` of a failed bound, is known only when a decoder runs; see
[conformance.md](conformance.md#what-the-verifier-checks).

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

A case writes an issue without `message` when the message is derived, and with it, exactly as it is
given, when the message is given. `raoh-verify check-suite` rejects a case that writes a derived
message, or that leaves out or changes a given one. An issue that two parts of a decoder could give
with different types or messages is ambiguous, and a case that expects one is rejected too. A runner always writes the message its implementation gave. The verifier derives the
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

## Where issues arise, and in what order

Every form of `catalog/operations.json` declares in its `flow` how it gives its issues. A flow is
an expression over the lists of issues a decoder can give; the empty list is success.

| Flow | Issue lists |
|------|-------------|
| `"own"` | none, or one of the form's own issues (those no other part of the flow names), where the form runs |
| `"none"` | none |
| `{"arg": a}` | the lists of decoder argument `a`: of one of its variants, for a variants argument; of its fields one after the other, for a fields argument |
| `{"cat": [x, y, ...]}` | a list of `x`, then a list of `y`, ... |
| `{"chain": [x, y, ...]}` | a non-empty list of `x`; or, when `x` gives none, a list of `{"chain": [y, ...]}` |
| `{"each_element": x}`, `{"each_member": x}` | a list of `x` for each element of an array input, or each member of an object input, in order, at its path |
| `{"at": {"member": a, "flow": x}}` | a list of `x` at the member the string argument `a` names |
| `{"unknown_members": {"known": ..., "issue": k}}` | issue `k` for every member of an object input not among the known names (a list argument, or the members the fields of a fields argument read), in any order, at its path |
| `{"candidates": {"decoders": a, "issue": k}}` | none, when some decoder of argument `a` can give none; or issue `k`, listing for every decoder of `a` a non-empty list it gives |
| `{"fixture": a}` | none, or the issue the fixture argument `a` declares |
| `{"discard": [a, ...]}` | none: the issues of the arguments listed never reach the caller |

A decoder's operations run in order, each only if what came before succeeded: the flow of a form
followed by operations is the chain of the form's flow and each operation's.

There is no expression for a choice. What excludes each other in Raoh is a form's own issues
(`own`) and the variants of a variants argument, and only those: a form never skips a decoder it
runs, so an issue certain below it cannot be avoided by the form choosing another branch. Where a
form checks something before running its decoders (`list` checks for an array, a field for an
object, `discriminate` for its tag), the flow is a `chain`.

The flow says which issues can come together and in what order; it does not say which of a form's
own issues arise for an input, and a form may always succeed. `unknown_members` is the one part
that follows from the input alone: every member the form does not know is reported, so a case must
list them all, and cannot expect success when there is one. Two `unknown_members` are two groups,
even on the same object.

For a case, the verifier parses the expected issues with the flow for the case's input: each issue
takes its place in the flow, where it is typed and its metadata and message are settled. A case
whose issues the flow does not give, or give in two ways that type, word or group an issue
differently, is rejected; so is a case that expects success where the flow cannot give the empty
list. That reading is the only one the verifier makes of an outcome: an implementation's issues
are matched against it, each read at the place of the expected issue it is matched with, in order,
except that the consecutive issues of one `unknown_members` group are matched in any order. A
divergence's issues are read as a case's are, with the messages they write.

`one_of_failed` is given only when every candidate failed, and it lists every candidate exactly
once, by index; the order of the list does not matter. Each candidate's issues are parsed with
that candidate's flow on the same input. As raoh-java writes them, they have a path, a code, a
message and metadata, and no message key. Their message is the one their place gives, as for any
other issue; a case may write it, and then it has to be that message.

## Issues from fixtures

A fixture that fails, such as a `refine` whose predicate does not hold, creates its issue itself,
as the user code it stands for would. `catalog/fixtures.json` declares the issue each fixture
creates: its code, message key, metadata types and message. Such an issue is not one of the
variants of `catalog/issues.json`, even when it has the same code.
