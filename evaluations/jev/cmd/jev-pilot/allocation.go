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

type authIdentityKey struct {
	AuthorizationReference string `json:"authorizationReference"`
	AuthorizationCap       int64  `json:"authorizationCapMicrodollars"`
	FrozenPriorExposure    int64  `json:"frozenPriorExposureMicrodollars"`
	InventoryHash          string `json:"inventoryHash"`
	ManifestSHA256         string `json:"executionManifestSha256"`
}

type authorizationLedgerEntry struct {
	Key         authIdentityKey    `json:"key"`
	Allocations []allocationRecord `json:"allocations"`
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

func authIdentityKeyFromRecord(r allocationRecord) authIdentityKey {
	return authIdentityKey{
		AuthorizationReference: r.AuthorizationReference,
		AuthorizationCap:       r.AuthorizationCap,
		FrozenPriorExposure:    r.FrozenPriorExposure,
		InventoryHash:          r.InventoryHash,
		ManifestSHA256:         r.ManifestSHA256,
	}
}

func authIdentityKeyFromManifest(m executionManifest, manifestSHA string) authIdentityKey {
	return authIdentityKey{
		AuthorizationReference: m.AuthorizationReference,
		AuthorizationCap:       int64(m.AuthorizationCap),
		FrozenPriorExposure:    int64(m.FrozenPriorExposure),
		InventoryHash:          m.InventoryHash,
		ManifestSHA256:         manifestSHA,
	}
}

func authorizationLedgerPath(allocationRecordPath string) string {
	// Store the authorization ledger in the run directory (parent of allocation record)
	// so that each run gets its own isolated ledger. The ledger filename is derived
	// from a fixed string to prevent forking within the same run directory.
	runDir := filepath.Dir(allocationRecordPath)
	sum := sha256.Sum256([]byte("jev-pilot-authorization-ledger"))
	return filepath.Join(runDir, hex.EncodeToString(sum[:16])+".json")
}

func loadAuthorizationLedger(allocationRecordPath string) (authorizationLedgerEntry, error) {
	path := authorizationLedgerPath(allocationRecordPath)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return authorizationLedgerEntry{Allocations: nil}, nil
	}
	if err != nil {
		return authorizationLedgerEntry{}, fmt.Errorf("pilot: read authorization ledger: %w", err)
	}
	var ledger authorizationLedgerEntry
	if err := json.Unmarshal(data, &ledger); err != nil {
		return authorizationLedgerEntry{}, fmt.Errorf("pilot: decode authorization ledger: %w", err)
	}
	return ledger, nil
}

func saveAuthorizationLedger(allocationRecordPath string, ledger authorizationLedgerEntry) error {
	path := authorizationLedgerPath(allocationRecordPath)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return err
	}
	// Use a temporary file and atomic rename to avoid O_EXCL conflict
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, append(b, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func checkAuthorizationExclusivity(expected allocationRecord) error {
	key := authIdentityKeyFromRecord(expected)
	ledger, err := loadAuthorizationLedger(expected.AllocationRecordPath)
	if err != nil {
		return err
	}
	// If ledger has no key yet (first allocation), no existing allocations to check
	if ledger.Key.AuthorizationReference == "" {
		return nil
	}
	// Verify the key matches
	if ledger.Key != key {
		return fmt.Errorf("pilot: authorization ledger key mismatch: existing=%+v new=%+v", ledger.Key, key)
	}
	// Check if an allocation with this identity already exists
	for _, existing := range ledger.Allocations {
		if authIdentityKeyFromRecord(existing) == key {
			return errors.New("pilot: allocation for this authorization identity already exists; refusing duplicate allocation")
		}
	}
	return nil
}

func recordAllocation(expected allocationRecord) error {
	key := authIdentityKeyFromRecord(expected)
	ledger, err := loadAuthorizationLedger(expected.AllocationRecordPath)
	if err != nil {
		return err
	}
	// Always update the ledger key to match the allocation's key
	ledger.Key = key
	ledger.Allocations = append(ledger.Allocations, expected)
	return saveAuthorizationLedger(expected.AllocationRecordPath, ledger)
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
		// Check authorization exclusivity BEFORE claiming
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

func claimAllocation(path string, record allocationRecord) error {
	b, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFrozen(path, append(b, '\n')); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("pilot: allocation was claimed concurrently; refusing fresh run")
		}
		return fmt.Errorf("pilot: claim allocation: %w", err)
	}
	// Record the allocation in the authorization ledger
	if err := recordAllocation(record); err != nil {
		// If ledger recording fails, remove the allocation record to keep consistency
		_ = os.Remove(path)
		return fmt.Errorf("pilot: record allocation in authorization ledger: %w", err)
	}
	return nil
}
