package loader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

func writeFixtures(t *testing.T, dir string, fixtures []protocol.Fixture) string {
	t.Helper()
	path := filepath.Join(dir, "fixtures.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, fix := range fixtures {
		if err := enc.Encode(fix); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	return path
}

func writeManifest(t *testing.T, dir string, m protocol.Manifest) string {
	t.Helper()
	path := filepath.Join(dir, "manifest.json")
	if err := WriteManifest(path, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

func sampleFixtures() []protocol.Fixture {
	return []protocol.Fixture{
		{
			ID: "f1", Task: protocol.TaskEntityType, SourceGroup: "g1", Split: protocol.SplitTuning, Author: "agent", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "person", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskEntityType),
			Excerpts: []protocol.Excerpt{{ID: "e1", Text: "Alice Chen is a person."}},
		},
		{
			ID: "f2", Task: protocol.TaskClaimSupport, SourceGroup: "g1", Split: protocol.SplitTuning, Author: "agent", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
			Excerpts:   []protocol.Excerpt{{ID: "e1", Text: "Alice Chen is a person."}},
			Candidates: []protocol.Candidate{{ID: "title:0", Title: "Alice Chen"}},
		},
	}
}

func mustEncode(fs []protocol.Fixture) []byte {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	for _, f := range fs {
		_ = enc.Encode(f)
	}
	return []byte(b.String())
}

func TestLoadAndDriftFree(t *testing.T) {
	dir := t.TempDir()
	fixPath := writeFixtures(t, dir, sampleFixtures())
	m, err := BuildManifest(fixPath, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	manPath := writeManifest(t, dir, m)
	loaded, err := Load(fixPath, manPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Drift != nil {
		t.Fatalf("expected no drift, got %v", loaded.Drift)
	}
	if len(loaded.Fixtures) != 2 {
		t.Fatalf("expected 2 fixtures, got %d", len(loaded.Fixtures))
	}
}

func TestLoadDetectsHashMismatch(t *testing.T) {
	dir := t.TempDir()
	fixPath := writeFixtures(t, dir, sampleFixtures())
	m, err := BuildManifest(fixPath, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	manPath := writeManifest(t, dir, m)
	mutated := sampleFixtures()
	mutated[0].ExpectedLabel = "tool"
	if err := os.WriteFile(fixPath, mustEncode(mutated), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	loaded, err := Load(fixPath, manPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Drift == nil {
		t.Fatal("expected drift report")
	}
	if len(loaded.Drift.HashMismatches) == 0 {
		t.Fatalf("expected hash mismatch, got %+v", loaded.Drift)
	}
}

func TestVerifySplitIndependenceDetectsOverlap(t *testing.T) {
	fs := []protocol.Fixture{
		{ID: "f1", Task: protocol.TaskEntityType, SourceGroup: "g1", Split: protocol.SplitTuning, Author: "a", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "person", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskEntityType),
			Excerpts: []protocol.Excerpt{{ID: "e1", Text: "x"}}},
		{ID: "f2", Task: protocol.TaskEntityType, SourceGroup: "g1", Split: protocol.SplitHeldOut, Author: "a", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "person", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskEntityType),
			Excerpts: []protocol.Excerpt{{ID: "e1", Text: "x"}}},
	}
	_, err := VerifySplitIndependence(fs)
	if err == nil {
		t.Fatal("expected independence violation")
	}
}

func TestVerifySplitIndependenceAcceptsDistinctGroups(t *testing.T) {
	fs := []protocol.Fixture{
		{ID: "f1", Task: protocol.TaskEntityType, SourceGroup: "g1", Split: protocol.SplitTuning, Author: "a", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "person", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskEntityType),
			Excerpts: []protocol.Excerpt{{ID: "e1", Text: "x"}}},
		{ID: "f2", Task: protocol.TaskEntityType, SourceGroup: "g2", Split: protocol.SplitHeldOut, Author: "a", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "person", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskEntityType),
			Excerpts: []protocol.Excerpt{{ID: "e1", Text: "x"}}},
	}
	indep, err := VerifySplitIndependence(fs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, item := range indep {
		if !item.Independent {
			t.Errorf("expected independence for %s, got %+v", item.Split, item)
		}
	}
}

func TestVerifySplitIndependenceDetectsCrossSplitEntityLeak(t *testing.T) {
	// R2: "Heldar Range" appearing in both tuning and held-out must
	// fail independence even if the group IDs and template families
	// are disjoint. This test reproduces the v0.2 leak the re-review
	// flagged.
	fs := []protocol.Fixture{
		{ID: "f1", Task: protocol.TaskClaimSupport, SourceGroup: "g1", Split: protocol.SplitTuning, Author: "a", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
			Candidates: []protocol.Candidate{{ID: "g1-e1", Title: "Heldar Range", Alias: "Heldar"}},
			Excerpts:   []protocol.Excerpt{{ID: "e1", Text: "x"}}},
		{ID: "f2", Task: protocol.TaskClaimSupport, SourceGroup: "g2", Split: protocol.SplitHeldOut, Author: "a", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
			Candidates: []protocol.Candidate{{ID: "g2-e1", Title: "Heldar Range", Alias: "Heldar Mountains"}},
			Excerpts:   []protocol.Excerpt{{ID: "e1", Text: "x"}}},
	}
	_, err := VerifySplitIndependence(fs)
	if err == nil {
		t.Fatal("expected cross-split entity leak to be flagged")
	}
}

func TestVerifySplitIndependenceDetectsV03BodyEntityLeakBehindSaltedCandidates(t *testing.T) {
	fs := []protocol.Fixture{
		{ID: "f1", Task: protocol.TaskClaimSupport, SourceGroup: "tune-group", TemplateFamily: "tune-prose", Split: protocol.SplitTuning, Author: "agent", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
			Candidates: []protocol.Candidate{{ID: "tune-e1", Title: "tune/vornholt-pass", Alias: "tune/vornholt"}},
			Excerpts:   []protocol.Excerpt{{ID: "e1", Text: "The Vornholt Pass region closes during winter."}}},
		{ID: "f2", Task: protocol.TaskClaimSupport, SourceGroup: "held-group", TemplateFamily: "held-docket", Split: protocol.SplitHeldOut, Author: "agent", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
			Candidates: []protocol.Candidate{{ID: "held-e1", Title: "held/vornholt-pass", Alias: "held/pass"}},
			Excerpts:   []protocol.Excerpt{{ID: "e1", Text: "Inspectors reopened the Vornholt Pass region in spring."}}},
	}
	_, err := VerifySplitIndependence(fs)
	if err == nil || !strings.Contains(err.Error(), "body entity") {
		t.Fatalf("v0.3-style body leak was not rejected: %v", err)
	}
}

func TestVerifySplitIndependenceDetectsCandidateToBodyCrossChannelLeak(t *testing.T) {
	fs := []protocol.Fixture{
		{ID: "f1", Task: protocol.TaskClaimSupport, SourceGroup: "tune-group", TemplateFamily: "tune-prose", Split: protocol.SplitTuning, Author: "agent", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
			Candidates: []protocol.Candidate{{ID: "tune-e1", Title: "Copper Finch Registry", Alias: "Copper Finch Office"}},
			Excerpts:   []protocol.Excerpt{{ID: "e1", Text: "A distinct record is discussed."}}},
		{ID: "f2", Task: protocol.TaskClaimSupport, SourceGroup: "held-group", TemplateFamily: "held-docket", Split: protocol.SplitHeldOut, Author: "agent", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
			Candidates: []protocol.Candidate{{ID: "held-e1", Title: "Silver Heron Ledger", Alias: "Silver Heron File"}},
			Excerpts:   []protocol.Excerpt{{ID: "e1", Text: "The Copper Finch Registry approved the filing."}}},
	}
	_, err := VerifySplitIndependence(fs)
	if err == nil || !strings.Contains(err.Error(), "candidate") || !strings.Contains(err.Error(), "body text") {
		t.Fatalf("candidate-to-body cross-channel leak was not rejected: %v", err)
	}
}

func TestVerifySplitIndependenceDetectsBareCaseVariantAcrossChannels(t *testing.T) {
	fs := []protocol.Fixture{
		{ID: "f1", Task: protocol.TaskClaimSupport, SourceGroup: "tune-group", TemplateFamily: "tune-prose", Split: protocol.SplitTuning, Author: "agent", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
			Candidates: []protocol.Candidate{{ID: "tune-e1", Title: "Grey Cloak Workshop", Alias: "Grey Cloak"}},
			Excerpts:   []protocol.Excerpt{{ID: "e1", Text: "A distinct record is discussed."}}},
		{ID: "f2", Task: protocol.TaskClaimSupport, SourceGroup: "held-group", TemplateFamily: "held-docket", Split: protocol.SplitHeldOut, Author: "agent", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
			Candidates: []protocol.Candidate{{ID: "held-e1", Title: "Silver Heron Ledger", Alias: "Silver Heron File"}},
			Excerpts:   []protocol.Excerpt{{ID: "e1", Text: "the grey cloak workshop approved the filing."}}},
	}
	_, err := VerifySplitIndependence(fs)
	if err == nil || !strings.Contains(err.Error(), "candidate") || !strings.Contains(err.Error(), "body text") {
		t.Fatalf("bare lower-case case variant was not rejected: %v", err)
	}
}

func TestVerifySplitIndependenceDetectsBareCaseVariantAcrossBodies(t *testing.T) {
	fs := []protocol.Fixture{
		{ID: "f1", Task: protocol.TaskClaimSupport, SourceGroup: "tune-group", TemplateFamily: "tune-prose", Split: protocol.SplitTuning, Author: "agent", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
			Candidates: []protocol.Candidate{{ID: "tune-e1", Title: "Copper Finch Registry"}},
			Excerpts:   []protocol.Excerpt{{ID: "e1", Text: "The Grey Cloak Workshop approved the filing."}}},
		{ID: "f2", Task: protocol.TaskClaimSupport, SourceGroup: "held-group", TemplateFamily: "held-docket", Split: protocol.SplitHeldOut, Author: "agent", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
			Candidates: []protocol.Candidate{{ID: "held-e1", Title: "Silver Heron Ledger"}},
			Excerpts:   []protocol.Excerpt{{ID: "e1", Text: "the grey cloak workshop approved the filing."}}},
	}
	_, err := VerifySplitIndependence(fs)
	if err == nil || !strings.Contains(err.Error(), "body entity") {
		t.Fatalf("bare lower-case body-to-body case variant was not rejected: %v", err)
	}
}

func TestVerifySplitIndependenceAcceptsNormalizedDistinctEntities(t *testing.T) {
	// Same surface form but case differs; the loader must not flag a
	// case-difference as an entity collision. The fixture-supplied
	// distinct entities must pass.
	fs := []protocol.Fixture{
		{ID: "f1", Task: protocol.TaskClaimSupport, SourceGroup: "g1", Split: protocol.SplitTuning, Author: "a", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
			Candidates: []protocol.Candidate{{ID: "g1-e1", Title: "ALPHA Range", Alias: "alpha"}},
			Excerpts:   []protocol.Excerpt{{ID: "e1", Text: "x"}}},
		{ID: "f2", Task: protocol.TaskClaimSupport, SourceGroup: "g2", Split: protocol.SplitHeldOut, Author: "a", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
			Candidates: []protocol.Candidate{{ID: "g2-e1", Title: "BETA Range", Alias: "beta"}},
			Excerpts:   []protocol.Excerpt{{ID: "e1", Text: "x"}}},
	}
	if _, err := VerifySplitIndependence(fs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateFixturesRejectsDuplicateIDs(t *testing.T) {
	fs := sampleFixtures()
	fs[1].ID = fs[0].ID
	if err := ValidateFixtures(fs); err == nil {
		t.Fatal("expected duplicate id error")
	} else if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate error, got %v", err)
	}
}

// TestBuildManifestCountsAllFourTasks is the regression for the missing
// TaskCandidateType case in BuildManifest's per-task switch. Before the
// fix, candidate-type fixtures were silently dropped from TaskCounts
// even though they were counted in the total.
func TestBuildManifestCountsAllFourTasks(t *testing.T) {
	dir := t.TempDir()
	fixPath := writeFixtures(t, dir, []protocol.Fixture{
		{
			ID: "et1", Task: protocol.TaskEntityType, SourceGroup: "g1", Split: protocol.SplitTuning, Author: "agent", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "person", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskEntityType),
			Excerpts: []protocol.Excerpt{{ID: "e1", Text: "x"}},
		},
		{
			ID: "ct1", Task: protocol.TaskCandidateType, SourceGroup: "g1", Split: protocol.SplitTuning, Author: "agent", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "PERSON", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskCandidateType),
			Excerpts:   []protocol.Excerpt{{ID: "e1", Text: "x"}},
			Candidates: []protocol.Candidate{{ID: "title:0", Title: "Alice Chen"}},
		},
		{
			ID: "rel1", Task: protocol.TaskRelationship, SourceGroup: "g1", Split: protocol.SplitTuning, Author: "agent", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "related-to", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskRelationship),
			Excerpts:     []protocol.Excerpt{{ID: "e1", Text: "x"}},
			Candidates:   []protocol.Candidate{{ID: "title:0", Title: "Alice"}, {ID: "title:1", Title: "Bob"}},
			EdgeSourceID: "title:0", EdgeTargetID: "title:1",
		},
		{
			ID: "cs1", Task: protocol.TaskClaimSupport, SourceGroup: "g1", Split: protocol.SplitTuning, Author: "agent", ReviewStatus: protocol.ReviewUnreviewed,
			ExpectedLabel: "supported", AllowedLabels: protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
			Excerpts:   []protocol.Excerpt{{ID: "e1", Text: "x"}},
			Candidates: []protocol.Candidate{{ID: "title:0", Title: "Alice"}},
		},
	})
	m, err := BuildManifest(fixPath, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if m.TaskCounts.EntityType != 1 {
		t.Errorf("EntityType count = %d, want 1", m.TaskCounts.EntityType)
	}
	if m.TaskCounts.CandidateType != 1 {
		t.Errorf("CandidateType count = %d, want 1 (regression: missing case in switch)", m.TaskCounts.CandidateType)
	}
	if m.TaskCounts.Relationship != 1 {
		t.Errorf("Relationship count = %d, want 1", m.TaskCounts.Relationship)
	}
	if m.TaskCounts.ClaimSupport != 1 {
		t.Errorf("ClaimSupport count = %d, want 1", m.TaskCounts.ClaimSupport)
	}
	if m.TaskCounts.Total != 4 {
		t.Errorf("TaskCounts.Total = %d, want 4", m.TaskCounts.Total)
	}
	//// Sum invariant: 1 + 1 + 1 + 1 == 4 == Total.
	if got := m.TaskCounts.EntityType + m.TaskCounts.CandidateType + m.TaskCounts.Relationship + m.TaskCounts.ClaimSupport; got != m.TaskCounts.Total {
		t.Errorf("sum of per-task counts %d != Total %d", got, m.TaskCounts.Total)
	}
}
