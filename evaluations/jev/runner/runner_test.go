package runner

import (
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
	"github.com/Clarit-AI/Plexium/evaluations/jev/scoring"
)

func sampleFixtures() []protocol.Fixture {
	return []protocol.Fixture{
		{
			ID: "f1", Task: protocol.TaskEntityType, SourceGroup: "g1", Split: protocol.SplitTuning, Author: "a", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "person", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskEntityType),
		},
		{
			ID: "f2", Task: protocol.TaskClaimSupport, SourceGroup: "g1", Split: protocol.SplitTuning, Author: "a", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
			Candidates: []protocol.Candidate{{ID: "title:0", Title: "Alice"}},
		},
	}
}

func TestRunBaselineProducesOnePerFixture(t *testing.T) {
	preds := RunBaseline(sampleFixtures())
	if len(preds) != 2 {
		t.Fatalf("expected 2 predictions, got %d", len(preds))
	}
	if preds[0].Source != scoring.SourceBaseline {
		t.Errorf("expected source=baseline, got %s", preds[0].Source)
	}
	if preds[0].Confidence != nil {
		t.Errorf("baseline must not record confidence")
	}
	if preds[0].LatencyMS != nil {
		t.Errorf("baseline must not record latency")
	}
}

func TestRunBaselineDoesNotInventPredictions(t *testing.T) {
	preds := RunBaseline(sampleFixtures())
	// entity-type baseline always predicts "document".
	if preds[0].PredictedLabel != "document" {
		t.Errorf("expected document, got %q", preds[0].PredictedLabel)
	}
	// claim-support baseline always predicts "insufficient-evidence" and abstains.
	if preds[1].PredictedLabel != "insufficient-evidence" {
		t.Errorf("expected insufficient-evidence, got %q", preds[1].PredictedLabel)
	}
	if !preds[1].Abstained {
		t.Errorf("claim-support baseline should abstain")
	}
}

func TestRunReplayPreservesFields(t *testing.T) {
	conf := 0.8
	latency := 123.0
	cost := 0.001
	entries := []ReplayEntry{
		{
			FixtureID:      "f1",
			Source:         scoring.SourceReplay,
			PredictedLabel: "supported",
			Abstained:      false,
			ExpectedLabel:  "supported",
			Task:           protocol.TaskClaimSupport,
			SourceGroup:    "g1",
			Split:          protocol.SplitTuning,
			Confidence:     &conf,
			LatencyMS:      &latency,
			CostUSD:        &cost,
			Attempts:       2,
		},
	}
	preds := RunReplay(entries)
	if len(preds) != 1 {
		t.Fatalf("expected 1 prediction")
	}
	if preds[0].Confidence == nil || *preds[0].Confidence != 0.8 {
		t.Errorf("confidence not preserved")
	}
	if preds[0].LatencyMS == nil || *preds[0].LatencyMS != 123.0 {
		t.Errorf("latency not preserved")
	}
	if preds[0].CostUSD == nil || *preds[0].CostUSD != 0.001 {
		t.Errorf("cost not preserved")
	}
	if preds[0].Attempts != 2 {
		t.Errorf("attempts not preserved")
	}
}
