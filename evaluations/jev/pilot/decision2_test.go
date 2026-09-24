package pilot

// Decision-2 (jev-policy-decisions) regressions: halt-on-discrepancy with
// positive-billing settle-then-halt, and MaxOutputTokens as a non-cost
// resource bound whose breach halts without creating billing exposure.

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
	"github.com/Clarit-AI/Plexium/evaluations/jev/ledger"
)

// TestRateSemanticsDiscrepancyAccountsBillingThenHaltsBothArms: a nonempty
// RateSemanticsDiscrepancy accounts positive billing exactly once and then
// HALTS BOTH arms (never a normal continue); with conservatively zero
// billing the reservation is retained.
func TestRateSemanticsDiscrepancyAccountsBillingThenHaltsBothArms(t *testing.T) {
	for _, cost := range []string{"0", "0.000001"} {
		t.Run("cost-"+cost, func(t *testing.T) {
			slots := []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev, PayloadSHA: "p"}, {Ordinal: 2, FixtureID: "x", Arm: ArmNano, PayloadSHA: "p"}}
			r, done := newTestRunner(t, slots, 20, 10, 10)
			defer done()
			var calls atomic.Int32
			r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
				calls.Add(1)
				o := zeroObservation("jev")
				o.Billing.Cost = decimal(cost)
				o.Billing.RateSemanticsDiscrepancy = "reported cost differs from upstream_inference_cost; tariff/currency/fee semantics unresolved"
				return o, nil
			}
			r.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
				calls.Add(1)
				return zeroObservation("nano"), nil
			}
			_, err := r.Run(context.Background())
			if err == nil || !strings.Contains(err.Error(), "rate semantics") {
				t.Fatalf("discrepancy did not halt: %v", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("both arms not halted: calls=%d", calls.Load())
			}
			if !r.Journal.State().Halted {
				t.Fatal("halt not durable")
			}
			accountings := 0
			for _, e := range r.Config.Jev.Ledger.Entries() {
				if e.Type == ledger.EntrySettlement || e.Type == ledger.EntryAdjustment || e.Type == ledger.EntryMismatch {
					accountings++
				}
			}
			if cost == "0" {
				if r.Config.Jev.Ledger.Balance() != r.Config.Jev.Reservation {
					t.Fatalf("zero-billing discrepancy released reservation: balance=%d", r.Config.Jev.Ledger.Balance())
				}
				if accountings != 0 {
					t.Fatalf("zero-billing discrepancy accounted billing %d times", accountings)
				}
			} else {
				// Positive billing accounted exactly once (no double-count).
				if accountings != 1 {
					t.Fatalf("positive billing accounted %d times, want exactly 1", accountings)
				}
				if r.Config.Jev.Ledger.Balance() != 1 {
					t.Fatalf("positive billing not accounted: balance=%d want 1", r.Config.Jev.Ledger.Balance())
				}
			}
			// Never a normal continue: the discrepancy is surfaced on the outcome.
			out, err := ReplayOutcomes(r.Inventory, r.Journal.State(), r.Config.EvidenceDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(out) == 0 || !strings.Contains(out[0].Error, "rate semantics") {
				t.Fatalf("discrepancy outcome lost: %+v", out)
			}
		})
	}
}

// TestMaxOutputTokensIsNonCostResourceBound: exceeding MaxOutputTokens
// halts the run and never represents billing exposure — the conservative
// exposure stays the reservation, and the halt is independent of cost.
func TestMaxOutputTokensIsNonCostResourceBound(t *testing.T) {
	r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev}, {Ordinal: 2, FixtureID: "x", Arm: ArmNano}}, 20, 10, 10)
	defer done()
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		o := zeroObservation("jev")
		o.Billing.Cost = decimal("0")
		o.Billing.OutputTokens = decimal("999") // exceeds OutputBound=1
		return o, nil
	}
	r.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		return zeroObservation("nano"), nil
	}
	_, err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "usage exceeds bounds") {
		t.Fatalf("output bound breach did not halt: %v", err)
	}
	if !r.Journal.State().Halted {
		t.Fatal("output bound breach did not durably halt")
	}
	// The breach is a resource halt: it created NO billing exposure beyond
	// the retained reservation.
	if r.Config.Jev.Ledger.Balance() != r.Config.Jev.Reservation {
		t.Fatalf("output bound breach altered billing exposure: balance=%d reservation=%d", r.Config.Jev.Ledger.Balance(), r.Config.Jev.Reservation)
	}
}
