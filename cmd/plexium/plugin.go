package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Clarit-AI/Plexium/internal/plugins"
	"github.com/spf13/cobra"
)

var pluginAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a new Plexium plugin adapter",
	Args:  cobra.ExactArgs(1),
	RunE:  runPluginAdd,
}

var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available Plexium plugins",
	RunE:  runPluginList,
}

func init() {
	pluginCmd.AddCommand(pluginAddCmd)
	pluginCmd.AddCommand(pluginListCmd)

	pluginAddCmd.Flags().String("path", "", "Install plugin from local path")
}

func runPluginAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	pluginPath, _ := cmd.Flags().GetString("path")

	repoRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	outputJSON, _ := cmd.Flags().GetBool("output-json")
	result, err := plugins.InstallAdapter(repoRoot, name, pluginPath)
	if err != nil {
		return err
	}

	if outputJSON {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal install result to JSON: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	sourceLabel := "custom"
	if result.BuiltIn {
		sourceLabel = "built-in"
	}
	fmt.Printf("Plugin %q installed successfully (%s)\n", name, sourceLabel)
	fmt.Printf("Generated instruction file: %s\n", result.InstructionFile)

	return nil
}

func runPluginList(cmd *cobra.Command, args []string) error {
	repoRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	outputJSON, _ := cmd.Flags().GetBool("output-json")
	available, err := plugins.ListAdapters(repoRoot)
	if err != nil {
		return err
	}

	if outputJSON {
		data, err := json.MarshalIndent(available, "", "  ")
		if err != nil {
			return fmt.Errorf("json marshal available plugins: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	if len(available) == 0 {
		fmt.Println("No plugins available")
		return nil
	}

	fmt.Println("Available plugins:")
	for _, p := range available {
		status := "built-in"
		if !p.BuiltIn {
			status = "custom"
		}
		if p.Installed {
			status += ", installed"
		}
		fmt.Printf("  - %s [%s]: %s\n", p.Name, status, p.Description)
	}

	return nil
}
