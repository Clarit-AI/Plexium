package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
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
