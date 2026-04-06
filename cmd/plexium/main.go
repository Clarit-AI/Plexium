package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Clarit-AI/Plexium/internal/ci"
	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/convert"
	"github.com/Clarit-AI/Plexium/internal/hook"
	"github.com/Clarit-AI/Plexium/internal/integrations/pageindex"
	"github.com/Clarit-AI/Plexium/internal/lint"
	"github.com/Clarit-AI/Plexium/internal/migrate"
	"github.com/Clarit-AI/Plexium/internal/publish"
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

	// Register subcommands
	ciCmd.AddCommand(ciiCheckCmd)
	hookCmd.AddCommand(hookPreCommitCmd)
	hookCmd.AddCommand(hookPostCommitCmd)

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

		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
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

		if outputJSON {
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		if dryRun {
			fmt.Println("[dry-run] No files were created.")
			fmt.Printf("Would create %d directories and %d files:\n", len(result.DirsCreated), len(result.FilesCreated))
		} else {
			fmt.Printf("Initialized Plexium wiki in %s\n", result.WikiDir)
			fmt.Printf("Created %d directories and %d files\n", len(result.DirsCreated), len(result.FilesCreated))
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
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(" plexium sync")
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
			// LLM-augmented lint — RunFull with nil client uses deterministic only
			// A real LLM client would be injected here when integrations.llmProvider is configured
			report, err = linter.RunFull(nil)
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
	Short: "Run Plexium in daemon mode",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(" plexium daemon")
		return nil
	},
}

// compile command
var compileCmd = &cobra.Command{
	Use:   "compile",
	Short: "Regenerate shared navigation files",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(" plexium compile")
		return nil
	},
}

// agent command
var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage assistive agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(" plexium agent")
		return nil
	},
}

// orchestrate command
var orchestrateCmd = &cobra.Command{
	Use:   "orchestrate",
	Short: "Run orchestrated wiki-update for issue",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(" plexium orchestrate")
		return nil
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}