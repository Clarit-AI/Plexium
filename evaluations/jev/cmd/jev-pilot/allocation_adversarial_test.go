package main

// Preserved and independent adversarial probes for W2-R1 (allocation
// exclusivity), folded in as permanent regressions:
//   - TestReviewerAuthorizationReuseViaNewRecordPath (original wiring review)
//   - disjoint run/state trees with the same authorization identity
//     (distinct manifest SHAs and identical manifest SHA)
//   - registry deletion between claims (claim entry and history, separately)
//   - deterministic check/claim interleaving (TOCTOU)
//   - a two-process barrier race
//   - the frozen 50,144 µ$ prior exposure declared exactly once
//
// Adaptation note: the probes isolate the machine/user-scoped allocation
// registry per test (fresh HOME/XDG/AppData) so fixed test identities do not
// collide across `go test` runs; every scenario's semantics are unchanged.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/ledger"
)

// isolateAllocationRegistry points the (non caller-selectable) allocation
// registry at a fresh per-test root so each test run starts unclaimed.
func isolateAllocationRegistry(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
}

// TestReviewerAuthorizationReuseViaNewRecordPath is the preserved W2-R1
// reproduction from the original wiring review: a second allocation under
// the same authorization identity must be refused even with a fresh record
// path and run/state identity.
func TestReviewerAuthorizationReuseViaNewRecordPath(t *testing.T) {
	isolateAllocationRegistry(t)
	d := t.TempDir()
	m := executionManifest{AuthorizationReference: "same-$1-authorization", AuthorizationCap: 1000000, FrozenPriorExposure: 50144, CombinedCap: 949856, AllocationID: "one", AllocationRecordPath: filepath.Join(d, "allocation-one.json"), InventoryHash: "inventory", RunID: "one", JournalPath: "j", EvidenceDir: "e", RunLockPath: "l", ProbeReconciliation: probeReconciliationPrecondition, Jev: armManifest{LedgerPath: "jl"}, Nano: armManifest{LedgerPath: "nl"}}
	a, _, err := preflightAllocation(m, "manifest1", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = claimAllocation(m.AllocationRecordPath, a); err != nil {
		t.Fatal(err)
	}
	m.AllocationID = "two"
	m.AllocationRecordPath = filepath.Join(d, "allocation-two.json")
	m.RunID = "two"
	m.JournalPath = "j2"
	m.Jev.LedgerPath = "jl2"
	m.Nano.LedgerPath = "nl2"
	m.EvidenceDir = "e2"
	m.RunLockPath = "l2"
	a, _, err = preflightAllocation(m, "manifest2", false)
	if err == nil {
		if err = claimAllocation(m.AllocationRecordPath, a); err == nil {
			t.Fatal("same authorization allocated 949856 twice with frozen exposure 50144")
		}
	}
}

const advAuthRef = "authorization-$1-kha-579"

func advManifest(authRef, dir, allocID string) executionManifest {
	return executionManifest{
		InventoryHash:          "adv-inventory-hash",
		AuthorizationReference: authRef,
		AuthorizationCap:       ledger.MicroUnit(1_000_000),
		FrozenPriorExposure:    ledger.MicroUnit(50_144),
		AllocationID:           allocID,
		AllocationRecordPath:   filepath.Join(dir, "allocation.json"),
		ProbeReconciliation:    probeReconciliationPrecondition,
		RunID:                  "run-" + allocID,
		JournalPath:            filepath.Join(dir, "journal.jsonl"),
		EvidenceDir:            filepath.Join(dir, "evidence"),
		RunLockPath:            filepath.Join(dir, "run.lock"),
		CombinedCap:            ledger.MicroUnit(949_856),
		Jev:                    armManifest{LedgerPath: filepath.Join(dir, "jev.jsonl")},
		Nano:                   armManifest{LedgerPath: filepath.Join(dir, "nano.jsonl")},
	}
}

// countClaimDeclarations scans every JSON file under root and counts
// allocation-record declarations with the frozen prior exposure.
func countClaimDeclarations(root string, frozen int64) (claims int, filesWithNumber []string) {
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if strings.Contains(string(data), fmt.Sprintf("%d", frozen)) {
			filesWithNumber = append(filesWithNumber, path)
		}
		var rec allocationRecord
		if json.Unmarshal(data, &rec) == nil && rec.FrozenPriorExposure == frozen && rec.AllocationID != "" {
			// An allocation-record file: one claim = one declaration of the
			// frozen prior exposure.
			claims++
		}
		return nil
	})
	return claims, filesWithNumber
}

// Probe (a): FULLY DISJOINT run/state directory trees, same authorization
// identity, realistic distinct manifest SHAs.
func TestAdvW2R1a_DisjointRunTreesSameAuthorization(t *testing.T) {
	isolateAllocationRegistry(t)
	root := t.TempDir()
	dirA := filepath.Join(root, "runA", "state")
	dirB := filepath.Join(root, "runB", "state")
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mA := advManifest(advAuthRef, dirA, "alloc-A")
	mB := advManifest(advAuthRef, dirB, "alloc-B")

	aA, _, err := preflightAllocation(mA, "manifest-sha-A", false)
	if err != nil {
		t.Fatalf("first preflight: %v", err)
	}
	if err := claimAllocation(mA.AllocationRecordPath, aA); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	aB, _, err := preflightAllocation(mB, "manifest-sha-B", false)
	errB := claimAllocation(mB.AllocationRecordPath, aB)
	if err == nil && errB == nil {
		claims, files := countClaimDeclarations(root, 50_144)
		t.Fatalf("W2-R1 HOLE REPRODUCED (disjoint trees): same authorization identity allocated 949,856 twice across disjoint run dirs; frozen 50,144 declared %d times across claims; files carrying it: %v", claims, files)
	}
	t.Logf("second allocation refused: preflight=%v claim=%v", err, errB)
}

// Probe (a2): disjoint trees AND identical manifest SHA (strongest fork: even
// an identical key never meets the first record).
func TestAdvW2R1a2_DisjointRunTreesIdenticalManifestSHA(t *testing.T) {
	isolateAllocationRegistry(t)
	root := t.TempDir()
	dirA := filepath.Join(root, "a")
	dirB := filepath.Join(root, "b")
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mA := advManifest(advAuthRef, dirA, "alloc-A")
	mB := advManifest(advAuthRef, dirB, "alloc-B")
	aA, _, err := preflightAllocation(mA, "same-manifest-sha", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := claimAllocation(mA.AllocationRecordPath, aA); err != nil {
		t.Fatal(err)
	}
	aB, _, err := preflightAllocation(mB, "same-manifest-sha", false)
	errB := claimAllocation(mB.AllocationRecordPath, aB)
	if err == nil && errB == nil {
		t.Fatal("W2-R1 HOLE REPRODUCED (disjoint trees, identical key): second 949,856 allocation admitted")
	}
	t.Logf("refused: preflight=%v claim=%v", err, errB)
}

// Probe (b): the registry is not reset by deletion of ONE of its artifacts.
// Deleting the claim entry must fail closed on the claim history ("history
// entry without its registry record"); deleting the history must still hit
// the atomic claim's existence refusal. Deleting BOTH is physical filesystem
// tampering and out of model (documented trust boundary).
func TestAdvW2R1b_RegistryDeletedBetweenClaims(t *testing.T) {
	for _, target := range []string{"claim", "history"} {
		t.Run("delete-"+target, func(t *testing.T) {
			isolateAllocationRegistry(t)
			dir := t.TempDir()
			m1 := advManifest(advAuthRef, dir, "alloc-one")
			a1, _, err := preflightAllocation(m1, "manifest-sha-1", false)
			if err != nil {
				t.Fatal(err)
			}
			if err := claimAllocation(m1.AllocationRecordPath, a1); err != nil {
				t.Fatal(err)
			}
			claimPath, err := authorizationClaimPath(a1)
			if err != nil {
				t.Fatal(err)
			}
			historyPath, err := authorizationHistoryPath(a1)
			if err != nil {
				t.Fatal(err)
			}
			victim := claimPath
			if target == "history" {
				victim = historyPath
			}
			if _, err := os.Stat(victim); err != nil {
				t.Fatalf("registry artifact not where expected: %v", err)
			}
			if err := os.Remove(victim); err != nil {
				t.Fatal(err)
			}
			m2 := advManifest(advAuthRef, dir, "alloc-two")
			m2.AllocationRecordPath = filepath.Join(dir, "allocation-two.json")
			m2.RunID = "run-two"
			m2.JournalPath = filepath.Join(dir, "journal-two.jsonl")
			m2.EvidenceDir = filepath.Join(dir, "evidence-two")
			m2.RunLockPath = filepath.Join(dir, "run-two.lock")
			m2.Jev.LedgerPath = filepath.Join(dir, "jev-two.jsonl")
			m2.Nano.LedgerPath = filepath.Join(dir, "nano-two.jsonl")
			a2, _, err := preflightAllocation(m2, "manifest-sha-1", false)
			if err == nil {
				if claimErr := claimAllocation(m2.AllocationRecordPath, a2); claimErr == nil {
					t.Fatalf("W2-R1 HOLE REPRODUCED (%s deletion): second allocation admitted after the registry %s was deleted", target, target)
				}
			}
			t.Logf("second claim refused after %s deletion: %v", target, err)
		})
	}
}

// Probe (c): deterministic check-then-claim interleaving (the TOCTOU window a
// second process can hit): both exclusivity checks run before either claim.
func TestAdvW2R1c_CheckThenClaimInterleave(t *testing.T) {
	isolateAllocationRegistry(t)
	dir := t.TempDir()
	m1 := advManifest(advAuthRef, dir, "alloc-one")
	m2 := advManifest(advAuthRef, dir, "alloc-two")
	m2.AllocationRecordPath = filepath.Join(dir, "allocation-two.json")
	m2.RunID = "run-two"
	m2.JournalPath = filepath.Join(dir, "journal-two.jsonl")
	m2.EvidenceDir = filepath.Join(dir, "evidence-two")
	m2.RunLockPath = filepath.Join(dir, "run-two.lock")
	m2.Jev.LedgerPath = filepath.Join(dir, "jev-two.jsonl")
	m2.Nano.LedgerPath = filepath.Join(dir, "nano-two.jsonl")

	a1, _, err1 := preflightAllocation(m1, "manifest-sha-same", false) // includes exclusivity check
	a2, _, err2 := preflightAllocation(m2, "manifest-sha-same", false) // both checks pass: neither claimed yet
	if err1 != nil || err2 != nil {
		t.Fatalf("interleaved preflights refused early: %v / %v", err1, err2)
	}
	c1 := claimAllocation(m1.AllocationRecordPath, a1)
	c2 := claimAllocation(m2.AllocationRecordPath, a2)
	if c1 == nil && c2 == nil {
		claims, _ := countClaimDeclarations(dir, 50_144)
		t.Fatalf("W2-R1 HOLE REPRODUCED (check/claim interleave): both allocations claimed after both checks passed; %d claims declare frozen 50,144", claims)
	}
	t.Logf("claims: %v / %v", c1, c2)
}

// Probe (d): two real processes racing with a forced barrier between
// exclusivity check and claim.
func TestAdvW2R1d_TwoProcessRace(t *testing.T) {
	isolateAllocationRegistry(t)
	dir := t.TempDir()
	ids := []string{"one", "two"}
	cmds := make([]*exec.Cmd, len(ids))
	bufs := make([]strings.Builder, len(ids))
	for i, id := range ids {
		cmd := exec.Command(os.Args[0], "-test.run=^TestAdvW2R1ClaimHelper$", "-test.v")
		cmd.Env = append(os.Environ(),
			"JEV_W2R1_HELPER=1",
			"JEV_W2R1_DIR="+dir,
			"JEV_W2R1_ID="+id,
			"JEV_W2R1_PEER="+map[string]string{"one": "two", "two": "one"}[id],
			"OPENROUTER_API_KEY=",
		)
		cmd.Stdout = &bufs[i]
		cmd.Stderr = &bufs[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds[i] = cmd
	}
	// Start gate: both helpers wait for this before preflight so their
	// check/claim windows overlap regardless of process spawn latency.
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for i, id := range ids {
		err := cmds[i].Wait()
		out[id] = bufs[i].String()
		if err != nil {
			t.Logf("helper %s exited: %v\n%s", id, err, out[id])
		}
	}
	claimed := 0
	for id, log := range out {
		if strings.Contains(log, "RESULT "+id+" CLAIMED") {
			claimed++
		}
	}
	if claimed == 2 {
		t.Fatalf("W2-R1 HOLE REPRODUCED (two-process race): both racing processes claimed the same authorization identity:\n--- one ---\n%s\n--- two ---\n%s", out["one"], out["two"])
	}
	t.Logf("claimed=%d\n--- one ---\n%s\n--- two ---\n%s", claimed, out["one"], out["two"])
}

// TestAdvW2R1ClaimHelper is the subprocess body for probe (d).
func TestAdvW2R1ClaimHelper(t *testing.T) {
	if os.Getenv("JEV_W2R1_HELPER") != "1" {
		t.Skip("helper for TestAdvW2R1d_TwoProcessRace")
	}
	dir := os.Getenv("JEV_W2R1_DIR")
	id := os.Getenv("JEV_W2R1_ID")
	peer := os.Getenv("JEV_W2R1_PEER")
	m := advManifest(advAuthRef, dir, "alloc-"+id)
	// Fully disjoint record/state paths within the shared run directory,
	// exactly as two independently launched pilots would select.
	m.AllocationRecordPath = filepath.Join(dir, "allocation-"+id+".json")
	m.RunID = "run-" + id
	m.JournalPath = filepath.Join(dir, "journal-"+id+".jsonl")
	m.EvidenceDir = filepath.Join(dir, "evidence-"+id)
	m.RunLockPath = filepath.Join(dir, "run-"+id+".lock")
	m.Jev.LedgerPath = filepath.Join(dir, "jev-"+id+".jsonl")
	m.Nano.LedgerPath = filepath.Join(dir, "nano-"+id+".jsonl")
	// Wait for the parent's start gate so both processes overlap.
	gateDeadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go")); err == nil {
			break
		}
		if time.Now().After(gateDeadline) {
			fmt.Printf("RESULT %s GATE-TIMEOUT\n", id)
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	a, _, err := preflightAllocation(m, "manifest-sha-race", false) // exclusivity check happens here
	if err != nil {
		fmt.Printf("RESULT %s REFUSED-PREFLIGHT %v\n", id, err)
		return
	}
	// Barrier: both processes have passed the exclusivity check; neither has
	// recorded its allocation yet. Force the race window open.
	ready := filepath.Join(dir, "ready-"+id)
	if err := os.WriteFile(ready, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "ready-"+peer)); err == nil {
			break
		}
		if time.Now().After(deadline) {
			fmt.Printf("RESULT %s BARRIER-TIMEOUT\n", id)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := claimAllocation(m.AllocationRecordPath, a); err != nil {
		fmt.Printf("RESULT %s REFUSED-CLAIM %v\n", id, err)
		return
	}
	fmt.Printf("RESULT %s CLAIMED\n", id)
}

// Probe (e): the frozen 50,144 µ$ prior exposure must be declared exactly
// once across the refusal path and across disjoint trees.
func TestAdvW2R1e_PriorExposureDeclaredOnceOnRefusalPath(t *testing.T) {
	// Scenario 1: refusal path (same directory; second claim must be refused).
	isolateAllocationRegistry(t)
	dir := t.TempDir()
	m1 := advManifest(advAuthRef, dir, "alloc-one")
	a1, _, err := preflightAllocation(m1, "manifest-sha-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := claimAllocation(m1.AllocationRecordPath, a1); err != nil {
		t.Fatal(err)
	}
	m2 := advManifest(advAuthRef, dir, "alloc-two")
	m2.AllocationRecordPath = filepath.Join(dir, "allocation-two.json")
	m2.RunID = "run-two"
	m2.JournalPath = filepath.Join(dir, "journal-two.jsonl")
	m2.EvidenceDir = filepath.Join(dir, "evidence-two")
	m2.RunLockPath = filepath.Join(dir, "run-two.lock")
	m2.Jev.LedgerPath = filepath.Join(dir, "jev-two.jsonl")
	m2.Nano.LedgerPath = filepath.Join(dir, "nano-two.jsonl")
	if a2, _, pErr := preflightAllocation(m2, "manifest-sha-2", false); pErr == nil {
		_ = claimAllocation(m2.AllocationRecordPath, a2)
	}
	claims, files := countClaimDeclarations(dir, 50_144)
	if claims != 1 {
		t.Fatalf("refusal path: frozen 50,144 declared by %d claims (want 1); files carrying it: %v", claims, files)
	}
	t.Logf("refusal path OK: exactly 1 claim declares 50,144 (files carrying the number: %v)", files)

	// Scenario 2: disjoint-tree path (the fork). Each claim is its own
	// declaration of the same frozen prior exposure.
	isolateAllocationRegistry(t)
	root := t.TempDir()
	dirA := filepath.Join(root, "a")
	dirB := filepath.Join(root, "b")
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mA := advManifest(advAuthRef, dirA, "alloc-A")
	mB := advManifest(advAuthRef, dirB, "alloc-B")
	aA, _, _ := preflightAllocation(mA, "sha-A", false)
	_ = claimAllocation(mA.AllocationRecordPath, aA)
	aB, _, _ := preflightAllocation(mB, "sha-B", false)
	_ = claimAllocation(mB.AllocationRecordPath, aB)
	claims2, files2 := countClaimDeclarations(root, 50_144)
	if claims2 > 1 {
		t.Fatalf("W2-R1 HOLE REPRODUCED (prior exposure double-declaration): frozen 50,144 declared by %d claims across disjoint trees; files: %v", claims2, files2)
	}
	t.Logf("disjoint trees: %d claims declare 50,144", claims2)
}
