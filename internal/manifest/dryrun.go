package manifest

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
		target := d.normalizeTarget(path)
		fullPath := filepath.Join(d.outputDir, target)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("dry-run mkdir %s: %w", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, content, 0644); err != nil {
			return fmt.Errorf("dry-run write %s: %w", fullPath, err)
		}
		fmt.Fprintf(d.w, "  [dry-run] would create: %s\n", target)
		return nil
	}

	// Real write
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	return os.WriteFile(path, content, 0644)
}

func (d *DryRunner) normalizeTarget(path string) string {
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) {
		if rel, err := filepath.Rel(d.outputDir, clean); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			return rel
		}
		if repoRoot, ok := inferDryRunRepoRoot(d.outputDir); ok {
			if rel, err := filepath.Rel(repoRoot, clean); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
				return rel
			}
		}
		clean = strings.TrimPrefix(clean, filepath.VolumeName(clean))
		clean = strings.TrimLeft(clean, string(filepath.Separator))
	}
	return sanitizeDryRunTarget(clean)
}

func inferDryRunRepoRoot(outputDir string) (string, bool) {
	clean := filepath.Clean(outputDir)
	if filepath.Base(clean) != "output" {
		return "", false
	}
	plexiumDir := filepath.Dir(clean)
	if filepath.Base(plexiumDir) != ".plexium" {
		return "", false
	}
	return filepath.Dir(plexiumDir), true
}

func sanitizeDryRunTarget(path string) string {
	parts := strings.FieldsFunc(filepath.ToSlash(path), func(r rune) bool {
		return r == '/'
	})
	safe := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			continue
		}
		safe = append(safe, part)
	}
	if len(safe) == 0 {
		return "dry-run-output"
	}
	return filepath.FromSlash(strings.Join(safe, "/"))
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
