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
the decoder language produce, with its code and the type of each metadata entry. It also lists
three that no operation of this language produces and raoh-java's message catalogue has:
`missing_field`, `out_of_range` and `type_mismatch.string_keys`. A variant may
have type parameters: `out_of_range.minimum` has `min` and `actual` of type `T`, and `T` is the
type of the value being checked (`int32` for `int().min(1)`, `float64` for `double().min(0.5)`).
`catalog/operations.json` says, for each operation, which variants it produces, what their type
parameters are, and which metadata values it always gives (the `expected` of the type mismatch
`int` gives is always `"integer"`).

A variant's `meta` lists every entry an issue of it can have. Two things leave an entry out, and
they mean different things. An `omit` on an issue in `catalog/operations.json` names entries that
the issue never has where that form gives it: `toInt` gives `type_mismatch` without `actual`,
since the input was a string, the kind it expects, and only the text in it failed to read as an
integer, and `discriminate` gives `not_allowed` without `actual`, while the value `oneOf` always
gives it. A variant's `optional_meta` lists entries that an issue may or may not have at a site
that neither omits the entry nor gives it a source, depending on what the decoder ran into. Every
other entry is always present.

Both are checked. The catalogue is rejected when an entry in `optional_meta` is omitted or given a
source by every form that gives the variant, and `raoh-verify check-suite` fails when no case
leaves such an entry out, as it fails for a feature no case needs: without that case nothing shows
that the entry can be absent. A variant no form gives, such as `out_of_range`, which only the
message catalogue has, is not checked. A template writes only entries its variant always has, so
an entry in `optional_meta` cannot be written into a message, and the catalogue is rejected when a
form omits an entry the template of its issue writes: that issue's message could not be derived
there.

An issue's metadata has exactly these entries, and each is compared as a value of its instantiated
type.

### What `actual` of `type_mismatch` says

The `actual` of `type_mismatch` names the kind of input the decoder found where it expected
another: `null`, `boolean`, `number`, `string`, `array` or `object` for a value of the
[input model](input-model.md), and `missing` for an absent value, such as the member of an object
that is itself absent. Its type in `catalog/issues.json` is the symbol type of these seven words,
so a case cannot write another. `missing` is not a kind of value; it says what the decoder
observed, which is that there was no value.

An implementation that decodes values of its host language directly, without first mapping them
onto the input model (a Java `LocalDate` in a `Map`, for instance), reports what it finds there in
its own words. That is outside this specification, which says what `actual` is for input-model
values only.

The form that gives an issue decides some of its metadata, and `catalog/operations.json` says
where each such value comes from: a constant (`{"const": "integer"}`), a constant for each type the
entry can have (`{"const_by_type": ...}`: `positive` gives `min` 1 for an integer and 0 otherwise),
an argument (`{"arg": "min"}`), an argument sorted in ascending order (`{"sorted": "allowed"}`:
strings by code point, other values as the bounding operations order them), an argument with A-Z
read as a-z and then sorted (`{"ascii_lower_sorted": "symbols"}`), the sorted names of a
`variants` argument (`{"sorted_keys": "variants"}`), or the name of the member the issue is at
(`{"member": true}`). A case's value for such an entry must be that value. A source has to fit its
entry for every receiver an operation applies to, and this is checked when the catalogue is read:
a `const_by_type` gives a value for exactly the types the entry can have, and where the case
decides the entry's type (`contains` on `list<E>`), only an argument of that same type can give
it. An entry with no source,
such as the `actual` of a failed bound, is known only when a decoder runs; see
[conformance.md](conformance.md#what-the-verifier-checks).

## Messages

The message of an issue is either derived or given.

A derived message is the English template of the issue's message key, from
`catalog/messages/en.properties`, with every placeholder `{name}` replaced by the message form
(below) of the metadata entry `name`. A placeholder with no entry of that name stays as it is
written. If the catalogue has no template for the message key, the template of the code is used.

A given message is one the user of the library supplied, such as the `"bad"` of
`string().toInt("bad")`, or the message of an issue a fixture creates. A message argument gives the
message of every issue its form declares, `toInt`'s `type_mismatch` and `type_mismatch.numeric_range`
alike, and of no other: the issues of the form's decoder, field and fixture arguments keep their
own messages, as `literal`'s string decoder's `type_mismatch` does when `literal` is given one. It is the message, whatever
the catalogue says, and resolving messages leaves it alone. A fixture that stands for user code
creates its issue with a given message, with whatever the implementation offers for that (in
raoh-java 0.8.0, `refine` or `Result.failCustom`; an issue made with `Result.fail` has its message
replaced when it is resolved).

A case writes an issue without `message` when the message is derived, and with it, exactly as it is
given, when the message is given. `raoh-verify check-suite` rejects a case that writes a derived
message, or that leaves out or changes a given one. An issue that two parts of a decoder could give
with different types or messages is ambiguous, and a case that expects one is rejected too. A runner always writes the message its implementation gives for an issue once the implementation
has resolved its messages with its default English catalogue (in raoh-java 0.8.0,
`Issues.resolve(MessageResolver.DEFAULT)`); the issues a `one_of_failed` lists are written the same
way. The verifier derives the message the case leaves out and compares it with what the runner
wrote.

### Message forms

A metadata value appears in a message in its message form:

| Type | Message form |
|------|--------------|
| `bool` | `true` or `false` |
| `int32`, `int64` | the integer in decimal |
| `float32`, `float64` | the canonical decimal of the float ([observation.md](observation.md#floats)), written plainly with at least one digit after the point when the exponent of its first digit is from -3 to 6 (`0.5`, `100.0`, `0.001`), and as a mantissa with at least one digit after the point, `E` and that exponent otherwise (`1.0E7`, `1.0E-4`, `4.9E-324`); zeros are `0.0` and `-0.0`, the others `NaN`, `Infinity` and `-Infinity` |
| `decimal` | the coefficient with the point placed by the scale when the scale is not negative and the adjusted exponent (the exponent of the first digit) is at least -6 (`0.00010`, `10`); otherwise the first digit, then a point and the other digits when there are any, `E`, a sign and the adjusted exponent (`1E+3`, `1.5E-7`) |
| `string`, `symbol`, `uuid`, `uri` | the text |
| `date` | the year as its observation writes it ([observation.md](observation.md): four digits from 0000 to 9999, otherwise a sign and its digits), `-`, two digits of month, `-`, two digits of day |
| `time` | `hh:mm`, followed by `:ss` when the seconds or the fraction are not zero, followed by the fraction in three, six or nine digits when it is not zero |
| `datetime` | the date, `T`, the time |
| `offset_datetime` | the date-time, then `Z` for a zero offset, otherwise `±hh:mm`, followed by `:ss` when the offset's seconds are not zero |
| `instant` | the date, `T`, the time with the seconds always written, `Z` |
| `list<T>` | `[`, the message forms of the elements separated by `, `, `]` |

Other types have no message form. A message writes only metadata whose type has one: an issue
whose template writes an entry of another type is not given, and a form that would give it does not
type-check ([decoder-language.md](decoder-language.md#types)).


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
| `{"unknown_members": {"after": x, "known": ..., "issue": k}}` | a list of `x`, then issue `k` for every member of an object input not among the known names (a list argument, or the members the fields of a fields argument read) and not already reported unknown by that list, in any order, at its path |
| `{"candidates": {"decoders": a, "issue": k, "meta": m}}` | none, when some decoder of argument `a` can give none; or issue `k`, listing in its metadata entry `m` for every decoder of `a` a non-empty list it gives |
| `{"fixture": a}` | none, or the issue the fixture argument `a` declares |
| `{"form": [...]}` | the lists of the decoder form written there, such as the `["string"]` `discriminate` reads its tag with; that form is part of the meaning of the one that embeds it, and not a feature a case needs |
| `{"discard": [a, ...]}` | none: the issues of the arguments listed, decoders or fixtures that give issues, never reach the caller |

A decoder's operations run in order, each only if what came before succeeded: the flow of a form
followed by operations is the chain of the form's flow and each operation's.

There is no expression for a choice. What excludes each other in Raoh is a form's own issues
(`own`) and the variants of a variants argument, and only those: a form never skips a decoder it
runs, so an issue certain below it cannot be avoided by the form choosing another branch. Where a
form checks something before running its decoders (`list` checks for an array, a field for an
object, `discriminate` for its tag), the flow is a `chain`.

The flow says which issues can come together and in what order; it does not say which of a form's
own issues arise for an input, and a form may always succeed. `unknown_members` is the one part
that does not leave that open: for a given list of its `after`, the input determines its issues.
Every member that is not known and that the list of `after` has not reported unknown is reported,
so a case must list exactly those members, and cannot expect success when there is one. The issues
of one `unknown_members` form one group, and two `unknown_members` are two groups, even on the
same object.

The list of `after` has reported a member unknown when it holds, at that member's path, issue `k`
from an `unknown_members`. Such a member is not reported again: `strict` inside `strict` reports a
member once, by the innermost form that does not know it, and accepts only members both know. This
is a rule of `unknown_members`, not of issues in general: an issue of another variant at the
member's path, a `type_mismatch` say, does not count, and both are reported.

For a case, the verifier parses the expected issues with the flow for the case's input: each issue
takes its place in the flow, where it is typed and its metadata and message are settled. A case
whose issues the flow does not give, or give in two ways that type, word or group an issue
differently, is rejected; so is a case that expects success where the flow cannot give the empty
list. That reading is the only one the verifier makes of an outcome: an implementation's issues
are matched against it, each read at the place of the expected issue it is matched with, in order,
except that the consecutive issues of one `unknown_members` group are matched in any order. A
divergence's issues are read as a case's are, with the messages they write.

The metadata entry that lists the candidates is the one the flow names: its type is
`list<record<candidate:int32,issues:issues>>`, the form gives it no source, and the variant cannot
leave it out. `one_of_failed` lists them in `candidates`, and is given only when every candidate
failed; it lists every candidate exactly once, by index; the order of the list does not matter. Each candidate's issues are parsed with
that candidate's flow on the same input. As raoh-java writes them, they have a path, a code, a
message and metadata, and no message key. Their message is the one their place gives, as for any
other issue; a case may write it, and then it has to be that message.

## Issues from fixtures

A fixture that fails, such as a `refine` whose predicate does not hold, creates its issue itself,
as the user code it stands for would. `catalog/fixtures.json` declares the issue each fixture
creates: its code, message key, metadata types and message. Such an issue is not one of the
variants of `catalog/issues.json`, even when it has the same code.
