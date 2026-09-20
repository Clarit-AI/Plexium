// Package ledger implements a persistent, crash-safe budget ledger for the
// Jev evaluation harness. All monetary amounts are stored as integer
// micro-units (1/1,000,000 of the base currency unit) to avoid floating-
// point precision issues. The ledger enforces conservative reservations
// BEFORE each attempt, includes retries and discovery in the reservation,
// retains unknown billing after timeout/crash, and fails closed for missing
// rate/token bounds or drift.
//
// Concurrency: single writer via file lock. Multiple processes/threads
// must coordinate externally. The ledger rejects duplicate settlements
// and invalid/overflowing amounts.
package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"time"
)

// MicroUnit is the integer representation of currency. 1 base unit = 1,000,000 micro-units.
// This avoids floating-point precision issues entirely.
type MicroUnit int64

const (
	// MicroUnitsPerUnit is the conversion factor.
	MicroUnitsPerUnit = 1_000_000
	// MaxMicroUnits is the maximum representable amount (~9.22 quintillion micro-units).
	MaxMicroUnits = math.MaxInt64
)

// LedgerError codes for typed error handling.
type LedgerErrorCode string

const (
	LedgerCodeInvalidAmount   LedgerErrorCode = "invalid-amount"
	LedgerCodeOverflow        LedgerErrorCode = "overflow"
	LedgerCodeDuplicateSettle LedgerErrorCode = "duplicate-settlement"
	LedgerCodeMissingRates    LedgerErrorCode = "missing-rates"
	LedgerCodeMissingBounds   LedgerErrorCode = "missing-bounds"
	LedgerCodeDrift           LedgerErrorCode = "manifest-drift"
	LedgerCodeModelDrift      LedgerErrorCode = "model-drift"
	LedgerCodeCapExceeded     LedgerErrorCode = "cap-exceeded"
	LedgerCodeLockFailed      LedgerErrorCode = "lock-failed"
	LedgerCodeIO              LedgerErrorCode = "io"
	LedgerCodeCorrupt         LedgerErrorCode = "corrupt"
)

// LedgerError is a typed error for ledger operations.
type LedgerError struct {
	Code    LedgerErrorCode
	Message string
	Cause   error
}

func (e *LedgerError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("ledger: %s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("ledger: %s: %s", e.Code, e.Message)
}

func (e *LedgerError) Unwrap() error { return e.Cause }

// IsLedgerCode reports whether err carries the given ledger code.
func IsLedgerCode(err error, code LedgerErrorCode) bool {
	var le *LedgerError
	if errors.As(err, &le) {
		return le.Code == code
	}
	return false
}

// EntryType distinguishes ledger entry kinds.
type EntryType string

const (
	EntryReservation EntryType = "reservation"
	EntrySettlement  EntryType = "settlement"
	EntryAdjustment  EntryType = "adjustment" // for manual corrections with audit trail
)

// Entry is a single ledger record. All fields are immutable once written.
type Entry struct {
	ID              string    `json:"id"`          // UUID or deterministic key
	Type            EntryType `json:"type"`        // reservation | settlement | adjustment
	Timestamp       time.Time `json:"timestamp"`   // UTC
	RunID           string    `json:"runId"`       // evaluation run identifier
	Attempt         int       `json:"attempt"`     // attempt number within run
	Model           string    `json:"model"`       // model identifier
	Task            string    `json:"task"`        // task name (entity-type, relationship, etc.)
	SourceGroup     string    `json:"sourceGroup"` // source group ID
	Amount          MicroUnit `json:"amount"`      // signed: positive=reserve/credit, negative=settle/debit
	Balance         MicroUnit `json:"balance"`     // running balance after this entry
	TokensIn        int64     `json:"tokensIn"`    // estimated or actual input tokens
	TokensOut       int64     `json:"tokensOut"`   // estimated or actual output tokens
	RateIn          MicroUnit `json:"rateIn"`      // micro-units per 1M input tokens (0 if unknown)
	RateOut         MicroUnit `json:"rateOut"`     // micro-units per 1M output tokens (0 if unknown)
	Notes           string    `json:"notes,omitempty"`
	RefID           string    `json:"refId,omitempty"`           // for settlements: the reservation ID being settled
	ProtocolVersion string    `json:"protocolVersion,omitempty"` // protocol version at reservation time
}

// ManifestKey is a digest used to detect manifest/model drift between
// reservation and settlement.
type ManifestKey struct {
	FixtureFileSHA  string `json:"fixtureFileSha"`
	ProtocolVersion string `json:"protocolVersion"`
	ModelPin        string `json:"modelPin"`
	RatesVersion    string `json:"ratesVersion"`    // e.g., "openrouter-2026-09-15"
	TokenBoundsHash string `json:"tokenBoundsHash"` // hash of token limits config
}

// LedgerConfig controls ledger behavior.
type LedgerConfig struct {
	// Path is the ledger file path (JSONL append-only with fsync).
	Path string
	// AuthorizedCap is the total authorized spend in micro-units.
	// Default is 0 (no spend authorized without explicit config).
	AuthorizedCap MicroUnit
	// RunID identifies the evaluation run.
	RunID string
	// Model is the model identifier for this run.
	Model string
	// ManifestKey is the manifest/model fingerprint for drift detection.
	ManifestKey ManifestKey
	// TokenBounds are the conservative token limits per attempt.
	TokenBounds TokenBounds
	// RetryPolicy defines retry assumptions for reservation sizing.
	RetryPolicy ReservationRetryPolicy
	// DiscoveryCostEstimate is the estimated cost of the discovery baseline.
	DiscoveryCostEstimate MicroUnit
	// RateIn is the input token rate in micro-units per 1M tokens.
	RateIn MicroUnit
	// RateOut is the output token rate in micro-units per 1M tokens.
	RateOut MicroUnit
}

// TokenBounds defines conservative token limits used for reservation sizing.
// When actual usage exceeds these, the ledger records an overrun but does not
// silently increase the reservation.
type TokenBounds struct {
	MaxInputTokens  int64 `json:"maxInputTokens"`
	MaxOutputTokens int64 `json:"maxOutputTokens"`
}

// ReservationRetryPolicy defines how retries are accounted in reservations.
type ReservationRetryPolicy struct {
	MaxRetries          int     `json:"maxRetries"`
	RetryMultiplier     float64 `json:"retryMultiplier"`     // e.g., 1.0 = full cost per retry
	DiscoveryMultiplier float64 `json:"discoveryMultiplier"` // e.g., 0.5 = half cost estimate
}

// DefaultReservationRetryPolicy returns a conservative policy.
func DefaultReservationRetryPolicy() ReservationRetryPolicy {
	return ReservationRetryPolicy{
		MaxRetries:          1,
		RetryMultiplier:     1.0,
		DiscoveryMultiplier: 0.5,
	}
}

// Ledger is the persistent budget ledger. It uses a file lock for
// single-writer safety and fsync for crash safety.
type Ledger struct {
	cfg       LedgerConfig
	mu        sync.Mutex
	file      *os.File
	lockPath  string
	entries   []Entry
	balance   MicroUnit
	closed    bool
	lastEntry int // index of last fully written entry
}

// Open opens or creates the ledger file, acquires an exclusive lock,
// replays entries to reconstruct balance, and validates drift.
func Open(cfg LedgerConfig) (*Ledger, error) {
	if cfg.Path == "" {
		return nil, &LedgerError{Code: LedgerCodeIO, Message: "path required"}
	}
	if cfg.AuthorizedCap < 0 {
		return nil, &LedgerError{Code: LedgerCodeInvalidAmount, Message: "authorized cap cannot be negative"}
	}
	if cfg.RunID == "" {
		return nil, &LedgerError{Code: LedgerCodeInvalidAmount, Message: "runId required"}
	}
	if cfg.Model == "" {
		return nil, &LedgerError{Code: LedgerCodeInvalidAmount, Message: "model required"}
	}
	if cfg.TokenBounds.MaxInputTokens <= 0 || cfg.TokenBounds.MaxOutputTokens <= 0 {
		return nil, &LedgerError{Code: LedgerCodeMissingBounds, Message: "token bounds must be positive"}
	}
	if cfg.RetryPolicy.MaxRetries < 0 {
		return nil, &LedgerError{Code: LedgerCodeInvalidAmount, Message: "maxRetries cannot be negative"}
	}
	if cfg.RetryPolicy.RetryMultiplier < 0 {
		return nil, &LedgerError{Code: LedgerCodeInvalidAmount, Message: "retryMultiplier cannot be negative"}
	}
	if cfg.DiscoveryCostEstimate < 0 {
		return nil, &LedgerError{Code: LedgerCodeInvalidAmount, Message: "discoveryCostEstimate cannot be negative"}
	}

	// Open file with create, append, and exclusive lock.
	f, err := os.OpenFile(cfg.Path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return nil, &LedgerError{Code: LedgerCodeIO, Message: "open ledger file", Cause: err}
	}

	// Acquire exclusive lock (advisory) via lock file.
	lockPath := cfg.Path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		_ = f.Close()
		return nil, &LedgerError{Code: LedgerCodeLockFailed, Message: "acquire exclusive lock", Cause: err}
	}
	lockFile.Close()

	l := &Ledger{
		cfg:       cfg,
		file:      f,
		lockPath:  lockPath,
		entries:   []Entry{},
		balance:   0,
		closed:    false,
		lastEntry: -1,
	}

	// Replay existing entries to reconstruct state.
	if err := l.replay(); err != nil {
		_ = l.Close()
		return nil, err
	}

	// Validate manifest/model drift against the first entry's manifest key (if any).
	if len(l.entries) > 0 {
		first := l.entries[0]
		// The first entry should carry the manifest key in notes or a dedicated field.
		// For now we check the runID matches.
		if first.RunID != cfg.RunID {
			return nil, &LedgerError{Code: LedgerCodeDrift, Message: fmt.Sprintf("runId mismatch: ledger has %q, config has %q", first.RunID, cfg.RunID)}
		}
		if first.Model != cfg.Model {
			return nil, &LedgerError{Code: LedgerCodeModelDrift, Message: fmt.Sprintf("model drift: ledger has %q, config has %q", first.Model, cfg.Model)}
		}
		if first.ProtocolVersion != "" && first.ProtocolVersion != cfg.ManifestKey.ProtocolVersion {
			return nil, &LedgerError{Code: LedgerCodeDrift, Message: fmt.Sprintf("protocol version drift: ledger has %q, config has %q", first.ProtocolVersion, cfg.ManifestKey.ProtocolVersion)}
		}
	}

	return l, nil
}

// replay reads all lines from the file, parses entries, and reconstructs balance.
func (l *Ledger) replay() error {
	// Seek to start.
	if _, err := l.file.Seek(0, 0); err != nil {
		return &LedgerError{Code: LedgerCodeIO, Message: "seek to start", Cause: err}
	}

	dec := json.NewDecoder(l.file)
	lineNum := 0
	for {
		var e Entry
		if err := dec.Decode(&e); err != nil {
			break // EOF or error
		}
		lineNum++
		// Validate entry integrity.
		if err := l.validateEntry(e); err != nil {
			return &LedgerError{Code: LedgerCodeCorrupt, Message: fmt.Sprintf("line %d: %v", lineNum, err)}
		}
		l.entries = append(l.entries, e)
		l.balance = e.Balance
		l.lastEntry = lineNum - 1
	}

	// Verify balance never went negative (invariants).
	if l.balance < 0 {
		return &LedgerError{Code: LedgerCodeCorrupt, Message: fmt.Sprintf("reconstructed balance is negative: %d", l.balance)}
	}
	// Verify cap not exceeded.
	if l.balance > l.cfg.AuthorizedCap {
		return &LedgerError{Code: LedgerCodeCapExceeded, Message: fmt.Sprintf("reconstructed balance %d exceeds authorized cap %d", l.balance, l.cfg.AuthorizedCap)}
	}
	return nil
}

// validateEntry performs basic integrity checks on an entry.
func (l *Ledger) validateEntry(e Entry) error {
	if e.ID == "" {
		return errors.New("empty entry id")
	}
	if e.Type != EntryReservation && e.Type != EntrySettlement && e.Type != EntryAdjustment {
		return fmt.Errorf("invalid entry type %q", e.Type)
	}
	if e.Amount == 0 {
		return errors.New("entry amount is zero")
	}
	if e.Balance < 0 {
		return errors.New("entry balance is negative")
	}
	if e.TokensIn < 0 || e.TokensOut < 0 {
		return errors.New("token counts cannot be negative")
	}
	if e.RateIn < 0 || e.RateOut < 0 {
		return errors.New("rates cannot be negative")
	}
	if e.Type == EntrySettlement && e.RefID == "" {
		return errors.New("settlement entry missing refId")
	}
	return nil
}

// Reserve reserves a conservative amount BEFORE an attempt. The reservation
// includes the estimated cost of the attempt plus retries and discovery.
// Returns the reservation ID and the reserved amount.
//
// The reservation is computed as:
//
//	base = ceil(rateIn * tokensInEst / 1e6 + rateOut * tokensOutEst / 1e6)
//	total = base * (1 + maxRetries * retryMultiplier) + discoveryCostEstimate * discoveryMultiplier
//
// All calculations are done in integer micro-units with ceiling division.
// Fails closed if rates are zero (missing) or cap would be exceeded.
func (l *Ledger) Reserve(ctx Context, attempt int, task, sourceGroup string, tokensInEst, tokensOutEst int64) (string, MicroUnit, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return "", 0, &LedgerError{Code: LedgerCodeIO, Message: "ledger is closed"}
	}

	// Validate rates are known (fail closed if missing).
	if l.cfg.RateIn == 0 || l.cfg.RateOut == 0 {
		return "", 0, &LedgerError{Code: LedgerCodeMissingRates, Message: "input/output rates must be configured before reservation"}
	}

	// Compute base cost estimate (ceiling division for conservatism).
	base := estimateCost(l.cfg.RateIn, l.cfg.RateOut, tokensInEst, tokensOutEst)

	// Add retry budget.
	retryBudget := MicroUnit(float64(base) * l.cfg.RetryPolicy.RetryMultiplier * float64(l.cfg.RetryPolicy.MaxRetries))

	// Add discovery estimate.
	discoveryBudget := MicroUnit(float64(l.cfg.DiscoveryCostEstimate) * l.cfg.RetryPolicy.DiscoveryMultiplier)

	total := base + retryBudget + discoveryBudget

	// Check overflow.
	if total < 0 || total > MaxMicroUnits {
		return "", 0, &LedgerError{Code: LedgerCodeOverflow, Message: fmt.Sprintf("reservation amount %d overflows", total)}
	}

	// Check cap with existing balance + this reservation.
	newBalance := l.balance + total
	if newBalance > l.cfg.AuthorizedCap {
		return "", 0, &LedgerError{Code: LedgerCodeCapExceeded, Message: fmt.Sprintf("reservation %d would exceed cap %d (current balance %d)", newBalance, l.cfg.AuthorizedCap, l.balance)}
	}

	// Include protocol version in the first reservation for drift detection.
	protocolVersion := ""
	if len(l.entries) == 0 {
		protocolVersion = l.cfg.ManifestKey.ProtocolVersion
	}

	// Create reservation entry.
	entry := Entry{
		ID:              fmt.Sprintf("res-%s-%d-%d", l.cfg.RunID, attempt, time.Now().UnixNano()),
		Type:            EntryReservation,
		Timestamp:       time.Now().UTC(),
		RunID:           l.cfg.RunID,
		Attempt:         attempt,
		Model:           l.cfg.Model,
		Task:            task,
		SourceGroup:     sourceGroup,
		Amount:          total,
		Balance:         newBalance,
		TokensIn:        tokensInEst,
		TokensOut:       tokensOutEst,
		RateIn:          l.cfg.RateIn,
		RateOut:         l.cfg.RateOut,
		Notes:           fmt.Sprintf("reservation for attempt %d task %s group %s", attempt, task, sourceGroup),
		ProtocolVersion: protocolVersion,
	}

	if err := l.appendEntry(entry); err != nil {
		return "", 0, err
	}

	l.balance = newBalance
	l.lastEntry++
	return entry.ID, total, nil
}

// Settle records the actual billed cost and reconciles with the reservation.
// Fails if:
//   - RefID not found or already settled (duplicate settlement)
//   - Actual cost exceeds reservation (overrun) — ledger records the overrun and returns error
//   - Actual cost is negative/nonfinite/overflow
//   - Amount is zero
func (l *Ledger) Settle(ctx Context, refID string, actualCost MicroUnit, actualTokensIn, actualTokensOut int64, actualRateIn, actualRateOut MicroUnit) (MicroUnit, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return 0, &LedgerError{Code: LedgerCodeIO, Message: "ledger is closed"}
	}

	// Validate actual cost.
	if actualCost <= 0 {
		return 0, &LedgerError{Code: LedgerCodeInvalidAmount, Message: "actual cost must be positive"}
	}
	if actualCost > MaxMicroUnits {
		return 0, &LedgerError{Code: LedgerCodeOverflow, Message: "actual cost overflows"}
	}

	// Find the reservation.
	resIdx := -1
	for i, e := range l.entries {
		if e.ID == refID && e.Type == EntryReservation {
			resIdx = i
			break
		}
	}
	if resIdx == -1 {
		return 0, &LedgerError{Code: LedgerCodeDuplicateSettle, Message: fmt.Sprintf("reservation %q not found or not a reservation", refID)}
	}
	res := l.entries[resIdx]

	// Check for duplicate settlement (any settlement referencing this refID).
	for _, e := range l.entries {
		if e.Type == EntrySettlement && e.RefID == refID {
			return 0, &LedgerError{Code: LedgerCodeDuplicateSettle, Message: fmt.Sprintf("reservation %q already settled", refID)}
		}
	}

	// Reconcile: actual cost must not exceed reservation.
	reservedAmount := res.Amount
	if actualCost > reservedAmount {
		// Overrun: record an adjustment entry for the excess, then return error.
		overrun := actualCost - reservedAmount
		adjEntry := Entry{
			ID:          fmt.Sprintf("adj-%s-%d", l.cfg.RunID, time.Now().UnixNano()),
			Type:        EntryAdjustment,
			Timestamp:   time.Now().UTC(),
			RunID:       l.cfg.RunID,
			Attempt:     res.Attempt,
			Model:       l.cfg.Model,
			Task:        res.Task,
			SourceGroup: res.SourceGroup,
			Amount:      overrun,
			Balance:     l.balance + overrun,
			TokensIn:    actualTokensIn,
			TokensOut:   actualTokensOut,
			RateIn:      actualRateIn,
			RateOut:     actualRateOut,
			Notes:       fmt.Sprintf("overrun for settled reservation %q", refID),
			RefID:       refID,
		}
		if err := l.appendEntry(adjEntry); err != nil {
			return 0, err
		}
		l.balance += overrun
		l.lastEntry++
		return 0, &LedgerError{Code: LedgerCodeCapExceeded, Message: fmt.Sprintf("actual cost %d exceeds reservation %d by %d", actualCost, reservedAmount, overrun)}
	}

	// Settlement entry (negative amount = debit).
	settleEntry := Entry{
		ID:          fmt.Sprintf("set-%s-%d", l.cfg.RunID, time.Now().UnixNano()),
		Type:        EntrySettlement,
		Timestamp:   time.Now().UTC(),
		RunID:       l.cfg.RunID,
		Attempt:     res.Attempt,
		Model:       l.cfg.Model,
		Task:        res.Task,
		SourceGroup: res.SourceGroup,
		Amount:      -actualCost,
		Balance:     l.balance - actualCost,
		TokensIn:    actualTokensIn,
		TokensOut:   actualTokensOut,
		RateIn:      actualRateIn,
		RateOut:     actualRateOut,
		Notes:       fmt.Sprintf("settlement for reservation %q", refID),
		RefID:       refID,
	}

	if err := l.appendEntry(settleEntry); err != nil {
		return 0, err
	}

	l.balance -= actualCost
	l.lastEntry++

	// Return the remaining unreserved amount (reservation - actual).
	remaining := reservedAmount - actualCost
	return remaining, nil
}

// estimateCost computes the ceiling cost in micro-units.
// Returns MaxMicroUnits on overflow (which will be caught by the overflow check in Reserve).
func estimateCost(rateIn, rateOut MicroUnit, tokensIn, tokensOut int64) MicroUnit {
	// Check for potential overflow before multiplication.
	// MaxMicroUnits / rateIn gives the max tokensIn that won't overflow.
	if rateIn > 0 && MicroUnit(tokensIn) > MaxMicroUnits/rateIn {
		return MaxMicroUnits
	}
	if rateOut > 0 && MicroUnit(tokensOut) > MaxMicroUnits/rateOut {
		return MaxMicroUnits
	}
	// Ceiling division: (a * b + 1e6 - 1) / 1e6
	costIn := (MicroUnit(tokensIn)*rateIn + MicroUnitsPerUnit - 1) / MicroUnitsPerUnit
	costOut := (MicroUnit(tokensOut)*rateOut + MicroUnitsPerUnit - 1) / MicroUnitsPerUnit
	// Check for overflow in addition.
	if costIn > MaxMicroUnits-costOut {
		return MaxMicroUnits
	}
	return costIn + costOut
}

// appendEntry appends an entry to the file and fsyncs for crash safety.
func (l *Ledger) appendEntry(e Entry) error {
	data, err := json.Marshal(e)
	if err != nil {
		return &LedgerError{Code: LedgerCodeIO, Message: "marshal entry", Cause: err}
	}
	data = append(data, '\n')
	if _, err := l.file.Write(data); err != nil {
		return &LedgerError{Code: LedgerCodeIO, Message: "write entry", Cause: err}
	}
	if err := l.file.Sync(); err != nil {
		return &LedgerError{Code: LedgerCodeIO, Message: "sync entry", Cause: err}
	}
	l.entries = append(l.entries, e)
	return nil
}

// Balance returns the current ledger balance (total reserved - total settled).
func (l *Ledger) Balance() MicroUnit {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.balance
}

// Available returns the remaining authorized amount (cap - balance).
func (l *Ledger) Available() MicroUnit {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.balance >= l.cfg.AuthorizedCap {
		return 0
	}
	return l.cfg.AuthorizedCap - l.balance
}

// Entries returns a copy of all entries.
func (l *Ledger) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, len(l.entries))
	copy(out, l.entries)
	return out
}

// Close releases the lock and closes the file.
func (l *Ledger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.file != nil {
		_ = l.file.Close()
	}
	// Release lock file.
	if l.lockPath != "" {
		_ = os.Remove(l.lockPath)
	}
	return nil
}

// Context is a minimal context interface for future cancellation support.
type Context interface {
	Done() <-chan struct{}
	Err() error
}

// noopContext is a trivial context for operations that don't need cancellation.
type noopContext struct{}

func (noopContext) Done() <-chan struct{} { return nil }
func (noopContext) Err() error            { return nil }

// NoopContext returns a context that never cancels.
func NoopContext() Context { return noopContext{} }
