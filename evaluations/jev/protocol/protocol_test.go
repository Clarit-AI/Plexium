package protocol

import (
	"strings"
	"testing"
)

func TestTaskValid(t *testing.T) {
	cases := []struct {
		t    Task
		want bool
	}{
		{TaskEntityType, true},
		{TaskCandidateType, true},
		{TaskRelationship, true},
		{TaskClaimSupport, true},
		{Task("mystery"), false},
	}
	for _, c := range cases {
		if c.t.Valid() != c.want {
			t.Errorf("%s valid: got %v want %v", c.t, c.t.Valid(), c.want)
		}
	}
}

func TestIsAllowedLabel(t *testing.T) {
	if !IsAllowedLabel(TaskEntityType, "concept") {
		t.Errorf("concept should be allowed")
	}
	if IsAllowedLabel(TaskEntityType, "PERSON") {
		t.Errorf("PERSON is the entity-role tag, not a document label")
	}
	if !IsAllowedLabel(TaskRelationship, "depends-on") {
		t.Errorf("depends-on should be allowed")
	}
	if IsAllowedLabel(TaskRelationship, "supports") {
		t.Errorf("supports is not a generic predicate")
	}
	if !IsAllowedLabel(TaskClaimSupport, "insufficient-evidence") {
		t.Errorf("insufficient-evidence should be allowed for claim-support")
	}
}

func TestAllowedLabelsForExcludesEntityRole(t *testing.T) {
	for _, l := range AllowedLabelsFor(TaskEntityType) {
		if strings.ToUpper(l) == l {
			t.Errorf("entity-type vocab should be lowercase; got %q", l)
		}
	}
}

func TestFixtureValidateRejectsBadLabel(t *testing.T) {
	f := Fixture{
		ID: "x", Task: TaskEntityType, SourceGroup: "g", Split: SplitTuning, Author: "a",
		ExpectedLabel: "alien",
	}
	if err := f.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestFixtureValidateRequiresExcerptsUnlessMissingEvidence(t *testing.T) {
	f := Fixture{
		ID: "x", Task: TaskClaimSupport, SourceGroup: "g", Split: SplitTuning, Author: "a",
		ExpectedLabel: "supported", Candidates: []Candidate{{ID: "title:0", Title: "A"}},
	}
	if err := f.Validate(); err == nil {
		t.Fatal("expected validation error for missing excerpts")
	}
}

func TestDistinctSourceGroups(t *testing.T) {
	fs := []Fixture{
		{ID: "1", SourceGroup: "b"},
		{ID: "2", SourceGroup: "a"},
		{ID: "3", SourceGroup: "a"},
		{ID: "4", SourceGroup: "c"},
	}
	got := DistinctSourceGroups(fs)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("at %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestReviewStatusDefaultUnreviewed(t *testing.T) {
	if ReviewUnreviewed != "unreviewed" {
		t.Errorf("expected default literal unreviewed")
	}
}
