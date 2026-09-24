# Jev/Nano offline pilot runner

This directory contains the frozen, gold-free 24-case × 2-arm request inventory and the Stage 2 runner implementation. The checked-in inventory is tuning-only. It contains the questions, evidence, candidate pool, relationship endpoints, task vocabularies, common rubric, exact arm payload bytes, hashes, and the seed-579 paired schedule. It deliberately excludes expected labels, rationales, supporting gold spans, authorship/review metadata, challenge tags, and candidate-generation commentary.

`request-inventory.json` and `corpus-reference.json` are generated once with `jev-pilot prepare` using exclusive creation and read-only file permissions. Gold remains in `review-pilot/fixtures.jsonl` and is joined locally only by `pilot.Project` during scoring.

Exact request bodies are stored as base64 byte strings. This prevents JSON pretty-printing from rewriting the frozen wire bytes; each decoded body must match its recorded SHA-256 before use.

## Network-free preparation

```sh
go run ./cmd/jev-pilot prepare \
  -fixtures review-pilot/fixtures.jsonl \
  -manifest review-pilot/fixtures.manifest.json \
  -out /new/run/request-inventory.json \
  -corpus-reference-out /new/run/corpus-reference.json \
  -jev-request-model typesafe/jev-1.13 \
  -nano-request-model openai/gpt-4.1-nano \
  -nano-provider OpenAI
```

`report` replays a journal without network access:

```sh
go run ./cmd/jev-pilot report \
  -journal /run/journal.jsonl \
  -inventory /run/request-inventory.json \
  -evidence-dir /run/evidence \
  -fixtures review-pilot/fixtures.jsonl \
  -manifest review-pilot/fixtures.manifest.json
```

## Execution remains gated

`run` requires a separately reviewed execution manifest. There are no cap, endpoint, identity, rate, token-bound, credential, or authorization defaults. It refuses execution unless `liveContractsVerified` is explicit, the authorization reference and combined cap are present, both fixed arm subcaps fit within that cap, the inventory hash matches, exact request aliases/response pins/provider identities are supplied, and both rate/bound configurations open their persistent ledgers without drift.

The manifest also carries an allocation ID, the full authorization cap, the frozen prior exposure, and an exclusive allocation-record path. The allocation record binds the manifest SHA-256, inventory hash, run ID, journal, both ledgers, evidence directory, and run-lock path. A fresh run cannot reuse a claimed allocation; resume requires the exact same allocation record. The required `probeReconciliationPrecondition` value is `freeze-execution-and-re-review-allocation-before-any-further-send`: any reconciliation of probe exposure into a later pilot allocation must be frozen and independently reviewed before another send. For the current authorization, `949856` microdollars is a bound for one uniquely allocated pilot run after preserving the frozen `50144` microdollar probe exposure; it is not a reusable cap for disjoint fresh runs.

The historical 8,064 + 24,144 microdollar figures are planning inputs only and are intentionally not embedded as executable defaults. Before any live run, independent review must verify provider billing fields and fee semantics, enforceable billed-token bounds, exact alias-to-response-pin mappings, response-linked provider identity, environment-backed credentials, and a spend authorization bound to this exact inventory and run directory.

The runner performs one attempt per fixture/arm and never retries a fixture. Its append-only fsynced journal begins with the inventory, authorization reference, combined cap, and complete execution-manifest hash, then records intent, reservation, send start, bounded private response evidence, structured observation, and reconciliation. Resume cross-checks journal reservations in both directions against both ledgers and verifies every raw and structured evidence file by hash. `RequestSent` means only that `http.Client.Do` was attempted. Any incomplete or orphaned attempt is never resent automatically. Missing, null, invalid, or schema-rejected billing; observed usage beyond either token bound; and schema/identity/auth/payload contract failures halt both arms. Explicit numeric zero is recorded separately and retains the full reservation; it is never converted to a fake positive settlement.

Before persistence, the pilot semantically decodes JSON and screens every decoded string value against the environment credential values. Identity fields, response headers, retry diagnostics, billing detail diagnostics, journal errors, halt reasons, report rows, and returned adapter errors use the screened values. Numeric JSON tokens remain byte-identical. When response bytes cannot be screened safely, the response body is replaced by a hash-only marker; when decoded credential material is redacted, the original bytes are withheld and represented by their SHA-256 alongside an explicit evidence reason.

## Offline three-arm binding

`jev-pilot compare-sidecar` creates a frozen comparison identity after the offline baseline report and the two live-arm pilot report exist. It binds the protocol version, fixture-file and manifest digests, request-inventory hash, and canonical identities for the deterministic baseline, Jev, and Nano reports. Verification rebuilds the identity from the same files and rejects any corpus, inventory, source, protocol, fixture-count, container, or report digest mismatch. The sidecar is scoring-only and never enters the gold-free live path.
