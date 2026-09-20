# Offline evaluation harness for KHA-579 (Jev), protocol v0.3

This directory contains the offline evaluation harness for Linear
[KHA-579](https://linear.app/khaentertainment/issue/KHA-579): a Go-based
harness that scores candidate-generation, deterministic-baseline, and
remote-model pipelines against a synthesized corpus of evaluation
fixtures.

The harness follows protocol **v0.3**, which supersedes v0.2. The v0.3
changes are recorded in `CHANGELOG.md`. Earlier reports are superseded.
Use the root-level fixture and report paths below; the `pilot/` directory
is a stale duplicate location, not an authoritative historical snapshot.

## Scope disclosure — SMOKE TEST ONLY

**This corpus is a smoke test for the offline harness, not a
measured-quality study.** Key limitations:

- All 332 fixtures carry `reviewStatus: "unreviewed"` — no human
  adjudication has been performed.
- Body text mentions the same underlying fictional entities across
  nominal tuning/held-out splits (e.g., "Vornholt Pass", "Greycloak
  Workshop", "Maringen", "Breyganth", "Cantorian", "Steppes" appear in
  both splits). Candidate-level IDs are salted and disjoint, but the
  body-level entity channel is NOT eliminated.
- Shared perturbation syntax recurs across splits (e.g., the exact
  "[Conflicting update for X: Y does NOT participate; this contradicts
  the prior statement.]" template and "Formerly known as X." pattern
  are byte-identical modulo salted names). TemplateFamily IDs are
  per-group, so the split check passes, but the syntactic pattern is
  shared.
- Split independence check (`loader.VerifySplitIndependence`) establishes
  **field-level disjointness only** (group IDs, template-family IDs,
  normalized entity-name/alias sets). It does NOT establish semantic or
  template independence, and the protocol's 150-independent-negative
  bound is not met.
- All `GateEligible` fields reflect **vocabulary coverage only** — every
  label in each task's closed vocabulary appears at least once in gold.
  This does NOT imply statistical validity or study readiness.
- Contradiction recall has exactly **one** `contradicted` gold case in the
  entire corpus; no meaningful recall estimate is possible.

**Held-out quality study: NOT prepared.** Human labels, effective
independent sample size, and real model comparisons remain absent.

## What this package does

* Reads a JSONL fixture set and its companion manifest, validating the
  fixture schema, source-group independence across splits, template-family
  independence across splits, and SHA-256 digests for both the file and
  each fixture record.
* Implements isolated typed HTTP adapters for OpenRouter Decisions
  (`/api/alpha/decisions`) and structured-output chat
  (`/api/v1/chat/completions`) with full transport validation: required
  fields, finite distributions in [0,1] with sum tolerance 1e-3, optional
  probabilities and confidence, model-pin mismatch, payload size cap,
  timeouts, cancellation, and 401/402/403/429/5xx mapping.
* Captures per-attempt durations and total wall time including backoff
  and body reads; **never** relabels any of these as provider cold/warm.
* Runs a deterministic abstaining baseline per task with no model
  dependencies; baseline predictions never invent latency, cost, or
  confidence.
* Computes per-task and per-(task, split) scoring: confusion matrix,
  macro-F1, macro-precision, macro-recall, accuracy, abstention rate,
  abstention numerator/denominator, unsupported-acceptance
  numerator/denominator/rate, contradiction-miss
  numerator/denominator/rate, edge precision/recall, direction accuracy,
  candidate recall average, failure and retry counts. The negative-class
  denominators are exposed explicitly so the protocol's headline gates
  (≤ 0.02 unsupported acceptance, ≥ 0.95 contradiction recall) are
  computed on the right base.
* Marks gate eligibility separately and reports the reason when the
  corpus does not exercise every vocabulary label or every required
  negative class. Gate eligibility is never silently false.
* Calibration block: Brier score (from distributions only), plus two
  **separate** reliability tables that are NEVER blended:
  - `ChoiceConfidence`: model-reported `Confidence` values (when present)
  - `MaxProbability`: derived `max(Probabilities)` values (when present)
  Each table has its own sample population and coverage-by-threshold.
  Absence of either is recorded as absence, never synthesized to 0.
* JSONL replay path joins `fixtureId` to the authoritative loaded corpus;
  the replay stream carries NO gold / task / split / sourceGroup fields,
  eliminating the prior code's adversarial override vector. Replay
  entries are validated against the same shared semantic validator the
  live adapters use, and rejected under a declared coverage policy
  (strict, allow-unknown, first-wins) with optional RunKey for
  repetitions.
* Runs an actual candidate-generation exercise (`jev-candidategen`) that
  invokes `candidate.Generate` end-to-end and reports entity / edge
  recall against the supplied pool. The exercise supports deliberate
  gold-omission cases so a "perfect recall" claim cannot be gold-fed.
  **This exercise is explicitly labeled `supplied-pool-stress-test` and
  does NOT measure realistic discovery recall.**
* Evidence-only deterministic discovery baseline (`discovery` package):
  extracts candidates from raw markdown body via three regex patterns
  (heading, wikilink, definition), never reads fixture Candidates or
  ExpectedLabel; gold is joined only at scoring time. Reports
  `missedEntities` list showing what real discovery would need to add.
* Never performs network inference. Live runner paths are reachable but
  the default invocation runs offline only.

## What this package does not yet do

* No live paid runs. Adapter transport is fully covered by httptest
  suites, but no request has actually been sent to OpenRouter; the
  resolved model pin (`typesafe/jev-1.13-20260917` vs `jev-1.13.0`)
  remains unverified and the adapter rejects any pin mismatch rather
  than silently aliasing.
* No fixture label adjudication. Every fixture carries
  `reviewStatus: "unreviewed"`; humans must approve or adjust labels
  before any claim of measured quality is made.
* No held-out evaluation against a real model. The pilot corpus is a
  reproducer for harness behaviour, not a completed 840-case study.
* No replace-in-production semantics. None of the harness functions
  imports Plexium production code or MarkedUp's enrich/extract path.

## Layout

```
evaluations/jev/
├── README.md                  this file
├── CHANGELOG.md               protocol version history
├── go.mod                     standalone module (no root go.mod touched)
├── protocol/                  fixture schema, vocabularies, manifest types
├── adapter/                   typed HTTP adapters for Decisions + chat
├── candidate/                 deterministic candidate generation
├── candidateex/               candidate-generation exercise (invoke + measure)
├── discovery/                 evidence-only deterministic discovery baseline
├── baseline/                  deterministic abstaining baseline
├── scoring/                   per-task metrics, calibration
├── validate/                  shared semantic validator (replay + adapter)
├── loader/                    JSONL + manifest read/write, drift detection
├── runner/                    baseline + replay runners
├── cmd/jev-corpus/            writes the pilot fixture JSONL
├── cmd/jev-manifest/          writes the manifest from a fixture JSONL
├── cmd/jev-eval/              runs the deterministic baseline + optional replay
├── cmd/jev-candidategen/      runs the candidate-generation exercise
├── fixtures.jsonl             332 fixtures across 29 source groups
├── fixtures.manifest.json     manifest with file + per-fixture SHA-256
├── eval-report.json           baseline + discovery + (optional) replay report
├── candidategen-report.json   candidate-generation exercise output (supplied-pool stress test)
└── pilot/                     stale duplicate location; do not use for evaluation
```

## Reproducible offline run

The reproducible command line:

```bash
cd evaluations/jev
gofmt -l .
go vet ./...
go test -race ./...
go run ./cmd/jev-corpus -out fixtures.jsonl
go run ./cmd/jev-manifest -fixtures fixtures.jsonl -out fixtures.manifest.json
go run ./cmd/jev-eval -fixtures fixtures.jsonl \
    -manifest fixtures.manifest.json \
    -out eval-report.json
go run ./cmd/jev-candidategen -fixtures fixtures.jsonl \
    -manifest fixtures.manifest.json \
    -out candidategen-report.json
```

The eval CLI does not require any API keys and does not dial out. Live
inference is not in scope of the default invocation; future live runners
must add explicit `--live` and credentials flags.

**Reproducibility comparison:** Compare semantic JSON after excluding only
runtime timestamps and measured duration fields. Raw outputs may differ
in those fields. Preserve raw timing records, and never remove cost or
usage fields from comparison. The final independent reproducibility check
is recorded in the KHA-579 review artifacts.

## Pilot corpus (v0.3)

* 332 fixtures across 29 synthetic source-group IDs (20 base
  groups: 4 tuning / 16 held-out; plus 9 coverage fillers targeting
  missing vocabulary labels in the held-out split).
* Entities, titles, and bodies are fictional. This does not establish
  independent scenarios or remove repeated-pattern leakage.
* Candidate-level IDs are salt-suffixed with the group ID
  (`sg-001/vornholt-pass`) guaranteeing disjointness across splits.
* Perturbation text and family IDs embed group IDs, but shared syntax
  still crosses splits. This corpus cannot support held-out quality claims.
* Field-level split check (per protocol v0.3): the loader
  enforces template-family, group-ID, and normalized entity-name/alias
  disjointness across splits with explicit per-dimension flags.

Per-split counts:

| Task | Tuning | Held-out | Total |
| --- | --- | --- | --- |
| entity-type | 12 | 75 | 87 |
| candidate-type | 14 | 67 | 81 |
| relationship | 16 | 76 | 92 |
| claim-support | 12 | 60 | 72 |
| **total** | **54** | **278** | **332** |

All 10 entity-type labels, all 7 candidate-type labels, all 9
relationship vocabulary entries (7 generic predicates + 2 abstain labels),
and all 3 claim-support verdicts are exercised by gold in the held-out
split. The protocol's headline gates are computable on this corpus
without gate-ineligibility firing for "label not exercised" in held-out.
**Tuning split gate eligibility is false for multiple tasks due to
missing labels** (by design — tuning is small).

## Candidate generation (v0.3)

* Pool entries carry an opaque ID (the source group's `LocalID`); the
  shortlist preserves the ID verbatim. Classifier inputs see only opaque
  tokens; ranker role tags (`title:N`, etc.) do not leak into candidate
  IDs.
* The candidate-generation exercise (`candidateex`) runs
  `candidate.Generate` end-to-end against curated fixtures and reports
  entity / edge recall against the supplied pool. It supports deliberate
  gold-omission cases so recall cannot silently hit 1.0 due to gold
  feeding.
* **Kind is `supplied-pool-stress-test`** — the pool is gold-derived and
  the exercise measures stress-test recall only, NOT realistic discovery
  recall. See `candidategen-report.json` for the explicit provenance
  note.
* Truncation flags are recorded on the shortlist and surfaced in the
  exercise report's `TruncatedFixtures` list.

Latest candidate-exercise summary (pilot):

| Metric | Value |
| --- | --- |
| Entity recall (overall) | 0.270 |
| Edge recall (overall) | 0.015 |
| Omitted gold fixtures | 0 |
| Truncated fixtures | 0 |

## Discovery baseline (v0.3, new)

* Package `discovery` implements an evidence-only deterministic baseline.
* Three extraction patterns: markdown headings (`# ` through `###### `),
  wikilinks (`[[Title]]` or `[[Title|alias]]`), definitions
  (`def: <Title> = <description>`).
* **Never reads fixture `Candidates`, `ExpectedLabel`, or `GoldEntities`**.
  Gold is joined only at scoring time via `discovery.JoinGold`.
* Reports `entityRecall`, `edgeRecall`, `missedEntities`, `missedEdges`,
  and per-pattern counts. On this corpus (bodies lack markdown patterns),
  entity recall = 0.0 and the full gold entity list appears in
  `missedEntities` — this is the intended honest signal.
* The baseline is explicitly labeled an "intentionally narrow low-recall
  upper bound on what an entity-aware extractor might achieve."

## Complexity inventory

This section enumerates the moving parts of the harness. Each entry says
what the harness does, what it does not do, and what evidence is still
missing before a future adoption claim.

### 1. Candidate generation

* Deterministic title / alias / wikilink match against evidence excerpts,
  capped at 12 entities and 24 directed edge candidates.
* Truncation is recorded on the `Shortlist` so the scorer can distinguish
  "missed because truncated" from "missed because no evidence".
* Pool entries may carry an opaque ID; the shortlist preserves it.
* No generative candidate discovery. The harness accepts the supplied
  candidate pool as ground truth for evaluation.

Still missing:

* Confidence-calibrated candidate ranker.
* Empirical evidence for whether 12 / 24 caps are right at scale.
* Generative discovery pass (deliberately not in scope; the
  fixture-supplied candidate list is the only source of truth).

### 2. Typed adapters

* Decisions and chat adapters share a typed `TransportError` taxonomy.
* Auth (401/403), usage overrun (402), schema/model-pin failures, timeouts,
  payload overflow, transport errors, and rate-limit/server retries are
  covered.
* Auth and schema errors do not retry; transient errors retry at most
  once, respecting Retry-After up to the run deadline.
* Per-attempt durations and total wall time (including backoff and body
  reads) are recorded; the harness never relabels any of these as
  provider cold/warm — that attribution requires a separate measurement
  the adapter cannot make from a single call.
* Confidence is `*float64`: nil means the provider omitted it; the
  scorer treats absence as absence rather than 0.0.
* Probabilities are optional. When present they must be finite, in
  [0,1], and sum to ~1. When absent the harness records absence and the
  scorer does not synthesize a degenerate distribution.

Still missing:

* Live exercised against `/api/alpha/decisions` with the documented Jev
  pin (`jev-1.13.0` or `typesafe/jev-1.13-20260917`). Until that pin is
  verified as requestable, the harness refuses to alias any other
  identifier silently.
* Paid-run accounting. The `Decision.Usage.Cost` field is preserved
  verbatim; the harness never invents cost when the provider omits it.
* Empirical injection-resistance evidence. Unit tests cover parser
  robustness but cannot prove prompt-injection resistance — that
  requires live quality measurements on adversarial cases.

### 3. Deterministic abstaining baseline

* `entity-type` always returns the default fallback `document`.
* `candidate-type` always returns the first vocabulary label `PERSON`
  (the baseline's fixed vocabulary for candidate-type is the first
  `CandidateTypeLabels` entry).
* `relationship` always returns `insufficient-evidence`. The protocol
  requires deterministic code to abstain rather than guess a predicate.
* `claim-support` always returns `insufficient-evidence`.

The baseline does not record confidence, latency, or cost. The scorer is
responsible for not fabricating them — the calibration report is `nil`
when no probabilities are observed.

### 4. Scoring

* Per-task confusion matrix indexed by vocabulary label.
* Macro-F1 / macro-precision / macro-recall across the closed vocabulary.
* Per-(task, split) reporting; the per-split report never blends tuning
  and held-out.
* Edge precision / recall and direction accuracy on relationship tasks.
* Unsupported-acceptance: numerator / denominator / rate. The
  denominator is the count of abstain-expected (negative) cases; the
  rate is `IsApplicable=true` only when the denominator is non-zero.
* Contradiction-miss: numerator / denominator / rate. The denominator is
  the count of contradicted cases; same null-when-zero rule.
* Failure and retry counts surfaced from `Prediction.ErrorMessage` and
  `Prediction.Attempts`.
* Gate eligibility flag with explicit reason when the corpus does not
  support a gate (e.g., a vocabulary label is missing).

Still missing:

* Subgroup bootstrap intervals. The protocol requires paired source-group
  bootstrap 95% lower bounds; the harness does not compute them yet.
* Cost / latency rollups when a live run supplies them.

### 5. Replay path

* Joins `fixtureId` to the authoritative fixture corpus. The replay
  stream does NOT carry `expectedLabel`, `task`, `split`, or
  `sourceGroup`; the runner looks those up from the corpus.
* Validates each observation against the shared `validate` package (same
  rules the live adapters enforce).
* Rejects unknown / duplicate / missing IDs under a declared coverage
  policy (`strict`, `allow-unknown`, `first-wins`).
* Supports repetitions via an explicit `runKey` field; multiple
  observations for the same `fixtureId` without a `runKey` are rejected
  as duplicates.
* Reports coverage against the corpus via `runner.ComputeCoverage` so
  reviewers can see which authoritative fixtures have any replay
  observation.

### 6. Fixture corpus

* 29 source-group IDs total: 20 base groups and 9 coverage fillers.
* All entities and bodies are fictional; no real-world facts.
* Per-source-group split assignment with field-level disjointness checks;
  semantic independence is not established.
* Adversarial / conflicting / irrelevant / numeric-trap / rename /
  missing-evidence perturbations attached to every group, with
  per-group perturbation text (TemplateFamily IDs are per-group for
  perturbation cases; base cases have empty TemplateFamily).
* All 10 entity-type labels, all 7 candidate-type labels, all 9
  relationship vocabulary entries, and all 3 claim-support verdicts are
  exercised by gold in held-out.

Still missing:

* ≥ 30 tuning cases per task. The pilot currently carries 12/14/16/12
  tuning cases per task; the protocol target is ≥ 30/task.
* ≥ 150 held-out negatives per negative-class gate. Held-out has 57
  relationship / 43 claim-support negative cases (protocol target
  ≥ 150 each).
* Human review / adjudication for all 332 fixtures.
* Numeric invariant automated checks (recorded as
  `NumericInvariants`, runtime check deferred to a future revision).

## Still-missing acceptance evidence

Concrete items the orchestrator must still produce before any
Jev-adoption decision can be made:

1. **Verified OpenRouter request pin.** The documented OpenRouter request
   example uses `typesafe/jev-1.13`; its response example resolves to
   `typesafe/jev-1.13-20260917`. Requestability of the dated ID is unverified.
   `jev-1.13.0` is a native TypeSafe identifier, not a verified OpenRouter ID.
   The harness already enforces that the response
   model equals the pinned model; it does not silently alias.
2. **Verified TypeSafe model pin and price.** The published $0.042/M
   input figure is documented but not yet confirmed for the resolved
   pin.
3. **Authorized budget and ledger.** The protocol proposes a $10 cap and
   $1 pilot sub-cap; neither is authorized. The harness neither enforces
   nor reserves a budget. Current paid-run authorization is $0.
4. **Approved / adjusted labels for every fixture.** Today every fixture
   is `unreviewed`. No metric from this harness is a "measured quality"
   result until human adjudication changes that.
5. **Held-out run with three repetitions per case and fixed seed.**
   Record first/subsequent requests separately; provider cold/warm state
   remains unknown unless independently established. None has been done.
6. **Subgroup bootstrap intervals.** Required by the protocol for
   paired-baseline comparisons.
7. **Cost / latency adoption gates.** The plan enumerates a 20%
   improvement requirement; the harness does not currently compute
   these.

## Expansion and adjudication plan

When the orchestrator is ready to expand the corpus and adjudicate
labels, the recommended sequence is:

1. **Author review pass.** A human reviewer reads each fixture and
   moves `reviewStatus` to `approved` or `adjusted`. Adjusted fixtures
   must populate the `Reviewer` field with a stable handle. Conflicting
   labels become `disputed` and either resolve into a new
   `insufficient-evidence` abstain or trigger a fixture revision.
2. **Add more relationship tuning cases.** Target ≥ 30 cases per task in
   the tuning split.
3. **Add held-out negatives.** Target ≥ 150 per negative-class gate in
   held-out.
4. **Numeric invariants.** Promote `NumericInvariants` from records
   into runtime checks: the harness should verify each span resolves
   to the expected value, and any mismatch becomes an
   `unsupported-acceptance` event.
5. **Held-out baseline.** First author a separate, independently grouped,
   human-adjudicated corpus; do not promote these 332 smoke fixtures into
   held-out evidence. Compare an appropriate current/deterministic baseline,
   inexpensive structured-output model and pinned Jev. The scorer reports per-task
   and per-(task, split) predictions, so this only requires another
   runner and a second invocation.
6. **Adapter live probe.** Run a small (5-case) live request batch
   against `/api/alpha/decisions` once paid authorization is in place.
   The transport validation already enforces the response shape; the
   probe's purpose is to confirm the pin resolves to the expected model
   string.
7. **Three-repetition held-out run.** Each held-out fixture is run three
   times with the seeded interleaving. The runner preserves raw offline
   runs and never labels them as "Jev results" — only after human review
   can a measurement be promoted into an adoption claim.
8. **Decision gate.** Compare Pinned-Jev against the second baseline
   using the protocol's paired bootstrap. If the lower bound is
   ≥ -0.02 on the quality axis and the protocol's other gates pass,
   document a concrete retirement proposal for the existing extraction
   / claim-checker code paths; otherwise retain the incumbent.

## Notes on review responses

The review artifact flagged ten distinct defects across P1–P4 in v0.1;
v0.2 addressed them (see `CHANGELOG.md`). The v0.3 re-review cataloged
N1–N4; N1 (stale docs) and N2 (report reproducibility) are addressed
in this commit. N3 (tuning/held-out sample size) and N4 (body-level
entity reuse) are documented limitations, not harness defects.

## Notes for reviewers

* The pilot corpus is marked `unreviewed` and may contain factual
  errors. Reviewers should treat each fixture's `expectedLabel` as a
  proposal, not a gold answer.
* Preserve the fixture's agent author provenance. Record a human reviewer
  separately when review actually occurs; do not replace authorship.
* The deterministic baseline abstains on relationship and claim-support
  tasks, and returns `PERSON` for candidate-type. That is the
  deliberately weak baseline, not a protocol requirement or a measured
  approximation of the existing implementation.
* All transport tests use `httptest`. The harness never opens a real
  network connection in this directory.
* `cmd/jev-candidategen` is a separate CLI from `cmd/jev-eval`; it
  exercises the candidate-generation path end-to-end and produces a
  separate report. The two reports must be cross-referenced before any
  adoption claim.

## Budget ledger (v0.3.2, new)

* Package `ledger` implements a persistent, crash-safe budget ledger.
* All monetary amounts stored as integer micro-units (1/1,000,000 base unit).
* Conservative reservations BEFORE each attempt (base + retries + discovery).
* Retains unknown billing after timeout/crash; same-run resume preserves cap.
* Fails closed for missing rates, missing token bounds, manifest/model drift.
* Single writer via file lock; atomic append+fsync for crash safety.
* Cost/usage never stripped/fabricated; reconcile actual vs reservation; halt on overrun.
* **Explicit zero output price (`RateOut = 0`) supported** — represents free output (Jev advertises free output). Omitted rate still rejected.
* Halt blocks NEW reservations/settlements but permits reconciliation of in-flight billed costs.

## Dry-run CLI (v0.3.2, new)

* Command `jev-dryrun` computes offline cost projection (ZERO network calls).
* Takes explicit unverified rates, token limits, retry assumptions as strings.
* Emits per-task/per-group reservations with retry ceiling + discovery cost.
* Maximum possible cost under plan + missing approvals list.
* Default authorized cap = $0 (proposed $10/$1 not authorized).
* Exit code 1 if plan exceeds cap.
* **Explicit `-repetitions` flag (default 3 per protocol)**.
* **Exact decimal parsing with conservative ceiling rounding** — parses rate strings directly without float64 intermediate; rejects negative/non-finite/overflow; never saturates positive cost down.
* Uses real SHA-256 for token bounds hash.

### Dry-run reproducible command

```bash
cd evaluations/jev
go run ./cmd/jev-dryrun \
  -fixtures fixtures.jsonl \
  -manifest fixtures.manifest.json \
  -model typesafe/jev-1.13 \
  -rate-in 0.042 \
  -rate-out 0.042 \
  -cap 10 \
  -repetitions 3 \
  -out dryrun-report.json
```

For free-output models (Jev advertises free output):

```bash
go run ./cmd/jev-dryrun \
  -fixtures fixtures.jsonl \
  -manifest fixtures.manifest.json \
  -model typesafe/jev-1.13 \
  -rate-in 0.042 \
  -rate-out 0 \
  -cap 10 \
  -repetitions 3 \
  -out dryrun-report.json
```

