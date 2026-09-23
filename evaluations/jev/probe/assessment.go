package probe

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/ledger"
	"github.com/Clarit-AI/Plexium/evaluations/jev/pilot"
)

const DerivedAssessmentVersion = "jev-pin-probe-derived-assessment-v1"

type AssessmentSource struct {
	Path                string           `json:"path"`
	SHA256              string           `json:"sha256"`
	Halted              bool             `json:"halted"`
	SelectedOrdinals    []int            `json:"selectedOrdinals"`
	AttemptCount        int              `json:"attemptCount"`
	LedgerBalance       ledger.MicroUnit `json:"ledgerBalanceMicrodollars"`
	ProviderReportedRaw string           `json:"providerReportedRawCostBaseUnits"`
}

type DerivedAssessment struct {
	Version                           string             `json:"version"`
	GeneratedAt                       time.Time          `json:"generatedAt"`
	InventorySHA256                   string             `json:"inventorySha256"`
	SourceReports                     []AssessmentSource `json:"sourceReports"`
	SelectedOrdinals                  []int              `json:"selectedOrdinals"`
	SelectedRequests                  []RequestBinding   `json:"selectedRequests"`
	Gates                             []Gate             `json:"gates"`
	ProviderReportedRawCostBaseUnits  string             `json:"providerReportedRawCostBaseUnits"`
	ProviderReportedRawCostMicroUnits string             `json:"providerReportedRawCostMicroUnits"`
	AdmittedCostBaseUnits             string             `json:"admittedCostBaseUnits"`
	AdmittedCostMicroUnits            string             `json:"admittedCostMicroUnits"`
	UntrustedReportedCostBaseUnits    string             `json:"untrustedReportedCostBaseUnits"`
	UntrustedReportedCostMicroUnits   string             `json:"untrustedReportedCostMicroUnits"`
	ConservativeExposureMicrodollars  ledger.MicroUnit   `json:"conservativeExposureMicrodollars"`
	SpendCharacterization             string             `json:"spendCharacterization"`
	AccountingBasis                   string             `json:"accountingBasis"`
	ExecutionReady                    bool               `json:"executionReady"`
	ExecutionReadiness                string             `json:"executionReadiness"`
}

func DeriveAssessmentToFile(inventoryPath string, sourcePaths []string, outputPath string) (*DerivedAssessment, error) {
	if len(sourcePaths) == 0 {
		return nil, errors.New("probe: at least one source report is required")
	}
	if outputPath == "" {
		return nil, errors.New("probe: derived assessment output path is required")
	}
	assessment, err := deriveAssessment(inventoryPath, sourcePaths)
	if err != nil {
		return nil, err
	}
	for _, source := range assessment.SourceReports {
		if source.Path == outputPath {
			return nil, fmt.Errorf("probe: derived assessment must not overwrite source report %s", source.Path)
		}
	}
	data, err := json.MarshalIndent(assessment, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("probe: encode derived assessment: %w", err)
	}
	data = append(data, '\n')
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("probe: create derived assessment: %w", err)
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("probe: write derived assessment: %w", err)
	}
	for _, source := range assessment.SourceReports {
		data, readErr := os.ReadFile(source.Path)
		if readErr != nil {
			return nil, fmt.Errorf("probe: re-read source report %s: %w", source.Path, readErr)
		}
		if digest(data) != source.SHA256 {
			return nil, fmt.Errorf("probe: source report changed while deriving assessment: %s", source.Path)
		}
	}
	return assessment, nil
}

func deriveAssessment(inventoryPath string, sourcePaths []string) (*DerivedAssessment, error) {
	plan, err := BuildPlan(inventoryPath, Endpoints{Jev: JevEndpoint, Nano: NanoEndpoint})
	if err != nil {
		return nil, err
	}
	planBindings := make(map[int]RequestBinding, len(plan.SelectedRequests))
	for _, binding := range plan.SelectedRequests {
		planBindings[binding.Ordinal] = binding
	}

	var sources []AssessmentSource
	selected := make(map[int]RequestBinding)
	accepted := make(map[int]Attempt)
	allAttempts := make([]Attempt, 0)
	providerReported := new(big.Rat)
	admitted := new(big.Rat)
	untrusted := new(big.Rat)
	var exposure ledger.MicroUnit
	seenSourceDigests := make(map[string]string, len(sourcePaths))
	seenStableEvidence := make(map[string][]stableAttemptIdentity)

	for _, path := range sourcePaths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("probe: read source report %s: %w", path, readErr)
		}
		sourceDigest := digest(data)
		if earlierPath, duplicate := seenSourceDigests[sourceDigest]; duplicate {
			return nil, fmt.Errorf("probe: source report %s duplicates source content from %s", path, earlierPath)
		}
		seenSourceDigests[sourceDigest] = path
		var report Report
		if err := decodeOneJSON(data, &report); err != nil {
			return nil, fmt.Errorf("probe: decode source report %s: %w", path, err)
		}
		selectionFields, err := selectionFieldPresence(data)
		if err != nil {
			return nil, fmt.Errorf("probe: inspect source report %s selection fields: %w", path, err)
		}
		if report.InventorySHA256 != plan.InventorySHA256 {
			return nil, fmt.Errorf("probe: source report %s inventory digest mismatch", path)
		}
		bindings, err := reportBindings(&report, planBindings, selectionFields)
		if err != nil {
			return nil, fmt.Errorf("probe: source report %s: %w", path, err)
		}
		selectedInSource := make(map[int]RequestBinding, len(bindings))
		for _, binding := range bindings {
			selected[binding.Ordinal] = binding
			selectedInSource[binding.Ordinal] = binding
		}

		// A probe run can attempt each selected ordinal at most once. Reject every
		// within-source ordinal duplicate before coverage or cost aggregation,
		// whether the rows are accepted, rejected, or failed. Across distinct
		// sources, a retry remains separate evidence only when its concrete
		// reservation/response identity is distinct.
		seenOrdinals := make(map[int]bool, len(report.Attempts))
		for _, attempt := range report.Attempts {
			if seenOrdinals[attempt.Ordinal] {
				return nil, fmt.Errorf("probe: source report %s duplicates attempt ordinal %d", path, attempt.Ordinal)
			}
			seenOrdinals[attempt.Ordinal] = true
			binding, selectedBySource := selectedInSource[attempt.Ordinal]
			if !selectedBySource {
				return nil, fmt.Errorf("probe: source report %s attempt ordinal %d is outside its selected bindings", path, attempt.Ordinal)
			}
			if !attemptIdentityMatches(attempt, binding) {
				return nil, fmt.Errorf("probe: source report %s attempt identity mismatch for ordinal %d", path, attempt.Ordinal)
			}
			stable := stableIdentity(attempt, path)
			if err := validateStableIdentity(stable); err != nil {
				return nil, fmt.Errorf("probe: source report %s attempt ordinal %d lacks mandatory stable evidence: %w", path, attempt.Ordinal, err)
			}
			namespace := attempt.RequestID + "\x00" + attempt.RequestSHA256
			for _, earlier := range seenStableEvidence[namespace] {
				if stableEvidenceConflicts(earlier, stable) {
					return nil, fmt.Errorf("probe: source report %s repeats or conflicts with concrete attempt evidence from %s for ordinal %d", path, earlier.SourcePath, attempt.Ordinal)
				}
			}
			seenStableEvidence[namespace] = append(seenStableEvidence[namespace], stable)
		}
		for _, attempt := range report.Attempts {
			binding := selectedInSource[attempt.Ordinal]
			allAttempts = append(allAttempts, attempt)
			if attemptAccepted(attempt, binding) {
				if _, exists := accepted[attempt.Ordinal]; !exists {
					accepted[attempt.Ordinal] = attempt
				}
			}
			if raw, ok := numericEvidenceRat(attempt.Billing.Cost); ok {
				providerReported.Add(providerReported, raw)
				if attempt.Billing.Cost.Valid && attempt.State == "settled" {
					admitted.Add(admitted, raw)
				} else {
					untrusted.Add(untrusted, raw)
				}
			}
		}
		if report.LedgerBalance > ledger.MaxMicroUnits-exposure {
			return nil, errors.New("probe: conservative exposure overflow")
		}
		exposure += report.LedgerBalance
		sources = append(sources, AssessmentSource{
			Path: path, SHA256: sourceDigest, Halted: report.Halted,
			SelectedOrdinals: bindingOrdinals(bindings), AttemptCount: len(report.Attempts),
			LedgerBalance: report.LedgerBalance, ProviderReportedRaw: formatBaseUnits(sumAttemptRawCosts(report.Attempts)),
		})
	}

	ordinals := make([]int, 0, len(selected))
	for ordinal := range selected {
		ordinals = append(ordinals, ordinal)
	}
	sort.Ints(ordinals)
	bindings := make([]RequestBinding, 0, len(ordinals))
	effectiveAttempts := make([]Attempt, 0, len(ordinals))
	for _, ordinal := range ordinals {
		bindings = append(bindings, selected[ordinal])
		if attempt, ok := accepted[ordinal]; ok {
			effectiveAttempts = append(effectiveAttempts, attempt)
			continue
		}
		for _, attempt := range allAttempts {
			if attempt.Ordinal == ordinal {
				effectiveAttempts = append(effectiveAttempts, attempt)
				break
			}
		}
	}
	aggregate := &Report{SelectedRequests: bindings, SelectedOrdinals: ordinals, Attempts: effectiveAttempts}

	return &DerivedAssessment{
		Version: DerivedAssessmentVersion, GeneratedAt: time.Now().UTC(), InventorySHA256: plan.InventorySHA256,
		SourceReports: sources, SelectedOrdinals: ordinals, SelectedRequests: bindings, Gates: assessGates(aggregate),
		ProviderReportedRawCostBaseUnits:  formatBaseUnits(providerReported),
		ProviderReportedRawCostMicroUnits: formatMicroUnits(providerReported),
		AdmittedCostBaseUnits:             formatBaseUnits(admitted), AdmittedCostMicroUnits: formatMicroUnits(admitted),
		UntrustedReportedCostBaseUnits: formatBaseUnits(untrusted), UntrustedReportedCostMicroUnits: formatMicroUnits(untrusted),
		ConservativeExposureMicrodollars: exposure,
		SpendCharacterization:            "provider-reported raw cost is not verified spend; invalid or rejected billing remains untrusted and conservative exposure remains separate",
		AccountingBasis:                  AccountingBasis, ExecutionReady: false, ExecutionReadiness: ReadinessReason,
	}, nil
}

type selectionPresence struct {
	Ordinals bool
	Requests bool
}

func selectionFieldPresence(data []byte) (selectionPresence, error) {
	var properties map[string]json.RawMessage
	if err := decodeOneJSON(data, &properties); err != nil {
		return selectionPresence{}, err
	}
	_, ordinals := properties["selectedOrdinals"]
	_, requests := properties["selectedRequests"]
	return selectionPresence{Ordinals: ordinals, Requests: requests}, nil
}

func reportBindings(report *Report, plan map[int]RequestBinding, presence selectionPresence) ([]RequestBinding, error) {
	if presence.Requests {
		if len(report.SelectedRequests) == 0 {
			return nil, errors.New("selectedRequests is present but empty or null")
		}
		bindings := make([]RequestBinding, len(report.SelectedRequests))
		seen := make(map[int]bool, len(report.SelectedRequests))
		for i, binding := range report.SelectedRequests {
			expected, ok := plan[binding.Ordinal]
			if !ok || binding != expected || seen[binding.Ordinal] {
				return nil, fmt.Errorf("selected request identity mismatch for ordinal %d", binding.Ordinal)
			}
			seen[binding.Ordinal] = true
			bindings[i] = binding
		}
		if presence.Ordinals {
			if len(report.SelectedOrdinals) == 0 {
				return nil, errors.New("selectedOrdinals is present but empty or null")
			}
			if len(report.SelectedOrdinals) != len(bindings) {
				return nil, errors.New("selected ordinals and request bindings differ in length")
			}
			seenOrdinals := make(map[int]bool, len(report.SelectedOrdinals))
			for i, ordinal := range report.SelectedOrdinals {
				if seenOrdinals[ordinal] || ordinal != bindings[i].Ordinal {
					return nil, fmt.Errorf("selected ordinal %d is inconsistent with request bindings", ordinal)
				}
				seenOrdinals[ordinal] = true
			}
		}
		return bindings, nil
	}
	if presence.Ordinals {
		if len(report.SelectedOrdinals) == 0 {
			return nil, errors.New("selectedOrdinals is present but empty or null")
		}
		bindings := make([]RequestBinding, 0, len(report.SelectedOrdinals))
		seen := make(map[int]bool)
		for _, ordinal := range report.SelectedOrdinals {
			binding, ok := plan[ordinal]
			if !ok || seen[ordinal] {
				return nil, fmt.Errorf("invalid selected ordinal %d", ordinal)
			}
			seen[ordinal] = true
			bindings = append(bindings, binding)
		}
		return bindings, nil
	}
	// Reports written before subset selection used nil to mean the approved full plan.
	ordinals := make([]int, 0, len(plan))
	for ordinal := range plan {
		ordinals = append(ordinals, ordinal)
	}
	sort.Ints(ordinals)
	bindings := make([]RequestBinding, 0, len(ordinals))
	for _, ordinal := range ordinals {
		bindings = append(bindings, plan[ordinal])
	}
	return bindings, nil
}

type stableAttemptIdentity struct {
	ReservationRef  string
	RawResponseHash string
	EvidenceHash    string
	SourcePath      string
}

func stableIdentity(attempt Attempt, sourcePath string) stableAttemptIdentity {
	return stableAttemptIdentity{
		ReservationRef: attempt.ReservationRef, RawResponseHash: attempt.RawResponseSHA256,
		EvidenceHash: attempt.EvidenceSHA256, SourcePath: sourcePath,
	}
}

func validateStableIdentity(identity stableAttemptIdentity) error {
	// Every row must independently bind the attempted reservation to response
	// evidence before it can contribute coverage or accounting. Pairwise reuse
	// checks alone cannot validate a first or sole row.
	if identity.ReservationRef == "" {
		return errors.New("reservation reference is empty")
	}
	if identity.RawResponseHash == "" && identity.EvidenceHash == "" {
		return errors.New("raw-response and persisted-evidence hashes are both empty")
	}
	return nil
}

func stableEvidenceConflicts(left, right stableAttemptIdentity) bool {
	// A retry is distinct only when it has both a new reservation and
	// comparable, different response evidence. Mutable provenance such as the
	// start time or provider-request header cannot establish a new attempt.
	if left.ReservationRef == "" || right.ReservationRef == "" || left.ReservationRef == right.ReservationRef {
		return true
	}
	comparableResponseEvidence := false
	if left.RawResponseHash != "" && right.RawResponseHash != "" {
		if left.RawResponseHash == right.RawResponseHash {
			return true
		}
		comparableResponseEvidence = true
	}
	if left.EvidenceHash != "" && right.EvidenceHash != "" {
		if left.EvidenceHash == right.EvidenceHash {
			return true
		}
		comparableResponseEvidence = true
	}
	return !comparableResponseEvidence
}

func decodeOneJSON(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return errors.New("trailing JSON value")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func numericEvidenceRat(field DecimalEvidence) (*big.Rat, bool) {
	if !field.Present || field.Null || field.Raw == "" {
		return nil, false
	}
	var number json.Number
	decoder := json.NewDecoder(strings.NewReader(field.Raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return nil, false
	}
	value, ok := new(big.Rat).SetString(number.String())
	return value, ok && value.Sign() >= 0
}

func sumAttemptRawCosts(attempts []Attempt) *big.Rat {
	total := new(big.Rat)
	for _, attempt := range attempts {
		if value, ok := numericEvidenceRat(attempt.Billing.Cost); ok {
			total.Add(total, value)
		}
	}
	return total
}

func formatBaseUnits(value *big.Rat) string { return formatDecimal(value, 18) }

func formatMicroUnits(value *big.Rat) string {
	return formatDecimal(new(big.Rat).Mul(new(big.Rat).Set(value), big.NewRat(1_000_000, 1)), 12)
}

func formatDecimal(value *big.Rat, places int) string {
	formatted := value.FloatString(places)
	formatted = strings.TrimRight(formatted, "0")
	formatted = strings.TrimRight(formatted, ".")
	if formatted == "" || formatted == "-0" {
		return "0"
	}
	return formatted
}

func bindingOrdinals(bindings []RequestBinding) []int {
	ordinals := make([]int, len(bindings))
	for i, binding := range bindings {
		ordinals[i] = binding.Ordinal
	}
	return ordinals
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func scopeCovered(bindings []RequestBinding, attempts []Attempt, arms []pilot.Arm, kind string) bool {
	requiredArms := make(map[pilot.Arm]bool, len(arms))
	for _, arm := range arms {
		requiredArms[arm] = true
	}
	seenArms := make(map[pilot.Arm]bool, len(arms))
	scoped := make(map[int]RequestBinding)
	for _, binding := range bindings {
		if !requiredArms[binding.Arm] || (kind != "" && binding.Kind != kind) {
			continue
		}
		if _, duplicate := scoped[binding.Ordinal]; duplicate {
			return false
		}
		scoped[binding.Ordinal] = binding
		seenArms[binding.Arm] = true
	}
	for _, arm := range arms {
		if !seenArms[arm] {
			return false
		}
	}
	for ordinal, binding := range scoped {
		matches := 0
		for _, attempt := range attempts {
			if attempt.Ordinal != ordinal {
				continue
			}
			matches++
			if !attemptAccepted(attempt, binding) {
				return false
			}
		}
		if matches != 1 {
			return false
		}
	}
	return len(scoped) > 0
}

func attemptAccepted(attempt Attempt, binding RequestBinding) bool {
	return attemptIdentityMatches(attempt, binding) && attempt.State == "settled" &&
		attempt.RequestSent && attempt.ResponseReceived && attempt.Status == 200 &&
		attempt.ProviderRequestID != "" && attempt.ResolvedModel == binding.CandidatePin &&
		attempt.ResponseProvider == binding.CandidateProvider && attempt.AdapterError == "" &&
		attempt.ReadError == "" && !attempt.RawTruncated && attempt.Billing.Cost.Present &&
		!attempt.Billing.Cost.Null && attempt.Billing.Cost.Valid
}

func attemptIdentityMatches(attempt Attempt, binding RequestBinding) bool {
	return attempt.Ordinal == binding.Ordinal && attempt.RequestID == binding.ID &&
		attempt.Arm == binding.Arm && attempt.RequestSHA256 == binding.RequestSHA256 &&
		attempt.RequestedAlias == binding.RequestedAlias && attempt.ExpectedPin == binding.CandidatePin &&
		attempt.ExpectedProvider == binding.CandidateProvider
}
