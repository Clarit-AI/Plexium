package probe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	dir := t.TempDir()
	run1Path := filepath.Join(dir, "run-1.json")
	run2Path := filepath.Join(dir, "run-2.json")
	writeAssessmentTestReport(t, run1Path, &run1)
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

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
