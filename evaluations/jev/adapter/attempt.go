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
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// DecimalField preserves the distinction between an omitted, explicit null,
// numeric zero, positive decimal, and malformed billing/usage field. Raw is
// retained so callers can perform exact decimal accounting without a binary
// floating-point round trip.
type DecimalField struct {
	Present bool
	Null    bool
	Valid   bool
	Raw     string
	Number  json.Number
	Error   string
}

// BillingObservation is extracted before semantic response validation. The
// adapter does not infer missing values from configured rates.
type BillingObservation struct {
	Cost         DecimalField
	InputTokens  DecimalField
	OutputTokens DecimalField
	TotalTokens  DecimalField
}

// AttemptObservation is the durable evidence returned by each one-shot call,
// including failure paths. RawResponse is bounded and never included in error
// strings. Callers persist it privately (0600) if they need durable evidence.
type AttemptObservation struct {
	Status           int
	RequestID        string
	ResponseModel    string
	ResponseProvider string
	ResponseHeaders  map[string]string
	RequestSHA256    string
	RawResponse      []byte
	RawSHA256        string
	RawTruncated     bool
	ReadError        string
	StartedAt        time.Time
	EndedAt          time.Time
	Duration         time.Duration
	Billing          BillingObservation
	Decision         *Decision
	Chat             *ChatObservation
}

// ValidateDecisionFrozenBody validates immutable Decisions wire bytes before
// reservation/send. It requires the single canonical verdict choice question.
func ValidateDecisionFrozenBody(body []byte, requestModel string) (DecisionRequest, error) {
	var req DecisionRequest
	if err := decodeStrictSingle(body, &req); err != nil {
		return req, fmt.Errorf("decode frozen Decisions request: %w", err)
	}
	if err := validateDecisionRequest(req); err != nil {
		return req, err
	}
	if req.Model != requestModel {
		return req, fmt.Errorf("request model %q does not match configured alias %q", req.Model, requestModel)
	}
	if len(req.Questions) != 1 {
		return req, fmt.Errorf("questions must contain exactly %q", DecisionVerdictID)
	}
	q, ok := req.Questions[DecisionVerdictID]
	if !ok {
		return req, fmt.Errorf("questions missing exact id %q", DecisionVerdictID)
	}
	if q.Type != "choice" {
		return req, fmt.Errorf("question %q type must be choice", DecisionVerdictID)
	}
	return req, nil
}

// SubmitDecisionsOnce performs exactly one POST with already-frozen bytes. It
// never retries, redirects, sleeps, repairs, or changes the supplied payload.
func (c *Client) SubmitDecisionsOnce(ctx context.Context, frozenBody []byte) (AttemptObservation, error) {
	if err := validateEndpoint(c.cfg.Endpoint, "/api/alpha/decisions"); err != nil {
		return failedLocalAttempt(err)
	}
	req, err := ValidateDecisionFrozenBody(frozenBody, c.cfg.Model)
	if err != nil {
		return failedLocalAttempt(err)
	}
	return performOnce(ctx, onceConfig{
		endpoint: c.cfg.Endpoint, apiKey: c.cfg.APIKey, timeout: c.cfg.Timeout,
		httpClient: c.cfg.HTTPClient,
	}, frozenBody, func(obs *AttemptObservation) error {
		if obs.Status >= 400 {
			return &TransportError{Status: obs.Status, Code: codeForStatus(obs.Status), Message: fmt.Sprintf("HTTP %d", obs.Status), Latency: obs.Duration, Attempts: 1}
		}
		dec, err := parseDecisionResponse(obs.RawResponse, c.cfg.ResponseModel, obs.Duration, 1)
		if err != nil {
			return err
		}
		var response DecisionResponse
		if err := json.Unmarshal(obs.RawResponse, &response); err != nil || len(response.Answers) != 1 {
			return &TransportError{Code: "schema", Message: "response must contain exactly the verdict answer", Latency: obs.Duration, Attempts: 1}
		}
		answer := req.Questions[DecisionVerdictID]
		if _, ok := answer.Criteria[dec.Choice]; !ok {
			return &TransportError{Code: "schema", Message: fmt.Sprintf("answer choice %q is outside request vocabulary", dec.Choice), Latency: obs.Duration, Attempts: 1}
		}
		if dec.Probabilities != nil && !sameKeys(dec.Probabilities, answer.Criteria) {
			return &TransportError{Code: "schema", Message: "probability keys do not exactly match request vocabulary", Latency: obs.Duration, Attempts: 1}
		}
		if c.cfg.ResponseProvider != "" {
			if obs.ResponseProvider == "" || obs.ResponseProvider != c.cfg.ResponseProvider {
				return &TransportError{Code: "provider-pin", Message: fmt.Sprintf("response provider %q does not match accepted provider %q", obs.ResponseProvider, c.cfg.ResponseProvider), Latency: obs.Duration, Attempts: 1}
			}
		}
		obs.Decision = dec
		return nil
	})
}

// ValidateChatFrozenBody validates the strict Nano request envelope and
// returns its exact label vocabulary. Response provider identity remains a
// separate admission check; request routing alone is not proof of service.
func ValidateChatFrozenBody(body []byte, requestModel, requiredProvider string) (ChatRequest, []string, error) {
	var req ChatRequest
	if err := decodeStrictSingle(body, &req); err != nil {
		return req, nil, fmt.Errorf("decode frozen chat request: %w", err)
	}
	if req.Model != requestModel {
		return req, nil, fmt.Errorf("request model %q does not match configured alias %q", req.Model, requestModel)
	}
	if len(req.Messages) == 0 {
		return req, nil, errors.New("messages must contain at least one entry")
	}
	for i, msg := range req.Messages {
		if msg.Role == "" || msg.Content == "" || msg.Refusal != nil {
			return req, nil, fmt.Errorf("message %d must have role/content and no refusal", i)
		}
	}
	if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_schema" || req.ResponseFormat.JSONSchema == nil || !req.ResponseFormat.JSONSchema.Strict {
		return req, nil, errors.New("response_format must be strict json_schema")
	}
	labels, err := strictLabelVocabulary(req.ResponseFormat.JSONSchema.Schema)
	if err != nil {
		return req, nil, err
	}
	if req.MaxTokens == nil || *req.MaxTokens != 256 {
		return req, nil, errors.New("max_tokens must be explicitly frozen to 256")
	}
	if requiredProvider == "" {
		return req, nil, errors.New("response provider contract is unresolved")
	}
	if req.Provider == nil || len(req.Provider.Only) != 1 || req.Provider.Only[0] != requiredProvider || req.Provider.AllowFallbacks == nil || *req.Provider.AllowFallbacks || req.Provider.RequireParameters == nil || !*req.Provider.RequireParameters {
		return req, nil, errors.New("provider routing must pin one provider, disable fallbacks, and require parameters")
	}
	return req, labels, nil
}

// CompleteOnce performs exactly one strict structured-output chat POST.
func (c *ChatClient) CompleteOnce(ctx context.Context, frozenBody []byte) (AttemptObservation, error) {
	if err := validateEndpoint(c.cfg.Endpoint, "/api/v1/chat/completions"); err != nil {
		return failedLocalAttempt(err)
	}
	_, labels, err := ValidateChatFrozenBody(frozenBody, c.cfg.Model, c.cfg.ResponseProvider)
	if err != nil {
		return failedLocalAttempt(err)
	}
	return performOnce(ctx, onceConfig{
		endpoint: c.cfg.Endpoint, apiKey: c.cfg.APIKey, timeout: c.cfg.Timeout,
		httpClient: c.cfg.HTTPClient,
	}, frozenBody, func(obs *AttemptObservation) error {
		if obs.Status >= 400 {
			return &TransportError{Status: obs.Status, Code: codeForStatus(obs.Status), Message: fmt.Sprintf("HTTP %d", obs.Status), Latency: obs.Duration, Attempts: 1}
		}
		chat, err := parseStrictChatResponse(obs.RawResponse, c.cfg.ResponseModel, c.cfg.ResponseProvider, labels, obs.Duration)
		if err != nil {
			return err
		}
		obs.Chat = chat
		return nil
	})
}

type onceConfig struct {
	endpoint   string
	apiKey     string
	timeout    time.Duration
	httpClient *http.Client
}

func performOnce(ctx context.Context, cfg onceConfig, frozenBody []byte, validate func(*AttemptObservation) error) (AttemptObservation, error) {
	obs := AttemptObservation{StartedAt: time.Now().UTC()}
	requestSum := sha256.Sum256(frozenBody)
	obs.RequestSHA256 = hex.EncodeToString(requestSum[:])
	finish := func() {
		obs.EndedAt = time.Now().UTC()
		obs.Duration = obs.EndedAt.Sub(obs.StartedAt)
	}
	reqCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, cfg.endpoint, bytes.NewReader(frozenBody))
	if err != nil {
		finish()
		return obs, &TransportError{Code: "payload", Message: "build request: " + err.Error(), Cause: err, Latency: obs.Duration, Attempts: 1}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if cfg.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.apiKey)
	}
	client := *cfg.httpClient
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		finish()
		return obs, &TransportError{Code: classifyTransport(err), Message: err.Error(), Cause: err, Latency: obs.Duration, Attempts: 1}
	}
	obs.Status = resp.StatusCode
	obs.ResponseHeaders = safeHeaders(resp.Header)
	obs.RequestID = requestIDFromHeaders(resp.Header)
	raw, truncated, readErr := readBounded(resp.Body)
	closeErr := resp.Body.Close()
	obs.RawResponse = raw
	obs.RawTruncated = truncated
	if readErr != nil {
		obs.ReadError = readErr.Error()
	} else if closeErr != nil {
		obs.ReadError = closeErr.Error()
	}
	finish()
	sum := sha256.Sum256(raw)
	obs.RawSHA256 = hex.EncodeToString(sum[:])
	extractResponseEvidence(raw, &obs)
	if obs.ReadError != "" {
		return obs, &TransportError{Status: obs.Status, Code: "transport", Message: "response body read failed", Cause: readErr, Latency: obs.Duration, Attempts: 1}
	}
	if truncated {
		return obs, &TransportError{Status: obs.Status, Code: "payload", Message: fmt.Sprintf("response exceeded %d bytes", MaxResponseBytes), Latency: obs.Duration, Attempts: 1}
	}
	if err := invalidBilling(obs.Billing); err != nil {
		return obs, &TransportError{Status: obs.Status, Code: "billing", Message: err.Error(), Cause: err, Latency: obs.Duration, Attempts: 1}
	}
	if err := validate(&obs); err != nil {
		return obs, err
	}
	return obs, nil
}

func validateEndpoint(rawURL, expectedPath string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid endpoint: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("endpoint must use http or https with an explicit host")
	}
	if parsed.Path != expectedPath || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return fmt.Errorf("endpoint must be exactly %s without credentials, query, or fragment", expectedPath)
	}
	return nil
}

func invalidBilling(b BillingObservation) error {
	for name, field := range map[string]DecimalField{
		"cost": b.Cost, "input tokens": b.InputTokens,
		"output tokens": b.OutputTokens, "total tokens": b.TotalTokens,
	} {
		if field.Present && !field.Null && !field.Valid {
			return fmt.Errorf("invalid %s field: %s", name, field.Error)
		}
	}
	return nil
}

func failedLocalAttempt(err error) (AttemptObservation, error) {
	now := time.Now().UTC()
	return AttemptObservation{StartedAt: now, EndedAt: now}, &TransportError{Code: "schema", Message: err.Error(), Cause: err}
}

func readBounded(r io.Reader) ([]byte, bool, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxResponseBytes+1))
	if len(b) > MaxResponseBytes {
		return append([]byte(nil), b[:MaxResponseBytes]...), true, err
	}
	return append([]byte(nil), b...), false, err
}

func safeHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for _, key := range []string{"X-Request-Id", "Openrouter-Request-Id", "Cf-Ray"} {
		if value := h.Get(key); value != "" {
			out[http.CanonicalHeaderKey(key)] = value
		}
	}
	return out
}

func requestIDFromHeaders(h http.Header) string {
	for _, key := range []string{"X-Request-Id", "Openrouter-Request-Id"} {
		if value := h.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func extractResponseEvidence(body []byte, obs *AttemptObservation) {
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil {
		return
	}
	decodeString(top["id"], &obs.RequestID)
	decodeString(top["model"], &obs.ResponseModel)
	decodeString(top["provider"], &obs.ResponseProvider)
	var usage map[string]json.RawMessage
	if raw, ok := top["usage"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		_ = json.Unmarshal(raw, &usage)
	}
	obs.Billing.Cost = decimalFrom(firstRaw(usage, top, "cost"), false)
	obs.Billing.InputTokens = decimalFrom(firstRaw(usage, nil, "input_tokens", "prompt_tokens"), true)
	obs.Billing.OutputTokens = decimalFrom(firstRaw(usage, nil, "output_tokens", "completion_tokens"), true)
	obs.Billing.TotalTokens = decimalFrom(firstRaw(usage, nil, "total_tokens"), true)
}

func decodeString(raw json.RawMessage, dst *string) {
	if len(raw) == 0 {
		return
	}
	var value string
	if json.Unmarshal(raw, &value) == nil && value != "" {
		*dst = value
	}
}

func firstRaw(primary, secondary map[string]json.RawMessage, keys ...string) json.RawMessage {
	for _, key := range keys {
		if raw, ok := primary[key]; ok {
			return raw
		}
		if raw, ok := secondary[key]; ok {
			return raw
		}
	}
	return nil
}

func decimalFrom(raw json.RawMessage, integral bool) DecimalField {
	if len(raw) == 0 {
		return DecimalField{}
	}
	field := DecimalField{Present: true, Raw: string(raw)}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		field.Null = true
		return field
	}
	var number json.Number
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&number); err != nil {
		field.Error = err.Error()
		return field
	}
	field.Number = number
	if integral {
		if value, err := number.Int64(); err != nil || value < 0 {
			field.Error = "token count must be a nonnegative integer"
			return field
		}
	} else {
		value, err := strconv.ParseFloat(number.String(), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			field.Error = "cost must be a finite nonnegative JSON number"
			return field
		}
	}
	field.Valid = true
	return field
}

func decodeStrictSingle(body []byte, dst any) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return errors.New("empty JSON")
	}
	if err := rejectDuplicateKeys(body); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func rejectDuplicateKeys(body []byte) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if err := walkJSON(dec, tok); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func walkJSON(dec *json.Decoder, tok json.Token) error {
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyTok.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = true
			value, err := dec.Token()
			if err != nil {
				return err
			}
			if err := walkJSON(dec, value); err != nil {
				return err
			}
		}
		_, err := dec.Token()
		return err
	case '[':
		for dec.More() {
			value, err := dec.Token()
			if err != nil {
				return err
			}
			if err := walkJSON(dec, value); err != nil {
				return err
			}
		}
		_, err := dec.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
}

func strictLabelVocabulary(schema map[string]any) ([]string, error) {
	if schema["type"] != "object" || schema["additionalProperties"] != false {
		return nil, errors.New("schema must be an object with additionalProperties=false")
	}
	required, ok := schema["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "label" {
		return nil, errors.New("schema must require exactly label")
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) != 1 {
		return nil, errors.New("schema must define exactly one property")
	}
	labelDef, ok := properties["label"].(map[string]any)
	if !ok || labelDef["type"] != "string" {
		return nil, errors.New("label property must be a string")
	}
	enum, ok := labelDef["enum"].([]any)
	if !ok || len(enum) == 0 {
		return nil, errors.New("label property must have a nonempty enum")
	}
	labels := make([]string, len(enum))
	seen := map[string]bool{}
	for i, value := range enum {
		label, ok := value.(string)
		if !ok || label == "" || seen[label] {
			return nil, errors.New("label enum must contain unique nonempty strings")
		}
		seen[label] = true
		labels[i] = label
	}
	return labels, nil
}

func parseStrictChatResponse(body []byte, expectedModel, expectedProvider string, labels []string, latency time.Duration) (*ChatObservation, error) {
	if err := rejectDuplicateKeys(body); err != nil {
		return nil, &TransportError{Code: "schema", Message: "decode chat response: " + err.Error(), Cause: err, Latency: latency, Attempts: 1}
	}
	var resp ChatResponse
	if err := decodeStrictSingle(body, &resp); err != nil {
		return nil, &TransportError{Code: "schema", Message: "decode chat response: " + err.Error(), Cause: err, Latency: latency, Attempts: 1}
	}
	if resp.Model != expectedModel {
		return nil, &TransportError{Code: "model-pin", Message: fmt.Sprintf("chat response model %q does not match accepted pin %q", resp.Model, expectedModel), Latency: latency, Attempts: 1}
	}
	if resp.Provider == "" || resp.Provider != expectedProvider {
		return nil, &TransportError{Code: "provider-pin", Message: fmt.Sprintf("chat response provider %q does not match accepted provider %q", resp.Provider, expectedProvider), Latency: latency, Attempts: 1}
	}
	if len(resp.Choices) != 1 {
		return nil, &TransportError{Code: "schema", Message: "chat response must contain exactly one choice", Latency: latency, Attempts: 1}
	}
	choice := resp.Choices[0]
	if choice.Index != 0 || choice.Message.Role != "assistant" || choice.Message.Refusal != nil || choice.FinishReason != "stop" {
		return nil, &TransportError{Code: "schema", Message: "chat response choice is refused, truncated, or malformed", Latency: latency, Attempts: 1}
	}
	label, err := decodeExactLabel(choice.Message.Content)
	if err != nil {
		return nil, &TransportError{Code: "schema", Message: err.Error(), Cause: err, Latency: latency, Attempts: 1}
	}
	allowed := false
	for _, candidate := range labels {
		allowed = allowed || candidate == label
	}
	if !allowed {
		return nil, &TransportError{Code: "schema", Message: fmt.Sprintf("label %q is outside request vocabulary", label), Latency: latency, Attempts: 1}
	}
	return &ChatObservation{
		ResolvedModel: resp.Model,
		Content:       map[string]any{"label": label}, Usage: resp.Usage,
		Latency: latency, TotalLatency: latency, AttemptLatencies: []time.Duration{latency}, Attempts: 1,
		Raw: append(json.RawMessage(nil), body...),
	}, nil
}

func decodeExactLabel(content string) (string, error) {
	if err := rejectDuplicateKeys([]byte(content)); err != nil {
		return "", fmt.Errorf("chat response content: %w", err)
	}
	var value struct {
		Label string `json:"label"`
	}
	if err := decodeStrictSingle([]byte(content), &value); err != nil {
		return "", fmt.Errorf("chat response content must be exactly one label object: %w", err)
	}
	if value.Label == "" {
		return "", errors.New("chat response label is empty")
	}
	return value.Label, nil
}

func sameKeys(values map[string]float64, criteria map[string]DecisionCriteria) bool {
	if len(values) != len(criteria) {
		return false
	}
	for key := range criteria {
		if _, ok := values[key]; !ok {
			return false
		}
	}
	return true
}
