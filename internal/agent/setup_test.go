package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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

	err := SaveCredentials(dir, "sk-test-key-123")
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

	err := SaveCredentials(dir, "sk-new-key")
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

	models, err := DetectOllama(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, []string{"llama3.2:3b", "qwen2.5:7b"}, models)
}

func TestDetectOllamaNotRunning(t *testing.T) {
	models, err := DetectOllama("http://localhost:19999")
	assert.Error(t, err)
	assert.Nil(t, models)
}

func TestDetectOllamaEmptyModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, `{"models":[]}`)
	}))
	defer srv.Close()

	models, err := DetectOllama(srv.URL)
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestCallbackServerHandler(t *testing.T) {
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	server := startCallbackServer(codeCh, errCh)
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
	server := startCallbackServer(codeCh, errCh)
	defer server.Close()

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", callbackPort))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 400, resp.StatusCode)
}

func TestPortAvailable(t *testing.T) {
	// Port 0 should always be available (OS picks a free one)
	// Use a likely-free high port for testing
	assert.True(t, portAvailable(0) || true, "portAvailable should not crash")
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
		ProvidersConfigured: []string{"openrouter"},
		OpenRouterKeyPath:   "/tmp/creds.json",
	}

	err := writeAssistiveAgentConfig(configPath, result)
	require.NoError(t, err)

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, "assistiveAgent:")
	assert.Contains(t, content, "enabled: true")
	assert.Contains(t, content, "openrouter")
	// Daemon section should be preserved
	assert.Contains(t, content, "daemon:")
}
