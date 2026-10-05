# Fixtures

Some decoders and encoders take a function: `map` takes the function it applies, `refine` a
predicate, `recoverWith` a function from the issues to a value. A case cannot carry code, so it names
a fixture: a function with a fixed meaning that every runner implements. `catalog/fixtures.json`
lists them.

Fixtures are part of the suite, not of Raoh. A Raoh implementation has no fixtures; a runner builds
each one out of the implementation's own functions and passes it where the case names it.

Each fixture has a kind, which says where it may be used, an input type and, except for `refine`, an
output type. The kind fixes that shape. It also says whether the fixture gives an issue: a `refine`
or `flatMap` fixture does and the others do not. An argument of `catalog/operations.json` that takes
a `refine` or `flatMap` fixture gives issues in turn, and its form places or discards them as it
does those of a decoder. Types may have parameters: `first` takes a `product<T>` for any `T`. A fixture takes one
value, so its output type and the metadata types of its issue mention only parameters of its input:
once the input is known, so are they. Where a case uses a generic fixture, its issue's metadata has
the types its parameters take there.

| Kind | Used by | Meaning |
|------|---------|---------|
| `map` | the `map` operation | a function from the input type to the output type |
| `refine` | the `refine` operation | a predicate over the input type, with the issue to give when it does not hold |
| `flatMap` | the `flatMap` operation | a function that gives a value of the output type, or fails with the issue it declares |
| `recover` | the `recoverWith` constructor | a function from the issues of a failure to a value |
| `getter` | encoder properties | a function that reads the property's value from the value being encoded |

## Issues from fixtures

A `refine` or `flatMap` fixture declares the issue it gives: its code, message key, message and
metadata types, and for `flatMap` the path, relative to the path the decoder runs at, where it gives
it. `values` says where each metadata value comes from: `"input"` is the value the fixture was
given. The message is given, not derived (see [issues.md](issues.md#messages)); the empty string is
a message like any other.

A runner implements such a fixture as a user of the implementation would write it, with whatever
the implementation offers for a custom issue, so that the issue has exactly the declared parts.

## Adding fixtures

A fixture should be the smallest function that lets a case exercise what it checks. A case that
needs a new one adds it to `catalog/fixtures.json` in the same pull request; that is a minor change
(see [conformance.md](conformance.md#versioning)), because every runner has to implement it.
