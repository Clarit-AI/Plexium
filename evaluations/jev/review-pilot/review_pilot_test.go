// Schema and manifest validation tests for the review-pilot packet.
// Human-reviewer spotcheck: verify proposed labels are in the closed
// vocabulary, the ten adjudicated fixtures carry human approval, and the
// remaining fixtures stay unreviewed.
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

	approvedIDs := map[string]bool{
		"rp-et-001":  true,
		"rp-et-002":  true,
		"rp-et-003":  true,
		"rp-et-006":  true,
		"rp-rel-013": true,
		"rp-rel-014": true,
		"rp-rel-015": true,
		"rp-ct-012":  true,
		"rp-rel-016": true,
		"rp-cs-020":  true,
	}
	// Spotcheck: every proposed label is in vocab for its task; exactly the
	// ten adjudicated fixtures are approved; every author is set.
	for _, f := range fxs {
		if approvedIDs[f.ID] {
			if f.ReviewStatus != protocol.ReviewApproved {
				t.Errorf("fixture %s: reviewStatus=%v (want approved)", f.ID, f.ReviewStatus)
			}
			if f.Reviewer != "KHAEntertainment" {
				t.Errorf("fixture %s: reviewer=%q (want KHAEntertainment)", f.ID, f.Reviewer)
			}
		} else {
			if f.ReviewStatus != protocol.ReviewUnreviewed {
				t.Errorf("fixture %s: reviewStatus=%v (want unreviewed)", f.ID, f.ReviewStatus)
			}
			if f.Reviewer != "" {
				t.Errorf("fixture %s: reviewer=%q (want empty for unreviewed fixture)", f.ID, f.Reviewer)
			}
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

	// All 3 claim verdicts remain represented after human correction.
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
	if sup != 2 || contra != 1 || insuf != 3 {
		t.Errorf("claim verdicts: supported=%d contradicted=%d insufficient-evidence=%d (want 2/1/3)", sup, contra, insuf)
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
	if m.ReviewStatusCount[protocol.ReviewUnreviewed] != 14 {
		t.Errorf("manifest reviewStatusCount[unreviewed] = %d, want 14", m.ReviewStatusCount[protocol.ReviewUnreviewed])
	}
	if m.ReviewStatusCount[protocol.ReviewApproved] != 10 {
		t.Errorf("manifest reviewStatusCount[approved] = %d, want 10", m.ReviewStatusCount[protocol.ReviewApproved])
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
// TestReviewPilotLabelsByCurrentAdjudication locks the current fixture labels,
// including the three human corrections layered on the v0.4 packet.
func TestReviewPilotLabelsByCurrentAdjudication(t *testing.T) {
	fxPath, mfPath := paths(t)
	loaded, err := loader.Load(fxPath, mfPath)
	if err != nil {
		t.Fatalf("loader.Load: %v", err)
	}
	want := map[string]string{
		"rp-et-001":  "place",                 // approved regional subject
		"rp-et-002":  "project",               // approved civic foundation project
		"rp-et-003":  "document",              // approved accord-as-formal-document
		"rp-et-004":  "insufficient-evidence", // empty body (missing evidence)
		"rp-et-006":  "place",                 // approved named-region environment
		"rp-ct-010":  "insufficient-evidence", // placeholder candidate, no context
		"rp-ct-012":  "insufficient-evidence", // approved ambiguous survey referent
		"rp-rel-013": "related-to",            // monitoring association, not functional use
		"rp-rel-014": "related-to",            // ownership association survives reversed order
		"rp-rel-015": "related-to",            // geographically consistent sources
		"rp-rel-016": "related-to",            // approved supply association, not entity use
		"rp-cs-020":  "insufficient-evidence", // no incompatible or exhaustive endpoint evidence
	}
	got := map[string]string{}
	for _, f := range loaded.Fixtures {
		if _, ok := want[f.ID]; ok {
			got[f.ID] = f.ExpectedLabel
		}
	}
	for id, exp := range want {
		if got[id] != exp {
			t.Errorf("fixture %s: expected gold %q, got %q", id, exp, got[id])
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

func TestReviewPilotHumanAdjudicationMetadata(t *testing.T) {
	fxPath, mfPath := paths(t)
	loaded, err := loader.Load(fxPath, mfPath)
	if err != nil {
		t.Fatalf("loader.Load: %v", err)
	}

	want := map[string]struct {
		label             string
		rationaleFragment string
	}{
		"rp-et-001":  {label: "place", rationaleFragment: "intentionally ambiguous benchmark case"},
		"rp-et-002":  {label: "project", rationaleFragment: "public works project"},
		"rp-et-003":  {label: "document", rationaleFragment: "identifiable formal agreements"},
		"rp-et-006":  {label: "place", rationaleFragment: "environmental characteristics"},
		"rp-rel-013": {label: "related-to", rationaleFragment: "not explicit functional use"},
		"rp-rel-014": {label: "related-to", rationaleFragment: "does not justify broadening part-of semantics"},
		"rp-rel-015": {label: "related-to", rationaleFragment: "geographically consistent"},
		"rp-ct-012":  {label: "insufficient-evidence", rationaleFragment: "ambiguous between a survey activity and a resulting document"},
		"rp-rel-016": {label: "related-to", rationaleFragment: "not explicit use of the source supplier entity itself"},
		"rp-cs-020":  {label: "insufficient-evidence", rationaleFragment: "does not declare the listed endpoints exhaustive"},
	}
	approved := make(map[string]protocol.Fixture)
	for i := range loaded.Fixtures {
		if loaded.Fixtures[i].ReviewStatus == protocol.ReviewApproved {
			approved[loaded.Fixtures[i].ID] = loaded.Fixtures[i]
		}
	}
	if len(approved) != len(want) {
		t.Fatalf("approved fixture count = %d, want %d: %v", len(approved), len(want), approved)
	}
	for id, expected := range want {
		fixture, ok := approved[id]
		if !ok {
			t.Errorf("fixture %s is not approved", id)
			continue
		}
		if fixture.ExpectedLabel != expected.label {
			t.Errorf("fixture %s: label=%q, want %q", id, fixture.ExpectedLabel, expected.label)
		}
		if fixture.Reviewer != "KHAEntertainment" {
			t.Errorf("fixture %s: reviewer=%q, want KHAEntertainment", id, fixture.Reviewer)
		}
		if !bytes.Contains([]byte(fixture.Rationale), []byte(expected.rationaleFragment)) {
			t.Errorf("fixture %s: rationale %q does not contain %q", id, fixture.Rationale, expected.rationaleFragment)
		}
	}
	if fixture := approved["rp-et-001"]; len(fixture.ChallengeCategories) != 1 || fixture.ChallengeCategories[0] != protocol.ChallengeCompeting {
		t.Fatalf("rp-et-001 challenge categories = %v, want [competing-candidates]", fixture.ChallengeCategories)
	}
	if fixture := approved["rp-rel-015"]; len(fixture.ChallengeCategories) != 1 || fixture.ChallengeCategories[0] != protocol.ChallengeStraightPositive {
		t.Fatalf("rp-rel-015 challenge categories = %v, want [straightforward-positive]", fixture.ChallengeCategories)
	}
	if fixture := approved["rp-ct-012"]; len(fixture.ChallengeCategories) != 1 || fixture.ChallengeCategories[0] != protocol.ChallengeAdversarial {
		t.Fatalf("rp-ct-012 challenge categories = %v, want [adversarial-instruction]", fixture.ChallengeCategories)
	}
	if fixture := approved["rp-cs-020"]; len(fixture.ChallengeCategories) != 1 || fixture.ChallengeCategories[0] != protocol.ChallengeMissingEvidence {
		t.Fatalf("rp-cs-020 challenge categories = %v, want [missing-evidence]", fixture.ChallengeCategories)
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
