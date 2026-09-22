# Jev/Nano offline pilot runner

This directory contains the frozen, gold-free 24-case × 2-arm request inventory and the Stage 2 runner implementation. The checked-in inventory is tuning-only. It contains the questions, evidence, candidate pool, relationship endpoints, task vocabularies, common rubric, exact arm payload bytes, hashes, and the seed-579 paired schedule. It deliberately excludes expected labels, rationales, supporting gold spans, authorship/review metadata, challenge tags, and candidate-generation commentary.

`request-inventory.json` and `corpus-reference.json` are generated once with `jev-pilot prepare` using exclusive creation and read-only file permissions. Gold remains in `review-pilot/fixtures.jsonl` and is joined locally only by `pilot.Project` during scoring.

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
  -fixtures review-pilot/fixtures.jsonl \
  -manifest review-pilot/fixtures.manifest.json
```

## Execution remains gated

`run` requires a separately reviewed execution manifest. There are no cap, endpoint, identity, rate, token-bound, credential, or authorization defaults. It refuses execution unless `liveContractsVerified` is explicit, the authorization reference and combined cap are present, both fixed arm subcaps fit within that cap, the inventory hash matches, exact request aliases/response pins/provider identities are supplied, and both rate/bound configurations open their persistent ledgers without drift.

The historical 8,064 + 24,144 microdollar figures are planning inputs only and are intentionally not embedded as executable defaults. Before any live run, independent review must verify provider billing fields and fee semantics, enforceable billed-token bounds, exact alias-to-response-pin mappings, response-linked provider identity, environment-backed credentials, and a spend authorization bound to this exact inventory and run directory.

The runner performs one attempt per fixture/arm and never retries a fixture. Its append-only fsynced journal records intent, reservation, send start, bounded private response evidence, observation, and reconciliation. `RequestSent` means only that `http.Client.Do` was attempted. Any incomplete journaled attempt or attempted request without a response is never resent automatically. Missing, null, invalid, or schema-rejected billing halts both arms. Explicit numeric zero is recorded separately and retains the full reservation; it is never converted to a fake positive settlement.
