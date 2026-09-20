# KHA-579 evaluation harness changelog

## v0.3.4 — ledger/dryrun B1 + B2 correction pass

Supersedes v0.3.3 (commit `8f4b550`). The v0.3.3 ledger and dryrun code
remains in git history as commits `93742d2` and `8f4b550`; the prior
mismatch-evidence defect was unaddressed. Every existing v0.3 ledger
test continues to pass; the new regressions below fail to compile at
`8f4b550` because `EntryMismatch`, `ReservationTerminal`,
`ExpectedRateIn`, `ActualRateIn`, `ActualCost`, and `MismatchReason`
did not exist on the `Entry` type at that commit.

The v0.3.3 re-review artifact
(`/Users/bbrenner/.traycer/epics/1088c958-cb42-463c-8c23-d9e728f7a0aa/artifacts/jev-ledger-review/v0.3.3-rereview/index.md`)
cataloged B1, B2, C1–C3. The table below records the finding, the
resolution, and the test that demonstrates the fix.

### B1 — CRITICAL carryover: rate/token-mismatched billing had no durable evidence

| Finding | Resolution | Verification |
|---------|------------|--------------|
| Settle with rate/token mismatch returns an error string but writes nothing; later "valid" settle on the same reservation erases evidence; halt is not latched | `Settle` detects rate/token mismatch BEFORE returning the error, appends a durable `EntryMismatch` entry carrying the actual billed rate/tokens/cost, the reservation ID, and expected-vs-actual rates, marks the reservation terminal/disputed (`ReservationTerminal: true`), preserves `max(reservation, actual)` conservative exposure on the ledger, and latches a durable halt. Re-settle of the same reservation returns `LedgerCodeDuplicateSettle`. New `Reserve` on a halted ledger returns `LedgerCodeHalted`. Other already-in-flight reservations (made before halt) can still settle. `validateEntry` accepts the new `EntryMismatch` type. `validateDrift` is unchanged in this regard (mismatch entries are not init entries). | `ledger/mismatch_test.go::TestSettleRateMismatchPersistsEvidenceAndHalts`, `TestSettleTokenMismatchPersistsEvidenceAndHalts`. The mismatch test fails to compile at `8f4b550` because the `EntryMismatch` EntryType and the required Entry fields do not exist. |

### B2 — required: numeric rate/bounds binding

| Finding | Resolution | Verification |
|---------|------------|--------------|
| `validateDrift` only compared manifest hash strings; a caller who recomputed the hash over new values bypassed the binding | Init entry now persists `RateIn`, `RateOut` (with `RateOutPresent` marker), and numeric `TokenBoundsValue{MaxInputTokens, MaxOutputTokens}`. `validateDrift` compares these numeric values directly in addition to the manifest hashes. | `TestRateInDriftRejectedOnUsedLedger`, `TestRateOutDriftRejectedOnUsedLedger`, `TestRateOutPresenceDriftRejectedOnUsedLedger`, `TestNumericTokenBoundsDriftRejectedOnUsedLedger`, `TestEmptyLedgerRateInValidation`. |

### C1 — minor: dryrun tests were structural, not CLI-executed

| Finding | Resolution | Verification |
|---------|------------|--------------|
| The 26 dryrun tests called `analyzeFixtures` / `estimateCostMicro` / `MicroUnitFromBaseString` directly; none shelled out to `go run ./cmd/jev-dryrun` | Added `cmd/jev-dryrun/cli_integration_test.go` with `TestDryRunCLIIntegration` that builds the binary into a temp dir and exercises it via `os/exec` against the real `fixtures.jsonl`. Cases: free output (`-rate-out 0`), 332-fixture count, default `repetitions=3`, default `$0` cap → nonzero exit with `EXCEEDS CAP`, missing `-rate-in` → nonzero exit. | `TestDryRunCLIIntegration/free_output_accepted`, `/default_repetitions_3`, `/default_cap_zero_nonzero_exit`, `/missing_rate_in_nonzero_exit`. |

### C2 — minor: missing directory fsync after rename

| Finding | Resolution | Verification |
|---------|------------|--------------|
| `rewriteInitEntry` temp+rename was correct but the directory was not fsynced, so the rename itself was not durable across crashes | Added `syncDir` helper. `Open` calls it after the initial init write; `rewriteInitEntry` calls it after the rename. Both are fail-closed on sync error. | Covered indirectly by every `TestSettle*` and `TestHalted*` test, which exercise both paths. |

### C3 — cosmetic: README heading said "v0.3.2"

| Finding | Resolution | Verification |
|---------|------------|--------------|
| README heading said `Budget ledger (v0.3.2, new)` though this commit is v0.3.4 | README rewritten to describe v0.3.4 ledger and dryrun semantics; explicit section on mismatch-evidence persistence, numeric config binding, and directory fsync. | `git diff 8f4b550..HEAD -- README.md` shows new "Budget ledger (v0.3.4)" and "Dry-run CLI (v0.3.4)" sections. |

### Quality Gates (v0.3.4)

| Check | Result |
|-------|--------|
| `gofmt -l .` | Clean |
| `go vet ./...` | Clean |
| `go test -race -count=1 ./...` | 13 packages pass |
| B1 regression at `8f4b550` | Build fails: undefined `EntryMismatch`, `ReservationTerminal`, `ExpectedRateIn`, `ActualRateIn`, `ActualCost`, `MismatchReason` |
| B1 regression at HEAD | `TestSettleRateMismatchPersistsEvidenceAndHalts`, `TestSettleTokenMismatchPersistsEvidenceAndHalts` PASS |
| B2 regression at `8f4b550` | No numeric-binding rejection path exists |
| B2 regression at HEAD | `TestRateInDriftRejectedOnUsedLedger`, `TestRateOutDriftRejectedOnUsedLedger`, `TestRateOutPresenceDriftRejectedOnUsedLedger`, `TestNumericTokenBoundsDriftRejectedOnUsedLedger`, `TestEmptyLedgerRateInValidation` PASS |
| C1 regression at HEAD | `TestDryRunCLIIntegration` (4 subtests) PASS |

### Pre-existing accepted v0.3.3 fixes (preserved, not redone)

The v0.3.3 commit (`8f4b550`) accepted R1 (explicit zero rate-out), R2
(actual fixture count 332 not 110), R3 (real dryrun tests), M1 (atomic
rewriteInitEntry via temp+rename), M2 (real SHA-256 token bounds hash),
M3 (exact decimal parsing with conservative ceiling). These are
preserved as-is in v0.3.4; this commit only adds the B1/B2/C1/C2/C3
corrections on top of the v0.3.3 baseline. The `MicroUnitFromBaseString`
path remains the only monetary CLI parser; no `float64` monetary
arithmetic was reintroduced.

### Remaining scope (documented, not corrected in this commit)

* N3 (sample-size targets), N4 (body-level entity reuse), and the
  GateEligible-as-vocabulary-coverage caveat from v0.3 are unchanged.
* Discovery recall remains unmeasured against real extraction patterns;
  the `discovery` package's `entityRecall = 0` reflects honest reporting
  on bodies lacking markdown patterns, not a bug.
* Authorized cap remains $0 by default. The `-cap 10` and `-rate-out 0.042`
  examples in the README are unverified planning numbers, not grant of
  authorization.
* Live `jev-eval` and `jev-candidategen` paths remain unprobed against
  OpenRouter until paid authorization is in place.

---

## v0.3.3 — R1/R2/R3/M1/M2/M3 correction pass

Supersedes v0.3.2 (commit `93742d2`). Preserved as the parent of the
v0.3.4 B1/B2/C1/C2/C3 corrections. See the v0.3.3 review artifact for
the original R1–R3/M1–M3 list and probe results.

## v0.3.0 — focused v0.3 correction pass

Supersedes v0.2.0. The v0.2 corpus, manifest, eval-report, and reports
remain in git history as commit `dba725b`; they are NOT promoted to
measured-quality status. Every v0.2 measurement must be re-run against
the v0.3 corpus before any adoption decision.

The v0.3 re-review artifact
(`/Users/bbrenner/.traycer/epics/1088c958-cb42-463c-8c23-d9e728f7a0aa/artifacts/jev-harness-review/v0.3-rereview/index.md`)
cataloged N1–N4. The table below records the finding, the resolution,
and the test that demonstrates the fix.

### N1 — P2: README/CHANGELOG stale at v0.2

| Finding | Resolution | Verification |
|---------|------------|--------------|
| README claimed 425 fixtures / 32 groups / v0.2 corpus, omitted `candidate-type` task, `discovery` package, calibration tables, and smoke-test disclosure | README fully rewritten for v0.3: 332 fixtures / 29 groups / 4 tasks; explicit SMOKE TEST ONLY header; body-level entity reuse and shared perturbation syntax across splits disclosed; GateEligible = vocabulary coverage only; one contradicted case caveat | `git diff dba725b..HEAD -- README.md` shows full rewrite |
| CHANGELOG stopped at v0.2 | CHANGELOG extended with v0.3 section documenting N1–N4 resolutions | `git diff dba725b..HEAD -- CHANGELOG.md` shows new section |

### N2 — P2: Committed reports not byte-reproducible

| Finding | Resolution | Verification |
|---------|------------|--------------|
| `pilot/eval-report.json` omits `discovery` block (predates discovery wiring) | Legacy `pilot/` directory retained for reference; canonical reports now at root (`eval-report.json`, `candidategen-report.json`, `fixtures.manifest.json`). Discovery block included. | `go run ./cmd/jev-eval ...` produces root `eval-report.json` with discovery block |
| `missedEntities` ordering non-deterministic (map iteration) | `discovery.Run` now sorts `missedEntities` and `missedEdges` before serialization; all unordered report lists sorted (support labels, labels, etc.) | `scoring_test.go::TestReportDeterministicOrdering`, `discovery_test.go::TestRunDeterministicOutput` |
| Duplicate reports in root vs `pilot/` confusing | Root is canonical; `pilot/` marked legacy v0.2. README reproduction commands use root paths. | README commands updated |
| `generatedAt`, `generationNs`, timing fields differ on each run | Timing fields preserved in raw records; deterministic projection test compares semantic content excluding timing | `scoring_test.go::TestReportDeterministicProjection` |

### N3 — P3: Corpus below protocol tuning targets (documented limitation)

| Finding | Resolution |
|---------|------------|
| Tuning: 12/14/16/12 per task (protocol ≥ 30) | Documented in README "Still-missing acceptance evidence" and "Expansion plan". `GateEligible` correctly reflects vocabulary coverage only. `SplitGroupIndependence` doc comment explicitly disclaims statistical independence and unmet 150-negative bound. |
| Held-out: 57 relationship / 43 claim-support negatives (protocol ≥ 150) | Same disclosure. |
| Exactly one `contradicted` gold case | Disclosed in README scope header and pilot corpus section. |

### N4 — P3: Body-level entity reuse across splits (documented limitation)

| Finding | Resolution |
|---------|------------|
| Same fictional entities ("Vornholt Pass", "Greycloak Workshop", etc.) appear in tuning and held-out bodies | README "Scope disclosure — SMOKE TEST ONLY" explicitly states: body names and shared perturbation syntax cross nominal splits despite salted candidate-ID/name disjointness. Split check establishes field-level disjointness only. |

### v0.3 Structural Changes (R1–R3 from prior assignment)

| # | Change | Resolution |
|---|--------|------------|
| R1 | Candidate-typing separate task with `CandidateTypeLabels` (7 uppercase role tags) distinct from `DocumentTypeLabels` | `protocol.TaskCandidateType`, `protocol.CandidateTypeLabels`, propagated through protocol/loader/runner/baseline/scoring/validate/CLI. `TestPerfectPredictionAllTasksAllSplits` proves macro-F1=1.0 reachable on all 4 tasks and both splits. |
| R2 | Entity/alias disjointness enforced; template-family grouping respected | `loader.VerifySplitIndependence` checks normalized entity-name/alias, group-ID, and template-family disjointness with per-dimension flags. Corpus salt-suffixes all names. |
| R3 | Candidate exercise relabeled `supplied-pool-stress-test`; discovery baseline added | `candidateex.Kind = "supplied-pool-stress-test"` with explicit provenance. New `discovery` package: evidence-only regex baseline (heading/wikilink/definition), never reads fixture Candidates/ExpectedLabel, gold joined at scoring, reports `missedEntities`. |
| — | Calibration split: ChoiceConfidence vs MaxProbability tables never blended | `CalibrationReport.ChoiceConfidence` and `CalibrationReport.MaxProbability` separate populations; Brier from distributions only. `TestCalibrationSeparatesConfidenceFromMaxProbability`, `TestCalibrationMixedPresenceCounterexample`. |

### Quality Gates (v0.3)

| Check | Result |
|-------|--------|
| `gofmt -l .` | Clean |
| `go vet ./...` | Clean |
| `go test -race -count=1 ./...` | 11 packages pass |
| Byte-reproducible reports | Verified by running reproduction twice and comparing deterministic projections |

---

## v0.2.0 — review-driven correction pass (preserved from prior commit)

Supersedes v0.1.0. The v0.1 corpus, manifest, eval-report, and decisions
remain in git history as commit `5f4d8ab` (same tree as `672c893`); they
are NOT promoted to measured-quality status. Every v0.1 measurement must
be re-run against the v0.2 corpus before any adoption decision.

The review artifact (`/Users/bbrenner/.traycer/epics/1088c958-cb42-463c-8c23-d9e728f7a0aa/artifacts/jev-harness-review/index.md`)
cataloged 14 defects across P1–P4. The table below records the
finding, the resolution, and the test that demonstrates the fix.

### P1 — measurement-corrupting defects

| # | Finding | Resolution | Verification |
|---|---------|------------|--------------|
| 1 | Scorer panics on out-of-vocab labels (nil-map write in `Confusion["__outside__"]`) | `scoring.scoreTask` reserves the `__outside__` row and every vocabulary column before iterating predictions; both `expected` and `predicted` outside the closed vocab land there safely | `scoring_test.go::TestScoreDoesNotPanicOnOutsideVocabLabel` |
| 2 | Replay path trusts caller gold/task/split/group and skips validation | `runner.ReplayEntry` no longer carries `expectedLabel`, `task`, `split`, or `sourceGroup`; `runner.RunReplay` joins `fixtureId` to the authoritative fixture corpus and runs each observation through `validate.Decision`. New `runner.ReplayConfig.Policy` covers `strict`, `allow-unknown`, `first-wins` | `runner_test.go::TestRunReplayJoinsFixtureAndIgnoresCallerGold`, `TestRunReplayRejectsAdversarialGoldOverride`, `TestRunReplayRejectsVocabularyMismatch`, `TestRunReplayRejectsInvalidProbabilities`, `TestRunReplayRejectsBlankOrWhitespaceFixtureID`, `TestRunReplayRejectsDuplicateWithoutRunKey`, `TestRunReplayAllowsRepeatsWithExplicitRunKey`, `TestCoverageReportDetectsUncoveredNegativeClass`, `TestCoverageReportDetectsExtrasAndDuplicates`, `TestRunReplayMixedPositiveNegativeDenominator` |
| 3 | Contradiction-miss denominator = all predictions | `TaskReport.ContradictionMiss` is a `*Rate` with `Numerator`, `Denominator`, `IsApplicable`. Denominator counts contradicted cases only; errors on contradicted-expected cases increment the numerator | `scoring_test.go::TestContradictionMissDenominatorIsContradictedCasesOnly`, `TestErrorsCountAsFailuresAndMissesNotAbstentions` |
| 4 | Unsupported-acceptance denominator = all task predictions | `TaskReport.UnsupportedAccept` is a `*Rate` with `Numerator`, `Denominator`, `IsApplicable`. Denominator counts abstain-expected cases only; the prior 2/2 example now reports 2/2 (rate 1.0) instead of 2/4 (rate 0.5) | `scoring_test.go::TestUnsupportedAcceptDenominatorIsNegativeCasesOnly`, `TestRateNullWhenDenominatorZero`, `TestRunReplayMixedPositiveNegativeDenominator` |
| 5 | Macro-F1 ≥ 0.90 unreachable on incomplete corpus | `scoring.TaskReport` exposes `Labels`, `SupportedLabels`, `GateEligible`, `GateIneligibleReason`. When a vocabulary label has zero gold-observed cases, `MacroF1` is still computed but `GateEligible=false` with a reason string. The harness never silently passes the gate | `scoring_test.go::TestMacroF1GateIneligibleWhenLabelMissing`, `TestHandComputablePerfectClassifier` |
| 6 | Absent confidence recorded as 0.0 | `adapter.Decision.Confidence` is `*float64`; `validate.DecisionObservation.Confidence` is `*float64`. The scorer treats nil as absence (calibration block records `unscorableReason`). Same shape for `Probabilities` (nil = absent) | `adapter_test.go::TestSubmitDecisionsConfidenceAbsenceIsRecordedAsNil`, `TestSubmitDecisionsProbabilitiesOptional`; `validate_test.go::TestDecisionAcceptsAbsentOptionalFields`; `scoring_test.go::TestCalibrationUnscorableWhenConfidenceAndMaxAbsent` |
| 7 | Latency accounting loses attempts and backoff wall time | `adapter.Decision` and `adapter.ChatObservation` carry `AttemptLatencies []time.Duration` (per-attempt including body read) and `TotalLatency time.Duration` (including backoff and body reads). The harness never relabels these as provider cold/warm | `adapter_test.go::TestSubmitDecisionsHappyPath` checks `len(AttemptLatencies)==1` and `TotalLatency > 0` |

### P2 — corpus independence and leakage

| # | Finding | Resolution | Verification |
|---|---------|------------|--------------|
| 8 | "35 independent source groups" was nominal; real entities crossed splits and perturbation templates were byte-identical | `cmd/jev-corpus/main.go` regenerated from scratch with 32 distinct fictional source groups (all entity names, titles, bodies fictional). All bodies, names, and perturbation texts embed the source group's ID. The new corpus covers all 10 entity-type labels, all 9 relationship vocabulary entries, and all 3 claim-support verdicts. `loader.VerifySplitIndependence` now also enforces template-family independence across splits | The new manifest's `splitIndependence` block reports `independent: true` for every split; all perturbation template families are per-source-group |
| 9 | Fixture candidates were hand-listed gold pools; `candidate.Generate` was never invoked outside tests; candidate IDs leaked ranker role tags | `candidate.PoolEntry` carries an opaque `ID` field; the shortlist preserves it verbatim. The new `candidateex` package wraps `candidate.Generate` end-to-end and reports entity/edge recall. `cmd/jev-candidategen` is the CLI surface. Deliberate gold omission is supported via `RunOptions.DeliberateGoldOmit`. The fixture's own `CandidateGeneration` field documents whether the candidate list is supplied or generated | `candidateex_test.go::TestRunInvokesCandidateGenerate`, `TestRunDeliberateGoldOmitReportsMiss`, `TestRunTruncationFlagRecorded`. The pilot `candidate-report.json` records the actual recall numbers (entity 0.949, edge 0.880) |

### P3 — label validity

| # | Finding | Resolution | Verification |
|---|---------|------------|--------------|
| 10 | Reversed / wrong-direction gold (e.g., `OpenAI created-by GPT-3`) | `client.relationshipBase` and `client.relationshipReverse` produce distinct gold labels with explicit `EdgeSourceID` / `EdgeTargetID` reversal. Reverse direction is gold `no-supported-relationship` (a wrong edge, not a partial success). The `DirectionAccuracy` metric now counts only non-abstain pairs | `scoring_test.go::TestHandComputableWrongDirectionPredictionsAreConcreteErrors` |
| 11 | Conflicting-source gold misassigned as `contradicted` | Equal-authority contradictory evidence without declared precedence is gold `insufficient-evidence` (per protocol v0.1 rule). `client.claimSupportNoPrecedence` enforces this | The pilot manifest's `reviewStatusCount` shows all 425 fixtures unreviewed; the no-precedence template explicitly documents the rule |
| 12 | Entity-type golds conflated document type with subject type | Each source group declares `EntityType` (document-level) and per-entity `Type` (candidate-typing role tag). Two distinct fixtures exercise each: `entitytype-base` tests the document classification; `candidate-typing` tests the candidate role tag. The candidate-typing fixture supplies its own `AllowedLabels` vocabulary so a candidate role like `PERSON` is admissible there but rejected at the document-typing layer | The pilot manifest's `taskCounts` shows entity-type coverage of all 10 labels |
| 13 | Relationship fallback cases violated the protocol's source/target ID contract | `Fixture.Validate` requires `EdgeSourceID` and `EdgeTargetID` on every relationship fixture and rejects `EdgeSourceID == EdgeTargetID`. Reverse-direction fixtures populate `EdgeSourceID` and `EdgeTargetID` with the reversed entities | `scoring_test.go` and the corpus generator's `relationshipReverse()` enforce the field set |
| 14 | Reversed-direction cases were semantically ill-formed (direction unrepresented in label space) | Labels are still predicates (no direction bit), but the source/target ID reversal produces a different gold label (`no-supported-relationship` vs the forward predicate) so the scorer can detect the reversal as a wrong edge | `scoring_test.go::TestHandComputableWrongDirectionPredictionsAreConcreteErrors` |

### P4 — README / documentation

| Finding | Resolution |
|---------|------------|
| "Protocol forbids charging [generative discovery] cost" was inverted | README corrected to match protocol v0.1: "charge its entire cost and latency to every pipeline that needs it" |
| "Concurrency is bounded to 1 by `Config.HTTPClient`" — false mechanism | README no longer claims this. The runner declares caller responsibility: the harness leaves it to the caller to serialize adapter calls (the recommended pattern is one goroutine per call) |
| "Confidence-calibrated candidate ranker" listed as missing (not requested) | Removed from the complexity inventory's "still missing" list |
| Tuning counts (35 / 35 / 28) contradicted the table (30 / 18 / 50) | Removed; README now reports the v0.2 figures (48 / 36 / 72 tuning; 85 / 64 / 120 held-out) |
| `relationship` tuning was 18, not 35; tuning had no cases that met the target | README reports the v0.2 figure (36) and acknowledges expansion is still required for ≥ 30 per task in held-out |
| `eval-report.json` embedded the entire corpus (18k lines) | Replaced with `ManifestSummary` (counts only). Per-fixture predictions live in `predictions.jsonl` (replay input/output) when supplied |
| README claimed `boundary "concurrency is bounded to 1 by Config.HTTPClient"` | Removed; runner and adapters expose caller responsibility |

### Additional changes beyond the review catalog (v0.2)

* New `validate` package with a shared semantic validator used by both
  the live adapters and the JSONL replay path. The validator enforces
  label membership, finite probabilities, [0,1] range, sum ≈ 1, missing
  field semantics, and required fields.
* New `candidateex` package implementing the candidate-generation
  exercise required by directive #5. The package records honest
  recall including deliberate gold omissions and truncation flags.
* New `cmd/jev-candidategen` CLI for the candidate exercise.
* `cmd/jev-eval` now emits a compact report (no embedded fixture list)
  and supports an optional `-raw-predictions` flag for replay output.
* `loader.VerifySplitIndependence` widened to check template-family
  disjointness across splits.
* `protocol.Fixture` gained `TemplateFamily` and `EdgeSourceID` /
  `EdgeTargetID` fields with validation rules.
* `protocol.PredicateLabels` now includes `no-supported-relationship`
  and `insufficient-evidence` as explicit vocabulary entries (matching
  the protocol spec).
* `runner.RunReplay` returns `(*[]Prediction, error)` so coverage /
  validation failures fail closed.
* Scoring outputs per-task and per-(task, split) reports; the report
  never blends tuning and held-out.