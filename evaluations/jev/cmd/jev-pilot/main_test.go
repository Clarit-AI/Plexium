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
	"sync/atomic"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/ledger"
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
	m := executionManifest{InventoryPath: inventoryPath, InventoryHash: inv.InventoryHash, AuthorizationReference: "mock-only", RunID: "mock-run", JournalPath: filepath.Join(dir, "journal.jsonl"), EvidenceDir: filepath.Join(dir, "evidence"), RunLockPath: filepath.Join(dir, "run.lock"), CombinedCap: 48, LiveContractsVerified: true, Jev: arm("jev", server.URL+"/api/alpha/decisions", "jev-alias", "jev-pin"), Nano: arm("nano", server.URL+"/api/v1/chat/completions", "nano-alias", "nano-pin")}
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

func TestRunFailsClosedWithoutReviewedContracts(t *testing.T) {
	p := filepath.Join(t.TempDir(), "execution.json")
	if err := os.WriteFile(p, []byte(`{"liveContractsVerified":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runCLI([]string{"run", "-execution-manifest", p}); err == nil {
		t.Fatal("run accepted unresolved live contracts")
	}
}
