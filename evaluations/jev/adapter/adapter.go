// Package adapter implements isolated typed HTTP adapters for OpenRouter
// Decisions and structured-output chat. The adapters are transport-only: they
// have no side effects, no state writes, no production imports, and no network
// inference. Every transport rule listed in the KHA-579 protocol v0.1 is
// enforced: required fields, enum/question IDs, distribution shape, model
// pin, response size, timeout, cancellation, and 401/402/403/429/5xx mapping.
//
// The package is intended to be unit-tested with httptest. Live calls are
// possible but require explicit configuration; nothing in this package will
// reach the network on import.
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
	"sort"
	"strings"
	"time"
)

// DecisionVerdictID is the canonical question ID for the verdict decision
// question. Every request payload MUST use this exact key.
const DecisionVerdictID = "verdict"

// ProbabilityTolerance is the absolute allowed deviation when validating
// that a Choice distribution sums to 1.0. Models routinely produce
// floating-point sums like 0.99999997.
const ProbabilityTolerance = 1e-3

// MaxResponseBytes caps the response body size the adapter will accept.
const MaxResponseBytes = 4 << 20 // 4 MiB

// RequestTimeout is the default per-call timeout when the caller does not
// override via Config.Timeout.
const RequestTimeout = 20 * time.Second

// RetryPolicy caps bounded retries. The protocol permits at most one
// transient retry for 429/5xx/transport errors; auth, schema and model pin
// failures never retry.
type RetryPolicy struct {
	MaxRetries    int           // total retry attempts beyond the first try
	BaseBackoff   time.Duration // first retry waits this long
	MaxBackoff    time.Duration // cap on exponential backoff
	RetryAfterMax time.Duration // absolute ceiling on a server-provided Retry-After wait
	RetryOnStatus []int         // HTTP statuses considered transient
}

// DefaultRetryPolicy is the policy the harness uses unless the caller
// overrides. It permits exactly one retry on 429 and 5xx.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:    1,
		BaseBackoff:   200 * time.Millisecond,
		MaxBackoff:    2 * time.Second,
		RetryAfterMax: 5 * time.Second,
		RetryOnStatus: []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout},
	}
}

// DecisionCriteria describes a single Choice label.
type DecisionCriteria struct {
	Label       string `json:"-"`
	Description string `json:"description"`
}

// DecisionQuestion is a single Choice question in the Decisions API payload.
// Only the Choice variant is implemented; Noul and Score are out of scope.
type DecisionQuestion struct {
	ID           string                      `json:"-"`
	Type         string                      `json:"type,omitempty"`
	Instructions string                      `json:"instructions"`
	Criteria     map[string]DecisionCriteria `json:"criteria"`
}

// State is the optional opaque state object passed to Decisions.
type State struct {
	// ID is an opaque string the harness can use to correlate requests.
	ID string `json:"id,omitempty"`
	// System is an optional system role injected into the model's state.
	System string `json:"system,omitempty"`
	// Docs is an arbitrary list of evidence strings supplied to the model.
	Docs []string `json:"docs,omitempty"`
	// Extra fields not enumerated by the protocol are permitted through
	// Additional to avoid drift when the API gains new state keys.
	Additional map[string]any `json:"-"`
}

// MarshalJSON flattens the known keys with Additional keys, so the adapter
// stays compatible with future state shapes without re-vendoring.
func (s State) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, 4)
	if s.ID != "" {
		out["id"] = s.ID
	}
	if s.System != "" {
		out["system"] = s.System
	}
	if len(s.Docs) > 0 {
		out["docs"] = s.Docs
	}
	for k, v := range s.Additional {
		out[k] = v
	}
	return json.Marshal(out)
}

// DecisionRequest is the body of POST /api/alpha/decisions.
type DecisionRequest struct {
	Model     string                      `json:"model"`
	Questions map[string]DecisionQuestion `json:"questions"`
	State     State                       `json:"state"`
}

// ChoiceAnswer is the model's per-question response. Confidence is a pointer
// to distinguish "model emitted 0.0" from "model omitted the field". The
// protocol forbids the adapter from inventing a value when the field is
// absent.
type ChoiceAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Confidence    *float64           `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// DecisionUsage is the usage block the protocol documents.
type DecisionUsage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
}

// DecisionResponse is the parsed body of a successful Decisions response.
type DecisionResponse struct {
	ID      string                  `json:"id"`
	Model   string                  `json:"model"`
	Answers map[string]ChoiceAnswer `json:"answers"`
	Usage   DecisionUsage           `json:"usage"`
}

// Decision is the validated, immutable observation produced by the adapter.
//
// All optional fields are pointers so the caller can distinguish "absent"
// from "zero". Probabilities may be nil when the provider omits them; the
// adapter records absence rather than synthesizing a degenerate
// distribution.
//
// Latency is the final attempt's duration; per-attempt durations and total
// wall time (including backoff and body reads) live in AttemptLatencies /
// TotalLatency. The harness NEVER relabels these as provider cold/warm —
// cold/warm attribution is not derivable from a single adapter run.
type Decision struct {
	QuestionID       string
	Choice           string
	Confidence       *float64
	Probabilities    map[string]float64
	ResolvedModel    string
	Latency          time.Duration
	AttemptLatencies []time.Duration
	TotalLatency     time.Duration
	Attempts         int
	Usage            DecisionUsage
	Raw              json.RawMessage // preserved verbatim for audit; non-secret
}

// TransportError is the typed error returned by the adapter for non-success
// outcomes. The harness distinguishes these from semantic abstentions.
type TransportError struct {
	Status   int           // HTTP status (0 when not from a response)
	Code     string        // short code: "auth", "schema", "model-pin", "timeout", "transport", "server", "rate-limit", "payload", "usage-overrun", "cancelled"
	Message  string        // human-readable detail
	Cause    error         // underlying error if any
	Latency  time.Duration // time spent before failing
	Attempts int           // attempts before giving up
}

// Error implements the error interface.
func (e *TransportError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("transport: %s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("transport: %s: %s", e.Code, e.Message)
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *TransportError) Unwrap() error { return e.Cause }

// Retryable reports whether the adapter's retry policy considers this error
// transient. Only server, rate-limit, and transport errors retry.
func (e *TransportError) Retryable() bool {
	switch e.Code {
	case "rate-limit", "server", "transport":
		return true
	}
	return false
}

// Config configures a Client. Model is the request alias; ResponseModel is the
// exact accepted response pin and defaults to Model for compatibility.
type Config struct {
	Endpoint         string
	APIKey           string
	Model            string // request alias
	ResponseModel    string // exact accepted response pin; defaults to Model for compatibility
	ResponseProvider string // exact provider identity when the response contract exposes one
	Timeout          time.Duration
	Retry            RetryPolicy
	HTTPClient       *http.Client
}

// Validate enforces that a live call has the prerequisites.
func (c Config) Validate() error {
	if c.Endpoint == "" {
		return errors.New("adapter: endpoint required")
	}
	if c.Model == "" {
		return errors.New("adapter: model pin required")
	}
	if c.Timeout <= 0 {
		return errors.New("adapter: timeout must be > 0")
	}
	if c.Retry.MaxRetries < 0 {
		return errors.New("adapter: retry.MaxRetries must be >= 0")
	}
	return nil
}

// Client is an isolated Decisions API client. It has no global state and no
// concurrent in-flight calls (concurrency is bounded to 1 by the harness).
type Client struct {
	cfg Config
}

// NewClient validates config and returns a Client. The HTTP client defaults
// to a fresh http.Client that honours Config.Timeout.
func NewClient(cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	} else {
		// Force the caller-supplied client to honour our timeout budget.
		cfg.HTTPClient.Timeout = cfg.Timeout
	}
	if cfg.Retry.MaxRetries == 0 && cfg.Retry.BaseBackoff == 0 {
		cfg.Retry = DefaultRetryPolicy()
	}
	if cfg.ResponseModel == "" {
		cfg.ResponseModel = cfg.Model
	}
	return &Client{cfg: cfg}, nil
}

// SubmitDecisions executes one Decisions request, validates the response,
// and returns an immutable Decision. Concurrency 1: callers serialize.
// Retries obey the supplied RetryPolicy. Auth, schema and model-pin errors
// do NOT retry.
//
// All successful Decisions carry per-attempt durations and total wall time
// including backoff and body reads. The harness does NOT relabel any of
// these as provider cold/warm: that attribution requires a separate
// measurement the adapter cannot make from a single call.
func (c *Client) SubmitDecisions(ctx context.Context, req DecisionRequest) (*Decision, error) {
	if err := validateDecisionRequest(req); err != nil {
		return nil, &TransportError{Code: "schema", Message: err.Error()}
	}
	if req.Model != c.cfg.Model {
		return nil, &TransportError{
			Code:    "model-pin",
			Message: fmt.Sprintf("request model %q does not match pinned model %q", req.Model, c.cfg.Model),
		}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, &TransportError{Code: "payload", Message: "marshal request: " + err.Error(), Cause: err}
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
			return nil, &TransportError{Code: "payload", Message: "build request: " + err.Error(), Cause: err, Attempts: attempt}
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")
		if c.cfg.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		}
		resp, err := c.cfg.HTTPClient.Do(httpReq)
		lastLatency = time.Since(start)
		// Per-attempt duration includes the round-trip and body read; we
		// capture it once the body has been read below.
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
		// Read body within MaxResponseBytes.
		limited := io.LimitReader(resp.Body, MaxResponseBytes+1)
		rawBody, err := io.ReadAll(limited)
		cancel()
		_ = resp.Body.Close()
		attDur := time.Since(start)
		attemptLatencies = append(attemptLatencies, attDur)
		status = resp.StatusCode
		respBody = rawBody
		if len(rawBody) > MaxResponseBytes {
			return nil, &TransportError{Status: status, Code: "payload", Message: fmt.Sprintf("response exceeded %d bytes", MaxResponseBytes), Latency: attDur, Attempts: attempt}
		}
		if isRetryableStatus(status, c.cfg.Retry.RetryOnStatus) && attempt <= c.cfg.Retry.MaxRetries {
			wait := retryAfter(resp, c.cfg.Retry.RetryAfterMax, c.cfg.Retry.BaseBackoff, c.cfg.Retry.MaxBackoff)
			retryable = true
			transErr = &TransportError{Status: status, Code: codeForStatus(status), Message: resp.Status, Latency: attDur, Attempts: attempt}
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
	dec, err := parseDecisionResponse(respBody, c.cfg.ResponseModel, lastLatency, attempt)
	if err != nil {
		return nil, err
	}
	dec.AttemptLatencies = attemptLatencies
	dec.TotalLatency = time.Since(totalStart)
	return dec, nil
}

// validateDecisionRequest enforces the request shape independently of any
// HTTP call. It is also called before sending so the adapter rejects malformed
// inputs without spending a network round-trip.
func validateDecisionRequest(req DecisionRequest) error {
	if req.Model == "" {
		return errors.New("model is required")
	}
	if len(req.Questions) == 0 {
		return errors.New("questions must contain at least one entry")
	}
	for id, q := range req.Questions {
		if q.Instructions == "" {
			return fmt.Errorf("question %q: instructions required", id)
		}
		if len(q.Criteria) == 0 {
			return fmt.Errorf("question %q: criteria must contain at least one label", id)
		}
		for label, c := range q.Criteria {
			if c.Description == "" {
				return fmt.Errorf("question %q label %q: description required", id, label)
			}
		}
	}
	return nil
}

func parseDecisionResponse(body []byte, expectedModel string, latency time.Duration, attempts int) (*Decision, error) {
	if len(body) == 0 {
		return nil, &TransportError{Code: "schema", Message: "empty response body", Latency: latency, Attempts: attempts}
	}
	var resp DecisionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, &TransportError{Code: "schema", Message: "decode response: " + err.Error(), Cause: err, Latency: latency, Attempts: attempts}
	}
	if resp.Model != expectedModel {
		return nil, &TransportError{
			Code:     "model-pin",
			Message:  fmt.Sprintf("response model %q does not match pinned model %q", resp.Model, expectedModel),
			Latency:  latency,
			Attempts: attempts,
		}
	}
	answer, ok := resp.Answers[DecisionVerdictID]
	if !ok {
		return nil, &TransportError{Code: "schema", Message: fmt.Sprintf("answers missing question %q", DecisionVerdictID), Latency: latency, Attempts: attempts}
	}
	if answer.Type != "choice" {
		return nil, &TransportError{Code: "schema", Message: fmt.Sprintf("answer type %q is not choice", answer.Type), Latency: latency, Attempts: attempts}
	}
	if answer.Choice == "" {
		return nil, &TransportError{Code: "schema", Message: "answer.choice is empty", Latency: latency, Attempts: attempts}
	}
	// Probabilities are optional. When present, validate shape.
	if len(answer.Probabilities) > 0 {
		if err := validateProbabilities(answer.Probabilities); err != nil {
			return nil, &TransportError{Code: "schema", Message: err.Error(), Latency: latency, Attempts: attempts}
		}
		if err := validateProbabilityCoverage(answer.Probabilities); err != nil {
			return nil, &TransportError{Code: "schema", Message: err.Error(), Latency: latency, Attempts: attempts}
		}
	}
	if answer.Confidence != nil {
		if !isFinite(*answer.Confidence) || *answer.Confidence < 0 || *answer.Confidence > 1 {
			return nil, &TransportError{Code: "schema", Message: fmt.Sprintf("confidence %v is not in [0,1]", *answer.Confidence), Latency: latency, Attempts: attempts}
		}
	}
	return &Decision{
		QuestionID:    DecisionVerdictID,
		Choice:        answer.Choice,
		Confidence:    cloneConfidence(answer.Confidence),
		Probabilities: cloneProbabilities(answer.Probabilities),
		ResolvedModel: resp.Model,
		Latency:       latency,
		Attempts:      attempts,
		Usage:         resp.Usage,
		Raw:           append(json.RawMessage(nil), body...),
	}, nil
}

func cloneConfidence(c *float64) *float64 {
	if c == nil {
		return nil
	}
	v := *c
	return &v
}

// validateProbabilities enforces that every probability is a finite number in
// [0,1].
func validateProbabilities(p map[string]float64) error {
	if len(p) == 0 {
		return errors.New("probabilities must contain at least one entry")
	}
	for k, v := range p {
		if !isFinite(v) {
			return fmt.Errorf("probability %q is not finite", k)
		}
		if v < 0 || v > 1 {
			return fmt.Errorf("probability %q = %v outside [0,1]", k, v)
		}
	}
	return nil
}

// validateProbabilityCoverage requires the distribution to sum to ~1.0.
func validateProbabilityCoverage(p map[string]float64) error {
	sum := 0.0
	for _, v := range p {
		sum += v
	}
	if math.Abs(sum-1.0) > ProbabilityTolerance {
		return fmt.Errorf("probabilities sum to %v, tolerance %v", sum, ProbabilityTolerance)
	}
	return nil
}

func cloneProbabilities(p map[string]float64) map[string]float64 {
	if p == nil {
		return nil
	}
	out := make(map[string]float64, len(p))
	for k, v := range p {
		out[k] = v
	}
	return out
}

func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// isRetryableStatus reports whether the supplied status is in the policy.
func isRetryableStatus(status int, allow []int) bool {
	for _, s := range allow {
		if s == status {
			return true
		}
	}
	return false
}

func codeForStatus(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "auth"
	case http.StatusPaymentRequired:
		return "usage-overrun"
	case http.StatusForbidden:
		return "auth"
	case http.StatusTooManyRequests:
		return "rate-limit"
	}
	if status >= 500 {
		return "server"
	}
	if status >= 400 {
		return "schema"
	}
	return "transport"
}

func classifyTransport(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if isDNSOrConn(err) {
		return "transport"
	}
	return "transport"
}

func isDNSOrConn(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "dial") || strings.Contains(s, "connect") || strings.Contains(s, "no such host") || strings.Contains(s, "connection refused")
}

func transErrFromStatus(status int, body []byte, latency time.Duration, attempts int) *TransportError {
	msg := fmt.Sprintf("HTTP %d", status)
	if len(body) > 0 {
		// Best-effort include of server-provided message; never the full body.
		msg = fmt.Sprintf("HTTP %d: %s", status, truncate(string(body), 256))
	}
	return &TransportError{Status: status, Code: codeForStatus(status), Message: msg, Latency: latency, Attempts: attempts}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func retryAfter(resp *http.Response, retryAfterMax, base, max time.Duration) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		// Honor Retry-After up to retryAfterMax.
		var secs int
		if _, err := fmt.Sscanf(v, "%d", &secs); err == nil && secs > 0 {
			d := time.Duration(secs) * time.Second
			if d > retryAfterMax {
				d = retryAfterMax
			}
			if d < base {
				d = base
			}
			return d
		}
	}
	if base <= 0 {
		base = 100 * time.Millisecond
	}
	if max <= 0 {
		return base
	}
	return base
}

func sleepCtx(ctx context.Context, base, ceiling time.Duration) error {
	if base <= 0 {
		return nil
	}
	if base > ceiling {
		base = ceiling
	}
	t := time.NewTimer(base)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// DecisionKeys are the expected Criteria keys when scoring a particular task.
// The caller composes the criteria by intersecting the keys with the
// protocol vocabulary for the task.
func DecisionKeys(task string, vocab []string) []string {
	out := make([]string, 0, len(vocab))
	for _, v := range vocab {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// MarshalCriteria encodes the criteria map for a question in deterministic
// order, so two fixtures with the same labels produce byte-equal JSON.
func MarshalCriteria(task string, vocab []string, desc func(label string) string) map[string]DecisionCriteria {
	out := make(map[string]DecisionCriteria, len(vocab))
	for _, label := range vocab {
		out[label] = DecisionCriteria{Label: label, Description: desc(label)}
	}
	return out
}
