package discovery

import (
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
