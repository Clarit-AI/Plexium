package agent

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/internal/capabilityprofile"
	"gopkg.in/yaml.v3"
)

const (
	openRouterAuthURL  = "https://openrouter.ai/auth"
	openRouterTokenURL = "https://openrouter.ai/api/v1/auth/keys"
	openRouterKeyURL   = "https://openrouter.ai/api/v1/auth/key"
	pkceMethodS256     = "S256"
	callbackPort       = 3000
	callbackURL        = "http://localhost:3000"
	oauthAppName       = "Plexium"
	oauthTimeout       = 180 * time.Second
)

// SetupResult holds the outcome of the interactive setup.
type SetupResult struct {
	ProvidersConfigured         []string
	OllamaEndpoint              string
	OllamaModel                 string
	OpenRouterKeyPath           string
	OpenRouterModel             string
	OpenRouterCapabilityProfile string
	ConfigUpdated               bool
}

// SetupOptions controls non-interactive setup behavior.
type SetupOptions struct {
	// APIKey provides a non-interactive OpenRouter setup path. If empty, the
	// setup flow also checks OPENROUTER_API_KEY before falling back to
	// interactive prompts.
	APIKey string
	// HTTPClient allows tests to inject a request transport without mutating the
	// package-level shared client.
	HTTPClient *http.Client
	// Model optionally selects an OpenRouter model non-interactively.
	Model  string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// RunInteractiveSetup runs the full interactive provider setup flow.
func RunInteractiveSetup(repoRoot string, opts SetupOptions) (*SetupResult, error) {
	// Verify config exists
	configPath := filepath.Join(repoRoot, ".plexium", "config.yml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf(".plexium/config.yml not found — run 'plexium init' first")
	}

	result := &SetupResult{}
	stdin := opts.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	reader := bufio.NewReader(stdin)
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	client := clientOrDefault(opts.HTTPClient)
	interactive := isInteractiveInput(stdin)

	if opts.APIKey == "" {
		opts.APIKey = os.Getenv("OPENROUTER_API_KEY")
	}
	if opts.APIKey != "" {
		fmt.Fprintln(stdout, "Plexium Agent Setup")
		fmt.Fprintln(stdout, "===================")
		fmt.Fprintln(stdout)
		fmt.Fprint(stdout, "Validating provided API key... ")
		if _, err := validateKey(client, opts.APIKey); err != nil {
			fmt.Fprintf(stdout, "FAILED (%v)\n", err)
			return nil, fmt.Errorf("key validation failed: %w", err)
		}
		fmt.Fprintln(stdout, "OK")

		if err := SaveCredentials(repoRoot, opts.APIKey, stdout, stderr); err != nil {
			return nil, fmt.Errorf("saving credentials: %w", err)
		}

		envPath := filepath.Join(repoRoot, ".plexium", ".env")
		envContent := fmt.Sprintf("# Source this file: source .plexium/.env\nexport OPENROUTER_API_KEY=%q\n", opts.APIKey)
		if err := os.WriteFile(envPath, []byte(envContent), 0o600); err != nil {
			fmt.Fprintf(stderr, "Warning: could not write %s: %v\n", envPath, err)
		}

		model, profile, err := resolveOpenRouterModelChoice(reader, stdout, opts.Model, interactive)
		if err != nil {
			return nil, fmt.Errorf("select OpenRouter model: %w", err)
		}
		setOpenRouterSelection(result, model, profile)
		result.OpenRouterKeyPath = filepath.Join(repoRoot, ".plexium", "credentials.json")
		result.ProvidersConfigured = append(result.ProvidersConfigured, "openrouter")
		if err := writeAssistiveAgentConfig(configPath, result); err != nil {
			fmt.Fprintf(stdout, "Warning: could not update config: %v\n", err)
		} else {
			result.ConfigUpdated = true
		}
		fmt.Fprintln(stdout, "OpenRouter configured.")
		return result, nil
	}

	fmt.Fprintln(stdout, "Plexium Agent Setup")
	fmt.Fprintln(stdout, "===================")
	fmt.Fprintln(stdout)

	// Ollama detection
	ollamaEndpoint := "http://localhost:11434"
	models, err := DetectOllama(client, ollamaEndpoint)
	if err == nil && len(models) > 0 {
		fmt.Fprintf(stdout, "Checking for Ollama... found (%s)\n", ollamaEndpoint)
		fmt.Fprintf(stdout, "Available models: %s\n\n", strings.Join(models, ", "))

		if promptYesNo(reader, stdout, "Configure Ollama?", true) {
			model := promptChoice(reader, stdout, "Select model", models[0], models)
			result.OllamaEndpoint = ollamaEndpoint
			result.OllamaModel = model
			result.ProvidersConfigured = append(result.ProvidersConfigured, "ollama")
		}
	} else {
		fmt.Fprintln(stdout, "Checking for Ollama... not found")
		fmt.Fprintln(stdout, "  Install from https://ollama.ai to use local models")
		fmt.Fprintln(stdout)
	}

	// OpenRouter setup
	if promptYesNo(reader, stdout, "Configure OpenRouter?", true) {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Choose setup method:")
		fmt.Fprintln(stdout, "  1. Browser OAuth (recommended)")
		fmt.Fprintln(stdout, "  2. Manual API key")
		fmt.Fprint(stdout, "> ")
		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)

		var apiKey string
		switch choice {
		case "1", "":
			fmt.Fprintln(stdout)
			apiKey, err = RunOAuthFlow(client, oauthAppName, stdout, stderr)
			if err != nil {
				fmt.Fprintf(stdout, "OAuth failed: %v\n", err)
				fmt.Fprintln(stdout, "Falling back to manual entry...")
				apiKey, err = promptManualKey(reader, stdout)
				if err != nil {
					return nil, fmt.Errorf("manual key entry: %w", err)
				}
			}
		case "2":
			apiKey, err = promptManualKey(reader, stdout)
			if err != nil {
				return nil, fmt.Errorf("manual key entry: %w", err)
			}
		default:
			fmt.Fprintln(stdout, "Skipping OpenRouter.")
		}

		if apiKey != "" {
			// Validate key before persisting
			fmt.Fprint(stdout, "Validating key... ")
			if _, vErr := validateKey(client, apiKey); vErr != nil {
				fmt.Fprintf(stdout, "FAILED (%v)\n", vErr)
				fmt.Fprintln(stdout, "Key not saved. Check the key and try again.")
			} else {
				fmt.Fprintln(stdout, "OK")
				if err := SaveCredentials(repoRoot, apiKey, stdout, stderr); err != nil {
					return nil, fmt.Errorf("saving credentials: %w", err)
				}

				// Also write .env for convenience
				envPath := filepath.Join(repoRoot, ".plexium", ".env")
				envContent := fmt.Sprintf("# Source this file: source .plexium/.env\nexport OPENROUTER_API_KEY=%q\n", apiKey)
				if envErr := os.WriteFile(envPath, []byte(envContent), 0o600); envErr != nil {
					fmt.Fprintf(stderr, "Warning: could not write %s: %v\n", envPath, envErr)
				}

				model, profile, err := resolveOpenRouterModelChoice(reader, stdout, opts.Model, interactive)
				if err != nil {
					return nil, fmt.Errorf("select OpenRouter model: %w", err)
				}
				setOpenRouterSelection(result, model, profile)
				result.OpenRouterKeyPath = filepath.Join(repoRoot, ".plexium", "credentials.json")
				result.ProvidersConfigured = append(result.ProvidersConfigured, "openrouter")
			}
		}
	}

	// Update config
	if len(result.ProvidersConfigured) > 0 {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Updating .plexium/config.yml...")
		if err := writeAssistiveAgentConfig(configPath, result); err != nil {
			fmt.Fprintf(stdout, "Warning: could not update config: %v\n", err)
			fmt.Fprintln(stdout, "You may need to update .plexium/config.yml manually.")
		} else {
			result.ConfigUpdated = true
		}
	}

	return result, nil
}

// --- PKCE OAuth Flow ---

// generatePKCEPair returns (code_verifier, code_challenge) using S256 PKCE.
func generatePKCEPair() (string, string, error) {
	verifierBytes := make([]byte, 64)
	if _, err := rand.Read(verifierBytes); err != nil {
		return "", "", fmt.Errorf("generating random bytes: %w", err)
	}
	codeVerifier := base64.RawURLEncoding.EncodeToString(verifierBytes)

	digest := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(digest[:])

	return codeVerifier, codeChallenge, nil
}

// RunOAuthFlow runs the PKCE OAuth flow for OpenRouter.
// Returns the API key on success.
func RunOAuthFlow(client *http.Client, appName string, stdout, stderr io.Writer) (string, error) {
	client = clientOrDefault(client)
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	if !portAvailable(callbackPort) {
		return "", fmt.Errorf("port %d is in use (required by OpenRouter OAuth callback)", callbackPort)
	}

	maxAttempts := 2
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		codeVerifier, codeChallenge, err := generatePKCEPair()
		if err != nil {
			return "", err
		}

		params := url.Values{
			"callback_url":          {callbackURL},
			"code_challenge":        {codeChallenge},
			"code_challenge_method": {pkceMethodS256},
			"app_name":              {appName},
		}
		authURL := openRouterAuthURL + "?" + params.Encode()

		// Start callback server before opening browser.
		codeCh := make(chan string, 1)
		errCh := make(chan error, 1)
		server, err := startCallbackServer(codeCh, errCh)
		if err != nil {
			return "", fmt.Errorf("start callback server: %w", err)
		}

		if attempt == 1 {
			fmt.Fprintln(stdout, "Opening browser for OpenRouter authorization...")
			fmt.Fprintf(stdout, "  %s\n", authURL)
			fmt.Fprintln(stdout)
			openBrowser(authURL)
		} else {
			fmt.Fprintf(stdout, "Attempt %d of %d — open this URL in your browser:\n", attempt, maxAttempts)
			fmt.Fprintf(stdout, "  %s\n\n", authURL)
		}
		fmt.Fprintf(stdout, "Waiting up to %ds for authorization...\n", int(oauthTimeout.Seconds()))

		// Wait for callback or timeout
		var code string
		select {
		case code = <-codeCh:
			// got the code
		case err := <-errCh:
			server.Close()
			return "", fmt.Errorf("callback server: %w", err)
		case <-time.After(oauthTimeout):
			server.Close()
			return "", fmt.Errorf("timed out waiting for authorization")
		}
		server.Close()

		fmt.Fprint(stdout, "Exchanging code for API key... ")
		apiKey, err := exchangeCode(client, code, codeVerifier)
		if err != nil {
			if attempt < maxAttempts && strings.Contains(err.Error(), "400") {
				fmt.Fprintf(stdout, "FAILED (%v)\nRetrying with fresh PKCE pair...\n", err)
				continue
			}
			fmt.Fprintln(stdout, "FAILED")
			return "", err
		}
		fmt.Fprintln(stdout, "OK")

		// Validate
		fmt.Fprint(stdout, "Validating key... ")
		label, err := validateKey(client, apiKey)
		if err != nil {
			fmt.Fprintln(stderr, "SKIPPED (network error)")
		} else {
			fmt.Fprintf(stdout, "OK (%s)\n", label)
		}

		return apiKey, nil
	}

	return "", fmt.Errorf("all OAuth attempts failed")
}

func startCallbackServer(codeCh chan<- string, errCh chan<- error) (*http.Server, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code != "" {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(200)
			fmt.Fprint(w, "<html><body><h2>Plexium authorized!</h2><p>You can close this tab.</p></body></html>")
			codeCh <- code
		} else {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(400)
			fmt.Fprint(w, "<html><body><h2>Authorization failed.</h2><p>No code received.</p></body></html>")
		}
	})

	server := &http.Server{
		Addr:    fmt.Sprintf("localhost:%d", callbackPort),
		Handler: mux,
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return nil, err
	}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	return server, nil
}

func exchangeCode(client *http.Client, code, codeVerifier string) (string, error) {
	client = clientOrDefault(client)

	payload, _ := json.Marshal(map[string]string{
		"code":                  code,
		"code_verifier":         codeVerifier,
		"code_challenge_method": pkceMethodS256,
	})

	resp, err := client.Post(openRouterTokenURL, "application/json", strings.NewReader(string(payload)))
	if err != nil {
		return "", fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("token exchange HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Key    string `json:"key"`
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing token response: %w", err)
	}

	key := result.Key
	if key == "" {
		key = result.APIKey
	}
	if key == "" {
		return "", fmt.Errorf("no key in token response")
	}
	return key, nil
}

func validateKey(client *http.Client, apiKey string) (string, error) {
	client = clientOrDefault(client)

	req, _ := http.NewRequest("GET", openRouterKeyURL, nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("key validation failed (HTTP %d)", resp.StatusCode)
	}

	var result struct {
		Label string `json:"label"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("invalid validation response: %w", err)
	}

	label := result.Label
	if label == "" {
		label = "valid"
	}
	return label, nil
}

// --- Ollama Detection ---

// DetectOllama checks if Ollama is running and returns available model names.
func DetectOllama(client *http.Client, endpoint string) ([]string, error) {
	client = clientOrDefault(client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing ollama response: %w", err)
	}

	var names []string
	for _, m := range result.Models {
		names = append(names, m.Name)
	}
	return names, nil
}

func clientOrDefault(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return httpClient
}

// --- Credential Storage ---

// SaveCredentials writes the API key to .plexium/credentials.json with mode 0600.
func SaveCredentials(repoRoot string, key string, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	credDir := filepath.Join(repoRoot, ".plexium")
	if err := os.MkdirAll(credDir, 0o755); err != nil {
		return fmt.Errorf("creating .plexium directory: %w", err)
	}

	credPath := filepath.Join(credDir, "credentials.json")

	// Read existing credentials if any
	existing := make(map[string]string)
	if data, err := os.ReadFile(credPath); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			fmt.Fprintf(stderr, "Warning: existing credentials.json is malformed, will be overwritten\n")
		}
	}

	existing["openrouter_api_key"] = key

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling credentials: %w", err)
	}

	// Write atomically via temp file
	tmpPath := credPath + ".tmp"
	if err := os.WriteFile(tmpPath, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing temp credentials: %w", err)
	}
	if err := os.Rename(tmpPath, credPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming credentials: %w", err)
	}

	fmt.Fprintf(stdout, "Key saved to %s\n", credPath)
	return nil
}

// --- Config Writing ---

func writeAssistiveAgentConfig(configPath string, result *SetupResult) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	content := string(data)

	// Build the assistiveAgent YAML block
	var providers strings.Builder
	for _, p := range result.ProvidersConfigured {
		switch p {
		case "ollama":
			providers.WriteString(fmt.Sprintf(`    - name: local-ollama
      enabled: true
      type: ollama
      endpoint: %s
      model: %s
      capabilityProfile: constrained-local
`, result.OllamaEndpoint, result.OllamaModel))
		case "openrouter":
			providers.WriteString(fmt.Sprintf(`    - name: openrouter
      enabled: true
      type: openai-compatible
      endpoint: https://openrouter.ai/api
      model: %s
      apiKeyEnv: OPENROUTER_API_KEY
      capabilityProfile: %s
`, yamlQuoteString(result.OpenRouterModel), yamlQuoteString(result.OpenRouterCapabilityProfile)))
		}
	}

	agentBlock := fmt.Sprintf(`assistiveAgent:
  enabled: true
  providers:
%s  budget:
    dailyUSD: 1.00
`, providers.String())

	// Replace existing assistiveAgent block or append
	if strings.Contains(content, "assistiveAgent:") {
		// Find and replace the block — from "assistiveAgent:" to the next top-level key
		lines := strings.Split(content, "\n")
		var out []string
		inBlock := false
		for _, line := range lines {
			if strings.HasPrefix(line, "assistiveAgent:") {
				inBlock = true
				continue
			}
			if inBlock {
				// End of block: next line at column 0 that isn't empty or indented
				trimmed := strings.TrimRight(line, " \t")
				if trimmed != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
					inBlock = false
					out = append(out, line)
				}
				continue
			}
			out = append(out, line)
		}
		content = strings.Join(out, "\n")
		content = strings.TrimRight(content, "\n") + "\n\n" + agentBlock
	} else {
		content = strings.TrimRight(content, "\n") + "\n\n" + agentBlock
	}

	return os.WriteFile(configPath, []byte(content), 0o644)
}

// --- Helpers ---

func portAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func promptYesNo(reader *bufio.Reader, stdout io.Writer, question string, defaultYes bool) bool {
	hint := "[Y/n]"
	if !defaultYes {
		hint = "[y/N]"
	}
	fmt.Fprintf(stdout, "%s %s: ", question, hint)
	answer, err := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if err != nil && answer == "" {
		return false
	}
	if answer == "" {
		return defaultYes
	}
	return answer == "y" || answer == "yes"
}

func promptChoice(reader *bufio.Reader, stdout io.Writer, label, defaultVal string, options []string) string {
	fmt.Fprintf(stdout, "%s [%s]: ", label, defaultVal)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return defaultVal
	}
	// Validate against options
	for _, opt := range options {
		if answer == opt {
			return answer
		}
	}
	return defaultVal
}

func promptManualKey(reader *bufio.Reader, stdout io.Writer) (string, error) {
	fmt.Fprint(stdout, "Enter OpenRouter API key: ")
	key, _ := reader.ReadString('\n')
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("no key entered")
	}
	return key, nil
}

type openRouterModelOption struct {
	Label             string
	Model             string
	CapabilityProfile string
}

var curatedOpenRouterModels = []openRouterModelOption{
	{
		Label:             "google/gemma-4-31b-it — 262K context — $0.14/M input — $0.40/M output (recommended)",
		Model:             "google/gemma-4-31b-it",
		CapabilityProfile: capabilityprofile.Balanced,
	},
	{
		Label:             "qwen/qwen3.5-35b-a3b — 262K context — $0.1625/M input — $1.30/M output",
		Model:             "qwen/qwen3.5-35b-a3b",
		CapabilityProfile: capabilityprofile.Balanced,
	},
	{
		Label:             "openai/gpt-5.4-nano — 400K context — $0.20/M input — $1.25/M output",
		Model:             "openai/gpt-5.4-nano",
		CapabilityProfile: capabilityprofile.FrontierLargeContext,
	},
	{
		Label:             "nvidia/nemotron-3-super-120b-a12b — 262K context — $0.10/M input — $0.50/M output",
		Model:             "nvidia/nemotron-3-super-120b-a12b",
		CapabilityProfile: capabilityprofile.Balanced,
	},
}

func resolveOpenRouterModelChoice(reader *bufio.Reader, stdout io.Writer, requested string, interactive bool) (string, string, error) {
	if requested != "" {
		model := strings.TrimSpace(requested)
		if model != "" {
			return model, capabilityProfileForModel(model), nil
		}
	}

	defaultOption := curatedOpenRouterModels[0]
	if !interactive {
		return defaultOption.Model, defaultOption.CapabilityProfile, nil
	}

	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Choose an OpenRouter model:")
	for idx, option := range curatedOpenRouterModels {
		fmt.Fprintf(stdout, "  %d. %s\n", idx+1, option.Label)
	}
	fmt.Fprintf(stdout, "  %d. Custom model…\n", len(curatedOpenRouterModels)+1)
	fmt.Fprintf(stdout, "Select model [%d]: ", 1)

	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", "", err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" || answer == "1" {
		if err == io.EOF && answer == "" {
			return "", "", io.EOF
		}
		return defaultOption.Model, defaultOption.CapabilityProfile, nil
	}
	if answer == fmt.Sprintf("%d", len(curatedOpenRouterModels)+1) {
		for {
			fmt.Fprint(stdout, "Enter OpenRouter model id: ")
			custom, err := reader.ReadString('\n')
			if err != nil && err != io.EOF {
				return "", "", err
			}
			custom = strings.TrimSpace(custom)
			if custom != "" {
				return custom, capabilityProfileForModel(custom), nil
			}
			if err == io.EOF {
				return "", "", io.EOF
			}
			fmt.Fprintln(stdout, "Model id cannot be empty.")
		}
	}
	for idx, option := range curatedOpenRouterModels {
		if answer == fmt.Sprintf("%d", idx+1) {
			return option.Model, option.CapabilityProfile, nil
		}
	}
	return defaultOption.Model, defaultOption.CapabilityProfile, nil
}

func capabilityProfileForModel(model string) string {
	for _, option := range curatedOpenRouterModels {
		if model == option.Model {
			return option.CapabilityProfile
		}
	}
	return capabilityprofile.Balanced
}

func setOpenRouterSelection(result *SetupResult, model, profile string) {
	result.OpenRouterModel = model
	result.OpenRouterCapabilityProfile = profile
}

func yamlQuoteString(value string) string {
	var node yaml.Node
	node.Kind = yaml.ScalarNode
	node.Tag = "!!str"
	node.Value = value
	node.Style = yaml.DoubleQuotedStyle

	data, err := yaml.Marshal(&node)
	if err != nil {
		return `""`
	}
	return strings.TrimSpace(string(data))
}

func isInteractiveInput(r io.Reader) bool {
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
