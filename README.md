# Raoh Specification

This repository defines what a Raoh decoder library does, independently of the language it is
written in, and provides the suite and the verifier that check an implementation against it.

The specification is the source of truth. [raoh-java](https://github.com/raoh-project/raoh-java)
is the first implementation, not the reference: where raoh-java and this specification disagree,
raoh-java either changes or declares a divergence, exactly as any other implementation does.
Version 0.8.0 was written by extracting the language-independent behaviour of raoh-java 0.8.0,
case by case, from the compatibility corpus the Go and Rust ports had been keeping.

## What is specified

The specification covers the behaviour of decoders over the Raoh input model (the values a JSON
text denotes, with the lexeme of each number kept), the encoders whose output is JSON-representable,
the issue model, paths, and the issue and message catalogues.

It does not cover adapters: how a host language's own values, a database library's records or a
JSON library's trees are mapped onto the input model. raoh-java's `ObjectDecoders`, `MapEncoders`
and `raoh-jooq`, a PHP associative array or a Rust `serde_json::Value` are all adapters.

## Layout

| Path | Contents |
|------|----------|
| `specification.json` | The version of the specification this revision describes |
| `spec/` | The prose specification |
| `catalog/issues.json` | Every issue variant, keyed by message key, with the type of its metadata |
| `catalog/operations.json` | Every constructor and operation the decoder language has, with its type |
| `catalog/fixtures.json` | The named functions the suite uses where a decoder takes a function |
| `catalog/messages/` | The English and Japanese message catalogues |
| `suite/` | The conformance cases |
| `schema/` | JSON Schemas for cases, runner results, conformance declarations and reports |
| `cmd/raoh-verify` | The verifier |

The Go packages at the root (`jsontext`, `value`, `catalog`, `dsl`, `suite`, `compare`,
`manifest`, `verify`) are the verifier's implementation. They are not a Raoh implementation and
share no code with raoh-go.

## Checking an implementation

An implementation provides a runner that binds the features it implements, runs every case it can
and writes what each case gave as its observation, together with the message catalogues it ships.
The runner does not compare anything. `raoh-verify` compares the runner's result with the suite
and with the implementation's `conformance.json`, which declares the features it does not
implement and the cases where it gives something else on purpose, and writes a report:

```sh
raoh-verify verify --spec path/to/raoh-specification \
    --result runner-result.json --conformance conformance.json -o conformance-report.json
```

See [spec/conformance.md](spec/conformance.md) for the runner protocol, the declaration and the
meaning of the statuses a report gives.

## Recording is not specifying

An implementation that is ahead of the specification can record what it does for new inputs, and
propose those recordings here. A recording is a candidate: it becomes a case only after review,
normalisation and the assignment of a stable case ID in this repository. No tool writes into
`suite/` from an implementation's behaviour.

## Versioning

See [spec/conformance.md](spec/conformance.md#versioning).

## License

Apache License 2.0
