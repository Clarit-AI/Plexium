package adapter

import (
	"bytes"
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

func frozenDecisionBody(t *testing.T, model string) []byte {
	t.Helper()
	body, err := json.Marshal(DecisionRequest{
		Model: model,
		Questions: map[string]DecisionQuestion{DecisionVerdictID: {
			Type: "choice", Instructions: "choose exactly one label",
			Criteria: map[string]DecisionCriteria{
				"supported":             {Description: "supported by evidence"},
				"insufficient-evidence": {Description: "evidence is insufficient"},
			},
		}},
		State: State{ID: "fixture-1", Docs: []string{"evidence"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func frozenChatBody(t *testing.T, model, provider string) []byte {
	t.Helper()
	body, err := json.Marshal(ChatRequest{
		Model: model,
		Messages: []ChatMessage{
			{Role: "system", Content: "Return one label."},
			{Role: "user", Content: "Evidence."},
		},
		ResponseFormat: &ResponseFormat{Type: "json_schema", JSONSchema: &JSONSchema{
			Name: "verdict", Strict: true,
			Schema: map[string]any{
				"type": "object", "additionalProperties": false,
				"required": []string{"label"},
				"properties": map[string]any{"label": map[string]any{
					"type": "string", "enum": []string{"supported", "insufficient-evidence"},
				}},
			},
		}},
		MaxTokens: intPointer(256),
		Provider: &ProviderRoute{
			Only: []string{provider}, AllowFallbacks: boolPointer(false), RequireParameters: boolPointer(true),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestSubmitDecisionsOncePreservesEvidenceAndAliasPinSplit(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("X-Request-Id", "header-id")
		w.Header().Set("Set-Cookie", "secret=never-capture")
		_, _ = io.WriteString(w, `{"id":"body-id","model":"typesafe/jev-1.13-20260917","provider":"JevProvider","answers":{"verdict":{"type":"choice","choice":"supported","confidence":0.8,"probabilities":{"supported":0.8,"insufficient-evidence":0.2}}},"usage":{"input_tokens":12,"output_tokens":3,"cost":0}}`)
	}))
	defer srv.Close()
	c, err := NewClient(Config{
		Endpoint: srv.URL + "/api/alpha/decisions", Model: "typesafe/jev-1.13", ResponseModel: "typesafe/jev-1.13-20260917",
		ResponseProvider: "JevProvider", Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	obs, err := c.SubmitDecisionsOnce(context.Background(), frozenDecisionBody(t, "typesafe/jev-1.13"))
	if err != nil {
		t.Fatalf("once: %v", err)
	}
	if calls != 1 || obs.Decision == nil || obs.Decision.Choice != "supported" {
		t.Fatalf("calls=%d decision=%+v", calls, obs.Decision)
	}
	if obs.RequestID != "body-id" || obs.ResponseModel != "typesafe/jev-1.13-20260917" || obs.ResponseProvider != "JevProvider" {
		t.Fatalf("identity evidence missing: %+v", obs)
	}
	if !obs.Billing.Cost.Present || !obs.Billing.Cost.Valid || obs.Billing.Cost.Number.String() != "0" {
		t.Fatalf("zero cost presence lost: %+v", obs.Billing.Cost)
	}
	if _, ok := obs.ResponseHeaders["Set-Cookie"]; ok {
		t.Fatal("unsafe response header captured")
	}
	if obs.RawSHA256 == "" || len(obs.RawResponse) == 0 || obs.Duration <= 0 {
		t.Fatalf("incomplete attempt evidence: %+v", obs)
	}
}

func TestSubmitDecisionsOnceFailureStillReturnsBillingEvidence(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("X-Request-Id", "req-429")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"slow down"},"usage":{"input_tokens":7,"output_tokens":0,"cost":0.0000004}}`)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL + "/api/alpha/decisions", Model: "alias", ResponseModel: "pin", Timeout: time.Second})
	obs, err := c.SubmitDecisionsOnce(context.Background(), frozenDecisionBody(t, "alias"))
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "rate-limit" || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	if obs.Status != 429 || obs.RequestID != "req-429" || !obs.Billing.Cost.Valid || obs.Billing.Cost.Raw != "0.0000004" {
		t.Fatalf("failure evidence missing: %+v", obs)
	}
	if strings.Contains(err.Error(), "slow down") {
		t.Fatal("raw provider body leaked through error string")
	}
}

func TestSubmitDecisionsOnceRejectsBeforeSendAndNeverRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "server", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL + "/api/alpha/decisions", Model: "alias", Timeout: time.Second})
	bad := []byte(`{"model":"alias","questions":{}}`)
	if _, err := c.SubmitDecisionsOnce(context.Background(), bad); err == nil || calls != 0 {
		t.Fatalf("invalid body should not send: err=%v calls=%d", err, calls)
	}
	if _, err := c.SubmitDecisionsOnce(context.Background(), frozenDecisionBody(t, "alias")); err == nil || calls != 1 {
		t.Fatalf("503 must be one visible attempt: err=%v calls=%d", err, calls)
	}
}

func TestOnceRejectsWrongEndpointPathBeforeSend(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL + "/wrong", Model: "alias", Timeout: time.Second})
	obs, err := c.SubmitDecisionsOnce(context.Background(), frozenDecisionBody(t, "alias"))
	if err == nil || calls != 0 || obs.Status != 0 || len(obs.RawResponse) != 0 {
		t.Fatalf("wrong endpoint must fail before send: err=%v calls=%d obs=%+v", err, calls, obs)
	}
}

func TestSubmitDecisionsOnceDoesNotFollowRedirect(t *testing.T) {
	var sourceCalls, targetCalls int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&targetCalls, 1)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&sourceCalls, 1)
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	c, _ := NewClient(Config{Endpoint: source.URL + "/api/alpha/decisions", Model: "alias", Timeout: time.Second})
	obs, err := c.SubmitDecisionsOnce(context.Background(), frozenDecisionBody(t, "alias"))
	if err == nil || obs.Status != http.StatusTemporaryRedirect || sourceCalls != 1 || targetCalls != 0 {
		t.Fatalf("redirect was not stopped: status=%d err=%v source=%d target=%d", obs.Status, err, sourceCalls, targetCalls)
	}
}

func TestCompleteOnceStrictSuccess(t *testing.T) {
	var requestBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestBody, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"id":"chat-1","model":"openai/gpt-4.1-nano-2026-09-01","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13,"cost":0}}`)
	}))
	defer srv.Close()
	c, _ := NewChatClient(ChatConfig{
		Endpoint: srv.URL + "/api/v1/chat/completions", Model: "openai/gpt-4.1-nano", ResponseModel: "openai/gpt-4.1-nano-2026-09-01",
		ResponseProvider: "OpenAI", Timeout: time.Second,
	})
	obs, err := c.CompleteOnce(context.Background(), frozenChatBody(t, "openai/gpt-4.1-nano", "OpenAI"))
	if err != nil {
		t.Fatalf("once: %v raw=%s", err, obs.RawResponse)
	}
	if obs.Chat == nil || obs.Chat.Content["label"] != "supported" || !obs.Billing.Cost.Valid {
		t.Fatalf("strict result/evidence missing: %+v", obs)
	}
	for _, fragment := range []string{`"strict":true`, `"max_tokens":256`, `"allow_fallbacks":false`, `"require_parameters":true`} {
		if !bytes.Contains(requestBody, []byte(fragment)) {
			t.Errorf("frozen request missing %s: %s", fragment, requestBody)
		}
	}
}

func TestCompleteOnceRejectsStrictFailureMatrix(t *testing.T) {
	tests := map[string]string{
		"duplicate-label":  `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\",\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"extra-key":        `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\",\"why\":\"x\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"wrong-enum":       `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"other\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"refusal":          `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}","refusal":"no"},"finish_reason":"stop"}],"usage":{}}`,
		"truncated":        `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"length"}],"usage":{}}`,
		"multiple":         `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"},{"index":1,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"wrong-model":      `{"id":"c","model":"other","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"missing-provider": `{"id":"c","model":"pin","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"wrong-provider":   `{"id":"c","model":"pin","provider":"Other","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"trailing-json":    `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}{}"},"finish_reason":"stop"}],"usage":{}}`,
	}
	for name, response := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, response) }))
			defer srv.Close()
			c, _ := NewChatClient(ChatConfig{Endpoint: srv.URL + "/api/v1/chat/completions", Model: "alias", ResponseModel: "pin", ResponseProvider: "OpenAI", Timeout: time.Second})
			obs, err := c.CompleteOnce(context.Background(), frozenChatBody(t, "alias", "OpenAI"))
			if err == nil || obs.Chat != nil || len(obs.RawResponse) == 0 {
				t.Fatalf("expected preserved strict failure, err=%v obs=%+v", err, obs)
			}
		})
	}
}

func TestBillingPresenceStatesRemainDistinct(t *testing.T) {
	omitted := decimalFrom(nil, false)
	null := decimalFrom(json.RawMessage(`null`), false)
	zero := decimalFrom(json.RawMessage(`0`), false)
	positive := decimalFrom(json.RawMessage(`0.0000004`), false)
	if omitted.Present || !null.Present || !null.Null || !zero.Valid || zero.Number.String() != "0" || !positive.Valid || positive.Raw != "0.0000004" {
		t.Fatalf("presence collapsed: omitted=%+v null=%+v zero=%+v positive=%+v", omitted, null, zero, positive)
	}
	if bad := decimalFrom(json.RawMessage(`1.5`), true); bad.Valid || bad.Error == "" {
		t.Fatalf("nonintegral token count accepted: %+v", bad)
	}
}

func TestOnceRejectsInvalidBillingButPreservesIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"r","model":"pin","answers":{"verdict":{"type":"choice","choice":"supported","probabilities":{"supported":0.7,"insufficient-evidence":0.3}}},"usage":{"input_tokens":1.5,"output_tokens":0,"cost":0}}`)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL + "/api/alpha/decisions", Model: "alias", ResponseModel: "pin", Timeout: time.Second})
	obs, err := c.SubmitDecisionsOnce(context.Background(), frozenDecisionBody(t, "alias"))
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "billing" || obs.Billing.InputTokens.Valid || obs.Billing.InputTokens.Error == "" {
		t.Fatalf("invalid billing not preserved/rejected: err=%v field=%+v", err, obs.Billing.InputTokens)
	}
}

func TestOnceBoundsOversizedRawResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), MaxResponseBytes+128))
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL + "/api/alpha/decisions", Model: "alias", Timeout: 5 * time.Second})
	obs, err := c.SubmitDecisionsOnce(context.Background(), frozenDecisionBody(t, "alias"))
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "payload" || !obs.RawTruncated || len(obs.RawResponse) != MaxResponseBytes || obs.RawSHA256 == "" {
		t.Fatalf("oversized response evidence wrong: err=%v len=%d truncated=%v hash=%q", err, len(obs.RawResponse), obs.RawTruncated, obs.RawSHA256)
	}
}

func TestFrozenRequestRejectsDuplicateKeysAndTrailingJSON(t *testing.T) {
	for name, body := range map[string][]byte{
		"duplicate": []byte(`{"model":"alias","model":"alias","questions":{}}`),
		"trailing":  append(frozenDecisionBody(t, "alias"), []byte(` {}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateDecisionFrozenBody(body, "alias"); err == nil {
				t.Fatal("expected frozen request rejection")
			}
		})
	}
}

func TestOnceCapturesPartialReadError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200, Header: make(http.Header),
			Body: io.NopCloser(&errorAfterReader{data: []byte(`{"usage":{"cost":0}}`)}),
		}, nil
	})}
	c, _ := NewClient(Config{Endpoint: "https://example.invalid/api/alpha/decisions", Model: "alias", Timeout: time.Second, HTTPClient: client})
	obs, err := c.SubmitDecisionsOnce(context.Background(), frozenDecisionBody(t, "alias"))
	if err == nil || obs.ReadError == "" || len(obs.RawResponse) == 0 || !obs.Billing.Cost.Valid {
		t.Fatalf("partial read evidence lost: err=%v obs=%+v", err, obs)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type errorAfterReader struct {
	data []byte
	done bool
}

func (r *errorAfterReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, errors.New("forced read failure")
	}
	r.done = true
	return copy(p, r.data), nil
}

func boolPointer(value bool) *bool { return &value }
func intPointer(value int) *int    { return &value }
