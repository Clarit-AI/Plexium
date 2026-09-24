package candidateex

import (
	"fmt"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

func sampleFixtures() []protocol.Fixture {
	return []protocol.Fixture{
		{
			ID: "f1", SourceGroup: "g1", Task: protocol.TaskClaimSupport, Split: protocol.SplitTuning,
			Excerpts: []protocol.Excerpt{{ID: "e1", Text: "Plexium is a self-documenting repo system."}},
		},
		{
			ID: "f2", SourceGroup: "g2", Task: protocol.TaskClaimSupport, Split: protocol.SplitTuning,
			Excerpts: []protocol.Excerpt{{ID: "e1", Text: "MarkedUp is a knowledge graph library."}},
		},
	}
}

func TestRunInvokesCandidateGenerate(t *testing.T) {
	pools := map[string][]PoolEntry{
		"g1": {{Index: 0, Title: "Plexium"}, {Index: 1, Title: "MarkedUp"}},
		"g2": {{Index: 0, Title: "MarkedUp"}, {Index: 1, Title: "Plexium"}},
	}
	golds := map[string][]string{
		"g1": {"Plexium"},
		"g2": {"MarkedUp"},
	}
	opts := RunOptions{
		Pool:         func(g string) []PoolEntry { return pools[g] },
		GoldEntities: func(g string) []string { return golds[g] },
	}
	rep, err := Run(sampleFixtures(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.FixtureCount != 2 {
		t.Fatalf("expected 2 fixtures, got %d", rep.FixtureCount)
	}
	if rep.Overall.EntityRecall != 1.0 {
		t.Fatalf("expected perfect entity recall, got %v", rep.Overall.EntityRecall)
	}
}

func TestRunDeliberateGoldOmitReportsMiss(t *testing.T) {
	// P2 finding #9 / directive #5: a "gold omission" case must report
	// recall that is NOT equal to 1.0 because the gold is intentionally
	// not in the pool.
	pools := map[string][]PoolEntry{
		"g1": {{Index: 0, Title: "Plexium"}, {Index: 1, Title: "MarkedUp"}},
		"g2": {{Index: 0, Title: "MarkedUp"}},
	}
	golds := map[string][]string{
		"g1": {"Plexium"},
		"g2": {"Plexium"}, // NOT in g2's pool
	}
	opts := RunOptions{
		Pool:         func(g string) []PoolEntry { return pools[g] },
		GoldEntities: func(g string) []string { return golds[g] },
		DeliberateGoldOmit: func(g string) string {
			if g == "g2" {
				return "Plexium"
			}
			return ""
		},
	}
	rep, err := Run(sampleFixtures(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// The g2 fixture should report 0 recall (gold not in pool).
	sg2 := rep.BySourceGroup["g2"]
	if sg2.EntityRecall != 0 {
		t.Fatalf("expected 0 recall for omitted gold, got %v", sg2.EntityRecall)
	}
	if len(rep.OmittedGoldFixtures) != 1 || rep.OmittedGoldFixtures[0] != "f2" {
		t.Fatalf("expected f2 in omitted list, got %v", rep.OmittedGoldFixtures)
	}
}

func TestRunTruncationFlagRecorded(t *testing.T) {
	// Build a pool large enough to trigger MaxEntities truncation. Each
	// pool entry's title must appear in the excerpt so the candidate
	// ranker matches it.
	var pool []PoolEntry
	var haystack []string
	for i := 0; i < 30; i++ {
		title := fmt.Sprintf("Candi%d", i)
		pool = append(pool, PoolEntry{Index: i, Title: title})
		haystack = append(haystack, title)
	}
	excerpt := ""
	for _, t := range haystack {
		excerpt += t + " "
	}
	pools := map[string][]PoolEntry{"g1": pool}
	golds := map[string][]string{"g1": {"Candi0"}}
	opts := RunOptions{
		Pool:         func(g string) []PoolEntry { return pools[g] },
		GoldEntities: func(g string) []string { return golds[g] },
	}
	rep, err := Run([]protocol.Fixture{
		{ID: "f1", SourceGroup: "g1", Task: protocol.TaskEntityType, Split: protocol.SplitTuning,
			Excerpts: []protocol.Excerpt{{ID: "e1", Text: excerpt}}},
	}, opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(rep.TruncatedFixtures) == 0 {
		t.Fatalf("expected at least one truncated fixture, got 0")
	}
}

func TestRunRequiresPool(t *testing.T) {
	_, err := Run(nil, RunOptions{})
	if err == nil {
		t.Fatal("expected error when Pool is nil")
	}
}
