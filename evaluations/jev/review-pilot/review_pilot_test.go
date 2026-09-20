// Schema and manifest validation tests for the review-pilot packet.
// Human-reviewer spotcheck: verify proposed labels are in the closed
// vocabulary for the task and reviewStatus is "unreviewed".
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/loader"
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

func paths(t *testing.T) (string, string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// Tests run from package directory (evaluations/jev/review-pilot).
	fxCand := []string{
		filepath.Join(wd, "fixtures.jsonl"),
		filepath.Join(wd, "..", "review-pilot", "fixtures.jsonl"),
		filepath.Join(wd, "..", "..", "review-pilot", "fixtures.jsonl"),
	}
	mfCand := []string{
		filepath.Join(wd, "fixtures.manifest.json"),
		filepath.Join(wd, "..", "review-pilot", "fixtures.manifest.json"),
		filepath.Join(wd, "..", "..", "review-pilot", "fixtures.manifest.json"),
	}
	var fxPath, mfPath string
	for _, p := range fxCand {
		if _, err := os.Stat(p); err == nil {
			fxPath = p
			break
		}
	}
	for _, p := range mfCand {
		if _, err := os.Stat(p); err == nil {
			mfPath = p
			break
		}
	}
	if fxPath == "" {
		t.Fatalf("fixtures.jsonl not found near %s", wd)
	}
	if mfPath == "" {
		t.Fatalf("fixtures.manifest.json not found near %s", wd)
	}
	return fxPath, mfPath
}

func TestReviewPilotSchemaValidation(t *testing.T) {
	fxPath, mfPath := paths(t)
	loaded, err := loader.Load(fxPath, mfPath)
	if err != nil {
		t.Fatalf("loader.Load failed: %v", err)
	}
	fxs := loaded.Fixtures
	if len(fxs) != 24 {
		t.Fatalf("expected 24 fixtures, got %d", len(fxs))
	}

	// Spotcheck: every proposed label in vocab for its task; every
	// reviewStatus is "unreviewed"; every author is set.
	for _, f := range fxs {
		if f.ReviewStatus != protocol.ReviewUnreviewed {
			t.Errorf("fixture %s: reviewStatus=%v (want unreviewed)", f.ID, f.ReviewStatus)
		}
		if f.Author == "" {
			t.Errorf("fixture %s: missing author", f.ID)
		}
		if !protocol.IsAllowedLabel(f.Task, f.ExpectedLabel) {
			t.Errorf("fixture %s: expectedLabel %q not in %s vocab", f.ID, f.ExpectedLabel, f.Task)
		}
	}

	// Per-task counts.
	var entity, candT, rel, claim int
	for _, f := range fxs {
		switch f.Task {
		case protocol.TaskEntityType:
			entity++
		case protocol.TaskCandidateType:
			candT++
		case protocol.TaskRelationship:
			rel++
		case protocol.TaskClaimSupport:
			claim++
		}
	}
	if entity != 6 {
		t.Errorf("entity-type count = %d, want 6", entity)
	}
	if candT != 6 {
		t.Errorf("candidate-type count = %d, want 6", candT)
	}
	if rel != 6 {
		t.Errorf("relationship count = %d, want 6", rel)
	}
	if claim != 6 {
		t.Errorf("claim-support count = %d, want 6", claim)
	}
	// Sum invariant: 6 + 6 + 6 + 6 == 24 == total.
	if total := entity + candT + rel + claim; total != 24 {
		t.Errorf("sum of per-task counts %d != 24", total)
	}

	// All 3 claim verdicts represented twice.
	var sup, contra, insuf int
	for _, f := range fxs {
		if f.Task != protocol.TaskClaimSupport {
			continue
		}
		switch f.ExpectedLabel {
		case "supported":
			sup++
		case "contradicted":
			contra++
		case "insufficient-evidence":
			insuf++
		}
	}
	if sup != 2 || contra != 2 || insuf != 2 {
		t.Errorf("claim verdicts: supported=%d contradicted=%d insufficient-evidence=%d (want 2/2/2)", sup, contra, insuf)
	}

	// 24 distinct source groups.
	seen := make(map[string]bool)
	for _, f := range fxs {
		if seen[f.SourceGroup] {
			t.Errorf("duplicate source group %s", f.SourceGroup)
		}
		seen[f.SourceGroup] = true
	}
	if len(seen) != 24 {
		t.Errorf("distinct source groups = %d, want 24", len(seen))
	}

	// All split tuning.
	for _, f := range fxs {
		if f.Split != protocol.SplitTuning {
			t.Errorf("fixture %s: split=%v (want tuning)", f.ID, f.Split)
		}
	}
}

func TestReviewPilotManifest(t *testing.T) {
	_, mfPath := paths(t)
	data, err := os.ReadFile(mfPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m protocol.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m.FixtureCount != 24 {
		t.Errorf("manifest FixtureCount = %d, want 24", m.FixtureCount)
	}
	if m.SourceGroupCount != 24 {
		t.Errorf("manifest SourceGroupCount = %d, want 24", m.SourceGroupCount)
	}
	if m.SplitCounts.Tuning != 24 || m.SplitCounts.HoldOut != 0 {
		t.Errorf("manifest SplitCounts wrong: %+v", m.SplitCounts)
	}
	if m.ReviewStatusCount[protocol.ReviewUnreviewed] != 24 {
		t.Errorf("manifest reviewStatusCount[unreviewed] = %d, want 24", m.ReviewStatusCount[protocol.ReviewUnreviewed])
	}
	// Per-task count regression: each of the 4 tasks has 6 fixtures.
	if got := m.TaskCounts.EntityType; got != 6 {
		t.Errorf("manifest TaskCounts.EntityType = %d, want 6", got)
	}
	if got := m.TaskCounts.CandidateType; got != 6 {
		t.Errorf("manifest TaskCounts.CandidateType = %d, want 6 (regression: missing case)", got)
	}
	if got := m.TaskCounts.Relationship; got != 6 {
		t.Errorf("manifest TaskCounts.Relationship = %d, want 6", got)
	}
	if got := m.TaskCounts.ClaimSupport; got != 6 {
		t.Errorf("manifest TaskCounts.ClaimSupport = %d, want 6", got)
	}
	if got := m.TaskCounts.Total; got != 24 {
		t.Errorf("manifest TaskCounts.Total = %d, want 24", got)
	}
}

// TestReviewPilotMetadataRoundtrip is the P1-1 regression: the
// CandidateSource and RationaleEvidence JSON fields must survive
// loader roundtrip (raw read -> Unmarshal -> Marshal -> re-read).
// Before the fix, these fields were absent from protocol.Fixture
// and encoding/json silently discarded them on Unmarshal.
// TestReviewPilotV0D4LabelsByConvention is the behavioural regression
// for the v0.4 corrections: 4 fixtures must use insufficient-evidence
// gold per the explicit-abstention convention; 1 must use paper per
// the primary-subject typing convention. At v0.3, these 4 cases used
// the legacy default-fallback (document / PERSON) and rp-et-006 used
// document.
func TestReviewPilotV0D4LabelsByConvention(t *testing.T) {
	fxPath, mfPath := paths(t)
	loaded, err := loader.Load(fxPath, mfPath)
	if err != nil {
		t.Fatalf("loader.Load: %v", err)
	}
	want := map[string]string{
		"rp-et-002": "insufficient-evidence", // mixed subjects, no clear primary
		"rp-et-004": "insufficient-evidence", // empty body (missing evidence)
		"rp-et-006": "paper",                 // primary subject = climate analysis
		"rp-ct-010": "insufficient-evidence", // placeholder candidate, no context
	}
	got := map[string]string{}
	for _, f := range loaded.Fixtures {
		if _, ok := want[f.ID]; ok {
			got[f.ID] = f.ExpectedLabel
		}
	}
	for id, exp := range want {
		if got[id] != exp {
			t.Errorf("fixture %s: v0.4 expected gold %q, got %q", id, exp, got[id])
		}
	}
	// Manifest protocolVersion must be 0.4.0.
	data, err := os.ReadFile(filepath.Join(wd(t), "fixtures.manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m protocol.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.ProtocolVersion != "0.4.0" {
		t.Errorf("manifest ProtocolVersion = %q, want 0.4.0", m.ProtocolVersion)
	}
}

func wd(t *testing.T) string {
	t.Helper()
	wd, _ := os.Getwd()
	return wd
}

func TestReviewPilotMetadataRoundtrip(t *testing.T) {
	fxPath, mfPath := paths(t)
	rawBytes, err := os.ReadFile(fxPath)
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	// Parse each line as raw JSON, compare CandidateSource and
	// RationaleEvidence values against the loader's parsed Fixture.
	dec := json.NewDecoder(bytes.NewReader(rawBytes))
	var rawLines [][]byte
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode raw: %v", err)
		}
		rawLines = append(rawLines, raw)
	}
	if len(rawLines) != 24 {
		t.Fatalf("raw line count = %d, want 24", len(rawLines))
	}
	loaded, err := loader.Load(fxPath, mfPath)
	if err != nil {
		t.Fatalf("loader.Load: %v", err)
	}
	if len(loaded.Fixtures) != 24 {
		t.Fatalf("loaded fixture count = %d, want 24", len(loaded.Fixtures))
	}
	for i, f := range loaded.Fixtures {
		var rawMap map[string]any
		if err := json.Unmarshal(rawLines[i], &rawMap); err != nil {
			t.Fatalf("unmarshal raw line %d: %v", i, err)
		}
		rawCandSrc, _ := rawMap["candidateSource"].(string)
		rawRatEv, _ := rawMap["rationaleEvidence"].(string)
		if f.CandidateSource != rawCandSrc {
			t.Errorf("fixture %s: CandidateSource lost in roundtrip; raw=%q got=%q", f.ID, rawCandSrc, f.CandidateSource)
		}
		if f.RationaleEvidence != rawRatEv {
			t.Errorf("fixture %s: RationaleEvidence lost in roundtrip; raw=%q got=%q", f.ID, rawRatEv, f.RationaleEvidence)
		}
		// Roundtrip via Marshal -> Unmarshal must also preserve the values.
		remarshaled, err := json.Marshal(f)
		if err != nil {
			t.Fatalf("marshal fixture %s: %v", f.ID, err)
		}
		var reparsed protocol.Fixture
		if err := json.Unmarshal(remarshaled, &reparsed); err != nil {
			t.Fatalf("unmarshal remarshal %s: %v", f.ID, err)
		}
		if reparsed.CandidateSource != f.CandidateSource {
			t.Errorf("fixture %s: remarshal CandidateSource mismatch", f.ID)
		}
		if reparsed.RationaleEvidence != f.RationaleEvidence {
			t.Errorf("fixture %s: remarshal RationaleEvidence mismatch", f.ID)
		}
	}
}
