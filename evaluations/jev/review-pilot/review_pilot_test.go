// Schema and manifest validation tests for the review-pilot packet.
// Human-reviewer spotcheck: verify proposed labels are in the closed
// vocabulary for the task and reviewStatus is "unreviewed".
package main

import (
	"encoding/json"
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
}
