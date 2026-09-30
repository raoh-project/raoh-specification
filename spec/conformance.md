# Conformance

An implementation conforms to this specification when, for every case of the profiles it is
checked against, it gives the outcome the case expects. This document says how that is checked and
what an implementation may state about the result.

## Versioning

The specification is versioned independently of any implementation, with semantic versioning. The
version this revision describes is in `specification.json`. Between releases it carries a
prerelease suffix (`0.8.0-dev`); a release is a tag `vX.Y.Z` on a commit whose
`specification.json` says `X.Y.Z`, and a tag is never moved.

The part of the version a change increments depends on which implementations it can turn from
conforming into not conforming:

| Change | Before 1.0 | From 1.0 |
|--------|-----------|----------|
| Wording, examples, or a case that follows from what is already specified | patch | patch |
| Making one of several behaviours the specification allowed the only one allowed | minor | major |
| Changing what an existing case expects | minor | major |
| A new constructor, operation, fixture or issue variant | minor | minor |
| A new profile that no implementation is required to be checked against | minor | minor |

A case that tests behaviour the specification already requires is a patch even when no case tested
it before: an implementation that fails it did not conform before either. A case that settles
behaviour the specification did not require is not a patch, because an implementation that
conformed can stop conforming without changing.

## Case IDs

Every case has an ID that names what it checks, such as `string.min_length.counts_utf16_units`.
An ID is permanent: it does not change when the outcome the case expects changes, and it is not
reused for a case that checks something else. When what a case checks changes, the case gets a new
ID and the old one is retired. Conformance declarations refer to cases by ID.

## Profiles

| Profile | What it checks |
|---------|----------------|
| `core` | Decoding over the input model, the issue model, paths and message resolution |
| `encode` | Encoding into JSON-representable values |
| `messages-en` | That the English catalogue the implementation ships is `catalog/messages/en.properties` |
| `messages-ja` | That the Japanese catalogue the implementation ships is `catalog/messages/ja.properties` |

The profiles an implementation lists are the ones it is checked against. Listing a profile is not
a claim that the implementation conforms to it; the report says whether it does.

## Checking

Checking involves three parties. The runner belongs to the implementation: it binds each feature
of `catalog/operations.json` and `catalog/fixtures.json` the implementation has, runs the cases
whose features are all bound, and writes a runner result. The declaration, `conformance.json`,
also belongs to the implementation. The verifier, `raoh-verify`, belongs to this repository: it
reads the suite, the catalogues, the runner result and the declaration, and alone decides what
each case's outcome means. The runner never compares, skips by name or classifies.

### Runner result

A runner result (`schema/runner-result.schema.json`) records:

- `format`: `raoh-runner-result/v1`.
- `specification`: the `version` of the specification the cases were read from, the `revision`
  (commit) they were read at, and the `manifest_digest` of that revision (see below).
- `implementation`: its `name`, `version` and `revision`.
- `environment`: the language, its version, the operating system and the architecture. It is
  recorded, never compared.
- `bound_features`: the feature IDs the runner binds.
- `results`: for each case ID the runner ran, the `observed` outcome, written as the case writes its
  expected outcome.
- `catalogs`: for each locale the implementation ships (`en`, `ja`), every message key and its
  template.

The manifest digest ties a result to the exact suite it was produced from. It is the SHA-256 of
every file under `spec/`, `catalog/`, `schema/` and `suite/`, and of `specification.json`, taken
in the order of their paths (relative, `/`-separated, compared byte by byte). Each file contributes

```text
<length of the path in bytes> LF <path> LF <length of the content in bytes> LF <content>
```

where the content has every CRLF replaced by LF. The digest is written `sha256:<hex>`.
`raoh-verify manifest` prints it.

### Declaration

A declaration (`schema/conformance.schema.json`) records:

- `implementation`: the implementation's name.
- `specification`: the version it is checked against.
- `profiles`: the profiles it is checked against.
- `unsupported_features`: for each feature the implementation does not have, why.
- `divergences`: for each case where the implementation gives something else on purpose, its
  `category`, the `reason`, and the outcome it gives, as `observed`.

A divergence's category is `platform` when it follows from a constraint of a runtime or platform
the implementation supports, and `design` when the implementation chose it. A known bug is neither:
it is a failure until it is fixed, and cannot be declared.

The declaration explains the gaps the verifier finds; it does not create them. A feature the runner
binds cannot be declared unsupported, and a case cannot be declared divergent unless the runner can
run it.

### Invalid input

Before comparing anything, the verifier checks that it can trust the comparison. The run is
invalid, and the verifier exits with status 2 without writing a report, when:

- the suite or the catalogues fail `raoh-verify check-suite`;
- the runner result or the declaration does not match its schema;
- the runner result's manifest digest is not the digest of the suite the verifier was given;
- the declaration's specification version is not the suite's version;
- the runner result or the declaration refers to a case ID or feature ID the suite does not have;
- a divergence has a category other than `platform` or `design`;
- a divergence refers to a case that needs a feature the runner does not bind, or to a case of a
  profile the declaration does not list;
- a divergence declares the outcome the case expects;
- the runner result has an outcome for a case that needs a feature the runner does not bind;
- a feature the runner binds is declared unsupported.

### Outcomes

Each case of a profile the declaration lists is classified in this order:

1. If a feature the case needs is not bound: `unsupported` when every such feature is declared
   unsupported, `failed` otherwise.
2. If the runner result has no outcome for the case: `failed`.
3. If the outcome is the one the case expects: `matched`, unless the case is declared divergent, in
   which case the divergence is stale and the case is `failed`.
4. If the case is declared divergent and the outcome is the one the declaration gives: `divergent`.
5. Otherwise: `failed`.

For `messages-en` and `messages-ja`, each message key of the catalogue is a case: `matched` when
the implementation's template is the catalogue's, `failed` when it differs or is missing. A key the
implementation ships and the catalogue does not have is also `failed`.

### Status

The report gives each profile a status:

| Status | Condition |
|--------|-----------|
| `conformant` | No case is `failed`, `divergent` or `unsupported` |
| `partially_conformant` | No case is `failed`; some are `divergent` or `unsupported` |
| `non_conformant` | Some case is `failed` |

`raoh-verify verify` exits with status 0 when no profile is `non_conformant`, and 1 otherwise.
A stale divergence is a failure: the declaration has to be brought up to date.

An implementation states its conformance per profile, with the specification version and the
counts, for example: Raoh Specification 0.8.0 — core: conformant; encode: partially conformant
(1 unsupported). Only a report produced by `raoh-verify` supports such a statement.
