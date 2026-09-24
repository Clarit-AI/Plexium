package compare

// W3-R4 / decision-2 regressions: report->corpus provenance admission,
// fabricated BaselineVersion rejection, and the decision-2 report statements
// (rate semantics UNRECONCILED, liveContractsVerified not-true, RateOut
// explicit zero, MaxOutputTokens non-cost resource bound).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/baseline"
	"github.com/Clarit-AI/Plexium/evaluations/jev/loader"
	"github.com/Clarit-AI/Plexium/evaluations/jev/pilot"
	"github.com/Clarit-AI/Plexium/evaluations/jev/scoring"
)

// TestGenuineEndToEndBuildVerifyPasses: the honest production shape (reports
// with embedded provenance matching the frozen corpus) must Build and Verify.
func TestGenuineEndToEndBuildVerifyPasses(t *testing.T) {
	dir := t.TempDir()
	baselinePath := filepath.Join(dir, "baseline.json")
	pilotPath := filepath.Join(dir, "pilot.json")
	writeGenuineReports(t, baselinePath, pilotPath, nil)
	cfg := genuineCfg(baselinePath, pilotPath)
	sidecar, err := Build(cfg)
	if err != nil {
		t.Fatalf("genuine end-to-end Build failed: %v", err)
	}
	if err := Verify(sidecar, cfg); err != nil {
		t.Fatalf("genuine end-to-end Verify failed: %v", err)
	}
}

// TestFabricatedBaselineVersionRejected: a fabricated BaselineVersion must
// not bind to the claimed v0.4 comparison.
func TestFabricatedBaselineVersionRejected(t *testing.T) {
	for _, version := range []string{"fabricated-foreign-baseline-v99", ""} {
		t.Run("baseline-version-"+version, func(t *testing.T) {
			dir := t.TempDir()
			baselinePath := filepath.Join(dir, "baseline.json")
			pilotPath := filepath.Join(dir, "pilot.json")
			writeGenuineReports(t, baselinePath, pilotPath, func(b, _, _ *BoundReport) {
				b.BaselineVersion = version
			})
			cfg := genuineCfg(baselinePath, pilotPath)
			if sidecar, err := Build(cfg); err == nil {
				if vErr := Verify(sidecar, cfg); vErr == nil {
					t.Fatalf("W3-R4 HOLE: fabricated baselineVersion %q built and verified", version)
				}
			}
		})
	}
	// And a live-arm report must not claim a baseline version at all.
	dir := t.TempDir()
	baselinePath := filepath.Join(dir, "baseline.json")
	pilotPath := filepath.Join(dir, "pilot.json")
	writeGenuineReports(t, baselinePath, pilotPath, func(_, j, _ *BoundReport) {
		j.BaselineVersion = string(baseline.BaselineV2)
	})
	cfg := genuineCfg(baselinePath, pilotPath)
	if sidecar, err := Build(cfg); err == nil {
		if vErr := Verify(sidecar, cfg); vErr == nil {
			t.Fatal("W3-R4 HOLE: jev report claiming a baseline version built and verified")
		}
	}
}

// TestReportProvenanceIsMandatory: a report without the embedded corpus
// digests + fixture identity set + frozen execution-manifest SHA is never
// admitted, even on the genuine corpus.
func TestReportProvenanceIsMandatory(t *testing.T) {
	dir := t.TempDir()
	baselinePath := filepath.Join(dir, "baseline.json")
	pilotPath := filepath.Join(dir, "pilot.json")
	b := scoring.Report{Source: scoring.SourceBaseline, ProtocolVersion: "0.4.0", FixtureCount: 24, BaselineVersion: string(baseline.BaselineV2)}
	j := scoring.Report{Source: scoring.Source("jev"), ProtocolVersion: "0.4.0", FixtureCount: 24}
	n := scoring.Report{Source: scoring.Source("nano"), ProtocolVersion: "0.4.0", FixtureCount: 24}
	writeJSON(t, baselinePath, map[string]any{"baseline": b})
	writeJSON(t, pilotPath, map[string]any{"tuningOnlyScores": map[string]any{"jev": j, "nano": n}})
	cfg := genuineCfg(baselinePath, pilotPath)
	if sidecar, err := Build(cfg); err == nil {
		if vErr := Verify(sidecar, cfg); vErr == nil {
			t.Fatal("W3-R4 HOLE: provenance-less reports admitted")
		}
	}
}

// TestForgedConsistentFabricatedSetRejectedByFrozenManifestIdentity is the
// hard bar: a fabricated set that forges its embedded provenance to be
// self-consistent with its regenerated corpus must still be rejected — the
// regenerated manifest SHA cannot match the frozen manifest identity (the
// root of trust).
func TestForgedConsistentFabricatedSetRejectedByFrozenManifestIdentity(t *testing.T) {
	cfg, baselinePath, pilotPath := buildFabricatedCorpusSet(t)
	prov, err := ProvenanceFromFiles(cfg.FixturesPath, cfg.ManifestPath, cfg.InventoryPath)
	if err != nil {
		t.Fatal(err)
	}
	// Forge fully self-consistent embedded provenance for the fabricated corpus.
	forge := func(b, j, n *BoundReport) {
		for _, r := range []*BoundReport{b, j, n} {
			r.CorpusProvenance = prov
		}
	}
	invBytes, err := os.ReadFile(cfg.InventoryPath)
	if err != nil {
		t.Fatal(err)
	}
	var inv pilot.Inventory
	if err := json.Unmarshal(invBytes, &inv); err != nil {
		t.Fatal(err)
	}
	mk := func(src scoring.Source) BoundReport {
		rep := scoring.Report{Source: src, ProtocolVersion: inv.Protocol, FixtureCount: inv.Corpus.FixtureCount}
		if src == scoring.SourceBaseline {
			rep.BaselineVersion = string(baseline.BaselineV2)
		}
		return BoundReport{Report: rep, CorpusProvenance: prov, BillingBasis: DefaultBillingBasis()}
	}
	b := mk(scoring.SourceBaseline)
	j := mk(scoring.Source("jev"))
	n := mk(scoring.Source("nano"))
	forge(&b, &j, &n)
	writeJSON(t, baselinePath, map[string]any{"fixtureFileSha256": prov.FixtureFileSHA, "manifestSha256": prov.ManifestSHA, "baseline": b})
	writeJSON(t, pilotPath, map[string]any{"fixtureFileSha256": prov.FixtureFileSHA, "manifestSha256": prov.ManifestSHA, "tuningOnlyScores": map[string]any{"jev": j, "nano": n}})

	sidecar, err := Build(cfg)
	if err == nil {
		if vErr := Verify(sidecar, cfg); vErr == nil {
			t.Fatal("W3-R4 HOLE REPRODUCED (hard bar): forged self-consistent fabricated set verified; the frozen manifest identity is not the root of trust")
		}
	}
	t.Logf("forged consistent set rejected: %v", err)
}

// TestDecision2ReportStatements: every report must state rate semantics
// UNRECONCILED and must never claim live contracts verified; the Jev output
// rate is bound as an explicit zero (not omitted); MaxOutputTokens is a
// non-cost resource bound.
func TestDecision2ReportStatements(t *testing.T) {
	basis := DefaultBillingBasis()
	if basis.RateSemantics != RateSemanticsUnreconciled {
		t.Fatalf("rateSemantics = %q, want %q", basis.RateSemantics, RateSemanticsUnreconciled)
	}
	if basis.LiveContractsVerified {
		t.Fatal("liveContractsVerified must remain not-true")
	}
	encoded, err := json.Marshal(basis)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"jevRateOutPerMillionMicrodollars":0`) {
		t.Fatalf("RateOut explicit zero not bound in the report JSON (must be present, not omitted): %s", encoded)
	}
	if !strings.Contains(string(encoded), `"liveContractsVerified":false`) {
		t.Fatalf("liveContractsVerified not explicitly not-true: %s", encoded)
	}
	if !strings.Contains(string(encoded), MaxOutputTokensBound) {
		t.Fatalf("MaxOutputTokens non-cost resource bound not stated: %s", encoded)
	}

	// Admissions reject a report whose basis claims verified live contracts
	// or omits the unreconciled rate-semantics statement.
	for _, tc := range []struct {
		name   string
		mutate func(b *BillingBasis)
	}{
		{"claims-live-contracts-verified", func(b *BillingBasis) { b.LiveContractsVerified = true }},
		{"drops-unreconciled-statement", func(b *BillingBasis) { b.RateSemantics = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			baselinePath := filepath.Join(dir, "baseline.json")
			pilotPath := filepath.Join(dir, "pilot.json")
			writeGenuineReports(t, baselinePath, pilotPath, func(b, _, _ *BoundReport) {
				tc.mutate(&b.BillingBasis)
			})
			cfg := genuineCfg(baselinePath, pilotPath)
			if sidecar, err := Build(cfg); err == nil {
				if vErr := Verify(sidecar, cfg); vErr == nil {
					t.Fatal("decision-2 report statement violation admitted")
				}
			}
		})
	}
}

// TestManifestRegenerationGuard: the frozen manifest identity pin must track
// the frozen manifest file exactly (a regenerated manifest must never match).
func TestManifestRegenerationGuard(t *testing.T) {
	manifestBytes, err := os.ReadFile(genuineManifest)
	if err != nil {
		t.Fatal(err)
	}
	if hashBytes(manifestBytes) != FrozenManifestSHA256 {
		t.Fatalf("FrozenManifestSHA256 is stale: got %s want %s", hashBytes(manifestBytes), FrozenManifestSHA256)
	}
	// A regenerated manifest (fresh generatedAt) over the SAME fixtures still
	// differs from the frozen identity.
	m, err := loader.BuildManifest(genuineFixtures, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	regenPath := filepath.Join(dir, "fixtures.manifest.json")
	if err := loader.WriteManifest(regenPath, m); err != nil {
		t.Fatal(err)
	}
	regenBytes, err := os.ReadFile(regenPath)
	if err != nil {
		t.Fatal(err)
	}
	if hashBytes(regenBytes) == FrozenManifestSHA256 {
		t.Fatal("a regenerated manifest matched the frozen manifest identity")
	}
}
