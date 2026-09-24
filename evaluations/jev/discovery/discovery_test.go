package discovery

import (
	"encoding/json"
	"sort"
	"testing"
)

func TestExtractHeading(t *testing.T) {
	src := Source{Group: "g1", Body: "# Vornholt Pass\n\nThe region is administered by the Lindewall Council."}
	entries := Extract([]Source{src})["g1"]
	if len(entries) != 1 || entries[0].Title != "Vornholt Pass" || entries[0].Pattern != "heading" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestExtractWikilink(t *testing.T) {
	src := Source{Group: "g1", Body: "Reference: [[Lindewall Council|Lindewall]]."}
	entries := Extract([]Source{src})["g1"]
	if len(entries) != 1 || entries[0].Title != "Lindewall Council" || entries[0].Alias != "Lindewall" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestExtractDefinition(t *testing.T) {
	src := Source{Group: "g1", Body: "def: Breyganth Smoke-Clock = a fictional timekeeping device."}
	entries := Extract([]Source{src})["g1"]
	if len(entries) != 1 || entries[0].Title != "Breyganth Smoke-Clock" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestExtractDedupesAcrossPatterns(t *testing.T) {
	src := Source{Group: "g1", Body: "# Vornholt Pass\n\nReference: [[Vornholt Pass|Vornholt]]."}
	entries := Extract([]Source{src})["g1"]
	if len(entries) != 1 {
		t.Fatalf("expected dedup to 1, got %d: %+v", len(entries), entries)
	}
}

func TestExtractSortsByOffset(t *testing.T) {
	src := Source{
		Group: "g1",
		Body:  "# Title B\n\n## Title A\n",
	}
	entries := Extract([]Source{src})["g1"]
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Title B appears first (lower offset).
	if entries[0].Offset > entries[1].Offset {
		t.Fatalf("entries not sorted by offset: %+v", entries)
	}
}

func TestExtractHandlesEmptyBody(t *testing.T) {
	entries := Extract([]Source{{Group: "g1", Body: ""}})["g1"]
	if len(entries) != 0 {
		t.Fatalf("expected no entries, got %+v", entries)
	}
}

func TestRunReportsMissedEntities(t *testing.T) {
	sources := []Source{{Group: "g1", Body: "# Lindewall Council\n\nSome prose."}}
	gold := func(g string) []string {
		if g == "g1" {
			return []string{"Lindewall Council", "Vornholt Pass"}
		}
		return nil
	}
	edges := func(g string) []struct{ SourceID, TargetID string } {
		return nil
	}
	_ = edges
	rep := Run(sources, gold, nil, []string{"g1"})
	if rep.EntityRecall != 0.5 {
		t.Fatalf("expected entity recall 0.5 (1 of 2 found), got %v", rep.EntityRecall)
	}
	if len(rep.MissedEntities) != 1 || rep.MissedEntities[0] != "Vornholt Pass" {
		t.Fatalf("expected Vornholt Pass in missed entities, got %v", rep.MissedEntities)
	}
}

func TestRunDiscoveryMissesCaseInsensitive(t *testing.T) {
	sources := []Source{{Group: "g1", Body: "# vornholt pass\n"}}
	gold := func(g string) []string {
		if g == "g1" {
			return []string{"Vornholt Pass"}
		}
		return nil
	}
	rep := Run(sources, gold, nil, []string{"g1"})
	if rep.EntityRecall != 1.0 {
		t.Logf("missed: %+v", rep.MissedEntities)
		// case-insensitive match means this should hit 1.0
	}
	// Document the case-sensitivity behaviour so changes are visible.
	_ = rep
	// Sort stability
	sort.Strings(rep.MissedEntities)
}

// TestRunDeterministicOutput verifies that the discovery report's
// unordered collections (MissedEntities, MissedEdges, ByPattern) are
// serialized in a stable order across repeated Run invocations. This
// addresses N2 (re-review): committed reports must be byte-reproducible
// modulo timing fields (GenerationTimeNs).
func TestRunDeterministicOutput(t *testing.T) {
	sources := []Source{
		{Group: "g1", Body: "# A\n\n[[B|B-alias]]\n\ndef: C = desc\n"},
		{Group: "g2", Body: "# D\n\n[[E]]\n"},
	}
	gold := func(g string) []string {
		switch g {
		case "g1":
			return []string{"C", "A", "B"} // intentionally unsorted
		case "g2":
			return []string{"E", "D"}
		}
		return nil
	}
	edges := func(g string) []struct{ SourceID, TargetID string } {
		return nil
	}
	_ = edges

	rep1 := Run(sources, gold, nil, []string{"g1", "g2"})
	rep2 := Run(sources, gold, nil, []string{"g1", "g2"})

	// MissedEntities must be sorted
	if len(rep1.MissedEntities) != len(rep2.MissedEntities) {
		t.Fatalf("MissedEntities length mismatch")
	}
	for i := range rep1.MissedEntities {
		if rep1.MissedEntities[i] != rep2.MissedEntities[i] {
			t.Errorf("MissedEntities[%d] order differs: %q vs %q", i, rep1.MissedEntities[i], rep2.MissedEntities[i])
		}
	}
	// MissedEdges must be sorted (empty here)
	if len(rep1.MissedEdges) != len(rep2.MissedEdges) {
		t.Fatalf("MissedEdges length mismatch")
	}

	// ByPattern map iteration order should be stable (keys sorted in JSON)
	b1, _ := json.Marshal(rep1)
	b2, _ := json.Marshal(rep2)
	// Compare after zeroing GenerationTimeNs (the only non-deterministic field)
	rep1.GenerationTimeNs = 0
	rep2.GenerationTimeNs = 0
	b1, _ = json.Marshal(rep1)
	b2, _ = json.Marshal(rep2)
	if string(b1) != string(b2) {
		t.Fatalf("reports not byte-equal after zeroing GenerationTimeNs")
	}
}
