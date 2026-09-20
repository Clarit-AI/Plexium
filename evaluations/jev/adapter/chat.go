package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// ChatRequest is the structured-output chat payload posted to
// /api/v1/chat/completions. The schema is the documented shape used by the
// protocol's chat comparator.
type ChatRequest struct {
	Model          string          `json:"model"`
	Messages       []ChatMessage   `json:"messages"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	Temperature    *float64        `json:"temperature,omitempty"`
}

// ChatMessage is a single role-tagged chat turn.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ResponseFormat declares the JSON Schema the response must conform to.
type ResponseFormat struct {
	Type       string      `json:"type"` // "json_schema"
	JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

// JSONSchema is the JSON Schema envelope used by /api/v1/chat/completions.
type JSONSchema struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
}

// ChatChoice is one of the choices returned in a successful chat response.
type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// ChatUsage mirrors the documented usage block.
type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatResponse is the parsed body of /api/v1/chat/completions.
type ChatResponse struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   ChatUsage    `json:"usage"`
}

// ChatObservation is the harness-visible artifact returned by the chat
// adapter. Content is the validated JSON object the model emitted, ready for
// scoring.
type ChatObservation struct {
	ResolvedModel string
	Content       map[string]any
	Usage         ChatUsage
	Latency       time.Duration
	Attempts      int
	Raw           json.RawMessage
}

// ChatConfig configures the chat adapter. It mirrors Config but uses a
// dedicated type so callers cannot accidentally reuse a Decisions config.
type ChatConfig struct {
	Endpoint   string
	APIKey     string
	Model      string
	Timeout    time.Duration
	Retry      RetryPolicy
	HTTPClient *http.Client
}

// ChatClient is the structured-output chat adapter. It has no global state.
type ChatClient struct {
	cfg ChatConfig
}

// NewChatClient validates the supplied config and returns a ChatClient.
func NewChatClient(cfg ChatConfig) (*ChatClient, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("adapter: chat endpoint required")
	}
	if cfg.Model == "" {
		return nil, errors.New("adapter: chat model pin required")
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("adapter: chat timeout must be > 0")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	} else {
		cfg.HTTPClient.Timeout = cfg.Timeout
	}
	if cfg.Retry.MaxRetries == 0 && cfg.Retry.BaseBackoff == 0 {
		cfg.Retry = DefaultRetryPolicy()
	}
	return &ChatClient{cfg: cfg}, nil
}

// Complete submits one chat completion request and returns the validated
// response. The first choice's message.content is parsed as JSON. The model's
// response MUST match the pinned model in cfg.Model or the call fails with
// model-pin.
func (c *ChatClient) Complete(ctx context.Context, req ChatRequest) (*ChatObservation, error) {
	if req.Model != c.cfg.Model {
		return nil, &TransportError{
			Code:    "model-pin",
			Message: fmt.Sprintf("chat request model %q does not match pinned model %q", req.Model, c.cfg.Model),
		}
	}
	if req.Model == "" {
		return nil, &TransportError{Code: "schema", Message: "model is required"}
	}
	if len(req.Messages) == 0 {
		return nil, &TransportError{Code: "schema", Message: "messages must contain at least one entry"}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, &TransportError{Code: "payload", Message: "marshal chat request: " + err.Error(), Cause: err}
	}
	var (
		attempt     int
		lastLatency time.Duration
		respBody    []byte
		status      int
		retryable   bool
		transErr    *TransportError
	)
	for {
		attempt++
		start := time.Now()
		reqCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
		httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.cfg.Endpoint, bytes.NewReader(body))
		if err != nil {
			cancel()
			return nil, &TransportError{Code: "payload", Message: "build chat request: " + err.Error(), Cause: err, Attempts: attempt}
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")
		if c.cfg.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		}
		resp, err := c.cfg.HTTPClient.Do(httpReq)
		lastLatency = time.Since(start)
		if err != nil {
			cancel()
			transErr = &TransportError{Code: classifyTransport(err), Message: err.Error(), Cause: err, Latency: lastLatency, Attempts: attempt}
			retryable = transErr.Retryable() && attempt <= c.cfg.Retry.MaxRetries && ctx.Err() == nil
			if !retryable {
				return nil, transErr
			}
			if err := sleepCtx(ctx, c.cfg.Retry.BaseBackoff, c.cfg.Retry.RetryAfterMax); err != nil {
				return nil, &TransportError{Code: "cancelled", Message: err.Error(), Cause: err, Attempts: attempt}
			}
			continue
		}
		limited := io.LimitReader(resp.Body, MaxResponseBytes+1)
		rawBody, err := io.ReadAll(limited)
		cancel()
		_ = resp.Body.Close()
		status = resp.StatusCode
		respBody = rawBody
		if len(rawBody) > MaxResponseBytes {
			return nil, &TransportError{Status: status, Code: "payload", Message: fmt.Sprintf("chat response exceeded %d bytes", MaxResponseBytes), Latency: lastLatency, Attempts: attempt}
		}
		if isRetryableStatus(status, c.cfg.Retry.RetryOnStatus) && attempt <= c.cfg.Retry.MaxRetries {
			wait := retryAfter(resp, c.cfg.Retry.RetryAfterMax, c.cfg.Retry.BaseBackoff, c.cfg.Retry.MaxBackoff)
			transErr = &TransportError{Status: status, Code: codeForStatus(status), Message: resp.Status, Latency: lastLatency, Attempts: attempt}
			retryable = true
			if err := sleepCtx(ctx, wait, c.cfg.Retry.RetryAfterMax); err != nil {
				return nil, &TransportError{Code: "cancelled", Message: err.Error(), Cause: err, Attempts: attempt}
			}
			continue
		}
		retryable = false
		break
	}
	if retryable {
		return nil, transErr
	}
	if status >= 400 {
		return nil, transErrFromStatus(status, respBody, lastLatency, attempt)
	}
	return parseChatResponse(respBody, c.cfg.Model, lastLatency, attempt)
}

func parseChatResponse(body []byte, expectedModel string, latency time.Duration, attempts int) (*ChatObservation, error) {
	if len(body) == 0 {
		return nil, &TransportError{Code: "schema", Message: "empty chat response body", Latency: latency, Attempts: attempts}
	}
	var resp ChatResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, &TransportError{Code: "schema", Message: "decode chat response: " + err.Error(), Cause: err, Latency: latency, Attempts: attempts}
	}
	if resp.Model != expectedModel {
		return nil, &TransportError{
			Code:     "model-pin",
			Message:  fmt.Sprintf("chat response model %q does not match pinned model %q", resp.Model, expectedModel),
			Latency:  latency,
			Attempts: attempts,
		}
	}
	if len(resp.Choices) == 0 {
		return nil, &TransportError{Code: "schema", Message: "chat response has no choices", Latency: latency, Attempts: attempts}
	}
	content := resp.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" {
		return nil, &TransportError{Code: "schema", Message: "chat response first choice has empty content", Latency: latency, Attempts: attempts}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, &TransportError{Code: "schema", Message: "chat response content is not a JSON object: " + err.Error(), Cause: err, Latency: latency, Attempts: attempts}
	}
	for k, v := range parsed {
		if f, ok := v.(float64); ok && (math.IsNaN(f) || math.IsInf(f, 0)) {
			return nil, &TransportError{Code: "schema", Message: fmt.Sprintf("chat response field %q has non-finite numeric value", k), Latency: latency, Attempts: attempts}
		}
	}
	return &ChatObservation{
		ResolvedModel: resp.Model,
		Content:       parsed,
		Usage:         resp.Usage,
		Latency:       latency,
		Attempts:      attempts,
		Raw:           append(json.RawMessage(nil), body...),
	}, nil
}
