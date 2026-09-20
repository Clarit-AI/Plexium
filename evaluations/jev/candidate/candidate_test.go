package candidate

import (
	"sort"
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

func TestGenerateTitleMatch(t *testing.T) {
	pool := Pool{
		{Index: 0, Title: "Plexium"},
		{Index: 1, Title: "MarkedUp"},
		{Index: 2, Title: "Traycer"},
	}
	ex := []protocol.Excerpt{
		{ID: "e1", Text: "Plexium is a self-documenting repo system."},
	}
	list := Generate(protocol.TaskEntityType, pool, ex)
	if len(list.Candidates) == 0 {
		t.Fatal("expected at least one candidate")
	}
	if list.Candidates[0].Title != "Plexium" {
		t.Fatalf("first candidate should be Plexium, got %q", list.Candidates[0].Title)
	}
	if list.Candidates[0].Role != "title" {
		t.Fatalf("first candidate role should be title, got %q", list.Candidates[0].Role)
	}
}

func TestGenerateAliasMatchBeatsCooccur(t *testing.T) {
	pool := Pool{
		{Index: 0, Title: "DecisionMaker", Aliases: []string{"JevDecision"}},
		{Index: 1, Title: "Decision Theory"},
	}
	ex := []protocol.Excerpt{
		{ID: "e1", Text: "The JevDecision product model is documented here."},
	}
	list := Generate(protocol.TaskEntityType, pool, ex)
	if len(list.Candidates) == 0 {
		t.Fatal("expected alias match")
	}
	if list.Candidates[0].Title != "DecisionMaker" {
		t.Fatalf("expected DecisionMaker (alias match), got %q", list.Candidates[0].Title)
	}
	if list.Candidates[0].Role != "alias" {
		t.Fatalf("expected role alias, got %q", list.Candidates[0].Role)
	}
}

func TestGenerateWikilinkMatchUsedWhenNoTitle(t *testing.T) {
	pool := Pool{
		{Index: 0, Title: "Completely Different", Wikilink: "JevDecision"},
	}
	ex := []protocol.Excerpt{
		{ID: "e1", Text: "Reference: [[JevDecision]] for details."},
	}
	list := Generate(protocol.TaskEntityType, pool, ex)
	if len(list.Candidates) == 0 {
		t.Fatal("expected wikilink match")
	}
	if list.Candidates[0].Role != "wikilink" {
		t.Fatalf("expected role wikilink, got %q", list.Candidates[0].Role)
	}
}

func TestGenerateTruncatesEntityTypeCap(t *testing.T) {
	pool := make(Pool, 20)
	for i := range pool {
		pool[i] = PoolEntry{Index: i, Title: "Entity" + strings.Repeat("Z", i+1)}
	}
	ex := []protocol.Excerpt{{ID: "e1", Text: "EntityZZZZZZZZZZZZZZZZZZZZZZZZZ appears here."}}
	list := Generate(protocol.TaskEntityType, pool, ex)
	if len(list.Candidates) != MaxEntities {
		t.Fatalf("expected %d candidates, got %d", MaxEntities, len(list.Candidates))
	}
	if !list.Truncated {
		t.Fatal("expected truncation flag")
	}
}

func TestGenerateStableTieBreakingByIndex(t *testing.T) {
	pool := Pool{
		{Index: 9, Title: "SameTitle"},
		{Index: 1, Title: "SameTitle"},
		{Index: 5, Title: "SameTitle"},
	}
	ex := []protocol.Excerpt{{ID: "e1", Text: "SameTitle mentioned here."}}
	list := Generate(protocol.TaskEntityType, pool, ex)
	if len(list.Candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(list.Candidates))
	}
	// Tie-broken by ascending pool Index; verify stable ordering across runs.
	got1 := candidateTitles(list)
	got2 := candidateTitles(Generate(protocol.TaskEntityType, pool, ex))
	if !equalSlices(got1, got2) {
		t.Fatalf("non-deterministic ordering: %v vs %v", got1, got2)
	}
	// Each candidate ID encodes the index; verify ascending.
	indices := candidateIndices(list)
	if !sort.SliceIsSorted(indices, func(i, j int) bool { return indices[i] < indices[j] }) {
		t.Fatalf("expected indices sorted, got %v", indices)
	}
}

func TestDirectedEdgesAreBoundedAndSkipSelf(t *testing.T) {
	pool := Pool{{Index: 0, Title: "A"}, {Index: 1, Title: "B"}, {Index: 2, Title: "C"}}
	ex := []protocol.Excerpt{{ID: "e1", Text: "A B C"}}
	list := Generate(protocol.TaskRelationship, pool, ex)
	edges := DirectedEdges(list, MaxDirectedEdgeCandidates)
	for _, e := range edges {
		if e.SourceID == e.TargetID {
			t.Fatalf("self-edge not allowed: %s -> %s", e.SourceID, e.TargetID)
		}
	}
	if len(edges) != 6 { // 3 * (3-1)
		t.Fatalf("expected 6 directed edges, got %d", len(edges))
	}
}

func TestDirectedEdgesRespectsMaxCap(t *testing.T) {
	pool := make(Pool, 8)
	for i := range pool {
		pool[i] = PoolEntry{Index: i, Title: "T"}
	}
	ex := []protocol.Excerpt{{ID: "e1", Text: "T T T T T T T T"}}
	list := Generate(protocol.TaskRelationship, pool, ex)
	edges := DirectedEdges(list, MaxDirectedEdgeCandidates)
	if len(edges) != MaxDirectedEdgeCandidates {
		t.Fatalf("expected %d edges, got %d", MaxDirectedEdgeCandidates, len(edges))
	}
}

func TestRecallEntity(t *testing.T) {
	pool := Pool{
		{Index: 0, Title: "Plexium"},
		{Index: 1, Title: "MarkedUp"},
		{Index: 2, Title: "Traycer"},
	}
	ex := []protocol.Excerpt{{ID: "e1", Text: "Plexium is a self-documenting repo system."}}
	list := Generate(protocol.TaskEntityType, pool, ex)
	recall, _ := Recall(list, []string{"Plexium", "MarkedUp"}, nil)
	if recall != 0.5 {
		t.Fatalf("expected 0.5 recall, got %v", recall)
	}
}

func TestRecallEdge(t *testing.T) {
	pool := Pool{
		{Index: 0, Title: "A"},
		{Index: 1, Title: "B"},
		{Index: 2, Title: "C"},
	}
	ex := []protocol.Excerpt{{ID: "e1", Text: "A B C"}}
	list := Generate(protocol.TaskRelationship, pool, ex)
	edges := DirectedEdges(list, MaxDirectedEdgeCandidates)
	gold := []DirectedEdgeCandidate{{SourceID: edges[0].SourceID, TargetID: edges[0].TargetID}}
	_, edgeRecall := Recall(list, nil, gold)
	if edgeRecall != 1.0 {
		t.Fatalf("expected 1.0 edge recall, got %v", edgeRecall)
	}
}

func candidateTitles(s Shortlist) []string {
	out := make([]string, len(s.Candidates))
	for i, c := range s.Candidates {
		out[i] = c.Title
	}
	return out
}

func candidateIndices(s Shortlist) []int {
	out := make([]int, len(s.Candidates))
	for i, c := range s.Candidates {
		idx := strings.LastIndex(c.ID, ":")
		if idx < 0 {
			t := s
			_ = t
			continue
		}
		n := 0
		for _, r := range c.ID[idx+1:] {
			n = n*10 + int(r-'0')
		}
		out[i] = n
	}
	return out
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var _ = sort.Slice
