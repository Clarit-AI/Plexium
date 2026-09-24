package pilot

// Independent adversarial probes for W1-R3 (numeric fidelity vs credential
// redaction at every text-sanitization boundary).
// Reviewer: Jev Evaluation KHA-579 independent review of 9123bce.
// Direction A: cost lexemes 0.0000020 / 20 / 120 / 205 / 0.000001 must
// survive BYTE-IDENTICAL through Runner -> journal rows -> evidence with fake
// key "20".  Direction B: a genuine credential that is numeric-shaped or
// numeric-adjacent in error/halt text must still be redacted, and invalid
// string-shaped billing raws must still be screened.  Corrupted numeric or a
// leaked credential in either direction is a defect.

import (
	"context"
	"encoding/json"

	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
)

const advFakeKey = "20"

// Direction A: the five required cost lexemes through the whole pipeline.
func TestAdvW1R3a_CostLexemesByteIdenticalThroughRunner(t *testing.T) {
	for _, lex := range []string{"0.0000020", "20", "120", "205", "0.000001"} {
		t.Run(lex, func(t *testing.T) {
			// Large subcaps so big nominal costs can settle cleanly.
			r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev, Repetition: 1}}, 250_000_000, 205_000_000, 45_000_000)
			defer done()
			r.Config.ScreeningSecrets = []string{advFakeKey}
			r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
				o := zeroObservation("jev")
				o.Billing.Cost = decimal(lex)
				o.Billing.InputTokens = decimal("1")
				o.Billing.OutputTokens = decimal("1")
				o.Billing.TotalTokens = decimal("1")
				return o, nil
			}
			// A settle overrun may halt the run; evidence rows still stand.
			outcomes, _ := r.Run(context.Background())
			if len(outcomes) != 1 {
				t.Fatalf("outcomes=%d", len(outcomes))
			}
			if outcomes[0].BillingRaw != lex {
				t.Fatalf("outcome BillingRaw corrupted: %q want %q", outcomes[0].BillingRaw, lex)
			}
			// Journal rows.
			journalBytes, err := os.ReadFile(r.Journal.path)
			if err != nil {
				t.Fatal(err)
			}
			sawCostRow := false
			for i, line := range strings.Split(strings.TrimSpace(string(journalBytes)), "\n") {
				var ev JournalEvent
				if err := json.Unmarshal([]byte(line), &ev); err != nil {
					t.Fatalf("journal line %d invalid: %v", i, err)
				}
				if ev.BillingCostRaw != "" {
					sawCostRow = true
					if ev.BillingCostRaw != lex {
						t.Fatalf("W1-R3 CORRUPTION journal line %d: BillingCostRaw=%q want %q", i, ev.BillingCostRaw, lex)
					}
				}
			}
			if !sawCostRow {
				t.Fatal("no journal billing row found")
			}
			// Evidence files.
			dir := r.Config.EvidenceDir
			obsBytes, err := os.ReadFile(filepath.Join(dir, "0001-x-jev.observation.json"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(obsBytes), advFakeKey) && !strings.Contains(string(obsBytes), `"`+lex+`"`) {
				t.Fatalf("fake key in evidence without its lexeme: %s", obsBytes)
			}
			var ev ObservationEvidence
			if err := json.Unmarshal(obsBytes, &ev); err != nil {
				t.Fatal(err)
			}
			if ev.Billing.Cost.Raw != lex {
				t.Fatalf("evidence billing cost corrupted: %q want %q", ev.Billing.Cost.Raw, lex)
			}
			for _, b := range [][]byte{journalBytes, obsBytes} {
				if strings.Contains(string(b), redactedCredential) {
					t.Fatalf("redaction marker in numeric evidence (lex %s): %s", lex, b)
				}
			}
			// Report rows via replay.
			rows, err := ReplayOutcomes(r.Inventory, r.Journal.State(), dir)
			if err != nil {
				t.Fatal(err)
			}
			if rows[0].BillingRaw != lex || rows[0].Observation.Billing.Cost.Raw != lex {
				t.Fatalf("replayed rows corrupted: %q / %q", rows[0].BillingRaw, rows[0].Observation.Billing.Cost.Raw)
			}
		})
	}
}

// Direction B1: invalid string-shaped billing raws must still be screened at
// the journal/evidence boundary (the numeric rule must not open a leak).
func TestAdvW1R3b_StringShapedBillingRawStillScreened(t *testing.T) {
	t.Run("json-string raw is redacted", func(t *testing.T) {
		r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev, Repetition: 1}}, 20, 10, 10)
		defer done()
		r.Config.ScreeningSecrets = []string{advFakeKey}
		r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
			o := zeroObservation("jev")
			o.Billing.Cost = adapter.DecimalField{Present: true, Valid: false, Raw: `"err-20-err"`}
			return o, nil
		}
		_, _ = r.Run(context.Background()) // billing invalid -> halt; rows still journaled
		journalBytes, err := os.ReadFile(r.Journal.path)
		if err != nil {
			t.Fatal(err)
		}
		obsBytes, err := os.ReadFile(filepath.Join(r.Config.EvidenceDir, "0001-x-jev.observation.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(journalBytes), "err-20-err") || strings.Contains(string(obsBytes), "err-20-err") {
			t.Fatalf("W1-R3 LEAK: string-shaped billing raw reached journal/evidence unredacted:\njournal:\n%s\nobs:\n%s", journalBytes, obsBytes)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(journalBytes)), "\n") {
			var ev JournalEvent
			if json.Unmarshal([]byte(line), &ev) == nil && ev.BillingCostRaw != "" {
				if strings.Contains(ev.BillingCostRaw, "20") {
					t.Fatalf("W1-R3 LEAK: journal BillingCostRaw carries the credential: %q", ev.BillingCostRaw)
				}
			}
		}
	})
	t.Run("bare non-JSON raw is withheld hash-only", func(t *testing.T) {
		r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev, Repetition: 1}}, 20, 10, 10)
		defer done()
		r.Config.ScreeningSecrets = []string{advFakeKey}
		r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
			o := zeroObservation("jev")
			o.Billing.Cost = adapter.DecimalField{Present: true, Valid: false, Raw: "err-20-err"}
			return o, nil
		}
		_, _ = r.Run(context.Background())
		obsBytes, err := os.ReadFile(filepath.Join(r.Config.EvidenceDir, "0001-x-jev.observation.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(obsBytes), "err-20-err") {
			t.Fatalf("W1-R3 LEAK: bare invalid raw persisted unredacted: %s", obsBytes)
		}
	})
}
