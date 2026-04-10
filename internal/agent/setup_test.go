package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratePKCEPair(t *testing.T) {
	verifier, challenge, err := generatePKCEPair()
	require.NoError(t, err)

	// Verifier should be base64url-encoded 64 random bytes
	assert.NotEmpty(t, verifier)
	assert.True(t, len(verifier) > 40, "verifier should be long enough for security")

	// Challenge should be different from verifier (it's SHA256 of verifier)
	assert.NotEqual(t, verifier, challenge)
	assert.NotEmpty(t, challenge)
}

func TestGeneratePKCEPairUniqueness(t *testing.T) {
	v1, c1, err := generatePKCEPair()
	require.NoError(t, err)

	v2, c2, err := generatePKCEPair()
	require.NoError(t, err)

	assert.NotEqual(t, v1, v2, "each call should produce unique verifier")
	assert.NotEqual(t, c1, c2, "each call should produce unique challenge")
}

func TestSaveCredentials(t *testing.T) {
	dir := t.TempDir()
	plexDir := filepath.Join(dir, ".plexium")
	require.NoError(t, os.MkdirAll(plexDir, 0o755))

	err := SaveCredentials(dir, "sk-test-key-123", io.Discard, io.Discard)
	require.NoError(t, err)

	// Verify file exists with correct permissions
	credPath := filepath.Join(plexDir, "credentials.json")
	info, err := os.Stat(credPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// Verify content
	data, err := os.ReadFile(credPath)
	require.NoError(t, err)

	var creds map[string]string
	require.NoError(t, json.Unmarshal(data, &creds))
	assert.Equal(t, "sk-test-key-123", creds["openrouter_api_key"])
}

func TestSaveCredentialsMergesExisting(t *testing.T) {
	dir := t.TempDir()
	plexDir := filepath.Join(dir, ".plexium")
	require.NoError(t, os.MkdirAll(plexDir, 0o755))

	// Write existing credentials
	existing := map[string]string{"other_key": "existing-value"}
	data, _ := json.Marshal(existing)
	require.NoError(t, os.WriteFile(filepath.Join(plexDir, "credentials.json"), data, 0o600))

	err := SaveCredentials(dir, "sk-new-key", io.Discard, io.Discard)
	require.NoError(t, err)

	// Verify both keys present
	result, err := os.ReadFile(filepath.Join(plexDir, "credentials.json"))
	require.NoError(t, err)

	var creds map[string]string
	require.NoError(t, json.Unmarshal(result, &creds))
	assert.Equal(t, "existing-value", creds["other_key"])
	assert.Equal(t, "sk-new-key", creds["openrouter_api_key"])
}

func TestDetectOllama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/tags", r.URL.Path)
		w.WriteHeader(200)
		fmt.Fprint(w, `{"models":[{"name":"llama3.2:3b"},{"name":"qwen2.5:7b"}]}`)
	}))
	defer srv.Close()

	models, err := DetectOllama(http.DefaultClient, srv.URL)
	require.NoError(t, err)
	assert.Equal(t, []string{"llama3.2:3b", "qwen2.5:7b"}, models)
}

func TestDetectOllamaNotRunning(t *testing.T) {
	models, err := DetectOllama(http.DefaultClient, "http://localhost:19999")
	assert.Error(t, err)
	assert.Nil(t, models)
}

func TestDetectOllamaEmptyModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, `{"models":[]}`)
	}))
	defer srv.Close()

	models, err := DetectOllama(http.DefaultClient, srv.URL)
	require.NoError(t, err)
	assert.Empty(t, models)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestExchangeCodeIncludesCodeChallengeMethod(t *testing.T) {
	var payload map[string]string
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, "POST", req.Method)
			assert.Equal(t, openRouterTokenURL, req.URL.String())

			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.NoError(t, req.Body.Close())
			require.NoError(t, json.Unmarshal(body, &payload))

			return &http.Response{
				StatusCode: 200,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewBufferString(`{"key":"sk-test-key"}`)),
			}, nil
		}),
	}

	key, err := exchangeCode(client, "test-auth-code", "test-verifier")
	require.NoError(t, err)
	assert.Equal(t, "sk-test-key", key)
	assert.Equal(t, "test-auth-code", payload["code"])
	assert.Equal(t, "test-verifier", payload["code_verifier"])
	assert.Equal(t, pkceMethodS256, payload["code_challenge_method"])
}

func TestCallbackServerHandler(t *testing.T) {
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	server, err := startCallbackServer(codeCh, errCh)
	require.NoError(t, err)
	defer server.Close()

	// Simulate OAuth callback with code
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/?code=test-auth-code", callbackPort))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "Authorization complete")
	assert.Contains(t, string(body), "Plexium OAuth")

	code := <-codeCh
	assert.Equal(t, "test-auth-code", code)
}

func TestCallbackServerNoCode(t *testing.T) {
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	server, err := startCallbackServer(codeCh, errCh)
	require.NoError(t, err)
	defer server.Close()

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", callbackPort))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 400, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "Authorization failed")
	assert.Contains(t, string(body), "No code received")
}

func TestStopCallbackServer_AllowsInFlightRequestToComplete(t *testing.T) {
	started := make(chan struct{})
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(200)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			close(started)
			time.Sleep(50 * time.Millisecond)
			_, _ = io.WriteString(w, "ok")
		}),
	}

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	defer listener.Close()

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(listener)
	}()

	resultCh := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://" + listener.Addr().String())
		if err != nil {
			resultCh <- err
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			resultCh <- err
			return
		}
		if resp.StatusCode != 200 {
			resultCh <- fmt.Errorf("unexpected status %d", resp.StatusCode)
			return
		}
		if strings.TrimSpace(string(body)) != "ok" {
			resultCh <- fmt.Errorf("unexpected body %q", string(body))
			return
		}
		resultCh <- nil
	}()

	select {
	case <-started:
		// Handler started successfully
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for handler to start")
	}

	stopCallbackServer(server)

	select {
	case err := <-resultCh:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for HTTP response")
	}
}

func TestPortAvailable(t *testing.T) {
	// Port 0 lets OS pick a free port, so binding should succeed
	assert.True(t, portAvailable(0), "port 0 should be available")

	// Occupy a port and verify it's detected as unavailable
	ln, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	assert.False(t, portAvailable(port), "occupied port should not be available")
}

func TestWriteAssistiveAgentConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")

	initial := `sources:
  include:
    - "**/*.go"
`
	require.NoError(t, os.WriteFile(configPath, []byte(initial), 0o644))

	result := &SetupResult{
		ProvidersConfigured: []string{"ollama"},
		OllamaEndpoint:      "http://localhost:11434",
		OllamaModel:         "llama3.2:3b",
	}

	err := writeAssistiveAgentConfig(configPath, result)
	require.NoError(t, err)

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, "assistiveAgent:")
	assert.Contains(t, content, "enabled: true")
	assert.Contains(t, content, "ollama")
	assert.Contains(t, content, "http://localhost:11434")
	assert.Contains(t, content, "llama3.2:3b")
	assert.Contains(t, content, "dailyUSD: 0")
	// Original content preserved
	assert.Contains(t, content, "sources:")
}

func TestWriteAssistiveAgentConfigReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")

	initial := `sources:
  include:
    - "**/*.go"

assistiveAgent:
  enabled: false
  providers: []

daemon:
  enabled: false
`
	require.NoError(t, os.WriteFile(configPath, []byte(initial), 0o644))

	result := &SetupResult{
		ProvidersConfigured:         []string{"openrouter"},
		OpenRouterKeyPath:           "/tmp/creds.json",
		OpenRouterModel:             "google/gemma-4-31b-it",
		OpenRouterCapabilityProfile: "balanced",
	}

	err := writeAssistiveAgentConfig(configPath, result)
	require.NoError(t, err)

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, "assistiveAgent:")
	assert.Contains(t, content, "enabled: true")
	assert.Contains(t, content, "openrouter")
	assert.Contains(t, content, "model: \"google/gemma-4-31b-it\"")
	assert.Contains(t, content, "capabilityProfile: \"balanced\"")
	// Daemon section should be preserved
	assert.Contains(t, content, "daemon:")
}

func TestWriteAssistiveAgentConfig_QuotesOpenRouterValues(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")

	initial := "sources:\n  include:\n    - \"**/*.go\"\n"
	require.NoError(t, os.WriteFile(configPath, []byte(initial), 0o644))

	result := &SetupResult{
		ProvidersConfigured:         []string{"openrouter"},
		OpenRouterModel:             "vendor/model:beta#1",
		OpenRouterCapabilityProfile: "frontier-large-context",
	}

	err := writeAssistiveAgentConfig(configPath, result)
	require.NoError(t, err)

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, "model: \"vendor/model:beta#1\"")
	assert.Contains(t, content, "capabilityProfile: \"frontier-large-context\"")
}

func TestRunInteractiveSetup_WithAPIKeyOption(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".plexium", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte("sources:\n  include:\n    - \"**/*.go\"\n"), 0o644))

	client, cleanup := stubOpenRouterValidation(t)
	defer cleanup()

	result, err := RunInteractiveSetup(dir, SetupOptions{
		APIKey:     "sk-or-v1-test",
		HTTPClient: client,
		Stdin:      bytes.NewBuffer(nil),
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"openrouter"}, result.ProvidersConfigured)
	assert.True(t, result.ConfigUpdated)
	assert.Equal(t, "google/gemma-4-31b-it", result.OpenRouterModel)
	assert.Equal(t, "balanced", result.OpenRouterCapabilityProfile)
	assert.False(t, result.BudgetConfigured)
	assert.Equal(t, 0.0, result.DailyBudgetUSD)
	assert.FileExists(t, filepath.Join(dir, ".plexium", "credentials.json"))
	assert.FileExists(t, filepath.Join(dir, ".plexium", ".env"))
}

func TestRunInteractiveSetup_UsesEnvVarFallback(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".plexium", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte("sources:\n  include:\n    - \"**/*.go\"\n"), 0o644))

	client, cleanup := stubOpenRouterValidation(t)
	defer cleanup()
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-env")

	result, err := RunInteractiveSetup(dir, SetupOptions{
		HTTPClient: client,
		Stdin:      bytes.NewBuffer(nil),
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"openrouter"}, result.ProvidersConfigured)
	assert.True(t, result.ConfigUpdated)
	assert.Equal(t, "google/gemma-4-31b-it", result.OpenRouterModel)
}

func TestLoadStoredOpenRouterKey_DoesNotFallbackToEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-env")
	assert.Equal(t, "", loadStoredOpenRouterKey(dir))
}

func TestRunInteractiveSetup_WithExplicitModel(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".plexium", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte("sources:\n  include:\n    - \"**/*.go\"\n"), 0o644))

	client, cleanup := stubOpenRouterValidation(t)
	defer cleanup()

	result, err := RunInteractiveSetup(dir, SetupOptions{
		APIKey:     "sk-or-v1-test",
		Model:      "openai/gpt-5.4-nano",
		HTTPClient: client,
		Stdin:      bytes.NewBuffer(nil),
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	})
	require.NoError(t, err)

	assert.Equal(t, "openai/gpt-5.4-nano", result.OpenRouterModel)
	assert.Equal(t, "frontier-large-context", result.OpenRouterCapabilityProfile)
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "model: \"openai/gpt-5.4-nano\"")
	assert.Contains(t, string(data), "capabilityProfile: \"frontier-large-context\"")
	assert.Contains(t, string(data), "dailyUSD: 0")
}

func TestRunInteractiveSetup_WithExplicitBudget(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".plexium", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte("sources:\n  include:\n    - \"**/*.go\"\n"), 0o644))

	client, cleanup := stubOpenRouterValidation(t)
	defer cleanup()
	budget := 2.5

	result, err := RunInteractiveSetup(dir, SetupOptions{
		APIKey:         "sk-or-v1-test",
		HTTPClient:     client,
		DailyBudgetUSD: &budget,
		Stdin:          bytes.NewBuffer(nil),
		Stdout:         io.Discard,
		Stderr:         io.Discard,
	})
	require.NoError(t, err)

	assert.True(t, result.BudgetConfigured)
	assert.Equal(t, 2.5, result.DailyBudgetUSD)
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "dailyUSD: 2.5")
}

func TestRunInteractiveSetup_WithAPIKeyOption_PreservesExistingProvidersAndBudget(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".plexium", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	content := `sources:
  include:
    - "**/*.go"

assistiveAgent:
  enabled: true
  providers:
    - name: local-ollama
      enabled: true
      type: ollama
      endpoint: http://localhost:11434
      model: llama3.2
    - name: openrouter
      enabled: true
      type: openai-compatible
      endpoint: https://openrouter.ai/api
      model: "nvidia/nemotron-3-super-120b-a12b"
      apiKeyEnv: OPENROUTER_API_KEY
      capabilityProfile: "balanced"
  budget:
    dailyUSD: 1.25
`
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))
	require.NoError(t, SaveCredentials(dir, "sk-or-v1-existing", io.Discard, io.Discard))

	client, cleanup := stubOpenRouterValidation(t)
	defer cleanup()

	result, err := RunInteractiveSetup(dir, SetupOptions{
		APIKey:     "sk-or-v1-test",
		HTTPClient: client,
		Stdin:      bytes.NewBuffer(nil),
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	})
	require.NoError(t, err)
	assert.True(t, result.BudgetConfigured)
	assert.Equal(t, 1.25, result.DailyBudgetUSD)
	assert.Contains(t, result.ProvidersConfigured, "openrouter")
	assert.Contains(t, result.ProvidersConfigured, "ollama")

	contents, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(contents), "name: local-ollama")
	assert.Contains(t, string(contents), "dailyUSD: 1.25")
}

func TestResolveOpenRouterModelChoice_InteractiveSelection(t *testing.T) {
	reader := bufio.NewReader(bytes.NewBufferString("2\n"))
	model, profile, err := resolveOpenRouterModelChoice(reader, io.Discard, "", true)
	require.NoError(t, err)
	assert.Equal(t, "qwen/qwen3.5-35b-a3b", model)
	assert.Equal(t, "balanced", profile)
}

func TestResolveOpenRouterModelChoice_Custom(t *testing.T) {
	reader := bufio.NewReader(bytes.NewBufferString("5\nanthropic/claude-sonnet-4\n"))
	model, profile, err := resolveOpenRouterModelChoice(reader, io.Discard, "", true)
	require.NoError(t, err)
	assert.Equal(t, "anthropic/claude-sonnet-4", model)
	assert.Equal(t, "balanced", profile)
}

func TestResolveOpenRouterModelChoice_CustomEOF(t *testing.T) {
	reader := bufio.NewReader(bytes.NewBufferString("5\n"))
	_, _, err := resolveOpenRouterModelChoice(reader, io.Discard, "", true)
	require.ErrorIs(t, err, io.EOF)
}

func TestPromptYesNo_EOFWithoutAnswerReturnsFalse(t *testing.T) {
	reader := bufio.NewReader(bytes.NewBuffer(nil))
	if promptYesNo(reader, io.Discard, "Configure OpenRouter?", true) {
		t.Fatalf("expected EOF without answer to return false")
	}
}

func TestPromptBudgetChoice_BlankMeansUnlimited(t *testing.T) {
	reader := bufio.NewReader(bytes.NewBufferString("\n"))
	result := &SetupResult{}
	require.NoError(t, promptBudgetChoice(reader, io.Discard, result))
	assert.False(t, result.BudgetConfigured)
	assert.Equal(t, 0.0, result.DailyBudgetUSD)
}

func TestPromptBudgetChoice_ParsesValue(t *testing.T) {
	reader := bufio.NewReader(bytes.NewBufferString("3.75\n"))
	result := &SetupResult{}
	require.NoError(t, promptBudgetChoice(reader, io.Discard, result))
	assert.True(t, result.BudgetConfigured)
	assert.Equal(t, 3.75, result.DailyBudgetUSD)
}

func TestPromptBudgetChoice_ZeroMeansUnlimited(t *testing.T) {
	reader := bufio.NewReader(bytes.NewBufferString("0\n"))
	result := &SetupResult{}
	require.NoError(t, promptBudgetChoice(reader, io.Discard, result))
	assert.False(t, result.BudgetConfigured)
	assert.Equal(t, 0.0, result.DailyBudgetUSD)
}

func TestPromptBudgetChoice_NegativeMeansUnlimited(t *testing.T) {
	reader := bufio.NewReader(bytes.NewBufferString("-2\n"))
	result := &SetupResult{}
	require.NoError(t, promptBudgetChoice(reader, io.Discard, result))
	assert.False(t, result.BudgetConfigured)
	assert.Equal(t, 0.0, result.DailyBudgetUSD)
}

func TestPromptBudgetChoice_InvalidValueFails(t *testing.T) {
	reader := bufio.NewReader(bytes.NewBufferString("abc"))
	result := &SetupResult{}
	require.Error(t, promptBudgetChoice(reader, io.Discard, result))
}

func TestPromptBudgetChoice_InvalidThenValidRetries(t *testing.T) {
	reader := bufio.NewReader(bytes.NewBufferString("abc\n1.5\n"))
	var stdout bytes.Buffer
	result := &SetupResult{}
	require.NoError(t, promptBudgetChoice(reader, &stdout, result))
	assert.True(t, result.BudgetConfigured)
	assert.Equal(t, 1.5, result.DailyBudgetUSD)
	assert.Contains(t, stdout.String(), "Enter a number like 2.5")
}

func TestApplyBudgetSelection_NonPositiveMeansUnlimited(t *testing.T) {
	result := &SetupResult{BudgetConfigured: true, DailyBudgetUSD: 1.25}
	zero := 0.0
	applyBudgetSelection(result, &zero)
	assert.False(t, result.BudgetConfigured)
	assert.Equal(t, 0.0, result.DailyBudgetUSD)

	negative := -4.0
	applyBudgetSelection(result, &negative)
	assert.False(t, result.BudgetConfigured)
	assert.Equal(t, 0.0, result.DailyBudgetUSD)
}

func TestRenderCallbackPage_EscapesHTML(t *testing.T) {
	page := renderCallbackPage("<title>", "<b>heading</b>", "<script>alert(1)</script>")
	assert.Contains(t, page, "&lt;title&gt;")
	assert.Contains(t, page, "&lt;b&gt;heading&lt;/b&gt;")
	assert.Contains(t, page, "&lt;script&gt;alert(1)&lt;/script&gt;")
	assert.NotContains(t, page, "<script>alert(1)</script>")
}

func TestRunInteractiveSetup_DetectsExistingOpenRouterAndKeepsCurrentSetup(t *testing.T) {
	dir := t.TempDir()
	writeExistingOpenRouterSetup(t, dir, "nvidia/nemotron-3-super-120b-a12b", "balanced", 1.25)
	require.NoError(t, SaveCredentials(dir, "sk-or-v1-existing", io.Discard, io.Discard))

	client, cleanup := stubOpenRouterValidation(t)
	defer cleanup()

	var stdout bytes.Buffer
	result, err := RunInteractiveSetup(dir, SetupOptions{
		HTTPClient: client,
		Stdin:      bytes.NewBufferString("\n"),
		Stdout:     &stdout,
		Stderr:     io.Discard,
	})
	require.NoError(t, err)

	assert.Contains(t, stdout.String(), "OpenRouter is already configured for this repo.")
	assert.Equal(t, []string{"openrouter"}, result.ProvidersConfigured)
	assert.Equal(t, "nvidia/nemotron-3-super-120b-a12b", result.OpenRouterModel)
	assert.True(t, result.BudgetConfigured)
	assert.Equal(t, 1.25, result.DailyBudgetUSD)

	data, err := os.ReadFile(filepath.Join(dir, ".plexium", "config.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "model: \"nvidia/nemotron-3-super-120b-a12b\"")
	assert.Contains(t, string(data), "dailyUSD: 1.25")
}

func TestRunInteractiveSetup_ReconfiguresExistingOpenRouterWithoutReauth(t *testing.T) {
	dir := t.TempDir()
	writeExistingOpenRouterSetup(t, dir, "nvidia/nemotron-3-super-120b-a12b", "balanced", 1.25)
	require.NoError(t, SaveCredentials(dir, "sk-or-v1-existing", io.Discard, io.Discard))

	client, cleanup := stubOpenRouterValidation(t)
	defer cleanup()

	result, err := RunInteractiveSetup(dir, SetupOptions{
		HTTPClient: client,
		Stdin:      bytes.NewBufferString("2\n3\n2.5\n"),
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"openrouter"}, result.ProvidersConfigured)
	assert.Equal(t, "openai/gpt-5.4-nano", result.OpenRouterModel)
	assert.Equal(t, "frontier-large-context", result.OpenRouterCapabilityProfile)
	assert.True(t, result.BudgetConfigured)
	assert.Equal(t, 2.5, result.DailyBudgetUSD)

	data, err := os.ReadFile(filepath.Join(dir, ".plexium", "config.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "model: \"openai/gpt-5.4-nano\"")
	assert.Contains(t, string(data), "capabilityProfile: \"frontier-large-context\"")
	assert.Contains(t, string(data), "dailyUSD: 2.5")
}

func TestRunInteractiveSetup_RemovesExistingOpenRouter(t *testing.T) {
	dir := t.TempDir()
	writeExistingOpenRouterSetup(t, dir, "nvidia/nemotron-3-super-120b-a12b", "balanced", 1.25)
	require.NoError(t, SaveCredentials(dir, "sk-or-v1-existing", io.Discard, io.Discard))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".plexium", ".env"), []byte("export OPENROUTER_API_KEY=\"sk-or-v1-existing\"\n"), 0o600))

	client, cleanup := stubOpenRouterValidation(t)
	defer cleanup()

	result, err := RunInteractiveSetup(dir, SetupOptions{
		HTTPClient: client,
		Stdin:      bytes.NewBufferString("4\n"),
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	})
	require.NoError(t, err)

	assert.Empty(t, result.ProvidersConfigured)
	assert.False(t, result.BudgetConfigured)
	assert.Equal(t, 0.0, result.DailyBudgetUSD)

	data, err := os.ReadFile(filepath.Join(dir, ".plexium", "config.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "enabled: false")
	assert.Contains(t, string(data), "providers: []")

	credData, err := os.ReadFile(filepath.Join(dir, ".plexium", "credentials.json"))
	if os.IsNotExist(err) {
		envData, envErr := os.ReadFile(filepath.Join(dir, ".plexium", ".env"))
		if os.IsNotExist(envErr) {
			return
		}
		require.NoError(t, envErr)
		assert.NotContains(t, string(envData), "OPENROUTER_API_KEY")
		return
	}
	require.NoError(t, err)
	assert.NotContains(t, string(credData), "openrouter_api_key")

	envData, envErr := os.ReadFile(filepath.Join(dir, ".plexium", ".env"))
	if os.IsNotExist(envErr) {
		return
	}
	require.NoError(t, envErr)
	assert.NotContains(t, string(envData), "OPENROUTER_API_KEY")
}

func writeExistingOpenRouterSetup(t *testing.T, dir, model, profile string, budget float64) {
	t.Helper()

	configPath := filepath.Join(dir, ".plexium", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	content := fmt.Sprintf(`sources:
  include:
    - "**/*.go"

assistiveAgent:
  enabled: true
  providers:
    - name: openrouter
      enabled: true
      type: openai-compatible
      endpoint: https://openrouter.ai/api
      model: %q
      apiKeyEnv: OPENROUTER_API_KEY
      capabilityProfile: %q
  budget:
    dailyUSD: %v
`, model, profile, budget)
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))
}

func stubOpenRouterValidation(t *testing.T) (*http.Client, func()) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/key" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got == "" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"label":"valid"}`)
	}))

	client := &http.Client{
		Transport: rewriteTransport{
			targetHost: server.Listener.Addr().String(),
			base:       http.DefaultTransport,
		},
	}

	return client, server.Close
}

type rewriteTransport struct {
	targetHost string
	base       http.RoundTripper
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = t.targetHost
	return t.base.RoundTrip(clone)
}