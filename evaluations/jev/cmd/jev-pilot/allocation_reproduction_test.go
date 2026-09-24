package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/ledger"
)

// TestW2R1_AllocationExclusivityByAuthorizationIdentity reproduces W2-R1:
// allocation exclusivity is path-local — the $1 can be double-allocated.
// The same $1 authorization admits TWO 949,856 µ$ allocations by changing
// the CALLER-SELECTED allocation path/ID and the state paths.
// The frozen 50,144 µ$ prior exposure gets declared twice.
func TestW2R1_AllocationExclusivityByAuthorizationIdentity(t *testing.T) {
	dir := t.TempDir()

	// Same authorization reference, same cap, same frozen prior exposure, same inventory
	authRef := "test-authorization-123"
	authCap := ledger.MicroUnit(1_000_000)
	frozenPrior := ledger.MicroUnit(50_144)
	combinedCap := ledger.MicroUnit(949_856)
	inventoryHash := "same-inventory-hash"
	manifestSHA := "same-manifest-sha"
	runID1 := "run-1"
	runID2 := "run-2"

	// First allocation with one set of paths
	allocationID1 := "allocation-id-1"
	allocationRecordPath1 := filepath.Join(dir, "allocation-1.json")
	journalPath1 := filepath.Join(dir, "journal-1.jsonl")
	jevLedgerPath1 := filepath.Join(dir, "jev-1.jsonl")
	nanoLedgerPath1 := filepath.Join(dir, "nano-1.jsonl")
	evidenceDir1 := filepath.Join(dir, "evidence-1")
	runLockPath1 := filepath.Join(dir, "run-1.lock")

	m1 := executionManifest{
		AuthorizationReference: authRef,
		AuthorizationCap:       authCap,
		FrozenPriorExposure:    frozenPrior,
		CombinedCap:            combinedCap,
		InventoryHash:          inventoryHash,
		AllocationID:           allocationID1,
		AllocationRecordPath:   allocationRecordPath1,
		RunID:                  runID1,
		JournalPath:            journalPath1,
		Jev:                    armManifest{LedgerPath: jevLedgerPath1},
		Nano:                   armManifest{LedgerPath: nanoLedgerPath1},
		EvidenceDir:            evidenceDir1,
		RunLockPath:            runLockPath1,
		ProbeReconciliation:    "freeze-execution-and-re-review-allocation-before-any-further-send",
	}

	// Claim first allocation
	expected1, err := expectedAllocation(m1, manifestSHA)
	if err != nil {
		t.Fatalf("expectedAllocation 1: %v", err)
	}
	if err := claimAllocation(allocationRecordPath1, expected1); err != nil {
		t.Fatalf("claimAllocation 1: %v", err)
	}

	// Second allocation with DIFFERENT paths but SAME authorization identity
	allocationID2 := "allocation-id-2"
	allocationRecordPath2 := filepath.Join(dir, "allocation-2.json")
	journalPath2 := filepath.Join(dir, "journal-2.jsonl")
	jevLedgerPath2 := filepath.Join(dir, "jev-2.jsonl")
	nanoLedgerPath2 := filepath.Join(dir, "nano-2.jsonl")
	evidenceDir2 := filepath.Join(dir, "evidence-2")
	runLockPath2 := filepath.Join(dir, "run-2.lock")

	m2 := executionManifest{
		AuthorizationReference: authRef,
		AuthorizationCap:       authCap,
		FrozenPriorExposure:    frozenPrior,
		CombinedCap:            combinedCap,
		InventoryHash:          inventoryHash,
		AllocationID:           allocationID2,
		AllocationRecordPath:   allocationRecordPath2,
		RunID:                  runID2,
		JournalPath:            journalPath2,
		Jev:                    armManifest{LedgerPath: jevLedgerPath2},
		Nano:                   armManifest{LedgerPath: nanoLedgerPath2},
		EvidenceDir:            evidenceDir2,
		RunLockPath:            runLockPath2,
		ProbeReconciliation:    "freeze-execution-and-re-review-allocation-before-any-further-send",
	}

	// This should FAIL because the authorization identity (authRef + cap + frozenPrior + inventoryHash + manifestSHA)
	// matches the first allocation
	expected2, err := expectedAllocation(m2, manifestSHA)
	if err != nil {
		t.Fatalf("expectedAllocation 2: %v", err)
	}

	err = checkAuthorizationExclusivity(expected2)
	if err != nil {
		t.Logf("W2-R1 FIXED: Second allocation correctly rejected at checkAuthorizationExclusivity: %v", err)
		return
	}

	err = claimAllocation(allocationRecordPath2, expected2)
	if err == nil {
		t.Fatal("W2-R1 REPRODUCED: Second allocation with different path/ID but same authorization identity was allowed. The frozen 50,144 prior exposure was declared twice.")
	}
	t.Logf("W2-R1 FIXED: Second allocation correctly rejected at claimAllocation: %v", err)

	// Also verify the frozen prior exposure is only declared once
	data1, _ := os.ReadFile(allocationRecordPath1)
	var record1 allocationRecord
	json.Unmarshal(data1, &record1)
	if record1.FrozenPriorExposure != int64(frozenPrior) {
		t.Fatalf("First allocation frozen prior exposure: got %d want %d", record1.FrozenPriorExposure, frozenPrior)
	}

	// The second allocation file should not exist
	if _, err := os.Stat(allocationRecordPath2); !os.IsNotExist(err) {
		t.Fatal("Second allocation file was created despite rejection")
	}
}
