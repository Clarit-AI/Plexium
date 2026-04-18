package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Verifies a v1 manifest on disk (no graph fields) round-trips cleanly:
// loading, saving, and loading again preserves Version=1 and doesn't
// introduce graph fields.
func TestLoad_V1Manifest_BackwardCompatible(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	v1 := `{
  "version": 1,
  "pages": [
    {
      "wikiPath": "modules/auth.md",
      "title": "Auth",
      "ownership": "managed",
      "section": "modules",
      "sourceFiles": [],
      "generatedFrom": [],
      "lastUpdated": "2026-01-01T00:00:00Z",
      "updatedBy": "plexium-convert",
      "inboundLinks": [],
      "outboundLinks": []
    }
  ],
  "unmanagedPages": []
}`
	if err := os.WriteFile(path, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := mgr.Load()
	if err != nil {
		t.Fatalf("load v1: %v", err)
	}
	if m.Version != 1 {
		t.Errorf("expected Version=1, got %d", m.Version)
	}
	if len(m.Pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(m.Pages))
	}
	if m.Pages[0].EntityType != "" || len(m.Pages[0].Entities) != 0 {
		t.Errorf("v1 load should leave graph fields empty")
	}

	if err := mgr.Save(m); err != nil {
		t.Fatal(err)
	}

	// Reload and confirm graph fields are still not present.
	reloaded, err := mgr.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Version != 1 {
		t.Errorf("save/reload should preserve Version=1, got %d", reloaded.Version)
	}

	// Confirm omitempty: raw bytes should not contain "entityType" or
	// "entities" keys after round-trip of a v1 manifest.
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "\"entityType\"") {
		t.Errorf("unexpected entityType key in v1 round-trip: %s", raw)
	}
	if strings.Contains(string(raw), "\"entities\"") {
		t.Errorf("unexpected entities key in v1 round-trip: %s", raw)
	}
	if strings.Contains(string(raw), "\"confidence\"") {
		t.Errorf("unexpected confidence key in v1 round-trip: %s", raw)
	}
}

func TestApplyGraphMetadata_UpdatesPageAndBumpsVersion(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Pages: []PageEntry{
			{WikiPath: "modules/auth.md", Title: "Auth", Ownership: "managed"},
		},
	}

	ok := m.ApplyGraphMetadata("modules/auth.md", GraphMetadata{
		EntityType: "module",
		Entities: []EntityRef{
			{Name: "JWT", Role: "algorithm"},
		},
		Relationships: []RelationshipRef{
			{Target: "modules/crypto.md", Type: "depends-on", Strength: 0.8},
		},
		Confidence:    0.9,
		SemanticHints: []string{"authentication", "session"},
		LastEnriched:  "2026-04-18T00:00:00Z",
		EnrichedBy:    "markedup-v1",
	})

	if !ok {
		t.Fatal("expected ApplyGraphMetadata to return true for existing page")
	}
	if m.Version != 2 {
		t.Errorf("expected Version bumped to 2, got %d", m.Version)
	}
	p := m.Pages[0]
	if p.EntityType != "module" {
		t.Errorf("EntityType=%q", p.EntityType)
	}
	if len(p.Entities) != 1 || p.Entities[0].Name != "JWT" {
		t.Errorf("Entities=%+v", p.Entities)
	}
	if len(p.Relationships) != 1 || p.Relationships[0].Target != "modules/crypto.md" {
		t.Errorf("Relationships=%+v", p.Relationships)
	}
	if p.Confidence != 0.9 {
		t.Errorf("Confidence=%v", p.Confidence)
	}
	if p.EnrichedBy != "markedup-v1" {
		t.Errorf("EnrichedBy=%q", p.EnrichedBy)
	}
}

func TestApplyGraphMetadata_UnknownPageReturnsFalse(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Pages: []PageEntry{
			{WikiPath: "existing.md"},
		},
	}
	ok := m.ApplyGraphMetadata("missing.md", GraphMetadata{EntityType: "module"})
	if ok {
		t.Error("expected false for missing page")
	}
	if m.Version != 1 {
		t.Errorf("Version should not bump when no page updated, got %d", m.Version)
	}
}

// Confirm that once v2 fields are written, a round-trip through disk
// preserves them including nested entities/relationships.
func TestV2Manifest_RoundTripsGraphFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	mgr, _ := NewManager(path)

	m := NewEmptyManifest()
	m.Pages = append(m.Pages, PageEntry{
		WikiPath:  "modules/auth.md",
		Title:     "Auth",
		Ownership: "managed",
	})
	m.ApplyGraphMetadata("modules/auth.md", GraphMetadata{
		EntityType: "module",
		Entities:   []EntityRef{{Name: "JWT"}, {Name: "OAuth", Role: "protocol"}},
		Relationships: []RelationshipRef{
			{Target: "modules/crypto.md", Type: "depends-on", Strength: 0.8},
		},
		Confidence:    0.9,
		SemanticHints: []string{"authentication"},
		EnrichedBy:    "markedup-v1",
	})

	if err := mgr.Save(m); err != nil {
		t.Fatal(err)
	}

	reloaded, err := mgr.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Version != 2 {
		t.Errorf("expected Version=2 after graph write, got %d", reloaded.Version)
	}
	if len(reloaded.Pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(reloaded.Pages))
	}
	p := reloaded.Pages[0]
	if p.EntityType != "module" {
		t.Errorf("EntityType=%q", p.EntityType)
	}
	if len(p.Entities) != 2 {
		t.Errorf("Entities count=%d", len(p.Entities))
	}
	if p.Entities[1].Role != "protocol" {
		t.Errorf("Entities[1].Role=%q", p.Entities[1].Role)
	}
	if len(p.Relationships) != 1 || p.Relationships[0].Strength != 0.8 {
		t.Errorf("Relationships=%+v", p.Relationships)
	}

	// Confirm omitempty: a second page with no graph fields stays clean.
	m2 := reloaded
	m2.Pages = append(m2.Pages, PageEntry{WikiPath: "plain.md", Title: "Plain", Ownership: "managed"})
	if err := mgr.Save(m2); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	pagesAny, _ := decoded["pages"].([]any)
	for _, p := range pagesAny {
		pm := p.(map[string]any)
		if pm["wikiPath"] == "plain.md" {
			for _, k := range []string{"entityType", "entities", "relationships", "confidence", "semanticHints", "lastEnriched", "enrichedBy"} {
				if _, present := pm[k]; present {
					t.Errorf("plain page should not carry graph key %q", k)
				}
			}
		}
	}
}
