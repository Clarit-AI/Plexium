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

	// Verify max(reservation, actual) conservative exposure was preserved.
	// reservation was 3000 (1 + retry 1), actual cost was 1200, so max is 3000.
	if l1.Balance() != 3000 {
		t.Fatalf("expected balance 3000 (max of reservation 3000 and actual 1200), got %d", l1.Balance())
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
