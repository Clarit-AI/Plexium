package pilot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sort"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
	"github.com/Clarit-AI/Plexium/evaluations/jev/loader"
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

const (
	InventoryVersion = "jev-pilot-inventory-v1"
	ScheduleVersion  = "fisher-yates-go-v1-paired-alternating"
	ScheduleSeed     = int64(579)
	ArmJev           = Arm("jev")
	ArmNano          = Arm("nano")
)

type Arm string

type BuildConfig struct {
	JevRequestModel  string
	NanoRequestModel string
	NanoProvider     string
}

type GoldFreeInput struct {
	FixtureID     string              `json:"fixtureId"`
	Task          protocol.Task       `json:"task"`
	SourceGroup   string              `json:"sourceGroup"`
	Question      string              `json:"question"`
	EdgeSourceID  string              `json:"edgeSourceId,omitempty"`
	EdgeTargetID  string              `json:"edgeTargetId,omitempty"`
	Excerpts      []protocol.Excerpt  `json:"excerpts"`
	Candidates    []GoldFreeCandidate `json:"candidates"`
	AllowedLabels []string            `json:"allowedLabels"`
}

type GoldFreeCandidate struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Alias string `json:"alias,omitempty"`
}

type Payload struct {
	Arm       Arm             `json:"arm"`
	Body      json.RawMessage `json:"body"`
	SHA256    string          `json:"sha256"`
	InputHash string          `json:"inputHash"`
}

type InventoryEntry struct {
	Input    GoldFreeInput `json:"input"`
	Payloads []Payload     `json:"payloads"`
}

type Slot struct {
	Ordinal    int    `json:"ordinal"`
	FixtureID  string `json:"fixtureId"`
	Arm        Arm    `json:"arm"`
	Repetition int    `json:"repetition"`
	PayloadSHA string `json:"payloadSha256"`
}

type CorpusReference struct {
	FixtureFileSHA string `json:"fixtureFileSha256"`
	ManifestSHA    string `json:"manifestSha256"`
	FixtureCount   int    `json:"fixtureCount"`
	Split          string `json:"split"`
}

type Inventory struct {
	Version       string           `json:"version"`
	Protocol      string           `json:"protocolVersion"`
	Rubric        string           `json:"rubric"`
	RubricSHA256  string           `json:"rubricSha256"`
	Corpus        CorpusReference  `json:"corpusReference"`
	Entries       []InventoryEntry `json:"entries"`
	ScheduleSeed  int64            `json:"scheduleSeed"`
	ScheduleAlgo  string           `json:"scheduleAlgorithm"`
	Schedule      []Slot           `json:"schedule"`
	InventoryHash string           `json:"inventorySha256"`
}

const CommonRubric = `Classify only the supplied question, evidence, candidates, and relationship endpoints. Use the exact allowed-label vocabulary. For document typing, classify the primary subject: document includes treaties, accords, and identifiable formal agreements; paper applies only when the publication itself is the subject; a named region's environmental characteristics are place, while climate as a general phenomenon is concept. Use insufficient-evidence when the supplied evidence cannot distinguish an allowed label. For relationships, used-by requires explicit functional use, ownership supports association but not part-of, and absence supports no-supported-relationship only when the case-specific question and evidence make that absence explicit. For claim support, contradicted requires incompatible evidence rather than assumed completeness.`

func BuildInventory(fixturePath, manifestPath string, cfg BuildConfig) (*Inventory, error) {
	if cfg.JevRequestModel == "" || cfg.NanoRequestModel == "" || cfg.NanoProvider == "" {
		return nil, errors.New("pilot: all request aliases and Nano provider are required")
	}
	loaded, err := loader.Load(fixturePath, manifestPath)
	if err != nil {
		return nil, err
	}
	if loaded.Drift != nil {
		return nil, errors.New("pilot: frozen corpus or manifest has drift")
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("pilot: read corpus manifest for digest: %w", err)
	}
	fixtures := loader.FilterBySplit(loaded.Fixtures, protocol.SplitTuning)
	if len(fixtures) != 24 {
		return nil, fmt.Errorf("pilot: frozen tuning packet has %d fixtures, want 24", len(fixtures))
	}
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].ID < fixtures[j].ID })
	inv := &Inventory{
		Version: InventoryVersion, Protocol: protocol.ProtocolVersion, Rubric: CommonRubric,
		RubricSHA256: hashBytes([]byte(CommonRubric)), ScheduleSeed: ScheduleSeed, ScheduleAlgo: ScheduleVersion,
		Corpus: CorpusReference{FixtureFileSHA: loaded.Manifest.SHA256FixtureFile, ManifestSHA: hashBytes(manifestBytes), FixtureCount: len(fixtures), Split: string(protocol.SplitTuning)},
	}
	for _, f := range fixtures {
		if f.ReviewStatus != protocol.ReviewApproved || f.Reviewer != "KHAEntertainment" {
			return nil, fmt.Errorf("pilot: fixture %s is not frozen human-approved tuning data", f.ID)
		}
		input := projectInput(f)
		inputBytes, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		jev, err := buildJevPayload(input, cfg.JevRequestModel)
		if err != nil {
			return nil, err
		}
		nano, err := buildNanoPayload(input, cfg.NanoRequestModel, cfg.NanoProvider)
		if err != nil {
			return nil, err
		}
		inv.Entries = append(inv.Entries, InventoryEntry{Input: input, Payloads: []Payload{
			{Arm: ArmJev, Body: jev, SHA256: hashBytes(jev), InputHash: hashBytes(inputBytes)},
			{Arm: ArmNano, Body: nano, SHA256: hashBytes(nano), InputHash: hashBytes(inputBytes)},
		}})
	}
	inv.Schedule = buildSchedule(inv.Entries)
	canonical, err := inventoryHashBytes(inv)
	if err != nil {
		return nil, err
	}
	inv.InventoryHash = hashBytes(canonical)
	return inv, nil
}

func projectInput(f protocol.Fixture) GoldFreeInput {
	candidates := make([]GoldFreeCandidate, len(f.Candidates))
	for i, candidate := range f.Candidates {
		candidates[i] = GoldFreeCandidate{ID: candidate.ID, Title: candidate.Title, Alias: candidate.Alias}
	}
	return GoldFreeInput{FixtureID: f.ID, Task: f.Task, SourceGroup: f.SourceGroup, Question: f.Question,
		EdgeSourceID: f.EdgeSourceID, EdgeTargetID: f.EdgeTargetID,
		Excerpts: append([]protocol.Excerpt(nil), f.Excerpts...), Candidates: candidates,
		AllowedLabels: append([]string(nil), f.AllowedLabels...)}
}

func buildJevPayload(input GoldFreeInput, model string) ([]byte, error) {
	criteria := make(map[string]adapter.DecisionCriteria, len(input.AllowedLabels))
	for _, label := range input.AllowedLabels {
		criteria[label] = adapter.DecisionCriteria{Description: "Return " + label + " only when supported by the shared rubric and supplied evidence."}
	}
	docs := make([]string, 0, len(input.Excerpts)+1)
	for _, e := range input.Excerpts {
		docs = append(docs, e.ID+": "+e.Text)
	}
	inputJSON, _ := json.Marshal(input)
	docs = append(docs, "structured input: "+string(inputJSON))
	req := adapter.DecisionRequest{Model: model, Questions: map[string]adapter.DecisionQuestion{
		adapter.DecisionVerdictID: {Type: "choice", Instructions: CommonRubric + "\nQuestion: " + input.Question, Criteria: criteria},
	}, State: adapter.State{ID: input.FixtureID, System: CommonRubric, Docs: docs}}
	return json.Marshal(req)
}

func buildNanoPayload(input GoldFreeInput, model, provider string) ([]byte, error) {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	zero, max := 0.0, 256
	allow, require := false, true
	req := adapter.ChatRequest{Model: model, Messages: []adapter.ChatMessage{
		{Role: "system", Content: CommonRubric}, {Role: "user", Content: string(inputJSON)},
	}, ResponseFormat: &adapter.ResponseFormat{Type: "json_schema", JSONSchema: &adapter.JSONSchema{Name: "classification", Strict: true, Schema: map[string]any{
		"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string", "enum": input.AllowedLabels}},
		"required": []string{"label"}, "additionalProperties": false,
	}}}, Temperature: &zero, MaxTokens: &max, Provider: &adapter.ProviderRoute{Only: []string{provider}, AllowFallbacks: &allow, RequireParameters: &require}}
	return json.Marshal(req)
}

func buildSchedule(entries []InventoryEntry) []Slot {
	order := make([]int, len(entries))
	for i := range order {
		order[i] = i
	}
	r := rand.New(rand.NewSource(ScheduleSeed))
	for i := len(order) - 1; i > 0; i-- {
		j := r.Intn(i + 1)
		order[i], order[j] = order[j], order[i]
	}
	var slots []Slot
	for pos, idx := range order {
		arms := []Arm{ArmJev, ArmNano}
		if pos%2 == 1 {
			arms[0], arms[1] = arms[1], arms[0]
		}
		for _, arm := range arms {
			p, _ := payloadFor(entries[idx], arm)
			slots = append(slots, Slot{Ordinal: len(slots) + 1, FixtureID: entries[idx].Input.FixtureID, Arm: arm, Repetition: 1, PayloadSHA: p.SHA256})
		}
	}
	return slots
}

func payloadFor(e InventoryEntry, arm Arm) (Payload, bool) {
	for _, p := range e.Payloads {
		if p.Arm == arm {
			return p, true
		}
	}
	return Payload{}, false
}
func hashBytes(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func inventoryHashBytes(inv *Inventory) ([]byte, error) {
	clone := *inv
	clone.InventoryHash = ""
	return json.Marshal(clone)
}

func ValidateInventory(inv *Inventory) error {
	if inv == nil || inv.InventoryHash == "" {
		return errors.New("pilot: inventory hash missing")
	}
	canonical, err := inventoryHashBytes(inv)
	if err != nil || hashBytes(canonical) != inv.InventoryHash {
		return errors.New("pilot: inventory content hash mismatch")
	}
	entries := map[string]InventoryEntry{}
	for _, entry := range inv.Entries {
		if entry.Input.FixtureID == "" {
			return errors.New("pilot: inventory fixture ID missing")
		}
		if _, ok := entries[entry.Input.FixtureID]; ok {
			return fmt.Errorf("pilot: duplicate inventory fixture %s", entry.Input.FixtureID)
		}
		inputBytes, _ := json.Marshal(entry.Input)
		inputHash := hashBytes(inputBytes)
		if len(entry.Payloads) != 2 {
			return fmt.Errorf("pilot: fixture %s has %d payloads, want 2", entry.Input.FixtureID, len(entry.Payloads))
		}
		seen := map[Arm]bool{}
		for _, p := range entry.Payloads {
			if seen[p.Arm] || !(p.Arm == ArmJev || p.Arm == ArmNano) {
				return fmt.Errorf("pilot: fixture %s has invalid arm set", entry.Input.FixtureID)
			}
			seen[p.Arm] = true
			if p.InputHash != inputHash || p.SHA256 != hashBytes(p.Body) {
				return fmt.Errorf("pilot: fixture %s payload hash drift", entry.Input.FixtureID)
			}
		}
		entries[entry.Input.FixtureID] = entry
	}
	seenSlots := map[string]bool{}
	for i, slot := range inv.Schedule {
		if slot.Ordinal != i+1 {
			return fmt.Errorf("pilot: schedule ordinal %d at position %d", slot.Ordinal, i+1)
		}
		entry, ok := entries[slot.FixtureID]
		if !ok {
			return fmt.Errorf("pilot: schedule fixture %s missing", slot.FixtureID)
		}
		p, ok := payloadFor(entry, slot.Arm)
		if !ok || p.SHA256 != slot.PayloadSHA {
			return fmt.Errorf("pilot: schedule payload drift at slot %d", slot.Ordinal)
		}
		key := fmt.Sprintf("%s/%s/%d", slot.FixtureID, slot.Arm, slot.Repetition)
		if seenSlots[key] {
			return fmt.Errorf("pilot: duplicate logical slot %s", key)
		}
		seenSlots[key] = true
	}
	return nil
}
