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
	heldNegativeSourceGroups := map[protocol.Task]map[string]struct{}{
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
				heldNegativeSourceGroups[fixture.Task][fixture.SourceGroup] = struct{}{}
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
		if got := len(heldNegativeSourceGroups[task]); got != 150 {
			t.Errorf("held-out negative source groups for %s=%d, want 150", task, got)
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
		if !item.Independent || !item.EntityDisjoint || !item.BodyEntityDisjoint || !item.CrossChannelEntityDisjoint || !item.TemplateFamilyDisjoint {
			t.Errorf("split %s lacks structural independence: %+v", item.Split, item)
		}
	}
}

func TestOrganizationEndpointGoldFollowsNamedEvidenceObject(t *testing.T) {
	loaded, err := loader.Load("fixtures.jsonl", "fixtures.manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	wantRelated := map[string]bool{
		"sg-held-002-rel-positive": false, // design
		"sg-held-003-rel-positive": false, // specification
		"sg-held-004-rel-positive": false, // readings
		"sg-held-013-rel-positive": false, // holdings
		"sg-tune-006-rel":          false, // collection
	}
	for _, fixture := range loaded.Fixtures {
		if _, ok := wantRelated[fixture.ID]; !ok {
			continue
		}
		if fixture.ExpectedLabel != "related-to" {
			t.Errorf("%s label=%q, want related-to for organization-mediated object", fixture.ID, fixture.ExpectedLabel)
		}
		if !strings.Contains(strings.ToLower(fixture.RationaleEvidence), "associat") {
			t.Errorf("%s rationale does not explain mediated association: %q", fixture.ID, fixture.RationaleEvidence)
		}
		wantRelated[fixture.ID] = true
	}
	for id, found := range wantRelated {
		if !found {
			t.Errorf("missing endpoint regression fixture %s", id)
		}
	}
}

func TestOrganizationTypingDoesNotCallSubjectOnlyRecorder(t *testing.T) {
	loaded, err := loader.Load("fixtures.jsonl", "fixtures.manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	organizations := map[string]string{
		"sg-tune-005-et": "Elmglass Records House",
		"sg-tune-016-et": "Pineward Civic Forum",
		"sg-tune-027-et": "Bluecap Mineral Board",
	}
	for id, organization := range organizations {
		var fixture *protocol.Fixture
		for i := range loaded.Fixtures {
			if loaded.Fixtures[i].ID == id {
				fixture = &loaded.Fixtures[i]
				break
			}
		}
		if fixture == nil {
			t.Fatalf("missing %s", id)
		}
		if fixture.ExpectedLabel != "organization" {
			t.Fatalf("unexpected organization fixture %s: %+v", id, fixture)
		}
		if strings.Contains(fixture.Excerpts[0].Text, "[["+organization+"]] is mentioned only as the recorder") {
			t.Errorf("%s calls its organization subject only the recorder", id)
		}
	}
}

func TestHeldOutNegativeConstructionClustersAreDisclosedAsTwoPerTask(t *testing.T) {
	loaded, err := loader.Load("fixtures.jsonl", "fixtures.manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	families := map[protocol.Task]map[string]struct{}{
		protocol.TaskRelationship: {},
		protocol.TaskClaimSupport: {},
	}
	for _, fixture := range loaded.Fixtures {
		if fixture.Split != protocol.SplitHeldOut {
			continue
		}
		negative := (fixture.Task == protocol.TaskRelationship && (fixture.ExpectedLabel == "no-supported-relationship" || fixture.ExpectedLabel == "insufficient-evidence")) ||
			(fixture.Task == protocol.TaskClaimSupport && fixture.ExpectedLabel != "supported")
		if negative {
			families[fixture.Task][fixture.TemplateFamily] = struct{}{}
		}
	}
	for task, got := range families {
		if len(got) != 2 {
			t.Errorf("%s negative construction families=%d (%v), want exactly 2 and no n=150 independence claim", task, len(got), got)
		}
	}
}

func TestHeldOutQuestionOnlyOracleIsAtChance(t *testing.T) {
	loaded, err := loader.Load("fixtures.jsonl", "fixtures.manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	type labelCounts map[string]int
	byTaskQuestion := map[protocol.Task]map[string]labelCounts{
		protocol.TaskRelationship: {},
		protocol.TaskClaimSupport: {},
	}
	for _, fixture := range loaded.Fixtures {
		if fixture.Split != protocol.SplitHeldOut {
			continue
		}
		if _, ok := byTaskQuestion[fixture.Task]; !ok {
			continue
		}
		if byTaskQuestion[fixture.Task][fixture.Question] == nil {
			byTaskQuestion[fixture.Task][fixture.Question] = labelCounts{}
		}
		byTaskQuestion[fixture.Task][fixture.Question][fixture.ExpectedLabel]++
	}
	for task, questions := range byTaskQuestion {
		total, oracleCorrect := 0, 0
		for question, counts := range questions {
			questionTotal, best := 0, 0
			for _, count := range counts {
				questionTotal += count
				if count > best {
					best = count
				}
			}
			if questionTotal != 2 || best != 1 {
				t.Errorf("%s question %q has label counts %v; want one positive and one negative", task, question, counts)
			}
			total += questionTotal
			oracleCorrect += best
		}
		if total != 300 || oracleCorrect*2 != total {
			t.Errorf("%s question-only oracle=%d/%d, want exactly chance", task, oracleCorrect, total)
		}
	}
}

func TestHeldOutGenuineReverseEdgesAndRawEvidence(t *testing.T) {
	loaded, err := loader.Load("fixtures.jsonl", "fixtures.manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	reversed, unmarked := 0, 0
	for _, fixture := range loaded.Fixtures {
		if fixture.Split != protocol.SplitHeldOut {
			continue
		}
		marked := false
		for _, excerpt := range fixture.Excerpts {
			marked = marked || strings.Contains(excerpt.Text, "[[")
		}
		if !marked {
			unmarked++
		}
		if fixture.TemplateFamily != "held-reverse-association" {
			continue
		}
		reversed++
		if fixture.ExpectedLabel != "related-to" || !strings.Contains(fixture.Excerpts[0].Text, "depends on") || !strings.Contains(fixture.Excerpts[0].Text, "inverse narrow predicate") {
			t.Errorf("%s is not a genuine reverse-edge association case: %+v", fixture.ID, fixture)
		}
	}
	if reversed != 30 {
		t.Errorf("genuine reverse-edge cases=%d, want 30", reversed)
	}
	if unmarked != 120 {
		t.Errorf("fully unmarked held-out cases=%d, want 120", unmarked)
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
