package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// EnsureClaudeHooks merges a PostToolUse hook entry into .claude/settings.json
// that runs `plexium hook post-edit` after Write/Edit tool uses. The function
// is idempotent — if the hook is already present, it does nothing.
func EnsureClaudeHooks(repoRoot string) (bool, error) {
	settingsPath := filepath.Join(repoRoot, ".claude", "settings.json")

	var settings map[string]any

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		settings = make(map[string]any)
	} else {
		if err := json.Unmarshal(data, &settings); err != nil {
			// Malformed JSON — start fresh rather than destroying existing file
			settings = make(map[string]any)
		}
	}

	// Check if a PostToolUse hook for Write|Edit already exists
	if hasPostEditHook(settings) {
		return false, nil
	}

	// Build the hook entry
	hookEntry := map[string]any{
		"matcher": "Write|Edit",
		"hooks": []any{
			map[string]any{
				"type":            "command",
				"command":         "plexium hook post-edit",
				"timeout":         5,
				"continueOnError": true,
			},
		},
	}

	// Merge into existing hooks structure
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = make(map[string]any)
	}
	postToolUse, _ := hooks["PostToolUse"].([]any)
	postToolUse = append(postToolUse, hookEntry)
	hooks["PostToolUse"] = postToolUse
	settings["hooks"] = hooks

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, err
	}

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(settingsPath, append(out, '\n'), 0o644); err != nil {
		return false, err
	}

	return true, nil
}

// hasPostEditHook checks if a PostToolUse hook matching Write|Edit already
// exists in the settings structure.
func hasPostEditHook(settings map[string]any) bool {
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return false
	}
	postToolUse, ok := hooks["PostToolUse"].([]any)
	if !ok {
		return false
	}
	for _, entry := range postToolUse {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		matcher, _ := m["matcher"].(string)
		if strings.Contains(matcher, "Write") || strings.Contains(matcher, "Edit") {
			return true
		}
	}
	return false
}
