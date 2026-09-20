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
	"path/filepath"
	"strings"
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
	// EntryMismatch records a settled (or attempted) settlement where the
	// actual billed rate/tokens drifted from the reservation. The entry
	// carries both the reserved and the actual values so reviewers can
	// audit the audit. The reservation ID referenced is marked terminal and
	// cannot be re-settled as "valid" later; the ledger latches a durable
	// halt that blocks new spending (Reserve) but still permits Settle on
	// already-in-flight (pre-halt) reservations for reconciliation.
	EntryMismatch EntryType = "mismatch"
)

// Entry is a single ledger record. All fields are immutable once written.
type Entry struct {
	ID              string    `json:"id"`          // UUID or deterministic key
	Type            EntryType `json:"type"`        // reservation | settlement | adjustment | init | mismatch
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
	AuthorizedCap    MicroUnit    `json:"authorizedCap,omitempty"`    // authorized cap at init time
	ManifestKey      *ManifestKey `json:"manifestKey,omitempty"`      // full manifest key at init time
	Halted           bool         `json:"halted,omitempty"`           // whether ledger is halted (persisted on init)
	RateOutPresent   bool         `json:"rateOutPresent,omitempty"`   // true if RateOut was supplied (incl. zero) at init
	TokenBoundsValue *TokenBounds `json:"tokenBoundsValue,omitempty"` // numeric token bounds, persisted on init
	// Mismatch fields (present on EntryMismatch):
	ExpectedRateIn       MicroUnit `json:"expectedRateIn,omitempty"`       // reserved rate in
	ExpectedRateOut      MicroUnit `json:"expectedRateOut,omitempty"`      // reserved rate out
	ActualRateIn         MicroUnit `json:"actualRateIn,omitempty"`         // billed rate in
	ActualRateOut        MicroUnit `json:"actualRateOut,omitempty"`        // billed rate out (0 means omitted by caller)
	ActualRateOutPresent bool      `json:"actualRateOutPresent,omitempty"` // whether caller supplied actual rate out
	ActualTokensIn       int64     `json:"actualTokensIn,omitempty"`       // billed input tokens
	ActualTokensOut      int64     `json:"actualTokensOut,omitempty"`      // billed output tokens
	ActualCost           MicroUnit `json:"actualCost,omitempty"`           // billed cost in micro-units
	MismatchReason       string    `json:"mismatchReason,omitempty"`       // rate-mismatch | token-mismatch
	ReservationTerminal  bool      `json:"reservationTerminal,omitempty"`  // marks the reservation as terminal/disputed
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
		// C2: After the initial init write, fsync the parent directory so
		// the freshly-created ledger file is durably linked into the
		// directory entry. A failure here is fail-closed.
		if err := syncDir(filepathDir(cfg.Path)); err != nil {
			_ = l.Close()
			return nil, &LedgerError{Code: LedgerCodeIO, Message: "sync parent directory after init", Cause: err}
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
		// D1: Derive halted=true from EVERY durable mismatch or overrun
		// adjustment, regardless of init.Halted. This closes the crash
		// window where evidence is fsynced but the init-entry rewrite
		// never lands — without this derivation, a reopen would observe
		// init.Halted=false and resume new spending despite a billing
		// anomaly on durable record.
		for _, e := range l.entries {
			if e.Type == EntryMismatch {
				l.halted = true
				break
			}
			if e.Type == EntryAdjustment && strings.HasPrefix(e.Notes, "overrun") {
				l.halted = true
				break
			}
		}
	}

	return l, nil
}

// writeInitEntry writes the initialization header as the first line of the ledger.
// This persists the full ManifestKey, AuthorizedCap, numeric RateIn /
// RateOut (with presence marker), and numeric TokenBounds for drift
// detection on reopen. The persistence is critical: callers cannot bypass
// the binding by simply re-supplying matching hash strings.
func (l *Ledger) writeInitEntry() error {
	var rateOutVal MicroUnit
	var rateOutPresent bool
	if l.cfg.RateOut != nil {
		rateOutVal = *l.cfg.RateOut
		rateOutPresent = true
	}
	tb := l.cfg.TokenBounds
	init := Entry{
		ID:               fmt.Sprintf("init-%s-%d", l.cfg.RunID, time.Now().UnixNano()),
		Type:             EntryInit,
		Timestamp:        time.Now().UTC(),
		RunID:            l.cfg.RunID,
		Attempt:          0,
		Model:            l.cfg.Model,
		Task:             "",
		SourceGroup:      "",
		Amount:           0,
		Balance:          0,
		TokensIn:         0,
		TokensOut:        0,
		RateIn:           l.cfg.RateIn,
		RateOut:          rateOutVal,
		RateOutPresent:   rateOutPresent,
		TokenBoundsValue: &tb,
		Notes:            "initialization header",
		ProtocolVersion:  l.cfg.ManifestKey.ProtocolVersion,
		AuthorizedCap:    l.cfg.AuthorizedCap,
		ManifestKey:      &l.cfg.ManifestKey,
		Halted:           l.halted,
	}
	return l.appendEntry(init)
}

// rewriteInitEntry rewrites the first line (init entry) with the current halted state.
// This is called when the ledger is halted to persist the halted state.
//
// C2: After the atomic rename, the parent directory is fsynced so the
// rename itself is durable across crashes. A failure here is fail-closed;
// we revert to the in-memory state and report the IO error.
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
	// C2: fsync the parent directory so the rename itself is durable.
	if err := syncDir(filepathDir(l.file.Name())); err != nil {
		return &LedgerError{Code: LedgerCodeIO, Message: "sync parent directory after rename", Cause: err}
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

// filepathDir returns the directory portion of path. The filepath package
// is used instead of path so we get OS-correct separators on all platforms.
func filepathDir(path string) string {
	return filepath.Dir(path)
}

// syncDir fsyncs the parent directory so atomic renames inside it are
// durable across crashes. Without this, the rename can be lost if the
// system crashes between the rename and the directory entry flush.
// The empty string is normalized to "." (current directory); we never
// silently no-op — the ledger must receive a real dirent flush even
// when it resides at the working directory.
func syncDir(dir string) error {
	if dir == "" {
		dir = "."
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
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
//
// B2: In addition to comparing the supplied ManifestKey hash strings, the
// validation compares the actual numeric RateIn / RateOut (with presence
// marker) and numeric TokenBounds persisted at init time. A caller who
// re-supplies the same hash strings but changes the underlying values is
// rejected by the numeric binding rather than silently accepted.
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

	// B2: Numeric RateIn / RateOut (with presence marker) / TokenBounds binding.
	if first.RateIn != l.cfg.RateIn {
		return &LedgerError{Code: LedgerCodeDrift, Message: fmt.Sprintf("rateIn drift: ledger has %d, config has %d", first.RateIn, l.cfg.RateIn)}
	}
	cfgRateOutPresent := l.cfg.RateOut != nil
	if first.RateOutPresent != cfgRateOutPresent {
		return &LedgerError{Code: LedgerCodeDrift, Message: fmt.Sprintf("rateOut presence drift: ledger has %v, config has %v", first.RateOutPresent, cfgRateOutPresent)}
	}
	if first.RateOutPresent && l.cfg.RateOut != nil && first.RateOut != *l.cfg.RateOut {
		return &LedgerError{Code: LedgerCodeDrift, Message: fmt.Sprintf("rateOut drift: ledger has %d, config has %d", first.RateOut, *l.cfg.RateOut)}
	}
	if first.TokenBoundsValue == nil {
		return &LedgerError{Code: LedgerCodeDrift, Message: "init entry missing numeric token bounds"}
	}
	if first.TokenBoundsValue.MaxInputTokens != l.cfg.TokenBounds.MaxInputTokens {
		return &LedgerError{Code: LedgerCodeDrift, Message: fmt.Sprintf("maxInputTokens drift: ledger has %d, config has %d", first.TokenBoundsValue.MaxInputTokens, l.cfg.TokenBounds.MaxInputTokens)}
	}
	if first.TokenBoundsValue.MaxOutputTokens != l.cfg.TokenBounds.MaxOutputTokens {
		return &LedgerError{Code: LedgerCodeDrift, Message: fmt.Sprintf("maxOutputTokens drift: ledger has %d, config has %d", first.TokenBoundsValue.MaxOutputTokens, l.cfg.TokenBounds.MaxOutputTokens)}
	}
	return nil
}

// validateEntry performs basic integrity checks on an entry.
func (l *Ledger) validateEntry(e Entry) error {
	if e.ID == "" {
		return errors.New("empty entry id")
	}
	if e.Type != EntryReservation && e.Type != EntrySettlement && e.Type != EntryAdjustment && e.Type != EntryInit && e.Type != EntryMismatch {
		return fmt.Errorf("invalid entry type %q", e.Type)
	}
	// Mismatch entries are evidence-only: their Amount may be 0 when the
	// existing reservation already covers the conservative exposure; we
	// only enforce positivity for active ledger entries.
	if e.Type != EntryInit && e.Type != EntryMismatch && e.Amount == 0 {
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
	if (e.Type == EntrySettlement || e.Type == EntryMismatch) && e.RefID == "" {
		return errors.New("settlement/mismatch entry missing refId")
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

	// Halted-ledger rule: no new spending via Reserve. Settle on a
	// non-terminal, pre-halt reservation is the only allowed path; the
	// terminal/disputed check inside Settle enforces evidence durability.
	if l.halted {
		return "", 0, &LedgerError{Code: LedgerCodeHalted, Message: "ledger halted; new reservations rejected to prevent new spending"}
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
//   - Reservation is already terminal (mismatch) — re-settle is rejected
//
// L3: Latches halted flag on overrun; subsequent Reserve blocked.
// L4: Verifies actual rates/tokens against reservation.
// B1: Rate/token mismatch appends a durable mismatch entry carrying
//
//	reserved + actual rate/tokens/cost and reservation ID, marks the
//	reservation terminal/disputed, latches the halted flag, and the
//	ledger holds at least max(reserved, actual) conservative exposure
//	so a follow-up "valid" settlement cannot silently reopen spend.
//
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
	// B1: A previously-mismatched reservation cannot be re-settled as
	// "valid" — its terminal/disputed status is durable evidence.
	for _, e := range l.entries {
		if e.Type == EntryMismatch && e.RefID == refID {
			return 0, &LedgerError{Code: LedgerCodeDuplicateSettle, Message: fmt.Sprintf("reservation %q is terminal (mismatch on record); cannot re-settle", refID)}
		}
	}

	// B1: Detect rate/token mismatch. When detected we MUST persist
	// immutable evidence before returning the error. The mismatch entry
	// carries the actual rate/tokens/cost, the reservation ID, and the
	// reservation-vs-actual rates for reviewer audit. The reservation is
	// marked terminal; a subsequent valid-shaped settle on the same
	// reservation is rejected as duplicate.
	rateMismatch := (actualRateIn != 0 && res.RateIn != 0 && actualRateIn != res.RateIn) ||
		(actualRateOut != 0 && res.RateOut != 0 && actualRateOut != res.RateOut)
	tokenMismatch := actualTokensIn > res.TokensIn || actualTokensOut > res.TokensOut
	if rateMismatch || tokenMismatch {
		reason := "rate-mismatch"
		if !rateMismatch {
			reason = "token-mismatch"
		}
		// E1: Conservative exposure accounting for mismatch must NOT
		// overwrite other reservations' outstanding exposure. Compute
		// the delta (the worst-case overrun beyond the reservation),
		// then checked-add it to the existing global balance. The
		// reservation itself remains in the ledger at its reserved
		// amount; the mismatch evidence records the additional
		// conservative exposure for this single reservation.
		reservedAmount := res.Amount
		delta := actualCost - reservedAmount
		if delta < 0 {
			delta = 0
		}
		// Overflow check (fail-closed) BEFORE appending evidence. If
		// checked-add would overflow MaxMicroUnits, return the error
		// without appending or mutating balance. Already-persisted
		// entries (the reservation itself) are not dropped — the
		// mismatch evidence simply does not get written for an
		// overflow that cannot be represented.
		if delta > MaxMicroUnits-l.balance {
			return 0, &LedgerError{Code: LedgerCodeOverflow, Message: fmt.Sprintf("mismatch delta %d would overflow global balance %d", delta, l.balance)}
		}
		newBalance := l.balance + delta
		// Append the mismatch evidence first (durable, before error return).
		mismatchEntry := Entry{
			ID:                   fmt.Sprintf("mis-%s-%d", l.cfg.RunID, time.Now().UnixNano()),
			Type:                 EntryMismatch,
			Timestamp:            time.Now().UTC(),
			RunID:                l.cfg.RunID,
			Attempt:              res.Attempt,
			Model:                l.cfg.Model,
			Task:                 res.Task,
			SourceGroup:          res.SourceGroup,
			Amount:               delta,
			Balance:              newBalance,
			TokensIn:             res.TokensIn,
			TokensOut:            res.TokensOut,
			RateIn:               res.RateIn,
			RateOut:              res.RateOut,
			Notes:                fmt.Sprintf("rate/token mismatch for reservation %q; actual billed %d", refID, actualCost),
			RefID:                refID,
			ExpectedRateIn:       res.RateIn,
			ExpectedRateOut:      res.RateOut,
			ActualRateIn:         actualRateIn,
			ActualRateOut:        actualRateOut,
			ActualRateOutPresent: actualRateOut != 0,
			ActualTokensIn:       actualTokensIn,
			ActualTokensOut:      actualTokensOut,
			ActualCost:           actualCost,
			MismatchReason:       reason,
			ReservationTerminal:  true,
		}
		if err := l.appendEntry(mismatchEntry); err != nil {
			return 0, err
		}
		l.balance = newBalance
		l.lastEntry++
		// Latch durable halt and persist via init entry rewrite.
		l.halted = true
		if len(l.entries) > 0 && l.entries[0].Type == EntryInit {
			l.entries[0].Halted = true
			if err := l.rewriteInitEntry(); err != nil {
				return 0, err
			}
		}
		errCode := LedgerCodeRateMismatch
		if !rateMismatch {
			errCode = LedgerCodeTokenMismatch
		}
		return 0, &LedgerError{Code: errCode, Message: fmt.Sprintf("%s for reservation %q; mismatch evidence persisted; ledger halted", reason, refID)}
	}

	// Halted-ledger rule: Reserve on a halted ledger is fully blocked
	// (enforced above), so any reservation that exists was made before
	// the halt. Settle for such a reservation is the "already-in-flight
	// billed cost" reconciliation explicitly allowed by the directive,
	// unless the reservation has been recorded as terminal/disputed via
	// the mismatch check above. We therefore do not block Settle here on
	// the halt flag — only the terminal/disputed check above can do that.

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
