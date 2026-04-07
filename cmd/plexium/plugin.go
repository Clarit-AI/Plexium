package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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

	var srcDir string

	if pluginPath != "" {
		// Install from local path
		srcDir = pluginPath
	} else {
		// Use official plugin from .plexium/plugins/<name>
		srcDir = filepath.Join(repoRoot, ".plexium", "plugins", name)
	}

	// Validate source plugin exists
	scriptPath := filepath.Join(srcDir, "plugin.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("plugin %q not found at %s", name, srcDir)
		}
		return fmt.Errorf("checking plugin: %w", err)
	}

	// Validate manifest
	manifestPath := filepath.Join(srcDir, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("reading plugin manifest: %w", err)
	}

	var manifest struct {
		Name            string   `json:"name"`
		Version         int      `json:"version"`
		Description    string   `json:"description"`
		InstructionFile string   `json:"instructionFile"`
		Requires        []string `json:"requires"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("parsing plugin manifest: %w", err)
	}

	// Copy plugin to .plexium/plugins/<name>/
	destDir := filepath.Join(repoRoot, ".plexium", "plugins", name)
	if err := os.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return fmt.Errorf("creating plugins directory: %w", err)
	}

	// Use cp to copy the plugin
	cpCmd := exec.Command("cp", "-r", srcDir, destDir)
	if err := cpCmd.Run(); err != nil {
		return fmt.Errorf("copying plugin: %w", err)
	}

	// Make plugin.sh executable
	scriptDest := filepath.Join(destDir, "plugin.sh")
	if err := os.Chmod(scriptDest, 0755); err != nil {
		return fmt.Errorf("making plugin executable: %w", err)
	}

	// Run plugin to generate instruction file
	runCmd := exec.Command("bash", scriptDest)
	runCmd.Dir = repoRoot
	runCmd.Env = append(os.Environ(), "PLEXIUM_DIR="+repoRoot)
	if err := runCmd.Run(); err != nil {
		return fmt.Errorf("running plugin: %w", err)
	}

	if outputJSON {
		result := map[string]interface{}{
			"name":            name,
			"installed":        true,
			"instructionFile":   manifest.InstructionFile,
			"description":      manifest.Description,
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Plugin %q installed successfully\n", name)
	fmt.Printf("Generated instruction file: %s\n", manifest.InstructionFile)

	return nil
}

func runPluginList(cmd *cobra.Command, args []string) error {
	repoRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	outputJSON, _ := cmd.Flags().GetBool("output-json")

	pluginsDir := filepath.Join(repoRoot, ".plexium", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			if outputJSON {
				fmt.Println("[]")
			} else {
				fmt.Println("No plugins installed")
			}
			return nil
		}
		return fmt.Errorf("reading plugins directory: %w", err)
	}

	type pluginInfo struct {
		Name        string `json:"name"`
		Installed   bool   `json:"installed"`
		Description string `json:"description"`
	}

	var plugins []pluginInfo

	for _, entry := range entries {
		if entry.IsDir() {
			pluginDir := filepath.Join(pluginsDir, entry.Name())
			manifestPath := filepath.Join(pluginDir, "manifest.json")

			var desc string
			if data, err := os.ReadFile(manifestPath); err == nil {
				var manifest struct {
					Description string `json:"description"`
				}
				if json.Unmarshal(data, &manifest) == nil {
					desc = manifest.Description
				}
			}

			plugins = append(plugins, pluginInfo{
				Name:        entry.Name(),
				Installed:   true,
				Description: desc,
			})
		}
	}

	if outputJSON {
		data, _ := json.MarshalIndent(plugins, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	if len(plugins) == 0 {
		fmt.Println("No plugins installed")
		return nil
	}

	fmt.Println("Installed plugins:")
	for _, p := range plugins {
		fmt.Printf("  - %s: %s\n", p.Name, p.Description)
	}

	return nil
}
