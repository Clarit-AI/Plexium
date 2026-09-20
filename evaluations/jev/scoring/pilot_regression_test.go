package scoring

import (
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

// TestPilotRegressionMatchesReviewProbe is a regression test that
// reproduces the review's probe numbers against the v0.2 scorer. If
// any future change to the scorer silently flips the denominators back
// to "all predictions", this test fails.
func TestPilotRegressionMatchesReviewProbe(t *testing.T) {
	// Probe setup from review finding #4: 2 negatives accepted + 2
	// correct positives. The prior scorer reported 0.5; v0.2 must report
	// 1.0 because the denominator is the negative case count (2).
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
		t.Fatalf("expected applicable rate")
	}
	if tr.UnsupportedAccept.Numerator != 2 || tr.UnsupportedAccept.Denominator != 2 {
		t.Fatalf("regression: expected 2/2 (negative denominator), got %d/%d",
			tr.UnsupportedAccept.Numerator, tr.UnsupportedAccept.Denominator)
	}
	if tr.UnsupportedAccept.Rate != 1.0 {
		t.Fatalf("regression: expected rate 1.0, got %v", tr.UnsupportedAccept.Rate)
	}
}

// TestPilotRegressionContradictionMatchesReviewProbe reproduces the
// review's probe for contradiction-miss on the v0.1 corpus: 70
// contradicted cases all missed. The v0.1 report read 0.4; v0.2 must
// report 70/70 = 1.0.
func TestPilotRegressionContradictionMatchesReviewProbe(t *testing.T) {
	preds := make([]Prediction, 70)
	for i := range preds {
		preds[i] = Prediction{
			Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut,
			ExpectedLabel: "contradicted", PredictedLabel: "insufficient-evidence", Abstained: true,
		}
	}
	rep := Score("0.2.0", SourceBaseline, preds)
	tr := rep.ByTask[protocol.TaskClaimSupport]
	if tr.ContradictionMiss == nil || !tr.ContradictionMiss.IsApplicable {
		t.Fatalf("expected applicable contradiction-miss rate")
	}
	if tr.ContradictionMiss.Numerator != 70 || tr.ContradictionMiss.Denominator != 70 {
		t.Fatalf("regression: expected 70/70, got %d/%d",
			tr.ContradictionMiss.Numerator, tr.ContradictionMiss.Denominator)
	}
	if tr.ContradictionMiss.Rate != 1.0 {
		t.Fatalf("regression: expected rate 1.0, got %v", tr.ContradictionMiss.Rate)
	}
}
