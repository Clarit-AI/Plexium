package probe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAssessGatesUsesSelectedIdentityCoverage(t *testing.T) {
	plan := assessmentTestPlan(t)
	bindings := make(map[int]RequestBinding)
	for _, binding := range plan.SelectedRequests {
		bindings[binding.Ordinal] = binding
	}

	tests := []struct {
		name          string
		ordinals      []int
		mutate        func([]Attempt)
		wantEvidence  bool
		wantNanoScope bool
		wantJevScope  bool
	}{
		{name: "three-attempt-subset", ordinals: []int{2, 3, 4}, wantEvidence: true, wantNanoScope: true, wantJevScope: true},
		{name: "full-plan", ordinals: []int{1, 2, 3, 4}, wantEvidence: true, wantNanoScope: true, wantJevScope: true},
		{name: "jev-only", ordinals: []int{1, 3}, wantEvidence: false, wantJevScope: true},
		{name: "failed-required-attempt", ordinals: []int{2, 3, 4}, mutate: func(attempts []Attempt) { attempts[0].State = "observed" }, wantJevScope: true},
		{name: "identity-mismatch", ordinals: []int{2, 3, 4}, mutate: func(attempts []Attempt) { attempts[1].RequestSHA256 = strings.Repeat("0", 64) }, wantNanoScope: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := Report{}
			for _, ordinal := range test.ordinals {
				report.SelectedOrdinals = append(report.SelectedOrdinals, ordinal)
				report.SelectedRequests = append(report.SelectedRequests, bindings[ordinal])
				report.Attempts = append(report.Attempts, acceptedAssessmentAttempt(bindings[ordinal]))
			}
			if test.mutate != nil {
				test.mutate(report.Attempts)
			}
			gates := assessGates(&report)
			if got := gates[0].Status == "EVIDENCE_COLLECTED" && gates[1].Status == "EVIDENCE_COLLECTED"; got != test.wantEvidence {
				t.Fatalf("gate 1/2 evidence=%v want=%v gates=%+v", got, test.wantEvidence, gates)
			}
			if got := strings.Contains(gates[2].Reason, "selected Nano requests were accepted"); got != test.wantNanoScope {
				t.Fatalf("Nano scope assessed=%v want=%v gate=%+v", got, test.wantNanoScope, gates[2])
			}
			if got := strings.Contains(gates[3].Reason, "observed Jev output usage"); got != test.wantJevScope {
				t.Fatalf("Jev scope assessed=%v want=%v gate=%+v", got, test.wantJevScope, gates[3])
			}
			for _, gate := range gates {
				if gate.Status == "CLOSED" {
					t.Fatalf("probe evidence closed gate: %+v", gate)
				}
			}
		})
	}
}

func TestDerivedAssessmentCombinesImmutableRunsAndSeparatesAccounting(t *testing.T) {
	plan := assessmentTestPlan(t)
	bindings := make(map[int]RequestBinding)
	for _, binding := range plan.SelectedRequests {
		bindings[binding.Ordinal] = binding
	}

	run1 := Report{
		Version: PlanVersion, InventorySHA256: plan.InventorySHA256, Halted: true,
		LedgerBalance: 50_016,
		Attempts: []Attempt{
			acceptedAssessmentAttemptWithCost(bindings[1], "0.000015288"),
			failedAssessmentAttemptWithCost(bindings[2], "0.000008613"),
		},
	}
	setAssessmentAttemptIdentity(&run1.Attempts[0], "run1-ordinal1")
	setAssessmentAttemptIdentity(&run1.Attempts[1], "run1-ordinal2")
	run2 := Report{
		Version: PlanVersion, InventorySHA256: plan.InventorySHA256,
		SelectedOrdinals: []int{2, 3, 4}, SelectedRequests: []RequestBinding{bindings[2], bindings[3], bindings[4]},
		LedgerBalance: 128,
		Attempts: []Attempt{
			acceptedAssessmentAttemptWithCost(bindings[2], "0.000008613"),
			acceptedAssessmentAttemptWithCost(bindings[3], "0.000061908"),
			acceptedAssessmentAttemptWithCost(bindings[4], "0.000056529"),
		},
	}
	setAssessmentAttemptIdentity(&run2.Attempts[0], "run2-ordinal2")
	setAssessmentAttemptIdentity(&run2.Attempts[1], "run2-ordinal3")
	setAssessmentAttemptIdentity(&run2.Attempts[2], "run2-ordinal4")

	dir := t.TempDir()
	run1Path := filepath.Join(dir, "run-1.json")
	run2Path := filepath.Join(dir, "run-2.json")
	writeAssessmentLegacyTestReport(t, run1Path, &run1)
	writeAssessmentTestReport(t, run2Path, &run2)
	before1 := fileDigest(t, run1Path)
	before2 := fileDigest(t, run2Path)
	output := filepath.Join(dir, "derived.json")

	assessment, err := DeriveAssessmentToFile(assessmentInventoryPath(), []string{run1Path, run2Path}, output)
	if err != nil {
		t.Fatal(err)
	}
	if fileDigest(t, run1Path) != before1 || fileDigest(t, run2Path) != before2 {
		t.Fatal("source report changed while deriving assessment")
	}
	if assessment.Gates[0].Status != "EVIDENCE_COLLECTED" || assessment.Gates[1].Status != "EVIDENCE_COLLECTED" {
		t.Fatalf("combined accepted coverage did not promote gates 1/2: %+v", assessment.Gates)
	}
	for _, gate := range assessment.Gates {
		if gate.Status == "CLOSED" {
			t.Fatalf("derived probe evidence closed gate: %+v", gate)
		}
	}
	if assessment.ProviderReportedRawCostBaseUnits != "0.000150951" || assessment.ProviderReportedRawCostMicroUnits != "150.951" ||
		assessment.AdmittedCostBaseUnits != "0.000142338" || assessment.AdmittedCostMicroUnits != "142.338" ||
		assessment.UntrustedReportedCostBaseUnits != "0.000008613" || assessment.UntrustedReportedCostMicroUnits != "8.613" ||
		assessment.ConservativeExposureMicrodollars != 50_144 {
		t.Fatalf("derived accounting mismatch: %+v", assessment)
	}
	if !strings.Contains(assessment.SpendCharacterization, "not verified spend") || !strings.Contains(assessment.SpendCharacterization, "untrusted") {
		t.Fatalf("provider-reported limitation missing: %q", assessment.SpendCharacterization)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("derived output missing: %v", err)
	}
	if _, err := DeriveAssessmentToFile(assessmentInventoryPath(), []string{run1Path, run2Path}, output); err == nil {
		t.Fatal("derived assessment unexpectedly overwrote existing output")
	}
}

func TestDerivedAssessmentRejectsWithinSourceDuplicatesBeforeAccounting(t *testing.T) {
	plan := assessmentTestPlan(t)
	binding := plan.SelectedRequests[1]

	tests := []struct {
		name     string
		attempts []Attempt
	}{
		{
			name: "duplicated-rejected-row",
			attempts: []Attempt{
				failedAssessmentAttemptWithCost(binding, "0.000008613"),
				failedAssessmentAttemptWithCost(binding, "0.000008613"),
			},
		},
		{
			name: "accepted-plus-failed-duplicate",
			attempts: []Attempt{
				acceptedAssessmentAttemptWithCost(binding, "0.000008613"),
				failedAssessmentAttemptWithCost(binding, "0.000008613"),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for i := range test.attempts {
				setAssessmentAttemptIdentity(&test.attempts[i], "same-run-ordinal2")
			}
			report := Report{
				Version: PlanVersion, InventorySHA256: plan.InventorySHA256,
				SelectedOrdinals: []int{2}, SelectedRequests: []RequestBinding{binding}, Attempts: test.attempts,
			}
			dir := t.TempDir()
			source := filepath.Join(dir, "source.json")
			writeAssessmentTestReport(t, source, &report)
			output := filepath.Join(dir, "derived.json")
			_, err := DeriveAssessmentToFile(assessmentInventoryPath(), []string{source}, output)
			if err == nil || !strings.Contains(err.Error(), "duplicates attempt ordinal 2") {
				t.Fatalf("duplicate evidence was not rejected before aggregation: %v", err)
			}
			if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
				t.Fatalf("duplicate evidence emitted derived totals: %v", statErr)
			}
		})
	}
}

func TestDerivedAssessmentPreservesDistinctCrossRunRetryEvidence(t *testing.T) {
	plan := assessmentTestPlan(t)
	binding := plan.SelectedRequests[1]
	first := acceptedAssessmentAttemptWithCost(binding, "0.000001")
	second := acceptedAssessmentAttemptWithCost(binding, "0.000002")
	setAssessmentAttemptIdentity(&first, "run-a-ordinal2")
	setAssessmentAttemptIdentity(&second, "run-b-ordinal2")

	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "run-a.json"), filepath.Join(dir, "run-b.json")}
	for i, attempt := range []Attempt{first, second} {
		report := Report{
			Version: PlanVersion, InventorySHA256: plan.InventorySHA256,
			SelectedOrdinals: []int{2}, SelectedRequests: []RequestBinding{binding}, Attempts: []Attempt{attempt},
		}
		writeAssessmentTestReport(t, paths[i], &report)
	}
	assessment, err := DeriveAssessmentToFile(assessmentInventoryPath(), paths, filepath.Join(dir, "derived.json"))
	if err != nil {
		t.Fatal(err)
	}
	if assessment.ProviderReportedRawCostBaseUnits != "0.000003" || assessment.AdmittedCostMicroUnits != "3" || len(assessment.SourceReports) != 2 {
		t.Fatalf("distinct cross-run retry evidence was not preserved: %+v", assessment)
	}
	if assessment.Gates[0].Status == "EVIDENCE_COLLECTED" || assessment.Gates[1].Status == "EVIDENCE_COLLECTED" {
		t.Fatalf("Nano-only retries promoted both-arm gates: %+v", assessment.Gates)
	}
}

func TestDerivedAssessmentEnforcesSourceSelectionMembershipAndLegacyFallback(t *testing.T) {
	plan := assessmentTestPlan(t)
	binding1, binding2 := plan.SelectedRequests[0], plan.SelectedRequests[1]

	t.Run("attempt-outside-selection", func(t *testing.T) {
		attempt := acceptedAssessmentAttempt(binding1)
		setAssessmentAttemptIdentity(&attempt, "outside-selection")
		report := Report{
			Version: PlanVersion, InventorySHA256: plan.InventorySHA256,
			SelectedOrdinals: []int{2}, SelectedRequests: []RequestBinding{binding2}, Attempts: []Attempt{attempt},
		}
		dir := t.TempDir()
		source := filepath.Join(dir, "source.json")
		writeAssessmentTestReport(t, source, &report)
		_, err := DeriveAssessmentToFile(assessmentInventoryPath(), []string{source}, filepath.Join(dir, "derived.json"))
		if err == nil || !strings.Contains(err.Error(), "outside its selected bindings") {
			t.Fatalf("out-of-selection attempt was not rejected: %v", err)
		}
	})

	t.Run("duplicate-selection", func(t *testing.T) {
		attempt := acceptedAssessmentAttempt(binding2)
		setAssessmentAttemptIdentity(&attempt, "duplicate-selection")
		report := Report{
			Version: PlanVersion, InventorySHA256: plan.InventorySHA256,
			SelectedOrdinals: []int{2, 2}, SelectedRequests: []RequestBinding{binding2, binding2}, Attempts: []Attempt{attempt},
		}
		dir := t.TempDir()
		source := filepath.Join(dir, "source.json")
		writeAssessmentTestReport(t, source, &report)
		_, err := DeriveAssessmentToFile(assessmentInventoryPath(), []string{source}, filepath.Join(dir, "derived.json"))
		if err == nil || !strings.Contains(err.Error(), "selected request identity mismatch") {
			t.Fatalf("duplicate source selection was not rejected: %v", err)
		}
	})

	t.Run("legacy-no-selection-fields", func(t *testing.T) {
		attempt := acceptedAssessmentAttempt(binding1)
		setAssessmentAttemptIdentity(&attempt, "legacy-full-plan")
		report := Report{Version: PlanVersion, InventorySHA256: plan.InventorySHA256, Attempts: []Attempt{attempt}}
		dir := t.TempDir()
		source := filepath.Join(dir, "source.json")
		writeAssessmentLegacyTestReport(t, source, &report)
		assessment, err := DeriveAssessmentToFile(assessmentInventoryPath(), []string{source}, filepath.Join(dir, "derived.json"))
		if err != nil {
			t.Fatal(err)
		}
		if got := assessment.SourceReports[0].SelectedOrdinals; len(got) != 4 || got[0] != 1 || got[3] != 4 {
			t.Fatalf("legacy full-plan fallback changed: %v", got)
		}
	})
}

func TestDerivedAssessmentRejectsRepeatedSourceOrConcreteEvidence(t *testing.T) {
	plan := assessmentTestPlan(t)
	attempt := acceptedAssessmentAttempt(plan.SelectedRequests[0])
	setAssessmentAttemptIdentity(&attempt, "same-source")
	report := Report{Version: PlanVersion, InventorySHA256: plan.InventorySHA256, Attempts: []Attempt{attempt}}
	t.Run("identical-source-content", func(t *testing.T) {
		dir := t.TempDir()
		first := filepath.Join(dir, "first.json")
		second := filepath.Join(dir, "second.json")
		writeAssessmentLegacyTestReport(t, first, &report)
		writeAssessmentLegacyTestReport(t, second, &report)
		_, err := DeriveAssessmentToFile(assessmentInventoryPath(), []string{first, second}, filepath.Join(dir, "derived.json"))
		if err == nil || !strings.Contains(err.Error(), "duplicates source content") {
			t.Fatalf("repeated source content was not rejected: %v", err)
		}
	})
	t.Run("same-concrete-attempt-in-distinct-source-content", func(t *testing.T) {
		dir := t.TempDir()
		first := filepath.Join(dir, "first.json")
		second := filepath.Join(dir, "second.json")
		writeAssessmentLegacyTestReport(t, first, &report)
		changed := report
		changed.LedgerBalance = 1
		writeAssessmentLegacyTestReport(t, second, &changed)
		_, err := DeriveAssessmentToFile(assessmentInventoryPath(), []string{first, second}, filepath.Join(dir, "derived.json"))
		if err == nil || !strings.Contains(err.Error(), "repeats or conflicts with concrete attempt evidence") {
			t.Fatalf("repeated concrete evidence was not rejected: %v", err)
		}
	})
}

func TestDerivedAssessmentRejectsStableEvidenceReuseDespiteMutableMetadata(t *testing.T) {
	plan := assessmentTestPlan(t)
	binding := plan.SelectedRequests[1]
	base := acceptedAssessmentAttemptWithCost(binding, "0.000008613")
	setAssessmentAttemptIdentity(&base, "stable-ordinal2")
	base.StartedAt = time.Unix(1_700_000_000, 0).UTC()

	tests := []struct {
		name   string
		mutate func(*Attempt)
	}{
		{
			name: "start-time-is-not-identity",
			mutate: func(attempt *Attempt) {
				attempt.StartedAt = attempt.StartedAt.Add(time.Nanosecond)
			},
		},
		{
			name: "provider-request-id-is-not-identity",
			mutate: func(attempt *Attempt) {
				attempt.ProviderRequestID = "different-provider-request-id"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := base
			test.mutate(&changed)
			dir := t.TempDir()
			paths := []string{filepath.Join(dir, "run-a.json"), filepath.Join(dir, "run-b.json")}
			for i, attempt := range []Attempt{base, changed} {
				report := Report{
					Version: PlanVersion, InventorySHA256: plan.InventorySHA256,
					SelectedOrdinals: []int{2}, SelectedRequests: []RequestBinding{binding}, Attempts: []Attempt{attempt},
				}
				writeAssessmentTestReport(t, paths[i], &report)
			}
			output := filepath.Join(dir, "derived.json")
			_, err := DeriveAssessmentToFile(assessmentInventoryPath(), paths, output)
			if err == nil || !strings.Contains(err.Error(), "repeats or conflicts with concrete attempt evidence") {
				t.Fatalf("stable evidence reuse was not rejected: %v", err)
			}
			if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
				t.Fatalf("reused evidence emitted derived totals: %v", statErr)
			}
		})
	}
}

func TestDerivedAssessmentDistinguishesAbsentFromEmptySelectionFields(t *testing.T) {
	plan := assessmentTestPlan(t)
	binding := plan.SelectedRequests[0]
	attempt := acceptedAssessmentAttempt(binding)
	setAssessmentAttemptIdentity(&attempt, "selection-presence")
	report := Report{Version: PlanVersion, InventorySHA256: plan.InventorySHA256, Attempts: []Attempt{attempt}}

	tests := []struct {
		name       string
		ordinals   *json.RawMessage
		requests   *json.RawMessage
		wantDetail string
	}{
		{name: "both-empty-arrays", ordinals: rawJSON(`[]`), requests: rawJSON(`[]`), wantDetail: "selectedRequests is present but empty or null"},
		{name: "requests-empty-ordinals-absent", requests: rawJSON(`[]`), wantDetail: "selectedRequests is present but empty or null"},
		{name: "ordinals-empty-requests-absent", ordinals: rawJSON(`[]`), wantDetail: "selectedOrdinals is present but empty or null"},
		{name: "both-null", ordinals: rawJSON(`null`), requests: rawJSON(`null`), wantDetail: "selectedRequests is present but empty or null"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "source.json")
			writeAssessmentReportWithSelectionFields(t, source, &report, test.ordinals, test.requests)
			output := filepath.Join(dir, "derived.json")
			_, err := DeriveAssessmentToFile(assessmentInventoryPath(), []string{source}, output)
			if err == nil || !strings.Contains(err.Error(), test.wantDetail) {
				t.Fatalf("explicit empty selection was not rejected: %v", err)
			}
			if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
				t.Fatalf("empty selection emitted derived output: %v", statErr)
			}
		})
	}
}

func assessmentTestPlan(t *testing.T) *Plan {
	t.Helper()
	plan, err := BuildPlan(assessmentInventoryPath(), Endpoints{Jev: JevEndpoint, Nano: NanoEndpoint})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func assessmentInventoryPath() string { return filepath.Join("..", "pilot", "request-inventory.json") }

func acceptedAssessmentAttempt(binding RequestBinding) Attempt {
	return acceptedAssessmentAttemptWithCost(binding, "0.000001")
}

func acceptedAssessmentAttemptWithCost(binding RequestBinding, cost string) Attempt {
	return Attempt{
		Ordinal: binding.Ordinal, RequestID: binding.ID, Arm: binding.Arm, Kind: binding.Kind, FixtureID: binding.FixtureID,
		RequestedAlias: binding.RequestedAlias, ExpectedPin: binding.CandidatePin, ExpectedProvider: binding.CandidateProvider,
		RequestSHA256: binding.RequestSHA256, State: "settled", RequestSent: true, ResponseReceived: true, Status: 200,
		ProviderRequestID: "provider-request", ResolvedModel: binding.CandidatePin, ResponseProvider: binding.CandidateProvider,
		Billing: BillingEvidence{Cost: DecimalEvidence{Present: true, Valid: true, Raw: cost}},
	}
}

func failedAssessmentAttemptWithCost(binding RequestBinding, cost string) Attempt {
	attempt := acceptedAssessmentAttemptWithCost(binding, cost)
	attempt.State = "observed"
	attempt.Billing.Cost.Valid = false
	attempt.Billing.Cost.Error = "response schema not admitted"
	return attempt
}

func setAssessmentAttemptIdentity(attempt *Attempt, identity string) {
	attempt.ReservationRef = "reservation-" + identity
	attempt.ProviderRequestID = "provider-" + identity
	sum := sha256.Sum256([]byte(identity))
	attempt.RawResponseSHA256 = hex.EncodeToString(sum[:])
	attempt.EvidenceSHA256 = hex.EncodeToString(sum[:])
}

func writeAssessmentTestReport(t *testing.T, path string, report *Report) {
	t.Helper()
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeAssessmentLegacyTestReport(t *testing.T, path string, report *Report) {
	t.Helper()
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var properties map[string]json.RawMessage
	if err := json.Unmarshal(data, &properties); err != nil {
		t.Fatal(err)
	}
	delete(properties, "selectedOrdinals")
	delete(properties, "selectedRequests")
	data, err = json.MarshalIndent(properties, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func rawJSON(value string) *json.RawMessage {
	raw := json.RawMessage(value)
	return &raw
}

func writeAssessmentReportWithSelectionFields(t *testing.T, path string, report *Report, ordinals, requests *json.RawMessage) {
	t.Helper()
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var properties map[string]json.RawMessage
	if err := json.Unmarshal(data, &properties); err != nil {
		t.Fatal(err)
	}
	delete(properties, "selectedOrdinals")
	delete(properties, "selectedRequests")
	if ordinals != nil {
		properties["selectedOrdinals"] = *ordinals
	}
	if requests != nil {
		properties["selectedRequests"] = *requests
	}
	data, err = json.MarshalIndent(properties, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
