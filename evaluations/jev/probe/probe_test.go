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
	secret := "test-key-never-printed"
	server, calls := probeServer(t, responseMode{usageDiagnosticRaw: `"\u0074est-key-never-printed"`})
	defer server.Close()
	cfg := testConfig(t, server)
	cfg.APIKey = secret
	report, err := Run(context.Background(), cfg)
	if err == nil || *calls != 1 {
		t.Fatalf("err=%v calls=%d", err, *calls)
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("credential leaked in report/error: report=%s err=%v", encoded, err)
	}
	assertDecodedJSONHasNoCredential(t, encoded, secret)
	err = filepath.WalkDir(cfg.StateDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.HasSuffix(path, ".json") {
			if checkErr := decodedJSONHasCredential(data, secret); checkErr != nil {
				return fmt.Errorf("credential screening failed in %s: %w", path, checkErr)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunInvalidStringCostCannotLeakOrBecomeSettleable(t *testing.T) {
	secret := "test-key-never-printed"
	server, calls := probeServer(t, responseMode{cost: `"test-key-never-printed"`})
	defer server.Close()
	cfg := testConfig(t, server)
	cfg.APIKey = secret
	report, err := Run(context.Background(), cfg)
	if err == nil || *calls != 1 {
		t.Fatalf("err=%v calls=%d report=%+v", err, *calls, report)
	}
	if len(report.Attempts) != 1 {
		t.Fatalf("attempts=%d", len(report.Attempts))
	}
	attempt := report.Attempts[0]
	if attempt.Billing.Cost.Valid || attempt.Billing.Cost.Raw != `"[REDACTED_CREDENTIAL]"` {
		t.Fatalf("invalid string cost was not preserved as untrusted redacted evidence: %+v", attempt.Billing.Cost)
	}
	if attempt.State == "settled" || report.LedgerBalance != attempt.Reservation {
		t.Fatalf("invalid string cost became settleable: state=%s balance=%d reservation=%d", attempt.State, report.LedgerBalance, attempt.Reservation)
	}
	encoded, marshalErr := json.Marshal(report)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	assertDecodedJSONHasNoCredential(t, encoded, secret)
	err = filepath.WalkDir(cfg.StateDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".json") {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if checkErr := decodedJSONHasCredential(data, secret); checkErr != nil {
			return fmt.Errorf("credential screening failed in %s: %w", path, checkErr)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBillingEvidenceScreensInvalidRawAndPreservesNumericLexemes(t *testing.T) {
	for _, lexeme := range []string{"20", "120", "205", "0.09", "0.000001"} {
		got := billingEvidence(adapter.BillingObservation{Cost: adapter.DecimalField{Present: true, Valid: true, Raw: lexeme}}, "20").Cost
		if got.Raw != lexeme || got.RawWithheld || got.RawSHA256 != "" {
			t.Fatalf("numeric lexeme %q changed: %+v", lexeme, got)
		}
	}

	secret := "test-key-never-printed"
	observation := adapter.BillingObservation{
		Cost:         adapter.DecimalField{Present: true, Valid: false, Raw: `"test-key-never-printed"`},
		InputTokens:  adapter.DecimalField{Present: true, Valid: false, Raw: `"\u0074est-key-never-printed"`},
		OutputTokens: adapter.DecimalField{Present: true, Valid: false, Raw: `{"diagnostic":"test-key-never-printed"}`},
		TotalTokens:  adapter.DecimalField{Present: true, Valid: false, Raw: `test-key-never-printed`},
	}
	encoded, err := json.Marshal(billingEvidence(observation, secret))
	if err != nil {
		t.Fatal(err)
	}
	assertDecodedJSONHasNoCredential(t, encoded, secret)
	got := billingEvidence(observation, secret)
	for name, field := range map[string]DecimalEvidence{
		"cost": got.Cost, "input": got.InputTokens, "output": got.OutputTokens, "total": got.TotalTokens,
	} {
		if field.Valid {
			t.Fatalf("%s invalid evidence became valid: %+v", name, field)
		}
	}
	if !got.TotalTokens.RawWithheld || got.TotalTokens.RawSHA256 == "" || got.TotalTokens.Raw != "" {
		t.Fatalf("malformed raw evidence was not withheld with a hash: %+v", got.TotalTokens)
	}
}

func TestBillingEvidenceMapsObservedNanoTaxonomyAndRateDiscrepancy(t *testing.T) {
	decimal := func(raw string) adapter.DecimalField {
		return adapter.DecimalField{Present: true, Valid: true, Raw: raw, Number: json.Number(raw)}
	}
	observation := adapter.BillingObservation{
		Cost: decimal("0.000008613"), InputTokens: decimal("63"), OutputTokens: decimal("6"), TotalTokens: decimal("69"),
		PromptTokenDetails: adapter.PromptTokenDetailsObservation{
			Present: true, Valid: true, CachedTokens: decimal("0"), CacheWriteTokens: decimal("0"), AudioTokens: decimal("0"), VideoTokens: decimal("0"),
		},
		CompletionTokenDetails: adapter.CompletionTokenDetailsObservation{
			Present: true, Valid: true, ReasoningTokens: decimal("0"), ImageTokens: decimal("0"), AudioTokens: decimal("0"),
		},
		CostDetails: adapter.CostDetailsObservation{
			Present: true, Valid: true, UpstreamInferenceCost: decimal("0.0000087"),
			UpstreamInferencePromptCost: decimal("0.0000063"), UpstreamInferenceCompletionsCost: decimal("0.0000024"),
		},
		IsBYOK:                   adapter.BooleanField{Present: true, Valid: true, Raw: "false", Value: false},
		RateSemanticsDiscrepancy: "reported cost 0.000008613 differs from upstream_inference_cost 0.0000087; tariff/currency/fee semantics unresolved",
	}
	got := billingEvidence(observation, "credential-not-present")
	if !got.PromptTokenDetails.Valid || !got.CompletionTokenDetails.Valid || !got.CostDetails.Valid || !got.IsBYOK.Valid {
		t.Fatalf("nested taxonomy validity lost: %+v", got)
	}
	if got.CostDetails.UpstreamInferenceCost.Raw != "0.0000087" || got.PromptTokenDetails.CachedTokens.Raw != "0" || got.IsBYOK.Raw != "false" {
		t.Fatalf("nested taxonomy lexical evidence changed: %+v", got)
	}
	if !strings.Contains(got.RateSemanticsDiscrepancy, "tariff/currency/fee semantics unresolved") {
		t.Fatalf("rate discrepancy missing: %+v", got)
	}
}

func TestRunScreensInvalidRawForEveryRealAdapterBillingField(t *testing.T) {
	secret := "test-key-never-printed"
	tests := []struct {
		name  string
		mode  responseMode
		field func(BillingEvidence) DecimalEvidence
	}{
		{name: "cost-literal", mode: responseMode{cost: `"test-key-never-printed"`}, field: func(b BillingEvidence) DecimalEvidence { return b.Cost }},
		{name: "input-unicode", mode: responseMode{inputTokensRaw: `"\u0074est-key-never-printed"`}, field: func(b BillingEvidence) DecimalEvidence { return b.InputTokens }},
		{name: "output-literal", mode: responseMode{outputTokensRaw: `"test-key-never-printed"`}, field: func(b BillingEvidence) DecimalEvidence { return b.OutputTokens }},
		{name: "total-unicode", mode: responseMode{totalTokensRaw: `"\u0074est-key-never-printed"`}, field: func(b BillingEvidence) DecimalEvidence { return b.TotalTokens }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, calls := probeServer(t, test.mode)
			defer server.Close()
			cfg := testConfig(t, server)
			cfg.APIKey = secret
			report, err := Run(context.Background(), cfg)
			if err == nil || *calls != 1 || len(report.Attempts) != 1 {
				t.Fatalf("err=%v calls=%d report=%+v", err, *calls, report)
			}
			field := test.field(report.Attempts[0].Billing)
			if field.Valid || field.Raw != `"[REDACTED_CREDENTIAL]"` {
				t.Fatalf("invalid billing evidence was not screened: %+v", field)
			}
			encoded, marshalErr := json.Marshal(report)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			assertDecodedJSONHasNoCredential(t, encoded, secret)
		})
	}
}

func TestRunNumericCredentialPreservesJSONAndNumericEvidence(t *testing.T) {
	server, calls := probeServer(t, responseMode{})
	defer server.Close()
	cfg := testConfig(t, server)
	cfg.APIKey = "20"
	report, err := Run(context.Background(), cfg)
	if err != nil || *calls != 4 {
		t.Fatalf("err=%v calls=%d report=%+v", err, *calls, report)
	}
	for _, attempt := range report.Attempts {
		if attempt.Billing.InputTokens.Raw != "20" || !attempt.Billing.InputTokens.Valid {
			t.Fatalf("numeric billing evidence corrupted: %+v", attempt.Billing.InputTokens)
		}
	}
	err = filepath.WalkDir(cfg.StateDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".json") {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !json.Valid(data) {
			return fmt.Errorf("invalid JSON in %s", path)
		}
		if strings.Contains(path, "evidence") {
			var body struct {
				Usage map[string]json.RawMessage `json:"usage"`
			}
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.UseNumber()
			if err := dec.Decode(&body); err != nil {
				return fmt.Errorf("decode evidence %s: %w", path, err)
			}
			raw := body.Usage["input_tokens"]
			if len(raw) == 0 {
				raw = body.Usage["prompt_tokens"]
			}
			if string(raw) != "20" {
				return fmt.Errorf("numeric token changed in %s: value=%s", path, raw)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestScreenResponseDecodesStringsPreservesNumbersAndDuplicateKeys(t *testing.T) {
	secret := "test-key-never-printed"
	raw := []byte(`{"input_tokens":20,"output_tokens":2,"cost":0.000001,"diagnostic":"\u0074est-key-never-printed","nested":["prefix test-key-never-printed suffix"],"duplicate":"safe","duplicate":"test-key-never-printed"}`)
	safe, redacted, err := screenResponse(raw, secret)
	if err != nil || !redacted || !json.Valid(safe) {
		t.Fatalf("safe=%s redacted=%v err=%v", safe, redacted, err)
	}
	assertDecodedJSONHasNoCredential(t, safe, secret)
	if bytes.Count(safe, []byte(`"duplicate"`)) != 2 {
		t.Fatalf("duplicate keys were collapsed: %s", safe)
	}
	var decoded map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(safe))
	dec.UseNumber()
	if err := dec.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"input_tokens": "20", "output_tokens": "2", "cost": "0.000001"} {
		if string(decoded[key]) != want {
			t.Fatalf("numeric field %s changed: got=%s want=%s", key, decoded[key], want)
		}
	}
}

func assertDecodedJSONHasNoCredential(t *testing.T, data []byte, credential string) {
	t.Helper()
	if err := decodedJSONHasCredential(data, credential); err != nil {
		t.Fatal(err)
	}
}

func decodedJSONHasCredential(data []byte, credential string) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return fmt.Errorf("parse screened JSON: %w", err)
	}
	var walk func(any) error
	walk = func(current any) error {
		switch typed := current.(type) {
		case string:
			if strings.Contains(typed, credential) {
				return fmt.Errorf("decoded credential remains in string value %q", typed)
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
					return fmt.Errorf("decoded credential remains in object key %q", key)
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

type responseMode struct {
	jevModel           string
	jevProvider        string
	omitCost           bool
	cost               string
	inputTokens        int64
	inputTokensRaw     string
	outputTokensRaw    string
	totalTokensRaw     string
	usageDiagnostic    string
	usageDiagnosticRaw string
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
			} else if mode.usageDiagnosticRaw != "" {
				diagnostic = `,"diagnostic":` + mode.usageDiagnosticRaw
			}
			inputTokens := fmt.Sprint(mode.inputTokens)
			if mode.inputTokensRaw != "" {
				inputTokens = mode.inputTokensRaw
			}
			outputTokens := "2"
			if mode.outputTokensRaw != "" {
				outputTokens = mode.outputTokensRaw
			}
			totalTokens := ""
			if mode.totalTokensRaw != "" {
				totalTokens = `,"total_tokens":` + mode.totalTokensRaw
			}
			fmt.Fprintf(w, `{"id":"d","model":%q,"provider":%q,"answers":{"verdict":{"type":"choice","choice":%q}},"usage":{"input_tokens":%s,"output_tokens":%s%s%s%s}}`, mode.jevModel, mode.jevProvider, labels[0], inputTokens, outputTokens, totalTokens, cost, diagnostic)
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
