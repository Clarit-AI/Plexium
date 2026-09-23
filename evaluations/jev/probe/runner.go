package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	Endpoints     Endpoints
	HTTPClient    *http.Client
	Timeout       time.Duration
	JevPin        string
	JevProvider   string
	NanoPin       string
	NanoProvider  string
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
	Present bool   `json:"present"`
	Null    bool   `json:"null"`
	Valid   bool   `json:"valid"`
	Raw     string `json:"raw,omitempty"`
	Error   string `json:"error,omitempty"`
}

type BillingEvidence struct {
	Cost         DecimalEvidence `json:"cost"`
	InputTokens  DecimalEvidence `json:"inputTokens"`
	OutputTokens DecimalEvidence `json:"outputTokens"`
	TotalTokens  DecimalEvidence `json:"totalTokens"`
	Error        string          `json:"error,omitempty"`
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
	RawResponseSHA256 string                     `json:"rawResponseSha256,omitempty"`
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
	Version         string           `json:"version"`
	InventorySHA256 string           `json:"inventorySha256"`
	AuthorizedCap   ledger.MicroUnit `json:"authorizedCapMicrodollars"`
	ProbeSubcap     ledger.MicroUnit `json:"probeSubcapMicrodollars"`
	ReservedTotal   ledger.MicroUnit `json:"reservedTotalMicrodollars"`
	LedgerBalance   ledger.MicroUnit `json:"ledgerBalanceMicrodollars"`
	StartedAt       time.Time        `json:"startedAt"`
	EndedAt         time.Time        `json:"endedAt,omitempty"`
	Halted          bool             `json:"halted"`
	HaltReason      string           `json:"haltReason,omitempty"`
	Attempts        []Attempt        `json:"attempts"`
	Gates           []Gate           `json:"gates"`
}

func Run(ctx context.Context, cfg RunConfig) (*Report, error) {
	return runWithLimits(ctx, cfg, runLimits{AuthorizedCap: AuthorizedCap, ProbeSubcap: ProbeSubcap})
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
	reportPath := filepath.Join(cfg.StateDir, reportName)
	if _, err := os.Stat(reportPath); err == nil {
		return nil, errors.New("probe: report already exists; refusing any resend")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("probe: inspect report: %w", err)
	}
	lockPath := filepath.Join(cfg.StateDir, lockName)
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("probe: acquire run lock: %w", err)
	}
	_ = lock.Close()
	defer os.Remove(lockPath)

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
	report := &Report{
		Version: PlanVersion, InventorySHA256: plan.InventorySHA256, AuthorizedCap: limits.AuthorizedCap,
		ProbeSubcap: limits.ProbeSubcap, StartedAt: time.Now().UTC(), Gates: initialGates(),
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

	for _, request := range plan.Requests {
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
			FixtureID: request.FixtureID, RequestedAlias: request.RequestedAlias,
			ExpectedPin: request.CandidatePin, ExpectedProvider: request.CandidateProvider,
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
		attempt = observationAttempt(attempt, obs, err)
		rawPath, rawErr := persistRaw(cfg.StateDir, request, obs.RawResponse)
		if rawErr == nil && len(obs.RawResponse) > 0 {
			attempt.RawEvidencePath = rawPath
		}
		report.Attempts[len(report.Attempts)-1] = attempt
		report.LedgerBalance = l.Balance()
		if persistErr := persistReport(reportPath, report); persistErr != nil {
			return report, persistErr
		}
		if rawErr != nil {
			return halt(reportPath, report, "persist raw evidence: "+rawErr.Error(), l)
		}
		if err != nil {
			return halt(reportPath, report, request.ID+": "+err.Error(), l)
		}
		if billingErr := requireCompleteBilling(obs.Billing); billingErr != nil {
			return halt(reportPath, report, request.ID+": "+billingErr.Error(), l)
		}
		cost, parseErr := ledger.MicroUnitFromBaseString(obs.Billing.Cost.Number.String())
		if parseErr != nil {
			return halt(reportPath, report, request.ID+": convert cost: "+parseErr.Error(), l)
		}
		inputTokens, _ := strconv.ParseInt(obs.Billing.InputTokens.Number.String(), 10, 64)
		outputTokens, _ := strconv.ParseInt(obs.Billing.OutputTokens.Number.String(), 10, 64)
		if inputTokens > accountingInBound || outputTokens > accountingOutBound {
			return halt(reportPath, report, request.ID+": usage exceeds conservative accounting envelope", l)
		}
		if cost == 0 {
			report.Attempts[len(report.Attempts)-1].State = "observed-zero-reservation-retained"
			report.Attempts[len(report.Attempts)-1].Reconciliation = "explicit valid zero recorded; full reservation retained under accepted ledger semantics"
		} else {
			release, settleErr := l.Settle(ctx, ref, cost, inputTokens, outputTokens, 0, 0)
			if settleErr != nil {
				return halt(reportPath, report, request.ID+": settle: "+settleErr.Error(), l)
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

func observationAttempt(attempt Attempt, obs adapter.AttemptObservation, callErr error) Attempt {
	attempt.State = "observed"
	attempt.RequestSent, attempt.ResponseReceived, attempt.Status = obs.RequestSent, obs.ResponseReceived, obs.Status
	attempt.ProviderRequestID, attempt.ResolvedModel, attempt.ResponseProvider = obs.RequestID, obs.ResponseModel, obs.ResponseProvider
	attempt.ResponseHeaders = obs.ResponseHeaders
	attempt.StartedAt, attempt.EndedAt, attempt.LatencyNanos = obs.StartedAt, obs.EndedAt, int64(obs.Duration)
	attempt.Billing = billingEvidence(obs.Billing)
	attempt.RawResponseSHA256, attempt.RawTruncated, attempt.ReadError = obs.RawSHA256, obs.RawTruncated, obs.ReadError
	attempt.UsageFields, attempt.IdentityFields, attempt.FinishReasons = responseEvidence(obs.RawResponse)
	if callErr != nil {
		attempt.AdapterError = callErr.Error()
	}
	return attempt
}

func billingEvidence(b adapter.BillingObservation) BillingEvidence {
	convert := func(f adapter.DecimalField) DecimalEvidence {
		return DecimalEvidence{Present: f.Present, Null: f.Null, Valid: f.Valid, Raw: f.Raw, Error: f.Error}
	}
	return BillingEvidence{Cost: convert(b.Cost), InputTokens: convert(b.InputTokens), OutputTokens: convert(b.OutputTokens), TotalTokens: convert(b.TotalTokens), Error: b.Error}
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

func persistRaw(stateDir string, request Request, raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	dir := filepath.Join(stateDir, "evidence")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%02d-%s.json", request.Ordinal, request.ID)
	path := filepath.Join(dir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	return filepath.ToSlash(filepath.Join("evidence", name)), err
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
		{ID: 1, Name: "alias-to-response-pin mappings", Status: "open", Reason: "no live response evidence"},
		{ID: 2, Name: "response-linked provider identity", Status: "open", Reason: "no live response evidence"},
		{ID: 3, Name: "Nano output-limit semantics and billing categories", Status: "open", Reason: "no live response evidence"},
		{ID: 4, Name: "finite Jev billed-output bound", Status: "open", Reason: "an observed output count is not an authoritative maximum"},
		{ID: 5, Name: "current rates, currency, and fee semantics", Status: "open", Reason: "requires real response billing fields"},
		{ID: 6, Name: "tokenizer-backed frozen-payload input bounds", Status: "open", Reason: "requires largest-payload provider usage"},
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
	if len(report.Attempts) != MaxRequests {
		return gates
	}
	gates[0] = Gate{ID: 1, Name: gates[0].Name, Status: "evidence-collected", Reason: "requested aliases and exact returned model strings are recorded for all four responses"}
	gates[1] = Gate{ID: 2, Name: gates[1].Name, Status: "evidence-collected", Reason: "response-linked provider fields matched the configured candidates on all responses"}
	gates[2] = Gate{ID: 3, Name: gates[2].Name, Status: "evidence-collected", Reason: "Nano accepted max_tokens=256; finish reasons and every returned usage field are preserved for review"}
	gates[3] = Gate{ID: 4, Name: gates[3].Name, Status: "open", Reason: "the probe records observed Jev output usage but cannot infer an authoritative finite maximum unless the raw response explicitly supplies one"}
	gates[4] = Gate{ID: 5, Name: gates[4].Name, Status: "evidence-collected", Reason: "raw usage/billing categories and exact cost tokens are preserved; currency and fee meaning require review of returned fields"}
	gates[5] = Gate{ID: 6, Name: gates[5].Name, Status: "evidence-collected", Reason: "provider-reported input tokens are recorded for each arm's exact largest frozen request hash"}
	return gates
}
