package scoring

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

func TestScoreEmptyReturnsZeros(t *testing.T) {
	rep := Score("0.3.0", SourceBaseline, nil)
	if rep.FixtureCount != 0 {
		t.Fatalf("expected 0, got %d", rep.FixtureCount)
	}
	for _, task := range []protocol.Task{protocol.TaskEntityType, protocol.TaskCandidateType, protocol.TaskRelationship, protocol.TaskClaimSupport} {
		tr := rep.ByTask[task]
		if tr.FixtureCount != 0 {
			t.Fatalf("task %s should be empty", task)
		}
	}
}

func TestScoreBaselineEntityType(t *testing.T) {
	preds := []Prediction{
		{Source: SourceBaseline, Task: protocol.TaskEntityType, SourceGroup: "g1", Split: protocol.SplitTuning, ExpectedLabel: "person", PredictedLabel: "document"},
		{Source: SourceBaseline, Task: protocol.TaskEntityType, SourceGroup: "g1", Split: protocol.SplitTuning, ExpectedLabel: "document", PredictedLabel: "document"},
	}
	rep := Score("0.3.0", SourceBaseline, preds)
	tr := rep.ByTask[protocol.TaskEntityType]
	if tr.FixtureCount != 2 {
		t.Fatalf("expected 2, got %d", tr.FixtureCount)
	}
	if math.Abs(tr.Accuracy-0.5) > 1e-9 {
		t.Fatalf("expected accuracy 0.5, got %v", tr.Accuracy)
	}
}

func TestScoreDoesNotPanicOnOutsideVocabLabel(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("scorer panicked: %v", r)
		}
	}()
	preds := []Prediction{
		{Source: SourceDecisions, Task: protocol.TaskEntityType, Split: protocol.SplitHeldOut, ExpectedLabel: "martian", PredictedLabel: "person"},
	}
	_ = Score("0.3.0", SourceDecisions, preds)
}

func TestContradictionMissDenominatorIsContradictedCasesOnly(t *testing.T) {
	ci := 0.8
	preds := []Prediction{
		{Task: protocol.TaskClaimSupport, SourceGroup: "g1", Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "supported", Confidence: &ci},
		{Task: protocol.TaskClaimSupport, SourceGroup: "g1", Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "supported", Confidence: &ci},
		{Task: protocol.TaskClaimSupport, SourceGroup: "g1", Split: protocol.SplitHeldOut,
			ExpectedLabel: "contradicted", PredictedLabel: "contradicted", Confidence: &ci},
		{Task: protocol.TaskClaimSupport, SourceGroup: "g1", Split: protocol.SplitHeldOut,
			ExpectedLabel: "contradicted", PredictedLabel: "insufficient-evidence", Abstained: true, Confidence: &ci},
	}
	rep := Score("0.3.0", SourceDecisions, preds)
	tr := rep.ByTask[protocol.TaskClaimSupport]
	if tr.ContradictionMiss == nil || !tr.ContradictionMiss.IsApplicable {
		t.Fatalf("expected applicable contradiction-miss rate")
	}
	if tr.ContradictionMiss.Numerator != 1 || tr.ContradictionMiss.Denominator != 2 {
		t.Fatalf("expected 1/2, got %v", tr.ContradictionMiss)
	}
}

func TestUnsupportedAcceptDenominatorIsNegativeCasesOnly(t *testing.T) {
	preds := []Prediction{
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "supported"},
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "supported"},
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "insufficient-evidence", PredictedLabel: "supported"},
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "insufficient-evidence", PredictedLabel: "supported"},
	}
	rep := Score("0.3.0", SourceDecisions, preds)
	tr := rep.ByTask[protocol.TaskClaimSupport]
	if tr.UnsupportedAccept == nil || !tr.UnsupportedAccept.IsApplicable {
		t.Fatalf("expected applicable unsupported-accept rate")
	}
	if tr.UnsupportedAccept.Numerator != 2 || tr.UnsupportedAccept.Denominator != 2 {
		t.Fatalf("expected 2/2, got %v", tr.UnsupportedAccept)
	}
}

func TestRateNullWhenDenominatorZero(t *testing.T) {
	preds := []Prediction{
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "supported"},
	}
	rep := Score("0.3.0", SourceDecisions, preds)
	tr := rep.ByTask[protocol.TaskClaimSupport]
	if tr.ContradictionMiss == nil || tr.ContradictionMiss.IsApplicable {
		t.Fatalf("expected inapplicable contradiction-miss when no contradicted cases")
	}
	if tr.UnsupportedAccept == nil || tr.UnsupportedAccept.IsApplicable {
		t.Fatalf("expected inapplicable unsupported-accept when no negative cases")
	}
}

func TestErrorsCountAsFailuresAndMissesNotAbstentions(t *testing.T) {
	ci := 0.8
	preds := []Prediction{
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "contradicted", PredictedLabel: "supported", ErrorMessage: "timeout", Confidence: &ci},
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "contradicted", PredictedLabel: "insufficient-evidence", Abstained: true, Confidence: &ci},
	}
	rep := Score("0.3.0", SourceDecisions, preds)
	tr := rep.ByTask[protocol.TaskClaimSupport]
	if tr.FailureCount != 1 {
		t.Fatalf("expected 1 failure, got %d", tr.FailureCount)
	}
	if tr.ContradictionMiss.Numerator != 2 || tr.ContradictionMiss.Denominator != 2 {
		t.Fatalf("expected 2/2 contradiction miss, got %v", tr.ContradictionMiss)
	}
}

func TestMacroF1GateIneligibleWhenLabelMissing(t *testing.T) {
	preds := []Prediction{
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "supported"},
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "contradicted", PredictedLabel: "contradicted"},
	}
	rep := Score("0.3.0", SourceDecisions, preds)
	tr := rep.ByTask[protocol.TaskClaimSupport]
	if tr.GateEligible {
		t.Fatalf("expected gate-ineligible when insufficient-evidence is missing")
	}
	if tr.GateIneligibleReason == "" {
		t.Fatalf("expected explicit ineligibility reason")
	}
}

func TestCalibrationAbsentWithoutProbabilities(t *testing.T) {
	preds := []Prediction{
		{Source: SourceBaseline, Task: protocol.TaskEntityType, SourceGroup: "g1", Split: protocol.SplitTuning,
			ExpectedLabel: "person", PredictedLabel: "document"},
	}
	rep := Score("0.3.0", SourceBaseline, preds)
	if rep.ConfidenceCalibration != nil {
		t.Fatalf("expected nil calibration when no probabilities recorded, got %v", rep.ConfidenceCalibration)
	}
}

func TestCalibrationSeparatesConfidenceFromMaxProbability(t *testing.T) {
	// R-3 (re-review calibration): when a run carries both confidence
	// and probabilities, the two tables must be reported under distinct
	// keys with distinct sample populations. Coverage-by-threshold must
	// reflect each table's own sample count.
	ci := 0.5
	preds := []Prediction{
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "supported",
			Confidence:    &ci,
			Probabilities: map[string]float64{"supported": 0.5, "contradicted": 0.3, "insufficient-evidence": 0.2},
		},
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "contradicted", PredictedLabel: "supported",
			// No confidence, only probabilities.
			Probabilities: map[string]float64{"supported": 0.5, "contradicted": 0.3, "insufficient-evidence": 0.2},
		},
	}
	rep := Score("0.3.0", SourceDecisions, preds)
	if rep.ConfidenceCalibration == nil {
		t.Fatal("expected calibration report")
	}
	if rep.ConfidenceCalibration.ChoiceConfidence == nil {
		t.Fatal("expected ChoiceConfidence table")
	}
	if rep.ConfidenceCalibration.MaxProbability == nil {
		t.Fatal("expected MaxProbability table")
	}
	if rep.ConfidenceCalibration.ChoiceConfidence.Source != "model-confidence" {
		t.Errorf("expected ChoiceConfidence.Source=model-confidence, got %q", rep.ConfidenceCalibration.ChoiceConfidence.Source)
	}
	if rep.ConfidenceCalibration.MaxProbability.Source != "max-probability" {
		t.Errorf("expected MaxProbability.Source=max-probability, got %q", rep.ConfidenceCalibration.MaxProbability.Source)
	}
	if rep.ConfidenceCalibration.ChoiceConfidence.SampleCount != 1 {
		t.Errorf("ChoiceConfidence sample count should be 1, got %d", rep.ConfidenceCalibration.ChoiceConfidence.SampleCount)
	}
	if rep.ConfidenceCalibration.MaxProbability.SampleCount != 2 {
		t.Errorf("MaxProbability sample count should be 2, got %d", rep.ConfidenceCalibration.MaxProbability.SampleCount)
	}
	// Brier depends only on the distribution population (size 2 here).
	if rep.ConfidenceCalibration.BrierSampleCount != 2 {
		t.Errorf("Brier sample count should be 2 (probabilities only), got %d", rep.ConfidenceCalibration.BrierSampleCount)
	}
}

func TestCalibrationMixedPresenceCounterexample(t *testing.T) {
	// One prediction has only Confidence; one has only probabilities; one
	// has both; one has neither. The two tables must report distinct
	// populations and Brier must use only the probabilities-bearing
	// subset.
	ci := 0.7
	preds := []Prediction{
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "supported",
			Confidence: &ci,
		},
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "supported",
			Probabilities: map[string]float64{"supported": 0.6, "contradicted": 0.3, "insufficient-evidence": 0.1},
		},
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "contradicted", PredictedLabel: "supported",
			Confidence:    &ci,
			Probabilities: map[string]float64{"supported": 0.6, "contradicted": 0.3, "insufficient-evidence": 0.1},
		},
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "supported",
			// no confidence, no probabilities
		},
	}
	rep := Score("0.3.0", SourceDecisions, preds)
	if rep.ConfidenceCalibration == nil {
		t.Fatal("expected calibration report")
	}
	if rep.ConfidenceCalibration.ChoiceConfidence == nil || rep.ConfidenceCalibration.ChoiceConfidence.SampleCount != 2 {
		t.Fatalf("ChoiceConfidence should have sample count 2, got %+v", rep.ConfidenceCalibration.ChoiceConfidence)
	}
	if rep.ConfidenceCalibration.MaxProbability == nil || rep.ConfidenceCalibration.MaxProbability.SampleCount != 2 {
		t.Fatalf("MaxProbability should have sample count 2, got %+v", rep.ConfidenceCalibration.MaxProbability)
	}
	if rep.ConfidenceCalibration.BrierSampleCount != 2 {
		t.Fatalf("Brier sample count should be 2 (only the probabilities-bearing subset), got %d", rep.ConfidenceCalibration.BrierSampleCount)
	}
}

func TestFormatThresholdHandlesZero(t *testing.T) {
	if got := formatThreshold(0); got != "0" {
		t.Fatalf("expected 0, got %q", got)
	}
	if got := formatThreshold(0.95); got == "" {
		t.Fatal("expected non-empty")
	}
}

func TestHandComputablePerfectClassifier(t *testing.T) {
	preds := []Prediction{
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "supported"},
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "contradicted", PredictedLabel: "contradicted"},
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "insufficient-evidence", PredictedLabel: "insufficient-evidence", Abstained: true},
	}
	rep := Score("0.3.0", SourceDecisions, preds)
	tr := rep.ByTask[protocol.TaskClaimSupport]
	if math.Abs(tr.MacroF1-1.0) > 1e-9 {
		t.Fatalf("expected macroF1 1.0, got %v", tr.MacroF1)
	}
	if math.Abs(tr.Accuracy-1.0) > 1e-9 {
		t.Fatalf("expected accuracy 1.0, got %v", tr.Accuracy)
	}
	if !tr.GateEligible {
		t.Fatalf("expected gate-eligible when every vocabulary label is exercised")
	}
}

func TestHandComputableAlwaysAbstain(t *testing.T) {
	preds := []Prediction{
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "insufficient-evidence", Abstained: true},
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "insufficient-evidence", Abstained: true},
	}
	rep := Score("0.3.0", SourceBaseline, preds)
	tr := rep.ByTask[protocol.TaskClaimSupport]
	if tr.Accuracy != 0 {
		t.Fatalf("expected accuracy 0, got %v", tr.Accuracy)
	}
	if tr.AbstentionRate != 1.0 {
		t.Fatalf("expected abstention 1.0, got %v", tr.AbstentionRate)
	}
}

func TestHandComputableWrongDirectionPredictionsAreConcreteErrors(t *testing.T) {
	preds := []Prediction{
		{Task: protocol.TaskRelationship, Split: protocol.SplitHeldOut,
			ExpectedLabel: "created-by", PredictedLabel: "created-by"},
		{Task: protocol.TaskRelationship, Split: protocol.SplitHeldOut,
			ExpectedLabel: "created-by", PredictedLabel: "depends-on"},
	}
	rep := Score("0.3.0", SourceDecisions, preds)
	tr := rep.ByTask[protocol.TaskRelationship]
	if math.Abs(tr.EdgePrecision-0.5) > 1e-9 {
		t.Fatalf("expected edge precision 0.5, got %v", tr.EdgePrecision)
	}
	if math.Abs(tr.EdgeRecall-0.5) > 1e-9 {
		t.Fatalf("expected edge recall 0.5, got %v", tr.EdgeRecall)
	}
}

func TestPerTaskSplitReportDoesNotBlendTuningAndHeldOut(t *testing.T) {
	preds := []Prediction{
		{Task: protocol.TaskEntityType, Split: protocol.SplitTuning,
			ExpectedLabel: "person", PredictedLabel: "person"},
		{Task: protocol.TaskEntityType, Split: protocol.SplitHeldOut,
			ExpectedLabel: "person", PredictedLabel: "document"},
	}
	rep := Score("0.3.0", SourceDecisions, preds)
	tuning := rep.ByTaskSplit[protocol.TaskEntityType][protocol.SplitTuning]
	held := rep.ByTaskSplit[protocol.TaskEntityType][protocol.SplitHeldOut]
	if math.Abs(tuning.Accuracy-1.0) > 1e-9 {
		t.Fatalf("expected tuning accuracy 1, got %v", tuning.Accuracy)
	}
	if math.Abs(held.Accuracy-0.0) > 1e-9 {
		t.Fatalf("expected held-out accuracy 0, got %v", held.Accuracy)
	}
}

func TestErrorsNeverCountAsSuccessfulAbstentions(t *testing.T) {
	preds := []Prediction{
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "insufficient-evidence", PredictedLabel: "insufficient-evidence", Abstained: true, ErrorMessage: "timeout"},
	}
	rep := Score("0.3.0", SourceDecisions, preds)
	tr := rep.ByTask[protocol.TaskClaimSupport]
	if tr.AbstentionNum != 0 {
		t.Fatalf("error cases must not increment abstention numerator, got %d", tr.AbstentionNum)
	}
	if tr.FailureCount != 1 {
		t.Fatalf("expected failure count 1, got %d", tr.FailureCount)
	}
}

func TestRollupSkipsEmptyTasks(t *testing.T) {
	preds := []Prediction{
		{Task: protocol.TaskEntityType, Split: protocol.SplitHeldOut,
			ExpectedLabel: "person", PredictedLabel: "person"},
	}
	rep := Score("0.3.0", SourceDecisions, preds)
	if rep.OverallOverall.MacroF1 == 0 {
		t.Fatalf("expected non-zero overall macro-F1 when at least one task has data")
	}
}

// TestPerfectPredictionAllTasksAllSplits: a hand-computed "perfect"
// classifier run that exercises every label across the four tasks in
// every split must score macro-F1 = 1.0 with no fixture landing in
// __outside__. The same run on each split independently must also
// report perfect macro-F1 — proving the per-split views do not blend.
//
// R1 (re-review): this is the regression test that proves the v0.2
// vocabulary-conflation bug cannot recur under v0.3.
func TestPerfectPredictionAllTasksAllSplits(t *testing.T) {
	tasks := []protocol.Task{
		protocol.TaskEntityType,
		protocol.TaskCandidateType,
		protocol.TaskRelationship,
		protocol.TaskClaimSupport,
	}
	splits := []protocol.Split{protocol.SplitTuning, protocol.SplitHeldOut}
	var preds []Prediction
	for _, t := range tasks {
		vocab := protocol.AllowedLabelsFor(t)
		for _, l := range vocab {
			for _, s := range splits {
				preds = append(preds, Prediction{
					Task:           t,
					Split:          s,
					ExpectedLabel:  l,
					PredictedLabel: l,
				})
			}
		}
	}
	rep := Score("0.3.0", SourceDecisions, preds)
	for _, task := range tasks {
		tr := rep.ByTask[task]
		if tr.FixtureCount == 0 {
			t.Errorf("task %s has zero fixtures in perfect run", task)
		}
		// No fixture should have landed in __outside__ because every
		// expected label is in the closed vocabulary and every
		// predicted label equals its expected label.
		if outside, ok := tr.Confusion["__outside__"]; ok && len(outside) > 0 {
			for k, v := range outside {
				if v > 0 {
					t.Errorf("task %s has %d predictions landing in __outside__[%s]; v0.2 bug pattern", task, v, k)
				}
			}
		}
		if math.Abs(tr.MacroF1-1.0) > 1e-9 {
			t.Errorf("task %s perfect run must yield macro-F1=1.0, got %v", task, tr.MacroF1)
		}
		if !tr.GateEligible {
			t.Errorf("task %s perfect run with full vocabulary must be gate-eligible", task)
		}
	}
	// Per-split views must also report perfect macro-F1.
	for _, task := range tasks {
		for _, s := range splits {
			tr := rep.ByTaskSplit[task][s]
			if tr.FixtureCount == 0 {
				t.Errorf("task %s split %s has zero fixtures in perfect run", task, s)
			}
			if math.Abs(tr.MacroF1-1.0) > 1e-9 {
				t.Errorf("task %s split %s perfect run must yield macro-F1=1.0, got %v", task, s, tr.MacroF1)
			}
		}
	}
}

// TestR1VocabulariesAreDistinct: the document-typing vocabulary and
// the candidate-typing vocabulary share NO CONTENT labels (i.e. no
// label that asserts a real-world typing). Abstention labels
// (insufficient-evidence) intentionally overlap across both typing
// tasks per v0.4 user convention — they are explicit abstention,
// not a content assertion. The scorer must still refuse to treat
// content labels from one task as the same label space; abstention
// labels are stored separately in task-level reporting.
func TestR1VocabulariesAreDistinct(t *testing.T) {
	doc := protocol.AllowedLabelsFor(protocol.TaskEntityType)
	cand := protocol.AllowedLabelsFor(protocol.TaskCandidateType)
	// Abstention labels that intentionally overlap across typing tasks.
	abstain := map[string]struct{}{"insufficient-evidence": {}}
	docSet := map[string]struct{}{}
	for _, l := range doc {
		if _, ok := abstain[l]; ok {
			continue
		}
		docSet[l] = struct{}{}
	}
	for _, l := range cand {
		if _, ok := abstain[l]; ok {
			continue
		}
		if _, ok := docSet[l]; ok {
			t.Errorf("vocabulary leak: %q appears in both entity-type and candidate-type", l)
		}
	}
}

// TestReportDeterministicOrdering verifies that the report's unordered
// collections (SupportedLabels, Labels, etc.) are serialized in a
// stable order across repeated Score invocations. This addresses N2
// (re-review): committed reports must be byte-reproducible modulo
// timing fields.
func TestReportDeterministicOrdering(t *testing.T) {
	preds := []Prediction{
		{Task: protocol.TaskEntityType, Split: protocol.SplitHeldOut,
			ExpectedLabel: "person", PredictedLabel: "person"},
		{Task: protocol.TaskEntityType, Split: protocol.SplitHeldOut,
			ExpectedLabel: "document", PredictedLabel: "document"},
		{Task: protocol.TaskEntityType, Split: protocol.SplitHeldOut,
			ExpectedLabel: "tool", PredictedLabel: "tool"},
	}
	rep1 := Score("0.3.0", SourceBaseline, preds)
	rep2 := Score("0.3.0", SourceBaseline, preds)
	// Compare SupportedLabels ordering
	tr1 := rep1.ByTask[protocol.TaskEntityType]
	tr2 := rep2.ByTask[protocol.TaskEntityType]
	if len(tr1.SupportedLabels) != len(tr2.SupportedLabels) {
		t.Fatalf("SupportedLabels length mismatch: %d vs %d", len(tr1.SupportedLabels), len(tr2.SupportedLabels))
	}
	for i := range tr1.SupportedLabels {
		if tr1.SupportedLabels[i] != tr2.SupportedLabels[i] {
			t.Errorf("SupportedLabels[%d] order differs: %q vs %q", i, tr1.SupportedLabels[i], tr2.SupportedLabels[i])
		}
	}
	// Labels ordering (vocabulary)
	for i := range tr1.Labels {
		if tr1.Labels[i] != tr2.Labels[i] {
			t.Errorf("Labels[%d] order differs: %q vs %q", i, tr1.Labels[i], tr2.Labels[i])
		}
	}
}

// TestReportDeterministicProjection verifies that a deterministic
// projection of the report (excluding timing fields: generatedAt,
// generationNs, AttemptLatencies, TotalWallMS) is stable across
// repeated invocations. This addresses N2 (re-review): reports must
// be comparable modulo timing.
func TestReportDeterministicProjection(t *testing.T) {
	preds := []Prediction{
		{Task: protocol.TaskEntityType, Split: protocol.SplitHeldOut,
			ExpectedLabel: "person", PredictedLabel: "person"},
		{Task: protocol.TaskEntityType, Split: protocol.SplitHeldOut,
			ExpectedLabel: "document", PredictedLabel: "document"},
	}
	rep1 := Score("0.3.0", SourceBaseline, preds)
	rep2 := Score("0.3.0", SourceBaseline, preds)
	// Both should have identical semantic content.
	// We compare the JSON-serialized reports after stripping timing
	// fields. Since Score() doesn't include timing fields in the
	// baseline predictions, the reports should be byte-equal.
	b1, _ := json.Marshal(rep1)
	b2, _ := json.Marshal(rep2)
	if string(b1) != string(b2) {
		t.Fatalf("reports not byte-equal for deterministic baseline")
	}
}
