package wiki

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ObsidianConfig holds all Obsidian vault configuration.
type ObsidianConfig struct {
	RepoRoot      string
	DryRun        bool
	filesCreated  []string
}

// NewObsidianConfig creates a new ObsidianConfig generator.
func NewObsidianConfig(repoRoot string, dryRun bool) *ObsidianConfig {
	return &ObsidianConfig{
		RepoRoot: repoRoot,
		DryRun:   dryRun,
	}
}

// Ensure creates the .obsidian/ directory structure and config files.
func (c *ObsidianConfig) Ensure() error {
	obsidianDir := filepath.Join(c.RepoRoot, ".wiki", ".obsidian")
	c.filesCreated = []string{}

	if c.DryRun {
		fmt.Printf("[dry-run] Would create Obsidian config in %s\n", obsidianDir)
		return nil
	}

	// Create .obsidian/ directory
	if err := os.MkdirAll(obsidianDir, 0755); err != nil {
		return fmt.Errorf("creating .obsidian directory: %w", err)
	}

	// Create app.json
	if err := c.writeAppConfig(obsidianDir); err != nil {
		return err
	}

	// Create community-plugins.json
	if err := c.writeCommunityPlugins(obsidianDir); err != nil {
		return err
	}

	// Create dataview plugin config
	if err := c.writeDataviewConfig(obsidianDir); err != nil {
		return err
	}

	return nil
}

func (c *ObsidianConfig) writeAppConfig(obsidianDir string) error {
	appConfig := map[string]interface{}{
		"pluginEnabled": map[string]bool{
			"global-search":    true,
			"file-explorer":     true,
			"backlink":          true,
			"graph":             true,
			"daily-notes":       false,
			"tag-pane":          true,
			"page-preview":      true,
			"word-count":        true,
			"ledger":           false,
			"workspaces":        false,
		},
		"attachmentFolderPath": "raw/assets",
		"newFileFolderPath":    ".",
		"showInlineTitle":      true,
		"userInputPrefix":       "",
		"userInputSuffix":      "",
		"legacyEditor":         false,
		"livePreview":          true,
		"readableLineLength":   true,
		"showLineNumber":       false,
		"spellcheck":           false,
		"strictLineBreaks":     true,
		"useMarkdownLinks":     false,
		"newLinkFormat":        "shortest",
	}

	data, err := json.MarshalIndent(appConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling app.json: %w", err)
	}

	path := filepath.Join(obsidianDir, "app.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing app.json: %w", err)
	}

	return nil
}

func (c *ObsidianConfig) writeCommunityPlugins(obsidianDir string) error {
	pluginsConfig := map[string][]string{
		"enabledPlugins": {
			"obsidian-dataview",
			"obsidian-marp",
			"obsidian-templater-obsidian",
		},
	}

	data, err := json.MarshalIndent(pluginsConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling community-plugins.json: %w", err)
	}

	path := filepath.Join(obsidianDir, "community-plugins.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing community-plugins.json: %w", err)
	}

	return nil
}

func (c *ObsidianConfig) writeDataviewConfig(obsidianDir string) error {
	pluginDir := filepath.Join(obsidianDir, "plugins", "dataview")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		return fmt.Errorf("creating dataview plugin directory: %w", err)
	}

	// Dataview settings
	dataviewSettings := map[string]interface{}{
		"enableJavaScriptFunctions":  true,
		"enableInlineJavaScript":     false,
		"pluginVersion":              "0.5.14",
		"defaultDateFormat":         "YYYY-MM-DD",
		"renderNullAs":              "-",
		"treatUnknownDatesAsFuture":  false,
		"showResultCount":           true,
		"exportPath":                nullString(""),
	}

	data, err := json.MarshalIndent(dataviewSettings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling dataview settings: %w", err)
	}

	path := filepath.Join(pluginDir, "data.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing dataview data.json: %w", err)
	}

	return nil
}

// nullString returns a pointer to an empty string for JSON null values.
func nullString(s string) *string {
	return &s
}

// UpdateInitOptions adds Obsidian config generation to wiki.Init.
// This should be called after Init creates the .obsidian/ directory.
func UpdateObsidianConfig(repoRoot string, obsidianDir string, dryRun bool) error {
	cfg := NewObsidianConfig(repoRoot, dryRun)
	return cfg.Ensure()
}

// FilesCreated returns the list of files created by Ensure.
func (c *ObsidianConfig) FilesCreated() []string {
	if c.DryRun {
		return nil
	}
	obsidianDir := filepath.Join(c.RepoRoot, ".wiki", ".obsidian")
	return []string{
		filepath.Join(obsidianDir, "app.json"),
		filepath.Join(obsidianDir, "community-plugins.json"),
		filepath.Join(obsidianDir, "plugins", "dataview", "data.json"),
	}
}
