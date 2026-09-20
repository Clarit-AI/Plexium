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

	"github.com/Clarit-AI/Plexium/evaluations/jev/loader"
	"github.com/Clarit-AI/Plexium/evaluations/jev/runner"
	"github.com/Clarit-AI/Plexium/evaluations/jev/scoring"
)

func main() {
	var (
		fixturesPath = flag.String("fixtures", "evaluations/jev/fixtures.jsonl", "path to the JSONL fixture file")
		manifestPath = flag.String("manifest", "evaluations/jev/fixtures.manifest.json", "path to the manifest JSON")
		replayPath   = flag.String("replay", "", "optional JSONL file of replay entries (offline model results)")
		outPath      = flag.String("out", "-", "report output path; - for stdout")
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
	baselineReport := scoring.Score(scoring.SourceBaseline, baselinePreds)

	var replayReport *scoring.Report
	if *replayPath != "" {
		entries, err := readReplay(*replayPath)
		if err != nil {
			die("replay: %v", err)
		}
		preds := runner.RunReplay(entries)
		rep := scoring.Score(scoring.SourceReplay, preds)
		replayReport = &rep
	}

	envelope := struct {
		Manifest loader.Loaded       `json:"manifestSummary"`
		Drift    *loader.DriftReport `json:"drift,omitempty"`
		Baseline scoring.Report      `json:"baseline"`
		Replay   *scoring.Report     `json:"replay,omitempty"`
	}{Manifest: *loaded, Drift: loaded.Drift, Baseline: baselineReport, Replay: replayReport}
	_ = envelope.Manifest

	if err := writeReport(*outPath, envelope); err != nil {
		die("write: %v", err)
	}
	if loaded.Drift != nil && *strict {
		os.Exit(2)
	}
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
