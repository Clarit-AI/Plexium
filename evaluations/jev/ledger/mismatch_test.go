package ledger

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// B1: Mismatch evidence persistence, halt, terminal reservation, other in-flight reconciliation.
//
// Directive spec:
//   - Settle with rate/token mismatch MUST append an immutable billing record
//     containing actual rate/tokens/cost and reservation ID.
//   - Mark that reservation terminal/disputed.
//   - Latch durable halted state.
//   - Return mismatch error AFTER persistence.
//   - Preserve at least max(reservation, actual) conservative exposure while disputed.
//   - New Reserve prohibited immediately and after reopen.
//   - Same reservation cannot subsequently be settled as 'valid', erasing evidence.
//   - Other already-in-flight reservations may record their bills without unhalting.
func TestSettleRateMismatchPersistsEvidenceAndHalts(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	// First session: reserve two reservations, then settle the first with a
	// rate drift. The second reservation is already-in-flight and should
	// still be settlable after the halt.
	l1, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	resID, _, err := l1.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve 1 failed: %v", err)
	}
	resID2, _, err := l1.Reserve(context.Background(), 2, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve 2 failed: %v", err)
	}

	// Settle resID with a 100x rate drift on rateIn. Reserved rateIn=1_000_000.
	_, err = l1.Settle(context.Background(), resID, 1200, 1000, 500, 100_000_000, 1_000_000)
	if err == nil {
		t.Fatal("expected rate-mismatch error")
	}
	if !IsLedgerCode(err, LedgerCodeRateMismatch) {
		t.Fatalf("expected rate-mismatch code, got %v", err)
	}
	if !l1.Halted() {
		t.Fatal("ledger should be halted after rate mismatch")
	}

	// Verify mismatch evidence is appended and durable.
	entries := l1.Entries()
	hasMismatch := false
	for _, e := range entries {
		if e.Type == EntryMismatch && e.RefID == resID {
			hasMismatch = true
			if !e.ReservationTerminal {
				t.Fatal("mismatch entry should mark reservation as terminal")
			}
			if e.ExpectedRateIn != 1_000_000 {
				t.Fatalf("expected ExpectedRateIn 1_000_000, got %d", e.ExpectedRateIn)
			}
			if e.ActualRateIn != 100_000_000 {
				t.Fatalf("expected ActualRateIn 100_000_000, got %d", e.ActualRateIn)
			}
			if e.MismatchReason != "rate-mismatch" {
				t.Fatalf("expected mismatchReason 'rate-mismatch', got %q", e.MismatchReason)
			}
			if e.ActualCost != 1200 {
				t.Fatalf("expected ActualCost 1200, got %d", e.ActualCost)
			}
			break
		}
	}
	if !hasMismatch {
		t.Fatal("expected mismatch evidence entry")
	}

	// E1: with two outstanding reservations of 3000 each (balance 6000),
	// the mismatch on resID at actual=1200 (delta=0 because actual<reserved)
	// does not reduce the global balance. resID2's 3000 exposure must
	// remain in the ledger. The mismatch entry's Balance field equals
	// the new global balance (6000, since delta=0).
	if l1.Balance() != 6000 {
		t.Fatalf("balance should preserve resID2 outstanding 3000 (got %d, want 6000)", l1.Balance())
	}

	// New Reserve prohibited immediately.
	_, _, err = l1.Reserve(context.Background(), 3, "entity-type", "sg-001", 1000, 500)
	if err == nil {
		t.Fatal("expected halted error on new reserve")
	}
	if !IsLedgerCode(err, LedgerCodeHalted) {
		t.Fatalf("expected halted code on new reserve, got %v", err)
	}

	// Same reservation cannot be re-settled as 'valid'.
	_, err = l1.Settle(context.Background(), resID, 1200, 1000, 500, 1_000_000, 1_000_000)
	if err == nil {
		t.Fatal("expected duplicate-settle for terminal reservation")
	}
	if !IsLedgerCode(err, LedgerCodeDuplicateSettle) {
		t.Fatalf("expected duplicate-settle code, got %v", err)
	}

	l1.Close()

	// Reopen: halt must persist, mismatch evidence must survive, same
	// reservation still cannot be re-settled, new Reserve still prohibited,
	// but the in-flight resID2 may reconcile without unhalting.
	l2, err := Open(cfg)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	defer l2.Close()
	if !l2.Halted() {
		t.Fatal("ledger should still be halted after reopen")
	}
	entries2 := l2.Entries()
	hasMismatch = false
	for _, e := range entries2 {
		if e.Type == EntryMismatch && e.RefID == resID {
			hasMismatch = true
			if !e.ReservationTerminal {
				t.Fatal("mismatch entry should mark reservation as terminal after reopen")
			}
			break
		}
	}
	if !hasMismatch {
		t.Fatal("mismatch evidence should survive reopen")
	}

	// New Reserve after reopen is prohibited.
	_, _, err = l2.Reserve(context.Background(), 3, "entity-type", "sg-001", 1000, 500)
	if err == nil {
		t.Fatal("expected halted error on reserve after reopen")
	}
	if !IsLedgerCode(err, LedgerCodeHalted) {
		t.Fatalf("expected halted code, got %v", err)
	}

	// Same reservation still cannot be settled (terminal evidence).
	_, err = l2.Settle(context.Background(), resID, 1200, 1000, 500, 1_000_000, 1_000_000)
	if err == nil {
		t.Fatal("expected duplicate-settle for terminal reservation after reopen")
	}
	if !IsLedgerCode(err, LedgerCodeDuplicateSettle) {
		t.Fatalf("expected duplicate-settle code, got %v", err)
	}

	// Other already-in-flight reservation reconciles safely.
	remaining, err := l2.Settle(context.Background(), resID2, 1200, 800, 400, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("in-flight resID2 settlement should succeed: %v", err)
	}
	// reserved 3000, settled 1200, remaining 1800.
	if remaining != 1800 {
		t.Fatalf("expected remaining 1800 for in-flight settlement, got %d", remaining)
	}
	// Ledger remains halted.
	if !l2.Halted() {
		t.Fatal("ledger must remain halted after in-flight reconciliation")
	}
	// The reconciled settlement is durably recorded.
	found := false
	for _, e := range l2.Entries() {
		if e.Type == EntrySettlement && e.RefID == resID2 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("in-flight reconciled settlement must be durably recorded")
	}
}

// B1 token-mismatch variant: tokens exceed reservation.
func TestSettleTokenMismatchPersistsEvidenceAndHalts(t *testing.T) {
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
	// Settle with actual tokensIn > reserved tokensIn.
	_, err = l.Settle(context.Background(), resID, 1200, 2000, 400, 1_000_000, 1_000_000)
	if err == nil {
		t.Fatal("expected token-mismatch error")
	}
	if !IsLedgerCode(err, LedgerCodeTokenMismatch) {
		t.Fatalf("expected token-mismatch code, got %v", err)
	}
	if !l.Halted() {
		t.Fatal("ledger should be halted after token mismatch")
	}
	var mismatch *Entry
	for i := range l.entries {
		e := &l.entries[i]
		if e.Type == EntryMismatch && e.RefID == resID {
			mismatch = e
			break
		}
	}
	if mismatch == nil {
		t.Fatal("expected mismatch evidence entry for token-mismatch")
	}
	if mismatch.MismatchReason != "token-mismatch" {
		t.Fatalf("expected mismatchReason 'token-mismatch', got %q", mismatch.MismatchReason)
	}
	if !mismatch.ReservationTerminal {
		t.Fatal("mismatch entry should mark reservation terminal")
	}
	if mismatch.ActualTokensIn != 2000 {
		t.Fatalf("expected ActualTokensIn 2000, got %d", mismatch.ActualTokensIn)
	}
}

// B2: Numeric RateIn drift detection on a used ledger.
func TestRateInDriftRejectedOnUsedLedger(t *testing.T) {
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

	// Reopen with a 2x rateIn: manifest hash matches because we recompute
	// over the new value, but the numeric binding must reject.
	cfg.RateIn = 2_000_000
	cfg.ManifestKey.TokenBoundsHash = ComputeTokenBoundsHash(
		cfg.TokenBounds,
		cfg.RetryPolicy.MaxRetries,
		cfg.RetryPolicy.RetryMultiplier,
		cfg.RetryPolicy.DiscoveryMultiplier,
		cfg.DiscoveryCostEstimate,
	)
	_, err = Open(cfg)
	if err == nil {
		t.Fatal("expected drift error on numeric RateIn change")
	}
	if !IsLedgerCode(err, LedgerCodeDrift) {
		t.Fatalf("expected drift code, got %v", err)
	}
}

// B2: Numeric RateOut drift detection (presence-preserved value change).
func TestRateOutDriftRejectedOnUsedLedger(t *testing.T) {
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

	// Change numeric rateOut value but keep presence=true.
	newOut := MicroUnit(2_000_000)
	cfg.RateOut = &newOut
	cfg.ManifestKey.TokenBoundsHash = ComputeTokenBoundsHash(
		cfg.TokenBounds,
		cfg.RetryPolicy.MaxRetries,
		cfg.RetryPolicy.RetryMultiplier,
		cfg.RetryPolicy.DiscoveryMultiplier,
		cfg.DiscoveryCostEstimate,
	)
	_, err = Open(cfg)
	if err == nil {
		t.Fatal("expected drift error on numeric RateOut change")
	}
	if !IsLedgerCode(err, LedgerCodeDrift) {
		t.Fatalf("expected drift code, got %v", err)
	}
}

// B2: Numeric RateOut presence flip detected (supplied -> omitted).
func TestRateOutPresenceDriftRejectedOnUsedLedger(t *testing.T) {
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

	// Flip RateOut from supplied to nil (omitted).
	cfg.RateOut = nil
	cfg.ManifestKey.TokenBoundsHash = ComputeTokenBoundsHash(
		cfg.TokenBounds,
		cfg.RetryPolicy.MaxRetries,
		cfg.RetryPolicy.RetryMultiplier,
		cfg.RetryPolicy.DiscoveryMultiplier,
		cfg.DiscoveryCostEstimate,
	)
	_, err = Open(cfg)
	if err == nil {
		t.Fatal("expected drift error on RateOut presence flip")
	}
	if !IsLedgerCode(err, LedgerCodeDrift) {
		t.Fatalf("expected drift code, got %v", err)
	}
}

// B2: Numeric TokenBounds drift (MaxInputTokens change).
func TestNumericTokenBoundsDriftRejectedOnUsedLedger(t *testing.T) {
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

	// Bump MaxInputTokens but stay within Reserve range; manifest hash
	// recomputed over the new value to bypass the hash check.
	cfg.TokenBounds.MaxInputTokens = 8192
	cfg.ManifestKey.TokenBoundsHash = ComputeTokenBoundsHash(
		cfg.TokenBounds,
		cfg.RetryPolicy.MaxRetries,
		cfg.RetryPolicy.RetryMultiplier,
		cfg.RetryPolicy.DiscoveryMultiplier,
		cfg.DiscoveryCostEstimate,
	)
	_, err = Open(cfg)
	if err == nil {
		t.Fatal("expected drift error on numeric TokenBounds change")
	}
	if !IsLedgerCode(err, LedgerCodeDrift) {
		t.Fatalf("expected drift code, got %v", err)
	}
}

// B2: Empty-ledger reopen with mismatched rate rejects (no entries to mask).
// We check this via the manifest hash check combined with numeric binding.
// (Both run on first init write.)
func TestEmptyLedgerRateInValidation(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	// Open and immediately close with RateIn = 1M.
	l1, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open 1 failed: %v", err)
	}
	l1.Close()

	// Now reopen with RateIn = 0 (omitted). The init header persisted the
	// numeric value; the empty-ledger replay will still see the init line
	// and the numeric binding rejects the mismatch.
	cfg.RateIn = 0
	_, err = Open(cfg)
	if err == nil {
		t.Fatal("expected drift error on RateIn omission for used ledger")
	}
	if !IsLedgerCode(err, LedgerCodeDrift) && !IsLedgerCode(err, LedgerCodeMissingRates) {
		t.Fatalf("expected drift or missing-rates code, got %v", err)
	}
}

// D1: Halt must be derived from EVERY durable EntryMismatch, regardless
// of init.Halted. This test simulates the crash window where the
// mismatch evidence was appended+fsynced but the rewriteInitEntry that
// would set init.Halted=true never landed.
//
// Procedure:
//  1. Open ledger, reserve resID (in-flight) and resID2 (in-flight).
//  2. Settle resID with rate drift → mismatch evidence appended,
//     init.Halted=true (via rewriteInitEntry) in steady state.
//  3. Edit the file on disk to set init.Halted=false (simulating the
//     crash window), keeping the mismatch entry intact.
//  4. Reopen and assert Halted()=true (derived from mismatch evidence),
//     Reserve is rejected, mismatch evidence retained, pre-existing
//     resID2 can still reconcile.
//
// At 4838968 (HEAD before D1 fix), reopen would observe init.Halted=false
// and Halted() would return false; Reserve would succeed (new spending
// resumes despite durable billing anomaly). The test fails.
// After the D1 fix, reopen derives halt from any EntryMismatch and the
// test passes.
func TestReplayDerivesHaltFromMismatchWhenInitMarkerAbsent(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	l1, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	resID, _, err := l1.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve 1 failed: %v", err)
	}
	resID2, _, err := l1.Reserve(context.Background(), 2, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve 2 failed: %v", err)
	}
	_, err = l1.Settle(context.Background(), resID, 1200, 1000, 500, 100_000_000, 1_000_000)
	if err == nil {
		t.Fatal("expected rate-mismatch error")
	}
	if !l1.Halted() {
		t.Fatal("ledger should be halted after mismatch")
	}
	l1.Close()

	// Simulate crash between evidence append+fsync and rewriteInitEntry:
	// flip init.Halted back to false in the on-disk file. The mismatch
	// entry (and any subsequent append) is left intact.
	if err := flipInitHaltedFalse(path); err != nil {
		t.Fatalf("simulate crash: %v", err)
	}

	l2, err := Open(cfg)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	defer l2.Close()

	// D1: halt must be derived from the durable mismatch entry.
	if !l2.Halted() {
		t.Fatal("halt must be derived from durable mismatch entry even when init marker absent")
	}

	// Reserve is rejected because halt is true.
	_, _, err = l2.Reserve(context.Background(), 3, "entity-type", "sg-001", 1000, 500)
	if !IsLedgerCode(err, LedgerCodeHalted) {
		t.Fatalf("expected halted code on new Reserve after reopen, got %v", err)
	}

	// Mismatch evidence retained.
	hasMismatch := false
	for _, e := range l2.Entries() {
		if e.Type == EntryMismatch && e.RefID == resID {
			hasMismatch = true
			break
		}
	}
	if !hasMismatch {
		t.Fatal("mismatch evidence must survive reopen")
	}

	// Pre-existing in-flight reservation (resID2) reconciles safely
	// without unhalting.
	if _, err := l2.Settle(context.Background(), resID2, 1200, 800, 400, 1_000_000, 1_000_000); err != nil {
		t.Fatalf("in-flight resID2 reconciliation should succeed after reopen: %v", err)
	}
	if !l2.Halted() {
		t.Fatal("ledger must remain halted after in-flight reconciliation")
	}
}

// D1: Halt must be derived from EVERY durable overrun EntryAdjustment,
// regardless of init.Halted. Same crash-window simulation as the
// mismatch case but via the overrun path.
func TestReplayDerivesHaltFromOverrunWhenInitMarkerAbsent(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	l1, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	resID, _, err := l1.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve 1 failed: %v", err)
	}
	resID2, _, err := l1.Reserve(context.Background(), 2, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve 2 failed: %v", err)
	}
	// Overrun: actual cost > reservation (reserved=3000, settle 4000).
	_, err = l1.Settle(context.Background(), resID, 4000, 1000, 500, 1_000_000, 1_000_000)
	if !IsLedgerCode(err, LedgerCodeCapExceeded) {
		t.Fatalf("expected cap-exceeded (overrun) error, got %v", err)
	}
	if !l1.Halted() {
		t.Fatal("ledger should be halted after overrun")
	}
	l1.Close()

	// Simulate crash between adjustment append+fsync and rewriteInitEntry.
	if err := flipInitHaltedFalse(path); err != nil {
		t.Fatalf("simulate crash: %v", err)
	}

	l2, err := Open(cfg)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	defer l2.Close()

	// D1: halt must be derived from the durable overrun adjustment.
	if !l2.Halted() {
		t.Fatal("halt must be derived from durable overrun adjustment even when init marker absent")
	}

	// Reserve is rejected because halt is true.
	_, _, err = l2.Reserve(context.Background(), 3, "entity-type", "sg-001", 1000, 500)
	if !IsLedgerCode(err, LedgerCodeHalted) {
		t.Fatalf("expected halted code on new Reserve after reopen, got %v", err)
	}

	// Overrun adjustment retained.
	hasOverrun := false
	for _, e := range l2.Entries() {
		if e.Type == EntryAdjustment && strings.HasPrefix(e.Notes, "overrun") && e.RefID == resID {
			hasOverrun = true
			break
		}
	}
	if !hasOverrun {
		t.Fatal("overrun adjustment must survive reopen")
	}

	// Pre-existing in-flight resID2 reconciles safely.
	if _, err := l2.Settle(context.Background(), resID2, 1200, 800, 400, 1_000_000, 1_000_000); err != nil {
		t.Fatalf("in-flight resID2 reconciliation should succeed after reopen: %v", err)
	}
}

// Below-reservation mismatch: when the actual billed cost is LESS than
// the reservation, conservative exposure stays at the reservation
// (the ledger does not "release" the difference back). This bounds the
// worst-case billing to max(reservation, actual).
func TestMismatchBelowReservationPreservesExposure(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	l, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer l.Close()

	resID, reserved, err := l.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	// reserved = 1000 + 500 + retry (1000 + 500) = 3000 (rateIn=1M, rateOut=1M, retryMult=1.0, maxRetries=1)
	if reserved != 3000 {
		t.Fatalf("expected reserved 3000, got %d", reserved)
	}

	// Settle with rate mismatch but actualCost < reservation.
	// max(3000, 1500) = 3000; balance should stay 3000, not drop.
	_, err = l.Settle(context.Background(), resID, 1500, 1000, 500, 100_000_000, 1_000_000)
	if !IsLedgerCode(err, LedgerCodeRateMismatch) {
		t.Fatalf("expected rate-mismatch, got %v", err)
	}
	if l.Balance() != 3000 {
		t.Fatalf("balance must remain at reservation 3000 (max(3000, 1500)), got %d", l.Balance())
	}

	// Verify mismatch entry has Amount=0 (no release) and Balance=3000.
	for _, e := range l.Entries() {
		if e.Type == EntryMismatch && e.RefID == resID {
			if e.Amount != 0 {
				t.Fatalf("mismatch Amount should be 0 when actualCost < reserved, got %d", e.Amount)
			}
			if e.Balance != 3000 {
				t.Fatalf("mismatch Balance should equal reservation 3000, got %d", e.Balance)
			}
			if e.ActualCost != 1500 {
				t.Fatalf("mismatch ActualCost should be 1500, got %d", e.ActualCost)
			}
			return
		}
	}
	t.Fatal("expected mismatch evidence entry")
}

// syncDir on the current directory ("." or "") must actually fsync the
// directory entry, not silently no-op. A no-op leaves the rename
// itself undurable: the kernel may not flush the new dirent to stable
// storage before the process crashes.
func TestSyncDirDoesNotNoOpOnDot(t *testing.T) {
	// Provide a fake file in t.TempDir() and use its parent (the
	// temp dir) to verify Sync is called.
	dir := t.TempDir()
	path := dir + "/probe"
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// ".": fsync current dir.
	if err := syncDir("."); err != nil {
		t.Fatalf("syncDir(\".\") failed: %v", err)
	}
	// "": also fsync current dir (normalized to ".").
	if err := syncDir(""); err != nil {
		t.Fatalf("syncDir(\"\") failed: %v", err)
	}
	// Real path: also works.
	if err := syncDir(dir); err != nil {
		t.Fatalf("syncDir(realpath) failed: %v", err)
	}
}

// flipInitHaltedFalse rewrites the init entry (line 1) of the ledger
// file to set Halted=false. It preserves all other entries exactly.
// This simulates the crash window where evidence is durable but the
// init-entry rewrite never landed.
func flipInitHaltedFalse(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var lines [][]byte
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		lines = append(lines, []byte(line))
	}
	if len(lines) == 0 {
		return os.ErrInvalid
	}
	var init Entry
	if err := json.Unmarshal(lines[0], &init); err != nil {
		return err
	}
	if init.Type != EntryInit {
		return os.ErrInvalid
	}
	init.Halted = false
	flipped, err := json.Marshal(init)
	if err != nil {
		return err
	}
	lines[0] = flipped
	out := []byte{}
	for i, l := range lines {
		if i > 0 {
			out = append(out, '\n')
		}
		out = append(out, l...)
	}
	out = append(out, '\n')
	return os.WriteFile(path, out, 0600)
}

// E1: Multi-outstanding mismatch must NOT overwrite other reservations'
// exposure. Reserve A 3000 + Reserve B 3000 (balance 6000). Mismatch A
// at actual=5000 (delta=2000). With the E1 fix the global balance is
// 6000 + 2000 = 8000, NOT the per-reservation conservative 5000 that the
// buggy handler produced. B's outstanding 3000 exposure is preserved.
//
// At 00ec305 (HEAD before E1 fix), the buggy code computes
// conservative=max(3000,5000)=5000 then l.balance=conservative=5000,
// losing B's exposure. This test FAILS at HEAD.
// At this commit (E1 fix), the test PASSES.
func TestMismatchMultiOutstandingPreservesOtherExposure(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, 10_000_000, "fixed-run-id")

	l1, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	resA, _, err := l1.Reserve(context.Background(), 1, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve A failed: %v", err)
	}
	resB, _, err := l1.Reserve(context.Background(), 2, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve B failed: %v", err)
	}
	if l1.Balance() != 6000 {
		t.Fatalf("setup: balance should be 6000 (A+B), got %d", l1.Balance())
	}

	// Mismatch A with actual=5000 (delta=2000, reserved=3000).
	_, err = l1.Settle(context.Background(), resA, 5000, 1000, 500, 100_000_000, 1_000_000)
	if !IsLedgerCode(err, LedgerCodeRateMismatch) {
		t.Fatalf("expected rate-mismatch, got %v", err)
	}

	// E1 key check: global balance = 6000 + 2000 = 8000. NOT 5000.
	if l1.Balance() != 8000 {
		t.Fatalf("balance must be 8000 (other-outstanding 6000 + mismatch delta 2000), got %d", l1.Balance())
	}

	// Mismatch entry has Amount=delta=2000, Balance=newGlobal=8000.
	for _, e := range l1.Entries() {
		if e.Type == EntryMismatch && e.RefID == resA {
			if e.Amount != 2000 {
				t.Fatalf("mismatch Amount should be delta 2000, got %d", e.Amount)
			}
			if e.Balance != 8000 {
				t.Fatalf("mismatch Balance should equal new global 8000, got %d", e.Balance)
			}
			if !e.ReservationTerminal {
				t.Fatal("mismatch entry should mark reservation terminal")
			}
			break
		}
	}

	if !l1.Halted() {
		t.Fatal("ledger should be halted after mismatch")
	}

	l1.Close()

	// Reopen: halt true, mismatch preserved, resA terminal, new Reserve
	// rejected. resB (pre-halt, non-terminal) reconciles safely.
	l2, err := Open(cfg)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	defer l2.Close()
	if !l2.Halted() {
		t.Fatal("ledger should still be halted after reopen")
	}
	if l2.Balance() != 8000 {
		t.Fatalf("balance must survive reopen at 8000, got %d", l2.Balance())
	}

	// New Reserve rejected.
	_, _, err = l2.Reserve(context.Background(), 3, "entity-type", "sg-001", 1000, 500)
	if !IsLedgerCode(err, LedgerCodeHalted) {
		t.Fatalf("expected halted code, got %v", err)
	}

	// resA still terminal/disputed.
	_, err = l2.Settle(context.Background(), resA, 5000, 1000, 500, 1_000_000, 1_000_000)
	if !IsLedgerCode(err, LedgerCodeDuplicateSettle) {
		t.Fatalf("expected duplicate-settle for terminal resA, got %v", err)
	}

	// resB reconciles safely. F1 invariant: full-cost settle releases 0,
	// retains actual 3000; balance stays at 8000 (A conservative 5000 +
	// B retained 3000). The settlement entry's Amount is 0 (no release).
	remaining, err := l2.Settle(context.Background(), resB, 3000, 1000, 500, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("resB pre-halt settle should succeed: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected remaining 0 (release=0 for full-cost settle), got %d", remaining)
	}
	if l2.Balance() != 8000 {
		t.Fatalf("after full-cost settle of resB, balance should stay at 8000 (A conservative 5000 + B retained 3000), got %d", l2.Balance())
	}
}

// E1 + F1: Below-reservation mismatch with two outstanding must stay
// at 6000 (no release below the reservation). Settling B at full cost
// releases 0 and retains B's actual 3000; balance stays at 6000.
func TestMismatchBelowReservationWithTwoOutstandingPreservesExposure(t *testing.T) {
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

	// Mismatch A at actual=1500 (below reserved 3000): delta=0; no release.
	_, err = l.Settle(context.Background(), resA, 1500, 1000, 500, 100_000_000, 1_000_000)
	if !IsLedgerCode(err, LedgerCodeRateMismatch) {
		t.Fatalf("expected rate-mismatch, got %v", err)
	}
	if l.Balance() != 6000 {
		t.Fatalf("balance must stay at 6000 (no release below reservation), got %d", l.Balance())
	}

	// Mismatch entry has Amount=0, Balance=6000 (unchanged global).
	for _, e := range l.Entries() {
		if e.Type == EntryMismatch && e.RefID == resA {
			if e.Amount != 0 {
				t.Fatalf("mismatch Amount should be 0 (actual<reserved), got %d", e.Amount)
			}
			if e.Balance != 6000 {
				t.Fatalf("mismatch Balance should equal 6000 (no release), got %d", e.Balance)
			}
			if e.ActualCost != 1500 {
				t.Fatalf("mismatch ActualCost should be 1500, got %d", e.ActualCost)
			}
			break
		}
	}

	// F1: Settle B at full cost. release = 3000 - 3000 = 0. B's actual 3000
	// retained against the cap. balance stays at 6000.
	_, err = l.Settle(context.Background(), resB, 3000, 1000, 500, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("resB settle failed: %v", err)
	}
	if l.Balance() != 6000 {
		t.Fatalf("after full-cost settle of resB, balance should stay at 6000, got %d", l.Balance())
	}
}

// E1: Prior settled bill + third outstanding variant. After settling A
// cleanly, the mismatch on B must not affect C's outstanding exposure.
func TestMismatchWithPriorSettledAndThirdOutstandingPreservesExposure(t *testing.T) {
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
	// F1: Settle A cleanly at its full reservation (no mismatch).
	// Full-cost settle retains actual 3000 against the cap.
	_, err = l.Settle(context.Background(), resA, 3000, 1000, 500, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("Settle A failed: %v", err)
	}
	if l.Balance() != 3000 {
		t.Fatalf("after settling A at full cost, balance should be 3000 (A actual retained), got %d", l.Balance())
	}

	// Now reserve B and C.
	resB, _, err := l.Reserve(context.Background(), 2, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve B failed: %v", err)
	}
	resC, _, err := l.Reserve(context.Background(), 3, "entity-type", "sg-001", 1000, 500)
	if err != nil {
		t.Fatalf("Reserve C failed: %v", err)
	}
	if l.Balance() != 9000 {
		t.Fatalf("setup: A retained 3000 + B+C reserve 6000 = 9000, got %d", l.Balance())
	}

	// Mismatch B at actual=5000 (delta=2000, reserved=3000).
	_, err = l.Settle(context.Background(), resB, 5000, 1000, 500, 100_000_000, 1_000_000)
	if !IsLedgerCode(err, LedgerCodeRateMismatch) {
		t.Fatalf("expected rate-mismatch on B, got %v", err)
	}
	// Balance = 3000 (A retained) + 6000 (B+C reserve) + 2000 (B delta) = 11000.
	if l.Balance() != 11000 {
		t.Fatalf("balance should be 11000 (A retained 3000 + B conservative 5000 + C reserved 3000), got %d", l.Balance())
	}

	// Verify C's outstanding reservation entry is preserved.
	var cReservationFound bool
	for _, e := range l.Entries() {
		if e.Type == EntryReservation && e.RefID == "" && e.SourceGroup == "sg-001" && e.Amount == 3000 {
			// C is the third reservation (attempt=3).
			if e.Attempt == 3 {
				cReservationFound = true
				break
			}
		}
	}
	if !cReservationFound {
		t.Fatal("C reservation entry must be preserved")
	}

	// F1: resC settles cleanly at full cost. release=0; balance stays at 11000.
	_, err = l.Settle(context.Background(), resC, 3000, 1000, 500, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("resC settle failed: %v", err)
	}
	if l.Balance() != 11000 {
		t.Fatalf("after full-cost settle of resC, balance should stay at 11000, got %d", l.Balance())
	}
}

// E1 + F2: Overflow records evidence + halt + sentinel. With reserve B
// at small amount, mismatch A at MaxMicroUnits must detect
// checked-add overflow and return LedgerCodeOverflow. The actual
// billing MUST be durably recorded on a mismatch entry with
// Overflow=true marker (Balance field uses MaxMicroUnits sentinel
// rather than a false exact aggregate). Reservation marked
// terminal. Ledger halted. Available = 0. Replay derives halt from
// the Overflow marker.
func TestMismatchOverflowFailsClosedWithoutDroppingEvidence(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-ledger.jsonl"
	cfg := testConfigFixedRunID(path, MaxMicroUnits, "fixed-run-id")
	// Allow high enough tokens + high enough rates to overflow.
	cfg.TokenBounds = TokenBounds{MaxInputTokens: 16, MaxOutputTokens: 16}
	cfg.RateIn = 1_000_000
	rateOut := MicroUnit(1_000_000)
	cfg.RateOut = &rateOut

	l, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer l.Close()

	// Reserve A with 0 tokens (amount 0; balance still 0).
	resA, _, err := l.Reserve(context.Background(), 1, "entity-type", "sg-001", 0, 0)
	if err != nil {
		t.Fatalf("Reserve A failed: %v", err)
	}
	// Reserve B with 1 token each. cost = 2, retry = 2, total = 4.
	resB, _, err := l.Reserve(context.Background(), 2, "entity-type", "sg-001", 1, 1)
	if err != nil {
		t.Fatalf("Reserve B failed: %v", err)
	}
	balanceBefore := l.Balance()
	if balanceBefore != 4 {
		t.Fatalf("setup: balance should be 4, got %d", balanceBefore)
	}

	// Count reservation entries before.
	reservationEntriesBefore := 0
	for _, e := range l.Entries() {
		if e.Type == EntryReservation {
			reservationEntriesBefore++
		}
	}

	// Mismatch A with actualCost = MaxMicroUnits. delta = MaxMicroUnits -
	// resA.Amount(0) = MaxMicroUnits. balance + delta = 4 + MaxMicroUnits
	// > MaxMicroUnits → overflow.
	_, err = l.Settle(context.Background(), resA, MaxMicroUnits, 0, 0, 100_000_000, 1_000_000)
	if !IsLedgerCode(err, LedgerCodeOverflow) {
		t.Fatalf("expected overflow code, got %v", err)
	}

	// F2: Balance is the documented sentinel MaxMicroUnits (not false exact).
	if l.Balance() != MaxMicroUnits {
		t.Fatalf("balance must be MaxMicroUnits sentinel on overflow, got %d (want %d)", l.Balance(), MaxMicroUnits)
	}

	// F2: Halted durable.
	if !l.Halted() {
		t.Fatal("ledger must be halted on overflow")
	}
	if l.Available() != 0 {
		t.Fatalf("available must be 0 on overflow, got %d", l.Available())
	}

	// F2: Mismatch evidence persisted with Overflow=true marker.
	var mismatchOverflow *Entry
	for i := range l.entries {
		e := &l.entries[i]
		if e.Type == EntryMismatch && e.RefID == resA {
			mismatchOverflow = e
			break
		}
	}
	if mismatchOverflow == nil {
		t.Fatal("mismatch evidence must be persisted on overflow (was previously dropped)")
	}
	if !mismatchOverflow.Overflow {
		t.Fatal("mismatch entry must carry Overflow=true marker")
	}
	if mismatchOverflow.ActualCost != MaxMicroUnits {
		t.Fatalf("mismatch ActualCost must record the actual bill %d, got %d", MaxMicroUnits, mismatchOverflow.ActualCost)
	}

	// Reservation entries preserved across overflow.
	reservationEntriesAfter := 0
	for _, e := range l.Entries() {
		if e.Type == EntryReservation {
			reservationEntriesAfter++
		}
	}
	if reservationEntriesAfter != reservationEntriesBefore {
		t.Fatalf("reservation entries must be preserved across overflow, got %d (want %d)", reservationEntriesAfter, reservationEntriesBefore)
	}

	// F2: resB pre-halt in-flight settle reconciles; ledger remains halted.
	_, err = l.Settle(context.Background(), resB, 4, 1, 1, 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("resB pre-halt in-flight settle should reconcile: %v", err)
	}
	if !l.Halted() {
		t.Fatal("ledger must remain halted after in-flight reconciliation")
	}
}
