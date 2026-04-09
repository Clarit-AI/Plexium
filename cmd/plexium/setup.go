package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Clarit-AI/Plexium/internal/agent"
	"github.com/Clarit-AI/Plexium/internal/compile"
	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/integrations/memento"
	"github.com/Clarit-AI/Plexium/internal/integrations/pageindex"
	"github.com/Clarit-AI/Plexium/internal/lint"
	"github.com/Clarit-AI/Plexium/internal/plugins"
	"github.com/Clarit-AI/Plexium/internal/prompts"
	"github.com/Clarit-AI/Plexium/internal/wiki"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

type setupStep struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type setupResult struct {
	Agent       string                `json:"agent"`
	RepoRoot    string                `json:"repoRoot"`
	WriteConfig bool                  `json:"writeConfig"`
	ConnectPlan *pageindexConnectPlan `json:"connectPlan"`
	Steps       []setupStep           `json:"steps"`
	Verify      *verifyResult         `json:"verify"`
	NextSteps   []string              `json:"nextSteps,omitempty"`
}

type verifyCheck struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

type verifyResult struct {
	Agent       string                `json:"agent"`
	RepoRoot    string                `json:"repoRoot"`
	Ready       bool                  `json:"ready"`
	Configured  bool                  `json:"configured"`
	ConnectPlan *pageindexConnectPlan `json:"connectPlan"`
	Checks      []verifyCheck         `json:"checks"`
}

type setupAgentOptions struct {
	WriteConfig        bool
	WithMemento        bool
	Stdin              io.Reader
	Stdout             io.Writer
	Stderr             io.Writer
	PromptForAssistive bool
}

var setupCmd = &cobra.Command{
	Use:   "setup <agent>",
	Short: "Initialize, connect, and verify Plexium for an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runSetupCommand,
}

var verifyCmd = &cobra.Command{
	Use:   "verify <agent>",
	Short: "Verify that Plexium is ready for an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runVerifyCommand,
}

func init() {
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(verifyCmd)

	setupCmd.Flags().Bool("write-config", false, "Run the native MCP configuration command")
	setupCmd.Flags().Bool("with-memento", false, "Initialize optional git-memento session tracking for this repository")
}

func runSetupCommand(cmd *cobra.Command, args []string) error {
	repoRoot, err := currentGitRepoRoot()
	if err != nil {
		return err
	}

	writeConfig, _ := cmd.Flags().GetBool("write-config")
	withMemento, _ := cmd.Flags().GetBool("with-memento")
	outputJSON, _ := cmd.Flags().GetBool("output-json")

	setupStdout := cmd.OutOrStdout()
	setupStderr := cmd.ErrOrStderr()
	if outputJSON {
		setupStdout = setupStderr
		setupStderr = &bytes.Buffer{}
	}

	result, err := setupAgent(repoRoot, args[0], setupAgentOptions{
		WriteConfig:        writeConfig,
		WithMemento:        withMemento,
		Stdin:              cmd.InOrStdin(),
		Stdout:             setupStdout,
		Stderr:             setupStderr,
		PromptForAssistive: !outputJSON,
	})
	if err != nil {
		return err
	}

	if outputJSON {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal setup result to JSON: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Plexium setup for %s\n", capitalizeFirst(result.Agent))
	fmt.Printf("Repository: %s\n", result.RepoRoot)
	for _, step := range result.Steps {
		fmt.Printf("- %s: %s\n", step.Name, step.Message)
	}

	passCount, warnCount, failCount := result.Verify.summary()
	fmt.Printf("Verification: %d pass, %d warning, %d fail\n", passCount, warnCount, failCount)
	if !result.Verify.Configured {
		fmt.Printf("Native MCP command: %s\n", result.ConnectPlan.Command)
		if !writeConfig {
			fmt.Println("Run the command above or rerun with --write-config to apply it for you.")
		}
	}
	if len(result.NextSteps) > 0 {
		fmt.Println("What to do next:")
		for i, step := range result.NextSteps {
			fmt.Printf("  %d. %s\n", i+1, step)
		}
	}
	if result.Verify.Ready {
		fmt.Println("Plexium tooling is wired. The wiki scaffold is intentionally minimal until you run `plexium convert` and let an agent enrich it.")
		return nil
	}
	return fmt.Errorf("setup completed with verification failures")
}

func runVerifyCommand(cmd *cobra.Command, args []string) error {
	repoRoot, err := currentGitRepoRoot()
	if err != nil {
		return err
	}

	outputJSON, _ := cmd.Flags().GetBool("output-json")
	result, err := verifyAgent(repoRoot, args[0])
	if err != nil {
		return err
	}

	if outputJSON {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal verify result to JSON: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Plexium verification for %s\n", capitalizeFirst(result.Agent))
	fmt.Printf("Repository: %s\n", result.RepoRoot)
	for _, check := range result.Checks {
		icon := "✅"
		switch check.Status {
		case "warning":
			icon = "⚠️"
		case "fail":
			icon = "❌"
		}
		fmt.Printf("%s %s: %s\n", icon, check.Name, check.Message)
		if check.Remediation != "" {
			fmt.Printf("   → %s\n", check.Remediation)
		}
	}

	if result.Ready {
		fmt.Println("Plexium is ready for this agent.")
		return nil
	}
	return fmt.Errorf("verification failed")
}

func setupAgent(repoRoot, agent string, opts setupAgentOptions) (*setupResult, error) {
	normalizedAgent := normalizeAgentName(agent)
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	plan, err := buildPageIndexConnectPlan(normalizedAgent)
	if err != nil {
		return nil, err
	}

	result := &setupResult{
		Agent:       normalizedAgent,
		RepoRoot:    repoRoot,
		WriteConfig: opts.WriteConfig,
		ConnectPlan: plan,
	}

	if needsPlexiumInit(repoRoot) {
		if _, err := wiki.Init(wiki.InitOptions{
			RepoRoot:      repoRoot,
			WithMemento:   false,
			WithPageIndex: true,
		}); err != nil {
			return nil, fmt.Errorf("initialize Plexium: %w", err)
		}
		result.Steps = append(result.Steps, setupStep{Name: "init", Status: "pass", Message: "initialized Plexium scaffold"})
	} else {
		result.Steps = append(result.Steps, setupStep{Name: "init", Status: "pass", Message: "existing Plexium scaffold detected"})
	}

	createdPrompts, err := prompts.EnsureRepoPack(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("materialize prompt pack: %w", err)
	}
	if len(createdPrompts) > 0 {
		result.Steps = append(result.Steps, setupStep{
			Name:    "prompts",
			Status:  "pass",
			Message: fmt.Sprintf("materialized editable prompt pack in %s", relativeToRepo(repoRoot, filepath.Join(repoRoot, ".plexium", "prompts"))),
		})
	} else {
		result.Steps = append(result.Steps, setupStep{
			Name:    "prompts",
			Status:  "pass",
			Message: "editable prompt pack already present",
		})
	}

	if _, err := compile.NewCompiler(repoRoot, false).Compile(); err != nil {
		return nil, fmt.Errorf("compile navigation: %w", err)
	}
	result.Steps = append(result.Steps, setupStep{Name: "compile", Status: "pass", Message: "compiled navigation files"})

	installResult, err := plugins.InstallAdapter(repoRoot, normalizedAgent, "")
	if err != nil {
		return nil, fmt.Errorf("install %s adapter: %w", normalizedAgent, err)
	}
	result.Steps = append(result.Steps, setupStep{
		Name:    "adapter",
		Status:  "pass",
		Message: fmt.Sprintf("installed %s adapter and generated %s", sourceLabel(installResult.BuiltIn), installResult.InstructionFile),
	})

	mcpPath, _, err := pageindex.EnsureProjectReference(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("write PageIndex reference: %w", err)
	}
	result.Steps = append(result.Steps, setupStep{
		Name:    "pageindex",
		Status:  "pass",
		Message: fmt.Sprintf("ensured PageIndex reference at %s", relativeToRepo(repoRoot, mcpPath)),
	})

	configured, _, err := detectMCPConfig(repoRoot, normalizedAgent)
	if err != nil {
		return nil, err
	}
	if opts.WithMemento {
		result.Steps = append(result.Steps, configureMemento(repoRoot, normalizedAgent, opts))
	}

	if opts.WriteConfig && !configured {
		if err := runPageIndexConnect(context.Background(), repoRoot, plan); err != nil {
			return nil, err
		}
		result.Steps = append(result.Steps, setupStep{
			Name:    "connect",
			Status:  "pass",
			Message: fmt.Sprintf("applied native %s MCP configuration", capitalizeFirst(normalizedAgent)),
		})
	} else if configured {
		result.Steps = append(result.Steps, setupStep{
			Name:    "connect",
			Status:  "pass",
			Message: fmt.Sprintf("%s MCP configuration already present", capitalizeFirst(normalizedAgent)),
		})
	} else {
		result.Steps = append(result.Steps, setupStep{
			Name:    "connect",
			Status:  "warning",
			Message: fmt.Sprintf("native MCP command ready: %s", plan.Command),
		})
	}

	result.Steps = append(result.Steps, maybeConfigureAssistiveProvider(repoRoot, normalizedAgent, opts))

	result.Verify, err = verifyAgent(repoRoot, normalizedAgent)
	if err != nil {
		return nil, err
	}
	result.NextSteps = buildSetupNextSteps(result)

	return result, nil
}

func configureMemento(repoRoot, agent string, opts setupAgentOptions) setupStep {
	result, err := memento.EnsureCLI(memento.EnsureCLIOptions{
		Stdin:  opts.Stdin,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
	})
	if err != nil {
		return setupStep{
			Name:    "memento",
			Status:  "warning",
			Message: fmt.Sprintf("git-memento install attempt failed: %v", err),
		}
	}

	if result == nil {
		return setupStep{Name: "memento", Status: "warning", Message: "git-memento setup was skipped"}
	}
	if !result.Available {
		message := "git-memento is still optional and not configured yet"
		if result.InstallCommand != "" {
			message = fmt.Sprintf("%s; install later with `%s`", message, result.InstallCommand)
		} else if result.ReleaseURL != "" {
			message = fmt.Sprintf("%s; install from %s", message, result.ReleaseURL)
		} else if result.ProjectURL != "" {
			message = fmt.Sprintf("%s; install from %s", message, result.ProjectURL)
		}
		return setupStep{Name: "memento", Status: "warning", Message: message}
	}

	initialized, err := memento.IsInitialized(repoRoot)
	if err != nil {
		return setupStep{
			Name:    "memento",
			Status:  "warning",
			Message: fmt.Sprintf("git-memento is installed but repo config could not be inspected: %v", err),
		}
	}
	provider, err := memento.ConfiguredProvider(repoRoot)
	if err != nil {
		return setupStep{
			Name:    "memento",
			Status:  "warning",
			Message: fmt.Sprintf("git-memento is installed but the configured provider could not be read: %v", err),
		}
	}
	if !initialized || provider != agent {
		if err := memento.InitRepo(repoRoot, agent); err != nil {
			return setupStep{
				Name:    "memento",
				Status:  "warning",
				Message: fmt.Sprintf("git-memento is installed but repo initialization failed: %v", err),
			}
		}
	}

	if agent == "claude" {
		if err := memento.ConfigureClaudeShim(repoRoot); err != nil {
			return setupStep{
				Name:    "memento",
				Status:  "warning",
				Message: fmt.Sprintf("git-memento is initialized but the Claude compatibility shim could not be configured: %v", err),
			}
		}
	}

	if err := enableMementoInConfig(repoRoot); err != nil {
		return setupStep{
			Name:    "memento",
			Status:  "warning",
			Message: fmt.Sprintf("git-memento is ready but Plexium config could not be updated: %v", err),
		}
	}

	message := "initialized git-memento for this repository"
	if result.Installed {
		message = "installed git-memento and initialized repo-local session tracking"
	}
	if agent == "claude" {
		message += " with the temporary Claude compatibility shim"
	}
	return setupStep{Name: "memento", Status: "pass", Message: message}
}

func maybeConfigureAssistiveProvider(repoRoot, agentName string, opts setupAgentOptions) setupStep {
	cfg, err := config.LoadFromDir(repoRoot)
	if err != nil {
		return setupStep{
			Name:    "assistive",
			Status:  "warning",
			Message: "assistive provider setup is available after the Plexium config can be loaded",
		}
	}

	providers := configuredProviderNames(cfg)
	if len(providers) > 0 {
		return setupStep{
			Name:    "assistive",
			Status:  "pass",
			Message: fmt.Sprintf("assistive provider already configured (%s, profile %s)", strings.Join(providers, ", "), prompts.ProfileFromConfig(cfg)),
		}
	}

	if !opts.PromptForAssistive || !isInteractiveReader(opts.Stdin) {
		return setupStep{
			Name:    "assistive",
			Status:  "warning",
			Message: fmt.Sprintf("no assistive provider configured yet; run `plexium convert`, then use %s, or run `plexium agent setup` to add Ollama/OpenRouter", initialPopulationMode(agentName)),
		}
	}

	fmt.Fprintln(opts.Stdout)
	fmt.Fprintln(opts.Stdout, "No assistive provider is configured yet.")
	fmt.Fprintln(opts.Stdout, "Plexium works without one, but autonomous upkeep, LLM lint, and maintenance helpers need Ollama or OpenRouter.")
	if !promptYesNo(opts.Stdin, opts.Stdout, "Configure an assistive provider now?", false) {
		return setupStep{
			Name:    "assistive",
			Status:  "warning",
			Message: fmt.Sprintf("skipped assistive provider setup; run `plexium convert` first, then use %s to enrich the wiki", initialPopulationMode(agentName)),
		}
	}

	setupResult, err := agent.RunInteractiveSetup(repoRoot, agent.SetupOptions{
		Stdin:  opts.Stdin,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
	})
	if err != nil {
		return setupStep{
			Name:    "assistive",
			Status:  "warning",
			Message: fmt.Sprintf("assistive provider setup did not complete: %v", err),
		}
	}
	if len(setupResult.ProvidersConfigured) == 0 {
		return setupStep{
			Name:    "assistive",
			Status:  "warning",
			Message: fmt.Sprintf("no assistive provider configured; fallback is `plexium convert` plus %s", initialPopulationMode(agentName)),
		}
	}
	return setupStep{
		Name:    "assistive",
		Status:  "pass",
		Message: fmt.Sprintf("configured assistive provider(s): %s (profile %s)", strings.Join(setupResult.ProvidersConfigured, ", "), prompts.ProfileFromConfig(loadConfigOrNil(repoRoot))),
	}
}

func enableMementoInConfig(repoRoot string) error {
	cfg, err := config.LoadFromDir(repoRoot)
	if err != nil {
		return err
	}
	cfg.Integrations.Memento = true
	cfg.Enforcement.MementoGate = true
	return config.SaveToDir(repoRoot, cfg)
}

func verifyAgent(repoRoot, agent string) (*verifyResult, error) {
	normalizedAgent := normalizeAgentName(agent)
	plan, err := buildPageIndexConnectPlan(normalizedAgent)
	if err != nil {
		return nil, err
	}

	result := &verifyResult{
		Agent:       normalizedAgent,
		RepoRoot:    repoRoot,
		ConnectPlan: plan,
	}

	doctor := lint.NewDoctor(repoRoot)
	doctorReport, err := doctor.Run()
	if err != nil {
		return nil, fmt.Errorf("run doctor: %w", err)
	}
	passed, failed, warnings, _ := doctorReport.Summary()
	doctorStatus := "pass"
	doctorMessage := fmt.Sprintf("doctor checks passed (%d pass)", passed)
	if failed > 0 {
		doctorStatus = "fail"
		doctorMessage = fmt.Sprintf("doctor reported %d failures and %d warnings", failed, warnings)
	} else if warnings > 0 {
		doctorStatus = "warning"
		doctorMessage = fmt.Sprintf("doctor reported %d warnings", warnings)
	}
	result.Checks = append(result.Checks, verifyCheck{
		Name:        "doctor",
		Status:      doctorStatus,
		Message:     doctorMessage,
		Remediation: "Run `plexium doctor` for detailed remediation steps.",
	})

	cfg, err := config.LoadFromDir(repoRoot)
	if err != nil {
		result.Checks = append(result.Checks, verifyCheck{
			Name:        "config",
			Status:      "fail",
			Message:     "Plexium config is missing or invalid",
			Remediation: "Run `plexium init` or `plexium setup " + normalizedAgent + "`.",
		})
		result.Ready = false
		return result, nil
	}

	indexCheck := verifyCompiledNavigation(repoRoot)
	result.Checks = append(result.Checks, indexCheck)

	instructionFile := instructionFileForAgent(repoRoot, normalizedAgent)
	instructionPath := filepath.Join(repoRoot, instructionFile)
	if _, err := os.Stat(instructionPath); err != nil {
		result.Checks = append(result.Checks, verifyCheck{
			Name:        "instruction-file",
			Status:      "fail",
			Message:     fmt.Sprintf("%s is missing", instructionFile),
			Remediation: fmt.Sprintf("Run `plexium plugin add %s` or `plexium setup %s`.", normalizedAgent, normalizedAgent),
		})
	} else {
		result.Checks = append(result.Checks, verifyCheck{
			Name:    "instruction-file",
			Status:  "pass",
			Message: fmt.Sprintf("%s is present", instructionFile),
		})
	}

	mcpReference := filepath.Join(repoRoot, ".plexium", "pageindex-mcp.json")
	if _, err := os.Stat(mcpReference); err != nil {
		result.Checks = append(result.Checks, verifyCheck{
			Name:        "pageindex-reference",
			Status:      "fail",
			Message:     ".plexium/pageindex-mcp.json is missing",
			Remediation: "Run `plexium setup " + normalizedAgent + "`.",
		})
	} else {
		result.Checks = append(result.Checks, verifyCheck{
			Name:    "pageindex-reference",
			Status:  "pass",
			Message: "PageIndex reference file is present",
		})
	}

	lintReport, err := lint.NewLinter(repoRoot, cfg).RunDeterministic()
	lintStatus := "pass"
	lintMessage := "deterministic lint passes cleanly"
	lintRemediation := ""
	if err != nil {
		lintStatus = "fail"
		lintMessage = fmt.Sprintf("deterministic lint could not run: %v", err)
		lintRemediation = "Run `plexium lint --deterministic` directly and resolve the reported error."
	} else if lintReport.Summary.Errors > 0 {
		lintStatus = "fail"
		lintMessage = fmt.Sprintf("deterministic lint reported %d errors and %d warnings", lintReport.Summary.Errors, lintReport.Summary.Warnings)
		lintRemediation = "Run `plexium lint --deterministic` and fix the reported issues."
	} else if lintReport.Summary.Warnings > 0 {
		lintStatus = "warning"
		lintMessage = fmt.Sprintf("deterministic lint reported %d warnings", lintReport.Summary.Warnings)
		lintRemediation = "Run `plexium lint --deterministic` and review the warnings."
	}
	result.Checks = append(result.Checks, verifyCheck{
		Name:        "lint",
		Status:      lintStatus,
		Message:     lintMessage,
		Remediation: lintRemediation,
	})

	configured, configLocation, err := detectMCPConfig(repoRoot, normalizedAgent)
	if err != nil {
		return nil, err
	}
	result.Configured = configured
	if configured {
		result.Checks = append(result.Checks, verifyCheck{
			Name:    "mcp",
			Status:  "pass",
			Message: fmt.Sprintf("%s MCP configuration is present (%s)", capitalizeFirst(normalizedAgent), configLocation),
		})
	} else if _, err := exec.LookPath(plan.Executable); err == nil {
		result.Checks = append(result.Checks, verifyCheck{
			Name:        "mcp",
			Status:      "warning",
			Message:     fmt.Sprintf("%s MCP configuration is not applied yet", capitalizeFirst(normalizedAgent)),
			Remediation: fmt.Sprintf("Run `%s` or rerun `plexium setup %s --write-config`.", plan.Command, normalizedAgent),
		})
	} else {
		result.Checks = append(result.Checks, verifyCheck{
			Name:        "mcp",
			Status:      "warning",
			Message:     fmt.Sprintf("%s CLI was not found, so MCP configuration could not be verified", capitalizeFirst(normalizedAgent)),
			Remediation: fmt.Sprintf("Install `%s` or run `plexium pageindex connect %s` on a machine where it is available.", plan.Executable, normalizedAgent),
		})
	}

	result.Ready = true
	for _, check := range result.Checks {
		if check.Status == "fail" {
			result.Ready = false
			break
		}
	}

	return result, nil
}

func (r *verifyResult) summary() (passCount, warnCount, failCount int) {
	for _, check := range r.Checks {
		switch check.Status {
		case "pass":
			passCount++
		case "warning":
			warnCount++
		case "fail":
			failCount++
		}
	}
	return
}

func buildSetupNextSteps(result *setupResult) []string {
	steps := []string{
		"Run `plexium convert` to replace the starter scaffold with grounded project pages.",
		"Run `plexium retrieve \"what does this project do?\"` to inspect what the wiki currently knows.",
		fmt.Sprintf("Run `plexium verify %s` or `plexium doctor` after major changes.", result.Agent),
	}

	assistiveConfigured := false
	for _, step := range result.Steps {
		if step.Name == "assistive" && step.Status == "pass" {
			assistiveConfigured = true
			break
		}
	}
	if assistiveConfigured {
		steps = append(steps, fmt.Sprintf("For the first wiki build, prefer %s and use `.plexium/prompts/assistive/initial-wiki-population.md` as the operating contract.", initialPopulationMode(result.Agent)))
	} else {
		steps = append(steps, fmt.Sprintf("If you want autonomous upkeep, run `plexium agent setup` to add Ollama or OpenRouter. Otherwise, use `plexium convert` first and then %s.", initialPopulationMode(result.Agent)))
	}
	return steps
}

func configuredProviderNames(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	var names []string
	for _, provider := range cfg.AssistiveAgent.Providers {
		if provider.Enabled {
			names = append(names, provider.Name)
		}
	}
	return names
}

func initialPopulationMode(agentName string) string {
	if normalizeAgentName(agentName) == "claude" {
		return "Claude agent teams (retriever, documenter, optional validator)"
	}
	return "Codex sub-agents (retriever, documenter, optional validator)"
}

func loadConfigOrNil(repoRoot string) *config.Config {
	cfg, err := config.LoadFromDir(repoRoot)
	if err != nil {
		return nil
	}
	return cfg
}

func isInteractiveReader(r io.Reader) bool {
	if r == nil {
		return false
	}
	file, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func promptYesNo(r io.Reader, w io.Writer, question string, defaultYes bool) bool {
	reader := bufio.NewReader(r)
	hint := "[Y/n]"
	if !defaultYes {
		hint = "[y/N]"
	}
	fmt.Fprintf(w, "%s %s: ", question, hint)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "" {
		return defaultYes
	}
	return answer == "y" || answer == "yes"
}

func verifyCompiledNavigation(repoRoot string) verifyCheck {
	indexPath := filepath.Join(repoRoot, ".wiki", "_index.md")
	sidebarPath := filepath.Join(repoRoot, ".wiki", "_Sidebar.md")

	indexData, indexErr := os.ReadFile(indexPath)
	sidebarData, sidebarErr := os.ReadFile(sidebarPath)
	if indexErr != nil || sidebarErr != nil {
		return verifyCheck{
			Name:        "compiled-navigation",
			Status:      "fail",
			Message:     "compiled navigation files are missing",
			Remediation: "Run `plexium compile`.",
		}
	}

	index := string(indexData)
	sidebar := string(sidebarData)
	if strings.TrimSpace(index) == "" || !strings.Contains(index, "# Wiki Index") {
		return verifyCheck{
			Name:        "compiled-navigation",
			Status:      "fail",
			Message:     "_index.md does not look compiled yet",
			Remediation: "Run `plexium compile`.",
		}
	}
	if strings.TrimSpace(sidebar) == "" || !strings.Contains(sidebar, "[[Home]]") {
		return verifyCheck{
			Name:        "compiled-navigation",
			Status:      "fail",
			Message:     "_Sidebar.md does not look compiled yet",
			Remediation: "Run `plexium compile`.",
		}
	}
	return verifyCheck{
		Name:    "compiled-navigation",
		Status:  "pass",
		Message: "compiled navigation files are present",
	}
}

func detectMCPConfig(repoRoot, agent string) (bool, string, error) {
	switch agent {
	case "claude":
		path := filepath.Join(repoRoot, ".mcp.json")
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return false, path, nil
			}
			return false, path, fmt.Errorf("read Claude MCP config: %w", err)
		}
		var cfg struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return false, path, fmt.Errorf("parse Claude MCP config: %w", err)
		}
		_, ok := cfg.MCPServers["plexium-wiki"]
		return ok, path, nil
	case "codex":
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return false, "", fmt.Errorf("resolve home directory: %w", err)
		}
		path := filepath.Join(homeDir, ".codex", "config.toml")
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return false, path, nil
			}
			return false, path, fmt.Errorf("read Codex config: %w", err)
		}
		var cfg struct {
			MCPServers map[string]struct {
				Command string   `toml:"command"`
				Args    []string `toml:"args"`
			} `toml:"mcp_servers"`
		}
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return false, path, fmt.Errorf("parse Codex config: %w", err)
		}
		_, ok := cfg.MCPServers["plexium-wiki"]
		return ok, path, nil
	default:
		return false, "", fmt.Errorf("unsupported agent %q", agent)
	}
}

func instructionFileForAgent(repoRoot, agent string) string {
	available, err := plugins.ListAdapters(repoRoot)
	if err == nil {
		for _, adapter := range available {
			if adapter.Name == agent && adapter.InstructionFile != "" {
				return adapter.InstructionFile
			}
		}
	}

	switch agent {
	case "claude":
		return "CLAUDE.md"
	case "codex":
		return "AGENTS.md"
	default:
		return strings.ToUpper(agent) + ".md"
	}
}

func sourceLabel(builtIn bool) string {
	if builtIn {
		return "built-in"
	}
	return "custom"
}

func relativeToRepo(repoRoot, path string) string {
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return path
	}
	return rel
}

func needsPlexiumInit(repoRoot string) bool {
	required := []string{
		filepath.Join(repoRoot, ".plexium", "config.yml"),
		filepath.Join(repoRoot, ".plexium", "manifest.json"),
		filepath.Join(repoRoot, ".wiki", "Home.md"),
	}
	for _, path := range required {
		if _, err := os.Stat(path); err != nil {
			return true
		}
	}
	return false
}

func currentGitRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = wd
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text != "" {
			return "", fmt.Errorf("current directory is not inside a git repository: %s", text)
		}
		return "", fmt.Errorf("current directory is not inside a git repository")
	}
	return strings.TrimSpace(string(output)), nil
}
