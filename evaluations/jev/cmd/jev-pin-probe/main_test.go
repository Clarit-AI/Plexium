package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/probe"
)

func TestDryRunDoesNotRequireCredentialAndPrintsPlan(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	var out bytes.Buffer
	err := runCLI([]string{
		"--dry-run",
		"--inventory", filepath.Join("..", "..", "pilot", "request-inventory.json"),
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, `"version": "jev-pin-probe-v1"`) || !strings.Contains(text, `"credentialEnv": "OPENROUTER_API_KEY"`) {
		t.Fatalf("unexpected dry plan: %s", text)
	}
}

func TestExecuteRequiresCredentialBeforeAnyRun(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	var out bytes.Buffer
	err := runCLI([]string{
		"--execute", "--state-dir", t.TempDir(),
		"--inventory", filepath.Join("..", "..", "pilot", "request-inventory.json"),
	}, &out)
	if err == nil || !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Fatalf("err=%v output=%s", err, out.String())
	}
}

func TestExecuteRedactsReflectedCredentialFromCLIAndFiles(t *testing.T) {
	secret := "fake-cli-key-reflected-579"
	t.Setenv(probe.CredentialEnv, secret)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"id":"d","model":%q,"provider":%q,"answers":{"verdict":{"type":"choice","choice":"supported"}},"usage":{"input_tokens":20,"output_tokens":2,"cost":0.000001,"diagnostic":%q}}`, probe.JevCandidatePin, probe.JevCandidateProvider, secret)
	}))
	defer server.Close()
	stateDir := t.TempDir()
	factory := func(inventory, state, key string) probe.RunConfig {
		cfg := probe.DefaultRunConfig(inventory, state, key)
		cfg.Endpoints = probe.Endpoints{Jev: server.URL + "/api/alpha/decisions", Nano: server.URL + "/api/v1/chat/completions"}
		cfg.HTTPClient = server.Client()
		cfg.Timeout = time.Second
		return cfg
	}
	var out bytes.Buffer
	err := runCLIWithConfig([]string{
		"--execute", "--state-dir", stateDir,
		"--inventory", filepath.Join("..", "..", "pilot", "request-inventory.json"),
	}, &out, factory)
	if err == nil {
		t.Fatal("expected schema halt")
	}
	if strings.Contains(out.String(), secret) || strings.Contains(err.Error(), secret) {
		t.Fatalf("credential leaked to CLI output: stdout=%s stderr=%v", out.String(), err)
	}
	err = filepath.WalkDir(stateDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(data, []byte(secret)) {
			return fmt.Errorf("credential leaked in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
