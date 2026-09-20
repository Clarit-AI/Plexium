# Offline evaluation harness for KHA-579 (Jev)

This directory contains the offline preparation for Linear
[KHA-579](https://linear.app/khaentertainment/issue/KHA-579): a Go-based
harness that scores a candidate-generation, deterministic-baseline, and
remote-model pipeline against a synthesized corpus of evaluation fixtures.

The harness follows protocol v0.1 declared in
[the parent artifact](../../../../../../../.traycer/epics/1088c958-cb42-463c-8c23-d9e728f7a0aa/artifacts/jev-evaluation/index.md).
It is owned by the Jev-evaluation agent only; the Plexium and MarkedUp
production code paths are not modified by anything in this directory.

## What this package does

* Reads a JSONL fixture set and its companion manifest, validating the
  fixture schema, source-group independence across splits, and SHA-256
  digests for both the file and each fixture record.
* Implements isolated typed HTTP adapters for OpenRouter Decisions
  (`/api/alpha/decisions`) and structured-output chat
  (`/api/v1/chat/completions`) with full transport validation: required
  fields, finite distributions, model-pin mismatch, payload size cap,
  timeouts, cancellation, and 401/402/403/429/5xx mapping.
* Runs a deterministic abstaining baseline per task with no model
  dependencies; baseline predictions never invent latency, cost, or
  confidence.
* Computes scoring per task: confusion matrix, macro-F1, macro-precision,
  macro-recall, accuracy, abstention rate, unsupported-acceptance rate,
  contradiction-miss rate, edge precision/recall, direction accuracy,
  candidate recall average, failure and retry counts.
* Computes calibration when probabilities are present: multiclass Brier
  score, five-bin reliability, and coverage at confidence thresholds
  `{0, 0.5, 0.7, 0.85, 0.95}`.
* Never performs network inference. Live runner paths are reachable but
  require explicit credentials and a non-empty live-mode flag; the
  default invocation runs offline only.

## What this package does not yet do

* No live paid runs. Adapter transport is fully covered by httptest
  suites, but the protocol forbids burning API calls until cost, retry
  budgets, and exact model pins are frozen. Any future live runner must
  be introduced with a separate dedicated CLI and explicit opt-in flags.
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
├── go.mod                     standalone module (no root go.mod touched)
├── protocol/                  fixture schema, vocabularies, manifest types
├── adapter/                   typed HTTP adapters for Decisions + chat
├── candidate/                 deterministic candidate generation
├── baseline/                  deterministic abstaining baseline
├── scoring/                   per-task metrics, calibration
├── loader/                    JSONL + manifest read/write, drift detection
├── runner/                    baseline + replay runners
├── cmd/jev-corpus/            writes the pilot fixture JSONL
├── cmd/jev-manifest/          writes the manifest from a fixture JSONL
├── cmd/jev-eval/              runs the deterministic baseline + optional replay
└── pilot/
    ├── fixtures.jsonl         342 agent-authored fixtures across 35 source groups
    ├── fixtures.manifest.json manifest with file + per-fixture SHA-256
    └── eval-report.json       deterministic-baseline report
```

## Reproducible offline run

The reproducible command line:

```bash
cd evaluations/jev
go test -race ./...
go run ./cmd/jev-eval -fixtures pilot/fixtures.jsonl \
    -manifest pilot/fixtures.manifest.json \
    -out pilot/eval-report.json
```

The eval CLI does not require any API keys and does not dial out. Live
inference is not in scope of the default invocation; future live runners
must add explicit `--live` and credentials flags.

## Pilot corpus (v0.1)

The pilot fixture JSONL has 342 fixtures across 35 independent
source groups (10 tuning, 25 held-out). Each source group contributes:

* one entity/document-type base case,
* one directed-relationship base case,
* one claim-support base case,
* a missing-evidence entity-type perturbation,
* a conflicting-source claim perturbation,
* an embedded-adversarial-instruction claim case,
* an irrelevant-context claim case,
* a misleading-numbers/dates claim case,
* a rename variant of the entity-type base.

Where the source group supplies at least one directed relation, an extra
reversed-direction case is appended. Reviewer status on every fixture is
`unreviewed`.

Per-split counts (current pilot):

| Task | Tuning | Held-out | Total |
| --- | --- | --- | --- |
| entity-type | 30 | 75 | 105 |
| relationship | 18 | 44 | 62 |
| claim-support | 50 | 125 | 175 |
| **total** | **98** | **244** | **342** |

Tuning relationship cases fall short of the protocol's per-task target
(30). The expansion plan below addresses this by adding two more
relationship cases per tuning group before the held-out run.

## Complexity inventory

This section enumerates the moving parts of the harness. Each entry says
what the harness does, what it does not do, and what evidence is still
missing before a future adoption claim.

### 1. Candidate generation

* Deterministic title / alias / wikilink match against evidence excerpts,
  capped at 12 entities and 24 directed edge candidates.
* Truncation is recorded on the `Shortlist` so the scorer can distinguish
  "missed because truncated" from "missed because no evidence".
* No generative candidate discovery. The harness accepts the supplied
  candidate pool as ground truth for evaluation; the protocol permits an
  optional generative pass but forbids charging its cost across all
  pipelines.

Still missing:

* Confidence-calibrated candidate ranker.
* Stable ID round-trip across multiple candidate-generation calls.
* Evidence for whether 12 / 24 caps are empirically right at scale.

### 2. Typed adapters

* Decisions and chat adapters share a typed `TransportError` taxonomy.
* Auth (401/403), usage overrun (402), schema/model-pin failures, timeouts,
  payload overflow, transport errors, and rate-limit/server retries are
  covered.
* Auth and schema errors do not retry; transient errors retry at most
  once, respecting Retry-After up to the run deadline.
* No state writes. Adapter holds no global state; concurrency is bounded
  to 1 by `Config.HTTPClient`.

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
when no probabilities are observed, and the latency / cost fields are
omitted from the report.

Still missing:

* A second non-trivial baseline (for example, a TF-IDF + predicate
  frequency heuristic) for the second-tier baseline the protocol
  specifies as the "ineligible strongest baseline" comparator.

### 4. Scoring

* Per-task confusion matrix indexed by vocabulary label.
* Macro-F1 / macro-precision / macro-recall across the closed vocabulary.
* Edge precision / recall and direction accuracy on relationship tasks.
* Unsupported-acceptance rate for relationships and claim-support.
* Contradiction-miss rate specific to claim-support.
* Failure and retry counts surfaced from `Prediction.ErrorMessage` and
  `Prediction.Attempts`.

Still missing:

* Subgroup bootstrap intervals. The protocol requires paired source-group
  bootstrap 95% lower bounds; the harness does not compute them yet.
* Cost / latency rollups when a live run supplies them.
* Latency / cost gates the protocol lists as adoption gates.

### 5. Fixture corpus

* 35 independent synthetic source groups covering paper, tool, person,
  event, project, place, organization, and concept document types.
* Per-source-group split assignment so independence is enforceable.
* Adversarial / conflicting / irrelevant / numeric-trap / rename /
  missing-evidence perturbations attached to every group.

Still missing:

* ≥ 30 tuning cases per task. The pilot currently carries:
  - 35 entity-type tuning cases,
  - 35 relationship tuning cases (one per tuning source group),
  - 28 claim-support tuning cases.
  Held-out cases exceed the per-task target.
* Human review / adjudication for all 342 fixtures.
* Numeric invariant automated checks. The harness records the expected
  spans; runtime verification of those spans lives in a future
  numeric-invariant checker.
* Documented second-tier baseline that the protocol requires for the
  adoption gate.

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
2. **Split tuning expansion.** Add 10 more source groups targeting
   underrepresented tasks (currently tuning has 28 claim-support cases;
   the protocol targets 30 per task).
3. **Numeric invariants.** Promote `NumericInvariants` from records
   into runtime checks: the harness should verify each span resolves
   to the expected value, and any mismatch becomes an
   `unsupported-acceptance` event.
4. **Held-out baseline.** Stand up a non-trivial second baseline
   (likely a TF-IDF + predicate prior) and run all 342 fixtures against
   it. The scorer already reports per-source predictions, so this only
   requires another runner and a second invocation.
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

## Notes for reviewers

* The pilot corpus is marked `unreviewed` and may contain factual
  errors. Reviewers should treat each fixture's `expectedLabel` as a
  proposal, not a gold answer.
* The fixture author field is `agent:KHA-579-pilot-author`. Reviewers
  must replace this with a human handle only on approval.
* The deterministic baseline abstains on relationship and claim-support
  tasks. That is the protocol-mandated behaviour, not a bug.
* All transport tests use `httptest`. The harness never opens a real
  network connection in this directory.
