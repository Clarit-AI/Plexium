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
	Version               int              `json:"version"`
	LastProcessedCommit   string           `json:"lastProcessedCommit"`
	LastPublishTimestamp  string           `json:"lastPublishTimestamp"`
	Pages                 []PageEntry      `json:"pages"`
	UnmanagedPages        []UnmanagedEntry `json:"unmanagedPages"`
}

// PageEntry represents a managed wiki page in the manifest
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
	path    string
	mu      sync.RWMutex
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
