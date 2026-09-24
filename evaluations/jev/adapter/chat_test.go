package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChatHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"response_format"`) {
			t.Errorf("expected response_format in chat request")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "c1", "model": "chat-model",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": `{"label":"supported","confidence":0.7}`,
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120,
			},
		})
	}))
	defer srv.Close()
	c, err := NewChatClient(ChatConfig{Endpoint: srv.URL, Model: "chat-model", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	obs, err := c.Complete(context.Background(), ChatRequest{
		Model: "chat-model",
		Messages: []ChatMessage{
			{Role: "system", Content: "be terse"},
			{Role: "user", Content: "answer"},
		},
		ResponseFormat: &ResponseFormat{Type: "json_schema", JSONSchema: &JSONSchema{
			Name: "verdict",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"label":      map[string]any{"type": "string"},
					"confidence": map[string]any{"type": "number"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if obs.Content["label"] != "supported" {
		t.Fatalf("expected label=supported, got %v", obs.Content)
	}
	if obs.ResolvedModel != "chat-model" {
		t.Fatalf("expected model chat-model, got %s", obs.ResolvedModel)
	}
}

func TestChatModelPinRejectsMismatchRequest(t *testing.T) {
	c, _ := NewChatClient(ChatConfig{Endpoint: "http://localhost", Model: "pinned", Timeout: time.Second})
	_, err := c.Complete(context.Background(), ChatRequest{Model: "other", Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "model-pin" {
		t.Fatalf("expected model-pin, got %v", err)
	}
}

func TestChatModelPinRejectsMismatchResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "c1", "model": "unexpected",
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "{}"}, "finish_reason": "stop"}},
			"usage":   map[string]any{},
		})
	}))
	defer srv.Close()
	c, _ := NewChatClient(ChatConfig{Endpoint: srv.URL, Model: "expected", Timeout: 2 * time.Second})
	_, err := c.Complete(context.Background(), ChatRequest{
		Model:    "expected",
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "model-pin" {
		t.Fatalf("expected model-pin, got %v", err)
	}
}

func TestChatRejectsNonJSONContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "c1", "model": "m",
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "not json"}, "finish_reason": "stop"}},
			"usage":   map[string]any{},
		})
	}))
	defer srv.Close()
	c, _ := NewChatClient(ChatConfig{Endpoint: srv.URL, Model: "m", Timeout: 2 * time.Second})
	_, err := c.Complete(context.Background(), ChatRequest{Model: "m", Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "schema" {
		t.Fatalf("expected schema, got %v", err)
	}
}

func TestChatRejectsEmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "c1", "model": "m", "choices": []any{}, "usage": map[string]any{}})
	}))
	defer srv.Close()
	c, _ := NewChatClient(ChatConfig{Endpoint: srv.URL, Model: "m", Timeout: 2 * time.Second})
	_, err := c.Complete(context.Background(), ChatRequest{Model: "m", Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "schema" {
		t.Fatalf("expected schema, got %v", err)
	}
}

func TestChatRejectsNaNValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// NaN is not representable in standard JSON; send an out-of-range
		// exponent that deserializes to +Inf instead, which is also invalid.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"{\"x\":1e500}"},"finish_reason":"stop"}],"usage":{}}`))
	}))
	defer srv.Close()
	c, _ := NewChatClient(ChatConfig{Endpoint: srv.URL, Model: "m", Timeout: 2 * time.Second})
	_, err := c.Complete(context.Background(), ChatRequest{Model: "m", Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "schema" {
		t.Fatalf("expected schema, got %v", err)
	}
}

func TestChat401NoRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauth", http.StatusUnauthorized)
	}))
	defer srv.Close()
	c, _ := NewChatClient(ChatConfig{Endpoint: srv.URL, Model: "m", Timeout: 2 * time.Second})
	_, err := c.Complete(context.Background(), ChatRequest{Model: "m", Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "auth" {
		t.Fatalf("expected auth, got %v", err)
	}
}

func TestChatRequiresMessages(t *testing.T) {
	c, _ := NewChatClient(ChatConfig{Endpoint: "http://localhost", Model: "m", Timeout: time.Second})
	_, err := c.Complete(context.Background(), ChatRequest{Model: "m"})
	var te *TransportError
	if !errors.As(err, &te) || te.Code != "schema" {
		t.Fatalf("expected schema, got %v", err)
	}
}
