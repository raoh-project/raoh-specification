# Input model

A decoder reads a value of the input model. The input model is what a JSON text (RFC 8259) denotes,
with one addition: a number keeps its lexeme.

| Kind | Holds |
|------|-------|
| null | nothing |
| boolean | true or false |
| number | its lexeme, such as `-0`, `1.50` or `1e400` |
| string | a sequence of Unicode scalar values |
| array | a sequence of values |
| object | a sequence of members, each a name (a string) and a value, with no name twice |

A decoder may also be given no value at all: an object field that is not there is absent, and
decoders tell an absent value from null where they say so.

A number is not reduced to a value before a decoder sees it, because decoders tell lexemes apart.
`int` accepts `1` and rejects `1.0`; `double` gives +0 for `-0` and -0 for `-0.0`; `decimal` gives
1.50 with scale 2 for `1.50`. An implementation need not keep the lexeme itself, as long as it can
tell every decoder what the decoder needs from it.

The order of an object's members is part of the value only as far as some decoders report issues in
that order (see [issues.md](issues.md#order)); no decoder's result depends on it.

## What is not in the input model

The input model has no text that is not JSON, no object that repeats a member name, and no string
holding an unpaired surrogate. How an implementation treats such input is outside this
specification, and the suite never gives it.

Values of a host language (a Java `Map`, a PHP array, a Go `map[string]any`) and values a library
gives (a database record, a JSON library's tree) are not in the input model either. An adapter that
maps them onto it, or decodes them directly, is outside this specification.

## In cases

A case writes its input as the JSON value of its `input` member. The verifier reads that value with
its lexemes, member order and member names as the case file gives them, and a runner has to give its
decoder that same value: in practice, the exact text of the `input` member.
