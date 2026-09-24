package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	jevcompare "github.com/Clarit-AI/Plexium/evaluations/jev/compare"
	"github.com/Clarit-AI/Plexium/evaluations/jev/ledger"
	"github.com/Clarit-AI/Plexium/evaluations/jev/pilot"
	"github.com/Clarit-AI/Plexium/evaluations/jev/scoring"
)

func TestPrepareWritesFrozenInventoryAndCorpusReference(t *testing.T) {
	d := t.TempDir()
	inv := filepath.Join(d, "inventory.json")
	ref := filepath.Join(d, "corpus.json")
	err := runCLI([]string{"prepare", "-fixtures", "../../review-pilot/fixtures.jsonl", "-manifest", "../../review-pilot/fixtures.manifest.json", "-out", inv, "-corpus-reference-out", ref, "-jev-request-model", "typesafe/jev-1.13", "-nano-request-model", "openai/gpt-4.1-nano", "-nano-provider", "OpenAI"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{inv, ref} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(`"expectedLabel"`)) {
			t.Fatalf("%s contains gold", p)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0222 != 0 {
			t.Fatalf("%s is writable: %v", p, info.Mode())
		}
	}
}

func TestPrepareReloadRunResumeAndReportWithMockHTTP(t *testing.T) {
	isolateAllocationRegistry(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/alpha/decisions":
			var request struct {
				Questions map[string]struct {
					Criteria map[string]any `json:"criteria"`
				} `json:"questions"`
			}
			if err := json.Unmarshal(body, &request); err != nil {
				t.Error(err)
				return
			}
			labels := make([]string, 0, len(request.Questions["verdict"].Criteria))
			for label := range request.Questions["verdict"].Criteria {
				labels = append(labels, label)
			}
			sort.Strings(labels)
			fmt.Fprintf(w, `{"id":"j-%d","model":"jev-pin","provider":"P","answers":{"verdict":{"type":"choice","choice":%q}},"usage":{"input_tokens":1,"output_tokens":1,"cost":0}}`, calls.Load(), labels[0])
		case "/api/v1/chat/completions":
			var request struct {
				ResponseFormat struct {
					JSONSchema struct {
						Schema struct {
							Properties struct {
								Label struct {
									Enum []string `json:"enum"`
								} `json:"label"`
							} `json:"properties"`
						} `json:"schema"`
					} `json:"json_schema"`
				} `json:"response_format"`
			}
			if err := json.Unmarshal(body, &request); err != nil {
				t.Error(err)
				return
			}
			content, _ := json.Marshal(map[string]string{"label": request.ResponseFormat.JSONSchema.Schema.Properties.Label.Enum[0]})
			fmt.Fprintf(w, `{"id":"n-%d","model":"nano-pin","provider":"P","choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0}}`, calls.Load(), string(content))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	inventoryPath := filepath.Join(dir, "inventory.json")
	corpusPath := filepath.Join(dir, "corpus.json")
	if err := runCLI([]string{"prepare", "-fixtures", "../../review-pilot/fixtures.jsonl", "-manifest", "../../review-pilot/fixtures.manifest.json", "-out", inventoryPath, "-corpus-reference-out", corpusPath, "-jev-request-model", "jev-alias", "-nano-request-model", "nano-alias", "-nano-provider", "P"}); err != nil {
		t.Fatal(err)
	}
	var inv struct {
		InventoryHash string `json:"inventorySha256"`
	}
	if err := readJSON(inventoryPath, &inv); err != nil {
		t.Fatal(err)
	}
	rateOut := ledger.MicroUnit(0)
	arm := func(name, endpoint, alias, pin string) armManifest {
		return armManifest{Endpoint: endpoint, RequestModel: alias, ResponseModel: pin, ResponseProvider: "P", APIKeyEnv: "JEV_PILOT_TEST_KEY", LedgerPath: filepath.Join(dir, name+".ledger.jsonl"), RatesVersion: "test", RateEvidence: "mock", TokenBoundEvidence: "mock", PinMappingEvidence: "mock", ProviderEvidence: "mock", OutputLimitEvidence: "mock", Subcap: 24, Reservation: 1, RateIn: 1_000_000, RateOut: &rateOut, InputBound: 1, OutputBound: 1}
	}
	m := executionManifest{InventoryPath: inventoryPath, InventoryHash: inv.InventoryHash, AuthorizationReference: "mock-only", AuthorizationCap: 100, FrozenPriorExposure: 52, AllocationID: "mock-allocation", AllocationRecordPath: filepath.Join(dir, "allocation.json"), ProbeReconciliation: probeReconciliationPrecondition, RunID: "mock-run", JournalPath: filepath.Join(dir, "journal.jsonl"), EvidenceDir: filepath.Join(dir, "evidence"), RunLockPath: filepath.Join(dir, "run.lock"), CombinedCap: 48, LiveContractsVerified: true, Jev: arm("jev", server.URL+"/api/alpha/decisions", "jev-alias", "jev-pin"), Nano: arm("nano", server.URL+"/api/v1/chat/completions", "nano-alias", "nano-pin")}
	manifestBytes, _ := json.Marshal(m)
	executionPath := filepath.Join(dir, "execution.json")
	if err := os.WriteFile(executionPath, manifestBytes, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JEV_PILOT_TEST_KEY", "mock-key")
	if err := runCLI([]string{"run", "-execution-manifest", executionPath}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 48 {
		t.Fatalf("calls=%d want 48", calls.Load())
	}
	if err := runCLI([]string{"run", "-execution-manifest", executionPath}); err != nil {
		t.Fatalf("safe resume failed: %v", err)
	}
	if calls.Load() != 48 {
		t.Fatalf("resume resent calls: %d", calls.Load())
	}
	mismatch := m
	mismatch.RunID = "different-run-identity"
	mismatchBytes, _ := json.Marshal(mismatch)
	mismatchPath := filepath.Join(dir, "execution-mismatch.json")
	if err := os.WriteFile(mismatchPath, mismatchBytes, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JEV_PILOT_TEST_KEY", "")
	if err := runCLI([]string{"run", "-execution-manifest", mismatchPath}); err == nil || !strings.Contains(err.Error(), "allocation record does not match") {
		t.Fatalf("mismatched allocation error=%v", err)
	}
	if calls.Load() != 48 {
		t.Fatalf("mismatched allocation dialed: calls=%d", calls.Load())
	}
	t.Setenv("JEV_PILOT_TEST_KEY", "mock-key")
	reportFile, err := os.Create(filepath.Join(dir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = reportFile
	reportErr := runCLI([]string{"report", "-journal", m.JournalPath, "-inventory", inventoryPath, "-evidence-dir", m.EvidenceDir, "-fixtures", "../../review-pilot/fixtures.jsonl", "-manifest", "../../review-pilot/fixtures.manifest.json"})
	_ = reportFile.Close()
	os.Stdout = oldStdout
	output, _ := os.ReadFile(reportFile.Name())
	if reportErr != nil {
		t.Fatal(reportErr)
	}
	var report map[string]any
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("report JSON: %v output=%s", err, output)
	}
	if report["scheduledSlots"] != float64(48) || report["reconciledSlots"] != float64(48) || report["requestAttempts"] != float64(48) {
		t.Fatalf("report=%v", report)
	}
	// Decision-2 statements must be bound in the report.
	basisRaw, err := json.Marshal(report["billingBasis"])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"rateSemantics":"UNRECONCILED"`, `"liveContractsVerified":false`, `"jevRateOutPerMillionMicrodollars":0`} {
		if !strings.Contains(string(basisRaw), want) {
			t.Fatalf("report billingBasis missing %s: %s", want, basisRaw)
		}
	}
	// The scored reports carry the embedded corpus provenance.
	scoredRaw, err := json.Marshal(report["tuningOnlyScores"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(scoredRaw), `"corpusProvenance"`) || !strings.Contains(string(scoredRaw), `"fixtureIdentities"`) {
		t.Fatalf("scored reports lack embedded provenance: %s", scoredRaw)
	}
	if err := os.Remove(m.JournalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.JournalPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := runCLI([]string{"run", "-execution-manifest", executionPath}); err == nil {
		t.Fatal("empty replacement journal accepted with existing reservations")
	}
	if calls.Load() != 48 {
		t.Fatalf("replacement journal caused resend: calls=%d", calls.Load())
	}
	for _, path := range []string{m.JournalPath, m.Jev.LedgerPath, m.Nano.LedgerPath} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("JEV_PILOT_TEST_KEY", "")
	if err := runCLI([]string{"run", "-execution-manifest", executionPath}); err == nil || !strings.Contains(err.Error(), "allocation already claimed") {
		t.Fatalf("reused allocation error=%v", err)
	}
	if calls.Load() != 48 {
		t.Fatalf("reused allocation dialed: calls=%d", calls.Load())
	}
}

func TestOutputRatePresenceAtManifestBoundary(t *testing.T) {
	base := `{"endpoint":"https://example.test/api/alpha/decisions","requestModel":"alias","responseModel":"pin","responseProvider":"P","apiKeyEnv":"TEST_KEY","ledgerPath":"LEDGER","ratesVersion":"v","rateEvidence":"r","tokenBoundEvidence":"t","pinMappingEvidence":"p","providerEvidence":"p","outputLimitEvidence":"o","subcapMicrodollars":100,"reservationMicrodollars":1,"rateInPerMillionMicrodollars":1000000,"maxBilledInputTokens":1,"maxBilledOutputTokens":1%s}`
	for _, tc := range []struct {
		name, suffix string
		wantOK       bool
	}{{"omitted", "", false}, {"null", `,"rateOutPerMillionMicrodollars":null`, false}, {"zero", `,"rateOutPerMillionMicrodollars":0`, true}, {"positive", `,"rateOutPerMillionMicrodollars":1000000`, true}} {
		t.Run(tc.name, func(t *testing.T) {
			var a armManifest
			if err := json.Unmarshal([]byte(fmt.Sprintf(base, tc.suffix)), &a); err != nil {
				t.Fatal(err)
			}
			a.LedgerPath = filepath.Join(t.TempDir(), "ledger.jsonl")
			err := validateArmContracts("jev", a)
			if tc.wantOK != (err == nil) {
				t.Fatalf("validate error=%v wantOK=%t", err, tc.wantOK)
			}
			l, b, openErr := openBudget("run", "jev", "hash", a)
			if tc.wantOK {
				if openErr != nil {
					t.Fatal(openErr)
				}
				defer l.Close()
				if b.RateOut != *a.RateOut {
					t.Fatalf("rate out=%d", b.RateOut)
				}
			} else if openErr == nil {
				l.Close()
				t.Fatal("missing/null output rate opened ledger")
			}
		})
	}
}

func TestCompareSidecarCLIWritesVerifiableThreeArmBinding(t *testing.T) {
	dir := t.TempDir()
	baselinePath := filepath.Join(dir, "baseline.json")
	pilotPath := filepath.Join(dir, "pilot.json")
	outPath := filepath.Join(dir, "comparison.json")
	prov, err := jevcompare.ProvenanceFromFiles("../../review-pilot/fixtures.jsonl", "../../review-pilot/fixtures.manifest.json", "../../pilot/request-inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	mk := func(src scoring.Source) jevcompare.BoundReport {
		rep := scoring.Report{Source: src, ProtocolVersion: "0.4.0", FixtureCount: 24}
		if src == scoring.SourceBaseline {
			rep.BaselineVersion = "v0.4-abstaining"
		}
		return jevcompare.BoundReport{Report: rep, CorpusProvenance: prov, BillingBasis: jevcompare.DefaultBillingBasis()}
	}
	for path, value := range map[string]any{
		baselinePath: map[string]any{"baseline": mk(scoring.SourceBaseline)},
		pilotPath:    map[string]any{"tuningOnlyScores": map[string]any{"jev": mk(scoring.Source("jev")), "nano": mk(scoring.Source("nano"))}},
	} {
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := jevcompare.Config{FixturesPath: "../../review-pilot/fixtures.jsonl", ManifestPath: "../../review-pilot/fixtures.manifest.json", InventoryPath: "../../pilot/request-inventory.json", BaselineReport: baselinePath, LivePilotReport: pilotPath}
	if err := runCLI([]string{"compare-sidecar", "-fixtures", cfg.FixturesPath, "-manifest", cfg.ManifestPath, "-inventory", cfg.InventoryPath, "-baseline-report", baselinePath, "-pilot-report", pilotPath, "-out", outPath}); err != nil {
		t.Fatal(err)
	}
	var sidecar jevcompare.Sidecar
	if err := readJSON(outPath, &sidecar); err != nil {
		t.Fatal(err)
	}
	if err := jevcompare.Verify(&sidecar, cfg); err != nil {
		t.Fatalf("written sidecar does not verify: %v", err)
	}
}

func TestRunFailsClosedWithoutReviewedContracts(t *testing.T) {
	p := filepath.Join(t.TempDir(), "execution.json")
	if err := os.WriteFile(p, []byte(`{"liveContractsVerified":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runCLI([]string{"run", "-execution-manifest", p}); err == nil {
		t.Fatal("run accepted unresolved live contracts")
	}
}

func TestBillingTotalsExcludeInvalidCostAndExposeKnownOverrun(t *testing.T) {
	state := pilot.ReplayState{Slots: map[int]pilot.SlotState{
		1: {Reserved: true, ReservedAmount: 2, Event: pilot.JournalEvent{SlotOrdinal: 1}},
		2: {Reserved: true, ReservedAmount: 2, Event: pilot.JournalEvent{SlotOrdinal: 2}},
	}}
	outcomes := []pilot.Outcome{
		{Slot: pilot.Slot{Ordinal: 1}, CostMicrodollars: 5000, KnownCost: false},
		{Slot: pilot.Slot{Ordinal: 2}, CostMicrodollars: 9, KnownCost: true},
	}
	known, exposure := billingTotals(outcomes, state)
	if known != 9 {
		t.Fatalf("known spend=%d want 9", known)
	}
	if exposure != 11 {
		t.Fatalf("exposure=%d want 11 (reservation 2 + overrun 9)", exposure)
	}
}

// TestDecision2RateOutExplicitZeroBoundInManifestAndReport: documented free
// Jev output binds RateOut as an explicit zero — present in the execution
// manifest JSON and in every report — never omitted, and MaxOutputTokens is
// stated as a non-cost resource bound.
func TestDecision2RateOutExplicitZeroBoundInManifestAndReport(t *testing.T) {
	const manifestJSON = `{"endpoint":"https://example.test/api/alpha/decisions","requestModel":"alias","responseModel":"pin","responseProvider":"P","apiKeyEnv":"TEST_KEY","ledgerPath":"LEDGER","ratesVersion":"v","rateEvidence":"r","tokenBoundEvidence":"t","pinMappingEvidence":"p","providerEvidence":"p","outputLimitEvidence":"o","subcapMicrodollars":100,"reservationMicrodollars":1,"rateInPerMillionMicrodollars":1000000,"rateOutPerMillionMicrodollars":0,"maxBilledInputTokens":1,"maxBilledOutputTokens":1}`
	var a armManifest
	if err := json.Unmarshal([]byte(manifestJSON), &a); err != nil {
		t.Fatal(err)
	}
	if a.RateOut == nil || *a.RateOut != 0 {
		t.Fatalf("RateOut explicit zero not bound in manifest: %+v", a.RateOut)
	}
	back, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(back), `"rateOutPerMillionMicrodollars":0`) {
		t.Fatalf("RateOut explicit zero omitted on manifest re-serialization: %s", back)
	}
	// The report binding states the same policy facts.
	basis := jevcompare.DefaultBillingBasis()
	encoded, err := json.Marshal(basis)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"rateSemantics":"UNRECONCILED"`, `"liveContractsVerified":false`, `"jevRateOutPerMillionMicrodollars":0`, "non-cost resource bound"} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("report billingBasis missing %q: %s", want, encoded)
		}
	}
}

// TestReportBasisStatesDecision5ToleranceRule: the report basis binds the
// accepted accounting basis plus the Decision-5 tolerance rule with its
// pinned bounds and decision-text digest (Decision 5: "binds the definition
// into the report basis"; reports keep every truthful statement).
func TestReportBasisStatesDecision5ToleranceRule(t *testing.T) {
	basis, err := reportBillingBasis()
	if err != nil {
		t.Fatal(err)
	}
	rule := pilot.Decision5Tolerance()
	if got := basis["rateToleranceRule"]; got != rule.Description {
		t.Fatalf("rateToleranceRule = %v", got)
	}
	if got := basis["rateToleranceDecisionSha256"]; got != rule.DecisionSHA256 || got == "" {
		t.Fatalf("rateToleranceDecisionSha256 = %v", got)
	}
	if got := basis["rateToleranceRatioBounds"]; len(got.([]string)) != 2 || got.([]string)[0] != "0.985" || got.([]string)[1] != "0.995" {
		t.Fatalf("rateToleranceRatioBounds = %v", got)
	}
	desc, _ := basis["rateToleranceRule"].(string)
	for _, want := range []string{"[0.985, 0.995]", "max(reported, upstream)", "magnitude shock"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("tolerance rule missing %q: %s", want, desc)
		}
	}
	// Decision-2 statements stay truthful alongside the tolerance rule.
	for _, want := range []string{"UNRECONCILED"} {
		if got := basis["rateSemantics"]; got != want {
			t.Fatalf("rateSemantics = %v want %s", got, want)
		}
	}
	if got := basis["liveContractsVerified"]; got != false {
		t.Fatalf("liveContractsVerified = %v, want false", got)
	}
	if got := basis["jevRateOutPerMillionMicrodollars"]; got != float64(0) && got != 0 {
		t.Fatalf("jevRateOutPerMillionMicrodollars = %v", got)
	}
}
