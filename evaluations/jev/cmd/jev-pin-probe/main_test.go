package main

import (
	"bytes"
	"encoding/json"
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
		fmt.Fprintf(w, `{"id":"d","model":%q,"provider":%q,"answers":{"verdict":{"type":"choice","choice":"supported"}},"usage":{"input_tokens":20,"output_tokens":2,"cost":0.000001,"diagnostic":"\u0066ake-cli-key-reflected-579"}}`, probe.JevCandidatePin, probe.JevCandidateProvider)
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
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("credential leaked to CLI output: stdout=%s stderr=%v", out.String(), err)
	}
	assertDecodedNoCredential(t, out.Bytes(), secret)
	err = filepath.WalkDir(stateDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.HasSuffix(path, ".json") {
			if checkErr := decodedHasCredential(data, secret); checkErr != nil {
				return fmt.Errorf("credential leaked in %s: %w", path, checkErr)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestExecuteInvalidStringCostCannotLeakThroughCLIOrFiles(t *testing.T) {
	secret := "test-key-never-printed"
	t.Setenv(probe.CredentialEnv, secret)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"id":"d","model":%q,"provider":%q,"answers":{"verdict":{"type":"choice","choice":"supported"}},"usage":{"input_tokens":20,"output_tokens":2,"cost":"test-key-never-printed"}}`, probe.JevCandidatePin, probe.JevCandidateProvider)
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
		t.Fatal("expected invalid-billing halt")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("credential leaked in CLI error: %v", err)
	}
	assertDecodedNoCredential(t, out.Bytes(), secret)
	var report probe.Report
	if decodeErr := json.Unmarshal(out.Bytes(), &report); decodeErr != nil {
		t.Fatalf("decode CLI report: %v", decodeErr)
	}
	if len(report.Attempts) != 1 || report.Attempts[0].Billing.Cost.Valid || report.Attempts[0].Billing.Cost.Raw != `"[REDACTED_CREDENTIAL]"` {
		t.Fatalf("CLI report lost invalid/untrusted billing distinction: %+v", report.Attempts)
	}
	err = filepath.WalkDir(stateDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".json") {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if checkErr := decodedHasCredential(data, secret); checkErr != nil {
			return fmt.Errorf("credential leaked in %s: %w", path, checkErr)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertDecodedNoCredential(t *testing.T, data []byte, credential string) {
	t.Helper()
	if err := decodedHasCredential(data, credential); err != nil {
		t.Fatal(err)
	}
}

func decodedHasCredential(data []byte, credential string) error {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}
	var walk func(any) error
	walk = func(current any) error {
		switch typed := current.(type) {
		case string:
			if strings.Contains(typed, credential) {
				return fmt.Errorf("decoded credential remains in %q", typed)
			}
		case []any:
			for _, item := range typed {
				if err := walk(item); err != nil {
					return err
				}
			}
		case map[string]any:
			for key, item := range typed {
				if strings.Contains(key, credential) {
					return fmt.Errorf("decoded credential remains in key %q", key)
				}
				if err := walk(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(value)
}
