package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	return nil
}
