package probe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
	"github.com/Clarit-AI/Plexium/evaluations/jev/ledger"
	"github.com/Clarit-AI/Plexium/evaluations/jev/pilot"
)

const (
	PlanVersion          = "jev-pin-probe-v1"
	CredentialEnv        = "OPENROUTER_API_KEY"
	JevEndpoint          = "https://openrouter.ai/api/alpha/decisions"
	NanoEndpoint         = "https://openrouter.ai/api/v1/chat/completions"
	JevAlias             = "typesafe/jev-1.13"
	JevCandidatePin      = "typesafe/jev-1.13-20260917"
	JevCandidateProvider = "TypeSafe"
	NanoAlias            = "openai/gpt-4.1-nano"
	NanoCandidatePin     = "openai/gpt-4.1-nano"
	NanoProvider         = "OpenAI"
	AuthorizedCap        = ledger.MicroUnit(1_000_000)
	ProbeSubcap          = ledger.MicroUnit(200_000)
	ReservationPerCall   = ledger.MicroUnit(50_000)
	MaxRequests          = 4
)

type Request struct {
	Ordinal           int              `json:"ordinal"`
	ID                string           `json:"id"`
	Arm               pilot.Arm        `json:"arm"`
	Kind              string           `json:"kind"`
	FixtureID         string           `json:"fixtureId,omitempty"`
	Endpoint          string           `json:"endpoint"`
	RequestedAlias    string           `json:"requestedAlias"`
	CandidatePin      string           `json:"candidateResponsePin"`
	CandidateProvider string           `json:"candidateProvider"`
	Purpose           []string         `json:"purpose"`
	RequestSHA256     string           `json:"requestSha256"`
	RequestBytes      int              `json:"requestBytes"`
	Reservation       ledger.MicroUnit `json:"reservationMicrodollars"`
	Body              []byte           `json:"-"`
}

type Plan struct {
	Version              string           `json:"version"`
	InventorySHA256      string           `json:"inventorySha256"`
	CredentialEnv        string           `json:"credentialEnv"`
	AuthorizedCap        ledger.MicroUnit `json:"authorizedCapMicrodollars"`
	ProbeSubcap          ledger.MicroUnit `json:"probeSubcapMicrodollars"`
	ReservationPerCall   ledger.MicroUnit `json:"reservationPerCallMicrodollars"`
	Requests             []Request        `json:"requests"`
	FailClosedConditions []string         `json:"failClosedConditions"`
}

func BuildPlan(inventoryPath string, endpoints Endpoints) (*Plan, error) {
	data, err := os.ReadFile(inventoryPath)
	if err != nil {
		return nil, fmt.Errorf("probe: read inventory: %w", err)
	}
	var inv pilot.Inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return nil, fmt.Errorf("probe: decode inventory: %w", err)
	}
	if err := pilot.ValidateInventory(&inv); err != nil {
		return nil, fmt.Errorf("probe: validate inventory: %w", err)
	}
	if endpoints.Jev == "" || endpoints.Nano == "" {
		return nil, errors.New("probe: both endpoints are required")
	}

	microJev, err := buildMicroJev()
	if err != nil {
		return nil, err
	}
	microNano, err := buildMicroNano()
	if err != nil {
		return nil, err
	}
	largeJev, largeNano, err := largestPayloads(&inv)
	if err != nil {
		return nil, err
	}

	requests := []Request{
		newRequest(1, "jev-contract", pilot.ArmJev, "purpose-built-micro", "", endpoints.Jev, JevAlias, JevCandidatePin, JevCandidateProvider, microJev,
			"observe exact alias-to-pin resolution", "observe Decisions response-linked provider and billing envelope"),
		newRequest(2, "nano-contract", pilot.ArmNano, "purpose-built-micro", "", endpoints.Nano, NanoAlias, NanoCandidatePin, NanoProvider, microNano,
			"observe exact alias-to-pin and OpenAI provider identity", "observe max_tokens acceptance, finish state, and complete usage/billing categories"),
		newRequest(3, "jev-largest-frozen", pilot.ArmJev, "largest-frozen-payload", largeJev.fixtureID, endpoints.Jev, JevAlias, JevCandidatePin, JevCandidateProvider, largeJev.body,
			"measure provider-tokenized input for the largest frozen Jev payload", "observe billed output tokens on a real tuning payload"),
		newRequest(4, "nano-largest-frozen", pilot.ArmNano, "largest-frozen-payload", largeNano.fixtureID, endpoints.Nano, NanoAlias, NanoCandidatePin, NanoProvider, largeNano.body,
			"measure provider-tokenized input for the largest frozen Nano payload", "confirm structured output and usage fields on a real tuning payload"),
	}
	for _, request := range requests {
		if err := validateRequest(request); err != nil {
			return nil, fmt.Errorf("probe: validate %s: %w", request.ID, err)
		}
	}
	return &Plan{
		Version: PlanVersion, InventorySHA256: inv.InventoryHash, CredentialEnv: CredentialEnv,
		AuthorizedCap: AuthorizedCap, ProbeSubcap: ProbeSubcap, ReservationPerCall: ReservationPerCall,
		Requests: requests,
		FailClosedConditions: []string{
			"any transport, HTTP, schema, model-pin, or provider-pin error",
			"missing, null, invalid, rejected, or incomplete billing evidence",
			"reservation, probe-subcap, or authorized-cap refusal",
			"token usage outside the conservative accounting envelope",
			"existing state, uncertain prior send, raw-evidence write failure, or report drift",
		},
	}, nil
}

type selectedPayload struct {
	fixtureID string
	body      []byte
}

func largestPayloads(inv *pilot.Inventory) (selectedPayload, selectedPayload, error) {
	entries := append([]pilot.InventoryEntry(nil), inv.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Input.FixtureID < entries[j].Input.FixtureID })
	selected := map[pilot.Arm]selectedPayload{}
	for _, entry := range entries {
		for _, payload := range entry.Payloads {
			current := selected[payload.Arm]
			if len(payload.Body) > len(current.body) {
				selected[payload.Arm] = selectedPayload{fixtureID: entry.Input.FixtureID, body: append([]byte(nil), payload.Body...)}
			}
		}
	}
	jev, jok := selected[pilot.ArmJev]
	nano, nok := selected[pilot.ArmNano]
	if !jok || !nok {
		return selectedPayload{}, selectedPayload{}, errors.New("probe: inventory does not contain both arms")
	}
	return jev, nano, nil
}

func buildMicroJev() ([]byte, error) {
	req := adapter.DecisionRequest{
		Model: JevAlias,
		Questions: map[string]adapter.DecisionQuestion{adapter.DecisionVerdictID: {
			Type: "choice", Instructions: "Choose exactly one supplied label from the evidence.",
			Criteria: map[string]adapter.DecisionCriteria{
				"supported":             {Description: "The evidence supports the claim."},
				"insufficient-evidence": {Description: "The evidence does not decide the claim."},
			},
		}},
		State: adapter.State{ID: "pin-probe-jev", Docs: []string{"The beacon is blue."}},
	}
	return json.Marshal(req)
}

func buildMicroNano() ([]byte, error) {
	zero, max := 0.0, 256
	allow, require := false, true
	req := adapter.ChatRequest{
		Model: NanoAlias,
		Messages: []adapter.ChatMessage{
			{Role: "system", Content: "Return exactly one allowed label."},
			{Role: "user", Content: "Evidence: The beacon is blue. Claim: The beacon is blue."},
		},
		ResponseFormat: &adapter.ResponseFormat{Type: "json_schema", JSONSchema: &adapter.JSONSchema{
			Name: "classification", Strict: true,
			Schema: map[string]any{
				"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string", "enum": []string{"supported", "insufficient-evidence"}}},
				"required": []string{"label"}, "additionalProperties": false,
			},
		}},
		Temperature: &zero, MaxTokens: &max,
		Provider: &adapter.ProviderRoute{Only: []string{NanoProvider}, AllowFallbacks: &allow, RequireParameters: &require},
	}
	return json.Marshal(req)
}

func newRequest(ordinal int, id string, arm pilot.Arm, kind, fixtureID, endpoint, alias, pin, provider string, body []byte, purpose ...string) Request {
	return Request{
		Ordinal: ordinal, ID: id, Arm: arm, Kind: kind, FixtureID: fixtureID, Endpoint: endpoint,
		RequestedAlias: alias, CandidatePin: pin, CandidateProvider: provider,
		Purpose: purpose, RequestSHA256: hash(body), RequestBytes: len(body), Reservation: ReservationPerCall,
		Body: append([]byte(nil), body...),
	}
}

func validateRequest(request Request) error {
	if request.RequestSHA256 != hash(request.Body) || request.Reservation != ReservationPerCall {
		return errors.New("body hash or reservation drift")
	}
	switch request.Arm {
	case pilot.ArmJev:
		_, err := adapter.ValidateDecisionFrozenBody(request.Body, request.RequestedAlias)
		return err
	case pilot.ArmNano:
		_, _, err := adapter.ValidateChatFrozenBody(request.Body, request.RequestedAlias, request.CandidateProvider)
		return err
	default:
		return fmt.Errorf("unsupported arm %q", request.Arm)
	}
}

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
