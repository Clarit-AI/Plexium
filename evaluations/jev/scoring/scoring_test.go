package scoring

import (
	"math"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

func TestScoreEmptyReturnsZeros(t *testing.T) {
	rep := Score(SourceBaseline, nil)
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
	rep := Score(SourceBaseline, preds)
	tr := rep.ByTask[protocol.TaskEntityType]
	if tr.FixtureCount != 2 {
		t.Fatalf("expected 2, got %d", tr.FixtureCount)
	}
	if tr.Confusion["person"]["document"] != 1 {
		t.Fatalf("expected 1 in person/document cell, got %v", tr.Confusion)
	}
	if math.Abs(tr.Accuracy-0.5) > 1e-9 {
		t.Fatalf("expected accuracy 0.5, got %v", tr.Accuracy)
	}
}

func TestScoreContradictionMissAndUnsupportedAcceptance(t *testing.T) {
	ci := 0.8
	preds := []Prediction{
		{
			Source: SourceDecisions, Task: protocol.TaskClaimSupport, SourceGroup: "g1", Split: protocol.SplitHeldOut,
			ExpectedLabel: "contradicted", PredictedLabel: "supported", Confidence: &ci,
		},
		{
			Source: SourceDecisions, Task: protocol.TaskRelationship, SourceGroup: "g1", Split: protocol.SplitHeldOut,
			ExpectedLabel: "insufficient-evidence", PredictedLabel: "depends-on", Abstained: false,
		},
	}
	rep := Score(SourceDecisions, preds)
	cs := rep.ByTask[protocol.TaskClaimSupport]
	if cs.ContradictionMiss != 1.0 {
		t.Fatalf("expected contradiction miss 1.0, got %v", cs.ContradictionMiss)
	}
	rel := rep.ByTask[protocol.TaskRelationship]
	if rel.UnsupportedAccept != 1.0 {
		t.Fatalf("expected unsupported accept 1.0, got %v", rel.UnsupportedAccept)
	}
}

func TestScoreFailureCount(t *testing.T) {
	preds := []Prediction{
		{Source: SourceDecisions, Task: protocol.TaskEntityType, ErrorMessage: "boom"},
		{Source: SourceDecisions, Task: protocol.TaskEntityType, ExpectedLabel: "person", PredictedLabel: "person"},
	}
	rep := Score(SourceDecisions, preds)
	if rep.FailureCount != 1 {
		t.Fatalf("expected 1 failure, got %d", rep.FailureCount)
	}
}

func TestScoreRetryCount(t *testing.T) {
	preds := []Prediction{
		{Source: SourceDecisions, Task: protocol.TaskEntityType, Attempts: 2, ExpectedLabel: "person", PredictedLabel: "person"},
		{Source: SourceDecisions, Task: protocol.TaskEntityType, Attempts: 1, ExpectedLabel: "person", PredictedLabel: "person"},
	}
	rep := Score(SourceDecisions, preds)
	if rep.RetryCount != 1 {
		t.Fatalf("expected 1 retry, got %d", rep.RetryCount)
	}
}

func TestScoreMacroF1WithThreeClasses(t *testing.T) {
	// All-correct predictions across three labels: macro F1 = 1.0
	preds := []Prediction{
		{Source: SourceDecisions, Task: protocol.TaskClaimSupport, SourceGroup: "g1", Split: protocol.SplitTuning,
			ExpectedLabel: "supported", PredictedLabel: "supported"},
		{Source: SourceDecisions, Task: protocol.TaskClaimSupport, SourceGroup: "g1", Split: protocol.SplitTuning,
			ExpectedLabel: "contradicted", PredictedLabel: "contradicted"},
		{Source: SourceDecisions, Task: protocol.TaskClaimSupport, SourceGroup: "g1", Split: protocol.SplitTuning,
			ExpectedLabel: "insufficient-evidence", PredictedLabel: "insufficient-evidence"},
	}
	rep := Score(SourceDecisions, preds)
	tr := rep.ByTask[protocol.TaskClaimSupport]
	if math.Abs(tr.MacroF1-1.0) > 1e-9 {
		t.Fatalf("expected macroF1 1.0, got %v", tr.MacroF1)
	}
}

func TestCalibrationReportBrierAndReliability(t *testing.T) {
	conf := 0.9
	preds := []Prediction{
		{
			Source: SourceDecisions, Task: protocol.TaskClaimSupport, SourceGroup: "g1", Split: protocol.SplitHeldOut,
			ExpectedLabel: "supported", PredictedLabel: "supported",
			Confidence: &conf,
			Probabilities: map[string]float64{
				"supported":             0.9,
				"contradicted":          0.05,
				"insufficient-evidence": 0.05,
			},
		},
	}
	rep := Score(SourceDecisions, preds)
	if rep.ConfidenceCalibration == nil {
		t.Fatal("expected calibration report")
	}
	if rep.ConfidenceCalibration.BrierScore >= 0.1 {
		t.Fatalf("expected small brier score, got %v", rep.ConfidenceCalibration.BrierScore)
	}
	if rep.ConfidenceCalibration.CoverageByTh["0.95"] != 0 {
		t.Fatalf("expected 0 coverage at 0.95, got %v", rep.ConfidenceCalibration.CoverageByTh["0.95"])
	}
}

func TestCalibrationAbsentWithoutProbabilities(t *testing.T) {
	preds := []Prediction{
		{Source: SourceBaseline, Task: protocol.TaskEntityType, SourceGroup: "g1", Split: protocol.SplitTuning,
			ExpectedLabel: "person", PredictedLabel: "document"},
	}
	rep := Score(SourceBaseline, preds)
	if rep.ConfidenceCalibration != nil {
		t.Fatalf("expected nil calibration when no probabilities recorded, got %v", rep.ConfidenceCalibration)
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
