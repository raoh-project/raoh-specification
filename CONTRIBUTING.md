# Contributing

## Branches

`develop` is the integration branch. Work on a `feature/<topic>` branch cut from `develop` and open
the pull request against `develop`. `main` receives releases only.

## Releasing

Cut a `release/<version>` branch from `develop`, set `version` in `specification.json` to the
release's `X.Y`, commit it as `release: Raoh Specification X.Y`, and open a pull request against
`main`. After it is merged, tag the merge commit `vX.Y` and push the tag. The tag triggers
`.github/workflows/release.yml`, which fails unless the tag is `v` followed by the version in
`specification.json`, then runs the tests and `check-suite`, builds `raoh-verify` for six
platforms and publishes a GitHub release. A tag is never moved; see
[spec/conformance.md](spec/conformance.md#versioning).

Afterwards, set `version` on `develop` to the next prerelease, such as `0.10-dev`, and commit it
as `chore: start X.Y-dev`.

## Adding or changing cases

A case lives in a file under `suite/<profile>/`, grouped by the constructor it exercises. Each case
has an ID, `R` and six digits, and a title. Give a new case the next number after the highest ID in
the suite or in `suite/retired.json`; the ID means nothing and never changes. Write in the title
what the case checks. When you remove a case, add its ID to `suite/retired.json`; when a case comes
to check something else, remove it and add a new one.

Before opening a pull request, run:

```sh
go run ./cmd/raoh-verify check-suite .
go test ./...
```

CI also runs `raoh-verify check-ids` against the branch the pull request targets.

`check-suite` also fails when a feature of `catalog/operations.json` or `catalog/fixtures.json` is
needed by no case: a feature is listed only once a case pins it. One case is the least a feature
needs, not proof that it is fully specified. In the same way, an entry a variant lists in
`optional_meta` needs a case that leaves it out where a form leaves it open, and each issue a form
that takes a message declares needs a case that gives that form its message.

`check-suite` rejects a case that does not type-check, whose expected outcome is not an observation
of the decoder's result type, whose issues are not ones the decoder can produce, or whose input is
not valid JSON or repeats a member name. It also rejects two cases with the same form and input,
up to whitespace and string escapes: they check one thing twice. When only the reason for an
outcome changes, change the title of the case that has it rather than adding another.

State in the pull request which row of the versioning table in
[spec/conformance.md](spec/conformance.md#versioning) the change falls under, or that it changes
only wording or examples, without changing a case or a requirement, and so no version.

## Proposing behaviour an implementation already has

An implementation that is ahead of the specification can record what it gives for a set of inputs.
Such a recording is a proposal: open a pull request that turns it into cases, with IDs, and says
why the recorded behaviour is the one the specification should require. The reviewers decide
whether it is; the recording decides nothing.

## Questions the specification has not settled

When a case would require behaviour that looks like an accident of one implementation rather than
a decision, open an issue instead of, or as well as, the case, and decide it as the next section
says.

## How a meaning is decided

Raoh's meanings are defined from its own value model and the laws of its operations. A standard
defines a domain where Raoh names a standardized concept. What an implementation or its host
language does is evidence, never the reason. Work through these in order, and stop at the first
that decides:

1. Is it a meaning of the language, or a fact about an adapter or a runtime? Only the first goes
   into the specification. How Jackson reads a number, or how `Double.compare` or
   `OffsetDateTime.compareTo` order values, is a fact about a host, and is not written down as the
   meaning.
2. What is the value? Decide what makes two values the same first (whether -0 and +0 differ,
   whether an offset date-time is an instant or a local date-time with an offset, whether a
   decimal keeps its scale). Equality, ordering and how a value is written mostly follow from it.
3. What laws does the operation keep? A meaning that makes an operation idempotent, or that keeps
   a relation such as chronology apart from another such as value order, is preferred to one that
   does not.
4. Where Raoh names a standard concept (a URI, an offset date-time, an email address), the
   standard defines the domain. Follow the standard's updates to the current text: an RFC that
   another updates is read as updated. Then check that Raoh's value can hold what the standard
   means; where it cannot, say so, or refuse what it cannot hold, rather than keep part of it
   silently.
5. The result must be the same in every implementation. A host library's formatting, a hash or
   set iteration order, or a choice a library leaves unspecified is not a meaning; write the rule
   out.
6. Only when the meanings left are equally good, keep the existing behaviour.

When an implementation's types cannot hold a value the specification defines (a URI library that
rejects part of RFC 3986, say), decide the specification and the implementation's migration
separately. The specification is not narrowed to fit an implementation's types.

A fact the specification closes is written as a type where it can be, so that the verifier checks
it: the `actual` of `type_mismatch` is a symbol type of its seven words, not a string described in
prose.
