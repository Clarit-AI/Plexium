package compare

// Independent adversarial probes for W3-R4 (report -> corpus provenance),
// preserved from the independent review of 9123bce and folded in as
// permanent regressions. Adapted only where the stronger mechanism makes a
// probe's "inconclusive" branch the correct outcome: a self-consistent
// foreign set is now rejected at Build outright (frozen-manifest identity
// root of trust), which is stronger than the relabel rejection the original
// probe characterized.
//
// (1) foreign/fabricated report content on a genuine corpus sidecar,
// (2) a genuinely consistent but fabricated set (revised gold, unchanged
//     count) whose digests are all self-consistent,
// (3) an inventory whose claimed hashes are fabricated vs the actual files,
// (4) Build over a foreign corpus then re-labeled as the frozen corpus.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/loader"
	"github.com/Clarit-AI/Plexium/evaluations/jev/pilot"
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
	"github.com/Clarit-AI/Plexium/evaluations/jev/scoring"
)

// TestReviewerForeignCorpusReportRejected is the preserved W3-R4
// reproduction from the original wiring review. It must PASS: foreign
// corpus digest claims must not build and verify as the frozen corpus.
func TestReviewerForeignCorpusReportRejected(t *testing.T) {
	d := t.TempDir()
	b := filepath.Join(d, "b.json")
	p := filepath.Join(d, "p.json")
	writeJSON(t, b, map[string]any{"fixtureFileSha256": "foreign-gold", "manifestSha256": "foreign-manifest", "baseline": scoring.Report{Source: scoring.SourceBaseline, ProtocolVersion: "0.4.0", FixtureCount: 24}})
	writeJSON(t, p, map[string]any{"fixtureFileSha256": "other-gold", "manifestSha256": "other-manifest", "tuningOnlyScores": map[string]any{"jev": scoring.Report{Source: "jev", ProtocolVersion: "0.4.0", FixtureCount: 24}, "nano": scoring.Report{Source: "nano", ProtocolVersion: "0.4.0", FixtureCount: 24}}})
	cfg := Config{FixturesPath: genuineFixtures, ManifestPath: genuineManifest, InventoryPath: genuineInventory, BaselineReport: b, LivePilotReport: p}
	got, err := Build(cfg)
	if err == nil && Verify(got, cfg) == nil {
		t.Fatal("foreign corpus reports build and verify as frozen corpus")
	}
	t.Logf("rejected: build=%v", err)
}

func genuineCfg(baselinePath, pilotPath string) Config {
	return Config{
		FixturesPath:    genuineFixtures,
		ManifestPath:    genuineManifest,
		InventoryPath:   genuineInventory,
		BaselineReport:  baselinePath,
		LivePilotReport: pilotPath,
	}
}

// fabricatedReports writes a baseline + pilot report pair whose content is
// invented ("built from unrelated runs"): same source/protocol/count
// superficial identity, arbitrary metrics, foreign provenance tags, and no
// embedded corpus provenance binding.
func fabricatedReports(t *testing.T, dir, protocolVersion string, count int) (baselinePath, pilotPath string) {
	t.Helper()
	mk := func(src scoring.Source) scoring.Report {
		return scoring.Report{
			Source:          src,
			ProtocolVersion: protocolVersion,
			FixtureCount:    count,
			BaselineVersion: "fabricated-foreign-baseline-v99",
			FailureCount:    99,
		}
	}
	baselinePath = filepath.Join(dir, "baseline.json")
	pilotPath = filepath.Join(dir, "pilot.json")
	writeJSON(t, baselinePath, map[string]any{
		"fixtureFileSha256": "foreign-gold",
		"manifestSha256":    "foreign-manifest",
		"baseline":          mk(scoring.SourceBaseline),
	})
	writeJSON(t, pilotPath, map[string]any{
		"fixtureFileSha256": "other-foreign-gold",
		"manifestSha256":    "other-foreign-manifest",
		"tuningOnlyScores": map[string]any{
			"jev":  mk(scoring.Source("jev")),
			"nano": mk(scoring.Source("nano")),
		},
	})
	return baselinePath, pilotPath
}

// buildFabricatedCorpusSet builds a loader-consistent fabricated corpus: the
// genuine 24 tuning fixtures with one gold label revised (unchanged count), a
// regenerated manifest, and a tooling-consistent request inventory — plus
// fabricated reports. Every digest in the set is self-consistent.
func buildFabricatedCorpusSet(t *testing.T) (Config, string, string) {
	t.Helper()
	dir := t.TempDir()

	raw, err := os.ReadFile(genuineFixtures)
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []protocol.Fixture
	var lines [][]byte
	for _, line := range splitLines(raw) {
		if len(line) == 0 {
			continue
		}
		var f protocol.Fixture
		if err := json.Unmarshal(line, &f); err != nil {
			t.Fatalf("decode genuine fixture: %v", err)
		}
		fixtures = append(fixtures, f)
	}
	// Revise the gold of exactly one fixture (within its vocabulary).
	revised := false
	for i := range fixtures {
		f := &fixtures[i]
		for _, allowed := range f.AllowedLabels {
			if allowed != f.ExpectedLabel {
				f.ExpectedLabel = allowed
				revised = true
				break
			}
		}
		if revised {
			break
		}
	}
	if !revised {
		t.Fatal("no fixture with a revisable gold label")
	}
	for i := range fixtures {
		b, err := json.Marshal(fixtures[i])
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, b)
	}
	fixturesPath := filepath.Join(dir, "fixtures.jsonl")
	if err := os.WriteFile(fixturesPath, joinLines(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := loader.BuildManifest(fixturesPath, time.Now())
	if err != nil {
		t.Fatalf("build fabricated manifest: %v", err)
	}
	manifestPath := filepath.Join(dir, "fixtures.manifest.json")
	if err := loader.WriteManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	inv, err := pilot.BuildInventory(fixturesPath, manifestPath, pilot.BuildConfig{
		JevRequestModel: "adv-jev-model", NanoRequestModel: "adv-nano-model", NanoProvider: "adv-provider",
	})
	if err != nil {
		t.Fatalf("build fabricated inventory: %v", err)
	}
	invBytes, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	invPath := filepath.Join(dir, "request-inventory.json")
	if err := os.WriteFile(invPath, invBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	baselinePath, pilotPath := fabricatedReports(t, dir, inv.Protocol, inv.Corpus.FixtureCount)
	return Config{
		FixturesPath:    fixturesPath,
		ManifestPath:    manifestPath,
		InventoryPath:   invPath,
		BaselineReport:  baselinePath,
		LivePilotReport: pilotPath,
	}, baselinePath, pilotPath
}

func splitLines(b []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			lines = append(lines, b[start:i])
			start = i + 1
		}
	}
	if start < len(b) {
		lines = append(lines, b[start:])
	}
	return lines
}

func joinLines(lines [][]byte) []byte {
	var out []byte
	for _, l := range lines {
		out = append(out, l...)
		out = append(out, '\n')
	}
	return out
}

// (1) Foreign/fabricated report content attached to a GENUINE corpus sidecar.
// Reports carry only source/protocol/count; their content is from unrelated
// runs. Must not Build/Verify.
func TestAdvW3R4a_FabricatedReportsOnGenuineCorpus(t *testing.T) {
	dir := t.TempDir()
	baselinePath, pilotPath := fabricatedReports(t, dir, "0.4.0", 24)
	cfg := genuineCfg(baselinePath, pilotPath)
	sidecar, err := Build(cfg)
	if err == nil {
		if vErr := Verify(sidecar, cfg); vErr == nil {
			t.Fatal("W3-R4 HOLE REPRODUCED (1): fabricated reports from unrelated runs accepted on the genuine frozen corpus; report->corpus provenance is unbound")
		}
	}
	t.Logf("rejected: build=%v", err)
}

// (2) A genuinely consistent but fabricated set (revised gold, unchanged
// count) built with the repo's own tooling — every digest self-consistent —
// must NOT verify.
func TestAdvW3R4b_FabricatedConsistentSetMustNotVerify(t *testing.T) {
	cfg, _, _ := buildFabricatedCorpusSet(t)
	sidecar, err := Build(cfg)
	if err != nil {
		t.Logf("fabricated set rejected at Build: %v", err)
		return
	}
	if vErr := Verify(sidecar, cfg); vErr == nil {
		t.Fatal("W3-R4 HOLE REPRODUCED (2): fabricated consistent set (revised gold, unchanged count, self-consistent digests, reports from unrelated runs) verified")
	}
	t.Logf("fabricated set rejected at Verify")
}

// (3) An inventory whose claimed hashes are fabricated vs the actual files
// must be rejected even when its self-hash is consistent.
func TestAdvW3R4c_FabricatedInventoryClaimsRejected(t *testing.T) {
	dir := t.TempDir()
	raw, err := os.ReadFile(genuineInventory)
	if err != nil {
		t.Fatal(err)
	}
	var inv pilot.Inventory
	if err := json.Unmarshal(raw, &inv); err != nil {
		t.Fatal(err)
	}
	inv.Corpus.FixtureFileSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	clone := inv
	clone.InventoryHash = ""
	canonical, err := json.Marshal(clone)
	if err != nil {
		t.Fatal(err)
	}
	inv.InventoryHash = hashBytes(canonical) // self-consistent lie
	invBytes, _ := json.MarshalIndent(inv, "", "  ")
	invPath := filepath.Join(dir, "request-inventory.json")
	if err := os.WriteFile(invPath, invBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	baselinePath, pilotPath := fabricatedReports(t, dir, "0.4.0", 24)
	cfg := genuineCfg(baselinePath, pilotPath)
	cfg.InventoryPath = invPath
	if sidecar, err := Build(cfg); err == nil {
		if vErr := Verify(sidecar, cfg); vErr == nil {
			t.Fatal("W3-R4 HOLE REPRODUCED (3): inventory with fabricated claimed hashes verified")
		}
	}
	t.Log("fabricated inventory claims rejected")
}

// (4) Build over a foreign corpus, then re-label it as the frozen corpus.
// Adapted from the preserved probe: with the frozen-manifest identity as the
// root of trust the foreign set is rejected at Build outright (stronger than
// the relabel rejection the original probe characterized); the relabel
// checks remain as defense in depth if a foreign sidecar somehow exists.
func TestAdvW3R4d_ForeignCorpusBuildThenRelabel(t *testing.T) {
	foreignCfg, _, _ := buildFabricatedCorpusSet(t)
	dir := t.TempDir()
	baselinePath, pilotPath := fabricatedReports(t, dir, "0.4.0", 24)
	genCfg := genuineCfg(baselinePath, pilotPath)

	foreign, err := Build(foreignCfg)
	if err != nil {
		// Expected outcome at this commit: the regenerated manifest cannot
		// match the frozen manifest identity (root of trust).
		t.Logf("foreign consistent set rejected at Build (root of trust): %v", err)
		return
	}
	if err := Verify(foreign, foreignCfg); err == nil {
		t.Log("NOTE: self-consistent foreign sidecar verifies against its own corpus (no external provenance root)")
	}
	// Relabel (a): swap the config for the frozen corpus.
	if err := Verify(foreign, genCfg); err == nil {
		t.Fatal("W3-R4 HOLE REPRODUCED (4a): foreign sidecar verified against the frozen corpus config")
	}
	// Relabel (b): rewrite the sidecar's corpus identity to the frozen one.
	genuine, err := Build(genCfg)
	if err != nil {
		t.Fatalf("genuine Build failed: %v", err)
	}
	relabeled := *foreign
	relabeled.Corpus = genuine.Corpus
	if err := Verify(&relabeled, genCfg); err == nil {
		t.Fatal("W3-R4 HOLE REPRODUCED (4b): re-labeled sidecar verified against the frozen corpus")
	}
	t.Log("both relabelings rejected")
}
