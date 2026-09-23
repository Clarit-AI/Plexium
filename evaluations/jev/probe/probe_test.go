package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
)

func TestDryPlanUsesFourValidatedGoldFreeRequests(t *testing.T) {
	plan, err := BuildPlan(inventoryPath(t), Endpoints{Jev: JevEndpoint, Nano: NanoEndpoint})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Requests) != 4 || plan.AuthorizedCap != 1_000_000 || plan.ProbeSubcap != 200_000 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if plan.AccountingBasis == "" || plan.ExecutionReady || !strings.Contains(plan.ExecutionReadiness, "cannot guarantee") {
		t.Fatalf("machine plan lacks accounting/readiness qualifiers: %+v", plan)
	}
	if plan.Requests[2].Kind != "largest-frozen-payload" || plan.Requests[3].Kind != "largest-frozen-payload" {
		t.Fatalf("largest payload probes missing: %+v", plan.Requests)
	}
	encoded, _ := json.Marshal(plan)
	if bytes.Contains(encoded, []byte(`"body"`)) || bytes.Contains(encoded, []byte(`"gold"`)) {
		t.Fatalf("dry plan leaked body or gold: %s", encoded)
	}
}

func TestRunSuccessfulProbeObservation(t *testing.T) {
	server, calls := probeServer(t, responseMode{})
	defer server.Close()
	report, err := Run(context.Background(), testConfig(t, server))
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 4 || report.Halted || len(report.Attempts) != 4 {
		t.Fatalf("calls=%d report=%+v", *calls, report)
	}
	for _, attempt := range report.Attempts {
		if attempt.State != "settled" || attempt.RawResponseSHA256 == "" || attempt.RawEvidencePath == "" {
			t.Fatalf("incomplete successful attempt: %+v", attempt)
		}
		if !attempt.Billing.Cost.Present || !attempt.Billing.Cost.Valid || len(attempt.UsageFields) < 3 {
			t.Fatalf("billing evidence missing: %+v", attempt)
		}
	}
	if report.Gates[2].Status != "UNRESOLVED" || report.Gates[3].Status != "OPEN" || report.Gates[4].Status != "UNRESOLVED" || report.Gates[5].Status != "UNRESOLVED" {
		t.Fatalf("gate assessment not honest: %+v", report.Gates)
	}
	if report.AccountingBasis == "" || report.ExecutionReady || !strings.Contains(report.ExecutionReadiness, "cannot guarantee") {
		t.Fatalf("machine report lacks accounting/readiness qualifiers: %+v", report)
	}
}

func TestRunModelPinMismatchHaltsAfterOneCall(t *testing.T) {
	server, calls := probeServer(t, responseMode{jevModel: "typesafe/jev-unexpected", cost: "0.09"})
	defer server.Close()
	report, err := Run(context.Background(), testConfig(t, server))
	if err == nil || !report.Halted || *calls != 1 || !strings.Contains(report.HaltReason, "model") {
		t.Fatalf("err=%v calls=%d report=%+v", err, *calls, report)
	}
	if report.LedgerBalance < 90_000 {
		t.Fatalf("wrong-pin billed exposure understated: %+v", report)
	}
}

func TestRunUsageOverrunAccountsKnownPositiveBillingBeforeHalt(t *testing.T) {
	server, calls := probeServer(t, responseMode{cost: "0.09", inputTokens: accountingInBound + 1})
	defer server.Close()
	report, err := Run(context.Background(), testConfig(t, server))
	if err == nil || !report.Halted || *calls != 1 || report.LedgerBalance < 90_000 {
		t.Fatalf("err=%v calls=%d report=%+v", err, *calls, report)
	}
}

func TestRunMissingBillingHaltsAndRetainsReservation(t *testing.T) {
	server, calls := probeServer(t, responseMode{omitCost: true})
	defer server.Close()
	report, err := Run(context.Background(), testConfig(t, server))
	if err == nil || !report.Halted || *calls != 1 {
		t.Fatalf("err=%v calls=%d report=%+v", err, *calls, report)
	}
	if report.LedgerBalance != ReservationPerCall || report.Attempts[0].Reconciliation != "" {
		t.Fatalf("missing billing released reservation: %+v", report)
	}
}

func TestRunUnexpectedProviderHaltsAfterOneCall(t *testing.T) {
	server, calls := probeServer(t, responseMode{jevProvider: "Unexpected"})
	defer server.Close()
	report, err := Run(context.Background(), testConfig(t, server))
	if err == nil || !report.Halted || *calls != 1 || report.Attempts[0].ResponseProvider != "Unexpected" {
		t.Fatalf("err=%v calls=%d report=%+v", err, *calls, report)
	}
}

func TestRunAuthorizedCapExhaustionRefusesBeforeDial(t *testing.T) {
	server, calls := probeServer(t, responseMode{})
	defer server.Close()
	cfg := testConfig(t, server)
	report, err := runWithLimits(context.Background(), cfg, runLimits{AuthorizedCap: ReservationPerCall - 1, ProbeSubcap: ProbeSubcap})
	if err == nil || !report.Halted || *calls != 0 || !strings.Contains(report.HaltReason, "cap") {
		t.Fatalf("err=%v calls=%d report=%+v", err, *calls, report)
	}
}

func TestRunRefusesExistingReportWithoutDial(t *testing.T) {
	server, calls := probeServer(t, responseMode{})
	defer server.Close()
	cfg := testConfig(t, server)
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.StateDir, reportName), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), cfg); err == nil || *calls != 0 {
		t.Fatalf("err=%v calls=%d", err, *calls)
	}
}

func TestRunRefusesOrphanLedgerAndEvidenceBeforeDial(t *testing.T) {
	server, calls := probeServer(t, responseMode{})
	defer server.Close()
	cfg := testConfig(t, server)
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if *calls != 4 {
		t.Fatalf("first run calls=%d", *calls)
	}
	if err := os.Remove(filepath.Join(cfg.StateDir, reportName)); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), cfg); err == nil || *calls != 4 {
		t.Fatalf("orphan state permitted resend: err=%v calls=%d", err, *calls)
	}
}

func TestRunConcurrentAdmissionAllowsOnlyOneRun(t *testing.T) {
	server, calls := probeServer(t, responseMode{})
	defer server.Close()
	cfg := testConfig(t, server)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := Run(context.Background(), cfg)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var successes int
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 || *calls != 4 {
		t.Fatalf("successes=%d calls=%d", successes, *calls)
	}
}

func TestRunExplicitZeroRetainsFullProbeReservations(t *testing.T) {
	server, calls := probeServer(t, responseMode{cost: "0"})
	defer server.Close()
	report, err := Run(context.Background(), testConfig(t, server))
	if err != nil || *calls != 4 || report.LedgerBalance != ProbeSubcap {
		t.Fatalf("err=%v calls=%d balance=%d report=%+v", err, *calls, report.LedgerBalance, report)
	}
}

func TestRunRedactsReflectedCredentialFromReportAndEvidence(t *testing.T) {
	secret := "fake-probe-key-reflected-579"
	server, calls := probeServer(t, responseMode{usageDiagnostic: secret})
	defer server.Close()
	cfg := testConfig(t, server)
	cfg.APIKey = secret
	report, err := Run(context.Background(), cfg)
	if err == nil || *calls != 1 {
		t.Fatalf("err=%v calls=%d", err, *calls)
	}
	encoded, _ := json.Marshal(report)
	if bytes.Contains(encoded, []byte(secret)) || strings.Contains(err.Error(), secret) {
		t.Fatalf("credential leaked in report/error: report=%s err=%v", encoded, err)
	}
	err = filepath.WalkDir(cfg.StateDir, func(path string, entry os.DirEntry, walkErr error) error {
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

type responseMode struct {
	jevModel        string
	jevProvider     string
	omitCost        bool
	cost            string
	inputTokens     int64
	usageDiagnostic string
}

func probeServer(t *testing.T, mode responseMode) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	if mode.jevModel == "" {
		mode.jevModel = JevCandidatePin
	}
	if mode.jevProvider == "" {
		mode.jevProvider = JevCandidateProvider
	}
	if mode.cost == "" {
		mode.cost = "0.000001"
	}
	if mode.inputTokens == 0 {
		mode.inputTokens = 20
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-Request-Id", fmt.Sprintf("probe-%d", atomic.LoadInt32(&calls)))
		switch r.URL.Path {
		case "/api/alpha/decisions":
			var request adapter.DecisionRequest
			if err := json.Unmarshal(body, &request); err != nil {
				t.Errorf("decode decision: %v", err)
			}
			labels := make([]string, 0, len(request.Questions[adapter.DecisionVerdictID].Criteria))
			for label := range request.Questions[adapter.DecisionVerdictID].Criteria {
				labels = append(labels, label)
			}
			sort.Strings(labels)
			cost := `,"cost":` + mode.cost
			if mode.omitCost {
				cost = ""
			}
			diagnostic := ""
			if mode.usageDiagnostic != "" {
				diagnostic = fmt.Sprintf(`,"diagnostic":%q`, mode.usageDiagnostic)
			}
			fmt.Fprintf(w, `{"id":"d","model":%q,"provider":%q,"answers":{"verdict":{"type":"choice","choice":%q}},"usage":{"input_tokens":%d,"output_tokens":2%s%s}}`, mode.jevModel, mode.jevProvider, labels[0], mode.inputTokens, cost, diagnostic)
		case "/api/v1/chat/completions":
			var request adapter.ChatRequest
			if err := json.Unmarshal(body, &request); err != nil {
				t.Errorf("decode chat: %v", err)
			}
			labels, err := labelsFromSchema(request.ResponseFormat.JSONSchema.Schema)
			if err != nil {
				t.Errorf("labels: %v", err)
			}
			content, _ := json.Marshal(map[string]string{"label": labels[0]})
			fmt.Fprintf(w, `{"id":"c","model":%q,"provider":%q,"choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":2,"total_tokens":22,"cost":%s}}`, NanoCandidatePin, NanoProvider, string(content), mode.cost)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return server, &calls
}

func labelsFromSchema(schema map[string]any) ([]string, error) {
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("properties missing")
	}
	label, ok := properties["label"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("label missing")
	}
	values, ok := label["enum"].([]any)
	if !ok {
		return nil, fmt.Errorf("enum missing")
	}
	labels := make([]string, 0, len(values))
	for _, value := range values {
		labels = append(labels, value.(string))
	}
	sort.Strings(labels)
	return labels, nil
}

func testConfig(t *testing.T, server *httptest.Server) RunConfig {
	t.Helper()
	cfg := DefaultRunConfig(inventoryPath(t), t.TempDir(), "test-key-never-printed")
	cfg.Endpoints = Endpoints{Jev: server.URL + "/api/alpha/decisions", Nano: server.URL + "/api/v1/chat/completions"}
	cfg.HTTPClient = server.Client()
	cfg.Timeout = time.Second
	return cfg
}

func inventoryPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "pilot", "request-inventory.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	return path
}
