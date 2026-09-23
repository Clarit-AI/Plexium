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
	if report.Gates[3].Status != "open" || report.Gates[5].Status != "evidence-collected" {
		t.Fatalf("gate assessment not honest: %+v", report.Gates)
	}
}

func TestRunModelPinMismatchHaltsAfterOneCall(t *testing.T) {
	server, calls := probeServer(t, responseMode{jevModel: "typesafe/jev-unexpected"})
	defer server.Close()
	report, err := Run(context.Background(), testConfig(t, server))
	if err == nil || !report.Halted || *calls != 1 || !strings.Contains(report.HaltReason, "model") {
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

type responseMode struct {
	jevModel    string
	jevProvider string
	omitCost    bool
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
			cost := `,"cost":0.000001`
			if mode.omitCost {
				cost = ""
			}
			fmt.Fprintf(w, `{"id":"d","model":%q,"provider":%q,"answers":{"verdict":{"type":"choice","choice":%q}},"usage":{"input_tokens":20,"output_tokens":2%s}}`, mode.jevModel, mode.jevProvider, labels[0], cost)
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
			fmt.Fprintf(w, `{"id":"c","model":%q,"provider":%q,"choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":2,"total_tokens":22,"cost":0.000002}}`, NanoCandidatePin, NanoProvider, string(content))
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
