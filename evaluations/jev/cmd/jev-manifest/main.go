// Command jev-manifest builds the fixture manifest for a JSONL fixture file.
// It computes per-fixture SHA-256 digests, the per-task and per-split counts,
// the review-status counts, the challenge category distribution, the split
// independence check, and writes a manifest JSON suitable for replay.
//
// Invoking this CLI rewrites the manifest on disk; this is the only way to
// regenerate hashes after a fixture change. Reviewers can use the
// loader.DriftReport to detect drift between the manifest and a later
// fixture change.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/loader"
)

func main() {
	var (
		fixturesPath = flag.String("fixtures", "evaluations/jev/fixtures.jsonl", "path to the JSONL fixture file")
		outPath      = flag.String("out", "evaluations/jev/fixtures.manifest.json", "output manifest path")
		genTime      = flag.String("generated-at", "", "override generation timestamp (RFC3339)")
	)
	flag.Parse()
	t := time.Now().UTC()
	if *genTime != "" {
		parsed, err := time.Parse(time.RFC3339, *genTime)
		if err != nil {
			die("parse generated-at: %v", err)
		}
		t = parsed
	}
	manifest, err := loader.BuildManifest(*fixturesPath, t)
	if err != nil {
		die("build manifest: %v", err)
	}
	if err := loader.WriteManifest(*outPath, manifest); err != nil {
		die("write manifest: %v", err)
	}
	fmt.Fprintf(os.Stderr, "wrote manifest with %d fixtures, file sha256=%s\n", manifest.FixtureCount, manifest.SHA256FixtureFile)
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "jev-manifest: "+format+"\n", args...)
	os.Exit(1)
}
