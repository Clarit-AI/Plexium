package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
	jevcompare "github.com/Clarit-AI/Plexium/evaluations/jev/compare"
	"github.com/Clarit-AI/Plexium/evaluations/jev/ledger"
	"github.com/Clarit-AI/Plexium/evaluations/jev/loader"
	"github.com/Clarit-AI/Plexium/evaluations/jev/pilot"
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
	"github.com/Clarit-AI/Plexium/evaluations/jev/scoring"
)

type armManifest struct {
	Endpoint            string            `json:"endpoint"`
	RequestModel        string            `json:"requestModel"`
	ResponseModel       string            `json:"responseModel"`
	ResponseProvider    string            `json:"responseProvider"`
	APIKeyEnv           string            `json:"apiKeyEnv"`
	LedgerPath          string            `json:"ledgerPath"`
	RatesVersion        string            `json:"ratesVersion"`
	RateEvidence        string            `json:"rateEvidence"`
	TokenBoundEvidence  string            `json:"tokenBoundEvidence"`
	PinMappingEvidence  string            `json:"pinMappingEvidence"`
	ProviderEvidence    string            `json:"providerEvidence"`
	OutputLimitEvidence string            `json:"outputLimitEvidence"`
	Subcap              ledger.MicroUnit  `json:"subcapMicrodollars"`
	Reservation         ledger.MicroUnit  `json:"reservationMicrodollars"`
	RateIn              ledger.MicroUnit  `json:"rateInPerMillionMicrodollars"`
	RateOut             *ledger.MicroUnit `json:"rateOutPerMillionMicrodollars"`
	InputBound          int64             `json:"maxBilledInputTokens"`
	OutputBound         int64             `json:"maxBilledOutputTokens"`
}
type executionManifest struct {
	InventoryPath          string           `json:"inventoryPath"`
	InventoryHash          string           `json:"inventoryHash"`
	AuthorizationReference string           `json:"authorizationReference"`
	AuthorizationCap       ledger.MicroUnit `json:"authorizationCapMicrodollars"`
	FrozenPriorExposure    ledger.MicroUnit `json:"frozenPriorExposureMicrodollars"`
	AllocationID           string           `json:"allocationId"`
	AllocationRecordPath   string           `json:"allocationRecordPath"`
	ProbeReconciliation    string           `json:"probeReconciliationPrecondition"`
	RunID                  string           `json:"runId"`
	JournalPath            string           `json:"journalPath"`
	EvidenceDir            string           `json:"evidenceDir"`
	RunLockPath            string           `json:"runLockPath"`
	CombinedCap            ledger.MicroUnit `json:"combinedCapMicrodollars"`
	LiveContractsVerified  bool             `json:"liveContractsVerified"`
	Jev                    armManifest      `json:"jev"`
	Nano                   armManifest      `json:"nano"`
}

func main() {
	if err := runCLI(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCLI(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: jev-pilot prepare|run|report|compare-sidecar")
	}
	switch args[0] {
	case "prepare":
		return prepare(args[1:])
	case "run":
		return execute(args[1:])
	case "report":
		return report(args[1:])
	case "compare-sidecar":
		return comparisonSidecar(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func comparisonSidecar(args []string) error {
	fs := flag.NewFlagSet("compare-sidecar", flag.ContinueOnError)
	fixtures := fs.String("fixtures", "", "frozen adjudicated fixture JSONL")
	manifest := fs.String("manifest", "", "frozen fixture manifest")
	inventory := fs.String("inventory", "", "frozen request inventory")
	baselineReport := fs.String("baseline-report", "", "offline baseline JSON report")
	pilotReport := fs.String("pilot-report", "", "live-arm pilot JSON report")
	out := fs.String("out", "", "new comparison sidecar path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("compare-sidecar accepts flags only")
	}
	if *out == "" {
		return errors.New("compare-sidecar requires out")
	}
	sidecar, err := jevcompare.Build(jevcompare.Config{FixturesPath: *fixtures, ManifestPath: *manifest, InventoryPath: *inventory, BaselineReport: *baselineReport, LivePilotReport: *pilotReport})
	if err != nil {
		return err
	}
	b, err := jevcompare.Marshal(sidecar)
	if err != nil {
		return err
	}
	return writeFrozen(*out, append(b, '\n'))
}

func prepare(args []string) error {
	fs := flag.NewFlagSet("prepare", flag.ContinueOnError)
	fixtures := fs.String("fixtures", "", "frozen fixture JSONL")
	manifest := fs.String("manifest", "", "frozen corpus manifest")
	out := fs.String("out", "", "new inventory path")
	corpus := fs.String("corpus-reference-out", "", "new corpus reference path")
	jev := fs.String("jev-request-model", "", "Jev request alias")
	nano := fs.String("nano-request-model", "", "Nano request alias")
	provider := fs.String("nano-provider", "", "Nano provider route")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *fixtures == "" || *manifest == "" || *out == "" || *corpus == "" {
		return errors.New("prepare requires fixtures, manifest, out, and corpus-reference-out")
	}
	inv, err := pilot.BuildInventory(*fixtures, *manifest, pilot.BuildConfig{JevRequestModel: *jev, NanoRequestModel: *nano, NanoProvider: *provider})
	if err != nil {
		return err
	}
	b, err := pilot.MarshalInventory(inv)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := writeFrozen(*out, b); err != nil {
		return err
	}
	ref, err := json.MarshalIndent(inv.Corpus, "", "  ")
	if err != nil {
		return err
	}
	return writeFrozen(*corpus, append(ref, '\n'))
}

func execute(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	path := fs.String("execution-manifest", "", "reviewed execution manifest")
	resumeDecision5 := fs.Bool("resume-superseded-halt-under-decision5", false, "auditable Decision-5 resume: reconcile the tolerated halted attempt and supersede the durable halt (binding the verbatim Decision-5 text digest and the pinned tolerance rule) before continuing the remaining slots; settled attempts are never resent")
	resumeDecision6 := fs.Bool("resume-superseded-halt-under-decision6", false, "auditable Decision-6 resume: re-classify the halted attempt's persisted response under the probability-mass admission rule, settle it as observed with the deficit recorded, and supersede the durable halt (binding the verbatim Decision-6 text digest and the admission rule) before continuing the remaining slots; settled attempts are never resent")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("run requires an explicit execution-manifest")
	}
	var m executionManifest
	manifestBytes, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return err
	}
	contractSum := sha256.Sum256(manifestBytes)
	contractSHA := hex.EncodeToString(contractSum[:])
	if !m.LiveContractsVerified {
		return errors.New("live execution denied: provider, rate, token, and pin contracts are not verified")
	}
	if err := validateArmContracts("jev", m.Jev); err != nil {
		return err
	}
	if err := validateArmContracts("nano", m.Nano); err != nil {
		return err
	}
	var inv pilot.Inventory
	if err := readJSON(m.InventoryPath, &inv); err != nil {
		return err
	}
	if inv.InventoryHash != m.InventoryHash {
		return errors.New("execution manifest inventory hash mismatch")
	}
	present := func(path string) bool { _, err := os.Stat(path); return err == nil }
	journalExists, jevExists, nanoExists := present(m.JournalPath), present(m.Jev.LedgerPath), present(m.Nano.LedgerPath)
	if (journalExists || jevExists || nanoExists) && !(journalExists && jevExists && nanoExists) {
		return errors.New("run state is partial: journal and both ledgers must all exist or all be new")
	}
	isResume := journalExists && jevExists && nanoExists
	allocation, allocationSHA, err := preflightAllocation(m, contractSHA, isResume)
	if err != nil {
		return err
	}
	credentials := make([]string, 0, 2)
	for name, a := range map[string]armManifest{"jev": m.Jev, "nano": m.Nano} {
		if value, ok := os.LookupEnv(a.APIKeyEnv); !ok || value == "" {
			return fmt.Errorf("%s credential environment variable is absent or empty", name)
		} else {
			credentials = append(credentials, value)
		}
	}
	if !isResume {
		if err := claimAllocation(m.AllocationRecordPath, allocation); err != nil {
			return err
		}
	}
	jl, jb, err := openBudget(m.RunID, "jev", m.InventoryHash, m.Jev)
	if err != nil {
		return err
	}
	defer jl.Close()
	nl, nb, err := openBudget(m.RunID, "nano", m.InventoryHash, m.Nano)
	if err != nil {
		return err
	}
	defer nl.Close()
	j, err := pilot.OpenJournal(m.JournalPath)
	if err != nil {
		return err
	}
	defer j.Close()
	httpClient := &http.Client{Timeout: 20 * time.Second}
	jc, err := adapter.NewClient(adapter.Config{Endpoint: m.Jev.Endpoint, APIKey: os.Getenv(m.Jev.APIKeyEnv), Model: m.Jev.RequestModel, ResponseModel: m.Jev.ResponseModel, ResponseProvider: m.Jev.ResponseProvider, Timeout: 20 * time.Second, HTTPClient: httpClient})
	if err != nil {
		return err
	}
	nc, err := adapter.NewChatClient(adapter.ChatConfig{Endpoint: m.Nano.Endpoint, APIKey: os.Getenv(m.Nano.APIKeyEnv), Model: m.Nano.RequestModel, ResponseModel: m.Nano.ResponseModel, ResponseProvider: m.Nano.ResponseProvider, Timeout: 20 * time.Second, HTTPClient: httpClient})
	if err != nil {
		return err
	}
	r := pilot.Runner{Inventory: &inv, Journal: j, Config: pilot.ExecutionConfig{InventoryHash: m.InventoryHash, AuthorizationRef: m.AuthorizationReference, CombinedCap: m.CombinedCap, LiveContractsVerified: m.LiveContractsVerified, RunLockPath: m.RunLockPath, EvidenceDir: m.EvidenceDir, ContractSHA: contractSHA, AllocationID: m.AllocationID, AllocationSHA: allocationSHA, ScreeningSecrets: credentials, SupersedeHaltUnderDecision5: *resumeDecision5, SupersedeHaltUnderDecision6: *resumeDecision6, Jev: jb, Nano: nb}, Attempts: map[pilot.Arm]pilot.AttemptFunc{pilot.ArmJev: jc.SubmitDecisionsOnce, pilot.ArmNano: nc.CompleteOnce}}
	_, err = r.Run(context.Background())
	return err
}

func validateArmContracts(name string, a armManifest) error {
	if a.Endpoint == "" || a.RequestModel == "" || a.ResponseModel == "" || a.ResponseProvider == "" || a.APIKeyEnv == "" || a.LedgerPath == "" || a.RatesVersion == "" || a.RateOut == nil {
		return fmt.Errorf("%s arm identity/endpoint/credential contract is incomplete", name)
	}
	if a.RateEvidence == "" || a.TokenBoundEvidence == "" || a.PinMappingEvidence == "" || a.ProviderEvidence == "" || a.OutputLimitEvidence == "" {
		return fmt.Errorf("%s arm execution evidence gates are incomplete", name)
	}
	return nil
}

func report(args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	path := fs.String("journal", "", "journal path")
	inventoryPath := fs.String("inventory", "", "frozen request inventory")
	fixturesPath := fs.String("fixtures", "", "local adjudicated fixtures for scoring")
	manifestPath := fs.String("manifest", "", "local adjudicated fixture manifest")
	evidenceDir := fs.String("evidence-dir", "", "private attempt evidence directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" || *inventoryPath == "" || *evidenceDir == "" {
		return errors.New("report requires journal, inventory, and evidence-dir")
	}
	s, err := pilot.ReplayJournal(*path)
	if err != nil {
		return err
	}
	var inv pilot.Inventory
	if err := readJSON(*inventoryPath, &inv); err != nil {
		return err
	}
	if err := pilot.ValidateInventory(&inv); err != nil {
		return err
	}
	if s.Binding == nil || s.Binding.InventoryHash != inv.InventoryHash {
		return errors.New("report journal is not bound to this inventory")
	}
	outcomes, err := pilot.ReplayOutcomes(&inv, s, *evidenceDir)
	if err != nil {
		return err
	}
	reconciled, failures, sent := 0, 0, 0
	billingCounts := map[string]int{}
	knownCost, exposure := billingTotals(outcomes, s)
	for _, out := range outcomes {
		if st := s.Slots[out.Slot.Ordinal]; st.Reconciled {
			reconciled++
		}
		if out.Error != "" {
			failures++
		}
		if out.RequestSent {
			sent++
		}
		billingCounts[out.Billing]++
	}
	result := map[string]any{"events": s.Sequence, "scheduledSlots": len(inv.Schedule), "reconciledSlots": reconciled, "requestAttempts": sent, "operationalFailures": failures, "billingCounts": billingCounts, "actualKnownSpendMicrodollars": knownCost, "conservativeLedgerExposureMicrodollars": exposure, "halted": s.Halted, "haltReason": s.HaltReason, "outcomes": outcomes}
	if supersessions := s.SupersessionList(); len(supersessions) > 0 {
		result["haltSupersessions"] = supersessions
	}
	result["probabilityMassCounters"] = probabilityMassCounters(outcomes)
	// Decision 1/2/5 policy statements, bound in every report: documented free
	// Jev output is an explicit zero rate (not omitted), MaxOutputTokens is a
	// non-cost resource bound, rate semantics are UNRECONCILED, and the
	// Decision-5 known-discrepancy tolerance rule is stated explicitly with
	// its pinned bounds.
	billingBasis, err := reportBillingBasis()
	if err != nil {
		return err
	}
	result["billingBasis"] = billingBasis
	if *fixturesPath != "" || *manifestPath != "" {
		if *fixturesPath == "" || *manifestPath == "" {
			return errors.New("report scoring requires both fixtures and manifest")
		}
		loaded, err := loader.Load(*fixturesPath, *manifestPath)
		if err != nil {
			return err
		}
		if err := pilot.ValidateScoringCorpus(&inv, loaded, *manifestPath); err != nil {
			return err
		}
		provenance, err := jevcompare.BuildProvenance(*fixturesPath, *manifestPath, inv.InventoryHash)
		if err != nil {
			return err
		}
		reports := map[pilot.Arm]jevcompare.BoundReport{}
		for _, arm := range []pilot.Arm{pilot.ArmJev, pilot.ArmNano} {
			preds, err := pilot.Project(loaded.Fixtures, outcomes, arm)
			if err != nil {
				return err
			}
			reports[arm] = jevcompare.BoundReport{Report: scoring.Score(protocol.ProtocolVersion, scoring.Source(arm), preds), CorpusProvenance: provenance, BillingBasis: jevcompare.DefaultBillingBasis()}
		}
		result["tuningOnlyScores"] = reports
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

// probabilityMassCounters is the Decision-6 contract-violation counter:
// deficit-marked responses over admitted responses per arm (the typed-
// decision contract sloppiness stays visible as a counted violation).
func probabilityMassCounters(outcomes []pilot.Outcome) map[string]map[string]int {
	counters := map[string]map[string]int{}
	for _, arm := range []string{string(pilot.ArmJev), string(pilot.ArmNano)} {
		counters[arm] = map[string]int{"deficitMarkedResponses": 0, "admittedResponses": 0}
	}
	for _, out := range outcomes {
		if out.Label == "" {
			continue
		}
		counter := counters[string(out.Slot.Arm)]
		counter["admittedResponses"]++
		if out.ProbabilityDeficitMarked {
			counter["deficitMarkedResponses"]++
		}
	}
	return counters
}

// reportBillingBasis is the report basis: the accepted accounting basis
// (decision 1/2) plus the Decision-5 known-discrepancy tolerance rule with
// its pinned bounds, stated explicitly in every report.
func reportBillingBasis() (map[string]any, error) {
	encoded, err := json.Marshal(jevcompare.DefaultBillingBasis())
	if err != nil {
		return nil, err
	}
	basis := map[string]any{}
	if err := json.Unmarshal(encoded, &basis); err != nil {
		return nil, err
	}
	rule := pilot.Decision5Tolerance()
	basis["rateToleranceRule"] = rule.Description
	basis["rateToleranceDecisionSha256"] = rule.DecisionSHA256
	basis["rateToleranceRatioBounds"] = []string{rule.RatioMin, rule.RatioMax}
	basis["probabilityMassAdmissionRule"] = pilot.Decision6RuleDescription()
	basis["probabilityMassAdmissionDecisionSha256"] = pilot.Decision6Digest()
	return basis, nil
}

func billingTotals(outcomes []pilot.Outcome, state pilot.ReplayState) (ledger.MicroUnit, ledger.MicroUnit) {
	knownCost := ledger.MicroUnit(0)
	knownBySlot := make(map[int]ledger.MicroUnit, len(outcomes))
	for _, out := range outcomes {
		if !out.KnownCost {
			continue
		}
		knownCost += out.CostMicrodollars
		knownBySlot[out.Slot.Ordinal] = out.CostMicrodollars
	}
	exposure := ledger.MicroUnit(0)
	for ordinal, st := range state.Slots {
		if !st.Reserved {
			continue
		}
		exposed := ledger.MicroUnit(st.ReservedAmount)
		if knownBySlot[ordinal] > exposed {
			exposed = knownBySlot[ordinal]
		}
		exposure += exposed
	}
	return knownCost, exposure
}

func openBudget(runID, name, inventoryHash string, a armManifest) (*ledger.Ledger, pilot.ArmBudget, error) {
	if a.LedgerPath == "" || a.RequestModel == "" || a.ResponseModel == "" || a.ResponseProvider == "" || a.RatesVersion == "" || a.Subcap <= 0 || a.Reservation <= 0 || a.InputBound <= 0 || a.OutputBound <= 0 || a.RateIn <= 0 || a.RateOut == nil || *a.RateOut < 0 {
		return nil, pilot.ArmBudget{}, fmt.Errorf("%s arm has unresolved execution contracts", name)
	}
	bounds := ledger.TokenBounds{MaxInputTokens: a.InputBound, MaxOutputTokens: a.OutputBound}
	out := *a.RateOut
	cfg := ledger.LedgerConfig{Path: a.LedgerPath, AuthorizedCap: a.Subcap, RunID: runID + "-" + name, Model: a.ResponseModel, ManifestKey: ledger.ManifestKey{FixtureFileSHA: inventoryHash, ProtocolVersion: protocol.ProtocolVersion, ModelPin: a.ResponseModel, RatesVersion: a.RatesVersion, TokenBoundsHash: ledger.ComputeTokenBoundsHash(bounds, 0, 0, 0, 0)}, TokenBounds: bounds, RetryPolicy: ledger.ReservationRetryPolicy{}, RateIn: a.RateIn, RateOut: &out}
	l, err := ledger.Open(cfg)
	if err != nil {
		return nil, pilot.ArmBudget{}, err
	}
	return l, pilot.ArmBudget{Ledger: l, Subcap: a.Subcap, Reservation: a.Reservation, InputBound: a.InputBound, OutputBound: a.OutputBound, RateIn: a.RateIn, RateOut: *a.RateOut}, nil
}

func readJSON(path string, dst any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
func writeFrozen(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0444)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
