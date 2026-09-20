package ledger

import (
	"context"
	"testing"
)

// F1: Repeated reserve+settle at full cost must exhaust the cap.
// cap=10000 (micro-units), three full-cost 3000 cycles → balance=9000,
// available=1000; the 4th reserve is rejected. State survives reopen.
func TestF1RepeatedReserveSettleExhaustsCap(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000, "fixed-run-id")

	l, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Three cycles of reserve 3000 / full-cost settle 3000.
	for i := 0; i < 3; i++ {
		resID, reserved, err := l.Reserve(context.Background(), i+1, "entity-type", "sg-001", 1000, 500)
		if err != nil {
			t.Fatalf("Reserve %d failed: %v", i+1, err)
		}
		if reserved != 3000 {
			t.Fatalf("reserve %d expected 3000, got %d", i+1, reserved)
		}
		// Settle at full cost (matches reservation): release=0, balance
		// unchanged.
		release, err := l.Settle(context.Background(), resID, 3000, 1000, 500, 1_000_000, 1_000_000)
		if err != nil {
			t.Fatalf("Settle %d failed: %v", i+1, err)
		}
		if release != 0 {
			t.Fatalf("full-cost settle %d should release 0, got %d", i+1, release)
		}
	}

	// After 3 full-cost cycles: balance=9000, available=1000.
	if l.Balance() != 9000 {
		t.Fatalf("balance should be 9000 after 3 full-cost cycles, got %d", l.Balance())
	}
	if l.Available() != 1000 {
		t.Fatalf("available should be 1000, got %d", l.Available())
	}

	// 4th reserve 3000 is rejected by cap.
	_, _, err = l.Reserve(context.Background(), 4, "entity-type", "sg-001", 1000, 500)
	if !IsLedgerCode(err, LedgerCodeCapExceeded) {
		t.Fatalf("expected cap-exceeded on 4th reserve, got %v", err)
	}

	// Close and reopen: same balance and cap state survive.
	l.Close()
	l2, err := Open(cfg)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	defer l2.Close()
	if l2.Balance() != 9000 {
		t.Fatalf("balance must survive reopen at 9000, got %d", l2.Balance())
	}
	if l2.Available() != 1000 {
		t.Fatalf("available must survive reopen at 1000, got %d", l2.Available())
	}

	// 4th reserve still rejected after reopen.
	_, _, err = l2.Reserve(context.Background(), 4, "entity-type", "sg-001", 1000, 500)
	if !IsLedgerCode(err, LedgerCodeCapExceeded) {
		t.Fatalf("expected cap-exceeded on 4th reserve after reopen, got %v", err)
	}
}

// F1: Multi-outstanding mismatch then full-cost settle B leaves
// balance at 8000 (A conservative 5000 + B retained 3000). The
// settlement releases 0 because actual equals reserved.
func TestF1MismatchThenFullCostSettleKeepsBalance(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	l, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer l.Close()

	resA, _, err := l.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve A failed: %v", err)
	}
	resB, _, err := l.Reserve(context.Background(), 2, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve B failed: %v", err)
	}
	if l.Balance() != 6000 {
		t.Fatalf("setup: balance 6000, got %d", l.Balance())
	}

	// Mismatch A at actual=5000 (delta=2000, reserved=3000).
	_, err = l.Settle(context.Background(), resA, 5000, 1000, 500, 100_000_000, 1_000_000)
	if !IsLedgerCode(err, LedgerCodeRateMismatch) {
		t.Fatalf("expected rate-mismatch, got %v", err)
	}
	if l.Balance() != 8000 {
		t.Fatalf("balance after mismatch should be 8000, got %d", l.Balance())
	}

	// F1: Full-cost settle of B releases 0; B's actual 3000 retained;
	// balance stays at 8000.
	release, err := l.Settle(context.Background(), resB, 3000, 1000, 500, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("Settle B failed: %v", err)
	}
	if release != 0 {
		t.Fatalf("full-cost settle should release 0, got %d", release)
	}
	if l.Balance() != 8000 {
		t.Fatalf("balance after full-cost settle B should stay at 8000, got %d", l.Balance())
	}
}

// F1: Partial settle retains actual; near-zero settle releases most
// of the reservation. The aggregate invariant is
// sum of (actual retained for normally-settled) +
// sum of (full unresolved reservation) +
// sum of (max(reserved, actual) for terminal/disputed).
func TestF1PartialAndNearZeroSettleRetainsActual(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 100_000_000, "fixed-run-id")

	l, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer l.Close()

	// Reserve A: 3000. Settle at actual=1500 (partial). release=1500,
	// balance=1500 (A actual retained).
	resA, _, err := l.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve A failed: %v", err)
	}
	release, err := l.Settle(context.Background(), resA, 1500, 1000, 500, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("Settle A failed: %v", err)
	}
	if release != 1500 {
		t.Fatalf("partial settle should release 1500 (reserved-actual), got %d", release)
	}
	if l.Balance() != 1500 {
		t.Fatalf("partial settle balance should be 1500 (actual retained), got %d", l.Balance())
	}

	// "Unknown billing" — settle B at actual=100 (near-zero). release
	// = 3000 - 100 = 2900. balance = 1500 + 3000 - 2900 = 1600
	// (A retained 1500 + B retained 100).
	resB, _, err := l.Reserve(context.Background(), 2, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve B failed: %v", err)
	}
	if l.Balance() != 4500 {
		t.Fatalf("after Reserve B balance should be 4500, got %d", l.Balance())
	}
	release, err = l.Settle(context.Background(), resB, 100, 1000, 500, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("Settle B failed: %v", err)
	}
	if release != 2900 {
		t.Fatalf("near-zero settle should release 2900, got %d", release)
	}
	if l.Balance() != 1600 {
		t.Fatalf("balance should be 1600 (A retained 1500 + B retained 100), got %d", l.Balance())
	}
}
