package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
	"github.com/Clarit-AI/Plexium/evaluations/jev/ledger"
	"github.com/Clarit-AI/Plexium/evaluations/jev/pilot"
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

const (
	reportName         = "probe-report.json"
	ledgerName         = "probe-ledger.jsonl"
	lockName           = "probe-run.lock"
	accountingInBound  = int64(50_000)
	accountingOutBound = int64(4_096)
)

type Endpoints struct {
	Jev  string `json:"jev"`
	Nano string `json:"nano"`
}

type RunConfig struct {
	InventoryPath string
	StateDir      string
	APIKey        string
	// RequestOrdinals is an explicitly approved subset of the immutable
	// four-request plan. Nil/empty preserves the full-plan compatibility path.
	RequestOrdinals []int
	Endpoints       Endpoints
	HTTPClient      *http.Client
	Timeout         time.Duration
	JevPin          string
	JevProvider     string
	NanoPin         string
	NanoProvider    string
}

func DefaultRunConfig(inventoryPath, stateDir, apiKey string) RunConfig {
	return RunConfig{
		InventoryPath: inventoryPath, StateDir: stateDir, APIKey: apiKey,
		Endpoints: Endpoints{Jev: JevEndpoint, Nano: NanoEndpoint}, Timeout: 20 * time.Second,
		JevPin: JevCandidatePin, JevProvider: JevCandidateProvider,
		NanoPin: NanoCandidatePin, NanoProvider: NanoProvider,
	}
}

type DecimalEvidence struct {
	Present     bool   `json:"present"`
	Null        bool   `json:"null"`
	Valid       bool   `json:"valid"`
	Raw         string `json:"raw,omitempty"`
	RawWithheld bool   `json:"rawWithheld,omitempty"`
	RawSHA256   string `json:"rawSha256,omitempty"`
	RawEvidence string `json:"rawEvidence,omitempty"`
	Error       string `json:"error,omitempty"`
}

type BooleanEvidence struct {
	Present     bool   `json:"present"`
	Null        bool   `json:"null"`
	Valid       bool   `json:"valid"`
	Value       bool   `json:"value"`
	Raw         string `json:"raw,omitempty"`
	RawWithheld bool   `json:"rawWithheld,omitempty"`
	RawSHA256   string `json:"rawSha256,omitempty"`
	RawEvidence string `json:"rawEvidence,omitempty"`
	Error       string `json:"error,omitempty"`
}

type PromptTokenDetailsEvidence struct {
	Present          bool            `json:"present"`
	Null             bool            `json:"null"`
	Valid            bool            `json:"valid"`
	CachedTokens     DecimalEvidence `json:"cachedTokens"`
	CacheWriteTokens DecimalEvidence `json:"cacheWriteTokens"`
	AudioTokens      DecimalEvidence `json:"audioTokens"`
	VideoTokens      DecimalEvidence `json:"videoTokens"`
	Error            string          `json:"error,omitempty"`
}

type CompletionTokenDetailsEvidence struct {
	Present                  bool            `json:"present"`
	Null                     bool            `json:"null"`
	Valid                    bool            `json:"valid"`
	ReasoningTokens          DecimalEvidence `json:"reasoningTokens"`
	ImageTokens              DecimalEvidence `json:"imageTokens"`
	AudioTokens              DecimalEvidence `json:"audioTokens"`
	AcceptedPredictionTokens DecimalEvidence `json:"acceptedPredictionTokens"`
	RejectedPredictionTokens DecimalEvidence `json:"rejectedPredictionTokens"`
	Error                    string          `json:"error,omitempty"`
}

type CostDetailsEvidence struct {
	Present                          bool            `json:"present"`
	Null                             bool            `json:"null"`
	Valid                            bool            `json:"valid"`
	UpstreamInferenceCost            DecimalEvidence `json:"upstreamInferenceCost"`
	UpstreamInferencePromptCost      DecimalEvidence `json:"upstreamInferencePromptCost"`
	UpstreamInferenceCompletionsCost DecimalEvidence `json:"upstreamInferenceCompletionsCost"`
	Error                            string          `json:"error,omitempty"`
}

type BillingEvidence struct {
	Cost                     DecimalEvidence                `json:"cost"`
	InputTokens              DecimalEvidence                `json:"inputTokens"`
	OutputTokens             DecimalEvidence                `json:"outputTokens"`
	TotalTokens              DecimalEvidence                `json:"totalTokens"`
	PromptTokenDetails       PromptTokenDetailsEvidence     `json:"promptTokenDetails"`
	CompletionTokenDetails   CompletionTokenDetailsEvidence `json:"completionTokenDetails"`
	CostDetails              CostDetailsEvidence            `json:"costDetails"`
	IsBYOK                   BooleanEvidence                `json:"isByok"`
	RateSemanticsDiscrepancy string                         `json:"rateSemanticsDiscrepancy,omitempty"`
	Error                    string                         `json:"error,omitempty"`
}

type Attempt struct {
	Ordinal           int                        `json:"ordinal"`
	RequestID         string                     `json:"requestId"`
	Arm               pilot.Arm                  `json:"arm"`
	Kind              string                     `json:"kind"`
	FixtureID         string                     `json:"fixtureId,omitempty"`
	RequestedAlias    string                     `json:"requestedAlias"`
	ExpectedPin       string                     `json:"expectedResponsePin"`
	ExpectedProvider  string                     `json:"expectedProvider"`
	RequestSHA256     string                     `json:"requestSha256"`
	ReservationRef    string                     `json:"reservationRef,omitempty"`
	Reservation       ledger.MicroUnit           `json:"reservationMicrodollars"`
	State             string                     `json:"state"`
	RequestSent       bool                       `json:"requestSent"`
	ResponseReceived  bool                       `json:"responseReceived"`
	Status            int                        `json:"status"`
	ProviderRequestID string                     `json:"providerRequestId,omitempty"`
	ResolvedModel     string                     `json:"resolvedModel,omitempty"`
	ResponseProvider  string                     `json:"responseProvider,omitempty"`
	ResponseHeaders   map[string]string          `json:"responseHeaders,omitempty"`
	StartedAt         time.Time                  `json:"startedAt,omitempty"`
	EndedAt           time.Time                  `json:"endedAt,omitempty"`
	LatencyNanos      int64                      `json:"latencyNanos"`
	Billing           BillingEvidence            `json:"billing"`
	UsageFields       map[string]json.RawMessage `json:"usageFields,omitempty"`
	IdentityFields    map[string]json.RawMessage `json:"identityFields,omitempty"`
	FinishReasons     []string                   `json:"finishReasons,omitempty"`
	RawResponseSHA256 string                     `json:"originalRawResponseSha256,omitempty"`
	EvidenceSHA256    string                     `json:"persistedEvidenceSha256,omitempty"`
	EvidenceSanitized bool                       `json:"persistedEvidenceCredentialRedacted"`
	EvidenceKind      string                     `json:"persistedEvidenceKind,omitempty"`
	RawEvidencePath   string                     `json:"rawEvidencePath,omitempty"`
	RawTruncated      bool                       `json:"rawTruncated"`
	ReadError         string                     `json:"readError,omitempty"`
	AdapterError      string                     `json:"adapterError,omitempty"`
	Reconciliation    string                     `json:"reconciliation,omitempty"`
}

type Gate struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type Report struct {
	Version            string           `json:"version"`
	InventorySHA256    string           `json:"inventorySha256"`
	AuthorizedCap      ledger.MicroUnit `json:"authorizedCapMicrodollars"`
	ProbeSubcap        ledger.MicroUnit `json:"probeSubcapMicrodollars"`
	AccountingBasis    string           `json:"accountingBasis"`
	ExecutionReady     bool             `json:"executionReady"`
	ExecutionReadiness string           `json:"executionReadiness"`
	SelectedOrdinals   []int            `json:"selectedOrdinals"`
	SelectedRequests   []RequestBinding `json:"selectedRequests"`
	ReservedTotal      ledger.MicroUnit `json:"reservedTotalMicrodollars"`
	LedgerBalance      ledger.MicroUnit `json:"ledgerBalanceMicrodollars"`
	StartedAt          time.Time        `json:"startedAt"`
	EndedAt            time.Time        `json:"endedAt,omitempty"`
	Halted             bool             `json:"halted"`
	HaltReason         string           `json:"haltReason,omitempty"`
	Attempts           []Attempt        `json:"attempts"`
	Gates              []Gate           `json:"gates"`
	credential         string
}

func Run(ctx context.Context, cfg RunConfig) (*Report, error) {
	report, err := runWithLimits(ctx, cfg, runLimits{AuthorizedCap: AuthorizedCap, ProbeSubcap: ProbeSubcap})
	if err != nil {
		return report, errors.New(sanitizeString(err.Error(), cfg.APIKey))
	}
	return report, nil
}

type runLimits struct {
	AuthorizedCap ledger.MicroUnit
	ProbeSubcap   ledger.MicroUnit
}

func runWithLimits(ctx context.Context, cfg RunConfig, limits runLimits) (*Report, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("probe: credential environment variable %s is empty", CredentialEnv)
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("probe: timeout must be positive")
	}
	if cfg.JevPin == "" || cfg.JevProvider == "" || cfg.NanoPin == "" || cfg.NanoProvider == "" {
		return nil, errors.New("probe: candidate pin/provider expectations are required")
	}
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		return nil, fmt.Errorf("probe: create state directory: %w", err)
	}
	lockPath := filepath.Join(cfg.StateDir, lockName)
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("probe: acquire run lock: %w", err)
	}
	_ = lock.Close()
	defer os.Remove(lockPath)
	if err := requirePristineState(cfg.StateDir); err != nil {
		return nil, err
	}
	reportPath := filepath.Join(cfg.StateDir, reportName)

	plan, err := BuildPlan(cfg.InventoryPath, cfg.Endpoints)
	if err != nil {
		return nil, err
	}
	for i := range plan.Requests {
		if plan.Requests[i].Arm == pilot.ArmJev {
			plan.Requests[i].CandidatePin, plan.Requests[i].CandidateProvider = cfg.JevPin, cfg.JevProvider
		} else {
			plan.Requests[i].CandidatePin, plan.Requests[i].CandidateProvider = cfg.NanoPin, cfg.NanoProvider
		}
	}
	selectedRequests, err := SelectPlanRequests(plan, cfg.RequestOrdinals)
	if err != nil {
		return nil, err
	}
	report := &Report{
		Version: PlanVersion, InventorySHA256: plan.InventorySHA256, AuthorizedCap: limits.AuthorizedCap,
		ProbeSubcap: limits.ProbeSubcap, AccountingBasis: AccountingBasis,
		ExecutionReady: false, ExecutionReadiness: ReadinessReason,
		SelectedOrdinals: append([]int(nil), plan.SelectedOrdinals...), SelectedRequests: append([]RequestBinding(nil), plan.SelectedRequests...),
		StartedAt: time.Now().UTC(), Gates: initialGates(), credential: cfg.APIKey,
	}
	if err := persistReport(reportPath, report); err != nil {
		return nil, err
	}

	zero := ledger.MicroUnit(0)
	bounds := ledger.TokenBounds{MaxInputTokens: accountingInBound, MaxOutputTokens: accountingOutBound}
	l, err := ledger.Open(ledger.LedgerConfig{
		Path: filepath.Join(cfg.StateDir, ledgerName), AuthorizedCap: limits.AuthorizedCap,
		RunID: "jev-pin-probe", Model: "mixed:" + cfg.JevPin + "+" + cfg.NanoPin,
		ManifestKey: ledger.ManifestKey{
			FixtureFileSHA: plan.InventorySHA256, ProtocolVersion: protocol.ProtocolVersion,
			ModelPin: cfg.JevPin + "+" + cfg.NanoPin, RatesVersion: "probe-reservation-envelope-v1-not-provider-tariff",
			TokenBoundsHash: ledger.ComputeTokenBoundsHash(bounds, 0, 0, 0, 0),
		},
		TokenBounds: bounds, RetryPolicy: ledger.ReservationRetryPolicy{},
		RateIn: 1_000_000, RateOut: &zero,
	})
	if err != nil {
		return halt(reportPath, report, "open ledger: "+err.Error(), nil)
	}
	defer l.Close()

	jevClient, err := adapter.NewClient(adapter.Config{
		Endpoint: cfg.Endpoints.Jev, APIKey: cfg.APIKey, Model: JevAlias,
		ResponseModel: cfg.JevPin, ResponseProvider: cfg.JevProvider,
		Timeout: cfg.Timeout, HTTPClient: cfg.HTTPClient,
	})
	if err != nil {
		return halt(reportPath, report, "create Jev client: "+err.Error(), l)
	}
	nanoClient, err := adapter.NewChatClient(adapter.ChatConfig{
		Endpoint: cfg.Endpoints.Nano, APIKey: cfg.APIKey, Model: NanoAlias,
		ResponseModel: cfg.NanoPin, ResponseProvider: cfg.NanoProvider,
		Timeout: cfg.Timeout, HTTPClient: cfg.HTTPClient,
	})
	if err != nil {
		return halt(reportPath, report, "create Nano client: "+err.Error(), l)
	}

	for _, request := range selectedRequests {
		if report.ReservedTotal+request.Reservation > limits.ProbeSubcap {
			return halt(reportPath, report, "probe subcap exhausted before "+request.ID, l)
		}
		ref, reserved, err := l.Reserve(ctx, request.Ordinal, "pin-probe", request.ID, accountingInBound, accountingOutBound)
		if err != nil {
			return halt(reportPath, report, "reserve "+request.ID+": "+err.Error(), l)
		}
		if reserved != request.Reservation {
			return halt(reportPath, report, fmt.Sprintf("reservation drift for %s: got %d want %d", request.ID, reserved, request.Reservation), l)
		}
		report.ReservedTotal += reserved
		report.LedgerBalance = l.Balance()
		attempt := Attempt{
			Ordinal: request.Ordinal, RequestID: request.ID, Arm: request.Arm, Kind: request.Kind,
			FixtureID: request.FixtureID, RequestedAlias: sanitizeString(request.RequestedAlias, cfg.APIKey),
			ExpectedPin: sanitizeString(request.CandidatePin, cfg.APIKey), ExpectedProvider: sanitizeString(request.CandidateProvider, cfg.APIKey),
			RequestSHA256: request.RequestSHA256, ReservationRef: ref, Reservation: reserved, State: "reserved",
		}
		report.Attempts = append(report.Attempts, attempt)
		if err := persistReport(reportPath, report); err != nil {
			return report, err
		}

		var obs adapter.AttemptObservation
		if request.Arm == pilot.ArmJev {
			obs, err = jevClient.SubmitDecisionsOnce(ctx, request.Body)
		} else {
			obs, err = nanoClient.CompleteOnce(ctx, request.Body)
		}
		safeRaw, redacted, screenErr := screenResponse(obs.RawResponse, cfg.APIKey)
		attempt = observationAttempt(attempt, obs, err, cfg.APIKey, safeRaw)
		var rawPath, evidenceHash string
		var rawErr error
		if screenErr == nil {
			rawPath, evidenceHash, rawErr = persistRaw(cfg.StateDir, request, safeRaw)
		} else if len(obs.RawResponse) > 0 {
			attempt.EvidenceKind = "hash-only: response could not be screened without changing a JSON key or preserving undecodable text"
		}
		if screenErr == nil && rawErr == nil && len(obs.RawResponse) > 0 {
			attempt.RawEvidencePath = rawPath
			attempt.EvidenceSHA256 = evidenceHash
			attempt.EvidenceSanitized = redacted
			if redacted {
				attempt.EvidenceKind = "credential-redacted JSON response; original bytes withheld and represented only by originalRawResponseSha256"
			} else {
				attempt.EvidenceKind = "credential-screened JSON response; persisted bytes equal the original bounded response"
			}
		}
		report.Attempts[len(report.Attempts)-1] = attempt

		billing, billingErr := parseBilling(obs.Billing)
		usageOverrun := billingErr == nil && (billing.inputTokens > accountingInBound || billing.outputTokens > accountingOutBound)
		var anomalies []string
		if screenErr != nil {
			anomalies = append(anomalies, "screen response evidence: "+sanitizeString(screenErr.Error(), cfg.APIKey))
		}
		if rawErr != nil {
			anomalies = append(anomalies, "persist sanitized evidence: "+sanitizeString(rawErr.Error(), cfg.APIKey))
		}
		if err != nil {
			anomalies = append(anomalies, sanitizeString(err.Error(), cfg.APIKey))
		}
		if billingErr != nil {
			anomalies = append(anomalies, billingErr.Error())
		}
		if usageOverrun {
			anomalies = append(anomalies, "usage exceeds conservative accounting envelope")
		}

		if len(anomalies) > 0 {
			if billingErr == nil {
				reconcileAnomalousBilling(ctx, l, ref, reserved, billing, usageOverrun, &report.Attempts[len(report.Attempts)-1], cfg.APIKey)
			}
			report.LedgerBalance = l.Balance()
			reason := request.ID + ": " + strings.Join(anomalies, "; ")
			return halt(reportPath, report, sanitizeString(reason, cfg.APIKey), l)
		}

		if billing.cost == 0 {
			report.Attempts[len(report.Attempts)-1].State = "observed-zero-reservation-retained"
			report.Attempts[len(report.Attempts)-1].Reconciliation = "explicit valid zero recorded; full reservation retained under accepted ledger semantics"
		} else {
			release, settleErr := l.Settle(ctx, ref, billing.cost, billing.inputTokens, billing.outputTokens, 0, 0)
			if settleErr != nil {
				report.Attempts[len(report.Attempts)-1].State = "accounted-halt"
				report.Attempts[len(report.Attempts)-1].Reconciliation = "positive billing submitted to accepted ledger before halt: " + sanitizeString(settleErr.Error(), cfg.APIKey)
				report.LedgerBalance = l.Balance()
				return halt(reportPath, report, request.ID+": settle: "+sanitizeString(settleErr.Error(), cfg.APIKey), l)
			}
			report.Attempts[len(report.Attempts)-1].State = "settled"
			report.Attempts[len(report.Attempts)-1].Reconciliation = fmt.Sprintf("actual billing settled; released %d microdollars", release)
		}
		report.LedgerBalance = l.Balance()
		if err := persistReport(reportPath, report); err != nil {
			return report, err
		}
	}
	report.EndedAt = time.Now().UTC()
	report.Gates = assessGates(report)
	if err := persistReport(reportPath, report); err != nil {
		return report, err
	}
	return report, nil
}

func requirePristineState(stateDir string) error {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return fmt.Errorf("probe: inspect state directory under run lock: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == lockName {
			continue
		}
		return fmt.Errorf("probe: pre-existing run state %q; refusing any resend", entry.Name())
	}
	return nil
}

func observationAttempt(attempt Attempt, obs adapter.AttemptObservation, callErr error, credential string, safeRaw []byte) Attempt {
	attempt.State = "observed"
	attempt.RequestSent, attempt.ResponseReceived, attempt.Status = obs.RequestSent, obs.ResponseReceived, obs.Status
	attempt.ProviderRequestID = sanitizeString(obs.RequestID, credential)
	attempt.ResolvedModel = sanitizeString(obs.ResponseModel, credential)
	attempt.ResponseProvider = sanitizeString(obs.ResponseProvider, credential)
	attempt.ResponseHeaders = sanitizeHeaders(obs.ResponseHeaders, credential)
	attempt.StartedAt, attempt.EndedAt, attempt.LatencyNanos = obs.StartedAt, obs.EndedAt, int64(obs.Duration)
	attempt.Billing = billingEvidence(obs.Billing, credential)
	attempt.RawResponseSHA256, attempt.RawTruncated = obs.RawSHA256, obs.RawTruncated
	attempt.ReadError = sanitizeString(obs.ReadError, credential)
	attempt.UsageFields, attempt.IdentityFields, attempt.FinishReasons = responseEvidence(safeRaw)
	if callErr != nil {
		attempt.AdapterError = sanitizeString(callErr.Error(), credential)
	}
	return attempt
}

func billingEvidence(b adapter.BillingObservation, credential string) BillingEvidence {
	convert := func(f adapter.DecimalField) DecimalEvidence {
		raw, withheld, rawSHA256, rawEvidence := screenDecimalRaw(f.Raw, credential)
		return DecimalEvidence{
			Present: f.Present, Null: f.Null, Valid: f.Valid,
			Raw: raw, RawWithheld: withheld, RawSHA256: rawSHA256, RawEvidence: rawEvidence,
			Error: sanitizeString(f.Error, credential),
		}
	}
	convertBool := func(f adapter.BooleanField) BooleanEvidence {
		raw, withheld, rawSHA256, rawEvidence := screenBooleanRaw(f.Raw, credential)
		return BooleanEvidence{
			Present: f.Present, Null: f.Null, Valid: f.Valid, Value: f.Value,
			Raw: raw, RawWithheld: withheld, RawSHA256: rawSHA256, RawEvidence: rawEvidence,
			Error: sanitizeString(f.Error, credential),
		}
	}
	return BillingEvidence{
		Cost: convert(b.Cost), InputTokens: convert(b.InputTokens), OutputTokens: convert(b.OutputTokens), TotalTokens: convert(b.TotalTokens),
		PromptTokenDetails: PromptTokenDetailsEvidence{
			Present: b.PromptTokenDetails.Present, Null: b.PromptTokenDetails.Null, Valid: b.PromptTokenDetails.Valid,
			CachedTokens: convert(b.PromptTokenDetails.CachedTokens), CacheWriteTokens: convert(b.PromptTokenDetails.CacheWriteTokens),
			AudioTokens: convert(b.PromptTokenDetails.AudioTokens), VideoTokens: convert(b.PromptTokenDetails.VideoTokens),
			Error: sanitizeString(b.PromptTokenDetails.Error, credential),
		},
		CompletionTokenDetails: CompletionTokenDetailsEvidence{
			Present: b.CompletionTokenDetails.Present, Null: b.CompletionTokenDetails.Null, Valid: b.CompletionTokenDetails.Valid,
			ReasoningTokens: convert(b.CompletionTokenDetails.ReasoningTokens), ImageTokens: convert(b.CompletionTokenDetails.ImageTokens),
			AudioTokens:              convert(b.CompletionTokenDetails.AudioTokens),
			AcceptedPredictionTokens: convert(b.CompletionTokenDetails.AcceptedPredictionTokens),
			RejectedPredictionTokens: convert(b.CompletionTokenDetails.RejectedPredictionTokens),
			Error:                    sanitizeString(b.CompletionTokenDetails.Error, credential),
		},
		CostDetails: CostDetailsEvidence{
			Present: b.CostDetails.Present, Null: b.CostDetails.Null, Valid: b.CostDetails.Valid,
			UpstreamInferenceCost:            convert(b.CostDetails.UpstreamInferenceCost),
			UpstreamInferencePromptCost:      convert(b.CostDetails.UpstreamInferencePromptCost),
			UpstreamInferenceCompletionsCost: convert(b.CostDetails.UpstreamInferenceCompletionsCost),
			Error:                            sanitizeString(b.CostDetails.Error, credential),
		},
		IsBYOK: convertBool(b.IsBYOK), RateSemanticsDiscrepancy: sanitizeString(b.RateSemanticsDiscrepancy, credential),
		Error: sanitizeString(b.Error, credential),
	}
}

func screenBooleanRaw(raw, credential string) (safe string, withheld bool, rawSHA256, evidence string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "true" || trimmed == "false" {
		return raw, false, "", ""
	}
	return screenDecimalRaw(raw, credential)
}

func screenDecimalRaw(raw, credential string) (safe string, withheld bool, rawSHA256, evidence string) {
	if raw == "" {
		return "", false, "", ""
	}
	if isJSONNumberToken(raw) {
		return raw, false, "", ""
	}
	if credential != "" {
		screened, redacted, err := screenResponse([]byte(raw), credential)
		if err == nil {
			reason := "invalid nonnumeric billing evidence semantically screened"
			if redacted {
				reason = "invalid nonnumeric billing evidence credential-redacted"
			}
			return string(screened), false, "", reason
		}
	}
	return "", true, hash([]byte(raw)), "invalid nonnumeric billing evidence withheld; hash retained"
}

func isJSONNumberToken(raw string) bool {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return false
	}
	if _, ok := value.(json.Number); !ok {
		return false
	}
	var trailing any
	return dec.Decode(&trailing) == io.EOF
}

func responseEvidence(raw []byte) (map[string]json.RawMessage, map[string]json.RawMessage, []string) {
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return nil, nil, nil
	}
	usage := map[string]json.RawMessage{}
	if value, ok := top["usage"]; ok {
		_ = json.Unmarshal(value, &usage)
	}
	identity := map[string]json.RawMessage{}
	for key, value := range top {
		lower := strings.ToLower(key)
		if key == "id" || strings.Contains(lower, "model") || strings.Contains(lower, "provider") {
			identity[key] = value
		}
	}
	var envelope struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	_ = json.Unmarshal(raw, &envelope)
	var reasons []string
	for _, choice := range envelope.Choices {
		if choice.FinishReason != "" {
			reasons = append(reasons, choice.FinishReason)
		}
	}
	return usage, identity, reasons
}

func requireCompleteBilling(b adapter.BillingObservation) error {
	for name, field := range map[string]adapter.DecimalField{
		"cost": b.Cost, "input tokens": b.InputTokens, "output tokens": b.OutputTokens,
	} {
		if !field.Present || field.Null || !field.Valid {
			return fmt.Errorf("billing %s missing, null, or invalid", name)
		}
	}
	return nil
}

type parsedBilling struct {
	cost         ledger.MicroUnit
	inputTokens  int64
	outputTokens int64
}

func parseBilling(b adapter.BillingObservation) (parsedBilling, error) {
	if err := requireCompleteBilling(b); err != nil {
		return parsedBilling{}, err
	}
	cost, err := ledger.MicroUnitFromBaseString(b.Cost.Number.String())
	if err != nil {
		return parsedBilling{}, fmt.Errorf("convert cost: %w", err)
	}
	inputTokens, err := strconv.ParseInt(b.InputTokens.Number.String(), 10, 64)
	if err != nil {
		return parsedBilling{}, fmt.Errorf("convert input tokens: %w", err)
	}
	outputTokens, err := strconv.ParseInt(b.OutputTokens.Number.String(), 10, 64)
	if err != nil {
		return parsedBilling{}, fmt.Errorf("convert output tokens: %w", err)
	}
	return parsedBilling{cost: cost, inputTokens: inputTokens, outputTokens: outputTokens}, nil
}

func reconcileAnomalousBilling(ctx context.Context, l *ledger.Ledger, ref string, reserved ledger.MicroUnit, billing parsedBilling, usageOverrun bool, attempt *Attempt, credential string) {
	if billing.cost == 0 {
		attempt.State = "anomalous-zero-reservation-retained"
		attempt.Reconciliation = "explicit valid zero recorded on rejected/anomalous response; full reservation retained"
		return
	}
	if billing.cost <= reserved && !usageOverrun {
		attempt.State = "anomalous-positive-reservation-retained"
		attempt.Reconciliation = "positive billing is within the reservation; full reservation retained because rejected/anomalous evidence is not a normal settlement"
		return
	}
	release, err := l.Settle(ctx, ref, billing.cost, billing.inputTokens, billing.outputTokens, 0, 0)
	attempt.State = "accounted-halt"
	if err != nil {
		attempt.Reconciliation = "positive billing submitted to accepted ledger before halt: " + sanitizeString(err.Error(), credential)
		return
	}
	attempt.Reconciliation = fmt.Sprintf("anomalous positive billing settled before halt; released %d microdollars", release)
}

func sanitizeString(value, credential string) string {
	if credential == "" || value == "" {
		return value
	}
	return strings.ReplaceAll(value, credential, "[REDACTED_CREDENTIAL]")
}

func screenResponse(value []byte, credential string) ([]byte, bool, error) {
	if len(value) == 0 || credential == "" {
		return append([]byte(nil), value...), false, nil
	}
	dec := json.NewDecoder(bytes.NewReader(value))
	dec.UseNumber()
	var out bytes.Buffer
	redacted, err := screenJSONValue(dec, &out, credential)
	if err != nil {
		return nil, false, fmt.Errorf("JSON response is not safely screenable; retain original hash only: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			err = errors.New("multiple top-level JSON values")
		}
		return nil, false, fmt.Errorf("JSON response is not safely screenable; retain original hash only: %w", err)
	}
	if !redacted {
		return append([]byte(nil), value...), false, nil
	}
	return out.Bytes(), true, nil
}

func screenJSONValue(dec *json.Decoder, out *bytes.Buffer, credential string) (bool, error) {
	token, err := dec.Token()
	if err != nil {
		return false, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			out.WriteByte('{')
			redacted := false
			for index := 0; dec.More(); index++ {
				if index > 0 {
					out.WriteByte(',')
				}
				keyToken, err := dec.Token()
				if err != nil {
					return false, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return false, errors.New("object key is not a string")
				}
				if strings.Contains(key, credential) {
					return false, errors.New("credential appears in a decoded JSON object key")
				}
				encodedKey, _ := json.Marshal(key)
				out.Write(encodedKey)
				out.WriteByte(':')
				childRedacted, err := screenJSONValue(dec, out, credential)
				if err != nil {
					return false, err
				}
				redacted = redacted || childRedacted
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim('}') {
				return false, errors.New("unterminated JSON object")
			}
			out.WriteByte('}')
			return redacted, nil
		case '[':
			out.WriteByte('[')
			redacted := false
			for index := 0; dec.More(); index++ {
				if index > 0 {
					out.WriteByte(',')
				}
				childRedacted, err := screenJSONValue(dec, out, credential)
				if err != nil {
					return false, err
				}
				redacted = redacted || childRedacted
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim(']') {
				return false, errors.New("unterminated JSON array")
			}
			out.WriteByte(']')
			return redacted, nil
		default:
			return false, fmt.Errorf("unexpected JSON delimiter %q", value)
		}
	case string:
		safe := sanitizeString(value, credential)
		encoded, _ := json.Marshal(safe)
		out.Write(encoded)
		return safe != value, nil
	case json.Number:
		out.WriteString(value.String())
		return false, nil
	case bool:
		if value {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
		return false, nil
	case nil:
		out.WriteString("null")
		return false, nil
	default:
		return false, fmt.Errorf("unsupported JSON token %T", token)
	}
}

func sanitizeHeaders(headers map[string]string, credential string) map[string]string {
	if headers == nil {
		return nil
	}
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		out[sanitizeString(key, credential)] = sanitizeString(value, credential)
	}
	return out
}

func persistRaw(stateDir string, request Request, raw []byte) (string, string, error) {
	if len(raw) == 0 {
		return "", "", nil
	}
	dir := filepath.Join(stateDir, "evidence")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	name := fmt.Sprintf("%02d-%s.json", request.Ordinal, request.ID)
	path := filepath.Join(dir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", "", err
	}
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	return filepath.ToSlash(filepath.Join("evidence", name)), hash(raw), err
}

func persistReport(path string, report *Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func halt(path string, report *Report, reason string, l *ledger.Ledger) (*Report, error) {
	reason = sanitizeString(reason, report.credential)
	report.Halted, report.HaltReason, report.EndedAt = true, reason, time.Now().UTC()
	if l != nil {
		report.LedgerBalance = l.Balance()
	}
	report.Gates = assessGates(report)
	if err := persistReport(path, report); err != nil {
		return report, fmt.Errorf("probe: %s; persist halt: %w", reason, err)
	}
	return report, errors.New("probe: " + reason)
}

func initialGates() []Gate {
	return []Gate{
		{ID: 1, Name: "alias-to-response-pin mappings", Status: "UNRESOLVED", Reason: "no live response evidence"},
		{ID: 2, Name: "response-linked provider identity", Status: "UNRESOLVED", Reason: "no live response evidence"},
		{ID: 3, Name: "Nano output-limit semantics and billing categories", Status: "UNRESOLVED", Reason: "a short accepted response cannot establish full output-limit or billable-category semantics"},
		{ID: 4, Name: "finite Jev billed-output bound", Status: "OPEN", Reason: "an observed output count is not an authoritative maximum"},
		{ID: 5, Name: "current rates, currency, and fee semantics", Status: "UNRESOLVED", Reason: "a returned cost does not establish tariff, currency, or fee semantics"},
		{ID: 6, Name: "tokenizer-backed frozen-payload input bounds", Status: "UNRESOLVED", Reason: "largest serialized bytes is not proof of largest tokenizer count across all frozen payloads"},
	}
}

func assessGates(report *Report) []Gate {
	gates := initialGates()
	if report.Halted {
		for i := range gates {
			gates[i].Reason = "probe halted: " + report.HaltReason
		}
		return gates
	}
	jevNanoCovered := scopeCovered(report.SelectedRequests, report.Attempts, []pilot.Arm{pilot.ArmJev, pilot.ArmNano}, "")
	if jevNanoCovered {
		gates[0] = Gate{ID: 1, Name: gates[0].Name, Status: "EVIDENCE_COLLECTED", Reason: "selected Jev and Nano requests have identity-matched accepted attempts; immutable alias semantics still require review"}
		gates[1] = Gate{ID: 2, Name: gates[1].Name, Status: "EVIDENCE_COLLECTED", Reason: "selected Jev and Nano requests have response-linked provider fields matching candidates; request-side routing enforcement remains unproved"}
	}
	if scopeCovered(report.SelectedRequests, report.Attempts, []pilot.Arm{pilot.ArmNano}, "") {
		gates[2] = Gate{ID: 3, Name: gates[2].Name, Status: "UNRESOLVED", Reason: "selected Nano requests were accepted with usage, but short responses do not establish full output-limit semantics or all billable categories"}
	}
	if scopeCovered(report.SelectedRequests, report.Attempts, []pilot.Arm{pilot.ArmJev}, "") {
		gates[3] = Gate{ID: 4, Name: gates[3].Name, Status: "OPEN", Reason: "observed Jev output usage is not an authoritative finite maximum unless the response explicitly supplies one"}
	}
	if jevNanoCovered {
		gates[4] = Gate{ID: 5, Name: gates[4].Name, Status: "UNRESOLVED", Reason: "returned costs and usage are preserved, but do not establish current tariff, currency, or fee semantics"}
	}
	if scopeCovered(report.SelectedRequests, report.Attempts, []pilot.Arm{pilot.ArmJev, pilot.ArmNano}, "largest-frozen-payload") {
		gates[5] = Gate{ID: 6, Name: gates[5].Name, Status: "UNRESOLVED", Reason: "provider counts cover largest-by-byte samples only; largest bytes is not largest tokens and the other frozen payloads remain unmeasured"}
	}
	return gates
}
