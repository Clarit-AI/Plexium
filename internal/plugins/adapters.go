package plugins

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed builtins/*/*
var builtinAdapters embed.FS

// Manifest describes a plugin adapter.
type Manifest struct {
	Name                string   `json:"name"`
	Version             int      `json:"version"`
	Description         string   `json:"description"`
	InstructionFile     string   `json:"instructionFile"`
	InstructionFilePath string   `json:"instructionFilePath"`
	SchemaInjection     bool     `json:"schemaInjection"`
	Requires            []string `json:"requires"`
}

// AdapterInfo describes a built-in or installed adapter.
type AdapterInfo struct {
	Name            string `json:"name"`
	Installed       bool   `json:"installed"`
	BuiltIn         bool   `json:"builtIn"`
	Description     string `json:"description"`
	InstructionFile string `json:"instructionFile"`
}

// InstallResult describes a completed adapter installation.
type InstallResult struct {
	Name            string `json:"name"`
	Installed       bool   `json:"installed"`
	BuiltIn         bool   `json:"builtIn"`
	Description     string `json:"description"`
	InstructionFile string `json:"instructionFile"`
}

// ListAdapters returns both bundled and installed adapters.
func ListAdapters(repoRoot string) ([]AdapterInfo, error) {
	builtin, err := builtinManifests()
	if err != nil {
		return nil, err
	}

	installedDir := filepath.Join(repoRoot, ".plexium", "plugins")
	entries, err := os.ReadDir(installedDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading plugins directory: %w", err)
	}

	installed := map[string]Manifest{}
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			manifest, readErr := readManifestFromFile(filepath.Join(installedDir, entry.Name(), "manifest.json"))
			if readErr != nil {
				continue
			}
			installed[entry.Name()] = manifest
		}
	}

	seen := map[string]bool{}
	var adapters []AdapterInfo

	for name, manifest := range builtin {
		adapters = append(adapters, AdapterInfo{
			Name:            name,
			Installed:       hasInstalledAdapter(repoRoot, name),
			BuiltIn:         true,
			Description:     manifest.Description,
			InstructionFile: manifest.InstructionFile,
		})
		seen[name] = true
	}

	for name, manifest := range installed {
		if seen[name] {
			continue
		}
		adapters = append(adapters, AdapterInfo{
			Name:            name,
			Installed:       true,
			BuiltIn:         false,
			Description:     manifest.Description,
			InstructionFile: manifest.InstructionFile,
		})
	}

	sort.Slice(adapters, func(i, j int) bool {
		return adapters[i].Name < adapters[j].Name
	})

	return adapters, nil
}

// builtinGoAdapters maps adapter names to Go-native implementations that
// replace plugin.sh execution. Adapters not in this map fall through to
// shell script execution.
var builtinGoAdapters = map[string]func(string) error{
	"claude": RunClaudeAdapter,
}

// InstallAdapter materializes an adapter into .plexium/plugins and runs it.
func InstallAdapter(repoRoot, name, pluginPath string) (*InstallResult, error) {
	manifest, srcDir, builtIn, err := resolveInstallSource(name, pluginPath)
	if err != nil {
		return nil, err
	}
	if pluginPath != "" {
		if _, err := readBuiltinManifest(name); err == nil {
			return nil, fmt.Errorf("adapter %q is bundled and cannot be overridden via --path", name)
		}
	}

	destDir := filepath.Join(repoRoot, ".plexium", "plugins", name)
	parentDir := filepath.Dir(destDir)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating plugins directory: %w", err)
	}
	stagedDir, err := os.MkdirTemp(parentDir, name+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("creating staged plugin directory: %w", err)
	}
	cleanupStaged := true
	defer func() {
		if cleanupStaged {
			_ = os.RemoveAll(stagedDir)
		}
	}()

	if builtIn {
		if err := materializeBuiltin(name, stagedDir); err != nil {
			return nil, err
		}
	} else {
		if err := copyDir(srcDir, stagedDir); err != nil {
			return nil, err
		}
	}

	// Use Go-native adapter if available, otherwise fall through to plugin.sh
	if goAdapter, ok := builtinGoAdapters[name]; ok && builtIn {
		if err := goAdapter(repoRoot); err != nil {
			return nil, fmt.Errorf("running Go adapter for %s: %w", name, err)
		}
	} else {
		scriptPath := filepath.Join(stagedDir, "plugin.sh")
		if err := os.Chmod(scriptPath, 0o755); err != nil {
			return nil, fmt.Errorf("making plugin executable: %w", err)
		}

		if err := runAdapterScript(repoRoot, scriptPath); err != nil {
			return nil, err
		}
	}

	generatedFile := manifest.InstructionFile
	if manifest.InstructionFilePath != "" && manifest.InstructionFilePath != "." {
		generatedFile = filepath.Join(manifest.InstructionFilePath, manifest.InstructionFile)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, generatedFile)); err != nil {
		return nil, fmt.Errorf("verifying generated instruction file %q: %w", generatedFile, err)
	}
	if err := os.RemoveAll(destDir); err != nil {
		return nil, fmt.Errorf("cleaning plugin directory: %w", err)
	}
	if err := os.Rename(stagedDir, destDir); err != nil {
		return nil, fmt.Errorf("activating plugin directory: %w", err)
	}
	cleanupStaged = false

	return &InstallResult{
		Name:            name,
		Installed:       true,
		BuiltIn:         builtIn,
		Description:     manifest.Description,
		InstructionFile: manifest.InstructionFile,
	}, nil
}

// RunAdapter executes an installed adapter, or falls back to a bundled one.
func RunAdapter(repoRoot, name string) error {
	scriptPath := filepath.Join(repoRoot, ".plexium", "plugins", name, "plugin.sh")
	if _, err := os.Stat(scriptPath); err == nil {
		return runAdapterScript(repoRoot, scriptPath)
	}

	if _, err := readBuiltinManifest(name); err != nil {
		return fmt.Errorf("adapter %q not found", name)
	}

	tempDir, err := os.MkdirTemp("", "plexium-adapter-*")
	if err != nil {
		return fmt.Errorf("creating temp adapter directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	destDir := filepath.Join(tempDir, name)
	if err := materializeBuiltin(name, destDir); err != nil {
		return err
	}

	return runAdapterScript(repoRoot, filepath.Join(destDir, "plugin.sh"))
}

func resolveInstallSource(name, pluginPath string) (Manifest, string, bool, error) {
	if pluginPath != "" {
		manifest, err := readManifestFromFile(filepath.Join(pluginPath, "manifest.json"))
		if err != nil {
			return Manifest{}, "", false, err
		}
		if _, err := os.Stat(filepath.Join(pluginPath, "plugin.sh")); err != nil {
			if os.IsNotExist(err) {
				return Manifest{}, "", false, fmt.Errorf("plugin %q not found at %s", name, pluginPath)
			}
			return Manifest{}, "", false, fmt.Errorf("checking plugin: %w", err)
		}
		return manifest, pluginPath, false, nil
	}

	manifest, err := readBuiltinManifest(name)
	if err != nil {
		return Manifest{}, "", false, fmt.Errorf("plugin %q not found in bundled adapters", name)
	}
	return manifest, "", true, nil
}

func builtinManifests() (map[string]Manifest, error) {
	rootEntries, err := fs.ReadDir(builtinAdapters, "builtins")
	if err != nil {
		return nil, fmt.Errorf("reading built-in adapters: %w", err)
	}

	result := make(map[string]Manifest, len(rootEntries))
	for _, entry := range rootEntries {
		if !entry.IsDir() {
			continue
		}
		manifest, err := readBuiltinManifest(entry.Name())
		if err != nil {
			return nil, err
		}
		result[entry.Name()] = manifest
	}

	return result, nil
}

func readBuiltinManifest(name string) (Manifest, error) {
	data, err := fs.ReadFile(builtinAdapters, filepath.ToSlash(filepath.Join("builtins", name, "manifest.json")))
	if err != nil {
		return Manifest{}, fmt.Errorf("reading plugin manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parsing plugin manifest: %w", err)
	}
	if err := validateManifest(manifest, name); err != nil {
		return Manifest{}, fmt.Errorf("invalid plugin manifest: %w", err)
	}

	return manifest, nil
}

func readManifestFromFile(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("reading plugin manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parsing plugin manifest: %w", err)
	}
	if err := validateManifest(manifest, ""); err != nil {
		return Manifest{}, fmt.Errorf("invalid plugin manifest: %w", err)
	}

	return manifest, nil
}

func validateManifest(manifest Manifest, expectedName string) error {
	if strings.TrimSpace(manifest.Name) == "" {
		return fmt.Errorf("missing name")
	}
	if expectedName != "" && manifest.Name != expectedName {
		return fmt.Errorf("name %q does not match adapter %q", manifest.Name, expectedName)
	}
	if strings.TrimSpace(manifest.InstructionFile) == "" {
		return fmt.Errorf("missing instructionFile")
	}
	if filepath.IsAbs(manifest.InstructionFile) {
		return fmt.Errorf("instructionFile must be relative")
	}
	cleanInstructionFile := filepath.Clean(manifest.InstructionFile)
	if cleanInstructionFile == "." || cleanInstructionFile == ".." || strings.HasPrefix(cleanInstructionFile, ".."+string(filepath.Separator)) {
		return fmt.Errorf("instructionFile must stay within the repository")
	}
	if manifest.InstructionFilePath != "" {
		if filepath.IsAbs(manifest.InstructionFilePath) {
			return fmt.Errorf("instructionFilePath must be relative")
		}
		cleanPath := filepath.Clean(manifest.InstructionFilePath)
		if cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) {
			return fmt.Errorf("instructionFilePath must stay within the repository")
		}
	}
	return nil
}

func materializeBuiltin(name, destDir string) error {
	root := filepath.ToSlash(filepath.Join("builtins", name))
	return fs.WalkDir(builtinAdapters, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(destDir, 0o755)
		}

		target := filepath.Join(destDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := fs.ReadFile(builtinAdapters, path)
		if err != nil {
			return fmt.Errorf("reading built-in adapter asset %q: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(target, ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return fmt.Errorf("writing adapter asset %q: %w", target, err)
		}

		return nil
	})
}

func copyDir(srcDir, destDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		src, err := os.Open(path)
		if err != nil {
			return err
		}

		dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			_ = src.Close()
			return err
		}

		_, copyErr := io.Copy(dst, src)
		if cerr := dst.Close(); copyErr == nil && cerr != nil {
			copyErr = cerr
		}
		if cerr := src.Close(); copyErr == nil && cerr != nil {
			copyErr = cerr
		}
		return copyErr
	})
}

func runAdapterScript(repoRoot, scriptPath string) error {
	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PLEXIUM_DIR="+repoRoot)
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text != "" {
			return fmt.Errorf("running plugin: %w: %s", err, text)
		}
		return fmt.Errorf("running plugin: %w", err)
	}
	return nil
}

func hasInstalledAdapter(repoRoot, name string) bool {
	_, err := os.Stat(filepath.Join(repoRoot, ".plexium", "plugins", name, "plugin.sh"))
	return err == nil
}