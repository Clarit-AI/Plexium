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
)

const (
	openRouterAuthURL  = "https://openrouter.ai/auth"
	openRouterTokenURL = "https://openrouter.ai/api/v1/auth/keys"
	openRouterKeyURL   = "https://openrouter.ai/api/v1/auth/key"
	callbackPort       = 3000
	callbackURL        = "http://localhost:3000"
	oauthAppName       = "Plexium"
	oauthTimeout       = 180 * time.Second
)

// SetupResult holds the outcome of the interactive setup.
type SetupResult struct {
	ProvidersConfigured []string
	OllamaEndpoint      string
	OllamaModel         string
	OpenRouterKeyPath   string
	ConfigUpdated       bool
}

// SetupOptions controls non-interactive setup behavior.
type SetupOptions struct {
	// APIKey bypasses OAuth and manual entry. If empty, the setup flow also
	// checks OPENROUTER_API_KEY before falling back to interactive prompts.
	APIKey string
}

// RunInteractiveSetup runs the full interactive provider setup flow.
func RunInteractiveSetup(repoRoot string, opts SetupOptions) (*SetupResult, error) {
	// Verify config exists
	configPath := filepath.Join(repoRoot, ".plexium", "config.yml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf(".plexium/config.yml not found — run 'plexium init' first")
	}

	result := &SetupResult{}
	reader := bufio.NewReader(os.Stdin)

	if opts.APIKey == "" {
		opts.APIKey = os.Getenv("OPENROUTER_API_KEY")
	}
	if opts.APIKey != "" {
		fmt.Println("Plexium Agent Setup")
		fmt.Println("===================")
		fmt.Println()
		fmt.Print("Validating provided API key... ")
		if _, err := validateKey(opts.APIKey); err != nil {
			fmt.Printf("FAILED (%v)\n", err)
			return nil, fmt.Errorf("key validation failed: %w", err)
		}
		fmt.Println("OK")

		if err := SaveCredentials(repoRoot, opts.APIKey); err != nil {
			return nil, fmt.Errorf("saving credentials: %w", err)
		}

		envPath := filepath.Join(repoRoot, ".plexium", ".env")
		envContent := fmt.Sprintf("# Source this file: source .plexium/.env\nexport OPENROUTER_API_KEY=%q\n", opts.APIKey)
		if err := os.WriteFile(envPath, []byte(envContent), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not write %s: %v\n", envPath, err)
		}

		result.OpenRouterKeyPath = filepath.Join(repoRoot, ".plexium", "credentials.json")
		result.ProvidersConfigured = append(result.ProvidersConfigured, "openrouter")
		if err := writeAssistiveAgentConfig(configPath, result); err != nil {
			fmt.Printf("Warning: could not update config: %v\n", err)
		} else {
			result.ConfigUpdated = true
		}
		fmt.Println("OpenRouter configured.")
		return result, nil
	}

	fmt.Println("Plexium Agent Setup")
	fmt.Println("===================")
	fmt.Println()

	// Ollama detection
	ollamaEndpoint := "http://localhost:11434"
	models, err := DetectOllama(ollamaEndpoint)
	if err == nil && len(models) > 0 {
		fmt.Printf("Checking for Ollama... found (%s)\n", ollamaEndpoint)
		fmt.Printf("Available models: %s\n\n", strings.Join(models, ", "))

		if promptYesNo(reader, "Configure Ollama?", true) {
			model := promptChoice(reader, "Select model", models[0], models)
			result.OllamaEndpoint = ollamaEndpoint
			result.OllamaModel = model
			result.ProvidersConfigured = append(result.ProvidersConfigured, "ollama")
		}
	} else {
		fmt.Println("Checking for Ollama... not found")
		fmt.Println("  Install from https://ollama.ai to use local models")
		fmt.Println()
	}

	// OpenRouter setup
	if promptYesNo(reader, "Configure OpenRouter?", true) {
		fmt.Println()
		fmt.Println("Choose setup method:")
		fmt.Println("  1. Browser OAuth (recommended)")
		fmt.Println("  2. Manual API key")
		fmt.Print("> ")
		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)

		var apiKey string
		switch choice {
		case "1", "":
			fmt.Println()
			apiKey, err = RunOAuthFlow(oauthAppName)
			if err != nil {
				fmt.Printf("OAuth failed: %v\n", err)
				fmt.Println("Falling back to manual entry...")
				apiKey, err = promptManualKey(reader)
				if err != nil {
					return nil, fmt.Errorf("manual key entry: %w", err)
				}
			}
		case "2":
			apiKey, err = promptManualKey(reader)
			if err != nil {
				return nil, fmt.Errorf("manual key entry: %w", err)
			}
		default:
			fmt.Println("Skipping OpenRouter.")
		}

		if apiKey != "" {
			// Validate key before persisting
			fmt.Print("Validating key... ")
			if _, vErr := validateKey(apiKey); vErr != nil {
				fmt.Printf("FAILED (%v)\n", vErr)
				fmt.Println("Key not saved. Check the key and try again.")
			} else {
				fmt.Println("OK")
				if err := SaveCredentials(repoRoot, apiKey); err != nil {
					return nil, fmt.Errorf("saving credentials: %w", err)
				}

				// Also write .env for convenience
				envPath := filepath.Join(repoRoot, ".plexium", ".env")
				envContent := fmt.Sprintf("# Source this file: source .plexium/.env\nexport OPENROUTER_API_KEY=%q\n", apiKey)
				if envErr := os.WriteFile(envPath, []byte(envContent), 0o600); envErr != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not write %s: %v\n", envPath, envErr)
				}

				result.OpenRouterKeyPath = filepath.Join(repoRoot, ".plexium", "credentials.json")
				result.ProvidersConfigured = append(result.ProvidersConfigured, "openrouter")
			}
		}
	}

	// Update config
	if len(result.ProvidersConfigured) > 0 {
		fmt.Println()
		fmt.Println("Updating .plexium/config.yml...")
		if err := writeAssistiveAgentConfig(configPath, result); err != nil {
			fmt.Printf("Warning: could not update config: %v\n", err)
			fmt.Println("You may need to update .plexium/config.yml manually.")
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
func RunOAuthFlow(appName string) (string, error) {
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
			"code_challenge_method": {"S256"},
			"app_name":              {appName},
		}
		authURL := openRouterAuthURL + "?" + params.Encode()

		// Start callback server before opening browser.
		codeCh := make(chan string, 1)
		errCh := make(chan error, 1)
		server := startCallbackServer(codeCh, errCh)

		if attempt == 1 {
			fmt.Println("Opening browser for OpenRouter authorization...")
			fmt.Printf("  %s\n", authURL)
			fmt.Println()
			openBrowser(authURL)
		} else {
			fmt.Printf("Attempt %d of %d — open this URL in your browser:\n", attempt, maxAttempts)
			fmt.Printf("  %s\n\n", authURL)
		}
		fmt.Printf("Waiting up to %ds for authorization...\n", int(oauthTimeout.Seconds()))

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

		fmt.Print("Exchanging code for API key... ")
		apiKey, err := exchangeCode(code, codeVerifier)
		if err != nil {
			if attempt < maxAttempts && strings.Contains(err.Error(), "400") {
				fmt.Printf("FAILED (%v)\nRetrying with fresh PKCE pair...\n", err)
				continue
			}
			fmt.Println("FAILED")
			return "", err
		}
		fmt.Println("OK")

		// Validate
		fmt.Print("Validating key... ")
		label, err := validateKey(apiKey)
		if err != nil {
			fmt.Println("SKIPPED (network error)")
		} else {
			fmt.Printf("OK (%s)\n", label)
		}

		return apiKey, nil
	}

	return "", fmt.Errorf("all OAuth attempts failed")
}

func startCallbackServer(codeCh chan<- string, errCh chan<- error) *http.Server {
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

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Give server a moment to bind
	time.Sleep(50 * time.Millisecond)
	return server
}

func exchangeCode(code, codeVerifier string) (string, error) {
	payload, _ := json.Marshal(map[string]string{
		"code":          code,
		"code_verifier": codeVerifier,
	})

	resp, err := httpClient.Post(openRouterTokenURL, "application/json", strings.NewReader(string(payload)))
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

func validateKey(apiKey string) (string, error) {
	req, _ := http.NewRequest("GET", openRouterKeyURL, nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := httpClient.Do(req)
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
func DetectOllama(endpoint string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
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

// --- Credential Storage ---

// SaveCredentials writes the API key to .plexium/credentials.json with mode 0600.
func SaveCredentials(repoRoot string, key string) error {
	credDir := filepath.Join(repoRoot, ".plexium")
	if err := os.MkdirAll(credDir, 0o755); err != nil {
		return fmt.Errorf("creating .plexium directory: %w", err)
	}

	credPath := filepath.Join(credDir, "credentials.json")

	// Read existing credentials if any
	existing := make(map[string]string)
	if data, err := os.ReadFile(credPath); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: existing credentials.json is malformed, will be overwritten\n")
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

	fmt.Printf("Key saved to %s\n", credPath)
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
`, result.OllamaEndpoint, result.OllamaModel))
		case "openrouter":
			providers.WriteString(`    - name: openrouter
      enabled: true
      type: openai-compatible
      endpoint: https://openrouter.ai/api
      model: meta-llama/llama-3.1-8b-instruct:free
      apiKeyEnv: OPENROUTER_API_KEY
`)
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

func promptYesNo(reader *bufio.Reader, question string, defaultYes bool) bool {
	hint := "[Y/n]"
	if !defaultYes {
		hint = "[y/N]"
	}
	fmt.Printf("%s %s: ", question, hint)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "" {
		return defaultYes
	}
	return answer == "y" || answer == "yes"
}

func promptChoice(reader *bufio.Reader, label, defaultVal string, options []string) string {
	fmt.Printf("%s [%s]: ", label, defaultVal)
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

func promptManualKey(reader *bufio.Reader) (string, error) {
	fmt.Print("Enter OpenRouter API key: ")
	key, _ := reader.ReadString('\n')
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("no key entered")
	}
	return key, nil
}
