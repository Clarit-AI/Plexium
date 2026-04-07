// Package inspect provides non-test inspection tools for validating
// Plexium's implementation against its phase plan.
//
// Run: go run ./validation/inspect/compliance_audit.go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PhaseMapping maps each milestone phase to its expected packages, commands, and key deliverables.
type PhaseMapping struct {
	Phase        string
	Milestone    string
	Packages     []string // expected Go packages
	Commands     []string // expected CLI commands
	Deliverables []string // key deliverables from phase docs
}

var phases = []PhaseMapping{
	{
		Phase:     "P0",
		Milestone: "Project Setup",
		Packages:  []string{}, // no code packages — toolchain setup only
		Commands:  []string{},
		Deliverables: []string{
			"Go module initialized",
			"bd epics created",
			"memento configured",
			"CI skeleton",
		},
	},
	{
		Phase:     "M1",
		Milestone: "CLI Foundation",
		Packages:  []string{"cmd/plexium", "internal/config", "internal/scanner", "internal/markdown", "internal/template"},
		Commands:  []string{"init", "sync", "lint", "publish", "doctor"},
		Deliverables: []string{
			"CLI binary with command routing",
			"Config loader with env overrides",
			"File scanner with glob patterns",
			"Markdown normalizer/frontmatter parser",
			"Template engine",
		},
	},
	{
		Phase:     "M2",
		Milestone: "Page Generation",
		Packages:  []string{"internal/generate"},
		Commands:  []string{},
		Deliverables: []string{
			"Taxonomy classifier",
			"Module generator",
			"Decision generator",
			"Concept generator",
			"Slug deduplication",
			"Navigation file generation",
		},
	},
	{
		Phase:     "M3",
		Milestone: "State & Publishing",
		Packages:  []string{"internal/manifest", "internal/publish", "internal/wiki"},
		Commands:  []string{"init", "publish"},
		Deliverables: []string{
			"Manifest CRUD",
			"Hash computation",
			"Bidirectional source/wiki mapping",
			"Publish command",
			"Init scaffolding",
			"Dry-run mode",
		},
	},
	{
		Phase:     "M4",
		Milestone: "Convert (Brownfield)",
		Packages:  []string{"internal/convert"},
		Commands:  []string{"convert"},
		Deliverables: []string{
			"Scour phase (source extraction)",
			"Filter phase (include/exclude)",
			"Ingest phase (page generation)",
			"Link phase (cross-reference injection)",
			"Lint phase (gap analysis)",
			"Report generation",
		},
	},
	{
		Phase:     "M5",
		Milestone: "Agent Adapters",
		Packages:  []string{"internal/plugins"},
		Commands:  []string{"plugin"},
		Deliverables: []string{
			"Plugin architecture",
			"Schema generation per tech stack",
			"Agent adapters (Claude/Codex/Gemini/Cursor)",
		},
	},
	{
		Phase:     "M6",
		Milestone: "Deterministic Lint",
		Packages:  []string{"internal/lint"},
		Commands:  []string{"lint", "doctor"},
		Deliverables: []string{
			"Link crawler",
			"Orphan detector",
			"Staleness detector",
			"Manifest validator",
			"Sidebar validator",
			"Frontmatter validator",
			"Doctor command",
		},
	},
	{
		Phase:     "M7",
		Milestone: "Reporting & Obsidian",
		Packages:  []string{"internal/reports", "internal/wiki"},
		Commands:  []string{"gh-wiki-sync"},
		Deliverables: []string{
			"Bootstrap report",
			"Sync report",
			"Lint report",
			"Obsidian config",
			"GitHub Wiki sync",
		},
	},
	{
		Phase:     "M8",
		Milestone: "Enforcement",
		Packages:  []string{"internal/hook", "internal/ci", "internal/migrate"},
		Commands:  []string{"hook pre-commit", "hook post-commit", "ci check", "migrate"},
		Deliverables: []string{
			"Pre-commit hook",
			"Post-commit hook (WIKI-DEBT)",
			"CI diff-aware check",
			"Schema migration",
			"Lefthook config",
			"GitHub Actions workflows",
		},
	},
	{
		Phase:     "M9",
		Milestone: "Tool Integrations",
		Packages: []string{
			"internal/integrations/memento",
			"internal/integrations/beads",
			"internal/integrations/pageindex",
			"internal/integrations/roles",
			"internal/lint", // LLM lint additions
		},
		Commands: []string{"retrieve", "pageindex serve", "beads link", "beads unlink"},
		Deliverables: []string{
			"Memento ingestor",
			"Memento gate (CI)",
			"Beads linker (bidirectional)",
			"PageIndex (hierarchical search)",
			"PageIndex MCP server",
			"Retriever (fallback chain)",
			"LLM-augmented lint",
			"Role types + registry",
		},
	},
	{
		Phase:     "M10",
		Milestone: "Orchestration",
		Packages: []string{
			"internal/agent",
			"internal/daemon",
			"internal/compile",
			"internal/retry",
		},
		Commands: []string{"agent start", "agent stop", "agent status", "agent test", "daemon", "compile", "orchestrate"},
		Deliverables: []string{
			"Provider cascade (Ollama/OpenRouter/inherit)",
			"Rate limit tracker",
			"Task router (complexity classification)",
			"Daemon loop",
			"Workspace manager (git worktrees)",
			"Tracker adapter (GitHub Issues)",
			"Runner adapter (CLI dispatch)",
			"Retry policy (exponential backoff)",
			"Compile command (deterministic nav regeneration)",
		},
	},
}

func main() {
	repoRoot := "."
	if len(os.Args) > 1 {
		repoRoot = os.Args[1]
	}

	fmt.Println("# Plexium Phase Compliance Audit")
	fmt.Println()

	totalPackages := 0
	foundPackages := 0
	totalDeliverables := 0
	missingPackages := []string{}

	for _, phase := range phases {
		fmt.Printf("## %s: %s\n\n", phase.Phase, phase.Milestone)

		// Check packages
		if len(phase.Packages) > 0 {
			fmt.Println("### Packages")
			for _, pkg := range phase.Packages {
				pkgPath := filepath.Join(repoRoot, pkg)
				totalPackages++
				goFiles := countGoFiles(pkgPath)
				testFiles := countTestFiles(pkgPath)

				if goFiles > 0 {
					foundPackages++
					fmt.Printf("  FOUND: %s (%d source, %d test files)\n", pkg, goFiles, testFiles)
				} else {
					missingPackages = append(missingPackages, pkg)
					fmt.Printf("  MISSING: %s\n", pkg)
				}
			}
			fmt.Println()
		}

		// Check deliverables
		fmt.Println("### Deliverables")
		for _, d := range phase.Deliverables {
			totalDeliverables++
			fmt.Printf("  - %s\n", d)
		}
		fmt.Println()
	}

	// Summary
	fmt.Println("## Summary")
	fmt.Println()
	fmt.Printf("- Packages expected: %d\n", totalPackages)
	fmt.Printf("- Packages found: %d\n", foundPackages)
	fmt.Printf("- Packages missing: %d\n", len(missingPackages))
	fmt.Printf("- Total deliverables tracked: %d\n", totalDeliverables)
	fmt.Println()

	if len(missingPackages) > 0 {
		fmt.Println("### Missing Packages")
		for _, pkg := range missingPackages {
			fmt.Printf("  - %s\n", pkg)
		}
	} else {
		fmt.Println("All expected packages are present.")
	}

	// Test coverage summary
	fmt.Println()
	fmt.Println("## Test Coverage by Package")
	fmt.Println()

	allPkgs := findAllPackages(filepath.Join(repoRoot, "internal"))
	for _, pkg := range allPkgs {
		goFiles := countGoFiles(pkg)
		testFiles := countTestFiles(pkg)
		rel, _ := filepath.Rel(repoRoot, pkg)

		coverage := "none"
		if testFiles > 0 {
			ratio := float64(testFiles) / float64(goFiles) * 100
			if ratio >= 50 {
				coverage = "good"
			} else {
				coverage = "partial"
			}
		}

		fmt.Printf("| %s | %d src | %d test | %s |\n", rel, goFiles, testFiles, coverage)
	}

	// Validation test coverage
	fmt.Println()
	fmt.Println("## Validation Test Coverage")
	fmt.Println()
	valPkg := filepath.Join(repoRoot, "validation")
	valTests := countTestFiles(valPkg)
	valFuncs := countTestFunctions(valPkg)
	fmt.Printf("- Validation test files: %d\n", valTests)
	fmt.Printf("- Validation test functions: %d\n", valFuncs)
}

func countGoFiles(dir string) int {
	count := 0
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			count++
		}
		return nil
	})
	return count
}

func countTestFiles(dir string) int {
	count := 0
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(path, "_test.go") {
			count++
		}
		return nil
	})
	return count
}

func countTestFunctions(dir string) int {
	count := 0
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(path, "_test.go") {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.HasPrefix(strings.TrimSpace(line), "func Test") {
					count++
				}
			}
		}
		return nil
	})
	return count
}

func findAllPackages(root string) []string {
	seen := map[string]bool{}
	var result []string

	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") {
			dir := filepath.Dir(path)
			if !seen[dir] {
				seen[dir] = true
				result = append(result, dir)
			}
		}
		return nil
	})

	return result
}
