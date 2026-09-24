package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const probeReconciliationPrecondition = "freeze-execution-and-re-review-allocation-before-any-further-send"

type allocationRecord struct {
	Version                   string `json:"version"`
	AllocationID              string `json:"allocationId"`
	AuthorizationReference    string `json:"authorizationReference"`
	AuthorizationCap          int64  `json:"authorizationCapMicrodollars"`
	FrozenPriorExposure       int64  `json:"frozenPriorExposureMicrodollars"`
	AllocatedCap              int64  `json:"allocatedCapMicrodollars"`
	InventoryHash             string `json:"inventoryHash"`
	RunID                     string `json:"runId"`
	ManifestSHA256            string `json:"executionManifestSha256"`
	AllocationRecordPath      string `json:"allocationRecordPath"`
	JournalPath               string `json:"journalPath"`
	JevLedgerPath             string `json:"jevLedgerPath"`
	NanoLedgerPath            string `json:"nanoLedgerPath"`
	EvidenceDir               string `json:"evidenceDir"`
	RunLockPath               string `json:"runLockPath"`
	ProbeReconciliationPolicy string `json:"probeReconciliationPrecondition"`
}

// authorizationScope is the authorization identity the allocation claim is
// exclusive against: the authorization reference, its cap, and the frozen
// prior exposure declared against it. Any second allocation under the same
// authorization — regardless of caller-selected run/state paths, allocation
// ID, inventory hash, or execution-manifest SHA — converges on the same
// registry entry and is refused.
type authorizationScope struct {
	AuthorizationReference string `json:"authorizationReference"`
	AuthorizationCap       int64  `json:"authorizationCapMicrodollars"`
	FrozenPriorExposure    int64  `json:"frozenPriorExposureMicrodollars"`
}

func authorizationScopeOf(r allocationRecord) authorizationScope {
	return authorizationScope{
		AuthorizationReference: r.AuthorizationReference,
		AuthorizationCap:       r.AuthorizationCap,
		FrozenPriorExposure:    r.FrozenPriorExposure,
	}
}

func expectedAllocation(m executionManifest, manifestSHA string) (allocationRecord, error) {
	if m.AllocationID == "" || m.AllocationRecordPath == "" || m.AuthorizationReference == "" || m.RunID == "" {
		return allocationRecord{}, errors.New("pilot: allocation identity, record path, authorization reference, and run ID are required")
	}
	if m.AuthorizationCap <= 0 || m.FrozenPriorExposure < 0 || m.CombinedCap <= 0 || m.CombinedCap > m.AuthorizationCap || m.FrozenPriorExposure > m.AuthorizationCap-m.CombinedCap {
		return allocationRecord{}, errors.New("pilot: frozen prior exposure plus allocated cap exceeds authorization")
	}
	if m.ProbeReconciliation != probeReconciliationPrecondition {
		return allocationRecord{}, fmt.Errorf("pilot: probe reconciliation precondition must equal %q", probeReconciliationPrecondition)
	}
	if m.JournalPath == "" || m.Jev.LedgerPath == "" || m.Nano.LedgerPath == "" || m.EvidenceDir == "" || m.RunLockPath == "" {
		return allocationRecord{}, errors.New("pilot: allocation requires complete run-state paths")
	}
	return allocationRecord{
		Version: "jev-pilot-allocation-v1", AllocationID: m.AllocationID,
		AuthorizationReference: m.AuthorizationReference, AuthorizationCap: int64(m.AuthorizationCap),
		FrozenPriorExposure: int64(m.FrozenPriorExposure), AllocatedCap: int64(m.CombinedCap),
		InventoryHash: m.InventoryHash, RunID: m.RunID, ManifestSHA256: manifestSHA,
		AllocationRecordPath: m.AllocationRecordPath, JournalPath: m.JournalPath,
		JevLedgerPath: m.Jev.LedgerPath, NanoLedgerPath: m.Nano.LedgerPath,
		EvidenceDir: m.EvidenceDir, RunLockPath: m.RunLockPath,
		ProbeReconciliationPolicy: m.ProbeReconciliation,
	}, nil
}

func allocationDigest(record allocationRecord) (string, error) {
	b, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// allocationRegistryRoot returns the machine/user-scoped allocation registry
// root. It is deliberately NOT derived from any caller-supplied path: the
// manifest cannot fork the registry by selecting a different run directory.
// Trust boundary: physical filesystem tampering by the operator (including
// manipulating the process environment that locates the user config dir) is
// out of model; accidental forking via different run-local paths is
// impossible by construction because the entry path depends only on the
// authorization identity.
func allocationRegistryRoot() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("pilot: allocation registry base: %w", err)
	}
	return filepath.Join(base, "jev-pilot-allocation-registry"), nil
}

// authorizationScopeDir derives the registry location from the authorization
// identity so disjoint run/state trees converge on the same entry.
func authorizationScopeDir(scope authorizationScope) (string, error) {
	root, err := allocationRegistryRoot()
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(scope)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return filepath.Join(root, hex.EncodeToString(sum[:])), nil
}

// authorizationClaimPath is the registry entry for this authorization. The
// claim IS an atomic O_CREATE|O_EXCL creation of this file; existence =
// claimed. There is no separate check/claim window.
func authorizationClaimPath(record allocationRecord) (string, error) {
	dir, err := authorizationScopeDir(authorizationScopeOf(record))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "claim.json"), nil
}

// authorizationHistoryPath is the append-only, hash-chained claim history
// (tamper evidence) for this authorization.
func authorizationHistoryPath(record allocationRecord) (string, error) {
	dir, err := authorizationScopeDir(authorizationScopeOf(record))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history.jsonl"), nil
}

type authorizationHistoryEntry struct {
	Seq           int    `json:"seq"`
	RecordSHA256  string `json:"recordSha256"`
	PrevEntryHash string `json:"prevEntryHash"`
	EntryHash     string `json:"entryHash"`
}

func historyEntryHash(seq int, recordSHA, prev string) string {
	canonical, _ := json.Marshal(struct {
		Seq           int    `json:"seq"`
		RecordSHA256  string `json:"recordSha256"`
		PrevEntryHash string `json:"prevEntryHash"`
	}{Seq: seq, RecordSHA256: recordSHA, PrevEntryHash: prev})
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// verifyAuthorizationRegistry fails closed on any registry/history anomaly:
// a hash-chain break, or a history entry without its registry record. A
// registry record without a history entry is the tolerated crash window
// between the atomic claim and the evidence append. The claim itself (if
// present) is authoritative for exclusivity regardless.
func verifyAuthorizationRegistry(record allocationRecord) error {
	claimPath, err := authorizationClaimPath(record)
	if err != nil {
		return err
	}
	historyPath, err := authorizationHistoryPath(record)
	if err != nil {
		return err
	}
	claimBytes, claimErr := os.ReadFile(claimPath)
	claimExists := claimErr == nil
	if claimErr != nil && !errors.Is(claimErr, os.ErrNotExist) {
		return fmt.Errorf("pilot: inspect allocation registry: %w", claimErr)
	}
	claimSHA := ""
	if claimExists {
		claimSHA = hashHex(claimBytes)
	}
	historyBytes, historyErr := os.ReadFile(historyPath)
	historyExists := historyErr == nil
	if historyErr != nil && !errors.Is(historyErr, os.ErrNotExist) {
		return fmt.Errorf("pilot: inspect allocation claim history: %w", historyErr)
	}
	if historyExists {
		entries, err := decodeHistory(historyBytes)
		if err != nil {
			return err
		}
		prev := ""
		for i, entry := range entries {
			if entry.Seq != i+1 || entry.PrevEntryHash != prev || entry.EntryHash != historyEntryHash(entry.Seq, entry.RecordSHA256, entry.PrevEntryHash) {
				return errors.New("pilot: allocation claim history chain break; refusing fail-closed")
			}
			if !claimExists || entry.RecordSHA256 != claimSHA {
				return errors.New("pilot: allocation claim history entry without its registry record; refusing fail-closed")
			}
			prev = entry.EntryHash
		}
	}
	return nil
}

func decodeHistory(data []byte) ([]authorizationHistoryEntry, error) {
	var entries []authorizationHistoryEntry
	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}
		var entry authorizationHistoryEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil, errors.New("pilot: allocation claim history undecodable; refusing fail-closed")
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, c := range data {
		if c == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// checkAuthorizationExclusivity is a read-only, fail-closed inspection of the
// authorization registry. It is NOT the enforcement point: exclusivity is
// enforced by the atomic O_CREATE|O_EXCL claim in claimAllocation, so no
// check/claim interleaving can double-allocate.
func checkAuthorizationExclusivity(expected allocationRecord) error {
	if err := verifyAuthorizationRegistry(expected); err != nil {
		return err
	}
	claimPath, err := authorizationClaimPath(expected)
	if err != nil {
		return err
	}
	if _, err := os.Stat(claimPath); err == nil {
		return errors.New("pilot: allocation for this authorization identity already exists; refusing duplicate allocation")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("pilot: inspect allocation registry: %w", err)
	}
	return nil
}

func preflightAllocation(m executionManifest, manifestSHA string, resume bool) (allocationRecord, string, error) {
	expected, err := expectedAllocation(m, manifestSHA)
	if err != nil {
		return allocationRecord{}, "", err
	}
	digest, err := allocationDigest(expected)
	if err != nil {
		return allocationRecord{}, "", err
	}
	data, readErr := os.ReadFile(m.AllocationRecordPath)
	if !resume {
		if readErr == nil {
			return allocationRecord{}, "", errors.New("pilot: allocation already claimed; a fresh run requires a newly reviewed allocation")
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			return allocationRecord{}, "", fmt.Errorf("pilot: inspect allocation record: %w", readErr)
		}
		// Fail-closed registry inspection BEFORE claiming and before any
		// credential lookup or dial. The atomic claim in claimAllocation is
		// the enforcement point; this early refusal only improves the error.
		if err := checkAuthorizationExclusivity(expected); err != nil {
			return allocationRecord{}, "", err
		}
		return expected, digest, nil
	}
	if readErr != nil {
		return allocationRecord{}, "", fmt.Errorf("pilot: resume requires its allocation record: %w", readErr)
	}
	var existing allocationRecord
	if err := json.Unmarshal(data, &existing); err != nil {
		return allocationRecord{}, "", fmt.Errorf("pilot: decode allocation record: %w", err)
	}
	if existing != expected {
		return allocationRecord{}, "", errors.New("pilot: allocation record does not match manifest, inventory, run, or state identity")
	}
	return expected, digest, nil
}

// claimAllocation claims the allocation. The authorization-scoped registry
// entry is created atomically with O_CREATE|O_EXCL: existence = claimed, so
// concurrent check/claim interleavings, disjoint run/state trees, and
// two-process races cannot double-allocate. The run-local allocation record
// (O_EXCL) and the hash-chained claim history are written around it.
func claimAllocation(path string, record allocationRecord) error {
	b, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	recordBytes := append(b, '\n')
	if err := writeFrozen(path, recordBytes); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("pilot: allocation was claimed concurrently; refusing fresh run")
		}
		return fmt.Errorf("pilot: claim allocation: %w", err)
	}
	if err := claimAuthorizationRegistry(record, recordBytes); err != nil {
		// The run-local record is ours to remove; the registry claim (if it
		// landed) is authoritative and is never reset by deletion here.
		_ = os.Remove(path)
		return err
	}
	return nil
}

func claimAuthorizationRegistry(record allocationRecord, recordBytes []byte) error {
	claimPath, err := authorizationClaimPath(record)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(claimPath), 0o700); err != nil {
		return fmt.Errorf("pilot: allocation registry: %w", err)
	}
	f, err := os.OpenFile(claimPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o444)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("pilot: allocation for this authorization identity already exists; refusing duplicate allocation")
		}
		return fmt.Errorf("pilot: allocation registry claim: %w", err)
	}
	if _, err := f.Write(recordBytes); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return fmt.Errorf("pilot: allocation registry claim: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("pilot: allocation registry claim: %w", closeErr)
	}
	if err := appendAuthorizationHistory(record, hashHex(recordBytes)); err != nil {
		return fmt.Errorf("pilot: record allocation claim history: %w", err)
	}
	return nil
}

// appendAuthorizationHistory extends the append-only, hash-chained claim
// history. The update is written under a unique temp name and renamed into
// place (never a shared fixed temp path).
func appendAuthorizationHistory(record allocationRecord, recordSHA string) error {
	historyPath, err := authorizationHistoryPath(record)
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(historyPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	entries, err := decodeHistory(existing)
	if err != nil {
		return err
	}
	prev := ""
	if len(entries) > 0 {
		prev = entries[len(entries)-1].EntryHash
	}
	entry := authorizationHistoryEntry{
		Seq:           len(entries) + 1,
		RecordSHA256:  recordSHA,
		PrevEntryHash: prev,
	}
	entry.EntryHash = historyEntryHash(entry.Seq, entry.RecordSHA256, entry.PrevEntryHash)
	entries = append(entries, entry)
	var out []byte
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		out = append(out, line...)
		out = append(out, '\n')
	}
	tmp, err := os.CreateTemp(filepath.Dir(historyPath), "history-*.jsonl.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(out); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return closeErr
	}
	if err := os.Rename(tmpPath, historyPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
