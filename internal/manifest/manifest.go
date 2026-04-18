package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Manifest represents the state manifest stored at .plexium/manifest.json
type Manifest struct {
	Version              int              `json:"version"`
	LastProcessedCommit  string           `json:"lastProcessedCommit"`
	LastPublishTimestamp string           `json:"lastPublishTimestamp"`
	Pages                []PageEntry      `json:"pages"`
	UnmanagedPages       []UnmanagedEntry `json:"unmanagedPages"`
}

// PageEntry represents a managed wiki page in the manifest.
//
// v2 adds knowledge-graph fields populated by the MarkedUp plugin. All
// v2 fields use `omitempty` so v1 manifests read/write cleanly: v1
// behaviour is preserved when no plugin populates them.
type PageEntry struct {
	WikiPath      string       `json:"wikiPath"`
	Title         string       `json:"title"`
	Ownership     string       `json:"ownership"` // managed | human-authored | co-maintained
	Section       string       `json:"section"`
	SourceFiles   []SourceFile `json:"sourceFiles"`
	GeneratedFrom []string     `json:"generatedFrom"`
	LastUpdated   string       `json:"lastUpdated"`
	UpdatedBy     string       `json:"updatedBy"`
	InboundLinks  []string     `json:"inboundLinks"`
	OutboundLinks []string     `json:"outboundLinks"`

	// Knowledge-graph fields (v2, populated by the MarkedUp plugin).
	//
	// Confidence uses the zero value (0.0) as "unset"; omitempty drops
	// zero values from the JSON output so v1 manifests round-trip
	// without stray confidence: 0 lines. A literal enricher-produced
	// "I have zero confidence" signal is semantically equivalent to
	// "unset" for current consumers — if we ever need to distinguish
	// the two cases we should switch this to *float64 at that point.
	EntityType    string            `json:"entityType,omitempty"`
	Entities      []EntityRef       `json:"entities,omitempty"`
	Relationships []RelationshipRef `json:"relationships,omitempty"`
	Confidence    float64           `json:"confidence,omitempty"`
	SemanticHints []string          `json:"semanticHints,omitempty"`
	LastEnriched  string            `json:"lastEnriched,omitempty"`
	EnrichedBy    string            `json:"enrichedBy,omitempty"`
}

// EntityRef is a reference to a named entity on a page. It mirrors the
// shape of schema.Entity in the markedup library without forcing a direct
// dependency on that schema at the manifest layer.
type EntityRef struct {
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
}

// RelationshipRef is a typed edge to another wiki page.
type RelationshipRef struct {
	Target   string  `json:"target"` // WikiPath of the target page
	Type     string  `json:"type"`   // e.g. "depends-on", "implements"
	Strength float64 `json:"strength,omitempty"`
}

// SourceFile represents a source file that feeds into a wiki page
type SourceFile struct {
	Path                string `json:"path"`
	Hash                string `json:"hash"`
	LastProcessedCommit string `json:"lastProcessedCommit"`
}

// UnmanagedEntry represents a wiki page not managed by Plexium
type UnmanagedEntry struct {
	WikiPath  string `json:"wikiPath"`
	FirstSeen string `json:"firstSeen"`
	Ownership string `json:"ownership"`
}

// Manager handles manifest CRUD operations
type Manager struct {
	path string
	mu   sync.RWMutex
}

// NewManager creates a new manifest manager for the given manifest path
func NewManager(path string) (*Manager, error) {
	if path == "" {
		return nil, fmt.Errorf("manifest path cannot be empty")
	}
	return &Manager{path: path}, nil
}

// DefaultPath returns the default manifest path relative to repo root
func DefaultPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".plexium", "manifest.json")
}

// Load reads the manifest from disk. Returns a new empty manifest if file doesn't exist.
func (m *Manager) Load() (*Manifest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Manifest{
				Version:        1,
				Pages:          []PageEntry{},
				UnmanagedPages: []UnmanagedEntry{},
			}, nil
		}
		return nil, fmt.Errorf("reading manifest %s: %w", m.path, err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest %s: %w", m.path, err)
	}

	// Ensure slices are non-nil
	if manifest.Pages == nil {
		manifest.Pages = []PageEntry{}
	}
	if manifest.UnmanagedPages == nil {
		manifest.UnmanagedPages = []UnmanagedEntry{}
	}

	return &manifest, nil
}

// Save writes the manifest to disk. Pages and UnmanagedPages are sorted
// by WikiPath before writing to ensure deterministic output.
func (m *Manager) Save(manifest *Manifest) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if manifest == nil {
		return fmt.Errorf("manifest cannot be nil")
	}

	// Sort pages by WikiPath for deterministic ordering
	sort.Slice(manifest.Pages, func(i, j int) bool {
		return manifest.Pages[i].WikiPath < manifest.Pages[j].WikiPath
	})
	// Sort unmanaged pages by WikiPath
	sort.Slice(manifest.UnmanagedPages, func(i, j int) bool {
		return manifest.UnmanagedPages[i].WikiPath < manifest.UnmanagedPages[j].WikiPath
	})

	// Ensure parent directory exists
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating manifest directory: %w", err)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}

	if err := os.WriteFile(m.path, data, 0644); err != nil {
		return fmt.Errorf("writing manifest %s: %w", m.path, err)
	}

	return nil
}

// NewEmptyManifest creates a fresh manifest with version 1
func NewEmptyManifest() *Manifest {
	return &Manifest{
		Version:        1,
		Pages:          []PageEntry{},
		UnmanagedPages: []UnmanagedEntry{},
	}
}

// GraphMetadata bundles the knowledge-graph fields set by an enrichment
// plugin. It's accepted by ApplyGraphMetadata to update a page in-place.
type GraphMetadata struct {
	EntityType    string
	Entities      []EntityRef
	Relationships []RelationshipRef
	Confidence    float64
	SemanticHints []string
	LastEnriched  string
	EnrichedBy    string
}

// ApplyGraphMetadata overwrites the v2 graph fields on the page entry with
// matching WikiPath. If no such page exists, returns false with no error.
// The manifest's Version is bumped to 2 only when g actually contains graph
// data — a no-op call with an empty GraphMetadata{} matches the page but
// does not silently upgrade a v1 manifest.
func (m *Manifest) ApplyGraphMetadata(wikiPath string, g GraphMetadata) bool {
	for i := range m.Pages {
		if m.Pages[i].WikiPath != wikiPath {
			continue
		}
		m.Pages[i].EntityType = g.EntityType
		m.Pages[i].Entities = g.Entities
		m.Pages[i].Relationships = g.Relationships
		m.Pages[i].Confidence = g.Confidence
		m.Pages[i].SemanticHints = g.SemanticHints
		m.Pages[i].LastEnriched = g.LastEnriched
		m.Pages[i].EnrichedBy = g.EnrichedBy
		if m.Version < 2 && hasGraphFields(g) {
			m.Version = 2
		}
		return true
	}
	return false
}

// hasGraphFields reports whether g carries any non-zero graph data. Used
// by ApplyGraphMetadata to avoid upgrading v1 manifests on no-op calls.
func hasGraphFields(g GraphMetadata) bool {
	return g.EntityType != "" ||
		len(g.Entities) > 0 ||
		len(g.Relationships) > 0 ||
		g.Confidence != 0 ||
		len(g.SemanticHints) > 0 ||
		g.LastEnriched != "" ||
		g.EnrichedBy != ""
}

// GraphMetadataForPage returns the graph metadata currently stored for
// the page with the given WikiPath. The bool is true when the page is
// tracked in the manifest (regardless of whether it has any graph
// fields set); false means the page is not tracked at all.
//
// Callers use this to check semantic equality before invoking
// ApplyGraphMetadata, so an unchanged enrichment result doesn't
// needlessly bump LastEnriched or rewrite the manifest on disk.
func (m *Manifest) GraphMetadataForPage(wikiPath string) (GraphMetadata, bool) {
	for i := range m.Pages {
		if m.Pages[i].WikiPath != wikiPath {
			continue
		}
		p := m.Pages[i]
		return GraphMetadata{
			EntityType:    p.EntityType,
			Entities:      p.Entities,
			Relationships: p.Relationships,
			Confidence:    p.Confidence,
			SemanticHints: p.SemanticHints,
			LastEnriched:  p.LastEnriched,
			EnrichedBy:    p.EnrichedBy,
		}, true
	}
	return GraphMetadata{}, false
}

// GraphMetadataSemanticEqual reports whether two GraphMetadata values
// are equivalent on their semantic fields (EntityType, Entities,
// Relationships, Confidence, SemanticHints, EnrichedBy). LastEnriched is
// deliberately excluded — it's a run-timestamp, not a content field, and
// equality on the rest means the enrichment produced no new information.
//
// Slice comparisons are order-sensitive. Callers relying on this for
// idempotency should ensure the enrichment source produces deterministic
// ordering; MarkedUp does.
func GraphMetadataSemanticEqual(a, b GraphMetadata) bool {
	if a.EntityType != b.EntityType {
		return false
	}
	if a.Confidence != b.Confidence {
		return false
	}
	if a.EnrichedBy != b.EnrichedBy {
		return false
	}
	if len(a.Entities) != len(b.Entities) {
		return false
	}
	for i := range a.Entities {
		if a.Entities[i] != b.Entities[i] {
			return false
		}
	}
	if len(a.Relationships) != len(b.Relationships) {
		return false
	}
	for i := range a.Relationships {
		if a.Relationships[i] != b.Relationships[i] {
			return false
		}
	}
	if len(a.SemanticHints) != len(b.SemanticHints) {
		return false
	}
	for i := range a.SemanticHints {
		if a.SemanticHints[i] != b.SemanticHints[i] {
			return false
		}
	}
	return true
}
