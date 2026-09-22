package heldout_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/loader"
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

const expectedAuthor = "agent:codex-gpt-5.6-sol-9ed7c23e"

func TestStudyCorpusTargetsAndReviewMetadata(t *testing.T) {
	loaded, err := loader.Load("fixtures.jsonl", "fixtures.manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Drift != nil {
		t.Fatalf("corpus drift: %+v", loaded.Drift)
	}
	tuningByTask := map[protocol.Task]int{}
	heldNegativeGroups := map[protocol.Task]map[string]struct{}{
		protocol.TaskRelationship: {},
		protocol.TaskClaimSupport: {},
	}
	heldNegativeCases := map[protocol.Task]int{}
	heldGroupCases := map[string]int{}
	contradicted := 0
	challengeCounts := map[protocol.ChallengeCategory]int{}
	for _, fixture := range loaded.Fixtures {
		if fixture.ReviewStatus != protocol.ReviewUnreviewed || fixture.Reviewer != "" {
			t.Errorf("fixture %s has non-agent review state %q/%q", fixture.ID, fixture.ReviewStatus, fixture.Reviewer)
		}
		if fixture.Author != expectedAuthor {
			t.Errorf("fixture %s author=%q", fixture.ID, fixture.Author)
		}
		if strings.TrimSpace(fixture.Rationale) == "" || strings.TrimSpace(fixture.RationaleEvidence) == "" {
			t.Errorf("fixture %s lacks adjudication rationale", fixture.ID)
		}
		if fixture.Split == protocol.SplitTuning {
			tuningByTask[fixture.Task]++
		}
		if fixture.Split == protocol.SplitHeldOut {
			heldGroupCases[fixture.SourceGroup]++
			negative := (fixture.Task == protocol.TaskRelationship && (fixture.ExpectedLabel == "no-supported-relationship" || fixture.ExpectedLabel == "insufficient-evidence")) ||
				(fixture.Task == protocol.TaskClaimSupport && fixture.ExpectedLabel != "supported")
			if negative {
				heldNegativeCases[fixture.Task]++
				heldNegativeGroups[fixture.Task][fixture.SourceGroup] = struct{}{}
			}
			if fixture.Task == protocol.TaskClaimSupport && fixture.ExpectedLabel == "contradicted" {
				contradicted++
			}
		}
		for _, challenge := range fixture.ChallengeCategories {
			challengeCounts[challenge]++
		}
	}
	for _, task := range []protocol.Task{protocol.TaskEntityType, protocol.TaskCandidateType, protocol.TaskRelationship, protocol.TaskClaimSupport} {
		if tuningByTask[task] < 30 {
			t.Errorf("tuning %s=%d, want >=30", task, tuningByTask[task])
		}
	}
	for _, task := range []protocol.Task{protocol.TaskRelationship, protocol.TaskClaimSupport} {
		if got := heldNegativeCases[task]; got != 150 {
			t.Errorf("held-out negative cases for %s=%d, want 150", task, got)
		}
		if got := len(heldNegativeGroups[task]); got < 150 {
			t.Errorf("independent held-out negative groups for %s=%d, want >=150", task, got)
		}
	}
	for group, count := range heldGroupCases {
		if count != 4 {
			t.Errorf("held-out group %s has %d fixtures, want 4", group, count)
		}
	}
	if contradicted < 50 {
		t.Errorf("held-out contradicted cases=%d, want >=50", contradicted)
	}
	for _, challenge := range []protocol.ChallengeCategory{
		protocol.ChallengeStraightPositive, protocol.ChallengeStraightNegative,
		protocol.ChallengeMissingEvidence, protocol.ChallengeCompeting,
		protocol.ChallengeNegation, protocol.ChallengeReversedDir,
		protocol.ChallengeRenamed, protocol.ChallengeConflicting,
		protocol.ChallengeMisleading, protocol.ChallengeIrrelevant,
		protocol.ChallengeInvalidating, protocol.ChallengeAdversarial,
	} {
		if challengeCounts[challenge] == 0 {
			t.Errorf("challenge %s has no fixtures", challenge)
		}
	}
}

func TestStudyCorpusStructuralSplitIndependence(t *testing.T) {
	loaded, err := loader.Load("fixtures.jsonl", "fixtures.manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	independence, err := loader.VerifySplitIndependence(loaded.Fixtures)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range independence {
		if !item.Independent || !item.EntityDisjoint || !item.BodyEntityDisjoint || !item.TemplateFamilyDisjoint {
			t.Errorf("split %s lacks structural independence: %+v", item.Split, item)
		}
	}
}

func TestStudyCorpusDoesNotReuseNormalizedBodyConstructionAcrossSplits(t *testing.T) {
	loaded, err := loader.Load("fixtures.jsonl", "fixtures.manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	bySplit := map[protocol.Split]map[string]string{
		protocol.SplitTuning:  {},
		protocol.SplitHeldOut: {},
	}
	for _, fixture := range loaded.Fixtures {
		for _, excerpt := range fixture.Excerpts {
			fingerprint := bodyConstructionFingerprint(excerpt.Text)
			if fingerprint != "" {
				bySplit[fixture.Split][fingerprint] = fixture.ID
			}
		}
	}
	for fingerprint, tuningID := range bySplit[protocol.SplitTuning] {
		if heldID, exists := bySplit[protocol.SplitHeldOut][fingerprint]; exists {
			t.Fatalf("normalized body construction reused across splits: %s and %s: %q", tuningID, heldID, fingerprint)
		}
	}
}

var (
	linkPattern   = regexp.MustCompile(`\[\[[^\]]+\]\]`)
	numberPattern = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)
	spacePattern  = regexp.MustCompile(`\s+`)
)

func bodyConstructionFingerprint(text string) string {
	text = strings.ToLower(text)
	text = linkPattern.ReplaceAllString(text, "[[entity]]")
	text = numberPattern.ReplaceAllString(text, "<number>")
	return strings.TrimSpace(spacePattern.ReplaceAllString(text, " "))
}
