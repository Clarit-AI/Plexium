// Command jev-eval is the offline runner. It loads the JSONL fixture set
// and its companion manifest, runs the deterministic abstaining baseline,
// optionally replays a recorded JSONL stream, validates the offline scoring
// outputs, and writes a single JSON report.
//
// Nothing here performs live inference; the CLI refuses any invocation that
// would dial out unless explicitly opted-in via flags AND credentials. The
// safest invocation runs offline replay or the deterministic baseline only.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/baseline"
	"github.com/Clarit-AI/Plexium/evaluations/jev/discovery"
	"github.com/Clarit-AI/Plexium/evaluations/jev/loader"
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
	"github.com/Clarit-AI/Plexium/evaluations/jev/runner"
	"github.com/Clarit-AI/Plexium/evaluations/jev/scoring"
)

func main() {
	var (
		fixturesPath = flag.String("fixtures", "evaluations/jev/fixtures.jsonl", "path to the JSONL fixture file")
		manifestPath = flag.String("manifest", "evaluations/jev/fixtures.manifest.json", "path to the manifest JSON")
		replayPath   = flag.String("replay", "", "optional JSONL file of replay entries (offline model results)")
		replayPolicy = flag.String("replay-policy", "strict", "replay coverage policy: strict|allow-unknown|first-wins")
		outPath      = flag.String("out", "-", "report output path; - for stdout")
		rawPredsPath = flag.String("raw-predictions", "", "optional JSONL file for raw per-case predictions (offline replay only)")
		strict       = flag.Bool("strict", true, "exit non-zero if manifest drift or split independence fail")
		baselineName = flag.String("baseline", string(baseline.BaselineV2), "deterministic baseline version: "+string(baseline.BaselineV2)+" (v0.4 abstaining, default) | "+string(baseline.BaselineLegacy)+" (v0.3 default-fallback)")
	)
	flag.Parse()

	baselineVersion, err := parseBaselineVersion(*baselineName)
	if err != nil {
		die("baseline: %v", err)
	}

	loaded, err := loader.Load(*fixturesPath, *manifestPath)
	if err != nil {
		die("load: %v", err)
	}
	if err := loader.ValidateFixtures(loaded.Fixtures); err != nil {
		die("validate fixtures: %v", err)
	}
	indep, err := loader.VerifySplitIndependence(loaded.Fixtures)
	if err != nil {
		if *strict {
			die("split independence: %v", err)
		}
		fmt.Fprintf(os.Stderr, "warning: split independence: %v\n", err)
	}
	loaded.Manifest.SplitIndependences = indep

	baselinePreds := runner.RunBaseline(loaded.Fixtures, baselineVersion)
	baselineReport := scoring.Score(loaded.Manifest.ProtocolVersion, scoring.SourceBaseline, baselinePreds)
	baselineReport.BaselineVersion = string(baselineVersion)
	if *rawPredsPath != "" {
		if err := writeRawPredictions(*rawPredsPath, baselinePreds); err != nil {
			die("raw baseline predictions: %v", err)
		}
	}

	// Discovery baseline: evidence-only deterministic extraction from raw
	// source text. This never reads fixture Candidates/ExpectedLabel/GoldEntities.
	discoveryReport := runDiscoveryBaseline(loaded.Fixtures)

	envelope := struct {
		ManifestSummary ManifestSummary        `json:"manifestSummary"`
		Drift           *loader.DriftReport    `json:"drift"`
		Baseline        scoring.Report         `json:"baseline"`
		Discovery       *discovery.Report      `json:"discovery,omitempty"`
		Replay          *scoring.Report        `json:"replay,omitempty"`
		ReplayCoverage  *runner.CoverageReport `json:"replayCoverage,omitempty"`
	}{ManifestSummary: summarizeManifest(loaded.Manifest), Drift: loaded.Drift, Baseline: baselineReport, Discovery: discoveryReport}

	if *replayPath != "" {
		entries, err := readReplay(*replayPath)
		if err != nil {
			die("replay: %v", err)
		}
		cov := runner.ComputeCoverage(loaded.Fixtures, entries)
		envelope.ReplayCoverage = &cov
		pol, err := parseCoveragePolicy(*replayPolicy)
		if err != nil {
			die("replay-policy: %v", err)
		}
		preds, err := runner.RunReplay(runner.ReplayConfig{Policy: pol, Fixture: loaded.Fixtures}, entries)
		if err != nil {
			die("run replay: %v", err)
		}
		rep := scoring.Score(loaded.Manifest.ProtocolVersion, scoring.SourceReplay, preds)
		envelope.Replay = &rep
		if *rawPredsPath != "" {
			if err := writeRawPredictions(*rawPredsPath, preds); err != nil {
				die("raw predictions: %v", err)
			}
		}
	}

	if err := writeReport(*outPath, envelope); err != nil {
		die("write: %v", err)
	}
	if loaded.Drift != nil && *strict {
		os.Exit(2)
	}
	// Drift is always present in JSON so consumers can see drift=null
	// explicitly rather than absence.
	_ = envelope.Drift
}

// runDiscoveryBaseline builds discovery.Source entries from the fixtures'
// SourceGroup Body (if present in sourceRevision.Note or a similar field)
// and runs the evidence-only baseline. Since our synthetic corpus doesn't
// store the raw markdown body in the fixture, we reconstruct Sources from
// the first fixture per source group's Excerpts (which contain the body).
// Gold entities/edges are looked up from the fixture's ExpectedLabel and
// Candidates fields.
func runDiscoveryBaseline(fixtures []protocol.Fixture) *discovery.Report {
	// Group fixtures by source group
	groupBody := map[string]string{}
	groupGoldEntities := map[string][]string{}

	for _, f := range fixtures {
		if _, ok := groupBody[f.SourceGroup]; !ok {
			// Use the first excerpt's text as the body
			if len(f.Excerpts) > 0 {
				groupBody[f.SourceGroup] = f.Excerpts[0].Text
			}
		}
		// For candidate-type and entity-type fixtures, the expected label
		// is an entity type; for relationship/claim-support, we collect
		// gold entities from the candidates list.
		// The gold entities for discovery are the candidate titles.
		for _, c := range f.Candidates {
			groupGoldEntities[f.SourceGroup] = append(groupGoldEntities[f.SourceGroup], c.Title)
		}
	}
	// Deduplicate gold entities per group
	for g, ents := range groupGoldEntities {
		seen := map[string]bool{}
		uniq := []string{}
		for _, e := range ents {
			if !seen[e] {
				seen[e] = true
				uniq = append(uniq, e)
			}
		}
		groupGoldEntities[g] = uniq
	}

	var sources []discovery.Source
	var allGroups []string
	for g, body := range groupBody {
		sources = append(sources, discovery.Source{Group: g, Body: body})
		allGroups = append(allGroups, g)
	}

	goldFn := func(group string) []string {
		return groupGoldEntities[group]
	}
	return discovery.Run(sources, goldFn, nil, allGroups)
}

// ManifestSummary is a compact view of the manifest. The full fixture list
// lives in fixtures.jsonl; the report does not embed it.
type ManifestSummary struct {
	ProtocolVersion   string                            `json:"protocolVersion"`
	GeneratedAt       time.Time                         `json:"generatedAt"`
	FixtureFile       string                            `json:"fixtureFile"`
	FixtureCount      int                               `json:"fixtureCount"`
	SplitCounts       protocol.SplitCount               `json:"splitCounts"`
	TaskCounts        protocol.TaskCount                `json:"taskCounts"`
	SourceGroupCount  int                               `json:"sourceGroupCount"`
	SplitIndependence []protocol.SplitGroupIndependence `json:"splitIndependence"`
}

func summarizeManifest(m protocol.Manifest) ManifestSummary {
	return ManifestSummary{
		ProtocolVersion:   m.ProtocolVersion,
		GeneratedAt:       m.GeneratedAt,
		FixtureFile:       m.FixtureFile,
		FixtureCount:      m.FixtureCount,
		SplitCounts:       m.SplitCounts,
		TaskCounts:        m.TaskCounts,
		SourceGroupCount:  m.SourceGroupCount,
		SplitIndependence: m.SplitIndependences,
	}
}

func parseCoveragePolicy(s string) (runner.CoveragePolicy, error) {
	switch s {
	case "strict":
		return runner.CoverageStrict, nil
	case "allow-unknown":
		return runner.CoverageAllowUnknown, nil
	case "first-wins":
		return runner.CoverageFirstWins, nil
	}
	return "", fmt.Errorf("unknown coverage policy %q", s)
}

// parseBaselineVersion maps the -baseline flag value to a known
// baseline.BaselineVersion. An empty value defaults to v0.4
// (current protocol). Unknown values are rejected at the CLI
// boundary rather than silently falling back, so the runnable
// baseline selection is always recorded honestly on the report.
func parseBaselineVersion(s string) (baseline.BaselineVersion, error) {
	trimmed := strings.TrimSpace(s)
	switch trimmed {
	case "":
		return baseline.BaselineV2, nil
	case string(baseline.BaselineV2):
		return baseline.BaselineV2, nil
	case string(baseline.BaselineLegacy), "v0.3", "legacy":
		return baseline.BaselineLegacy, nil
	}
	return "", fmt.Errorf("unknown baseline version %q (use %q or %q)", s, baseline.BaselineV2, baseline.BaselineLegacy)
}

func readReplay(path string) ([]runner.ReplayEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var out []runner.ReplayEntry
	for {
		var e runner.ReplayEntry
		if err := dec.Decode(&e); err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return nil, err
		}
		out = append(out, e)
	}
}

func writeRawPredictions(path string, preds []scoring.Prediction) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, p := range preds {
		if err := enc.Encode(p); err != nil {
			return err
		}
	}
	return nil
}

func writeReport(path string, env any) error {
	enc := json.NewEncoder(indentWriter(path))
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

func indentWriter(path string) io.Writer {
	if path == "" || path == "-" {
		return os.Stdout
	}
	f, err := os.Create(path)
	if err != nil {
		die("create report: %v", err)
	}
	return f
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "jev-eval: "+format+"\n", args...)
	os.Exit(1)
}
