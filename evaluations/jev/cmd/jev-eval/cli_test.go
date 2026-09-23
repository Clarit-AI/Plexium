// End-to-end CLI test: build jev-eval, run it on the 24-case tuning
// packet fixtures, verify the v0.4 abstaining baseline is used and
// recorded on the report. Also verify legacy baseline selection works.
package main_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/baseline"
)

const (
	reviewPilotRelDir = "review-pilot"
)

// TestJevEvalV0D4BaselineOnTuningPacket builds jev-eval and runs it
// against the 24-case tuning packet. The default -baseline flag must
// select the v0.4 abstaining baseline; the report must record
// BaselineVersion=v0.4-abstaining and the abstention predictions for
// rp-et-002, rp-et-004, rp-ct-010 must read insufficient-evidence.
func TestJevEvalV0D4BaselineOnTuningPacket(t *testing.T) {
	wd, _ := os.Getwd()
	// wd is evaluations/jev/cmd/jev-eval; the review-pilot dir is at
	// evaluations/jev/review-pilot relative to the package module root
	// (evaluations/jev).
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	fx := filepath.Join(repoRoot, reviewPilotRelDir, "fixtures.jsonl")
	mf := filepath.Join(repoRoot, reviewPilotRelDir, "fixtures.manifest.json")
	if _, err := os.Stat(fx); err != nil {
		t.Fatalf("fixtures.jsonl missing at %s: %v", fx, err)
	}

	binDir := t.TempDir()
	bin := filepath.Join(binDir, "jev-eval")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = wd
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	outPath := filepath.Join(binDir, "report.json")
	cmd := exec.Command(bin,
		"-fixtures", fx,
		"-manifest", mf,
		"-out", outPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("jev-eval failed: %v\nstderr=%s", err, stderr.String())
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report map[string]any
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal report: %v\n%s", err, data)
	}
	baselineSection, _ := report["baseline"].(map[string]any)
	manifestSection, _ := report["manifestSummary"].(map[string]any)
	if baselineSection == nil {
		t.Fatalf("report missing baseline section: %s", data)
	}
	if got := baselineSection["baselineVersion"]; got != string(baseline.BaselineV2) {
		t.Errorf("baseline.baselineVersion = %v, want %q", got, baseline.BaselineV2)
	}
	if got := manifestSection["protocolVersion"]; got != "0.4.0" {
		t.Errorf("manifestSummary.protocolVersion = %v, want 0.4.0", got)
	}

	// The v0.4 abstaining baseline returns "insufficient-evidence"
	// with abstained=true for ALL tasks and ALL fixtures; the
	// deterministic abstaining baseline cannot determine any
	// content label.
	preds := mustReadRawPredictions(t, bin, fx, mf, binDir, baseline.BaselineV2)
	if len(preds) != 24 {
		t.Fatalf("expected 24 baseline predictions, got %d", len(preds))
	}
	for _, p := range preds {
		if p.PredictedLabel != "insufficient-evidence" {
			t.Errorf("v0.4 baseline should predict insufficient-evidence; fixture %s got %q", p.FixtureID, p.PredictedLabel)
		}
		if !p.Abstained {
			t.Errorf("v0.4 baseline should mark abstained=true; fixture %s abstained=%v", p.FixtureID, p.Abstained)
		}
	}
}

// TestJevEvalLegacyBaselineOnTuningPacket verifies the explicit
// -baseline=legacy flag switches to the v0.3 default-fallback
// baseline. The report records BaselineVersion=v0.3-legacy and the
// predictions use the legacy default (entity-type → "document" instead
// of abstention).
func TestJevEvalLegacyBaselineOnTuningPacket(t *testing.T) {
	wd, _ := os.Getwd()
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	fx := filepath.Join(repoRoot, reviewPilotRelDir, "fixtures.jsonl")
	mf := filepath.Join(repoRoot, reviewPilotRelDir, "fixtures.manifest.json")

	binDir := t.TempDir()
	bin := filepath.Join(binDir, "jev-eval")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = wd
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	outPath := filepath.Join(binDir, "report.json")
	cmd := exec.Command(bin,
		"-fixtures", fx,
		"-manifest", mf,
		"-baseline", string(baseline.BaselineLegacy),
		"-out", outPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("jev-eval legacy failed: %v\nstderr=%s", err, stderr.String())
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report map[string]any
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	baselineSection, _ := report["baseline"].(map[string]any)
	if got := baselineSection["baselineVersion"]; got != string(baseline.BaselineLegacy) {
		t.Errorf("legacy baseline.baselineVersion = %v, want %q", got, baseline.BaselineLegacy)
	}

	// Legacy baseline:
	//   entity-type cases → "document" (default fallback, NOT abstention)
	//   candidate-type cases → "CONCEPT" (default fallback, NOT abstention)
	//   relationship and claim-support cases → "insufficient-evidence",
	//     abstained (unchanged from v0.3; same as v0.4 for these tasks).
	preds := mustReadRawPredictions(t, bin, fx, mf, binDir, baseline.BaselineLegacy)
	if len(preds) != 24 {
		t.Fatalf("expected 24 baseline predictions, got %d", len(preds))
	}
	for _, p := range preds {
		switch {
		case taskInSet(p.Task, "entity-type"):
			if p.PredictedLabel != "document" || p.Abstained {
				t.Errorf("legacy entity-type baseline should produce 'document' non-abstained; fixture %s got label=%q abstained=%v", p.FixtureID, p.PredictedLabel, p.Abstained)
			}
		case taskInSet(p.Task, "candidate-type"):
			if p.PredictedLabel != "CONCEPT" || p.Abstained {
				t.Errorf("legacy candidate-type baseline should produce 'CONCEPT' non-abstained; fixture %s got label=%q abstained=%v", p.FixtureID, p.PredictedLabel, p.Abstained)
			}
		default:
			// relationship, claim-support: legacy abstains.
			if p.PredictedLabel != "insufficient-evidence" || !p.Abstained {
				t.Errorf("legacy %s baseline should abstain with 'insufficient-evidence'; fixture %s got label=%q abstained=%v", p.Task, p.FixtureID, p.PredictedLabel, p.Abstained)
			}
		}
	}
}

func TestJevEvalHeldOutReportCarriesReadinessLimitations(t *testing.T) {
	wd, _ := os.Getwd()
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	fx := filepath.Join(repoRoot, "heldout", "fixtures.jsonl")
	mf := filepath.Join(repoRoot, "heldout", "fixtures.manifest.json")

	binDir := t.TempDir()
	bin := filepath.Join(binDir, "jev-eval")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = wd
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	outPath := filepath.Join(binDir, "heldout-report.json")
	cmd := exec.Command(bin, "-fixtures", fx, "-manifest", mf, "-out", outPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("jev-eval held-out failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal held-out report: %v", err)
	}
	manifest := report["manifestSummary"].(map[string]any)
	readiness := manifest["reviewReadiness"].(map[string]any)
	if readiness["studyGateEligible"] != false || readiness["unreviewedCount"] != float64(720) {
		t.Fatalf("report overclaims unreviewed readiness: %v", readiness)
	}
	evidence := manifest["negativeTaskEvidence"].([]any)
	if len(evidence) != 2 {
		t.Fatalf("negativeTaskEvidence=%d, want 2", len(evidence))
	}
	for _, raw := range evidence {
		item := raw.(map[string]any)
		if item["constructionClusterCount"] != float64(2) || item["independentNegativeGateEligible"] != false || item["statisticalIndependenceEstablished"] != false {
			t.Errorf("report overclaims negative-task independence: %v", item)
		}
		if !strings.Contains(item["statisticalLimitationReason"].(string), "structural disjointness") {
			t.Errorf("report lacks statistical limitation reason: %v", item)
		}
	}
	for _, raw := range manifest["splitIndependence"].([]any) {
		item := raw.(map[string]any)
		if item["independenceScope"] != "structural-only" || item["statisticalIndependenceEstablished"] != false {
			t.Errorf("split independence scope is ambiguous: %v", item)
		}
	}
	baselineSection := report["baseline"].(map[string]any)
	byTask := baselineSection["byTask"].(map[string]any)
	claim := byTask["claim-support"].(map[string]any)
	if claim["gateEligible"] != false || !strings.Contains(claim["gateIneligibleReason"].(string), "human-approved") {
		t.Fatalf("claim gate ignores unreviewed gold: %v", claim)
	}
	relationship := byTask["relationship"].(map[string]any)
	if relationship["gateEligible"] != false || !strings.Contains(relationship["gateIneligibleReason"].(string), "human-approved") {
		t.Fatalf("relationship gate does not preserve readiness limitation: %v", relationship)
	}
}

// mustReadRawPredictions invokes jev-eval with -raw-predictions and
// parses the resulting JSONL. Used by both default and legacy tests
// to inspect per-fixture baseline predictions. The caller passes
// the baseline version it wants to exercise so the resulting raw
// predictions reflect that version (otherwise the call uses the
// default v0.4 abstaining baseline).
func mustReadRawPredictions(t *testing.T, bin, fx, mf, binDir string, version baseline.BaselineVersion) []rawPred {
	t.Helper()
	rawPath := filepath.Join(binDir, "raw.jsonl")
	args := []string{"-fixtures", fx, "-manifest", mf}
	if version != "" {
		args = append(args, "-baseline", string(version))
	}
	args = append(args, "-raw-predictions", rawPath)
	cmd := exec.Command(bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("raw-predictions failed: %v\nstderr=%s", err, stderr.String())
	}
	data, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	var out []rawPred
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var p rawPred
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			t.Fatalf("unmarshal line: %v\n%s", err, line)
		}
		out = append(out, p)
	}
	return out
}

type rawPred struct {
	FixtureID      string `json:"fixtureId"`
	Task           string `json:"task"`
	PredictedLabel string `json:"predictedLabel"`
	Abstained      bool   `json:"abstained"`
}

// taskInSet reports whether task matches any of the named task IDs
// (e.g. "entity-type" → "entity-type"). Used by the legacy baseline
// assertions to categorise fixture tasks for expected behaviour.
func taskInSet(task string, names ...string) bool {
	for _, n := range names {
		if task == n {
			return true
		}
	}
	return false
}
