package validation

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/compile"
	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/lint"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// C1: Manifest struct has all fields expected by downstream consumers
// =============================================================================

func TestContract_ManifestStructFields(t *testing.T) {
	// This test locks the manifest schema. If fields are added/removed/renamed,
	// this test forces a conscious review of downstream impact.

	mType := reflect.TypeOf(manifest.PageEntry{})

	expectedFields := map[string]string{
		"WikiPath":      "string",
		"Title":         "string",
		"Ownership":     "string",
		"Section":       "string",
		"SourceFiles":   "[]manifest.SourceFile",
		"GeneratedFrom": "[]string",
		"LastUpdated":   "string",
		"UpdatedBy":     "string",
		"InboundLinks":  "[]string",
		"OutboundLinks": "[]string",
	}

	for name, expectedType := range expectedFields {
		field, ok := mType.FieldByName(name)
		assert.True(t, ok, "PageEntry missing expected field: %s", name)
		if ok {
			assert.Equal(t, expectedType, field.Type.String(),
				"PageEntry.%s type changed from %s to %s", name, expectedType, field.Type.String())
		}
	}

	// Verify no unexpected fields were added (would break serialization assumptions)
	assert.Equal(t, len(expectedFields), mType.NumField(),
		"PageEntry field count changed — review downstream consumers (lint, compile, publish, hooks)")
}

func TestContract_SourceFileStructFields(t *testing.T) {
	sfType := reflect.TypeOf(manifest.SourceFile{})

	expectedFields := map[string]string{
		"Path":                "string",
		"Hash":                "string",
		"LastProcessedCommit": "string",
	}

	for name, expectedType := range expectedFields {
		field, ok := sfType.FieldByName(name)
		assert.True(t, ok, "SourceFile missing expected field: %s", name)
		if ok {
			assert.Equal(t, expectedType, field.Type.String(),
				"SourceFile.%s type changed", name)
		}
	}
	assert.Equal(t, len(expectedFields), sfType.NumField())
}

func TestContract_UnmanagedEntryStructFields(t *testing.T) {
	ueType := reflect.TypeOf(manifest.UnmanagedEntry{})

	expectedFields := map[string]string{
		"WikiPath":  "string",
		"FirstSeen": "string",
		"Ownership": "string",
	}

	for name, expectedType := range expectedFields {
		field, ok := ueType.FieldByName(name)
		assert.True(t, ok, "UnmanagedEntry missing expected field: %s", name)
		if ok {
			assert.Equal(t, expectedType, field.Type.String())
		}
	}
	assert.Equal(t, len(expectedFields), ueType.NumField())
}

func TestContract_ManifestTopLevelFields(t *testing.T) {
	mType := reflect.TypeOf(manifest.Manifest{})

	expectedFields := []string{
		"Version", "LastProcessedCommit", "LastPublishTimestamp",
		"Pages", "UnmanagedPages",
	}

	for _, name := range expectedFields {
		_, ok := mType.FieldByName(name)
		assert.True(t, ok, "Manifest missing expected field: %s", name)
	}
	assert.Equal(t, len(expectedFields), mType.NumField(),
		"Manifest field count changed — review all consumers")
}

// =============================================================================
// C2: Ownership values are consistent across the system
// =============================================================================

func TestContract_OwnershipValuesValid(t *testing.T) {
	// These are the only valid ownership values across the entire system
	validValues := map[string]bool{
		"managed":        true,
		"human-authored": true,
		"co-maintained":  true,
	}

	// Test that manifest CRUD enforces valid ownership implicitly
	// by checking the human-authored protection path
	f := NewFixture(t)
	f.MkDir(".plexium")

	mgr, err := manifest.NewManager(filepath.Join(f.Root, ".plexium", "manifest.json"))
	require.NoError(t, err)

	// Save a manifest with all three ownership types
	m := &manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{
			{WikiPath: "managed.md", Title: "Managed", Ownership: "managed"},
			{WikiPath: "human.md", Title: "Human", Ownership: "human-authored"},
			{WikiPath: "co.md", Title: "Co", Ownership: "co-maintained"},
		},
	}
	err = mgr.Save(m)
	require.NoError(t, err)

	loaded, err := mgr.Load()
	require.NoError(t, err)

	for _, p := range loaded.Pages {
		assert.True(t, validValues[p.Ownership],
			"page %s has invalid ownership: %q", p.WikiPath, p.Ownership)
	}
}

func TestContract_HumanAuthoredProtectionConsistent(t *testing.T) {
	// Verify that UpsertPage protects human-authored from managed overwrite
	f := NewFixture(t)
	f.MkDir(".plexium")

	mgr, err := manifest.NewManager(filepath.Join(f.Root, ".plexium", "manifest.json"))
	require.NoError(t, err)

	// Create initial human-authored page
	m := &manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{
			{WikiPath: "protected.md", Title: "Protected", Ownership: "human-authored"},
		},
	}
	err = mgr.Save(m)
	require.NoError(t, err)

	// Attempt managed overwrite — should fail
	err = mgr.UpsertPage(manifest.PageEntry{
		WikiPath: "protected.md", Title: "Overwrite", Ownership: "managed",
	})
	assert.Error(t, err)

	// co-maintained overwrite of human-authored — should succeed
	err = mgr.UpsertPage(manifest.PageEntry{
		WikiPath: "protected.md", Title: "Co-Edit", Ownership: "co-maintained",
	})
	assert.NoError(t, err, "co-maintained should be able to update human-authored pages")
}

// =============================================================================
// C3: Config struct fields consumed by all commands
// =============================================================================

func TestContract_ConfigStructFields(t *testing.T) {
	cfgType := reflect.TypeOf(config.Config{})

	expectedTopLevel := []string{
		"Version", "Repo", "Sources", "Agents", "Wiki", "Taxonomy",
		"Publish", "Sync", "Enforcement", "Integrations", "Reports",
		"GitHubWiki", "Sensitivity", "AssistiveAgent", "Daemon", "Retry",
	}

	for _, name := range expectedTopLevel {
		_, ok := cfgType.FieldByName(name)
		assert.True(t, ok, "Config missing expected field: %s", name)
	}
}

func TestContract_ConfigWikiFieldsUsedByCompileAndLint(t *testing.T) {
	// Compile and lint both derive wikiRoot from config.Wiki.Root
	// Verify the field exists and the type matches expectations

	wikiType := reflect.TypeOf(config.Wiki{})
	expectedFields := []string{"Root", "Home", "Sidebar", "Footer", "Log", "Index", "Schema"}
	for _, name := range expectedFields {
		field, ok := wikiType.FieldByName(name)
		assert.True(t, ok, "Wiki config missing field: %s", name)
		if ok {
			assert.Equal(t, "string", field.Type.String(),
				"Wiki.%s should be a string", name)
		}
	}
}

// =============================================================================
// C4: Compile consumes manifest PageEntry correctly
// =============================================================================

func TestContract_CompileUsesManifestSectionAndTitle(t *testing.T) {
	f := InitializedFixture(t)

	m := &manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{
			{
				WikiPath:  "modules/my-module.md",
				Title:     "My Module Title",
				Ownership: "managed",
				Section:   "Modules",
			},
		},
	}
	f.WriteManifest(m)

	c := compile.NewCompiler(f.Root, false)
	_, err := c.Compile()
	require.NoError(t, err)

	index := f.ReadFile(".wiki/_index.md")

	// Compile should use Section for grouping and Title for display
	assert.Contains(t, index, "## Modules", "compile should group by Section")
	assert.Contains(t, index, "My Module Title", "compile should use Title field")
	assert.Contains(t, index, "[[my-module]]", "compile should extract slug from WikiPath")
}

func TestContract_CompileHandlesEmptySection(t *testing.T) {
	f := InitializedFixture(t)

	m := &manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{
			{
				WikiPath:  "misc/orphan.md",
				Title:     "Orphan Page",
				Ownership: "managed",
				Section:   "", // empty section
			},
		},
	}
	f.WriteManifest(m)

	c := compile.NewCompiler(f.Root, false)
	_, err := c.Compile()
	require.NoError(t, err)

	index := f.ReadFile(".wiki/_index.md")
	assert.Contains(t, index, "## Uncategorized",
		"pages with empty section should go to Uncategorized")
}

// =============================================================================
// C5: Lint report struct matches JSON contract
// =============================================================================

func TestContract_LintReportJSONShape(t *testing.T) {
	f := InitializedFixture(t)

	cfg, err := config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	require.NoError(t, err)

	linter := lint.NewLinter(f.Root, cfg)
	report, err := linter.RunDeterministic()
	require.NoError(t, err)

	data, err := report.ToJSON()
	require.NoError(t, err)

	// Parse back to raw JSON and verify expected structure
	var raw map[string]json.RawMessage
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	expectedKeys := []string{"type", "timestamp", "deterministic", "summary"}
	for _, key := range expectedKeys {
		_, exists := raw[key]
		assert.True(t, exists, "lint report JSON missing key: %s", key)
	}

	// Verify deterministic sub-structure
	var det map[string]json.RawMessage
	err = json.Unmarshal(raw["deterministic"], &det)
	require.NoError(t, err)

	detKeys := []string{
		"brokenLinks", "orphanPages", "staleCandidates",
		"missingSourceFiles", "manifestDrift", "sidebarIssues", "frontmatterIssues",
	}
	for _, key := range detKeys {
		_, exists := det[key]
		assert.True(t, exists, "deterministic report missing key: %s", key)
	}
}

func TestContract_LintExitCodes(t *testing.T) {
	// Exit code contract: 0 = clean, 1 = errors, 2 = warnings only
	report := &lint.LintReport{
		Summary: lint.LintSummary{Errors: 0, Warnings: 0},
	}
	assert.Equal(t, 0, report.ExitCode(), "clean report should return exit code 0")

	report.Summary.Errors = 1
	assert.Equal(t, 1, report.ExitCode(), "errors should return exit code 1")

	report.Summary.Errors = 0
	report.Summary.Warnings = 3
	assert.Equal(t, 2, report.ExitCode(), "warnings-only should return exit code 2")
}

// =============================================================================
// C6: Config roundtrip — load → validate → use
// =============================================================================

func TestContract_ConfigLoadValidateMinimal(t *testing.T) {
	f := NewFixture(t)
	f.WriteConfig(`version: 1
sources:
  include:
    - "**/*.go"
wiki:
  root: .wiki
`)

	cfg, err := config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	require.NoError(t, err)

	assert.Equal(t, 1, cfg.Version)
	assert.Equal(t, ".wiki", cfg.Wiki.Root)
	assert.Contains(t, cfg.Sources.Include, "**/*.go")
}

func TestContract_ConfigValidationFailsOnMissingRequired(t *testing.T) {
	f := NewFixture(t)

	// Missing version
	f.WriteConfig(`sources:
  include:
    - "**/*.go"
wiki:
  root: .wiki
`)
	_, err := config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	assert.Error(t, err, "should fail on missing version")

	// Missing wiki.root
	f.WriteConfig(`version: 1
sources:
  include:
    - "**/*.go"
`)
	_, err = config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	assert.Error(t, err, "should fail on missing wiki.root")

	// Missing sources.include
	f.WriteConfig(`version: 1
wiki:
  root: .wiki
`)
	_, err = config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	assert.Error(t, err, "should fail on missing sources.include")
}

// =============================================================================
// C7: Manifest version is preserved
// =============================================================================

func TestContract_ManifestVersionPreserved(t *testing.T) {
	f := NewFixture(t)
	f.MkDir(".plexium")

	mgr, err := manifest.NewManager(filepath.Join(f.Root, ".plexium", "manifest.json"))
	require.NoError(t, err)

	m := &manifest.Manifest{
		Version: 1,
		Pages:   []manifest.PageEntry{},
	}
	err = mgr.Save(m)
	require.NoError(t, err)

	loaded, err := mgr.Load()
	require.NoError(t, err)
	assert.Equal(t, 1, loaded.Version, "manifest version should be preserved through save/load")
}

// =============================================================================
// C8: NewEmptyManifest produces valid initial state
// =============================================================================

func TestContract_NewEmptyManifestValid(t *testing.T) {
	m := manifest.NewEmptyManifest()

	assert.Equal(t, 1, m.Version)
	assert.NotNil(t, m.Pages, "Pages should be non-nil slice")
	assert.NotNil(t, m.UnmanagedPages, "UnmanagedPages should be non-nil slice")
	assert.Empty(t, m.Pages)
	assert.Empty(t, m.UnmanagedPages)
}
