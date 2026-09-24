package compare

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/baseline"
	"github.com/Clarit-AI/Plexium/evaluations/jev/scoring"
)

const (
	genuineFixtures  = "../review-pilot/fixtures.jsonl"
	genuineManifest  = "../review-pilot/fixtures.manifest.json"
	genuineInventory = "../pilot/request-inventory.json"
)

// genuineBoundReport builds a report the way an honest producer does: with
// the embedded corpus provenance and decision-2 billing basis bound to the
// frozen corpus.
func genuineBoundReport(t *testing.T, source scoring.Source, count int) BoundReport {
	t.Helper()
	prov, err := ProvenanceFromFiles(genuineFixtures, genuineManifest, genuineInventory)
	if err != nil {
		t.Fatal(err)
	}
	rep := scoring.Report{Source: source, ProtocolVersion: "0.4.0", FixtureCount: count}
	if source == scoring.SourceBaseline {
		rep.BaselineVersion = string(baseline.BaselineV2)
	}
	return BoundReport{Report: rep, CorpusProvenance: prov, BillingBasis: DefaultBillingBasis()}
}

func writeGenuineReports(t *testing.T, baselinePath, pilotPath string, mutate func(baseline, jev, nano *BoundReport)) {
	t.Helper()
	b := genuineBoundReport(t, scoring.SourceBaseline, 24)
	j := genuineBoundReport(t, scoring.Source("jev"), 24)
	n := genuineBoundReport(t, scoring.Source("nano"), 24)
	if mutate != nil {
		mutate(&b, &j, &n)
	}
	writeJSON(t, baselinePath, map[string]any{"baseline": b})
	writeJSON(t, pilotPath, map[string]any{"tuningOnlyScores": map[string]any{"jev": j, "nano": n}})
}

func TestSidecarBindsMatchingThreeArmReportsAndRejectsDigestMismatch(t *testing.T) {
	dir := t.TempDir()
	baselinePath := filepath.Join(dir, "baseline.json")
	pilotPath := filepath.Join(dir, "pilot.json")
	writeGenuineReports(t, baselinePath, pilotPath, nil)
	cfg := Config{FixturesPath: genuineFixtures, ManifestPath: genuineManifest, InventoryPath: genuineInventory, BaselineReport: baselinePath, LivePilotReport: pilotPath}
	sidecar, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(sidecar.Reports) != 3 || sidecar.ComparisonID == "" || sidecar.Corpus.InventorySHA == "" {
		t.Fatalf("incomplete sidecar: %+v", sidecar)
	}
	if err := Verify(sidecar, cfg); err != nil {
		t.Fatalf("matching sidecar rejected: %v", err)
	}
	tampered := *sidecar
	tampered.Corpus = sidecar.Corpus
	tampered.Corpus.ManifestSHA = "mismatch"
	if err := Verify(&tampered, cfg); err == nil {
		t.Fatal("mismatched corpus digest accepted")
	}
	writeGenuineReports(t, baselinePath, pilotPath, func(_, _, nano *BoundReport) {
		nano.FixtureCount = 23
	})
	if err := Verify(sidecar, cfg); err == nil {
		t.Fatal("mismatched report identity accepted")
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}
