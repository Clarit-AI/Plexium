package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"context"
	"time"

	"github.com/Clarit-AI/Plexium/internal/agent"
	"github.com/Clarit-AI/Plexium/internal/ci"
	"github.com/Clarit-AI/Plexium/internal/compile"
	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/convert"
	"github.com/Clarit-AI/Plexium/internal/daemon"
	"github.com/Clarit-AI/Plexium/internal/hook"
	"github.com/Clarit-AI/Plexium/internal/integrations/beads"
	"github.com/Clarit-AI/Plexium/internal/integrations/memento"
	"github.com/Clarit-AI/Plexium/internal/integrations/pageindex"
	"github.com/Clarit-AI/Plexium/internal/lint"
	"github.com/Clarit-AI/Plexium/internal/migrate"
	"github.com/Clarit-AI/Plexium/internal/publish"
	"github.com/Clarit-AI/Plexium/internal/retry"
	plexiumsync "github.com/Clarit-AI/Plexium/internal/sync"
	"github.com/Clarit-AI/Plexium/internal/wiki"
	"github.com/spf13/cobra"
)

var (
	version = "0.1.0"
	cfgFile string
)

var rootCmd = &cobra.Command{
	Use:   "plexium",
	Short: "Self-documenting repositories via LLM Wiki pattern",
	Long: `Plexium transforms repositories into self-documenting systems by applying 
Karpathy's LLM Wiki pattern to agentic coding workflows. Instead of stateless RAG 
rediscovery on every session, LLM coding agents incrementally build and maintain 
a persistent, interlinked wiki — a compiled knowledge layer that compounds with 
every commit, every conversation, and every ingested source.`,
	Version: version,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", ".plexium/config.yml", "config file path")
	rootCmd.PersistentFlags().Bool("output-json", false, "Emit JSON output")

	// init flags
	initCmd.Flags().Bool("github-wiki", false, "Enable GitHub Wiki integration")
	initCmd.Flags().Bool("obsidian", false, "Generate Obsidian configuration")
	initCmd.Flags().String("strictness", "moderate", "Strictness level: strict|moderate|advisory")
	initCmd.Flags().Bool("dry-run", false, "Preview without writing files")
	initCmd.Flags().Bool("with-memento", false, "Initialize memento session tracking")
	initCmd.Flags().Bool("with-beads", false, "Initialize beads task tracking")
	initCmd.Flags().Bool("with-pageindex", false, "Initialize PageIndex retrieval")

	// convert flags
	convertCmd.Flags().String("depth", "shallow", "Scour depth: shallow|deep")
	convertCmd.Flags().Bool("dry-run", false, "Preview without writing to .wiki/")
	convertCmd.Flags().String("agent", "", "Run specified agent adapter after conversion")

	// publish flags
	publishCmd.Flags().Bool("dry-run", false, "Preview without pushing")

	// sync flags
	syncCmd.Flags().Bool("dry-run", false, "Preview without writing changes")

	// lint flags
	lintCmd.Flags().Bool("deterministic", false, "Run deterministic checks only (link/orphan/staleness validation)")
	lintCmd.Flags().Bool("full", false, "Run full lint including LLM-augmented semantic checks")
	lintCmd.Flags().Bool("ci", false, "CI mode: exit with non-zero code on lint errors or warnings")
	lintCmd.Flags().String("fail-on", "error", "Exit non-zero on this severity: error|warning")

	// gh-wiki-sync flags
	ghWikiSyncCmd.Flags().Bool("dry-run", false, "Preview sync without writing")
	ghWikiSyncCmd.Flags().Bool("push", false, "Push changes to GitHub Wiki")

	// retrieve flags
	retrieveCmd.Flags().String("format", "markdown", "Output format: json|markdown")

	// migrate flags
	migrateCmd.Flags().Bool("dry-run", false, "Preview migrations without applying")
	migrateCmd.Flags().Int("version", 0, "Target schema version (default: latest)")

	// ci check flags
	ciiCheckCmd.Flags().String("base", "", "Base commit SHA")
	ciiCheckCmd.Flags().String("head", "", "Head commit SHA")
	ciiCheckCmd.Flags().String("output", "", "Output file for JSON results")
	ciiCheckCmd.MarkFlagRequired("base")
	ciiCheckCmd.MarkFlagRequired("head")

	// compile flags
	compileCmd.Flags().Bool("dry-run", false, "Preview without writing files")

	// daemon flags
	daemonCmd.Flags().Int("poll-interval", 300, "Poll interval in seconds")
	daemonCmd.Flags().Int("max-concurrent", 2, "Maximum concurrent worktrees")

	// orchestrate flags
	orchestrateCmd.Flags().String("issue", "", "Issue ID to orchestrate")
	orchestrateCmd.MarkFlagRequired("issue")

	// agent subcommands
	agentCmd.AddCommand(agentStartCmd)
	agentCmd.AddCommand(agentStopCmd)
	agentCmd.AddCommand(agentStatusCmd)
	agentCmd.AddCommand(agentTestCmd)
	agentCmd.AddCommand(agentSpendCmd)
	agentCmd.AddCommand(agentBenchmarkCmd)
	agentCmd.AddCommand(agentSetupCmd)
	agentSetupCmd.Flags().String("api-key-file", "", "Read OpenRouter API key from a file")
	agentSetupCmd.Flags().Bool("api-key-stdin", false, "Read OpenRouter API key from stdin")
	agentSetupCmd.Flags().String("model", "", "Select the OpenRouter model to write into assistiveAgent config")
	agentSetupCmd.Flags().Float64("daily-budget-usd", 0, "Optional daily assistive-provider budget in USD (0 = unlimited)")
	agentSetupCmd.Flags().String("execution-mode", "", "Daemon upkeep mode: coding-agent-primary|provider-primary")
	agentTestCmd.Flags().String("provider", "", "Test a specific provider")

	// Register subcommands
	ciCmd.AddCommand(ciiCheckCmd)
	hookCmd.AddCommand(hookPreCommitCmd)
	hookCmd.AddCommand(hookPostCommitCmd)
	hookCmd.AddCommand(hookPostEditCmd)
	pageidxCmd.AddCommand(pageidxServeCmd)
	beadsCmd.AddCommand(beadsLinkCmd)
	beadsCmd.AddCommand(beadsUnlinkCmd)
	beadsCmd.AddCommand(beadsPagesCmd)
	beadsCmd.AddCommand(beadsTasksCmd)
	beadsCmd.AddCommand(beadsScanCmd)

	// Command groups for organized help output
	rootCmd.AddGroup(
		&cobra.Group{ID: "start", Title: "Getting Started"},
		&cobra.Group{ID: "daily", Title: "Daily Use"},
		&cobra.Group{ID: "agent", Title: "Agent Management"},
		&cobra.Group{ID: "advanced", Title: "Advanced"},
	)

	// Getting Started
	initCmd.GroupID = "start"
	convertCmd.GroupID = "start"

	// Daily Use
	syncCmd.GroupID = "daily"
	lintCmd.GroupID = "daily"
	retrieveCmd.GroupID = "daily"
	compileCmd.GroupID = "daily"
	doctorCmd.GroupID = "daily"

	// Agent Management
	agentCmd.GroupID = "agent"
	daemonCmd.GroupID = "agent"

	// Advanced
	publishCmd.GroupID = "advanced"
	ghWikiSyncCmd.GroupID = "advanced"
	migrateCmd.GroupID = "advanced"
	ciCmd.GroupID = "advanced"
	pluginCmd.GroupID = "advanced"
	hookCmd.GroupID = "advanced"
	beadsCmd.GroupID = "advanced"
	orchestrateCmd.GroupID = "advanced"
	bootstrapCmd.GroupID = "advanced"
	pageidxCmd.GroupID = "advanced"

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(convertCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(lintCmd)
	rootCmd.AddCommand(bootstrapCmd)
	rootCmd.AddCommand(retrieveCmd)
	rootCmd.AddCommand(publishCmd)
	rootCmd.AddCommand(ghWikiSyncCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(pluginCmd)
	rootCmd.AddCommand(hookCmd)
	rootCmd.AddCommand(ciCmd)
	rootCmd.AddCommand(pageidxCmd)
	rootCmd.AddCommand(beadsCmd)
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(compileCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(orchestrateCmd)
}

// init command
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Plexium in a repository",
	RunE: func(cmd *cobra.Command, args []string) error {
		githubWiki, _ := cmd.Flags().GetBool("github-wiki")
		obsidian, _ := cmd.Flags().GetBool("obsidian")
		strictness, _ := cmd.Flags().GetString("strictness")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		withMemento, _ := cmd.Flags().GetBool("with-memento")
		withBeads, _ := cmd.Flags().GetBool("with-beads")
		withPageIndex, _ := cmd.Flags().GetBool("with-pageindex")

		repoRoot, err := currentGitRepoRoot()
		if err != nil {
			return fmt.Errorf("plexium init requires a git repository — run 'git init' first, then retry")
		}

		outputJSON, _ := cmd.Flags().GetBool("output-json")

		result, err := wiki.Init(wiki.InitOptions{
			RepoRoot:      repoRoot,
			GitHubWiki:    githubWiki,
			Obsidian:      obsidian,
			Strictness:    strictness,
			DryRun:        dryRun,
			WithMemento:   withMemento,
			WithBeads:     withBeads,
			WithPageIndex: withPageIndex,
		})
		if err != nil {
			return fmt.Errorf("init failed: %w", err)
		}

		var mementoResult *memento.EnsureCLIResult
		if withMemento && !dryRun {
			mementoStdout := cmd.OutOrStdout()
			if outputJSON {
				mementoStdout = cmd.ErrOrStderr()
			}
			mementoResult, err = memento.EnsureCLI(memento.EnsureCLIOptions{
				Stdin:  cmd.InOrStdin(),
				Stdout: mementoStdout,
				Stderr: cmd.ErrOrStderr(),
			})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not install git-memento automatically: %v\n", err)
			}

			if mementoResult != nil && mementoResult.Available {
				initialized, initErr := memento.IsInitialized(repoRoot)
				if initErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not inspect git-memento repo config: %v\n", initErr)
				} else if !initialized {
					if initErr := memento.InitRepo(repoRoot, ""); initErr != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: git memento init failed: %v\n", initErr)
					}
				}
			}
		}

		if outputJSON {
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal init result to JSON: %w", err)
			}
			fmt.Println(string(data))
			return nil
		}

		if dryRun {
			fmt.Println("[dry-run] No files were created.")
			fmt.Printf("Would create %d directories and %d files:\n", len(result.DirsCreated), len(result.FilesCreated))
		} else {
			fmt.Printf("Initialized Plexium wiki in %s\n", result.WikiDir)
			fmt.Printf("Created %d directories and %d files\n", len(result.DirsCreated), len(result.FilesCreated))

			fmt.Println("\nNext steps:")
			fmt.Println("  1. Run 'plexium setup claude' or 'plexium setup codex' for agent-ready onboarding")
			fmt.Println("  2. Run 'plexium convert' to replace the starter scaffold with grounded project pages")
			fmt.Println("  3. Run 'plexium retrieve \"what does this project do?\"' to inspect early wiki coverage")
			fmt.Println("  4. Run 'plexium doctor' or 'plexium lint --deterministic' to validate the setup")
			fmt.Println("  5. For the first bulk wiki build, prefer agent teams/sub-agents when your coding agent supports them")

			if withPageIndex {
				fmt.Println("\nPageIndex MCP server ready.")
				fmt.Println("  Finish setup for Claude: plexium setup claude --write-config")
				fmt.Println("  Finish setup for Codex:  plexium setup codex --write-config")
				fmt.Println("  Or query directly: plexium retrieve \"<query>\"")
			}
			if withMemento {
				fmt.Println("\nMemento is optional and configured per repository.")
				switch {
				case mementoResult != nil && mementoResult.Available && mementoResult.Installed:
					fmt.Println("  Installed git-memento and initialized local repo metadata.")
				case mementoResult != nil && mementoResult.Available:
					fmt.Println("  git-memento is available and ready for repo-local initialization.")
				case mementoResult != nil && mementoResult.InstallCommand != "":
					fmt.Printf("  Install later with: %s\n", mementoResult.InstallCommand)
				case mementoResult != nil && mementoResult.ReleaseURL != "":
					fmt.Printf("  Install later from: %s\n", mementoResult.ReleaseURL)
				default:
					fmt.Println("  Install git-memento from https://github.com/mandel-macaque/memento when you want session provenance.")
				}
			}
		}

		return nil
	},
}

// convert command
var convertCmd = &cobra.Command{
	Use:   "convert",
	Short: "Bootstrap wiki from existing repository",
	RunE: func(cmd *cobra.Command, args []string) error {
		depth, _ := cmd.Flags().GetString("depth")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		agent, _ := cmd.Flags().GetString("agent")
		outputJSON, _ := cmd.Flags().GetBool("output-json")

		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		// Load config (optional for convert — may not exist yet)
		var cfg *config.Config
		cfg, _ = config.LoadFromDir(repoRoot)

		pipeline := convert.NewPipeline(convert.PipelineOptions{
			RepoRoot: repoRoot,
			Config:   cfg,
			DryRun:   dryRun,
			Depth:    depth,
			Agent:    agent,
		})

		result, err := pipeline.Run()
		if err != nil {
			return fmt.Errorf("convert failed: %w", err)
		}

		if outputJSON {
			data, _ := json.MarshalIndent(result.Report, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		if dryRun {
			fmt.Println("[dry-run] No files were written to .wiki/")
			fmt.Printf("Would create %d pages\n", len(result.Pages))
		} else {
			fmt.Printf("Converted %d pages\n", len(result.Pages))
			fmt.Printf("Stubs: %d\n", result.Report.Pages.Stubs)
			fmt.Printf("Gap score: %.0f%%\n", result.Report.GapScore*100)
		}

		if result.AdapterRan != "" {
			fmt.Printf("Ran agent adapter: %s\n", result.AdapterRan)
		}

		fmt.Printf("Report: %s\n", result.ReportPath)
		return nil
	},
}

// sync command
var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync wiki after source changes",
	Long:  "Detect changed source files, update manifest hashes, and recompile navigation.",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		outputJSON, _ := cmd.Flags().GetBool("output-json")

		cfg, err := config.LoadFromDir(repoRoot)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		result, err := plexiumsync.Run(plexiumsync.Options{
			RepoRoot: repoRoot,
			Config:   cfg,
			DryRun:   dryRun,
		})
		if err != nil {
			return fmt.Errorf("sync failed: %w", err)
		}

		if outputJSON {
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
		} else {
			if dryRun {
				fmt.Println("[dry-run] sync preview:")
			}
			fmt.Printf("Source files checked: %d\n", result.SourceFilesChecked)
			fmt.Printf("Stale pages found:   %d\n", result.StalePages)
			fmt.Printf("Hashes updated:      %d\n", result.HashesUpdated)
			fmt.Printf("Nav recompiled:      %v\n", result.NavRecompiled)
			if len(result.PagesAffected) > 0 {
				fmt.Println("Pages affected:")
				for _, p := range result.PagesAffected {
					fmt.Printf("  - %s\n", p)
				}
			}
		}
		return nil
	},
}

// lint command
var lintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Check wiki health (deterministic checks)",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		outputJSON, _ := cmd.Flags().GetBool("output-json")
		deterministic, _ := cmd.Flags().GetBool("deterministic")
		full, _ := cmd.Flags().GetBool("full")
		ciMode, _ := cmd.Flags().GetBool("ci")

		if !deterministic && !full {
			fmt.Println("Note: Use --deterministic for structural checks or --full for LLM-augmented analysis.")
			fmt.Println("Running deterministic checks only.")
			fmt.Println()
		}

		// Load config
		cfg, _ := config.LoadFromDir(repoRoot)

		linter := lint.NewLinter(repoRoot, cfg)

		var report *lint.LintReport
		if full {
			// Wire assistive agent cascade as LLM client when available
			var llmClient lint.LLMClient
			if cfg != nil && cfg.AssistiveAgent.Enabled {
				cascade, _ := buildCascadeFromConfig(cfg)
				llmClient = &agent.CascadeLLMClient{Cascade: cascade}
			}
			report, err = linter.RunFull(llmClient)
		} else {
			report, err = linter.RunDeterministic()
		}
		if err != nil {
			return fmt.Errorf("lint failed: %w", err)
		}

		if outputJSON {
			data, _ := report.ToJSON()
			fmt.Println(string(data))
		} else {
			// Human-readable output
			fmt.Printf("Lint Report - %s\n", report.Timestamp)
			fmt.Printf("========================\n\n")

			if len(report.Deterministic.BrokenLinks) > 0 {
				fmt.Printf("❌ Broken Links (%d):\n", len(report.Deterministic.BrokenLinks))
				for _, l := range report.Deterministic.BrokenLinks {
					fmt.Printf("   %s:%d → [[%s]] (target: %s)\n", l.PagePath, l.LineNum, l.RawLink, l.Target)
				}
				fmt.Println()
			}

			if len(report.Deterministic.OrphanPages) > 0 {
				fmt.Printf("⚠️  Orphan Pages (%d):\n", len(report.Deterministic.OrphanPages))
				for _, o := range report.Deterministic.OrphanPages {
					fmt.Printf("   %s (%s) - %s\n", o.WikiPath, o.Severity, o.Reason)
				}
				fmt.Println()
			}

			if len(report.Deterministic.StaleCandidates) > 0 {
				fmt.Printf("⚠️  Stale Pages (%d):\n", len(report.Deterministic.StaleCandidates))
				for _, s := range report.Deterministic.StaleCandidates {
					fmt.Printf("   %s - %d days since update\n", s.WikiPath, s.DaysSinceUpdate)
				}
				fmt.Println()
			}

			if len(report.Deterministic.ManifestDrift) > 0 {
				fmt.Printf("❌ Manifest Issues (%d):\n", len(report.Deterministic.ManifestDrift))
				for _, m := range report.Deterministic.ManifestDrift {
					fmt.Printf("   %s: %s\n", m.Path, m.Message)
				}
				fmt.Println()
			}

			if len(report.Deterministic.SidebarIssues) > 0 {
				fmt.Printf("⚠️  Sidebar Issues (%d):\n", len(report.Deterministic.SidebarIssues))
				for _, s := range report.Deterministic.SidebarIssues {
					fmt.Printf("   Line %d: [[%s]] → %s\n", s.LineNum, s.LinkText, s.Target)
				}
				fmt.Println()
			}

			if len(report.Deterministic.FrontmatterIssues) > 0 {
				fmt.Printf("❌ Frontmatter Issues (%d):\n", len(report.Deterministic.FrontmatterIssues))
				for _, f := range report.Deterministic.FrontmatterIssues {
					fmt.Printf("   %s: %s (%s)\n", f.WikiPath, f.Field, f.Message)
				}
				fmt.Println()
			}

			// Summary
			if report.Summary.Errors == 0 && report.Summary.Warnings == 0 {
				fmt.Println("✅ All checks passed!")
			} else {
				fmt.Printf("Summary: %d errors, %d warnings\n", report.Summary.Errors, report.Summary.Warnings)
			}
		}

		// Exit code
		if ciMode {
			os.Exit(report.ExitCode())
		}
		// Default: exit 1 only on errors
		if report.Summary.Errors > 0 {
			os.Exit(1)
		}
		return nil
	},
}

// bootstrap command
var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "Bootstrap new wiki pages",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(" plexium bootstrap")
		return nil
	},
}

// retrieve command
var retrieveCmd = &cobra.Command{
	Use:   "retrieve",
	Short: "Query wiki for information",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := args[0]
		format, _ := cmd.Flags().GetString("format")
		outputJSON, _ := cmd.Flags().GetBool("output-json")

		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		cfg, _ := config.LoadFromDir(repoRoot)
		wikiRoot := ".wiki"
		if cfg != nil && cfg.Wiki.Root != "" {
			wikiRoot = cfg.Wiki.Root
		}

		retriever := pageindex.NewRetriever(filepath.Join(repoRoot, wikiRoot))

		result, err := retriever.Retrieve(query)
		if err != nil {
			return fmt.Errorf("retrieve failed: %w", err)
		}

		if outputJSON || format == "json" {
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		// Markdown output
		if len(result.Pages) == 0 {
			fmt.Printf("No results for: %s\n", query)
			return nil
		}

		fmt.Printf("Results for: %s (%d hits)\n\n", query, len(result.Pages))
		for _, hit := range result.Pages {
			fmt.Printf("## %s\n", hit.Title)
			fmt.Printf("%s | relevance: %.1f\n", hit.Path, hit.Relevance)
			if hit.Summary != "" {
				fmt.Printf("%s\n", hit.Summary)
			}
			fmt.Println()
		}
		return nil
	},
}

// publish command
var publishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Push wiki to remote",
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		outputJSON, _ := cmd.Flags().GetBool("output-json")

		result, err := publish.Publish(publish.PublishOptions{
			RepoRoot: repoRoot,
			DryRun:   dryRun,
		})
		if err != nil {
			return fmt.Errorf("publish failed: %w", err)
		}

		if outputJSON {
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		if dryRun {
			fmt.Println("[dry-run] No files were pushed.")
			fmt.Printf("Would push %d files:\n", len(result.FilesPushed))
		} else {
			fmt.Printf("Published %d files\n", len(result.FilesPushed))
		}

		return nil
	},
}

// gh-wiki-sync command
var ghWikiSyncCmd = &cobra.Command{
	Use:   "gh-wiki-sync",
	Short: "Sync wiki to GitHub Wiki with publish/exclude filtering",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		push, _ := cmd.Flags().GetBool("push")
		outputJSON, _ := cmd.Flags().GetBool("output-json")

		result, err := publish.GHWikiSync(publish.SyncOptions{
			RepoRoot: repoRoot,
			DryRun:   dryRun,
			Push:     push,
		})
		if err != nil {
			return fmt.Errorf("gh-wiki-sync failed: %w", err)
		}

		if outputJSON {
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
		} else {
			if dryRun {
				// Dry run summary is printed by the syncer
			} else {
				fmt.Printf("Synced %d pages to GitHub Wiki\n", len(result.PagesIncluded))
				if result.Commit != "" {
					fmt.Printf("Commit: %s\n", result.Commit)
				}
				if result.Pushed {
					fmt.Println("Pushed to remote.")
				}
			}
		}

		return nil
	},
}

// doctor command
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Validate Plexium configuration and setup",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		outputJSON, _ := cmd.Flags().GetBool("output-json")

		doctor := lint.NewDoctor(repoRoot)
		report, err := doctor.Run()
		if err != nil {
			return fmt.Errorf("doctor failed: %w", err)
		}

		if outputJSON {
			data, _ := report.ToJSON()
			fmt.Println(string(data))
		} else {
			fmt.Println("Plexium Doctor - Health Check")
			fmt.Println("==============================")

			for _, c := range report.Checks {
				icon := "✅"
				switch c.Status {
				case "pass":
					icon = "✅"
				case "fail":
					icon = "❌"
				case "warning":
					icon = "⚠️"
				case "skip":
					icon = "⏭️"
				}
				fmt.Printf("%s %s: %s\n", icon, c.Name, c.Message)
				if c.Remediation != "" {
					fmt.Printf("   → %s\n", c.Remediation)
				}
			}

			fmt.Println()
			passed, failed, warnings, skipped := report.Summary()
			fmt.Printf("Summary: %d passed, %d failed, %d warnings, %d skipped\n",
				passed, failed, warnings, skipped)
		}

		return nil
	},
}

// hook command — parent for subcommands
var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Git hook entry points",
}

// hook pre-commit subcommand
var hookPreCommitCmd = &cobra.Command{
	Use:   "pre-commit",
	Short: "Pre-commit hook: check wiki updated with source changes",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		cfg, _ := config.LoadFromDir(repoRoot)
		h := hook.NewPreCommitHook(repoRoot, cfg)

		result, err := h.Run(args) // args = staged files from lefthook
		if err != nil {
			return fmt.Errorf("pre-commit hook failed: %w", err)
		}

		if result.Skipped {
			if result.SkipReason != "" {
				fmt.Fprintf(os.Stderr, "plexium: skipped (%s)\n", result.SkipReason)
			}
			return nil
		}

		if result.Allowed {
			return nil
		}

		// Blocked
		fmt.Fprintf(os.Stderr, "\n⚠️  Code files changed but .wiki/ was not updated.\n")
		fmt.Fprintf(os.Stderr, "Ask your coding agent to document the changes, or run:\n")
		fmt.Fprintf(os.Stderr, "  plexium sync\n")
		fmt.Fprintf(os.Stderr, "To bypass (with audit trail): git commit --no-verify\n\n")
		fmt.Fprintf(os.Stderr, "Strictness: %s\n", result.Strictness)

		os.Exit(1)
		return nil
	},
}

// hook post-commit subcommand
var hookPostCommitCmd = &cobra.Command{
	Use:   "post-commit",
	Short: "Post-commit hook: track WIKI-DEBT on --no-verify bypass",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		cfg, _ := config.LoadFromDir(repoRoot)
		wikiRoot := ".wiki"
		if cfg != nil && cfg.Wiki.Root != "" {
			wikiRoot = cfg.Wiki.Root
		}

		h := hook.NewPostCommitHook(repoRoot, wikiRoot)
		if err := h.Run(); err != nil {
			return fmt.Errorf("post-commit hook failed: %w", err)
		}

		return nil
	},
}

var hookPostEditCmd = &cobra.Command{
	Use:   "post-edit",
	Short: "Advisory hook: remind agent to update wiki after editing source files",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return nil // advisory — never fail
		}
		h := hook.NewPostEditHook(repoRoot)
		_ = h.Run(cmd.InOrStdin())
		return nil
	},
}

// ci command — parent for subcommands
var ciCmd = &cobra.Command{
	Use:   "ci",
	Short: "CI integration commands",
}

// ci check subcommand
var ciiCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Diff-aware wiki check for CI pipelines",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseSHA, _ := cmd.Flags().GetString("base")
		headSHA, _ := cmd.Flags().GetString("head")
		outputFile, _ := cmd.Flags().GetString("output")
		outputJSON, _ := cmd.Flags().GetBool("output-json")

		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		cfg, _ := config.LoadFromDir(repoRoot)
		checker := ci.NewCICheck(repoRoot, cfg)

		result, err := checker.Run(baseSHA, headSHA)
		if err != nil {
			return fmt.Errorf("CI check failed: %w", err)
		}

		if outputJSON || outputFile != "" {
			data, err := result.ToJSON()
			if err != nil {
				return fmt.Errorf("marshaling result: %w", err)
			}
			if outputFile != "" {
				if err := os.WriteFile(outputFile, data, 0644); err != nil {
					return fmt.Errorf("writing output: %w", err)
				}
				fmt.Printf("Results written to %s\n", outputFile)
			} else {
				fmt.Println(string(data))
			}
		} else {
			// Human-readable
			fmt.Printf("CI Wiki Check: %s..%s\n", baseSHA[:7], headSHA[:7])
			fmt.Printf("Changed files: %d (source: %d)\n", len(result.ChangedFiles), len(result.SourceFiles))
			fmt.Printf("Wiki updated: %v\n", result.WikiUpdated)
			fmt.Printf("Wiki debt: %d entries\n", result.DebtCount)
			if len(result.UntrackedChanges) > 0 {
				fmt.Printf("Untracked source files:\n")
				for _, f := range result.UntrackedChanges {
					fmt.Printf("  - %s\n", f)
				}
			}
			if result.Passes {
				fmt.Println("✅ Passes")
			} else {
				fmt.Println("❌ Fails — wiki updates required")
			}
		}

		if !result.Passes {
			os.Exit(1)
		}
		return nil
	},
}

// migrate command
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run schema migrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		targetVersion, _ := cmd.Flags().GetInt("version")

		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		cfg, _ := config.LoadFromDir(repoRoot)
		wikiRoot := ".wiki"
		if cfg != nil && cfg.Wiki.Root != "" {
			wikiRoot = cfg.Wiki.Root
		}

		m := migrate.NewMigrator(repoRoot, wikiRoot)
		result, err := m.Migrate(targetVersion, dryRun)
		if err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}

		fmt.Printf("Schema version: %d → %d\n", result.CurrentVersion, result.TargetVersion)

		if len(result.Applied) > 0 {
			fmt.Printf("Applied %d migration(s):\n", len(result.Applied))
			for _, mg := range result.Applied {
				fmt.Printf("  %d: %s\n", mg.Number, mg.Name)
			}
		} else if dryRun {
			fmt.Println("No pending migrations.")
		} else {
			fmt.Println("No pending migrations.")
		}

		if len(result.Errors) > 0 {
			fmt.Fprintf(os.Stderr, "\nErrors:\n")
			for _, e := range result.Errors {
				fmt.Fprintf(os.Stderr, "  - %s\n", e)
			}
			os.Exit(1)
		}

		return nil
	},
}

// plugin command
var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage Plexium plugins",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("use: plexium plugin add <name>")
	},
}

// daemon command
var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run autonomous wiki maintenance loop",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		pollInterval, _ := cmd.Flags().GetInt("poll-interval")
		maxConcurrent, _ := cmd.Flags().GetInt("max-concurrent")

		cfg, _ := config.LoadFromDir(repoRoot)

		workspace := daemon.NewWorkspaceMgr(repoRoot)

		// Read runner/tracker from config, default to noop/none
		runnerType := "noop"
		runnerModel := ""
		trackerType := "none"
		if cfg != nil {
			if cfg.Daemon.Runner != "" {
				runnerType = cfg.Daemon.Runner
			}
			runnerModel = cfg.Daemon.RunnerModel
			if cfg.Daemon.Tracker != "" {
				trackerType = cfg.Daemon.Tracker
			}
		}

		// Derive owner/repo from git remote for GitHub tracker
		owner, repo := "", ""
		if trackerType == "github" {
			if remoteURL, gitErr := exec.Command("git", "-C", repoRoot, "remote", "get-url", "origin").Output(); gitErr == nil {
				owner, repo = parseGitRemote(strings.TrimSpace(string(remoteURL)))
			}
			if owner == "" || repo == "" {
				return fmt.Errorf("tracker %q requires owner/repo but could not derive from git remote origin", trackerType)
			}
		}
		tracker := daemon.NewTracker(trackerType, owner, repo, os.Getenv("GITHUB_TOKEN"))
		runner, err := daemon.NewRunner(runnerType, runnerModel)
		if err != nil {
			return fmt.Errorf("creating runner %q: %w", runnerType, err)
		}

		// Use config values as defaults when flags weren't explicitly set
		if cfg != nil && cfg.Daemon.Enabled {
			if !cmd.Flags().Changed("poll-interval") && cfg.Daemon.PollInterval > 0 {
				pollInterval = cfg.Daemon.PollInterval
			}
			if !cmd.Flags().Changed("max-concurrent") && cfg.Daemon.MaxConcurrent > 0 {
				maxConcurrent = cfg.Daemon.MaxConcurrent
			}
		}

		opts := daemon.DaemonOpts{
			RepoRoot:      repoRoot,
			PollInterval:  time.Duration(pollInterval) * time.Second,
			MaxConcurrent: maxConcurrent,
			RunnerName:    runnerType,
			Config:        cfg,
		}

		if cfg != nil {
			opts.ExecutionMode = cfg.Daemon.ExecutionMode
			opts.Watches = daemon.WatchOpts{
				Staleness: daemon.WatchDef{
					Enabled:   cfg.Daemon.Watches.Staleness.Enabled,
					Action:    cfg.Daemon.Watches.Staleness.Action,
					Threshold: cfg.Daemon.Watches.Staleness.Threshold,
				},
				Lint: daemon.WatchDef{
					Enabled: cfg.Daemon.Watches.Lint.Enabled,
					Action:  cfg.Daemon.Watches.Lint.Action,
				},
				Ingest: daemon.WatchDef{
					Enabled: cfg.Daemon.Watches.Ingest.Enabled,
					Action:  cfg.Daemon.Watches.Ingest.Action,
				},
				Debt: daemon.WatchDef{
					Enabled:   cfg.Daemon.Watches.Debt.Enabled,
					Action:    cfg.Daemon.Watches.Debt.Action,
					Threshold: fmt.Sprintf("%d", cfg.Daemon.Watches.Debt.MaxDebt),
				},
			}
		}

		cascade, rateTracker := buildCascadeFromConfig(cfg)
		d := daemon.NewDaemon(opts, workspace, tracker, runner, cascade, rateTracker)

		fmt.Printf("Plexium daemon starting (poll=%ds, maxConcurrent=%d)\n", pollInterval, maxConcurrent)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Clean up stale worktrees from prior runs
		if err := workspace.CleanupAll(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: worktree cleanup: %v\n", err)
		}

		return d.Run(ctx)
	},
}

// compile command
var compileCmd = &cobra.Command{
	Use:   "compile",
	Short: "Regenerate shared navigation files from manifest",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		outputJSON, _ := cmd.Flags().GetBool("output-json")

		compiler := compile.NewCompiler(repoRoot, dryRun)
		result, err := compiler.Compile()
		if err != nil {
			return fmt.Errorf("compile failed: %w", err)
		}

		if outputJSON {
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		if dryRun {
			fmt.Println("[dry-run] No files written.")
			fmt.Printf("Would generate: %s\n", strings.Join(result.FilesSkipped, ", "))
		} else {
			fmt.Printf("Generated %d files:\n", len(result.FilesGenerated))
			for _, f := range result.FilesGenerated {
				fmt.Printf("  %s\n", f)
			}
		}
		return nil
	},
}

// agent command — parent for subcommands
var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage assistive agent",
}

// buildCascadeFromConfig creates a ProviderCascade from the loaded config.
func buildCascadeFromConfig(cfg *config.Config) (*agent.ProviderCascade, *agent.RateLimitTracker) {
	var providers []agent.Provider
	if cfg != nil {
		for _, pc := range cfg.AssistiveAgent.Providers {
			if !pc.Enabled {
				continue
			}
			switch pc.Type {
			case "ollama":
				providers = append(providers, agent.NewOllamaProvider(pc.Endpoint, pc.Model, agent.DefaultOllamaHTTPPost))
			case "openai-compatible":
				apiKey := loadAPIKey(pc.APIKeyEnv)
				providers = append(providers, agent.NewOpenRouterProvider(pc.Endpoint, pc.Model, apiKey, 0.0, agent.DefaultOpenRouterHTTPPost))
			case "inherit":
				providers = append(providers, &agent.InheritProvider{})
			}
		}
	}

	retryPolicy := retry.DefaultPolicy()
	if cfg != nil {
		retryPolicy = retry.FromConfig(cfg.Retry)
	}

	cascade := agent.NewCascade(providers, retryPolicy)

	stateFile := ".plexium/agent-state.json"
	tracker := agent.NewRateLimitTracker(stateFile)

	return cascade, tracker
}

var agentStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the assistive agent daemon in the background",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		pidFile := filepath.Join(repoRoot, ".plexium", "daemon.pid")

		// Check if already running
		if pid, err := readPIDFile(pidFile); err == nil {
			if processAlive(pid) {
				fmt.Printf("Daemon already running (PID %d)\n", pid)
				return nil
			}
			_ = os.Remove(pidFile)
		}

		// Launch plexium daemon as a background process
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("finding executable: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(pidFile), 0o755); err != nil {
			return fmt.Errorf("creating daemon directory: %w", err)
		}

		outLog, err := os.OpenFile(filepath.Join(repoRoot, ".plexium", "daemon.out.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("opening daemon stdout log: %w", err)
		}
		defer outLog.Close()
		errLog, err := os.OpenFile(filepath.Join(repoRoot, ".plexium", "daemon.err.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("opening daemon stderr log: %w", err)
		}
		defer errLog.Close()

		proc := exec.Command(exe, "daemon")
		proc.Dir = repoRoot
		proc.Stdout = outLog
		proc.Stderr = errLog
		proc.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

		if err := proc.Start(); err != nil {
			return fmt.Errorf("starting daemon: %w", err)
		}

		pid := proc.Process.Pid

		// Brief liveness check — verify process survived init
		time.Sleep(100 * time.Millisecond)
		if !processAlive(pid) {
			return fmt.Errorf("daemon exited immediately after start (PID %d)", pid)
		}

		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0o644); err != nil {
			_ = proc.Process.Kill()
			return fmt.Errorf("writing PID file: %w", err)
		}

		fmt.Printf("Daemon started (PID %d)\n", pid)
		fmt.Println("Use 'plexium agent stop' to stop, 'plexium agent status' to check.")

		_ = proc.Process.Release()
		return nil
	},
}

var agentStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the assistive agent daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		pidFile := filepath.Join(repoRoot, ".plexium", "daemon.pid")
		pid, err := readPIDFile(pidFile)
		if err != nil {
			fmt.Println("No daemon running (PID file not found)")
			return nil
		}

		process, err := os.FindProcess(pid)
		if err != nil {
			_ = os.Remove(pidFile)
			fmt.Println("No daemon running (process not found)")
			return nil
		}

		if err := process.Signal(syscall.SIGTERM); err != nil {
			_ = os.Remove(pidFile)
			fmt.Printf("Daemon process %d not responding, cleaned up PID file\n", pid)
			return nil
		}

		_ = os.Remove(pidFile)
		fmt.Printf("Daemon stopped (PID %d)\n", pid)
		return nil
	},
}

func readPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

// parseGitRemote extracts owner/repo from a git remote URL.
// Supports HTTPS (https://github.com/owner/repo.git) and SSH (git@github.com:owner/repo.git).
func parseGitRemote(remoteURL string) (string, string) {
	// HTTPS: https://github.com/owner/repo.git
	if strings.Contains(remoteURL, "://") {
		remoteURL = strings.TrimSuffix(remoteURL, ".git")
		parts := strings.Split(remoteURL, "/")
		if len(parts) >= 2 {
			return parts[len(parts)-2], parts[len(parts)-1]
		}
	}
	// SSH: git@github.com:owner/repo.git
	if strings.Contains(remoteURL, ":") {
		after := remoteURL[strings.Index(remoteURL, ":")+1:]
		after = strings.TrimSuffix(after, ".git")
		parts := strings.Split(after, "/")
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}
	return "", ""
}

// loadAPIKey tries .plexium/credentials.json first, then falls back to environment variable.
func loadAPIKey(envName string) string {
	if cwd, err := os.Getwd(); err == nil {
		credPath := filepath.Join(cwd, ".plexium", "credentials.json")
		if data, err := os.ReadFile(credPath); err == nil {
			var creds map[string]string
			if err := json.Unmarshal(data, &creds); err == nil {
				if key := creds["openrouter_api_key"]; key != "" {
					return key
				}
			}
		}
	}
	return os.Getenv(envName)
}

var agentStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show assistive agent status and provider health",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		cfg, _ := config.LoadFromDir(repoRoot)
		outputJSON, _ := cmd.Flags().GetBool("output-json")
		status, err := collectAgentStatus(repoRoot, cfg)
		if err != nil {
			return err
		}

		if outputJSON {
			fmt.Println(marshalAgentStatus(status))
			return nil
		}

		fmt.Print(formatAgentStatus(status))
		return nil
	},
}

var agentTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test provider connectivity",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		cfg, _ := config.LoadFromDir(repoRoot)
		cascade, _ := buildCascadeFromConfig(cfg)

		providerName, _ := cmd.Flags().GetString("provider")

		fmt.Printf("Testing provider cascade...\n")
		result, err := cascade.Complete(context.Background(), "Respond with: OK")
		if err != nil {
			fmt.Printf("Cascade test failed: %v\n", err)
			if providerName != "" {
				fmt.Printf("(filtered to provider: %s)\n", providerName)
			}
			return nil
		}

		fmt.Printf("Success via %s (latency: %dms, tokens: %d, cost: $%.4f)\n",
			result.Provider, result.LatencyMs, result.TokensUsed, result.CostUSD)
		return nil
	},
}

var agentSpendCmd = &cobra.Command{
	Use:   "spend",
	Short: "Show daily spend per provider",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		cfg, _ := config.LoadFromDir(repoRoot)
		_, rateTracker := buildCascadeFromConfig(cfg)
		outputJSON, _ := cmd.Flags().GetBool("output-json")

		state, err := rateTracker.Load()
		if err != nil {
			return fmt.Errorf("loading usage state: %w", err)
		}

		if outputJSON {
			data, _ := json.MarshalIndent(state, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		fmt.Printf("Daily spend (%s):\n\n", state.Date)
		totalCost := 0.0
		for name, rec := range state.Records {
			fmt.Printf("  %s: %d requests, %d tokens, $%.4f\n", name, rec.Requests, rec.Tokens, rec.CostUSD)
			totalCost += rec.CostUSD
		}
		fmt.Printf("\n  Total: $%.4f\n", totalCost)

		if cfg != nil && cfg.AssistiveAgent.Budget.DailyUSD > 0 {
			fmt.Printf("  Budget: $%.2f (%.1f%% used)\n",
				cfg.AssistiveAgent.Budget.DailyUSD,
				(totalCost/cfg.AssistiveAgent.Budget.DailyUSD)*100)
		}
		return nil
	},
}

var agentBenchmarkCmd = &cobra.Command{
	Use:   "benchmark",
	Short: "Benchmark provider latency",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		cfg, _ := config.LoadFromDir(repoRoot)
		cascade, _ := buildCascadeFromConfig(cfg)

		fmt.Println("Benchmarking providers (3 rounds)...")
		for i := 1; i <= 3; i++ {
			result, err := cascade.Complete(context.Background(), "Respond with: OK")
			if err != nil {
				fmt.Printf("  Round %d: FAILED (%v)\n", i, err)
				continue
			}
			fmt.Printf("  Round %d: %s — %dms\n", i, result.Provider, result.LatencyMs)
		}
		return nil
	},
}

// orchestrate command
var orchestrateCmd = &cobra.Command{
	Use:   "orchestrate",
	Short: "Run single orchestrated wiki-update for an issue",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		issueID, _ := cmd.Flags().GetString("issue")
		outputJSON, _ := cmd.Flags().GetBool("output-json")

		cfg, _ := config.LoadFromDir(repoRoot)
		runnerType := "noop"
		runnerModel := ""
		if cfg != nil && cfg.Daemon.Runner != "" {
			runnerType = cfg.Daemon.Runner
			runnerModel = cfg.Daemon.RunnerModel
		}

		workspace := daemon.NewWorkspaceMgr(repoRoot)
		runner, err := daemon.NewRunner(runnerType, runnerModel)
		if err != nil {
			return fmt.Errorf("creating runner %q: %w", runnerType, err)
		}

		// Create isolated worktree
		wt, err := workspace.Create(issueID)
		if err != nil {
			return fmt.Errorf("creating workspace: %w", err)
		}
		defer func() {
			_ = workspace.Cleanup(wt.ID)
		}()

		fmt.Printf("Orchestrating wiki-update for issue %s in %s\n", issueID, wt.Path)

		// Run agent sequence: Retriever → Documenter → Linter
		roles := []string{"retriever", "documenter"}
		var results []struct {
			Role    string `json:"role"`
			Output  string `json:"output"`
			Latency int64  `json:"latencyMs"`
		}

		for _, role := range roles {
			prompt := fmt.Sprintf("Wiki maintenance for issue %s. Role: %s. Workspace: %s", issueID, role, wt.Path)
			result, runErr := runner.Run(context.Background(), role, prompt, nil, wt.Path)
			if runErr != nil {
				fmt.Fprintf(os.Stderr, "  %s: failed (%v)\n", role, runErr)
				continue
			}
			results = append(results, struct {
				Role    string `json:"role"`
				Output  string `json:"output"`
				Latency int64  `json:"latencyMs"`
			}{Role: role, Output: result.Output, Latency: result.LatencyMs})
			fmt.Printf("  %s: done (%dms)\n", role, result.LatencyMs)
		}

		if outputJSON {
			data, _ := json.MarshalIndent(results, "", "  ")
			fmt.Println(string(data))
		}

		fmt.Printf("Orchestration complete for issue %s\n", issueID)
		return nil
	},
}

// pageindex command — parent for subcommands
var pageidxCmd = &cobra.Command{
	Use:   "pageindex",
	Short: "PageIndex MCP server management",
}

// pageindex serve subcommand
var pageidxServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start PageIndex MCP server (stdio mode)",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		cfg, _ := config.LoadFromDir(repoRoot)
		wikiRoot := ".wiki"
		if cfg != nil && cfg.Wiki.Root != "" {
			wikiRoot = cfg.Wiki.Root
		}

		server := pageindex.NewServer(filepath.Join(repoRoot, wikiRoot))
		fmt.Fprintf(os.Stderr, "PageIndex MCP server running (stdio mode)\n")
		return server.Start()
	},
}

// beads command — parent for subcommands
var beadsCmd = &cobra.Command{
	Use:   "beads",
	Short: "Manage beads task ↔ wiki page links",
}

// beads link subcommand
var beadsLinkCmd = &cobra.Command{
	Use:   "link <task-id> <wiki-path>",
	Short: "Link a task ID to a wiki page",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		cfg, _ := config.LoadFromDir(repoRoot)
		wikiRoot := ".wiki"
		if cfg != nil && cfg.Wiki.Root != "" {
			wikiRoot = cfg.Wiki.Root
		}

		linker := beads.NewLinker(filepath.Join(repoRoot, wikiRoot))
		result, err := linker.LinkTaskToPage(args[0], args[1])
		if err != nil {
			return fmt.Errorf("link failed: %w", err)
		}

		outputJSON, _ := cmd.Flags().GetBool("output-json")
		if outputJSON {
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
		} else {
			fmt.Printf("%s: %s ↔ %s\n", result.Action, result.TaskID, result.WikiPath)
		}
		return nil
	},
}

// beads unlink subcommand
var beadsUnlinkCmd = &cobra.Command{
	Use:   "unlink <task-id> <wiki-path>",
	Short: "Remove a task ID from a wiki page",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		cfg, _ := config.LoadFromDir(repoRoot)
		wikiRoot := ".wiki"
		if cfg != nil && cfg.Wiki.Root != "" {
			wikiRoot = cfg.Wiki.Root
		}

		linker := beads.NewLinker(filepath.Join(repoRoot, wikiRoot))
		result, err := linker.UnlinkTaskFromPage(args[0], args[1])
		if err != nil {
			return fmt.Errorf("unlink failed: %w", err)
		}

		outputJSON, _ := cmd.Flags().GetBool("output-json")
		if outputJSON {
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
		} else {
			fmt.Printf("%s: %s ↔ %s\n", result.Action, result.TaskID, result.WikiPath)
		}
		return nil
	},
}

// beads pages subcommand
var beadsPagesCmd = &cobra.Command{
	Use:   "pages <task-id>",
	Short: "Show wiki pages linked to a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		cfg, _ := config.LoadFromDir(repoRoot)
		wikiRoot := ".wiki"
		if cfg != nil && cfg.Wiki.Root != "" {
			wikiRoot = cfg.Wiki.Root
		}

		linker := beads.NewLinker(filepath.Join(repoRoot, wikiRoot))
		mapping, err := linker.GetTaskPages(args[0])
		if err != nil {
			return fmt.Errorf("lookup failed: %w", err)
		}

		outputJSON, _ := cmd.Flags().GetBool("output-json")
		if outputJSON {
			data, _ := json.MarshalIndent(mapping, "", "  ")
			fmt.Println(string(data))
		} else {
			if len(mapping.WikiPaths) == 0 {
				fmt.Printf("No wiki pages linked to task %s\n", args[0])
			} else {
				fmt.Printf("Task %s → %d page(s):\n", mapping.TaskID, len(mapping.WikiPaths))
				for _, p := range mapping.WikiPaths {
					fmt.Printf("  %s\n", p)
				}
			}
		}
		return nil
	},
}

// beads tasks subcommand
var beadsTasksCmd = &cobra.Command{
	Use:   "tasks <wiki-path>",
	Short: "Show task IDs linked to a wiki page",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		cfg, _ := config.LoadFromDir(repoRoot)
		wikiRoot := ".wiki"
		if cfg != nil && cfg.Wiki.Root != "" {
			wikiRoot = cfg.Wiki.Root
		}

		linker := beads.NewLinker(filepath.Join(repoRoot, wikiRoot))
		mapping, err := linker.GetPageTasks(args[0])
		if err != nil {
			return fmt.Errorf("lookup failed: %w", err)
		}

		outputJSON, _ := cmd.Flags().GetBool("output-json")
		if outputJSON {
			data, _ := json.MarshalIndent(mapping, "", "  ")
			fmt.Println(string(data))
		} else {
			if len(mapping.TaskIDs) == 0 {
				fmt.Printf("No tasks linked to %s\n", args[0])
			} else {
				fmt.Printf("%s → %d task(s):\n", mapping.WikiPath, len(mapping.TaskIDs))
				for _, id := range mapping.TaskIDs {
					fmt.Printf("  %s\n", id)
				}
			}
		}
		return nil
	},
}

// beads scan subcommand
var beadsScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan all wiki pages for beads task links",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		cfg, _ := config.LoadFromDir(repoRoot)
		wikiRoot := ".wiki"
		if cfg != nil && cfg.Wiki.Root != "" {
			wikiRoot = cfg.Wiki.Root
		}

		linker := beads.NewLinker(filepath.Join(repoRoot, wikiRoot))
		mappings, err := linker.ScanAllLinks()
		if err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}

		outputJSON, _ := cmd.Flags().GetBool("output-json")
		if outputJSON {
			data, _ := json.MarshalIndent(mappings, "", "  ")
			fmt.Println(string(data))
		} else {
			if len(mappings) == 0 {
				fmt.Println("No beads task links found in wiki.")
			} else {
				fmt.Printf("Found %d task(s) with wiki links:\n", len(mappings))
				for _, m := range mappings {
					fmt.Printf("  %s → %s\n", m.TaskID, strings.Join(m.WikiPaths, ", "))
				}
			}
		}
		return nil
	},
}

var agentSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Provider setup for Ollama and OpenRouter",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		apiKey, err := resolveSetupAPIKey(cmd, cmd.InOrStdin())
		if err != nil {
			return err
		}
		result, err := agent.RunInteractiveSetup(repoRoot, agent.SetupOptions{
			APIKey:         apiKey,
			Stdin:          cmd.InOrStdin(),
			Model:          strings.TrimSpace(mustGetString(cmd, "model")),
			DailyBudgetUSD: resolveSetupBudget(cmd),
			Stdout:         cmd.OutOrStdout(),
			Stderr:         cmd.ErrOrStderr(),
		})
		if err != nil {
			return fmt.Errorf("setup failed: %w", err)
		}
		if err := applySetupExecutionMode(repoRoot, strings.TrimSpace(mustGetString(cmd, "execution-mode"))); err != nil {
			return err
		}

		fmt.Println()
		if len(result.ProvidersConfigured) == 0 {
			fmt.Println("No providers configured. Run 'plexium agent setup' again when ready.")
		} else {
			fmt.Printf("Configured: %s\n", strings.Join(result.ProvidersConfigured, ", "))
			if result.ConfigUpdated {
				fmt.Println("Config updated in .plexium/config.yml")
			}
			fmt.Println("\nNext steps:")
			fmt.Println("  plexium agent test     — verify connectivity")
			fmt.Println("  plexium agent status   — check provider health")
		}
		return nil
	},
}

func resolveSetupAPIKey(cmd *cobra.Command, stdin io.Reader) (string, error) {
	apiKeyFile, _ := cmd.Flags().GetString("api-key-file")
	apiKeyStdin, _ := cmd.Flags().GetBool("api-key-stdin")

	if apiKeyFile != "" && apiKeyStdin {
		return "", fmt.Errorf("use only one of --api-key-file or --api-key-stdin")
	}

	if apiKeyFile != "" {
		data, err := os.ReadFile(apiKeyFile)
		if err != nil {
			return "", fmt.Errorf("read API key file: %w", err)
		}
		key := strings.TrimSpace(string(data))
		if key == "" {
			return "", fmt.Errorf("API key file is empty")
		}
		return key, nil
	}

	if apiKeyStdin {
		if file, ok := stdin.(*os.File); ok {
			if stat, err := file.Stat(); err == nil && (stat.Mode()&os.ModeCharDevice) != 0 {
				return "", fmt.Errorf("--api-key-stdin requires piped input")
			}
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read API key from stdin: %w", err)
		}
		key := strings.TrimSpace(string(data))
		if key == "" {
			return "", fmt.Errorf("--api-key-stdin requires piped input")
		}
		return key, nil
	}

	return "", nil
}

func resolveSetupBudget(cmd *cobra.Command) *float64 {
	if !cmd.Flags().Changed("daily-budget-usd") {
		return nil
	}
	value, _ := cmd.Flags().GetFloat64("daily-budget-usd")
	return &value
}

func mustGetString(cmd *cobra.Command, name string) string {
	value, _ := cmd.Flags().GetString(name)
	return value
}

func applySetupExecutionMode(repoRoot, mode string) error {
	if mode == "" {
		return nil
	}

	cfg, err := config.LoadFromDir(repoRoot)
	if err != nil {
		return fmt.Errorf("load config for execution mode update: %w", err)
	}

	switch mode {
	case "coding-agent-primary", "provider-primary":
	default:
		return fmt.Errorf("invalid --execution-mode %q (expected coding-agent-primary or provider-primary)", mode)
	}

	if mode == "provider-primary" {
		hasProvider := false
		for _, provider := range cfg.AssistiveAgent.Providers {
			if provider.Enabled {
				hasProvider = true
				break
			}
		}
		if !hasProvider {
			return fmt.Errorf("provider-primary requires at least one enabled assistive provider")
		}
	}

	cfg.Daemon.ExecutionMode = mode
	if err := config.SaveToDir(repoRoot, cfg); err != nil {
		return fmt.Errorf("save execution mode: %w", err)
	}
	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}