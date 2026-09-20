// Package runner ties candidate generation, deterministic baseline, and
// JSONL replay together. It produces Prediction records that scoring.Score
// consumes; nothing in this package performs network inference.
package runner

import (
	"github.com/Clarit-AI/Plexium/evaluations/jev/baseline"
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
	"github.com/Clarit-AI/Plexium/evaluations/jev/scoring"
)

// RunBaseline produces one baseline Prediction per fixture. The baseline
// does not record confidence, latency, or cost; the scorer is responsible for
// not fabricating them.
func RunBaseline(fixtures []protocol.Fixture) []scoring.Prediction {
	out := make([]scoring.Prediction, 0, len(fixtures))
	for _, f := range fixtures {
		label, abstained := baseline.Predict(f.Task, baseline.Wrap(f.Candidates))
		out = append(out, scoring.Prediction{
			Source:         scoring.SourceBaseline,
			FixtureID:      f.ID,
			Task:           f.Task,
			SourceGroup:    f.SourceGroup,
			Split:          f.Split,
			ExpectedLabel:  f.ExpectedLabel,
			PredictedLabel: label,
			Abstained:      abstained,
		})
	}
	return out
}

// ReplayEntry is one observation read from a recorded JSONL stream. The
// record is deliberately model-agnostic; the replay runner does not care
// which adapter produced it.
type ReplayEntry struct {
	FixtureID      string             `json:"fixtureId"`
	Source         scoring.Source     `json:"source"`
	PredictedLabel string             `json:"predictedLabel"`
	Abstained      bool               `json:"abstained"`
	ExpectedLabel  string             `json:"expectedLabel"`
	Task           protocol.Task      `json:"task"`
	SourceGroup    string             `json:"sourceGroup"`
	Split          protocol.Split     `json:"split"`
	Probabilities  map[string]float64 `json:"probabilities,omitempty"`
	Confidence     *float64           `json:"confidence,omitempty"`
	LatencyMS      *float64           `json:"latencyMs,omitempty"`
	CostUSD        *float64           `json:"costUsd,omitempty"`
	Attempts       int                `json:"attempts,omitempty"`
	ErrorMessage   string             `json:"errorMessage,omitempty"`
}

// RunReplay converts a stream of ReplayEntry to scoring.Prediction.
func RunReplay(entries []ReplayEntry) []scoring.Prediction {
	out := make([]scoring.Prediction, 0, len(entries))
	for _, e := range entries {
		out = append(out, scoring.Prediction{
			Source:         e.Source,
			FixtureID:      e.FixtureID,
			Task:           e.Task,
			SourceGroup:    e.SourceGroup,
			Split:          e.Split,
			ExpectedLabel:  e.ExpectedLabel,
			PredictedLabel: e.PredictedLabel,
			Abstained:      e.Abstained,
			Probabilities:  e.Probabilities,
			Confidence:     e.Confidence,
			LatencyMS:      e.LatencyMS,
			CostUSD:        e.CostUSD,
			Attempts:       e.Attempts,
			ErrorMessage:   e.ErrorMessage,
		})
	}
	return out
}
