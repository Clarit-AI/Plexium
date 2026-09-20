package runner

import (
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
	"github.com/Clarit-AI/Plexium/evaluations/jev/scoring"
)

func sampleFixtures() []protocol.Fixture {
	return []protocol.Fixture{
		{
			ID: "f1", Task: protocol.TaskEntityType, SourceGroup: "g1", TemplateFamily: "t1", Split: protocol.SplitTuning, Author: "a", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "person", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskEntityType),
		},
		{
			ID: "f2", Task: protocol.TaskClaimSupport, SourceGroup: "g1", TemplateFamily: "t1", Split: protocol.SplitTuning, Author: "a", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
			Candidates: []protocol.Candidate{{ID: "cand-1", Title: "Alice"}},
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
	if preds[0].PredictedLabel != "document" {
		t.Errorf("expected document, got %q", preds[0].PredictedLabel)
	}
	if preds[1].PredictedLabel != "insufficient-evidence" {
		t.Errorf("expected insufficient-evidence, got %q", preds[1].PredictedLabel)
	}
	if !preds[1].Abstained {
		t.Errorf("claim-support baseline should abstain")
	}
}

func TestRunReplayJoinsFixtureAndIgnoresCallerGold(t *testing.T) {
	entries := []ReplayEntry{
		{
			FixtureID:      "f1",
			Source:         scoring.SourceReplay,
			PredictedLabel: "person",
			Abstained:      false,
			Attempts:       1,
		},
	}
	preds, err := RunReplay(ReplayConfig{Policy: CoverageStrict, Fixture: sampleFixtures()}, entries)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if preds[0].ExpectedLabel != "person" {
		t.Errorf("expected gold from fixture, got %q", preds[0].ExpectedLabel)
	}
	if preds[0].Task != protocol.TaskEntityType {
		t.Errorf("expected task from fixture, got %s", preds[0].Task)
	}
	if preds[0].TemplateFamily != "t1" {
		t.Errorf("expected template family from fixture, got %q", preds[0].TemplateFamily)
	}
}

func TestRunReplayRejectsAdversarialGoldOverride(t *testing.T) {
	// P1 finding #2: prior code trusted caller's expectedLabel/task/split.
	// Now replay must NOT carry those fields, and the prediction must use
	// the fixture corpus values. There is no way to override gold from a
	// ReplayEntry.
	e := ReplayEntry{
		FixtureID:      "f1",
		Source:         scoring.SourceReplay,
		PredictedLabel: "person",
		Abstained:      false,
		Attempts:       1,
	}
	preds, err := RunReplay(ReplayConfig{Policy: CoverageStrict, Fixture: sampleFixtures()}, []ReplayEntry{e})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if preds[0].ExpectedLabel != "person" {
		t.Fatalf("adversarial override of gold leaked through")
	}
}

func TestRunReplayRejectsUnknownIDUnderStrict(t *testing.T) {
	e := ReplayEntry{
		FixtureID:      "ghost",
		Source:         scoring.SourceReplay,
		PredictedLabel: "person",
		Attempts:       1,
	}
	_, err := RunReplay(ReplayConfig{Policy: CoverageStrict, Fixture: sampleFixtures()}, []ReplayEntry{e})
	if err == nil {
		t.Fatal("expected unknown-id error")
	}
	re, ok := AsReplayError(err)
	if !ok || re.Kind != "unknown-id" {
		t.Fatalf("expected unknown-id, got %v", err)
	}
}

func TestRunReplayRejectsVocabularyMismatch(t *testing.T) {
	e := ReplayEntry{
		FixtureID:      "f1",
		Source:         scoring.SourceReplay,
		PredictedLabel: "martian", // not in entity-type vocab
		Attempts:       1,
	}
	_, err := RunReplay(ReplayConfig{Policy: CoverageStrict, Fixture: sampleFixtures()}, []ReplayEntry{e})
	if err == nil {
		t.Fatal("expected validation error")
	}
	re, _ := AsReplayError(err)
	if re == nil || re.Kind != "validation" {
		t.Fatalf("expected validation kind, got %v", err)
	}
}

func TestRunReplayRejectsInvalidProbabilities(t *testing.T) {
	e := ReplayEntry{
		FixtureID:      "f2",
		Source:         scoring.SourceReplay,
		PredictedLabel: "supported",
		Probabilities:  map[string]float64{"supported": 4.0, "contradicted": 1.0}, // sum off and out-of-range
		Attempts:       1,
	}
	_, err := RunReplay(ReplayConfig{Policy: CoverageStrict, Fixture: sampleFixtures()}, []ReplayEntry{e})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRunReplayRejectsDuplicateWithoutRunKey(t *testing.T) {
	entries := []ReplayEntry{
		{FixtureID: "f1", Source: scoring.SourceReplay, PredictedLabel: "person", Attempts: 1},
		{FixtureID: "f1", Source: scoring.SourceReplay, PredictedLabel: "concept", Attempts: 1},
	}
	_, err := RunReplay(ReplayConfig{Policy: CoverageStrict, Fixture: sampleFixtures()}, entries)
	if err == nil {
		t.Fatal("expected duplicate-id error")
	}
	re, _ := AsReplayError(err)
	if re == nil || re.Kind != "duplicate-id" {
		t.Fatalf("expected duplicate-id, got %v", err)
	}
}

func TestRunReplayAllowsRepeatsWithExplicitRunKey(t *testing.T) {
	entries := []ReplayEntry{
		{FixtureID: "f1", Source: scoring.SourceReplay, PredictedLabel: "person", Attempts: 1, RunKey: "r1"},
		{FixtureID: "f1", Source: scoring.SourceReplay, PredictedLabel: "concept", Attempts: 1, RunKey: "r2"},
		{FixtureID: "f1", Source: scoring.SourceReplay, PredictedLabel: "person", Attempts: 1, RunKey: "r3"},
	}
	preds, err := RunReplay(ReplayConfig{Policy: CoverageStrict, Fixture: sampleFixtures()}, entries)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(preds) != 3 {
		t.Fatalf("expected 3 predictions, got %d", len(preds))
	}
	if preds[0].RunKey != "r1" || preds[1].RunKey != "r2" || preds[2].RunKey != "r3" {
		t.Fatalf("RunKey not propagated")
	}
}

func TestRunReplayRejectsBlankOrWhitespaceFixtureID(t *testing.T) {
	cases := []string{"", " ", "\t", "with space", "with\nnewline"}
	for _, id := range cases {
		_, err := RunReplay(ReplayConfig{Policy: CoverageStrict, Fixture: sampleFixtures()}, []ReplayEntry{
			{FixtureID: id, Source: scoring.SourceReplay, PredictedLabel: "person", Attempts: 1},
		})
		if err == nil {
			t.Errorf("expected error for fixture ID %q", id)
		}
	}
}

func TestCoverageReportDetectsUncoveredNegativeClass(t *testing.T) {
	rep := ComputeCoverage(sampleFixtures(), []ReplayEntry{
		{FixtureID: "f1", Source: scoring.SourceReplay, PredictedLabel: "person"},
	})
	if rep.TotalFixtures != 2 {
		t.Fatalf("expected 2 fixtures, got %d", rep.TotalFixtures)
	}
	if rep.CoveredFixtures != 1 {
		t.Fatalf("expected 1 covered, got %d", rep.CoveredFixtures)
	}
	if len(rep.UncoveredFixtures) != 1 || rep.UncoveredFixtures[0] != "f2" {
		t.Fatalf("expected f2 uncovered, got %v", rep.UncoveredFixtures)
	}
}

func TestCoverageReportDetectsExtrasAndDuplicates(t *testing.T) {
	rep := ComputeCoverage(sampleFixtures(), []ReplayEntry{
		{FixtureID: "f1", Source: scoring.SourceReplay, PredictedLabel: "person"},
		{FixtureID: "f1", Source: scoring.SourceReplay, PredictedLabel: "concept"},
		{FixtureID: "ghost", Source: scoring.SourceReplay, PredictedLabel: "person"},
	})
	if len(rep.ExtraObservations) != 1 || rep.ExtraObservations[0] != "ghost" {
		t.Fatalf("expected ghost extra, got %v", rep.ExtraObservations)
	}
	if len(rep.DuplicateObservations) != 1 {
		t.Fatalf("expected 1 duplicate, got %v", rep.DuplicateObservations)
	}
}

func TestRunReplayMixedPositiveNegativeDenominator(t *testing.T) {
	// 2 supported-expected cases, 1 unsupported-expected case, 1 contradicted-expected case.
	// The Prediction stream returned has labels that test both positive and
	// negative cases; the scorer uses the fixture's gold to compute the
	// negative-class denominators.
	fixtures := []protocol.Fixture{
		{ID: "p1", Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut, SourceGroup: "g", TemplateFamily: "t", ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport)},
		{ID: "p2", Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut, SourceGroup: "g", TemplateFamily: "t", ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport)},
		{ID: "n1", Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut, SourceGroup: "g", TemplateFamily: "t", ExpectedLabel: "insufficient-evidence", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport)},
		{ID: "c1", Task: protocol.TaskClaimSupport, Split: protocol.SplitHeldOut, SourceGroup: "g", TemplateFamily: "t", ExpectedLabel: "contradicted", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport)},
	}
	entries := []ReplayEntry{
		{FixtureID: "p1", Source: scoring.SourceReplay, PredictedLabel: "supported", Attempts: 1},
		{FixtureID: "p2", Source: scoring.SourceReplay, PredictedLabel: "supported", Attempts: 1},
		{FixtureID: "n1", Source: scoring.SourceReplay, PredictedLabel: "supported", Abstained: false, Attempts: 1}, // wrong: predicts concrete on a negative
		{FixtureID: "c1", Source: scoring.SourceReplay, PredictedLabel: "contradicted", Attempts: 1},
	}
	preds, err := RunReplay(ReplayConfig{Policy: CoverageStrict, Fixture: fixtures}, entries)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(preds) != 4 {
		t.Fatalf("expected 4 predictions, got %d", len(preds))
	}
	// Gold came from fixtures, not entries.
	for i, p := range preds {
		if p.ExpectedLabel != fixtures[i].ExpectedLabel {
			t.Errorf("gold override leaked: %d", i)
		}
	}
}
