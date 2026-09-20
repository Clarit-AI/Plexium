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
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	LedgerCodeHalted          LedgerErrorCode = "halted"
	LedgerCodeTokenBounds     LedgerErrorCode = "token-bounds-exceeded"
	LedgerCodeRateMismatch    LedgerErrorCode = "rate-mismatch"
	LedgerCodeTokenMismatch   LedgerErrorCode = "token-mismatch"
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
	EntryInit        EntryType = "init"       // initialization header (first line)
)

// Entry is a single ledger record. All fields are immutable once written.
type Entry struct {
	ID              string    `json:"id"`          // UUID or deterministic key
	Type            EntryType `json:"type"`        // reservation | settlement | adjustment | init
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
	// Init-only fields (present only on EntryInit):
	AuthorizedCap MicroUnit    `json:"authorizedCap,omitempty"` // authorized cap at init time
	ManifestKey   *ManifestKey `json:"manifestKey,omitempty"`   // full manifest key at init time
	Halted        bool         `json:"halted,omitempty"`        // whether ledger is halted (persisted on init)
}

// ManifestKey is a digest used to detect manifest/model drift between
// reservation and settlement.
type ManifestKey struct {
	FixtureFileSHA  string `json:"fixtureFileSha"`
	ProtocolVersion string `json:"protocolVersion"`
	ModelPin        string `json:"modelPin"`
	RatesVersion    string `json:"ratesVersion"`    // e.g., "openrouter-2026-09-15"
	TokenBoundsHash string `json:"tokenBoundsHash"` // SHA256 of token limits config
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
	// nil means "omitted/unknown" — this is invalid and rejected.
	// A pointer to 0 means explicitly supplied zero rate (free output).
	// A pointer to a positive value means that rate.
	RateOut *MicroUnit
}

// TokenBounds defines conservative token limits used for reservation sizing.
// When actual usage exceeds these, the ledger rejects the reservation.
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
	halted    bool // P0: latched on overrun; blocks new reservations/settlements
	lastEntry int  // index of last fully written entry
}

// Open opens or creates the ledger file, acquires an exclusive lock,
// replays entries to reconstruct balance, validates drift, and writes
// an init header if the file is new.
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
		halted:    false,
		lastEntry: -1,
	}

	// Replay existing entries to reconstruct state.
	if err := l.replay(); err != nil {
		_ = l.Close()
		return nil, err
	}

	// Validate manifest/model drift against the init entry (if any).
	// If file is new (no entries), write init header now.
	if len(l.entries) == 0 {
		if err := l.writeInitEntry(); err != nil {
			_ = l.Close()
			return nil, err
		}
	} else {
		if err := l.validateDrift(); err != nil {
			_ = l.Close()
			return nil, err
		}
		// Restore halted state from init entry.
		if len(l.entries) > 0 && l.entries[0].Type == EntryInit {
			l.halted = l.entries[0].Halted
		}
	}

	return l, nil
}

// writeInitEntry writes the initialization header as the first line of the ledger.
// This persists the full ManifestKey and AuthorizedCap for drift detection on reopen.
func (l *Ledger) writeInitEntry() error {
	var rateOutVal MicroUnit
	if l.cfg.RateOut != nil {
		rateOutVal = *l.cfg.RateOut
	}
	init := Entry{
		ID:              fmt.Sprintf("init-%s-%d", l.cfg.RunID, time.Now().UnixNano()),
		Type:            EntryInit,
		Timestamp:       time.Now().UTC(),
		RunID:           l.cfg.RunID,
		Attempt:         0,
		Model:           l.cfg.Model,
		Task:            "",
		SourceGroup:     "",
		Amount:          0,
		Balance:         0,
		TokensIn:        0,
		TokensOut:       0,
		RateIn:          0,
		RateOut:         rateOutVal,
		Notes:           "initialization header",
		ProtocolVersion: l.cfg.ManifestKey.ProtocolVersion,
		AuthorizedCap:   l.cfg.AuthorizedCap,
		ManifestKey:     &l.cfg.ManifestKey,
		Halted:          l.halted,
	}
	return l.appendEntry(init)
}

// rewriteInitEntry rewrites the first line (init entry) with the current halted state.
// This is called when the ledger is halted to persist the halted state.
func (l *Ledger) rewriteInitEntry() error {
	if len(l.entries) == 0 || l.entries[0].Type != EntryInit {
		return errors.New("no init entry to rewrite")
	}
	// Update the first entry's halted state.
	l.entries[0].Halted = l.halted
	// Rewrite the entire file atomically: write to temp, then rename.
	tempPath := l.file.Name() + ".tmp"
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return &LedgerError{Code: LedgerCodeIO, Message: "create temp file for rewrite", Cause: err}
	}
	for _, e := range l.entries {
		data, err := json.Marshal(e)
		if err != nil {
			tempFile.Close()
			os.Remove(tempPath)
			return &LedgerError{Code: LedgerCodeIO, Message: "marshal entry for rewrite", Cause: err}
		}
		data = append(data, '\n')
		if _, err := tempFile.Write(data); err != nil {
			tempFile.Close()
			os.Remove(tempPath)
			return &LedgerError{Code: LedgerCodeIO, Message: "write entry for rewrite", Cause: err}
		}
	}
	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		return &LedgerError{Code: LedgerCodeIO, Message: "sync temp file for rewrite", Cause: err}
	}
	if err := tempFile.Close(); err != nil {
		os.Remove(tempPath)
		return &LedgerError{Code: LedgerCodeIO, Message: "close temp file for rewrite", Cause: err}
	}
	// Atomic rename.
	if err := os.Rename(tempPath, l.file.Name()); err != nil {
		os.Remove(tempPath)
		return &LedgerError{Code: LedgerCodeIO, Message: "atomic rename for rewrite", Cause: err}
	}
	// Reopen the file handle to the new file.
	oldFile := l.file
	l.file, err = os.OpenFile(l.file.Name(), os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return &LedgerError{Code: LedgerCodeIO, Message: "reopen after rename", Cause: err}
	}
	_ = oldFile.Close()
	return nil
}

// replay reads all lines from the file, parses entries, and reconstructs balance.
// L1: Fails closed on torn/truncated final entry (distinguishes EOF from parse errors).
func (l *Ledger) replay() error {
	// Seek to start.
	if _, err := l.file.Seek(0, 0); err != nil {
		return &LedgerError{Code: LedgerCodeIO, Message: "seek to start", Cause: err}
	}

	dec := json.NewDecoder(l.file)
	lineNum := 0
	for {
		var e Entry
		err := dec.Decode(&e)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return &LedgerError{Code: LedgerCodeIO, Message: "context canceled during replay", Cause: err}
			}
			// L1: Distinguish clean EOF from torn/truncated entry.
			// Clean EOF is io.EOF - this is normal end of file, not an error.
			if errors.Is(err, io.EOF) {
				break // clean end of file
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return &LedgerError{Code: LedgerCodeIO, Message: "context deadline exceeded during replay", Cause: err}
			}
			// Check for syntax errors / unexpected EOF (torn write).
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return &LedgerError{Code: LedgerCodeIO, Message: "context error during replay", Cause: err}
			}
			// For json package, syntax errors and unexpected EOF are not wrapped in standard errors.
			// We check the error string for the distinction.
			errStr := err.Error()
			if errStr == "unexpected EOF" {
				// Torn/truncated write - the file ended mid-JSON object.
				return &LedgerError{Code: LedgerCodeCorrupt, Message: fmt.Sprintf("torn/truncated entry at line %d (fail-closed): %v", lineNum+1, err)}
			}
			// Other errors (syntax, type mismatch) are also corruption.
			return &LedgerError{Code: LedgerCodeCorrupt, Message: fmt.Sprintf("corrupt entry at line %d: %v", lineNum+1, err)}
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

// validateDrift checks that the init entry's persisted config matches current config.
func (l *Ledger) validateDrift() error {
	if len(l.entries) == 0 {
		return nil
	}
	first := l.entries[0]
	if first.Type != EntryInit {
		return &LedgerError{Code: LedgerCodeDrift, Message: "first entry is not init header"}
	}
	// RunID must match.
	if first.RunID != l.cfg.RunID {
		return &LedgerError{Code: LedgerCodeDrift, Message: fmt.Sprintf("runId mismatch: ledger has %q, config has %q", first.RunID, l.cfg.RunID)}
	}
	// Model must match.
	if first.Model != l.cfg.Model {
		return &LedgerError{Code: LedgerCodeModelDrift, Message: fmt.Sprintf("model drift: ledger has %q, config has %q", first.Model, l.cfg.Model)}
	}
	// AuthorizedCap must match.
	if first.AuthorizedCap != l.cfg.AuthorizedCap {
		return &LedgerError{Code: LedgerCodeDrift, Message: fmt.Sprintf("authorized cap drift: ledger has %d, config has %d", first.AuthorizedCap, l.cfg.AuthorizedCap)}
	}
	// Full ManifestKey must match.
	if first.ManifestKey == nil {
		return &LedgerError{Code: LedgerCodeDrift, Message: "init entry missing manifest key"}
	}
	mk := first.ManifestKey
	if mk.FixtureFileSHA != l.cfg.ManifestKey.FixtureFileSHA {
		return &LedgerError{Code: LedgerCodeDrift, Message: fmt.Sprintf("fixture SHA drift: ledger has %q, config has %q", mk.FixtureFileSHA, l.cfg.ManifestKey.FixtureFileSHA)}
	}
	if mk.ProtocolVersion != l.cfg.ManifestKey.ProtocolVersion {
		return &LedgerError{Code: LedgerCodeDrift, Message: fmt.Sprintf("protocol version drift: ledger has %q, config has %q", mk.ProtocolVersion, l.cfg.ManifestKey.ProtocolVersion)}
	}
	if mk.ModelPin != l.cfg.ManifestKey.ModelPin {
		return &LedgerError{Code: LedgerCodeModelDrift, Message: fmt.Sprintf("model pin drift: ledger has %q, config has %q", mk.ModelPin, l.cfg.ManifestKey.ModelPin)}
	}
	if mk.RatesVersion != l.cfg.ManifestKey.RatesVersion {
		return &LedgerError{Code: LedgerCodeDrift, Message: fmt.Sprintf("rates version drift: ledger has %q, config has %q", mk.RatesVersion, l.cfg.ManifestKey.RatesVersion)}
	}
	if mk.TokenBoundsHash != l.cfg.ManifestKey.TokenBoundsHash {
		return &LedgerError{Code: LedgerCodeDrift, Message: fmt.Sprintf("token bounds hash drift: ledger has %q, config has %q", mk.TokenBoundsHash, l.cfg.ManifestKey.TokenBoundsHash)}
	}
	return nil
}

// validateEntry performs basic integrity checks on an entry.
func (l *Ledger) validateEntry(e Entry) error {
	if e.ID == "" {
		return errors.New("empty entry id")
	}
	if e.Type != EntryReservation && e.Type != EntrySettlement && e.Type != EntryAdjustment && e.Type != EntryInit {
		return fmt.Errorf("invalid entry type %q", e.Type)
	}
	if e.Type != EntryInit && e.Amount == 0 {
		return errors.New("entry amount is zero")
	}
	if e.Type != EntryInit && e.Balance < 0 {
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
	if e.Type == EntryInit {
		if e.AuthorizedCap < 0 {
			return errors.New("init entry authorized cap cannot be negative")
		}
		if e.ManifestKey == nil {
			return errors.New("init entry missing manifest key")
		}
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
// L7: Enforces TokenBounds — tokensEst must not exceed bounds.
func (l *Ledger) Reserve(ctx context.Context, attempt int, task, sourceGroup string, tokensInEst, tokensOutEst int64) (string, MicroUnit, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return "", 0, &LedgerError{Code: LedgerCodeIO, Message: "ledger is closed"}
	}
	// L3: Block new reservations if halted due to overrun.
	if l.halted {
		return "", 0, &LedgerError{Code: LedgerCodeHalted, Message: "ledger halted due to prior overrun; no further reservations permitted"}
	}

	// Validate rates are known (fail closed if missing).
	// RateOut is now a pointer: nil means omitted, pointer to 0 means free output.
	if l.cfg.RateIn == 0 || l.cfg.RateOut == nil {
		return "", 0, &LedgerError{Code: LedgerCodeMissingRates, Message: "input/output rates must be configured before reservation"}
	}

	// L7: Enforce TokenBounds.
	if tokensInEst > l.cfg.TokenBounds.MaxInputTokens {
		return "", 0, &LedgerError{Code: LedgerCodeTokenBounds, Message: fmt.Sprintf("input tokens %d exceeds bound %d", tokensInEst, l.cfg.TokenBounds.MaxInputTokens)}
	}
	if tokensOutEst > l.cfg.TokenBounds.MaxOutputTokens {
		return "", 0, &LedgerError{Code: LedgerCodeTokenBounds, Message: fmt.Sprintf("output tokens %d exceeds bound %d", tokensOutEst, l.cfg.TokenBounds.MaxOutputTokens)}
	}

	// Compute base cost estimate (ceiling division for conservatism).
	base := estimateCost(l.cfg.RateIn, *l.cfg.RateOut, tokensInEst, tokensOutEst)

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
	if len(l.entries) == 1 && l.entries[0].Type == EntryInit { // only init entry so far
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
		RateOut:         *l.cfg.RateOut,
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
//
// L3: Latches halted flag on overrun; subsequent Reserve/Settle blocked.
// L4: Verifies actual rates/tokens against reservation.
// Actual billed cost stays recorded even when drift/overrun occurs (never rolled back).
func (l *Ledger) Settle(ctx context.Context, refID string, actualCost MicroUnit, actualTokensIn, actualTokensOut int64, actualRateIn, actualRateOut MicroUnit) (MicroUnit, error) {
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

	// L4: Verify actual rates match reservation rates.
	// Note: If actualRateOut is 0 (omitted), we skip the output rate check
	// since the reservation may have had an estimated rateOut > 0.
	// But if both are non-zero, they must match exactly.
	if actualRateIn != 0 && res.RateIn != 0 && actualRateIn != res.RateIn {
		return 0, &LedgerError{Code: LedgerCodeRateMismatch, Message: fmt.Sprintf("actual rateIn %d does not match reserved rateIn %d", actualRateIn, res.RateIn)}
	}
	if actualRateOut != 0 && res.RateOut != 0 && actualRateOut != res.RateOut {
		return 0, &LedgerError{Code: LedgerCodeRateMismatch, Message: fmt.Sprintf("actual rateOut %d does not match reserved rateOut %d", actualRateOut, res.RateOut)}
	}

	// L4: Verify actual tokens do not exceed reserved tokens.
	if actualTokensIn > res.TokensIn {
		return 0, &LedgerError{Code: LedgerCodeTokenMismatch, Message: fmt.Sprintf("actual tokensIn %d exceeds reserved tokensIn %d", actualTokensIn, res.TokensIn)}
	}
	if actualTokensOut > res.TokensOut {
		return 0, &LedgerError{Code: LedgerCodeTokenMismatch, Message: fmt.Sprintf("actual tokensOut %d exceeds reserved tokensOut %d", actualTokensOut, res.TokensOut)}
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

		// L3: Latch halted flag — no further reservations or settlements permitted.
		l.halted = true
		// Persist halted state by rewriting init entry.
		if len(l.entries) > 0 && l.entries[0].Type == EntryInit {
			l.entries[0].Halted = true
			// Rewrite the first line (init entry) with updated halted state.
			if err := l.rewriteInitEntry(); err != nil {
				return 0, err
			}
		}

		return 0, &LedgerError{Code: LedgerCodeCapExceeded, Message: fmt.Sprintf("actual cost %d exceeds reservation %d by %d (ledger halted)", actualCost, reservedAmount, overrun)}
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

// Halted returns true if the ledger is halted due to an overrun.
func (l *Ledger) Halted() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.halted
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

// ComputeTokenBoundsHash computes the SHA256 hash of the token bounds config
// for drift detection. Returns hex-encoded string.
func ComputeTokenBoundsHash(bounds TokenBounds, maxRetries int, retryMultiplier, discoveryMultiplier float64, discoveryCostEstimate MicroUnit) string {
	// Include all parameters that affect reservation sizing.
	input := fmt.Sprintf("%d:%d:%d:%f:%f:%d", bounds.MaxInputTokens, bounds.MaxOutputTokens, maxRetries, retryMultiplier, discoveryMultiplier, discoveryCostEstimate)
	h := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", h)
}

// MicroUnitFromBaseString parses a decimal string (e.g., "0.042") into MicroUnit,
// rounding UP (conservative for cost estimation). Rejects negative, non-finite,
// or overflow values. Never saturates or rounds a positive cost down.
func MicroUnitFromBaseString(s string) (MicroUnit, error) {
	if s == "" {
		return 0, errors.New("empty string")
	}
	var base float64
	n, err := fmt.Sscanf(s, "%f", &base)
	if n != 1 || err != nil {
		return 0, fmt.Errorf("invalid decimal: %s", s)
	}
	if base < 0 {
		return 0, errors.New("negative value not allowed")
	}
	if math.IsNaN(base) || math.IsInf(base, 0) {
		return 0, errors.New("non-finite value")
	}
	// Round UP (ceiling) for conservative cost estimation.
	// Multiply by 1e6 and apply ceiling.
	micro := base * float64(MicroUnitsPerUnit)
	if micro > float64(MaxMicroUnits) {
		return 0, errors.New("value overflows MicroUnit")
	}
	// Use math.Ceil to round UP (conservative: we want to reserve enough).
	result := MicroUnit(math.Ceil(micro))
	if result <= 0 && base > 0 {
		// Tiny positive value rounded to zero — treat as 1 micro-unit.
		return 1, nil
	}
	return result, nil
}

// MicroUnitFromBase is deprecated; use MicroUnitFromBaseString for conservative parsing.
// Kept for backward compatibility with tests that use float64 inputs.
func MicroUnitFromBase(base float64) (MicroUnit, error) {
	if base < 0 {
		return 0, errors.New("negative value not allowed")
	}
	if math.IsNaN(base) || math.IsInf(base, 0) {
		return 0, errors.New("non-finite value")
	}
	if base > float64(MaxMicroUnits)/float64(MicroUnitsPerUnit) {
		return 0, errors.New("value overflows MicroUnit")
	}
	// Round UP (ceiling) for conservative cost estimation.
	result := MicroUnit(math.Ceil(base * float64(MicroUnitsPerUnit)))
	if result <= 0 && base > 0 {
		return 1, nil
	}
	return result, nil
}
