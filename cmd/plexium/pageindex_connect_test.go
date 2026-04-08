package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPageIndexConnectPlan_Claude(t *testing.T) {
	plan, err := buildPageIndexConnectPlan("claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.ConfigLocation != ".mcp.json" {
		t.Fatalf("unexpected config location: %s", plan.ConfigLocation)
	}
	if plan.Command == "" {
		t.Fatalf("expected command to be populated")
	}
}

func TestBuildPageIndexConnectPlan_Codex(t *testing.T) {
	plan, err := buildPageIndexConnectPlan("codex")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.Executable != "codex" {
		t.Fatalf("unexpected executable: %s", plan.Executable)
	}
	if len(plan.Args) == 0 || plan.Args[0] != "mcp" {
		t.Fatalf("unexpected args: %v", plan.Args)
	}
}

func TestBuildPageIndexConnectPlan_Invalid(t *testing.T) {
	if _, err := buildPageIndexConnectPlan("cursor"); err == nil {
		t.Fatalf("expected error for unsupported agent")
	}
}

func TestRunPageIndexConnect_UsesNativeCLI(t *testing.T) {
	repoRoot := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "claude.log")

	stub := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$PAGEINDEX_TEST_LOG\"\n"
	stubPath := filepath.Join(binDir, "claude")
	if err := os.WriteFile(stubPath, []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PAGEINDEX_TEST_LOG", logPath)

	plan, err := buildPageIndexConnectPlan("claude")
	if err != nil {
		t.Fatalf("unexpected plan error: %v", err)
	}

	if err := runPageIndexConnect(context.Background(), repoRoot, plan); err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	got := string(data)
	if got == "" || !containsAll(got, "mcp", "add", "--scope", "project", "plexium-wiki") {
		t.Fatalf("unexpected native CLI invocation: %q", got)
	}
}

func containsAll(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}
