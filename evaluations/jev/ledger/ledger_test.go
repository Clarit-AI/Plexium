package ledger

import (
	"context"
	"os"
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
	// Verify init entry was written.
	entries := l.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 init entry, got %d", len(entries))
	}
	if entries[0].Type != EntryInit {
		t.Fatalf("expected EntryInit, got %v", entries[0].Type)
	}
	if entries[0].AuthorizedCap != 10_000_000 {
		t.Fatalf("init entry authorizedCap mismatch: %d", entries[0].AuthorizedCap)
	}
	if entries[0].ManifestKey == nil {
		t.Fatal("init entry missing manifest key")
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
	resID, reserved, err := l1.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	// base = 1000 + 500 = 1500 (rate is per 1M tokens)
	// with maxRetries=1, retryMult=1.0: retryBudget = 1500
	// total = 3000
	if reserved != 3000 {
		t.Fatalf("expected reserved 3000, got %d", reserved)
	}
	_, err = l1.Settle(context.Background(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
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
	// Should have: init, reservation, settlement (3 entries)
	if len(l2.Entries()) != 3 {
		t.Fatalf("expected 3 entries on replay, got %d", len(l2.Entries()))
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
	resID, reserved, err := l.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
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
	remaining, err := l.Settle(context.Background(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
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

	resID, _, err := l.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	_, err = l.Settle(context.Background(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("First settle failed: %v", err)
	}
	// Second settle on same refID should fail.
	_, err = l.Settle(context.Background(), resID, 1000, 500, 500, 1_000_000, 1_000_000)
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

	resID, _, err := l.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	// Try to settle more than reserved (reserved=3000, try 4000).
	// Use same tokens as reserved (1000 in, 500 out) but with higher cost.
	_, err = l.Settle(context.Background(), resID, 4000, 1000, 500, 1_000_000, 1_000_000)
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
	// Ledger should be halted.
	if !l.Halted() {
		t.Fatal("ledger should be halted after overrun")
	}
	// Further reserve should be rejected.
	_, _, err = l.Reserve(context.Background(), 2, "entity-type", "sg-001", 1000, 500)
	if err == nil {
		t.Fatal("expected halted error on reserve after overrun")
	}
	if !IsLedgerCode(err, LedgerCodeHalted) {
		t.Fatalf("expected halted code, got %v", err)
	}
	// Further settle on a new reservation should also be rejected.
	resID2, _, _ := l.Reserve(context.Background(), 2, "entity-type", "sg-001", 1000, 500) // this will fail
	if resID2 == "" {
		// Expected - reserve failed due to halt
	} else {
		_, err = l.Settle(context.Background(), resID2, 100, 100, 100, 1_000_000, 1_000_000)
		if err == nil {
			t.Fatal("expected halted error on settle after overrun")
		}
		if !IsLedgerCode(err, LedgerCodeHalted) {
			t.Fatalf("expected halted code on settle, got %v", err)
		}
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
	_, _, err = l.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
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
	// Use a config with very high rates to trigger overflow in cost calculation.
	cfg := testConfigFixedRunID(path, MaxMicroUnits, "fixed-run-id")
	rateIn := MicroUnit(MaxMicroUnits / 1000) // Very high rate
	rateOut := MicroUnit(MaxMicroUnits / 1000)
	cfg.RateIn = rateIn
	cfg.RateOut = &rateOut
	l, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer l.Close()

	// Use normal tokens but huge rates to trigger overflow.
	_, _, err = l.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
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
	_, reserved, err := l.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
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
	resID, _, err := l1.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	_, err = l1.Settle(context.Background(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
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
	_, _, err = l2.Reserve(context.Background(), 2, "relationship", "sg-001", 1000, 500)
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
	resID, _, err := l1.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	_, err = l1.Settle(context.Background(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
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
	resID, _, err := l1.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	_, err = l1.Settle(context.Background(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
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

// L1: Torn/truncated final entry fails closed.
// L1: Torn/truncated final entry fails closed.
func TestReplayTornEntryFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	// Create ledger with an entry.
	l1, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open 1 failed: %v", err)
	}
	resID, _, err := l1.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	_, err = l1.Settle(context.Background(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("Settle failed: %v", err)
	}
	l1.Close()

	// Write a known truncated JSON directly (simulate torn write more reliably).
	// This creates a file that ends mid-JSON object - cut off in the middle of a string value.
	// This ensures the JSON decoder will fail with "unexpected EOF" or syntax error.
	tokenBoundsHash := cfg.ManifestKey.TokenBoundsHash
	// Valid init entry, then a reservation entry cut off in the middle of the notes field.
	truncatedJSON := `{"id":"init-fixed-run-id-123456789","type":"init","timestamp":"2024-01-01T00:00:00Z","runId":"fixed-run-id","attempt":0,"model":"test-model","task":"","sourceGroup":"","amount":0,"balance":0,"tokensIn":0,"tokensOut":0,"rateIn":0,"rateOut":0,"notes":"initialization header","protocolVersion":"0.3.0","authorizedCap":10000000,"manifestKey":{"fixtureFileSha":"abc123","protocolVersion":"0.3.0","modelPin":"test-model","ratesVersion":"test-1","tokenBoundsHash":"` + tokenBoundsHash + `"}}
{"id":"res-fixed-run-id-1-123456789","type":"reservation","timestamp":"2024-01-01T00:00:00Z","runId":"fixed-run-id","attempt":1,"model":"test-model","task":"entity-type","sourceGroup":"sg-001","amount":3000,"balance":3000,"tokensIn":1000,"tokensOut":500,"rateIn":1000000,"rateOut":1000000,"notes":"reservation for attempt 1 task entity-type group sg-001`
	if err := os.WriteFile(path, []byte(truncatedJSON), 0600); err != nil {
		t.Fatalf("write truncated failed: %v", err)
	}

	// Reopen should fail with corrupt error (torn entry), not drift.
	_, err = Open(cfg)
	if err == nil {
		t.Fatal("expected corrupt error on torn entry")
	}
	// Should be corrupt (torn), not drift.
	if !IsLedgerCode(err, LedgerCodeCorrupt) {
		t.Fatalf("expected corrupt code, got %v", err)
	}
}

// L2: AuthorizedCap drift detection.
func TestAuthorizedCapDriftDetection(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	l1, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open 1 failed: %v", err)
	}
	resID, _, err := l1.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	_, err = l1.Settle(context.Background(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("Settle failed: %v", err)
	}
	l1.Close()

	// Change authorized cap (simulating config edit).
	cfg.AuthorizedCap = 999_000_000 // 100x increase
	_, err = Open(cfg)
	if err == nil {
		t.Fatal("expected drift error on cap change")
	}
	if !IsLedgerCode(err, LedgerCodeDrift) {
		t.Fatalf("expected drift code, got %v", err)
	}
}

// L2: Fixture SHA drift detection.
func TestFixtureSHADriftDetection(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	l1, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open 1 failed: %v", err)
	}
	resID, _, err := l1.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	_, err = l1.Settle(context.Background(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("Settle failed: %v", err)
	}
	l1.Close()

	// Change fixture SHA.
	cfg.ManifestKey.FixtureFileSHA = "different-sha"
	_, err = Open(cfg)
	if err == nil {
		t.Fatal("expected drift error on fixture SHA change")
	}
	if !IsLedgerCode(err, LedgerCodeDrift) {
		t.Fatalf("expected drift code, got %v", err)
	}
}

// L2: Rates version drift detection.
func TestRatesVersionDriftDetection(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	l1, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open 1 failed: %v", err)
	}
	resID, _, err := l1.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	_, err = l1.Settle(context.Background(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("Settle failed: %v", err)
	}
	l1.Close()

	// Change rates version.
	cfg.ManifestKey.RatesVersion = "different-rates"
	_, err = Open(cfg)
	if err == nil {
		t.Fatal("expected drift error on rates version change")
	}
	if !IsLedgerCode(err, LedgerCodeDrift) {
		t.Fatalf("expected drift code, got %v", err)
	}
}

// L2: Token bounds hash drift detection.
func TestTokenBoundsHashDriftDetection(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	l1, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open 1 failed: %v", err)
	}
	resID, _, err := l1.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	_, err = l1.Settle(context.Background(), resID, 1200, 800, 400, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("Settle failed: %v", err)
	}
	l1.Close()

	// Change token bounds hash.
	cfg.ManifestKey.TokenBoundsHash = "different-hash"
	_, err = Open(cfg)
	if err == nil {
		t.Fatal("expected drift error on token bounds hash change")
	}
	if !IsLedgerCode(err, LedgerCodeDrift) {
		t.Fatalf("expected drift code, got %v", err)
	}
}

// L4: Settlement rate mismatch rejection.
func TestSettleRateMismatchRejected(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")
	l, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer l.Close()

	resID, _, err := l.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	// Try to settle with 100x the reserved rate.
	_, err = l.Settle(context.Background(), resID, 2000, 800, 400, 100_000_000, 1_000_000)
	if err == nil {
		t.Fatal("expected rate-mismatch error")
	}
	if !IsLedgerCode(err, LedgerCodeRateMismatch) {
		t.Fatalf("expected rate-mismatch code, got %v", err)
	}
}

// L4: Settlement token mismatch rejection.
func TestSettleTokenMismatchRejected(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")
	l, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer l.Close()

	resID, _, err := l.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	// Try to settle with more tokens than reserved.
	_, err = l.Settle(context.Background(), resID, 1200, 2000, 400, 1_000_000, 1_000_000)
	if err == nil {
		t.Fatal("expected token-mismatch error")
	}
	if !IsLedgerCode(err, LedgerCodeTokenMismatch) {
		t.Fatalf("expected token-mismatch code, got %v", err)
	}
}

// L7: Token bounds enforcement.
func TestReserveTokenBoundsEnforced(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")
	l, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer l.Close()

	// Try to reserve with tokens exceeding bounds (4096).
	_, _, err1 := l.Reserve(context.Background(), 1, "entity-type", "sg-001", 5000, 500)
	if err1 == nil {
		t.Fatal("expected token-bounds error")
	}
	if !IsLedgerCode(err1, LedgerCodeTokenBounds) {
		t.Fatalf("expected token-bounds code, got %v", err1)
	}

	// Try output tokens exceeding bounds (2048).
	_, _, err2 := l.Reserve(context.Background(), 2, "entity-type", "sg-001", 1000, 3000)
	if err2 == nil {
		t.Fatal("expected token-bounds error on output")
	}
	if !IsLedgerCode(err2, LedgerCodeTokenBounds) {
		t.Fatalf("expected token-bounds code, got %v", err2)
	}
}

// L3: Halted flag persists across reopen.
func TestHaltedPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	// Cause an overrun to halt the ledger.
	l1, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open 1 failed: %v", err)
	}
	resID, _, err := l1.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	_, err = l1.Settle(context.Background(), resID, 4000, 1000, 500, 1_000_000, 1_000_000)
	if err == nil {
		t.Fatal("expected overrun error")
	}
	if !l1.Halted() {
		t.Fatal("ledger should be halted after overrun")
	}
	l1.Close()

	// Reopen with same config.
	l2, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open 2 failed: %v", err)
	}
	defer l2.Close()
	if !l2.Halted() {
		t.Fatal("ledger should still be halted after reopen")
	}
	// Reserve should be rejected.
	_, _, err = l2.Reserve(context.Background(), 2, "entity-type", "sg-001", 1000, 500)
	if err == nil {
		t.Fatal("expected halted error on reserve after reopen")
	}
	if !IsLedgerCode(err, LedgerCodeHalted) {
		t.Fatalf("expected halted code, got %v", err)
	}
}

// MicroUnitFromBaseString tests.
func TestMicroUnitFromBaseString(t *testing.T) {
	tests := []struct {
		input   string
		want    MicroUnit
		wantErr bool
		desc    string
	}{
		{"0.042", 42000, false, "0.042"},
		{"0.0420001", 42001, false, "ceiling rounds up"},
		{"1", 1000000, false, "whole unit"},
		{"0.000001", 1, false, "1 micro-unit"},
		{"0.0000005", 1, false, "tiny positive rounds up to 1"},
		{"0", 0, false, "zero"},
		{"-0.042", 0, true, "negative rejected"},
		{"abc", 0, true, "invalid string"},
		{"", 0, true, "empty string"},
		{"1.7976931348623157e+308", 0, true, "inf rejected"},
		{"nan", 0, true, "nan rejected"},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := MicroUnitFromBaseString(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("MicroUnitFromBaseString(%q): err=%v, wantErr=%v", tc.input, err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Fatalf("MicroUnitFromBaseString(%q): got %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

// ComputeTokenBoundsHash test.
func TestComputeTokenBoundsHash(t *testing.T) {
	bounds := TokenBounds{MaxInputTokens: 4096, MaxOutputTokens: 2048}
	hash1 := ComputeTokenBoundsHash(bounds, 1, 1.0, 0.5, 0)
	hash2 := ComputeTokenBoundsHash(bounds, 1, 1.0, 0.5, 0)
	if hash1 != hash2 {
		t.Fatalf("hash not deterministic: %s vs %s", hash1, hash2)
	}
	if len(hash1) != 64 {
		t.Fatalf("hash should be 64 hex chars, got %d", len(hash1))
	}
	// Different bounds -> different hash.
	hash3 := ComputeTokenBoundsHash(TokenBounds{MaxInputTokens: 8192, MaxOutputTokens: 2048}, 1, 1.0, 0.5, 0)
	if hash1 == hash3 {
		t.Fatal("different bounds should produce different hash")
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
	rateOut := MicroUnit(1_000_000)
	return LedgerConfig{
		Path:                  path,
		AuthorizedCap:         cap,
		RunID:                 runID,
		Model:                 "test-model",
		ManifestKey:           ManifestKey{FixtureFileSHA: "abc123", ProtocolVersion: "0.3.0", ModelPin: "test-model", RatesVersion: "test-1", TokenBoundsHash: ComputeTokenBoundsHash(TokenBounds{MaxInputTokens: 4096, MaxOutputTokens: 2048}, 1, 1.0, 0.5, 0)},
		TokenBounds:           TokenBounds{MaxInputTokens: 4096, MaxOutputTokens: 2048},
		RetryPolicy:           DefaultReservationRetryPolicy(),
		DiscoveryCostEstimate: 0,
		RateIn:                1_000_000,
		RateOut:               &rateOut,
	}
}
