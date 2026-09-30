# Contributing

## Branches

`develop` is the integration branch. Work on a `feature/<topic>` branch cut from `develop` and open
the pull request against `develop`. `main` receives releases only.

## Adding or changing cases

A case lives in a file under `suite/<profile>/`, grouped by the constructor it exercises. Each case
has an ID of the form `<subject>.<what it checks>`, in lower snake case separated by dots, such as
`string.min_length.counts_code_points`. Choose the ID for what the case checks, not for its input:
the ID stays when the expected outcome changes, and a case that comes to check something else gets
a new ID. An ID that has been released is never reused.

Before opening a pull request, run:

```sh
go run ./cmd/raoh-verify check-suite .
go test ./...
```

`check-suite` also fails when a feature of `catalog/operations.json` or `catalog/fixtures.json` is
needed by no case: a feature is listed only once a case pins it. One case is the least a feature
needs, not proof that it is fully specified.

`check-suite` rejects a case that does not type-check, whose expected outcome is not an observation
of the decoder's result type, whose issues are not ones the decoder can produce, or whose input is
not valid JSON or repeats a member name.

State in the pull request which row of the versioning table in
[spec/conformance.md](spec/conformance.md#versioning) the change falls under.

## Proposing behaviour an implementation already has

An implementation that is ahead of the specification can record what it gives for a set of inputs.
Such a recording is a proposal: open a pull request that turns it into cases, with IDs, and says
why the recorded behaviour is the one the specification should require. The reviewers decide
whether it is; the recording decides nothing.

## Questions the specification has not settled

When a case would require behaviour that looks like an accident of one implementation rather than
a decision, open an issue instead of, or as well as, the case. Version 0.8.0 describes several such
behaviours of raoh-java as they are, and the issues tracker lists them.
