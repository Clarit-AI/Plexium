package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// PostEditHook is an advisory hook that runs after an agent edits a source
// file. It prints a brief reminder to stderr when a source file (not wiki)
// is modified. It always exits successfully — never blocks the agent.
type PostEditHook struct {
	repoRoot string
	Stderr   io.Writer
}

// NewPostEditHook creates a post-edit hook for the given repo root.
func NewPostEditHook(repoRoot string) *PostEditHook {
	return &PostEditHook{repoRoot: repoRoot, Stderr: os.Stderr}
}

// Run reads a JSON payload from r (typically stdin) and prints an advisory
// reminder if the edited file is a source file (not under .wiki/ or .plexium/).
func (h *PostEditHook) Run(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil || len(data) == 0 {
		return nil // silent on read failure
	}

	filePath := extractFilePath(data)
	if filePath == "" {
		return nil
	}

	// Wiki and plexium files don't need a reminder
	if strings.HasPrefix(filePath, ".wiki/") || strings.HasPrefix(filePath, ".wiki\\") ||
		strings.HasPrefix(filePath, ".plexium/") || strings.HasPrefix(filePath, ".plexium\\") {
		return nil
	}

	fmt.Fprintln(h.Stderr, "plexium: source file modified — remember to update .wiki/ before committing")
	return nil
}

// extractFilePath tries to extract file_path from a JSON payload.
// Handles both top-level {"file_path": "..."} and nested {"params": {"file_path": "..."}}.
func extractFilePath(data []byte) string {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return ""
	}

	// Try top-level file_path
	if raw, ok := top["file_path"]; ok {
		var s string
		if json.Unmarshal(raw, &s) == nil && s != "" {
			return s
		}
	}

	// Try params.file_path
	if raw, ok := top["params"]; ok {
		var params map[string]json.RawMessage
		if json.Unmarshal(raw, &params) == nil {
			if raw2, ok := params["file_path"]; ok {
				var s string
				if json.Unmarshal(raw2, &s) == nil {
					return s
				}
			}
		}
	}

	return ""
}
