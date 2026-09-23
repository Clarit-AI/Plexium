package adapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

func TestDecisionResponseKeySetStillMatchesCapturedEnvelopeExactly(t *testing.T) {
	captured := []byte(`{"id":"gen-dec-observed","model":"typesafe/jev-1.13-20260917","provider":"TypeSafe","answers":{"verdict":{"type":"choice","choice":"supported","confidence":0.8,"probabilities":{"supported":0.8,"insufficient-evidence":0.2}}},"usage":{"input_tokens":364,"output_tokens":44,"cost":0.000015288}}`)
	if err := validateDecisionResponseShape(captured); err != nil {
		t.Fatalf("captured Decisions envelope rejected: %v", err)
	}
	withUnexpected := bytes.Replace(captured, []byte(`{"id":`), []byte(`{"unexpected":true,"id":`), 1)
	if err := validateDecisionResponseShape(withUnexpected); err == nil || !strings.Contains(err.Error(), `unexpected property "unexpected"`) {
		t.Fatalf("Decisions exact-key discipline weakened: %v", err)
	}
}

func TestSubmitDecisionsOnceFailureStillReturnsBillingEvidence(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("X-Request-Id", "req-429")
		w.Header().Set("Retry-After", "3")
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
	if obs.RetryAfter != "3" || obs.ResponseHeaders["Retry-After"] != "3" {
		t.Fatalf("Retry-After evidence missing: %+v", obs)
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
	body := frozenDecisionBody(t, "alias")
	sum := sha256.Sum256(body)
	obs, err := c.SubmitDecisionsOnce(context.Background(), body)
	if err == nil || calls != 0 || obs.Status != 0 || len(obs.RawResponse) != 0 || obs.RequestSHA256 != hex.EncodeToString(sum[:]) || obs.RequestSent || obs.ResponseReceived {
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

func TestOnceRejectsRedirectWithValidBodyForBothArms(t *testing.T) {
	jevBody := `{"id":"r","model":"pin","answers":{"verdict":{"type":"choice","choice":"supported","probabilities":{"supported":0.7,"insufficient-evidence":0.3}}},"usage":{"input_tokens":1,"output_tokens":1,"cost":0}}`
	chatBody := `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0}}`
	for name, tc := range map[string]struct {
		path     string
		response string
		invoke   func(string) (AttemptObservation, error)
	}{
		"jev": {
			path: "/api/alpha/decisions", response: jevBody,
			invoke: func(endpoint string) (AttemptObservation, error) {
				c, _ := NewClient(Config{Endpoint: endpoint, Model: "alias", ResponseModel: "pin", Timeout: time.Second})
				return c.SubmitDecisionsOnce(context.Background(), frozenDecisionBody(t, "alias"))
			},
		},
		"chat": {
			path: "/api/v1/chat/completions", response: chatBody,
			invoke: func(endpoint string) (AttemptObservation, error) {
				c, _ := NewChatClient(ChatConfig{Endpoint: endpoint, Model: "alias", ResponseModel: "pin", ResponseProvider: "OpenAI", Timeout: time.Second})
				return c.CompleteOnce(context.Background(), frozenChatBody(t, "alias", "OpenAI"))
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "https://example.invalid/next")
				w.WriteHeader(http.StatusTemporaryRedirect)
				_, _ = io.WriteString(w, tc.response)
			}))
			defer srv.Close()
			obs, err := tc.invoke(srv.URL + tc.path)
			if err == nil || obs.Status != http.StatusTemporaryRedirect || obs.Decision != nil || obs.Chat != nil || obs.ResponseHeaders["Location"] == "" {
				t.Fatalf("valid-looking redirect admitted: err=%v obs=%+v", err, obs)
			}
		})
	}
}

func TestSubmitDecisionsOnceRejectsAmbiguousResponseJSON(t *testing.T) {
	tests := map[string]string{
		"duplicate-choice":        `{"id":"r","model":"pin","answers":{"verdict":{"type":"choice","choice":"bad","choice":"supported"}},"usage":{"input_tokens":1,"output_tokens":1,"cost":0}}`,
		"duplicate-verdict":       `{"id":"r","model":"pin","answers":{"verdict":{"type":"choice","choice":"bad"},"verdict":{"type":"choice","choice":"supported"}},"usage":{"input_tokens":1,"output_tokens":1,"cost":0}}`,
		"duplicate-model":         `{"id":"r","model":"bad","model":"pin","answers":{"verdict":{"type":"choice","choice":"supported"}},"usage":{"input_tokens":1,"output_tokens":1,"cost":0}}`,
		"duplicate-cost":          `{"id":"r","model":"pin","answers":{"verdict":{"type":"choice","choice":"supported"}},"usage":{"input_tokens":1,"output_tokens":1,"cost":10,"cost":0}}`,
		"unexpected-answer-field": `{"id":"r","model":"pin","answers":{"verdict":{"type":"choice","choice":"supported","unexpected":true}},"usage":{"input_tokens":1,"output_tokens":1,"cost":0}}`,
	}
	for name, response := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, response) }))
			defer srv.Close()
			c, _ := NewClient(Config{Endpoint: srv.URL + "/api/alpha/decisions", Model: "alias", ResponseModel: "pin", Timeout: time.Second})
			obs, err := c.SubmitDecisionsOnce(context.Background(), frozenDecisionBody(t, "alias"))
			if err == nil || obs.Decision != nil || obs.Billing.Cost.Valid || obs.Billing.Error == "" {
				t.Fatalf("ambiguous Jev response admitted: err=%v billing=%+v decision=%+v", err, obs.Billing, obs.Decision)
			}
		})
	}
}

func TestDuplicateBillingOnHTTPErrorIsMarkedAmbiguous(t *testing.T) {
	for name, tc := range map[string]struct {
		path   string
		invoke func(string) (AttemptObservation, error)
	}{
		"jev": {
			path: "/api/alpha/decisions",
			invoke: func(endpoint string) (AttemptObservation, error) {
				c, _ := NewClient(Config{Endpoint: endpoint, Model: "alias", Timeout: time.Second})
				return c.SubmitDecisionsOnce(context.Background(), frozenDecisionBody(t, "alias"))
			},
		},
		"chat": {
			path: "/api/v1/chat/completions",
			invoke: func(endpoint string) (AttemptObservation, error) {
				c, _ := NewChatClient(ChatConfig{Endpoint: endpoint, Model: "alias", ResponseProvider: "OpenAI", Timeout: time.Second})
				return c.CompleteOnce(context.Background(), frozenChatBody(t, "alias", "OpenAI"))
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, `{"error":{},"usage":{"cost":10,"cost":0}}`)
			}))
			defer srv.Close()
			obs, err := tc.invoke(srv.URL + tc.path)
			if err == nil || obs.Billing.Cost.Valid || obs.Billing.Error == "" {
				t.Fatalf("ambiguous error billing admitted: err=%v billing=%+v", err, obs.Billing)
			}
		})
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

func TestCompleteOnceAdmitsCapturedOpenRouterNanoEnvelopeAndBillingTaxonomy(t *testing.T) {
	const capturedShape = `{"id":"gen-observed","model":"openai/gpt-4.1-nano","object":"chat.completion","created":1790139785,"choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}","refusal":null,"reasoning":null,"annotations":[]},"finish_reason":"stop","native_finish_reason":"completed","logprobs":null}],"provider":"OpenAI","system_fingerprint":null,"service_tier":"default","usage":{"prompt_tokens":63,"completion_tokens":6,"total_tokens":69,"cost":0.000008613,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000087,"upstream_inference_prompt_cost":0.0000063,"upstream_inference_completions_cost":0.0000024},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, capturedShape)
	}))
	defer srv.Close()
	c, _ := NewChatClient(ChatConfig{
		Endpoint: srv.URL + "/api/v1/chat/completions", Model: "openai/gpt-4.1-nano", ResponseModel: "openai/gpt-4.1-nano",
		ResponseProvider: "OpenAI", Timeout: time.Second,
	})
	obs, err := c.CompleteOnce(context.Background(), frozenChatBody(t, "openai/gpt-4.1-nano", "OpenAI"))
	if err != nil {
		t.Fatalf("captured response shape rejected: %v billing=%+v", err, obs.Billing)
	}
	if obs.Chat == nil || obs.Chat.Content["label"] != "supported" {
		t.Fatalf("validated label missing: %+v", obs.Chat)
	}
	for name, field := range map[string]DecimalField{
		"cost":                obs.Billing.Cost,
		"input":               obs.Billing.InputTokens,
		"output":              obs.Billing.OutputTokens,
		"total":               obs.Billing.TotalTokens,
		"cached":              obs.Billing.PromptTokenDetails.CachedTokens,
		"cache-write":         obs.Billing.PromptTokenDetails.CacheWriteTokens,
		"prompt-audio":        obs.Billing.PromptTokenDetails.AudioTokens,
		"prompt-video":        obs.Billing.PromptTokenDetails.VideoTokens,
		"reasoning":           obs.Billing.CompletionTokenDetails.ReasoningTokens,
		"image":               obs.Billing.CompletionTokenDetails.ImageTokens,
		"completion-audio":    obs.Billing.CompletionTokenDetails.AudioTokens,
		"accepted-prediction": obs.Billing.CompletionTokenDetails.AcceptedPredictionTokens,
		"rejected-prediction": obs.Billing.CompletionTokenDetails.RejectedPredictionTokens,
		"upstream":            obs.Billing.CostDetails.UpstreamInferenceCost,
		"upstream-prompt":     obs.Billing.CostDetails.UpstreamInferencePromptCost,
		"upstream-completion": obs.Billing.CostDetails.UpstreamInferenceCompletionsCost,
	} {
		if !field.Present || !field.Valid {
			t.Errorf("%s was not mapped as valid presence-aware evidence: %+v", name, field)
		}
	}
	if obs.Billing.Cost.Raw != "0.000008613" || obs.Billing.InputTokens.Raw != "63" || obs.Billing.OutputTokens.Raw != "6" || obs.Billing.TotalTokens.Raw != "69" {
		t.Fatalf("primary billing lexemes changed: %+v", obs.Billing)
	}
	if obs.Billing.CompletionTokenDetails.AcceptedPredictionTokens.Raw != "0" || obs.Billing.CompletionTokenDetails.RejectedPredictionTokens.Raw != "0" {
		t.Fatalf("prediction-token lexemes changed: %+v", obs.Billing.CompletionTokenDetails)
	}
	if !obs.Billing.IsBYOK.Present || !obs.Billing.IsBYOK.Valid || obs.Billing.IsBYOK.Value {
		t.Fatalf("is_byok taxonomy missing: %+v", obs.Billing.IsBYOK)
	}
	if !strings.Contains(obs.Billing.RateSemanticsDiscrepancy, "0.000008613") || !strings.Contains(obs.Billing.RateSemanticsDiscrepancy, "0.0000087") {
		t.Fatalf("rate-semantics discrepancy not reported: %q", obs.Billing.RateSemanticsDiscrepancy)
	}
}

func TestCompleteOnceObjectPresenceIsStrict(t *testing.T) {
	const base = `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{"cost":0}}`
	tests := []struct {
		name       string
		objectJSON string
		wantError  bool
	}{
		{name: "absent"},
		{name: "exact", objectJSON: `"chat.completion"`},
		{name: "empty", objectJSON: `""`, wantError: true},
		{name: "null", objectJSON: `null`, wantError: true},
		{name: "plural", objectJSON: `"chat.completions"`, wantError: true},
		{name: "non-string", objectJSON: `1`, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := base
			if test.objectJSON != "" {
				response = strings.Replace(base, `"model":"pin",`, `"model":"pin","object":`+test.objectJSON+`,`, 1)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, response)
			}))
			defer srv.Close()
			c, _ := NewChatClient(ChatConfig{
				Endpoint: srv.URL + "/api/v1/chat/completions", Model: "alias", ResponseModel: "pin",
				ResponseProvider: "OpenAI", Timeout: time.Second,
			})
			obs, err := c.CompleteOnce(context.Background(), frozenChatBody(t, "alias", "OpenAI"))
			if test.wantError {
				if err == nil || obs.Chat != nil || obs.Billing.Cost.Valid || obs.Billing.Error == "" {
					t.Fatalf("invalid present object admitted or billing left valid: err=%v obs=%+v", err, obs)
				}
				return
			}
			if err != nil || obs.Chat == nil || !obs.Billing.Cost.Valid {
				t.Fatalf("compatible object form rejected: err=%v obs=%+v", err, obs)
			}
		})
	}
}

func TestCompleteOnceAdmitsClosedURLCitationAnnotation(t *testing.T) {
	response := `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}","annotations":[{"type":"url_citation","url_citation":{"end_index":8,"start_index":0,"title":"Source","url":"https://example.test/source"}}]},"finish_reason":"stop"}],"usage":{"cost":0}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, response)
	}))
	defer srv.Close()
	c, _ := NewChatClient(ChatConfig{
		Endpoint: srv.URL + "/api/v1/chat/completions", Model: "alias", ResponseModel: "pin",
		ResponseProvider: "OpenAI", Timeout: time.Second,
	})
	obs, err := c.CompleteOnce(context.Background(), frozenChatBody(t, "alias", "OpenAI"))
	if err != nil || obs.Chat == nil || !obs.Billing.Cost.Valid {
		t.Fatalf("documented url_citation annotation rejected: err=%v obs=%+v", err, obs)
	}
}

func TestCompleteOnceRejectsStrictFailureMatrix(t *testing.T) {
	tests := map[string]string{
		"duplicate-label":         `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\",\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"extra-key":               `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\",\"why\":\"x\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"wrong-enum":              `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"other\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"refusal":                 `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}","refusal":"no"},"finish_reason":"stop"}],"usage":{}}`,
		"truncated":               `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"length"}],"usage":{}}`,
		"multiple":                `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"},{"index":1,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"wrong-model":             `{"id":"c","model":"other","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"missing-provider":        `{"id":"c","model":"pin","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"wrong-provider":          `{"id":"c","model":"pin","provider":"Other","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"trailing-json":           `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}{}"},"finish_reason":"stop"}],"usage":{}}`,
		"uppercase-label":         `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"LABEL\":\"supported\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"mixed-case-label":        `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"insufficient-evidence\",\"LABEL\":\"supported\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"duplicate-envelope":      `{"id":"c","id":"other","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"trailing-envelope":       `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{}} {}`,
		"unexpected-envelope":     `{"id":"c","model":"pin","provider":"OpenAI","unexpected":true,"choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{}}`,
		"unexpected-usage-detail": `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens_details":{"cached_tokens":0,"unexpected":0}}}`,
		"unexpected-message":      `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}","unexpected":true},"finish_reason":"stop"}],"usage":{}}`,
		"unexpected-completion":   `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}"},"finish_reason":"stop"}],"usage":{"completion_tokens_details":{"reasoning_tokens":0,"unexpected":0}}}`,
		"unexpected-annotation":   `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}","annotations":[{"type":"url_citation","url_citation":{"end_index":1,"start_index":0,"title":"t","url":"https://example.test"},"unexpected":true}]},"finish_reason":"stop"}],"usage":{}}`,
		"unexpected-citation":     `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}","annotations":[{"type":"url_citation","url_citation":{"end_index":1,"start_index":0,"title":"t","url":"https://example.test","unexpected":true}}]},"finish_reason":"stop"}],"usage":{}}`,
		"wrong-annotation-type":   `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}","annotations":[{"type":"other","url_citation":{"end_index":1,"start_index":0,"title":"t","url":"https://example.test"}}]},"finish_reason":"stop"}],"usage":{}}`,
		"null-annotations":        `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\"}","annotations":null},"finish_reason":"stop"}],"usage":{}}`,
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

func TestSchemaRejectedChatDoesNotExposeValidBilling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"c","model":"pin","provider":"OpenAI","choices":[{"index":0,"message":{"role":"assistant","content":"{\"label\":\"supported\",\"extra\":true}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0}}`)
	}))
	defer srv.Close()
	c, _ := NewChatClient(ChatConfig{Endpoint: srv.URL + "/api/v1/chat/completions", Model: "alias", ResponseModel: "pin", ResponseProvider: "OpenAI", Timeout: time.Second})
	obs, err := c.CompleteOnce(context.Background(), frozenChatBody(t, "alias", "OpenAI"))
	if err == nil || obs.Chat != nil || obs.Billing.Cost.Valid || obs.Billing.Cost.Error == "" || obs.Billing.Error == "" {
		t.Fatalf("schema-rejected billing still appears valid: err=%v billing=%+v", err, obs.Billing)
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

func TestDecimalFromRejectsNonNumericAndOutOfRangeValues(t *testing.T) {
	for name, raw := range map[string]string{
		"quoted-zero":         `"0"`,
		"quoted-decimal":      `"0.004"`,
		"negative-underflow":  `-1e-999`,
		"accounting-overflow": `1e309`,
	} {
		t.Run(name, func(t *testing.T) {
			field := decimalFrom(json.RawMessage(raw), false)
			if field.Valid || field.Error == "" {
				t.Fatalf("invalid decimal admitted: raw=%s field=%+v", raw, field)
			}
		})
	}
	if field := decimalFrom(json.RawMessage(`1e-999`), false); !field.Valid || field.Number.String() != "1e-999" {
		t.Fatalf("exact tiny positive decimal should remain valid: %+v", field)
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

func TestOnceRejectsQuotedAndNegativeUnderflowCost(t *testing.T) {
	for name, cost := range map[string]string{
		"quoted":             `"0.5"`,
		"negative-underflow": `-1e-999`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, fmt.Sprintf(`{"id":"r","model":"pin","answers":{"verdict":{"type":"choice","choice":"supported"}},"usage":{"input_tokens":1,"output_tokens":1,"cost":%s}}`, cost))
			}))
			defer srv.Close()
			c, _ := NewClient(Config{Endpoint: srv.URL + "/api/alpha/decisions", Model: "alias", ResponseModel: "pin", Timeout: time.Second})
			obs, err := c.SubmitDecisionsOnce(context.Background(), frozenDecisionBody(t, "alias"))
			var te *TransportError
			if !errors.As(err, &te) || te.Code != "billing" || obs.Billing.Cost.Valid || obs.Billing.Cost.Error == "" || obs.Billing.Error == "" {
				t.Fatalf("invalid exact cost admitted: cost=%s err=%v billing=%+v", cost, err, obs.Billing)
			}
		})
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
	if err == nil || obs.ReadError == "" || len(obs.RawResponse) == 0 || obs.Billing.Cost.Valid || obs.Billing.Error == "" {
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
