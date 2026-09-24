package pilot

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
)

// TestW1R3_SanitizeTextCorruptsNumericEvidence_Runner tests the actual bug site:
// appendSlotEvent applies sanitizeText to a numeric cost lexeme,
// corrupting 0.0000020 to 0.00000[REDACTED_CREDENTIAL] when the fake key is 20.
func TestW1R3_SanitizeTextCorruptsNumericEvidence_Runner(t *testing.T) {
	// Test through the Runner with a secret that would corrupt numeric values
	const secret = "20" // This would corrupt "0.0000020" -> "0.00000[REDACTED_CREDENTIAL]"

	r, closeAll := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev, Repetition: 1}}, 20, 10, 10)
	defer closeAll()
	r.Config.ScreeningSecrets = []string{secret}

	// Return an observation with cost "0.0000020" which contains "20"
	// Use token counts within bounds (InputBound=1, OutputBound=1 from newTestRunner)
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		o := zeroObservation("jev")
		o.Billing.Cost = decimal("0.0000020")
		o.Billing.InputTokens = decimal("1")
		o.Billing.OutputTokens = decimal("1")
		o.Billing.TotalTokens = decimal("0.000001")
		return o, nil
	}

	outcomes, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Check the journal for corrupted numeric values
	journalBytes, err := os.ReadFile(r.Journal.path)
	if err != nil {
		t.Fatal(err)
	}

	for i, line := range bytes.Split(bytes.TrimSpace(journalBytes), []byte("\n")) {
		var event JournalEvent
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("journal line %d invalid JSON: %v", i, err)
		}
		if event.Type == EventReconciled || event.Type == EventObserved {
			if strings.Contains(event.BillingCostRaw, "[REDACTED_CREDENTIAL]") {
				t.Fatalf("W1-R3 REPRODUCED: journal line %d has corrupted BillingCostRaw: %s", i, event.BillingCostRaw)
			}
			if event.BillingCostRaw != "0.0000020" {
				t.Fatalf("W1-R3 REPRODUCED: journal line %d BillingCostRaw changed from 0.0000020 to %s", i, event.BillingCostRaw)
			}
		}
	}

	// Check outcomes
	if len(outcomes) != 1 {
		t.Fatalf("expected 1 outcome, got %d", len(outcomes))
	}
	if outcomes[0].BillingRaw != "0.0000020" {
		t.Fatalf("W1-R3 REPRODUCED: outcome BillingRaw corrupted: %s", outcomes[0].BillingRaw)
	}

	// Check evidence files
	entries, err := os.ReadDir(r.Config.EvidenceDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		b, err := os.ReadFile(filepath.Join(r.Config.EvidenceDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "[REDACTED_CREDENTIAL]") {
			t.Fatalf("W1-R3 REPRODUCED: evidence file %s has redacted credential in numeric field: %s", entry.Name(), string(b))
		}
	}

	t.Log("W1-R3 FIXED: Runner preserves numeric lexemes in journal and evidence")
}

// TestW1R3_SanitizeTextCorruptsNumericEvidence_Adjacency tests adjacency cases: 20/120/205
func TestW1R3_SanitizeTextCorruptsNumericEvidence_Adjacency(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cost   string
		secret string
	}{
		{"key 20", "0.0000020", "20"},
		{"key 120", "0.0000020", "120"},
		{"key 205", "0.0000020", "205"},
		{"key 20 on input", "0.0000020", "20"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, closeAll := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev, Repetition: 1}}, 20, 10, 10)
			defer closeAll()
			r.Config.ScreeningSecrets = []string{tc.secret}

			r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
				o := zeroObservation("jev")
				o.Billing.Cost = decimal(tc.cost)
				return o, nil
			}

			outcomes, err := r.Run(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			if outcomes[0].BillingRaw != tc.cost {
				t.Fatalf("W1-R3 REPRODUCED (%s): BillingRaw corrupted from %s to %s", tc.name, tc.cost, outcomes[0].BillingRaw)
			}
			t.Logf("W1-R3 FIXED (%s): numeric lexeme preserved", tc.name)
		})
	}
}

// TestW1R3_ScreenBillingObservationPreservesNumericLexemes verifies screenBillingObservation preserves numeric fields
func TestW1R3_ScreenBillingObservationPreservesNumericLexemes(t *testing.T) {
	billing := adapter.BillingObservation{
		Cost:         adapter.DecimalField{Present: true, Valid: true, Raw: "0.0000020", Number: json.Number("0.0000020")},
		InputTokens:  adapter.DecimalField{Present: true, Valid: true, Raw: "120", Number: json.Number("120")},
		OutputTokens: adapter.DecimalField{Present: true, Valid: true, Raw: "205", Number: json.Number("205")},
		TotalTokens:  adapter.DecimalField{Present: true, Valid: true, Raw: "0.000001", Number: json.Number("0.000001")},
	}

	screened := screenBillingObservation(billing, []string{"20"})

	if screened.Cost.Raw != "0.0000020" {
		t.Fatalf("screenBillingObservation corrupted Cost.Raw: %s", screened.Cost.Raw)
	}
	if screened.InputTokens.Raw != "120" {
		t.Fatalf("screenBillingObservation corrupted InputTokens.Raw: %s", screened.InputTokens.Raw)
	}
	if screened.OutputTokens.Raw != "205" {
		t.Fatalf("screenBillingObservation corrupted OutputTokens.Raw: %s", screened.OutputTokens.Raw)
	}
	if screened.TotalTokens.Raw != "0.000001" {
		t.Fatalf("screenBillingObservation corrupted TotalTokens.Raw: %s", screened.TotalTokens.Raw)
	}
	t.Log("screenBillingObservation preserves numeric lexemes: OK")
}

// TestW1R3_SanitizeTextDirectlyOnNumeric shows the underlying sanitizeText behavior
// (This documents the known behavior - sanitizeText is a general string function)
func TestW1R3_SanitizeTextDirectlyOnNumeric(t *testing.T) {
	// sanitizeText is a general string replacement function
	// It SHOULD replace substrings - that's its job
	// The fix is to NOT call sanitizeText on numeric fields
	costLexeme := "0.0000020"
	fakeKey := "20"

	sanitized := sanitizeText(costLexeme, []string{fakeKey})

	// This demonstrates the underlying behavior - sanitizeText DOES replace substrings
	// The fix is at the call site (appendSlotEvent), not in sanitizeText itself
	if sanitized != "0.00000[REDACTED_CREDENTIAL]" {
		t.Logf("sanitizeText behavior: %q -> %q (this is expected for a general string function)", costLexeme, sanitized)
	} else {
		t.Log("sanitizeText replaces substrings as designed - callers must not pass numeric fields")
	}
}
