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
	"time"

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
	)
	flag.Parse()

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

	baselinePreds := runner.RunBaseline(loaded.Fixtures)
	baselineReport := scoring.Score(loaded.Manifest.ProtocolVersion, scoring.SourceBaseline, baselinePreds)

	envelope := struct {
		ManifestSummary ManifestSummary       `json:"manifestSummary"`
		Drift           *loader.DriftReport   `json:"drift"`
		Baseline        scoring.Report        `json:"baseline"`
		Replay          *scoring.Report       `json:"replay,omitempty"`
		ReplayCoverage  *runner.CoverageReport `json:"replayCoverage,omitempty"`
	}{ManifestSummary: summarizeManifest(loaded.Manifest), Drift: loaded.Drift, Baseline: baselineReport}

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
