package pilot

// Decision 5 regressions (tolerance rule + auditable resume): the KNOWN B1
// discrepancy class (cost vs upstream_inference_cost at the documented ~0.99
// ratio) is tolerated with conservative max-figure accounting and never
// halts; every other discrepancy or novel anomaly keeps the Decision-2
// halt-both-arms net; a halted run resumes only through one append-only
// superseding record binding the verbatim decision digest, the tolerance
// rule with its bounds, and the durable halted event identity.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
	"github.com/Clarit-AI/Plexium/evaluations/jev/ledger"
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

// newDecision5Runner mirrors newTestRunner with the manifested Nano
// economics (rateIn 100,000 / rateOut 400,000 µ$/M; bounds 4,000/256) so the
// reservation is 503 µ$ and the real observed figures fit the envelope. It
// builds over an explicit dir so a second Runner can resume the same state.
func newDecision5Runner(t *testing.T, dir string, slots []Slot) (*Runner, func()) {
	t.Helper()
	mk := func(name string, cap ledger.MicroUnit) (*ledger.Ledger, ArmBudget) {
		rateIn := ledger.MicroUnit(100_000)
		rateOut := ledger.MicroUnit(400_000)
		bounds := ledger.TokenBounds{MaxInputTokens: 4000, MaxOutputTokens: 256}
		cfg := ledger.LedgerConfig{Path: filepath.Join(dir, name+".jsonl"), AuthorizedCap: cap, RunID: "run-" + name, Model: name, ManifestKey: ledger.ManifestKey{FixtureFileSHA: "f", ProtocolVersion: protocol.ProtocolVersion, ModelPin: name, RatesVersion: "test", TokenBoundsHash: ledger.ComputeTokenBoundsHash(bounds, 0, 0, 0, 0)}, TokenBounds: bounds, RetryPolicy: ledger.ReservationRetryPolicy{}, RateIn: rateIn, RateOut: &rateOut}
		l, err := ledger.Open(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return l, ArmBudget{Ledger: l, Subcap: cap, Reservation: 503, InputBound: 4000, OutputBound: 256, RateIn: rateIn, RateOut: rateOut}
	}
	jl, jb := mk("jev", 100_000)
	nl, nb := mk("nano", 100_000)
	j, err := OpenJournal(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	payloadHash := hashBytes([]byte(`{}`))
	for i := range slots {
		slots[i].PayloadSHA = payloadHash
	}
	seenFixtures := map[string]bool{}
	entries := make([]InventoryEntry, 0, len(slots))
	for _, slot := range slots {
		if seenFixtures[slot.FixtureID] {
			continue
		}
		seenFixtures[slot.FixtureID] = true
		input := GoldFreeInput{FixtureID: slot.FixtureID, Task: protocol.TaskEntityType, SourceGroup: "sg"}
		inputBytes, _ := json.Marshal(input)
		inputHash := hashBytes(inputBytes)
		entries = append(entries, InventoryEntry{Input: input, Payloads: []Payload{{Arm: ArmJev, Body: []byte(`{}`), SHA256: payloadHash, InputHash: inputHash}, {Arm: ArmNano, Body: []byte(`{}`), SHA256: payloadHash, InputHash: inputHash}}})
	}
	inv := &Inventory{Entries: entries, Schedule: slots}
	canonical, _ := inventoryHashBytes(inv)
	inv.InventoryHash = hashBytes(canonical)
	r := &Runner{Inventory: inv, Config: ExecutionConfig{InventoryHash: inv.InventoryHash, AuthorizationRef: "test-authorization", CombinedCap: 200_000, LiveContractsVerified: true, RunLockPath: filepath.Join(dir, "run.lock"), EvidenceDir: filepath.Join(dir, "evidence"), ContractSHA: "test-contract", AllocationID: "test-allocation", AllocationSHA: "test-allocation-sha", Jev: jb, Nano: nb}, Journal: j, Attempts: map[Arm]AttemptFunc{}}
	return r, func() { _ = j.Close(); _ = jl.Close(); _ = nl.Close() }
}

// strictNoTolerance is the pre-Decision-5 policy (Decision 2 alone): an empty
// ratio envelope tolerates nothing, so a known-class discrepancy halts.
func strictNoTolerance() *RateSemanticsTolerance {
	return &RateSemanticsTolerance{RuleName: "pre-decision-5-strict", RatioMin: "2", RatioMax: "1"}
}

func discrepancyObservation(arm, cost, upstream string) adapter.AttemptObservation {
	o := zeroObservation(arm)
	o.Billing.Cost = decimal(cost)
	if upstream != "" {
		o.Billing.CostDetails.UpstreamInferenceCost = decimal(upstream)
	}
	shown := upstream
	if shown == "" {
		shown = "(absent)"
	}
	o.Billing.RateSemanticsDiscrepancy = "reported cost " + cost + " differs from upstream_inference_cost " + shown + "; tariff/currency/fee semantics unresolved"
	return o
}

// TestDecision5KnownClassToleratedSettlesConservativeMax: the known class is
// accounted at max(reported, upstream) exactly once and does NOT halt.
func TestDecision5KnownClassToleratedSettlesConservativeMax(t *testing.T) {
	r, done := newDecision5Runner(t, t.TempDir(), []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmNano, Repetition: 1}})
	defer done()
	r.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		return discrepancyObservation("nano", "0.000054549", "0.0000551"), nil
	}
	out, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("known class halted: %v", err)
	}
	if r.Journal.State().Halted {
		t.Fatal("known class halted the journal")
	}
	// Conservative max accounting: 0.0000551 -> 56 µ$ (reported would be 55).
	if got := r.Config.Nano.Ledger.Balance(); got != 56 {
		t.Fatalf("accounted %d µ$, want conservative max 56", got)
	}
	if len(out) != 1 {
		t.Fatalf("outcomes=%d", len(out))
	}
	if out[0].CostMicrodollars != 56 || out[0].BillingRaw != "0.000054549" {
		t.Fatalf("outcome accounting: %+v", out[0])
	}
	for _, figure := range []string{"0.000054549", "0.0000551"} {
		if !strings.Contains(out[0].RateTolerance, figure) {
			t.Fatalf("figure %s not recorded verbatim on the outcome row: %q", figure, out[0].RateTolerance)
		}
	}
	if r.Journal.State().Slots[1].Event.RateTolerance == "" {
		t.Fatal("discrepancy not recorded on the journal outcome row")
	}
}

// TestDecision5ToleratedRunContinuesToNextSlots: tolerance is not a halt —
// the run continues normally after a tolerated discrepancy.
func TestDecision5ToleratedRunContinuesToNextSlots(t *testing.T) {
	r, done := newDecision5Runner(t, t.TempDir(), []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmNano, Repetition: 1}, {Ordinal: 2, FixtureID: "x", Arm: ArmJev, Repetition: 1}})
	defer done()
	var calls atomic.Int32
	r.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls.Add(1)
		return discrepancyObservation("nano", "0.000054549", "0.0000551"), nil
	}
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls.Add(1)
		return zeroObservation("jev"), nil
	}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatalf("tolerated discrepancy stopped the run: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d want 2 (run must continue)", calls.Load())
	}
}

// TestDecision5BoundaryMatrix: the class is narrow — only exact-0.99-class
// figure pairs inside the pinned [0.985, 0.995] envelope with both figures
// positive and no magnitude shock are tolerated.
func TestDecision5BoundaryMatrix(t *testing.T) {
	rule := Decision5Tolerance()
	cases := []struct {
		name        string
		cost, up    string
		reservation ledger.MicroUnit
		tolerated   bool
	}{
		{"documented-0.99", "0.000054549", "0.0000551", 503, true},
		{"just-inside-low-0.98512", "0.00005428", "0.0000551", 503, true},
		{"just-inside-high-0.99492", "0.00005482", "0.0000551", 503, true},
		{"just-outside-low-0.98475", "0.00005426", "0.0000551", 503, false},
		{"just-outside-high-0.99528", "0.00005484", "0.0000551", 503, false},
		{"missing-counterpart", "0.000054549", "", 503, false},
		{"missing-reported", "", "0.0000551", 503, false},
		{"zero-figure", "0", "0.0000551", 503, false},
		{"negative-figure", "-0.00005", "0.0000551", 503, false},
		{"absurd-ratio-low", "0.00002", "0.0000551", 503, false},
		{"absurd-ratio-high", "0.0002", "0.0000551", 503, false},
		{"magnitude-shock", "0.002", "0.00202", 503, false},
		{"no-discrepancy-no-tolerance-needed", "0.000054549", "0.0000551", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obs := discrepancyObservation("nano", tc.cost, tc.up)
			if tc.name == "no-discrepancy-no-tolerance-needed" {
				obs.Billing.RateSemanticsDiscrepancy = ""
			}
			v := rateToleranceVerdict(obs.Billing, tc.reservation, rule)
			if v.Tolerated != tc.tolerated {
				t.Fatalf("tolerated=%v want %v (detail %q)", v.Tolerated, tc.tolerated, v.Detail)
			}
			if tc.tolerated && v.Conservative != 56 {
				t.Fatalf("conservative=%d want 56", v.Conservative)
			}
		})
	}
}

// TestDecision5OutOfClassHaltsBothArms: out-of-class discrepancies keep the
// unchanged Decision-2 halt-both-arms path (halt after the first such
// response, reservations retained, never a normal continue).
func TestDecision5OutOfClassHaltsBothArms(t *testing.T) {
	cases := []struct {
		name     string
		cost, up string
	}{
		{"ratio-just-outside-low", "0.00005426", "0.0000551"},
		{"missing-counterpart", "0.000054549", ""},
		{"zero-figure", "0", "0.0000551"},
		{"absurd-ratio", "0.0002", "0.0000551"},
		{"magnitude-shock", "0.002", "0.00202"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			slots := []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmNano, Repetition: 1}, {Ordinal: 2, FixtureID: "x", Arm: ArmJev, Repetition: 1}}
			r, done := newDecision5Runner(t, t.TempDir(), slots)
			defer done()
			var calls atomic.Int32
			r.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
				calls.Add(1)
				return discrepancyObservation("nano", tc.cost, tc.up), nil
			}
			r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
				calls.Add(1)
				return zeroObservation("jev"), nil
			}
			_, err := r.Run(context.Background())
			if err == nil {
				t.Fatal("out-of-class discrepancy continued")
			}
			if calls.Load() != 1 || !r.Journal.State().Halted {
				t.Fatalf("calls=%d halted=%v", calls.Load(), r.Journal.State().Halted)
			}
		})
	}
}

// TestDecision5AuditableResumeAfterToleratedHalt is the end-to-end resume:
// a run halted by the known class (settled at the reported figure under the
// pre-Decision-5 rule) resumes the remaining slots under the SAME allocation
// and run identity via one append-only superseding record — settled attempts
// are never resent, attempt 2 is never double-settled, the durable halt
// remains present and bound, and the conservative top-up is documented as
// impermissible under accepted ledger semantics.
func TestDecision5AuditableResumeAfterToleratedHalt(t *testing.T) {
	dir := t.TempDir()
	slots := []Slot{
		{Ordinal: 1, FixtureID: "a", Arm: ArmJev, Repetition: 1},
		{Ordinal: 2, FixtureID: "b", Arm: ArmNano, Repetition: 1},
		{Ordinal: 3, FixtureID: "c", Arm: ArmJev, Repetition: 1},
		{Ordinal: 4, FixtureID: "d", Arm: ArmNano, Repetition: 1},
	}

	// Session 1: pre-Decision-5 rule — the known class halts (the real
	// halted-run shape: attempt 2 settled at the reported figure first).
	s1, done1 := newDecision5Runner(t, dir, slots)
	s1.Config.RateTolerance = strictNoTolerance()
	var calls1 atomic.Int32
	s1.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls1.Add(1)
		return zeroObservation("jev"), nil
	}
	s1.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls1.Add(1)
		return discrepancyObservation("nano", "0.000054549", "0.0000551"), nil
	}
	if _, err := s1.Run(context.Background()); err == nil {
		t.Fatal("strict session did not halt")
	} else {
		t.Logf("session-1 stopped: %v", err)
	}
	if calls1.Load() != 2 {
		t.Fatalf("session-1 calls=%d want 2", calls1.Load())
	}
	done1()

	haltSeq, haltSHA, haltLine := journalHaltIdentity(t, filepath.Join(dir, "journal.jsonl"))
	if haltSeq == 0 || haltSHA == "" {
		t.Fatal("halt identity missing")
	}

	// Session 2: Decision-5 auditable resume under the same state.
	s2, done2 := newDecision5Runner(t, dir, slots)
	defer done2()
	s2.Config.SupersedeHaltUnderDecision5 = true
	var calls2 atomic.Int32
	s2.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls2.Add(1)
		return zeroObservation("jev"), nil
	}
	s2.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls2.Add(1)
		return zeroObservation("nano"), nil
	}
	if _, err := s2.Run(context.Background()); err != nil {
		t.Fatalf("decision-5 resume failed: %v", err)
	}
	// Settled attempts 1 and 2 are NEVER resent: only slots 3 and 4 ran.
	if calls2.Load() != 2 {
		t.Fatalf("session-2 calls=%d want 2 (slots 3-4 only)", calls2.Load())
	}
	state := s2.Journal.State()
	sup, superseded := state.Supersessions[haltSeq]
	if !superseded {
		t.Fatal("superseding record missing")
	}
	// The durable halt event remains present and bound.
	if !state.Halted || state.HaltSequence != haltSeq || state.HaltSHA256 != haltSHA {
		t.Fatalf("halt identity drifted: %+v", state)
	}
	if sup.SupersededEventSequence != haltSeq || sup.SupersededEventSHA256 != haltSHA {
		t.Fatalf("supersession does not bind the halted event: %+v", sup)
	}
	if sup.DecisionSHA256 != Decision5Digest() || sup.DecisionSHA256 == "" {
		t.Fatalf("supersession decision digest wrong: %q", sup.DecisionSHA256)
	}
	if !strings.Contains(sup.ToleranceRule, "[0.985, 0.995]") {
		t.Fatalf("tolerance rule bounds not bound into the supersession: %q", sup.ToleranceRule)
	}
	// Attempt-2 conservative top-up documented as impermissible (+1 µ$:
	// 56 conservative vs 55 settled).
	if !strings.Contains(sup.TopUpNote, "top-up +1 microdollars") || !strings.Contains(sup.TopUpNote, "NOT applied") {
		t.Fatalf("top-up treatment not documented: %q", sup.TopUpNote)
	}
	// Slot 2 is reconciled (tolerated) with the figures verbatim.
	st2 := state.Slots[2]
	if !st2.Reconciled || st2.Event.Outcome != "settled-positive" || st2.Event.RateTolerance == "" {
		t.Fatalf("slot 2 not reconciled as tolerated: %+v", st2)
	}
	for _, figure := range []string{"0.000054549", "0.0000551"} {
		if !strings.Contains(st2.Event.RateTolerance, figure) {
			t.Fatalf("figure %s not byte-identical on the reconciled row: %q", figure, st2.Event.RateTolerance)
		}
	}
	// No double-settlement of attempt 2: exactly one terminal entry on the ref.
	terminals := 0
	for _, e := range s2.Config.Nano.Ledger.Entries() {
		if e.RefID == st2.ReservationID {
			switch e.Type {
			case ledger.EntrySettlement, ledger.EntryAdjustment, ledger.EntryMismatch:
				terminals++
			}
		}
	}
	if terminals != 1 {
		t.Fatalf("attempt 2 terminal entries=%d want exactly 1 (no double-settle)", terminals)
	}
	// Replayed report rows account the conservative max for the tolerated row.
	rows, err := ReplayOutcomes(s2.Inventory, state, s2.Config.EvidenceDir)
	if err != nil {
		t.Fatal(err)
	}
	if rows[1].CostMicrodollars != 56 || !rows[1].KnownCost {
		t.Fatalf("replayed tolerated row accounting: %+v", rows[1])
	}
	// The journal keeps the original halt line byte-identical.
	journalBytes, err := os.ReadFile(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(journalBytes), string(haltLine)) {
		t.Fatal("halt record rewritten or removed")
	}
	_ = journalBytes
}

// journalHaltIdentity locates the durable halt event and returns its
// sequence, line hash, and raw line.
func journalHaltIdentity(t *testing.T, path string) (int64, string, []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var e JournalEvent
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatal(err)
		}
		if e.Type == EventHalt {
			return e.Sequence, hashBytes(line), append([]byte(nil), line...)
		}
	}
	return 0, "", nil
}

// TestDecision5NumericFidelityInDiscrepancyRows: W1/R3 screening and numeric
// fidelity are unaffected — the figures survive byte-identical in the
// discrepancy rows even with a secret that overlaps a figure suffix.
func TestDecision5NumericFidelityInDiscrepancyRows(t *testing.T) {
	r, done := newDecision5Runner(t, t.TempDir(), []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmNano, Repetition: 1}})
	defer done()
	r.Config.ScreeningSecrets = []string{"551", "20"}
	r.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		return discrepancyObservation("nano", "0.000054549", "0.0000551"), nil
	}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	journalBytes, err := os.ReadFile(r.Journal.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, figure := range []string{"0.000054549", "0.0000551"} {
		if !strings.Contains(string(journalBytes), figure) {
			t.Fatalf("figure %s not byte-identical in the journal discrepancy rows: %s", figure, journalBytes)
		}
	}
	row := r.Journal.State().Slots[1].Event.RateTolerance
	if strings.Contains(row, redactedCredential) {
		t.Fatalf("numeric lexeme corrupted in the tolerance row: %q", row)
	}
	for _, figure := range []string{"0.000054549", "0.0000551"} {
		if !strings.Contains(row, figure) {
			t.Fatalf("figure %s not byte-identical in the tolerance row: %q", figure, row)
		}
	}
}
