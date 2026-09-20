package ledger

import (
	"testing"
)

func TestOpenNewLedger(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id") // $10 cap
	l, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer l.Close()
	if l.Balance() != 0 {
		t.Fatalf("expected balance 0, got %d", l.Balance())
	}
	if l.Available() != 10_000_000 {
		t.Fatalf("expected available 10_000_000, got %d", l.Available())
	}
}

func TestOpenExistingLedgerReplays(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	// Create and write some entries.
	l1, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open 1 failed: %v", err)
	}
	resID, reserved, err := l1.Reserve(NoopContext(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	// base = 1000 + 500 = 1500 (rate is per 1M tokens)
	// with maxRetries=1, retryMult=1.0: retryBudget = 1500
	// total = 3000
	if reserved != 3000 {
		t.Fatalf("expected reserved 3000, got %d", reserved)
	}
	_, err = l1.Settle(NoopContext(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("Settle failed: %v", err)
	}
	balance1 := l1.Balance()
	l1.Close()

	// Reopen and verify replay.
	l2, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open 2 failed: %v", err)
	}
	defer l2.Close()
	if l2.Balance() != balance1 {
		t.Fatalf("replay balance mismatch: %d vs %d", l2.Balance(), balance1)
	}
	// Should have: reservation, settlement (2 entries)
	if len(l2.Entries()) != 2 {
		t.Fatalf("expected 2 entries on replay, got %d", len(l2.Entries()))
	}
}

func TestReserveSettleRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")
	l, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer l.Close()

	// Reserve for attempt 1.
	resID, reserved, err := l.Reserve(NoopContext(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	if reserved != 3000 {
		t.Fatalf("expected 3000, got %d", reserved)
	}
	if l.Balance() != 3000 {
		t.Fatalf("balance after reserve: %d", l.Balance())
	}

	// Settle with actual cost less than reservation.
	remaining, err := l.Settle(NoopContext(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("Settle failed: %v", err)
	}
	if remaining != 1800 {
		t.Fatalf("expected remaining 1800, got %d", remaining)
	}
	// Balance = reserved - settled = 3000 - 1200 = 1800
	if l.Balance() != 1800 {
		t.Fatalf("balance after settle: %d", l.Balance())
	}

	// Available should be cap - balance = 10_000_000 - 1800 = 9_998_200
	if l.Available() != 9_998_200 {
		t.Fatalf("available after settle: %d", l.Available())
	}
}

func TestSettleDuplicateRejected(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")
	l, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer l.Close()

	resID, _, err := l.Reserve(NoopContext(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	_, err = l.Settle(NoopContext(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("First settle failed: %v", err)
	}
	// Second settle on same refID should fail.
	_, err = l.Settle(NoopContext(), resID, 1000, 500, 500, 1_000_000, 1_000_000)
	if err == nil {
		t.Fatal("expected duplicate settlement error")
	}
	if !IsLedgerCode(err, LedgerCodeDuplicateSettle) {
		t.Fatalf("expected duplicate-settlement code, got %v", err)
	}
}

func TestSettleOverrunRejected(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")
	l, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer l.Close()

	resID, _, err := l.Reserve(NoopContext(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	// Try to settle more than reserved (reserved=3000, try 4000).
	_, err = l.Settle(NoopContext(), resID, 4000, 1500, 500, 1_000_000, 1_000_000)
	if err == nil {
		t.Fatal("expected cap exceeded error on overrun")
	}
	if !IsLedgerCode(err, LedgerCodeCapExceeded) {
		t.Fatalf("expected cap-exceeded code, got %v", err)
	}
	// Balance should include the overrun adjustment.
	if l.Balance() != 4000 {
		t.Fatalf("balance should include overrun: %d", l.Balance())
	}
}

func TestReserveExceedsCapRejected(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	// Cap is $0.002 = 2000 micro-units. Reservation of 3000 will exceed.
	cfg := testConfigFixedRunID(path, 2000, "fixed-run-id")
	l, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer l.Close()

	// Try to reserve more than cap (3000 > 2000).
	_, _, err = l.Reserve(NoopContext(), 1, "entity-type", "sg-001", 1000, 500)
	if err == nil {
		t.Fatal("expected cap exceeded error")
	}
	if !IsLedgerCode(err, LedgerCodeCapExceeded) {
		t.Fatalf("expected cap-exceeded code, got %v", err)
	}
}

func TestOverflowRejected(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, MaxMicroUnits, "fixed-run-id")
	l, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer l.Close()

	// Use tokens that will cause the total reservation to exceed MaxMicroUnits.
	// base = tokensIn * rateIn / 1e6 + tokensOut * rateOut / 1e6
	// With rateIn=rateOut=1_000_000:
	// base = tokensIn + tokensOut (since rate/1e6 = 1)
	// We need total > MaxMicroUnits.
	// Use MaxMicroUnits / 2 for each to exceed when combined with retry/discovery.
	_, _, err = l.Reserve(NoopContext(), 1, "entity-type", "sg-001", MaxMicroUnits/2, MaxMicroUnits/2)
	if err == nil {
		t.Fatal("expected overflow error")
	}
	if !IsLedgerCode(err, LedgerCodeOverflow) {
		t.Fatalf("expected overflow code, got %v", err)
	}
}

func TestReservationIncludesRetriesAndDiscovery(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 100_000_000, "fixed-run-id")
	cfg.RetryPolicy = ReservationRetryPolicy{
		MaxRetries:          1,
		RetryMultiplier:     1.0,
		DiscoveryMultiplier: 0.5,
	}
	cfg.DiscoveryCostEstimate = 2_000_000
	l, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer l.Close()

	// Base: 1000 + 500 = 1500
	// Retry: 1500 * 1.0 * 1 = 1500
	// Discovery: 2M * 0.5 = 1M
	// Total: 1,003,000
	_, reserved, err := l.Reserve(NoopContext(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	expected := MicroUnit(1_003_000)
	if reserved != expected {
		t.Fatalf("expected reservation %d (base 1500 + retry 1500 + discovery 1M), got %d", expected, reserved)
	}
}

func TestSameRunResumeNoReset(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	// First session.
	l1, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open 1 failed: %v", err)
	}
	resID, _, err := l1.Reserve(NoopContext(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	_, err = l1.Settle(NoopContext(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("Settle failed: %v", err)
	}
	balance1 := l1.Balance()
	l1.Close()

	// Resume same run (same RunID, same cap).
	l2, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open 2 failed: %v", err)
	}
	defer l2.Close()

	// Balance should be preserved, not reset.
	if l2.Balance() != balance1 {
		t.Fatalf("resume balance mismatch: %d vs %d", l2.Balance(), balance1)
	}
	// Available should reflect the spent amount.
	if l2.Available() != cfg.AuthorizedCap-balance1 {
		t.Fatalf("resume available mismatch: %d", l2.Available())
	}
	// Can make new reservation within remaining cap.
	_, _, err = l2.Reserve(NoopContext(), 2, "relationship", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Resume reserve failed: %v", err)
	}
}

func TestDriftDetection(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	// Create ledger with model A and write an entry.
	l1, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open 1 failed: %v", err)
	}
	resID, _, err := l1.Reserve(NoopContext(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	_, err = l1.Settle(NoopContext(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("Settle failed: %v", err)
	}
	l1.Close()

	// Try to open with different model (same runID).
	cfg.Model = "different-model"
	_, err = Open(cfg)
	if err == nil {
		t.Fatal("expected model-drift error")
	}
	if !IsLedgerCode(err, LedgerCodeModelDrift) {
		t.Fatalf("expected model-drift code, got %v", err)
	}
}

func TestManifestDriftDetection(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	l1, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open 1 failed: %v", err)
	}
	resID, _, err := l1.Reserve(NoopContext(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	_, err = l1.Settle(NoopContext(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("Settle failed: %v", err)
	}
	l1.Close()

	// Change protocol version (simulating manifest drift).
	cfg.ManifestKey.ProtocolVersion = "9.9.9"
	_, err = Open(cfg)
	if err == nil {
		t.Fatal("expected drift error")
	}
	if !IsLedgerCode(err, LedgerCodeDrift) {
		t.Fatalf("expected drift code, got %v", err)
	}
}

func TestNegativeCapRejected(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, -1, "fixed-run-id")
	_, err := Open(cfg)
	if err == nil {
		t.Fatal("expected invalid-amount error for negative cap")
	}
	if !IsLedgerCode(err, LedgerCodeInvalidAmount) {
		t.Fatalf("expected invalid-amount code, got %v", err)
	}
}

func TestZeroTokenBoundsRejected(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")
	cfg.TokenBounds.MaxInputTokens = 0
	_, err := Open(cfg)
	if err == nil {
		t.Fatal("expected missing-bounds error for zero token bounds")
	}
	if !IsLedgerCode(err, LedgerCodeMissingBounds) {
		t.Fatalf("expected missing-bounds code, got %v", err)
	}
}

func testConfigFixedRunID(path string, cap MicroUnit, runID string) LedgerConfig {
	return LedgerConfig{
		Path:                  path,
		AuthorizedCap:         cap,
		RunID:                 runID,
		Model:                 "test-model",
		ManifestKey:           ManifestKey{FixtureFileSHA: "abc123", ProtocolVersion: "0.3.0", ModelPin: "test-model", RatesVersion: "test-1", TokenBoundsHash: "bounds1"},
		TokenBounds:           TokenBounds{MaxInputTokens: 4096, MaxOutputTokens: 2048},
		RetryPolicy:           DefaultReservationRetryPolicy(),
		DiscoveryCostEstimate: 0,
		RateIn:                1_000_000,
		RateOut:               1_000_000,
	}
}
