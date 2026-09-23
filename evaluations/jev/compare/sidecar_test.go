package compare

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/scoring"
)

func TestSidecarBindsMatchingThreeArmReportsAndRejectsDigestMismatch(t *testing.T) {
	dir := t.TempDir()
	baselinePath := filepath.Join(dir, "baseline.json")
	pilotPath := filepath.Join(dir, "pilot.json")
	baseline := scoring.Report{Source: scoring.SourceBaseline, ProtocolVersion: "0.4.0", FixtureCount: 24}
	jev := scoring.Report{Source: scoring.Source("jev"), ProtocolVersion: "0.4.0", FixtureCount: 24}
	nano := scoring.Report{Source: scoring.Source("nano"), ProtocolVersion: "0.4.0", FixtureCount: 24}
	writeJSON(t, baselinePath, map[string]any{"baseline": baseline})
	writeJSON(t, pilotPath, map[string]any{"tuningOnlyScores": map[string]any{"jev": jev, "nano": nano}})
	cfg := Config{FixturesPath: "../review-pilot/fixtures.jsonl", ManifestPath: "../review-pilot/fixtures.manifest.json", InventoryPath: "../pilot/request-inventory.json", BaselineReport: baselinePath, LivePilotReport: pilotPath}
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
	writeJSON(t, pilotPath, map[string]any{"tuningOnlyScores": map[string]any{"jev": jev, "nano": scoring.Report{Source: scoring.Source("nano"), ProtocolVersion: "0.4.0", FixtureCount: 23}}})
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
