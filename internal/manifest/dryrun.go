package manifest

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DryRunner wraps file operations so they can be redirected to an output
// directory instead of their real targets when dry-run is active.
type DryRunner struct {
	enabled   bool
	outputDir string
	w         io.Writer // for reporting what would happen
}

// NewDryRunner creates a new DryRunner. If enabled, writes go to outputDir
// and a summary is printed to w (defaults to stdout).
func NewDryRunner(enabled bool, outputDir string, w io.Writer) *DryRunner {
	if w == nil {
		w = os.Stdout
	}
	return &DryRunner{
		enabled:   enabled,
		outputDir: outputDir,
		w:         w,
	}
}

// Enabled returns whether dry-run mode is active.
func (d *DryRunner) Enabled() bool {
	return d.enabled
}

// WriteFile writes content to path. In dry-run mode, writes to outputDir instead.
func (d *DryRunner) WriteFile(path string, content []byte) error {
	if d.enabled {
		fullPath := filepath.Join(d.outputDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("dry-run mkdir %s: %w", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, content, 0644); err != nil {
			return fmt.Errorf("dry-run write %s: %w", fullPath, err)
		}
		fmt.Fprintf(d.w, "  [dry-run] would create: %s\n", path)
		return nil
	}

	// Real write
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	return os.WriteFile(path, content, 0644)
}

// MkdirAll creates directories. In dry-run mode, logs the action.
func (d *DryRunner) MkdirAll(path string) error {
	if d.enabled {
		fmt.Fprintf(d.w, "  [dry-run] would create dir: %s\n", path)
		return nil
	}
	return os.MkdirAll(path, 0755)
}

// Report prints a dry-run summary.
func (d *DryRunner) Report(action string) {
	if d.enabled {
		fmt.Fprintf(d.w, "[dry-run] %s\n", action)
	}
}
