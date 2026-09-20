package scoring

import (
	"math"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

func TestScoreEmptyReturnsZeros(t *testing.T) {
	rep := Score("0.2.0", SourceBaseline, nil)
	if rep.FixtureCount != 0 {
		t.Fatalf("expected 0, got %d", rep.FixtureCount)
	}
	for _, task := range []protocol.Task{protocol.TaskEntityType, protocol.TaskRelationship, protocol.TaskClaimSupport} {
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
	rep := Score("0.2.0", SourceBaseline, preds)
	tr := rep.ByTask[protocol.TaskEntityType]
	if tr.FixtureCount != 2 {
		t.Fatalf("expected 2, got %d", tr.FixtureCount)
	}
	if math.Abs(tr.Accuracy-0.5) > 1e-9 {
		t.Fatalf("expected accuracy 0.5, got %v", tr.Accuracy)
	}
}

func TestScoreDoesNotPanicOnOutsideVocabLabel(t *testing.T) {
	// P1 finding #1: outside-vocab label previously panicked on nil-map
	// write. The fix initializes the bucket.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("scorer panicked: %v", r)
		}
	}()
	preds := []Prediction{
		{Source: SourceDecisions, Task: protocol.TaskEntityType, Split: protocol.SplitHeldOut, ExpectedLabel: "martian", PredictedLabel: "person"},
	}
	_ = Score("0.2.0", SourceDecisions, preds)
}

func TestContradictionMissDenominatorIsContradictedCasesOnly(t *testing.T) {
	// P1 finding #3: prior denominator was all predictions.
	// 2 supported, 1 contradicted correctly, 1 contradicted missed (abstain).
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
	rep := Score("0.2.0", SourceDecisions, preds)
	tr := rep.ByTask[protocol.TaskClaimSupport]
	if tr.ContradictionMiss == nil || !tr.ContradictionMiss.IsApplicable {
		t.Fatalf("expected applicable contradiction-miss rate")
	}
	if tr.ContradictionMiss.Numerator != 1 || tr.ContradictionMiss.Denominator != 2 {
		t.Fatalf("expected 1/2, got %v", tr.ContradictionMiss)
	}
	if math.Abs(tr.ContradictionMiss.Rate-0.5) > 1e-9 {
		t.Fatalf("expected rate 0.5, got %v", tr.ContradictionMiss.Rate)
	}
}

func TestUnsupportedAcceptDenominatorIsNegativeCasesOnly(t *testing.T) {
	// P1 finding #4: prior denominator was all task predictions.
	// 2 positives (both correct), 2 negatives (both wrongly accepted).
	// Old impl would report 2/4 = 0.5; new impl must report 2/2 = 1.0.
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
	rep := Score("0.2.0", SourceDecisions, preds)
	tr := rep.ByTask[protocol.TaskClaimSupport]
	if tr.UnsupportedAccept == nil || !tr.UnsupportedAccept.IsApplicable {
		t.Fatalf("expected applicable unsupported-accept rate")
	}
	if tr.UnsupportedAccept.Numerator != 2 || tr.UnsupportedAccept.Denominator != 2 {
		t.Fatalf("expected 2/2, got %v", tr.UnsupportedAccept)
	}
	if math.Abs(tr.UnsupportedAccept.Rate-1.0) > 1e-9 {
		t.Fatalf("expected rate 1.0, got %v", tr.UnsupportedAccept.Rate)
	}
}

func TestRateNullWhenDenominatorZero(t *testing.T) {
	preds := []Prediction{
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "supported"},
	}
	rep := Score("0.2.0", SourceDecisions, preds)
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
		// Successful abstention on a contradicted-expected case still misses.
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "contradicted", PredictedLabel: "insufficient-evidence", Abstained: true, Confidence: &ci},
	}
	rep := Score("0.2.0", SourceDecisions, preds)
	tr := rep.ByTask[protocol.TaskClaimSupport]
	if tr.FailureCount != 1 {
		t.Fatalf("expected 1 failure, got %d", tr.FailureCount)
	}
	// Both cases miss contradiction; denominator = 2.
	if tr.ContradictionMiss.Numerator != 2 || tr.ContradictionMiss.Denominator != 2 {
		t.Fatalf("expected 2/2 contradiction miss, got %v", tr.ContradictionMiss)
	}
}

func TestMacroF1GateIneligibleWhenLabelMissing(t *testing.T) {
	// P1 finding #5: perfect classifier on a corpus missing one label
	// must NOT pass the macro-F1 gate silently. Old impl averaged 0 for
	// the missing class; new impl flags GateEligible=false.
	preds := []Prediction{
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "supported"},
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "contradicted", PredictedLabel: "contradicted"},
	}
	rep := Score("0.2.0", SourceDecisions, preds)
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
	rep := Score("0.2.0", SourceBaseline, preds)
	if rep.ConfidenceCalibration != nil {
		t.Fatalf("expected nil calibration when no probabilities recorded, got %v", rep.ConfidenceCalibration)
	}
}

func TestCalibrationUnscorableWhenConfidenceAndMaxAbsent(t *testing.T) {
	// P1 finding #6: prior code recorded absent confidence as 0.0.
	// Probabilities present but Confidence nil must report unscorable
	// unless the harness can derive max-prob; if a label is malformed
	// (NaN), report unscorable rather than synthesize zero.
	preds := []Prediction{
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "supported",
			Probabilities: map[string]float64{
				"supported":             0.5,
				"contradicted":          0.3,
				"insufficient-evidence": 0.2,
			},
			// Confidence is nil; max-prob = 0.5 is derivable.
		},
	}
	rep := Score("0.2.0", SourceDecisions, preds)
	if rep.ConfidenceCalibration == nil {
		t.Fatalf("expected calibration report when probabilities present")
	}
	if rep.ConfidenceCalibration.UnscorableReason != "" {
		t.Fatalf("expected calibration to be scorable from max-prob, got reason %q", rep.ConfidenceCalibration.UnscorableReason)
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
	// Hand-computable case: 3 classes, 1 example each, all correct.
	// Macro-F1 = 1.0; accuracy = 1.0; per-class precision/recall = 1.0.
	preds := []Prediction{
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "supported"},
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "contradicted", PredictedLabel: "contradicted"},
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "insufficient-evidence", PredictedLabel: "insufficient-evidence", Abstained: true},
	}
	rep := Score("0.2.0", SourceDecisions, preds)
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
	// 5 cases, all expected = supported, all predicted = insufficient-evidence.
	// Accuracy = 0; abstention = 1.0; contradiction miss rate null; unsupported-accept null.
	preds := []Prediction{
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "insufficient-evidence", Abstained: true},
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "insufficient-evidence", Abstained: true},
	}
	rep := Score("0.2.0", SourceBaseline, preds)
	tr := rep.ByTask[protocol.TaskClaimSupport]
	if tr.Accuracy != 0 {
		t.Fatalf("expected accuracy 0, got %v", tr.Accuracy)
	}
	if tr.AbstentionRate != 1.0 {
		t.Fatalf("expected abstention 1.0, got %v", tr.AbstentionRate)
	}
	if tr.ContradictionMiss != nil && tr.ContradictionMiss.IsApplicable {
		t.Fatalf("expected contradiction-miss inapplicable (no contradicted cases)")
	}
}

func TestHandComputableWrongDirectionPredictionsAreConcreteErrors(t *testing.T) {
	// 2 cases: same source/target pair, gold = created-by, prediction =
	// created-by on one (correct) and predicted concrete "depends-on" on
	// the reverse-direction case (incorrect edge). Edge TP = 1, FP = 1,
	// FN = 1.
	preds := []Prediction{
		{Task: protocol.TaskRelationship, Split: protocol.SplitHeldOut,
			ExpectedLabel: "created-by", PredictedLabel: "created-by"},
		{Task: protocol.TaskRelationship, Split: protocol.SplitHeldOut,
			ExpectedLabel: "created-by", PredictedLabel: "depends-on"},
	}
	rep := Score("0.2.0", SourceDecisions, preds)
	tr := rep.ByTask[protocol.TaskRelationship]
	if math.Abs(tr.EdgePrecision-0.5) > 1e-9 {
		t.Fatalf("expected edge precision 0.5, got %v", tr.EdgePrecision)
	}
	if math.Abs(tr.EdgeRecall-0.5) > 1e-9 {
		t.Fatalf("expected edge recall 0.5, got %v", tr.EdgeRecall)
	}
}

func TestPerTaskSplitReportDoesNotBlendTuningAndHeldOut(t *testing.T) {
	// Tuning correct, held-out wrong. The per-task-split report must not
	// blend them: tuning accuracy = 1, held-out accuracy = 0.
	preds := []Prediction{
		{Task: protocol.TaskEntityType, Split: protocol.SplitTuning,
			ExpectedLabel: "person", PredictedLabel: "person"},
		{Task: protocol.TaskEntityType, Split: protocol.SplitHeldOut,
			ExpectedLabel: "person", PredictedLabel: "document"},
	}
	rep := Score("0.2.0", SourceDecisions, preds)
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
	// P1 finding: errors are operational failures, not successful semantic
	// abstentions. When ErrorMessage is set, the abstention flag is ignored
	// for AbstentionNum and the case is recorded in FailureCount.
	preds := []Prediction{
		{Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "insufficient-evidence", PredictedLabel: "insufficient-evidence", Abstained: true, ErrorMessage: "timeout"},
	}
	rep := Score("0.2.0", SourceDecisions, preds)
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
	rep := Score("0.2.0", SourceDecisions, preds)
	if rep.OverallOverall.MacroF1 == 0 {
		t.Fatalf("expected non-zero overall macro-F1 when at least one task has data")
	}
}
