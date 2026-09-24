package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubmitDecisionsHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"verdict"`) {
			t.Errorf("missing verdict question ID in request: %s", string(body))
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing bearer token")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "resp-1",
			"model": "test-model",
			"answers": map[string]any{
				"verdict": map[string]any{
					"type":       "choice",
					"choice":     "supported",
					"confidence": 0.9,
					"probabilities": map[string]float64{
						"supported":             0.9,
						"contradicted":          0.05,
						"insufficient-evidence": 0.05,
					},
				},
			},
			"usage": map[string]any{
				"input_tokens":  42,
				"output_tokens": 12,
				"cost":          0.001,
			},
		})
	}))
	defer srv.Close()
	c, err := NewClient(Config{Endpoint: srv.URL, APIKey: "test-key", Model: "test-model", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	dec, err := c.SubmitDecisions(context.Background(), DecisionRequest{
		Model: "test-model",
		Questions: map[string]DecisionQuestion{
			"verdict": {
				Instructions: "Decide.",
				Criteria: map[string]DecisionCriteria{
					"supported":             {Description: "Supported evidence."},
					"contradicted":          {Description: "Evidence contradicts."},
					"insufficient-evidence": {Description: "No relevant evidence."},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if dec.Choice != "supported" {
		t.Fatalf("choice: %s", dec.Choice)
	}
	if dec.Confidence == nil || *dec.Confidence != 0.9 {
		t.Fatalf("confidence: %v", dec.Confidence)
	}
	if !approx(dec.Probabilities["supported"], 0.9) {
		t.Fatalf("prob: %v", dec.Probabilities)
	}
	if dec.ResolvedModel != "test-model" {
		t.Fatalf("model: %s", dec.ResolvedModel)
	}
	if len(dec.AttemptLatencies) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(dec.AttemptLatencies))
	}
	if dec.TotalLatency <= 0 {
		t.Fatalf("expected positive total wall time, got %v", dec.TotalLatency)
	}
}

func TestSubmitDecisionsConfidenceAbsenceIsRecordedAsNil(t *testing.T) {
	// P1 finding #6: absent confidence must be recorded as nil, not 0.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","model":"x","answers":{"verdict":{"type":"choice","choice":"a","probabilities":{"a":1.0}}},"usage":{}}`))
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL, Model: "x", Timeout: 2 * time.Second})
	dec, err := c.SubmitDecisions(context.Background(), DecisionRequest{
		Model:     "x",
		Questions: map[string]DecisionQuestion{"verdict": {Instructions: "x", Criteria: map[string]DecisionCriteria{"a": {Description: "y"}}}},
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if dec.Confidence != nil {
		t.Fatalf("expected nil confidence when model omits field, got %v", *dec.Confidence)
	}
}

func TestSubmitDecisionsProbabilitiesOptional(t *testing.T) {
	// Probabilities are optional per the documented recipe; the adapter
	// must accept a response without them and record nil.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","model":"x","answers":{"verdict":{"type":"choice","choice":"a","confidence":1.0}},"usage":{}}`))
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL, Model: "x", Timeout: 2 * time.Second})
	dec, err := c.SubmitDecisions(context.Background(), DecisionRequest{
		Model:     "x",
		Questions: map[string]DecisionQuestion{"verdict": {Instructions: "x", Criteria: map[string]DecisionCriteria{"a": {Description: "y"}}}},
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if dec.Probabilities != nil {
		t.Fatalf("expected nil probabilities, got %v", dec.Probabilities)
	}
}

func TestSubmitDecisionsModelPinMismatch(t *testing.T) {
	c, _ := NewClient(Config{Endpoint: "http://localhost", Model: "expected-model", Timeout: time.Second})
	_, err := c.SubmitDecisions(context.Background(), DecisionRequest{Model: "another-model", Questions: map[string]DecisionQuestion{
		"verdict": {Instructions: "x", Criteria: map[string]DecisionCriteria{"a": {Description: "y"}}},
	}})
	if err == nil {
		t.Fatal("expected model-pin error")
	}
	if !errors.As(err, new(*TransportError)) {
		t.Fatalf("expected TransportError, got %T", err)
	}
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "model-pin" {
		t.Fatalf("expected model-pin, got %v", err)
	}
}

func TestSubmitDecisionsModelPinRejectsPinnedMismatchResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "resp",
			"model": "unexpected-model",
			"answers": map[string]any{"verdict": map[string]any{
				"type": "choice", "choice": "a", "confidence": 0.5,
				"probabilities": map[string]float64{"a": 1.0},
			}},
			"usage": map[string]any{},
		})
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL, Model: "expected-model", Timeout: 2 * time.Second})
	_, err := c.SubmitDecisions(context.Background(), DecisionRequest{
		Model: "expected-model",
		Questions: map[string]DecisionQuestion{
			"verdict": {Instructions: "x", Criteria: map[string]DecisionCriteria{"a": {Description: "y"}}},
		},
	})
	if err == nil {
		t.Fatal("expected model-pin error")
	}
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "model-pin" {
		t.Fatalf("expected model-pin, got %v", err)
	}
}

func TestSubmitDecisions401DoesNotRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL, Model: "x", Timeout: 2 * time.Second})
	_, err := c.SubmitDecisions(context.Background(), DecisionRequest{
		Model:     "x",
		Questions: map[string]DecisionQuestion{"verdict": {Instructions: "x", Criteria: map[string]DecisionCriteria{"a": {Description: "y"}}}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "auth" {
		t.Fatalf("expected auth, got %v", err)
	}
	if calls := atomic.LoadInt32(&calls); calls != 1 {
		t.Fatalf("401 should not retry, got %d calls", calls)
	}
}

func TestSubmitDecisions402UsageOverrun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "payment required", http.StatusPaymentRequired)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL, Model: "x", Timeout: 2 * time.Second})
	_, err := c.SubmitDecisions(context.Background(), DecisionRequest{
		Model:     "x",
		Questions: map[string]DecisionQuestion{"verdict": {Instructions: "x", Criteria: map[string]DecisionCriteria{"a": {Description: "y"}}}},
	})
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "usage-overrun" {
		t.Fatalf("expected usage-overrun, got %v", err)
	}
}

func TestSubmitDecisions403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL, Model: "x", Timeout: 2 * time.Second})
	_, err := c.SubmitDecisions(context.Background(), DecisionRequest{
		Model:     "x",
		Questions: map[string]DecisionQuestion{"verdict": {Instructions: "x", Criteria: map[string]DecisionCriteria{"a": {Description: "y"}}}},
	})
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "auth" {
		t.Fatalf("expected auth, got %v", err)
	}
}

func TestSubmitDecisions429RetriesThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "too many", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "r", "model": "x",
			"answers": map[string]any{"verdict": map[string]any{
				"type": "choice", "choice": "a", "confidence": 0.7,
				"probabilities": map[string]float64{"a": 0.7, "b": 0.3},
			}},
			"usage": map[string]any{},
		})
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL, Model: "x", Timeout: 2 * time.Second})
	dec, err := c.SubmitDecisions(context.Background(), DecisionRequest{
		Model:     "x",
		Questions: map[string]DecisionQuestion{"verdict": {Instructions: "x", Criteria: map[string]DecisionCriteria{"a": {Description: "a"}, "b": {Description: "b"}}}},
	})
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if dec.Attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", dec.Attempts)
	}
}

func TestSubmitDecisions500RetriesOnceThenFails(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "server", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL, Model: "x", Timeout: 2 * time.Second})
	_, err := c.SubmitDecisions(context.Background(), DecisionRequest{
		Model:     "x",
		Questions: map[string]DecisionQuestion{"verdict": {Instructions: "x", Criteria: map[string]DecisionCriteria{"a": {Description: "y"}}}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls := atomic.LoadInt32(&calls); calls != 2 {
		t.Fatalf("expected 2 calls (initial + 1 retry), got %d", calls)
	}
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "server" {
		t.Fatalf("expected server, got %v", err)
	}
}

func TestSubmitDecisionsTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL, Model: "x", Timeout: 10 * time.Millisecond})
	_, err := c.SubmitDecisions(context.Background(), DecisionRequest{
		Model:     "x",
		Questions: map[string]DecisionQuestion{"verdict": {Instructions: "x", Criteria: map[string]DecisionCriteria{"a": {Description: "y"}}}},
	})
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "timeout" {
		t.Fatalf("expected timeout, got %v", err)
	}
}

func TestSubmitDecisionsMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL, Model: "x", Timeout: 2 * time.Second})
	_, err := c.SubmitDecisions(context.Background(), DecisionRequest{
		Model:     "x",
		Questions: map[string]DecisionQuestion{"verdict": {Instructions: "x", Criteria: map[string]DecisionCriteria{"a": {Description: "y"}}}},
	})
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "schema" {
		t.Fatalf("expected schema, got %v", err)
	}
}

func TestSubmitDecisionsDistributionNotFinite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","model":"x","answers":{"verdict":{"type":"choice","choice":"a","confidence":1.0,"probabilities":{"a":1.0,"b":-0.1}}},"usage":{}}`))
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL, Model: "x", Timeout: 2 * time.Second})
	_, err := c.SubmitDecisions(context.Background(), DecisionRequest{
		Model:     "x",
		Questions: map[string]DecisionQuestion{"verdict": {Instructions: "x", Criteria: map[string]DecisionCriteria{"a": {Description: "a"}, "b": {Description: "b"}}}},
	})
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "schema" {
		t.Fatalf("expected schema, got %v", err)
	}
}

func TestSubmitDecisionsDistributionSumOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","model":"x","answers":{"verdict":{"type":"choice","choice":"a","confidence":0.5,"probabilities":{"a":0.2,"b":0.1}}},"usage":{}}`))
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL, Model: "x", Timeout: 2 * time.Second})
	_, err := c.SubmitDecisions(context.Background(), DecisionRequest{
		Model:     "x",
		Questions: map[string]DecisionQuestion{"verdict": {Instructions: "x", Criteria: map[string]DecisionCriteria{"a": {Description: "a"}, "b": {Description: "b"}}}},
	})
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "schema" {
		t.Fatalf("expected schema, got %v", err)
	}
}

func TestSubmitDecisionsMissingVerdictQuestion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","model":"x","answers":{"other":{"type":"choice","choice":"a","confidence":1.0,"probabilities":{"a":1.0}}},"usage":{}}`))
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL, Model: "x", Timeout: 2 * time.Second})
	_, err := c.SubmitDecisions(context.Background(), DecisionRequest{
		Model:     "x",
		Questions: map[string]DecisionQuestion{"verdict": {Instructions: "x", Criteria: map[string]DecisionCriteria{"a": {Description: "a"}}}},
	})
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "schema" {
		t.Fatalf("expected schema, got %v", err)
	}
}

func TestSubmitDecisionsPayloadTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(make([]byte, MaxResponseBytes+1024))
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL, Model: "x", Timeout: 5 * time.Second})
	_, err := c.SubmitDecisions(context.Background(), DecisionRequest{
		Model:     "x",
		Questions: map[string]DecisionQuestion{"verdict": {Instructions: "x", Criteria: map[string]DecisionCriteria{"a": {Description: "y"}}}},
	})
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "payload" {
		t.Fatalf("expected payload, got %v", err)
	}
}

func TestSubmitDecisionsInvalidatesRequestBeforeSend(t *testing.T) {
	c, _ := NewClient(Config{Endpoint: "http://localhost", Model: "x", Timeout: time.Second})
	_, err := c.SubmitDecisions(context.Background(), DecisionRequest{Model: "x"})
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "schema" {
		t.Fatalf("expected schema, got %v", err)
	}
}

func TestSubmitDecisionsRequestBodyContainsQuestions(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","model":"x","answers":{"verdict":{"type":"choice","choice":"a","confidence":1.0,"probabilities":{"a":1.0}}},"usage":{}}`))
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL, Model: "x", Timeout: 2 * time.Second})
	_, _ = c.SubmitDecisions(context.Background(), DecisionRequest{
		Model:     "x",
		State:     State{ID: "trace-1", System: "be terse", Docs: []string{"a", "b"}},
		Questions: map[string]DecisionQuestion{"verdict": {Instructions: "decide", Criteria: map[string]DecisionCriteria{"a": {Description: "alpha"}}}},
	})
	if !strings.Contains(got, `"verdict"`) {
		t.Errorf("expected verdict question id in request body")
	}
	if !strings.Contains(got, `"trace-1"`) {
		t.Errorf("expected state id in request body")
	}
}

func approx(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}
