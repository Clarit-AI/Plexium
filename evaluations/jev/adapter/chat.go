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
	MaxTokens      *int            `json:"max_tokens,omitempty"`
	Provider       *ProviderRoute  `json:"provider,omitempty"`
}

// ProviderRoute is the OpenRouter routing envelope required by the frozen
// Nano request. A preference is not proof of the serving provider;
// CompleteOnce separately validates response-linked provider identity.
type ProviderRoute struct {
	Only              []string `json:"only"`
	AllowFallbacks    *bool    `json:"allow_fallbacks"`
	RequireParameters *bool    `json:"require_parameters"`
}

// ChatMessage is a single role-tagged chat turn.
type ChatMessage struct {
	Role    string  `json:"role"`
	Content string  `json:"content"`
	Refusal *string `json:"refusal,omitempty"`
}

// ResponseFormat declares the JSON Schema the response must conform to.
type ResponseFormat struct {
	Type       string      `json:"type"` // "json_schema"
	JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

// JSONSchema is the JSON Schema envelope used by /api/v1/chat/completions.
type JSONSchema struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict,omitempty"`
	Schema map[string]any `json:"schema"`
}

// ChatChoice is one of the choices returned in a successful chat response.
type ChatChoice struct {
	Index              int                 `json:"index"`
	Message            ChatResponseMessage `json:"message"`
	FinishReason       string              `json:"finish_reason"`
	NativeFinishReason *string             `json:"native_finish_reason,omitempty"`
	Logprobs           json.RawMessage     `json:"logprobs,omitempty"`
}

// ChatResponseMessage is kept separate from ChatMessage so response-only
// routing evidence cannot become accepted request input.
type ChatResponseMessage struct {
	Role        string           `json:"role"`
	Content     string           `json:"content"`
	Refusal     *string          `json:"refusal,omitempty"`
	Reasoning   *string          `json:"reasoning,omitempty"`
	Annotations []ChatAnnotation `json:"annotations,omitempty"`
}

// ChatAnnotation is the only annotation variant admitted by the supported
// OpenAI chat-completion profile. Tool calls, audio, and other response
// capabilities are intentionally outside the frozen Nano request profile.
type ChatAnnotation struct {
	Type        string          `json:"type"`
	URLCitation ChatURLCitation `json:"url_citation"`
}

// ChatURLCitation is the documented closed shape of a url_citation annotation.
type ChatURLCitation struct {
	EndIndex   int    `json:"end_index"`
	StartIndex int    `json:"start_index"`
	Title      string `json:"title"`
	URL        string `json:"url"`
}

type ChatPromptTokenDetails struct {
	CachedTokens     int `json:"cached_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	AudioTokens      int `json:"audio_tokens,omitempty"`
	VideoTokens      int `json:"video_tokens,omitempty"`
}

type ChatCompletionTokenDetails struct {
	ReasoningTokens          int `json:"reasoning_tokens,omitempty"`
	ImageTokens              int `json:"image_tokens,omitempty"`
	AudioTokens              int `json:"audio_tokens,omitempty"`
	AcceptedPredictionTokens int `json:"accepted_prediction_tokens,omitempty"`
	RejectedPredictionTokens int `json:"rejected_prediction_tokens,omitempty"`
}

type ChatCostDetails struct {
	UpstreamInferenceCost            json.RawMessage `json:"upstream_inference_cost,omitempty"`
	UpstreamInferencePromptCost      json.RawMessage `json:"upstream_inference_prompt_cost,omitempty"`
	UpstreamInferenceCompletionsCost json.RawMessage `json:"upstream_inference_completions_cost,omitempty"`
}

// ChatUsage mirrors the documented usage block.
type ChatUsage struct {
	PromptTokens            int                         `json:"prompt_tokens"`
	PromptTokensDetails     *ChatPromptTokenDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokens        int                         `json:"completion_tokens"`
	CompletionTokensDetails *ChatCompletionTokenDetails `json:"completion_tokens_details,omitempty"`
	TotalTokens             int                         `json:"total_tokens"`
	Cost                    json.RawMessage             `json:"cost,omitempty"`
	CostDetails             *ChatCostDetails            `json:"cost_details,omitempty"`
	IsBYOK                  *bool                       `json:"is_byok,omitempty"`
}

// ChatResponse is the parsed body of /api/v1/chat/completions. The admitted
// response profile is a deliberately closed subset of the documented OpenAI
// Chat Completions response and OpenRouter's compatible envelope; see README.md
// for the exact key sets and primary references.
type ChatResponse struct {
	ID                string       `json:"id"`
	Object            string       `json:"object,omitempty"`
	Created           int64        `json:"created,omitempty"`
	Model             string       `json:"model"`
	Provider          string       `json:"provider,omitempty"`
	Choices           []ChatChoice `json:"choices"`
	Usage             ChatUsage    `json:"usage"`
	SystemFingerprint *string      `json:"system_fingerprint,omitempty"`
	ServiceTier       *string      `json:"service_tier,omitempty"`
}

// ChatObservation is the harness-visible artifact returned by the chat
// adapter. Content is the validated JSON object the model emitted, ready for
// scoring.
//
// Latency is the final attempt's duration; per-attempt durations and total
// wall time (including backoff and body reads) live in AttemptLatencies /
// TotalLatency. The harness NEVER relabels these as provider cold/warm —
// cold/warm attribution is not derivable from a single adapter run.
type ChatObservation struct {
	ResolvedModel    string
	Content          map[string]any
	Usage            ChatUsage
	Latency          time.Duration
	AttemptLatencies []time.Duration
	TotalLatency     time.Duration
	Attempts         int
	Raw              json.RawMessage
}

// ChatConfig configures the chat adapter. It mirrors Config but uses a
// dedicated type so callers cannot accidentally reuse a Decisions config.
type ChatConfig struct {
	Endpoint         string
	APIKey           string
	Model            string // request alias
	ResponseModel    string // exact accepted response pin; defaults to Model for compatibility
	ResponseProvider string // exact provider identity required by CompleteOnce when configured
	Timeout          time.Duration
	Retry            RetryPolicy
	HTTPClient       *http.Client
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
	if cfg.ResponseModel == "" {
		cfg.ResponseModel = cfg.Model
	}
	return &ChatClient{cfg: cfg}, nil
}

// Complete submits one chat completion request and returns the validated
// response. The first choice's message.content is parsed as JSON. The model's
// response MUST match the pinned model in cfg.Model or the call fails with
// model-pin.
//
// All successful observations carry per-attempt durations and total wall
// time including backoff and body reads. The harness does NOT relabel any
// of these as provider cold/warm.
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
		attempt          int
		lastLatency      time.Duration
		respBody         []byte
		status           int
		retryable        bool
		transErr         *TransportError
		attemptLatencies []time.Duration
		totalStart       = time.Now()
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
			attemptLatencies = append(attemptLatencies, lastLatency)
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
		attDur := time.Since(start)
		attemptLatencies = append(attemptLatencies, attDur)
		status = resp.StatusCode
		respBody = rawBody
		if len(rawBody) > MaxResponseBytes {
			return nil, &TransportError{Status: status, Code: "payload", Message: fmt.Sprintf("chat response exceeded %d bytes", MaxResponseBytes), Latency: attDur, Attempts: attempt}
		}
		if isRetryableStatus(status, c.cfg.Retry.RetryOnStatus) && attempt <= c.cfg.Retry.MaxRetries {
			wait := retryAfter(resp, c.cfg.Retry.RetryAfterMax, c.cfg.Retry.BaseBackoff, c.cfg.Retry.MaxBackoff)
			transErr = &TransportError{Status: status, Code: codeForStatus(status), Message: resp.Status, Latency: attDur, Attempts: attempt}
			retryable = true
			if err := sleepCtx(ctx, wait, c.cfg.Retry.RetryAfterMax); err != nil {
				return nil, &TransportError{Code: "cancelled", Message: err.Error(), Cause: err, Attempts: attempt}
			}
			continue
		}
		retryable = false
		lastLatency = attDur
		break
	}
	if retryable {
		return nil, transErr
	}
	if status >= 400 {
		return nil, transErrFromStatus(status, respBody, lastLatency, attempt)
	}
	obs, err := parseChatResponse(respBody, c.cfg.ResponseModel, lastLatency, attempt)
	if err != nil {
		return nil, err
	}
	obs.AttemptLatencies = attemptLatencies
	obs.TotalLatency = time.Since(totalStart)
	return obs, nil
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
