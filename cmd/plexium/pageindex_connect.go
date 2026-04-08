package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

type pageindexConnectPlan struct {
	Agent          string   `json:"agent"`
	ConfigLocation string   `json:"configLocation"`
	Command        string   `json:"command"`
	Executable     string   `json:"-"`
	Args           []string `json:"-"`
}

var pageidxConnectCmd = &cobra.Command{
	Use:   "connect <agent>",
	Short: "Show or apply native MCP setup for an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		plan, err := buildPageIndexConnectPlan(args[0])
		if err != nil {
			return err
		}

		outputJSON, _ := cmd.Flags().GetBool("output-json")
		writeConfig, _ := cmd.Flags().GetBool("write-config")

		if outputJSON {
			data, _ := json.MarshalIndent(plan, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		if !writeConfig {
			fmt.Printf("Connect %s to the Plexium PageIndex MCP server with:\n", strings.Title(plan.Agent))
			fmt.Printf("  %s\n", plan.Command)
			fmt.Printf("Config target: %s\n", plan.ConfigLocation)
			fmt.Println("Add --write-config to have Plexium run that native command for you.")
			return nil
		}

		repoRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		fmt.Printf("Running native %s MCP setup...\n", strings.Title(plan.Agent))
		if err := runPageIndexConnect(context.Background(), repoRoot, plan); err != nil {
			return err
		}

		fmt.Printf("Configured %s MCP access for Plexium.\n", strings.Title(plan.Agent))
		fmt.Printf("Config target: %s\n", plan.ConfigLocation)
		return nil
	},
}

func init() {
	pageidxCmd.AddCommand(pageidxConnectCmd)
	pageidxConnectCmd.Flags().Bool("write-config", false, "Run the native MCP configuration command")
}

func buildPageIndexConnectPlan(agent string) (*pageindexConnectPlan, error) {
	switch normalizeAgentName(agent) {
	case "claude":
		args := []string{"mcp", "add", "--scope", "project", "plexium-wiki", "--", "plexium", "pageindex", "serve"}
		return &pageindexConnectPlan{
			Agent:          "claude",
			ConfigLocation: ".mcp.json",
			Executable:     "claude",
			Args:           args,
			Command:        shellJoin("claude", args),
		}, nil
	case "codex":
		args := []string{"mcp", "add", "plexium-wiki", "--", "plexium", "pageindex", "serve"}
		return &pageindexConnectPlan{
			Agent:          "codex",
			ConfigLocation: "Codex config.toml (typically ~/.codex/config.toml)",
			Executable:     "codex",
			Args:           args,
			Command:        shellJoin("codex", args),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported agent %q (expected claude or codex)", agent)
	}
}

func normalizeAgentName(agent string) string {
	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "claude", "claude-code":
		return "claude"
	case "codex", "openai-codex":
		return "codex"
	default:
		return strings.ToLower(strings.TrimSpace(agent))
	}
}

func runPageIndexConnect(ctx context.Context, repoRoot string, plan *pageindexConnectPlan) error {
	cmd := exec.CommandContext(ctx, plan.Executable, plan.Args...)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text != "" {
			return fmt.Errorf("running %s: %w: %s", plan.Executable, err, text)
		}
		return fmt.Errorf("running %s: %w", plan.Executable, err)
	}
	return nil
}

func shellJoin(command string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, command)
	for _, arg := range args {
		if arg == "" || strings.ContainsAny(arg, " \t\n\"'") {
			parts = append(parts, fmt.Sprintf("%q", arg))
		} else {
			parts = append(parts, arg)
		}
	}
	return strings.Join(parts, " ")
}
