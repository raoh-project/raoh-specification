# Decoder language

A case names the decoder it exercises with a form written in JSON. The form is not a Raoh API: each
implementation has its own names and its own way of combining decoders, and its runner translates
forms into them. The forms are specified here and in `catalog/operations.json`, which lists every
constructor, field, operation, encoder and property with its arguments, its type and the issues it
can give.

## Grammar

A decoder form is a JSON array. Its first element is the name of a constructor, the next elements
are the constructor's arguments, as many as the constructor has, and every element after them is an
operation applied to the decoder so far:

```json
["string", ["trim"], ["minLength", 3]]
["list", ["int", ["positive"]], ["nonempty"]]
["object", [["field", "name", ["string"]], ["optionalField", "nick", ["string"]]], ["map", "first"]]
```

An operation is a JSON array whose first element is the operation's name and whose other elements are
its arguments. An operation may have optional trailing arguments; a constructor may not, so that the
end of a constructor's arguments never depends on what follows.

Only a value or a message argument can be optional, and leaving one out means one thing: a value
argument left out stands for its `default`, which every optional value argument declares (`normalize`
without a form is `normalize` with `"NFC"`), and a message argument left out gives the message the
catalogue derives. A form's arguments have distinct names, and a form has at most one message
argument.

Arguments are of these kinds:

| Kind | Written as |
|------|------------|
| `decoder` | a decoder form |
| `decoders` | a JSON array of decoder forms |
| `variants` | a JSON object whose members are tags and decoder forms |
| `fields` | a JSON array of field forms: `[kind, name, decoder]`, or `["flat", decoder]` |
| `value` | an [observation](observation.md) of the argument's type |
| `message` | a JSON string: the message of the issues the operation gives |
| `fixture` | the name of a [fixture](fixtures.md) |
| `encoder`, `properties` | an encoder form, or a JSON array of property forms |

An encoder form has the same shape, with the encoders and properties of `catalog/operations.json`.

## Types

Every form has a type. A constructor's result type follows from its arguments: `["list", ["int"]]`
is a decoder of `list<int32>`, and `object` is a decoder of the product of its fields' types, in
order. An operation applies to the receiver types it lists and gives the result type it lists, where
`R` stands for the receiver's type and a receiver pattern such as `list<E>` binds `E`. The same
operation name can apply to several receiver types (`min` applies to every numeric type) and mean
the same thing for each.

A receiver pattern is `*`, for every type, or a type whose outer kind is not a parameter, and it
never mentions `R`. An operation has at most one pattern for each outer kind, and one that applies
to every type has no other: the receiver's kind alone decides which pattern, and so which form, an
operation means, whatever order `catalog/operations.json` lists them in. That is also what an
operation's feature names, so a feature ID determines the form and the pattern.

A type parameter a form mentions is bound by what a case gives: the receiver of an operation (`R`,
and the parameters of its pattern), the types of the decoders, encoders and properties it takes,
and the types of the fixtures it names. Its result, the types of its value arguments, the types its
issues bind and the type an encoder encodes or a property reads only use parameters, so every
parameter they mention has to appear where one is bound; the registry is rejected otherwise.
Whether a case gives enough to bind them is the case's matter: a form that leaves a type unknown,
such as a generic fixture where nothing fixes its input, does not type-check.

The types are bound in that order. The decoders, fields, encoders and properties a case gives bind
theirs first. Then the fixtures, together: each type a fixture argument declares is matched with
the fixture's own, part by part, and whatever a match fixes is kept, even while the rest of that
fixture is unknown, since another fixture may need it. `product<X,string>` matched with a fixture's
`product<int32,T>` fixes both `X` and `T`. The matches repeat until they fix nothing more; every
type on both sides has to be known by then, whatever order the form lists its fixtures in. Only
then are the value arguments read, as observations of types that are known by now. Wherever a
type written with parameters becomes the type of a value, it is checked once they are bound, as
`optional<T>` with `T` bound to `nullable<string>` is not a type values have.

A value argument is read as an observation of the type the argument has where it is used. In
`["float", ["min", 0.1]]` the bound is the float32 nearest 0.1; in `["decimal", ["min", "0.5"]]` it
is the decimal 0.5 with scale 1; `["int", ["min", 0.5]]` does not type-check.

Some forms put conditions on their arguments, listed as `requires` in `catalog/operations.json`:
the bounds of `range` and `between` must be in order, the divisor of `multipleOf` must not be
zero, the elements of `containsAll` must not be empty, and the symbols of `enum` must stay
distinct when A-Z are read as a-z. A `strictObject` cannot have a `flat` field, since it could not
tell which members that field reads. raoh-java refuses to construct a decoder that breaks one of
them, so such a form is not a decoder of this language either.

A form's result type follows from its arguments. `catalog/operations.json` writes it as a type,
which may mention the form's parameters; as `"product"`, the product of the types of the fields its
one fields argument reads (`object`); or as `"symbol"`, the symbol type whose alternatives are the
strings of the list argument `symbols_from` names (`enum`). `"product"` and `"symbol"` are not types
themselves: a type written anywhere lists everything it has, and `symbol` alone is none.

A form that does not type-check, or whose arguments do not meet what it requires, is not a
decoder, and a case that contains one is rejected.

## Features

Each constructor, field kind, operation on a kind of receiver, encoder, property kind and fixture is
a feature with an ID:

| Feature | ID |
|---------|----|
| a constructor | `decoder.<name>` |
| a field kind | `field.<name>` |
| an operation on receivers of a kind | `operation.<kind>.<name>`, such as `operation.int32.min` or `operation.list.nonempty`; `operation.any.<name>` for one that applies to every type |
| an encoder | `encoder.<name>` |
| a property kind | `property.<name>` |
| a fixture | `fixture.<name>` |

A case needs the features its forms use. An implementation that lacks one declares it unsupported
(see [conformance.md](conformance.md)), and every case that needs it is then unsupported rather than
failed.

## Meaning

`catalog/operations.json` gives the meaning of each form in its `doc`, the issues it can give in
`issues`, and how it gives them in `flow`, an expression over its arguments and its issues that
[issues.md](issues.md) describes. Every argument that gives issues (its decoders, or its fixture) is placed in the flow
or discarded exactly once, and every issue is declared once and given by exactly one part of it;
the registry is rejected otherwise, as it is when the flow or a metadata source names an argument
or an issue the form does not have, or a member the language does not define. These conditions are
checked when the catalogue is read, so a catalogue that breaks one is invalid rather than wrong for
some case. The cases in `suite/` are the specification of the details. Where a `doc` and a case
disagree, the specification has a defect; report it.

Several meanings in version 0.8.0 are raoh-java 0.8.0's behaviour written down, where a later
version may decide otherwise:

- Offset date-times are ordered by instant and, at the same instant, by local date-time
  (`OffsetDateTime.compareTo`), so `before`, `after` and `between` tell apart two values that
  denote the same instant.
- `float` and `double` read the number `-0` as +0 and `-0.0` as -0.
- Floats are ordered as `Double.compare` orders them, so `negative` accepts -0 and `nonNegative`
  rejects it.
- `strict` inside `strict` reports an unknown member once for each.

One meaning differs from what raoh-java 0.8.0 gives with Jackson's default configuration: `decimal`
keeps the scale the lexeme is written with, so `0.0001` gives scale 4. raoh-json documents that its
result depends on how the JSON library parsed the number (a fractional number parsed as a binary64
double comes back with the scale `Double.toString` writes, 0.00010), which makes it adapter
behaviour; the input model keeps the lexeme.

Each is an open issue in this repository.
