package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDryRunCLIIntegration exercises the real jev-dryrun binary via
// os/exec against the actual evaluations/jev/fixtures.jsonl. This is the
// C1 follow-up: at least one true CLI integration test against the real
// corpus, not just structural tests of the formulas.
//
// Cases verified:
//   - free output (-rate-out 0) accepted and reflected in plan
//   - 332-fixture corpus count
//   - default repetitions=3 attempt multiplication
//   - default cap=$0 produces nonzero exit (plan exceeds default cap)
//   - missing rate (-rate-in omitted) produces nonzero exit
func TestDryRunCLIIntegration(t *testing.T) {
	// Locate the repo root: tests run from evaluations/jev/cmd/jev-dryrun/.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	fixturesPath := filepath.Join(repoRoot, "fixtures.jsonl")
	manifestPath := filepath.Join(repoRoot, "fixtures.manifest.json")

	if _, err := os.Stat(fixturesPath); err != nil {
		t.Skipf("real fixtures.jsonl missing at %s: %v", fixturesPath, err)
	}
	if _, err := os.Stat(manifestPath); err != nil {
		t.Skipf("real fixtures.manifest.json missing at %s: %v", manifestPath, err)
	}

	// Build the binary once into a temp directory.
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "jev-dryrun")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = wd
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	t.Run("free_output_accepted", func(t *testing.T) {
		outPath := filepath.Join(t.TempDir(), "plan.json")
		cmd := exec.Command(binPath,
			"-fixtures", fixturesPath,
			"-manifest", manifestPath,
			"-model", "typesafe/jev-1.13",
			"-rate-in", "0.042",
			"-rate-out", "0", // free output
			"-cap", "10",
			"-out", outPath,
		)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("dryrun failed: %v\n%s", err, stderr.String())
		}
		data, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatalf("read output: %v", err)
		}
		var plan map[string]any
		if err := json.Unmarshal(data, &plan); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if plan["rateOut"] != float64(0) {
			t.Fatalf("expected rateOut 0 (free output), got %v", plan["rateOut"])
		}
		if plan["totalFixtures"] != float64(332) {
			t.Fatalf("expected totalFixtures 332, got %v", plan["totalFixtures"])
		}
	})

	t.Run("default_repetitions_3", func(t *testing.T) {
		outPath := filepath.Join(t.TempDir(), "plan.json")
		cmd := exec.Command(binPath,
			"-fixtures", fixturesPath,
			"-manifest", manifestPath,
			"-model", "typesafe/jev-1.13",
			"-rate-in", "0.042",
			"-rate-out", "0",
			"-cap", "100",
			"-out", outPath,
		)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("dryrun failed: %v\n%s", err, stderr.String())
		}
		data, _ := os.ReadFile(outPath)
		var plan map[string]any
		if err := json.Unmarshal(data, &plan); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if plan["repetitions"] != float64(3) {
			t.Fatalf("expected default repetitions=3, got %v", plan["repetitions"])
		}
	})

	t.Run("default_cap_zero_nonzero_exit", func(t *testing.T) {
		// Default -cap is 0. The plan will exceed $0, so the binary must
		// exit nonzero.
		outPath := filepath.Join(t.TempDir(), "plan.json")
		cmd := exec.Command(binPath,
			"-fixtures", fixturesPath,
			"-manifest", manifestPath,
			"-model", "typesafe/jev-1.13",
			"-rate-in", "0.042",
			"-rate-out", "0.042",
			"-out", outPath,
		)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err == nil {
			t.Fatal("expected nonzero exit when default cap=$0 is exceeded")
		}
		// Exit code 1 = cap exceeded per main.go.
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() != 1 {
				t.Fatalf("expected exit code 1, got %d", exitErr.ExitCode())
			}
		} else {
			t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
		}
		if !strings.Contains(stderr.String(), "EXCEEDS CAP") {
			t.Fatalf("stderr should mention EXCEEDS CAP, got: %s", stderr.String())
		}
	})

	t.Run("missing_rate_in_nonzero_exit", func(t *testing.T) {
		outPath := filepath.Join(t.TempDir(), "plan.json")
		cmd := exec.Command(binPath,
			"-fixtures", fixturesPath,
			"-manifest", manifestPath,
			"-model", "typesafe/jev-1.13",
			// -rate-in intentionally omitted
			"-rate-out", "0.042",
			"-cap", "100",
			"-out", outPath,
		)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err == nil {
			t.Fatal("expected nonzero exit when -rate-in is missing")
		}
		if !strings.Contains(stderr.String(), "rate-in") {
			t.Fatalf("stderr should mention rate-in, got: %s", stderr.String())
		}
	})
}
