package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/convert"
	"github.com/Clarit-AI/Plexium/internal/lint"
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

	// convert flags
	convertCmd.Flags().String("depth", "shallow", "Scour depth: shallow|deep")
	convertCmd.Flags().Bool("dry-run", false, "Preview without writing to .wiki/")
	convertCmd.Flags().String("agent", "", "Run specified agent adapter after conversion")

	// publish flags
	publishCmd.Flags().Bool("dry-run", false, "Preview without pushing")

	// lint flags
	lintCmd.Flags().Bool("deterministic", false, "Run deterministic checks only (link/orphan/staleness validation)")
	lintCmd.Flags().Bool("ci", false, "CI mode: exit with non-zero code on lint errors or warnings")
	lintCmd.Flags().String("fail-on", "error", "Exit non-zero on this severity: error|warning")

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

		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		outputJSON, _ := cmd.Flags().GetBool("output-json")

		result, err := wiki.Init(wiki.InitOptions{
			RepoRoot:   repoRoot,
			GitHubWiki: githubWiki,
			Obsidian:   obsidian,
			Strictness: strictness,
			DryRun:     dryRun,
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
		ciMode, _ := cmd.Flags().GetBool("ci")

		// --full (semantic lint) is Phase 9 work; only deterministic is available
		if !deterministic {
			fmt.Println("Note: --full semantic lint is not yet implemented.")
			fmt.Println("Running deterministic checks only. Use --deterministic to suppress this message.")
			fmt.Println()
		}

		// Load config
		cfg, _ := config.LoadFromDir(repoRoot)

		linter := lint.NewLinter(repoRoot, cfg)
		report, err := linter.RunDeterministic()
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
		fmt.Println(" plexium retrieve")
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
	Short: "Sync wiki to GitHub Wiki",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(" plexium gh-wiki-sync")
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

// migrate command
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run schema migrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(" plexium migrate")
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

// hook command
var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Git hook entry points",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(" plexium hook")
		return nil
	},
}

// ci command
var ciCmd = &cobra.Command{
	Use:   "ci",
	Short: "CI integration commands",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("use: plexium ci check --base SHA --head SHA")
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