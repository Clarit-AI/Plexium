package lint

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
)

// Doctor runs diagnostic checks on the Plexium setup.
type Doctor struct {
	repoRoot string
	cfg      *config.Config
}

// DoctorReport contains all doctor check results.
type DoctorReport struct {
	Checks []CheckResult
}

// CheckResult represents a single doctor check.
type CheckResult struct {
	Name         string
	Status       string // "pass", "fail", "warning", "skip"
	Message      string
	Remediation  string
}

// NewDoctor creates a new Doctor instance.
func NewDoctor(repoRoot string) *Doctor {
	return &Doctor{repoRoot: repoRoot}
}

// Run executes all doctor checks.
func (d *Doctor) Run() (*DoctorReport, error) {
	report := &DoctorReport{Checks: []CheckResult{}}

	// Load config (may fail if not initialized)
	cfg, err := config.LoadFromDir(d.repoRoot)
	if err != nil {
		// Config check will fail, but continue with other checks
		d.cfg = nil
	} else {
		d.cfg = cfg
	}

	// Run all checks
	d.checkGitRepo(report)
	d.checkConfig(report)
	d.checkManifest(report)
	d.checkWikiStructure(report)
	d.checkSchemaFile(report)
	d.checkLefthook(report)
	d.checkCIActions(report)
	d.checkMemento(report)

	return report, nil
}

func (d *Doctor) checkGitRepo(report *DoctorReport) {
	result := CheckResult{Name: "git-repo", Status: "pass"}

	if _, err := os.Stat(filepath.Join(d.repoRoot, ".git")); os.IsNotExist(err) {
		result.Status = "fail"
		result.Message = "Not a git repository"
		result.Remediation = "Run: git init"
	}

	report.Checks = append(report.Checks, result)
}

func (d *Doctor) checkConfig(report *DoctorReport) {
	result := CheckResult{Name: "config", Status: "skip"}

	if d.cfg == nil {
		result.Status = "warning"
		result.Message = "No config file found at .plexium/config.yml"
		result.Remediation = "Run: plexium init"
		report.Checks = append(report.Checks, result)
		return
	}

	// Validate config
	if err := d.cfg.Validate(); err != nil {
		result.Status = "fail"
		result.Message = fmt.Sprintf("Config validation failed: %v", err)
		result.Remediation = "Fix .plexium/config.yml"
	} else {
		result.Status = "pass"
		result.Message = "Config is valid"
	}

	report.Checks = append(report.Checks, result)
}

func (d *Doctor) checkManifest(report *DoctorReport) {
	result := CheckResult{Name: "manifest", Status: "skip"}

	manifestPath := filepath.Join(d.repoRoot, ".plexium", "manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		result.Status = "warning"
		result.Message = "manifest.json not found"
		result.Remediation = "Run: plexium convert to create initial manifest"
		report.Checks = append(report.Checks, result)
		return
	}

	// Try to load manifest
	mgr, err := manifest.NewManager(manifestPath)
	if err != nil {
		result.Status = "fail"
		result.Message = fmt.Sprintf("Failed to create manifest manager: %v", err)
		result.Remediation = "Check manifest.json syntax"
		report.Checks = append(report.Checks, result)
		return
	}

	_, err = mgr.Load()
	if err != nil {
		result.Status = "fail"
		result.Message = fmt.Sprintf("Failed to load manifest: %v", err)
		result.Remediation = "Check manifest.json syntax"
	} else {
		result.Status = "pass"
		result.Message = "Manifest is valid"
	}

	report.Checks = append(report.Checks, result)
}

func (d *Doctor) checkWikiStructure(report *DoctorReport) {
	result := CheckResult{Name: "wiki-structure", Status: "skip"}

	if d.cfg == nil {
		result.Status = "skip"
		result.Message = "Config not loaded, cannot check wiki"
		report.Checks = append(report.Checks, result)
		return
	}

	wikiRoot := filepath.Join(d.repoRoot, d.cfg.Wiki.Root)
	requiredFiles := []string{"_schema.md", "_index.md", "Home.md", "_Sidebar.md"}

	var missing []string
	for _, f := range requiredFiles {
		if _, err := os.Stat(filepath.Join(wikiRoot, f)); os.IsNotExist(err) {
			missing = append(missing, f)
		}
	}

	if len(missing) > 0 {
		result.Status = "fail"
		result.Message = fmt.Sprintf("Missing required wiki files: %s", strings.Join(missing, ", "))
		result.Remediation = "Run: plexium init --wiki to scaffold wiki structure"
	} else {
		result.Status = "pass"
		result.Message = "All required wiki files present"
	}

	report.Checks = append(report.Checks, result)
}

func (d *Doctor) checkSchemaFile(report *DoctorReport) {
	result := CheckResult{Name: "schema", Status: "skip"}

	if d.cfg == nil {
		report.Checks = append(report.Checks, result)
		return
	}

	schemaPath := filepath.Join(d.repoRoot, d.cfg.Wiki.Root, "_schema.md")
	content, err := os.ReadFile(schemaPath)
	if err != nil {
		result.Status = "fail"
		result.Message = "_schema.md not found"
		result.Remediation = "Run: plexium init"
		report.Checks = append(report.Checks, result)
		return
	}

	if !strings.Contains(string(content), "schema-version") {
		result.Status = "fail"
		result.Message = "_schema.md missing schema-version field"
		result.Remediation = "Ensure _schema.md contains schema-version frontmatter"
	} else {
		result.Status = "pass"
		result.Message = "_schema.md has schema-version"
	}

	report.Checks = append(report.Checks, result)
}

func (d *Doctor) checkLefthook(report *DoctorReport) {
	result := CheckResult{Name: "lefthook", Status: "skip"}

	// Check if lefthook is configured in enforcement
	if d.cfg != nil && !d.cfg.Enforcement.PreCommitHook {
		result.Status = "skip"
		result.Message = "Lefthook not configured (enforcement.preCommitHook is false)"
		report.Checks = append(report.Checks, result)
		return
	}

	// Check if lefthook is installed
	_, err := exec.LookPath("lefthook")
	if err != nil {
		result.Status = "warning"
		result.Message = "Lefthook not installed"
		result.Remediation = "Install lefthook: https://github.com/evilmartians/lefthook#install"
	} else {
		result.Status = "pass"
		result.Message = "Lefthook is installed"
	}

	report.Checks = append(report.Checks, result)
}

func (d *Doctor) checkCIActions(report *DoctorReport) {
	result := CheckResult{Name: "github-actions", Status: "skip"}

	if d.cfg != nil && !d.cfg.Enforcement.CICheck {
		result.Status = "skip"
		result.Message = "CI check not configured (enforcement.ciCheck is false)"
		report.Checks = append(report.Checks, result)
		return
	}

	workflowsDir := filepath.Join(d.repoRoot, ".github", "workflows")
	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
		result.Status = "warning"
		result.Message = ".github/workflows not found"
		result.Remediation = "Create CI workflow at .github/workflows/plexium.yml"
	} else {
		result.Status = "pass"
		result.Message = "GitHub Actions workflows directory exists"
	}

	report.Checks = append(report.Checks, result)
}

func (d *Doctor) checkMemento(report *DoctorReport) {
	result := CheckResult{Name: "memento", Status: "skip"}

	if d.cfg != nil && !d.cfg.Enforcement.MementoGate {
		result.Status = "skip"
		result.Message = "Memento gate not configured"
		report.Checks = append(report.Checks, result)
		return
	}

	// Check if git memento is functional
	cmd := exec.Command("git", "memento", "doctor")
	cmd.Dir = d.repoRoot
	if err := cmd.Run(); err != nil {
		result.Status = "warning"
		result.Message = "git memento doctor failed"
		result.Remediation = "Initialize memento: git memento init"
	} else {
		result.Status = "pass"
		result.Message = "git memento is functional"
	}

	report.Checks = append(report.Checks, result)
}

// ToJSON formats the report as JSON.
func (r *DoctorReport) ToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// Summary returns a count of checks by status.
func (r *DoctorReport) Summary() (passed, failed, warnings, skipped int) {
	for _, c := range r.Checks {
		switch c.Status {
		case "pass":
			passed++
		case "fail":
			failed++
		case "warning":
			warnings++
		case "skip":
			skipped++
		}
	}
	return
}
