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

	go func() {
		_ = server.Serve(listener)
	}()

	resultCh := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + listener.Addr().String())
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

	<-started
	stopCallbackServer(server)
	require.NoError(t, <-resultCh)
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
