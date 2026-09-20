# Offline evaluation harness for KHA-579 (Jev), protocol v0.2

This directory contains the offline evaluation harness for Linear
[KHA-579](https://linear.app/khaentertainment/issue/KHA-579): a Go-based
harness that scores candidate-generation, deterministic-baseline, and
remote-model pipelines against a synthesized corpus of evaluation
fixtures.

The harness follows protocol **v0.2**, which supersedes the v0.1
implementation that was the subject of the parent review. The v0.2
changes are recorded in the `CHANGELOG.md` style below and apply across
all packages in this directory. **No v0.1 measurement remains valid;**
the v0.1 eval report was superseded and is not committed here. Any v0.1
result remains in git history for forensic reference only.

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
* Calibration block (multiclass Brier, 5-bin reliability, coverage at
  thresholds {0, 0.5, 0.7, 0.85, 0.95}) is populated only when
  probabilities are present. When probabilities are present without a
  recorded confidence, the calibration uses the max-prob as a proxy and
  flags the report with `unscorableReason` if neither is available.
* JSONL replay path joins fixtureId to the authoritative loaded corpus;
  the replay stream carries NO gold / task / split / sourceGroup fields,
  eliminating the prior code's adversarial override vector. Replay
  entries are validated against the same shared semantic validator the
  live adapters use, and rejected under a declared coverage policy
  (strict, allow-unknown, first-wins) with optional RunKey for
  repetitions.
* Runs an actual candidate-generation exercise (`jev-candidategen`) that
  invokes `candidate.Generate` end-to-end and reports entity / edge
  recall against the gold pool. The exercise supports deliberate
  gold-omission cases so a "perfect recall" claim cannot be gold-fed.
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
├── baseline/                  deterministic abstaining baseline
├── scoring/                   per-task metrics, calibration
├── validate/                  shared semantic validator (replay + adapter)
├── loader/                    JSONL + manifest read/write, drift detection
├── runner/                    baseline + replay runners
├── cmd/jev-corpus/            writes the pilot fixture JSONL
├── cmd/jev-manifest/          writes the manifest from a fixture JSONL
├── cmd/jev-eval/              runs the deterministic baseline + optional replay
├── cmd/jev-candidategen/      runs the candidate-generation exercise
└── pilot/
    ├── fixtures.jsonl         425 fixtures across 32 distinct synthetic source groups
    ├── fixtures.manifest.json manifest with file + per-fixture SHA-256
    ├── eval-report.json       baseline + (optional) replay report
    ├── predictions.jsonl      raw per-case predictions (replay input/output)
    └── candidate-report.json  candidate-generation exercise output
```

## Reproducible offline run

The reproducible command line:

```bash
cd evaluations/jev
gofmt -l .
go vet ./...
go test -race ./...
go run ./cmd/jev-corpus -out pilot/fixtures.jsonl
go run ./cmd/jev-manifest -fixtures pilot/fixtures.jsonl -out pilot/fixtures.manifest.json
go run ./cmd/jev-eval -fixtures pilot/fixtures.jsonl \
    -manifest pilot/fixtures.manifest.json \
    -out pilot/eval-report.json
go run ./cmd/jev-candidategen -fixtures pilot/fixtures.jsonl \
    -manifest pilot/fixtures.manifest.json \
    -deliberate-omit sg-001 \
    -out pilot/candidate-report.json
```

The eval CLI does not require any API keys and does not dial out. Live
inference is not in scope of the default invocation; future live runners
must add explicit `--live` and credentials flags.

## Pilot corpus (v0.2)

* 425 fixtures across 32 independent synthetic source groups (12 tuning,
  20 held-out, plus 9 coverage fillers targeting missing vocabulary
  labels in the held-out split).
* All entities, titles, and bodies are fictional. No real-world facts;
  no parametric-memory bleed.
* Each source group's IDs are unique (e.g., `sg-001`); each
  perturbation template's text embeds the group ID so the same
  perturbation family cannot accidentally be reused across groups.
* Template-family independence check (per protocol v0.2): the loader
  refuses to admit fixtures that share a non-empty TemplateFamily across
  different splits.

Per-split counts:

| Task | Tuning | Held-out | Total |
| --- | --- | --- | --- |
| entity-type | 48 | 85 | 133 |
| relationship | 36 | 64 | 100 |
| claim-support | 72 | 120 | 192 |
| **total** | **156** | **269** | **425** |

All 10 entity-type labels are exercised by gold; all 9 relationship
vocabulary entries (7 generic predicates + 2 abstain labels) are
exercised; all 3 claim-support verdicts are exercised. The protocol's
headline gates are computable on this corpus without gate-ineligibility
firing for "label not exercised".

## Candidate generation (v0.2)

* Pool entries carry an opaque ID (the source group's `LocalID`); the
  shortlist preserves the ID verbatim. Classifier inputs see only opaque
  tokens; ranker role tags (`title:N`, etc.) do not leak into candidate
  IDs.
* The candidate-generation exercise (`candidateex`) runs
  `candidate.Generate` end-to-end against curated fixtures and reports
  entity / edge recall against the gold pool. It supports deliberate
  gold-omission cases so recall cannot silently hit 1.0 due to gold
  feeding.
* Truncation flags are recorded on the shortlist and surfaced in the
  exercise report's `TruncatedFixtures` list.

Latest candidate-exercise summary (pilot, deliberately omitting
sg-001's gold entity):

| Metric | Value |
| --- | --- |
| Entity recall (overall) | 0.949 |
| Edge recall (overall) | 0.880 |
| Omitted gold fixtures | 9 (sg-001 group) |
| Truncated fixtures | 0 |

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
  candidate pool as ground truth for evaluation; the protocol permits an
  optional generative pass whose cost is charged to every pipeline that
  uses it (v0.2 corrected the v0.1 README inversion on this point).

Still missing:

* Confidence-calibrated candidate ranker.
* Empirical evidence for whether 12 / 24 caps are right at scale.
* Generative discovery pass (deliberately not in scope of v0.2; the
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

* 32 distinct fictional source groups plus 9 held-out coverage fillers.
* All entities and bodies are fictional; no real-world facts.
* Per-source-group split assignment so independence is enforceable.
* Adversarial / conflicting / irrelevant / numeric-trap / rename /
  missing-evidence perturbations attached to every group, with
  per-group perturbation text (TemplateFamily IDs are per-group for
  perturbation cases; base cases have empty TemplateFamily).
* All 10 entity-type labels, all 9 relationship vocabulary entries, and
  all 3 claim-support verdicts are exercised by gold.

Still missing:

* ≥ 30 tuning cases per task. The pilot currently carries 36 relationship
  tuning cases, 48 entity-type, 72 claim-support; the relationship
  tuning is right at the target.
* Human review / adjudication for all 425 fixtures.
* Numeric invariant automated checks (recorded as
  `NumericInvariants`, runtime check deferred to a future revision).

## Still-missing acceptance evidence

Concrete items the orchestrator must still produce before any
Jev-adoption decision can be made:

1. **Verified OpenRouter request pin.** Either `jev-1.13.0` or
   `typesafe/jev-1.13-20260917` must be confirmed requestable by an
   adapter round-trip. The harness already enforces that the response
   model equals the pinned model; it does not silently alias.
2. **Verified TypeSafe model pin and price.** The published $0.042/M
   input figure is documented but not yet confirmed for the resolved
   pin.
3. **Frozen budget and ledger.** The protocol defines a $10 cap and $1
   pilot sub-cap; the harness neither enforces nor reserves a budget.
4. **Approved / adjusted labels for every fixture.** Today every fixture
   is `unreviewed`. No metric from this harness is a "measured quality"
   result until human adjudication changes that.
5. **Hold-out run with three repetitions per case, fixed seed, and
   tracked cold/warm latency.** None has been done.
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
3. **Numeric invariants.** Promote `NumericInvariants` from records
   into runtime checks: the harness should verify each span resolves
   to the expected value, and any mismatch becomes an
   `unsupported-acceptance` event.
4. **Held-out baseline.** Stand up a non-trivial second baseline and
   run all 425 fixtures against it. The scorer already reports per-task
   and per-(task, split) predictions, so this only requires another
   runner and a second invocation.
5. **Adapter live probe.** Run a small (5-case) live request batch
   against `/api/alpha/decisions` once paid authorization is in place.
   The transport validation already enforces the response shape; the
   probe's purpose is to confirm the pin resolves to the expected model
   string.
6. **Three-repetition held-out run.** Each held-out fixture is run three
   times with the seeded interleaving. The runner preserves raw offline
   runs and never labels them as "Jev results" — only after human review
   can a measurement be promoted into an adoption claim.
7. **Decision gate.** Compare Pinned-Jev against the second baseline
   using the protocol's paired bootstrap. If the lower bound is
   ≥ -0.02 on the quality axis and the protocol's other gates pass,
   document a concrete retirement proposal for the existing extraction
   / claim-checker code paths; otherwise retain the incumbent.

## Notes on review responses

The review artifact flagged ten distinct defects across P1–P4. Each
finding is addressed in the v0.2 commit; the resolution list and the
specific test that demonstrates the fix is in `CHANGELOG.md`. v0.1
reports and the v0.1 corpus remain in git history for forensic reference
but are superseded.

## Notes for reviewers

* The pilot corpus is marked `unreviewed` and may contain factual
  errors. Reviewers should treat each fixture's `expectedLabel` as a
  proposal, not a gold answer.
* The fixture author field is `agent:KHA-579-pilot-author-v2`. Reviewers
  must replace this with a human handle only on approval.
* The deterministic baseline abstains on relationship and claim-support
  tasks. That is the protocol-mandated behaviour, not a bug.
* All transport tests use `httptest`. The harness never opens a real
  network connection in this directory.
* `cmd/jev-candidategen` is a separate CLI from `cmd/jev-eval`; it
  exercises the candidate-generation path end-to-end and produces a
  separate report. The two reports must be cross-referenced before any
  adoption claim.
