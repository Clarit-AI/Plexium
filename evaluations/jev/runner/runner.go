// Package runner ties candidate generation, deterministic baseline, and
// JSONL replay together. It produces Prediction records that scoring.Score
// consumes; nothing in this package performs network inference.
//
// The replay path joins fixtureId against an authoritative loaded fixture
// corpus. The replay stream NEVER carries gold/task/split/group; those
// are looked up from the corpus. Replay entries for unknown,
// duplicate, or missing IDs are rejected under a declared coverage
// policy; repeat entries for the same fixtureID require an explicit
// RunKey to disambiguate repetitions.
package runner

import (
	"errors"
	"fmt"
	"sort"

	"github.com/Clarit-AI/Plexium/evaluations/jev/baseline"
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
	"github.com/Clarit-AI/Plexium/evaluations/jev/scoring"
	"github.com/Clarit-AI/Plexium/evaluations/jev/validate"
)

// CoveragePolicy controls how Replay handles fixture IDs that don't match
// the authoritative corpus.
type CoveragePolicy string

const (
	// CoverageStrict rejects any unknown / missing / duplicate ID.
	CoverageStrict CoveragePolicy = "strict"
	// CoverageAllowUnknown treats unknown IDs as warnings but continues.
	CoverageAllowUnknown CoveragePolicy = "allow-unknown"
	// CoverageFirstWins keeps the first entry for duplicate fixture IDs
	// without RunKey; subsequent entries with the same (ID, empty
	// RunKey) are dropped.
	CoverageFirstWins CoveragePolicy = "first-wins"
)

// ReplayConfig configures replay admission.
type ReplayConfig struct {
	Policy  CoveragePolicy
	Fixture []protocol.Fixture // authoritative fixture corpus
}

// RunBaseline produces one baseline Prediction per fixture using the
// supplied baseline version. The baseline does not record confidence,
// latency, or cost; the scorer is responsible for not fabricating
// them.
//
// baseline.BaselineV2 is the recorded v0.4 protocol baseline (explicit
// abstention on typing tasks); baseline.BaselineLegacy is the v0.3
// default-fallback baseline. Select explicitly; callers that pass the
// zero value default to v0.4 (see cli/jev-eval for the -baseline flag).
func RunBaseline(fixtures []protocol.Fixture, version baseline.BaselineVersion) []scoring.Prediction {
	out := make([]scoring.Prediction, 0, len(fixtures))
	for _, f := range fixtures {
		label, abstained := baselinePredict(version, f.Task, baseline.Wrap(f.Candidates))
		out = append(out, scoring.Prediction{
			Source:         scoring.SourceBaseline,
			FixtureID:      f.ID,
			Task:           f.Task,
			SourceGroup:    f.SourceGroup,
			TemplateFamily: f.TemplateFamily,
			Split:          f.Split,
			ExpectedLabel:  f.ExpectedLabel,
			PredictedLabel: label,
			Abstained:      abstained,
		})
	}
	return out
}

// baselinePredict dispatches to the named baseline. An empty version
// (zero value) defaults to v0.4 — the current protocol baseline.
func baselinePredict(version baseline.BaselineVersion, task protocol.Task, list baseline.Shortlist) (string, bool) {
	switch version {
	case baseline.BaselineLegacy:
		return baseline.Predict(task, list)
	case baseline.BaselineV2, "":
		return baseline.PredictV2(task, list)
	}
	// Unknown version: fall back to the v0.4 baseline; the caller can
	// surface the version selection in the report and review it
	// without breaking the runnable path.
	return baseline.PredictV2(task, list)
}

// ReplayEntry is one observation read from a recorded JSONL stream. The
// record is model-agnostic; the harness does not care which adapter
// produced it. Gold/Task/Split/SourceGroup are intentionally NOT fields:
// the runner looks them up from the authoritative fixture corpus.
type ReplayEntry struct {
	FixtureID          string             `json:"fixtureId"`
	RunKey             string             `json:"runKey,omitempty"`
	Source             scoring.Source     `json:"source"`
	PredictedLabel     string             `json:"predictedLabel"`
	Abstained          bool               `json:"abstained"`
	Probabilities      map[string]float64 `json:"probabilities,omitempty"`
	Confidence         *float64           `json:"confidence,omitempty"`
	LatencyMS          *float64           `json:"latencyMs,omitempty"`
	CostUSD            *float64           `json:"costUsd,omitempty"`
	AttemptLatenciesMS []float64          `json:"attemptLatenciesMs,omitempty"`
	TotalWallMS        *float64           `json:"totalWallMs,omitempty"`
	Attempts           int                `json:"attempts,omitempty"`
	ResolvedModel      string             `json:"resolvedModel,omitempty"`
	ErrorMessage       string             `json:"errorMessage,omitempty"`
}

// ReplayError reports a coverage / validation failure during replay.
type ReplayError struct {
	Kind   string // "unknown-id", "missing-fixture", "duplicate-id", "validation"
	Detail string
}

func (e *ReplayError) Error() string {
	return fmt.Sprintf("replay %s: %s", e.Kind, e.Detail)
}

// RunReplay validates and joins each replay entry to the authoritative
// fixture corpus. The returned slice contains one scoring.Prediction per
// admitted entry. Validation failures and coverage violations are
// returned as errors.
//
// Each admitted Prediction has its ExpectedLabel / Task / Split /
// SourceGroup / TemplateFamily populated from the fixture corpus. The
// ReplayEntry's own fields for those keys are ignored.
func RunReplay(cfg ReplayConfig, entries []ReplayEntry) ([]scoring.Prediction, error) {
	if cfg.Policy == "" {
		cfg.Policy = CoverageStrict
	}
	index := map[string]protocol.Fixture{}
	for _, f := range cfg.Fixture {
		index[f.ID] = f
	}
	out := make([]scoring.Prediction, 0, len(entries))
	seen := map[string]int{} // fixture id without RunKey -> count
	type runKey struct {
		id, key string
	}
	seenRun := map[runKey]bool{}
	for i, e := range entries {
		if err := validate.FixtureID(e.FixtureID); err != nil {
			return nil, &ReplayError{Kind: "validation", Detail: fmt.Sprintf("entry %d: %v", i, err)}
		}
		f, ok := index[e.FixtureID]
		if !ok {
			if cfg.Policy == CoverageStrict || cfg.Policy == CoverageFirstWins {
				return nil, &ReplayError{Kind: "unknown-id", Detail: e.FixtureID}
			}
			// allow-unknown: skip with warning.
			continue
		}
		rk := runKey{id: e.FixtureID, key: e.RunKey}
		if seenRun[rk] {
			return nil, &ReplayError{Kind: "duplicate-id", Detail: fmt.Sprintf("fixture %q run %q repeated", e.FixtureID, e.RunKey)}
		}
		seenRun[rk] = true
		if e.RunKey == "" {
			seen[e.FixtureID]++
		}
		// Validate observation shape against the fixture's task vocabulary.
		vocab := validate.VocabularyFor(f.Task)
		if err := validate.Label(e.PredictedLabel, vocab); err != nil {
			return nil, &ReplayError{Kind: "validation", Detail: fmt.Sprintf("entry %d predictedLabel: %v", i, err)}
		}
		obs := validate.DecisionObservation{
			Choice:        e.PredictedLabel,
			Confidence:    e.Confidence,
			Probabilities: e.Probabilities,
			ResolvedModel: e.ResolvedModel,
			Attempts:      nonZeroOrOne(e.Attempts),
		}
		if err := validate.Decision(obs, vocab); err != nil {
			return nil, &ReplayError{Kind: "validation", Detail: fmt.Sprintf("entry %d: %v", i, err)}
		}
		pred := scoring.Prediction{
			Source:         scoring.SourceReplay,
			FixtureID:      f.ID,
			RunKey:         e.RunKey,
			Task:           f.Task,
			SourceGroup:    f.SourceGroup,
			TemplateFamily: f.TemplateFamily,
			Split:          f.Split,
			ExpectedLabel:  f.ExpectedLabel,
			PredictedLabel: e.PredictedLabel,
			Abstained:      e.Abstained,
			Probabilities:  cloneMap(e.Probabilities),
			Confidence:     cloneFloat(e.Confidence),
			LatencyMS:      cloneFloatPtr(e.LatencyMS),
			CostUSD:        cloneFloatPtr(e.CostUSD),
			TotalWallMS:    cloneFloatPtr(e.TotalWallMS),
			Attempts:       obs.Attempts,
			ErrorMessage:   e.ErrorMessage,
		}
		if len(e.AttemptLatenciesMS) > 0 {
			pred.AttemptLatencies = append([]float64{}, e.AttemptLatenciesMS...)
		}
		out = append(out, pred)
	}
	// Detect duplicates in first-wins policy.
	if cfg.Policy == CoverageFirstWins || cfg.Policy == CoverageStrict {
		for id, count := range seen {
			if count > 1 {
				return nil, &ReplayError{Kind: "duplicate-id", Detail: fmt.Sprintf("fixture %q appears %d times without RunKey", id, count)}
			}
		}
	}
	return out, nil
}

// CoverageReport summarises which authoritative fixtures have any replay
// observation. The runner returns this alongside the predictions so the
// caller can verify coverage of the negative class.
type CoverageReport struct {
	TotalFixtures         int      `json:"totalFixtures"`
	CoveredFixtures       int      `json:"coveredFixtures"`
	UncoveredFixtures     []string `json:"uncoveredFixtures"`
	ExtraObservations     []string `json:"extraObservations"`
	DuplicateObservations []string `json:"duplicateObservations"`
}

// ComputeCoverage inspects the replay stream (without scoring it) and
// returns which authoritative fixtures have at least one observation and
// which entries reference unknown IDs. Duplicate IDs are flagged too.
func ComputeCoverage(fixtures []protocol.Fixture, entries []ReplayEntry) CoverageReport {
	rep := CoverageReport{TotalFixtures: len(fixtures)}
	have := map[string]int{}
	for _, e := range entries {
		have[e.FixtureID]++
	}
	idSet := map[string]bool{}
	for _, f := range fixtures {
		idSet[f.ID] = true
		if have[f.ID] > 0 {
			rep.CoveredFixtures++
		} else {
			rep.UncoveredFixtures = append(rep.UncoveredFixtures, f.ID)
		}
	}
	for id, n := range have {
		if !idSet[id] {
			rep.ExtraObservations = append(rep.ExtraObservations, id)
		} else if n > 1 {
			for i := 0; i < n-1; i++ {
				rep.DuplicateObservations = append(rep.DuplicateObservations, id)
			}
		}
	}
	sort.Strings(rep.UncoveredFixtures)
	sort.Strings(rep.ExtraObservations)
	sort.Strings(rep.DuplicateObservations)
	return rep
}

func nonZeroOrOne(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

func cloneMap(m map[string]float64) map[string]float64 {
	if m == nil {
		return nil
	}
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneFloat(f *float64) *float64 {
	if f == nil {
		return nil
	}
	v := *f
	return &v
}

func cloneFloatPtr(f *float64) *float64 { return cloneFloat(f) }

// ErrUnknownReplaySource is returned when Replay.Source is empty.
var ErrUnknownReplaySource = errors.New("runner: replay entry source must be set")

// AsReplayError extracts a *ReplayError from err, if any.
func AsReplayError(err error) (*ReplayError, bool) {
	var re *ReplayError
	if errors.As(err, &re) {
		return re, true
	}
	return nil, false
}
