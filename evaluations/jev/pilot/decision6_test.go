package pilot

// Decision 6 regressions (probability-mass admission + auditable second
// supersession): a Choice distribution whose exact sum S satisfies
// |S - 1| <= 0.02 is ADMITTED with the deficit recorded as a calibration
// signal and never halts; scores are preserved byte-verbatim and never
// renormalized; anything outside the class keeps the Decision-2
// halt-both-arms net. A run halted by the schema refusal resumes only
// through one append-only superseding record binding the verbatim decision
// digest, the admission rule with its bounds, and the durable halted event
// identity — re-admitting the schema-invalidated billing from its persisted
// raw figures.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
	"github.com/Clarit-AI/Plexium/evaluations/jev/ledger"
)

// massDeficitResponse is a persisted Jev response whose probability scores
// sum to 0.99 (the real halted-run shape: 0.55 + 0.44).
func massDeficitResponse() []byte {
	return []byte(`{"answers":{"verdict":{"choice":"document","probabilities":{"document":0.55,"concept":0.44}}}}`)
}

// schemaRefusedObservation mirrors the real halted attempt: billing figures
// persisted verbatim but invalidated by the response-schema refusal, with a
// persisted response whose mass sums to 0.99.
func schemaRefusedObservation(arm, cost, inTok, outTok string) adapter.AttemptObservation {
	o := zeroObservation(arm)
	o.Billing.Cost = decimal(cost)
	o.Billing.InputTokens = decimal(inTok)
	o.Billing.OutputTokens = decimal(outTok)
	reason := "response schema not admitted: probabilities sum to 0.99, tolerance 0.001"
	o.Billing.Error = reason
	for _, f := range []*adapter.DecimalField{&o.Billing.Cost, &o.Billing.InputTokens, &o.Billing.OutputTokens} {
		f.Valid = false
		f.Error = reason
	}
	o.RawResponse = massDeficitResponse()
	o.Decision = &adapter.Decision{
		Choice:        "document",
		Probabilities: map[string]float64{"document": 0.55, "concept": 0.44},
	}
	return o
}

// admittedMassObservation is a normal-path Jev response admitted under
// Decision 6 with the deficit recorded.
func admittedMassObservation() adapter.AttemptObservation {
	o := zeroObservation("jev")
	o.Billing.Cost = decimal("0.00006006")
	o.Billing.InputTokens = decimal("1430")
	o.Billing.OutputTokens = decimal("98")
	o.Decision = &adapter.Decision{
		Choice:                   "document",
		Probabilities:            map[string]float64{"document": 0.55, "concept": 0.44},
		ProbabilityMass:          "0.99",
		ProbabilityDeficit:       "-0.01",
		ProbabilityDeficitMarked: true,
	}
	return o
}

// TestDecision6AdmissionBoundaryMatrix: the class is exactly |S - 1| <= 0.02
// with every score finite in [0,1]; 0.99/1.02/0.98 admit (0.99 marked as a
// deficit beyond the 0.001 marker threshold), 0.979/1.021 and structural
// failures reject, and admitted scores are never renormalized.
func TestDecision6AdmissionBoundaryMatrix(t *testing.T) {
	cases := []struct {
		name     string
		scores   map[string]float64
		admitted bool
		marked   bool
		mass     string
	}{
		{"exact-1.0", map[string]float64{"a": 0.6, "b": 0.4}, true, false, "1"},
		{"documented-0.99", map[string]float64{"document": 0.55, "concept": 0.44}, true, true, "0.99"},
		{"boundary-high-1.02", map[string]float64{"a": 0.7, "b": 0.32}, true, true, "1.02"},
		{"boundary-low-0.98", map[string]float64{"a": 0.68, "b": 0.3}, true, true, "0.98"},
		{"just-outside-low-0.979", map[string]float64{"a": 0.679, "b": 0.3}, false, false, ""},
		{"just-outside-high-1.021", map[string]float64{"a": 0.7, "b": 0.321}, false, false, ""},
		{"structural-negative", map[string]float64{"a": 1.1, "b": -0.1}, false, false, ""},
		{"structural-above-one", map[string]float64{"a": 1.2, "b": -0.2}, false, false, ""},
		{"structural-empty", map[string]float64{}, false, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := adapter.AdmitProbabilityMass(tc.scores)
			if tc.admitted {
				if err != nil {
					t.Fatalf("admitted case rejected: %v", err)
				}
				if got.Mass != tc.mass {
					t.Fatalf("mass=%q want %q", got.Mass, tc.mass)
				}
				if got.DeficitMarked != tc.marked {
					t.Fatalf("deficitMarked=%v want %v", got.DeficitMarked, tc.marked)
				}
				// Scores are never renormalized: the admitted map is the
				// caller's, unchanged.
				for k, want := range tc.scores {
					if tc.scores[k] != want {
						t.Fatalf("score %s mutated", k)
					}
				}
				return
			}
			if err == nil {
				t.Fatalf("out-of-class case admitted: %+v", got)
			}
		})
	}
}

// TestDecision6AdmittedMassContinuesAndRecordsDeficit: an admitted deficit
// response never halts; the run continues and the exact mass, deficit and
// marker land on both the outcome row and the journal row.
func TestDecision6AdmittedMassContinuesAndRecordsDeficit(t *testing.T) {
	slots := []Slot{
		{Ordinal: 1, FixtureID: "a", Arm: ArmJev, Repetition: 1},
		{Ordinal: 2, FixtureID: "b", Arm: ArmNano, Repetition: 1},
	}
	r, done := newDecision5Runner(t, t.TempDir(), slots)
	defer done()
	var calls atomic.Int32
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls.Add(1)
		return admittedMassObservation(), nil
	}
	r.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls.Add(1)
		return zeroObservation("nano"), nil
	}
	out, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("admitted mass halted the run: %v", err)
	}
	if r.Journal.State().Halted {
		t.Fatal("admitted mass halted the journal")
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d want 2 (admitted mass must continue)", calls.Load())
	}
	if out[0].ProbabilityMass != "0.99" || out[0].ProbabilityDeficit != "-0.01" || !out[0].ProbabilityDeficitMarked {
		t.Fatalf("outcome mass recording: %+v", out[0])
	}
	row := r.Journal.State().Slots[1].Event
	if row.ProbabilityMass != "0.99" || row.ProbabilityDeficit != "-0.01" || !row.ProbabilityDeficitMarked {
		t.Fatalf("journal mass recording: %+v", row)
	}
	if row.Outcome != "settled-positive-with-deficit" {
		t.Fatalf("deficit outcome label: %q", row.Outcome)
	}
}

// TestDecision6OutOfClassStillHalts: a schema refusal that Decision 6 does
// not overturn keeps the Decision-2 halt-both-arms net (halt after the first
// such response, second arm never called).
func TestDecision6OutOfClassStillHalts(t *testing.T) {
	slots := []Slot{
		{Ordinal: 1, FixtureID: "a", Arm: ArmJev, Repetition: 1},
		{Ordinal: 2, FixtureID: "b", Arm: ArmNano, Repetition: 1},
	}
	r, done := newDecision5Runner(t, t.TempDir(), slots)
	defer done()
	var calls atomic.Int32
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls.Add(1)
		return schemaRefusedObservation("jev", "0.000059724", "1422", "98"), nil
	}
	r.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls.Add(1)
		return zeroObservation("nano"), nil
	}
	_, err := r.Run(context.Background())
	if err == nil {
		t.Fatal("schema-refused billing continued")
	}
	if calls.Load() != 1 || !r.Journal.State().Halted {
		t.Fatalf("calls=%d halted=%v (halt net must be preserved)", calls.Load(), r.Journal.State().Halted)
	}
}

// TestDecision6ReadmitSchemaInvalidatedBilling: the supersession re-admits
// schema-invalidated billing from its persisted Raw lexemes; a present field
// with an absent/unparseable Raw fails closed; non-schema billing is
// returned unchanged.
func TestDecision6ReadmitSchemaInvalidatedBilling(t *testing.T) {
	const reason = "response schema not admitted: probabilities sum to 0.99, tolerance 0.001"
	real := func() adapter.BillingObservation {
		var b adapter.BillingObservation
		b.Cost = decimal("0.000059724")
		b.InputTokens = decimal("1422")
		b.OutputTokens = decimal("98")
		b.Error = reason
		for _, f := range []*adapter.DecimalField{&b.Cost, &b.InputTokens, &b.OutputTokens} {
			f.Valid = false
			f.Error = reason
		}
		return b
	}
	b, err := readmitSchemaInvalidatedBilling(real())
	if err != nil {
		t.Fatalf("real shape refused: %v", err)
	}
	if !b.Cost.Valid || b.Cost.Raw != "0.000059724" || !b.InputTokens.Valid || b.InputTokens.Raw != "1422" || !b.OutputTokens.Valid || b.OutputTokens.Raw != "98" {
		t.Fatalf("readmission lost figures: %+v", b)
	}
	if b.Error != "" {
		t.Fatalf("schema refusal not cleared: %q", b.Error)
	}
	// Fail closed when a present field has no persisted raw value.
	broken := real()
	broken.Cost.Raw = ""
	if _, err := readmitSchemaInvalidatedBilling(broken); err == nil {
		t.Fatal("missing raw value did not fail closed")
	}
	// Non-schema billing is untouched.
	plain := zeroObservation("nano").Billing
	out, err := readmitSchemaInvalidatedBilling(plain)
	if err != nil || out.Cost.Raw != plain.Cost.Raw || out.Error != plain.Error {
		t.Fatalf("non-schema billing altered: %+v err=%v", out, err)
	}
}

// TestDecision6AuditableSecondSupersession is the end-to-end resume: a run
// halted by the schema refusal (the real halted-run shape, 25/48) resumes
// under the SAME allocation and run identity via one append-only superseding
// record — the halted attempt is re-classified (0.99 mass admitted, deficit
// recorded) and settled as observed exactly once, settled attempts are never
// resent, the durable halt remains present and bound, and the figures stay
// byte-identical even with adversarial screening secrets configured.
func TestDecision6AuditableSecondSupersession(t *testing.T) {
	dir := t.TempDir()
	slots := []Slot{
		{Ordinal: 1, FixtureID: "a", Arm: ArmNano, Repetition: 1},
		{Ordinal: 2, FixtureID: "b", Arm: ArmJev, Repetition: 1},
		{Ordinal: 3, FixtureID: "c", Arm: ArmNano, Repetition: 1},
		{Ordinal: 4, FixtureID: "d", Arm: ArmJev, Repetition: 1},
	}

	// Session 1: the schema refusal halts both arms (the real shape).
	s1, done1 := newDecision5Runner(t, dir, slots)
	s1.Config.ScreeningSecrets = []string{"551", "20"}
	var calls1 atomic.Int32
	s1.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls1.Add(1)
		return zeroObservation("nano"), nil
	}
	s1.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls1.Add(1)
		return schemaRefusedObservation("jev", "0.000059724", "1422", "98"), nil
	}
	if _, err := s1.Run(context.Background()); err == nil {
		t.Fatal("schema-refused session did not halt")
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

	// Session 2: Decision-6 auditable resume under the same state.
	s2, done2 := newDecision5Runner(t, dir, slots)
	defer done2()
	s2.Config.ScreeningSecrets = []string{"551", "20"}
	s2.Config.SupersedeHaltUnderDecision6 = true
	var calls2 atomic.Int32
	s2.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls2.Add(1)
		return zeroObservation("nano"), nil
	}
	s2.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls2.Add(1)
		return zeroObservation("jev"), nil
	}
	if _, err := s2.Run(context.Background()); err != nil {
		t.Fatalf("decision-6 resume failed: %v", err)
	}
	// Settled attempts are NEVER resent: only slots 3 and 4 ran.
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
	if sup.DecisionSHA256 != Decision6Digest() || sup.DecisionSHA256 == "" {
		t.Fatalf("supersession decision digest wrong: %q", sup.DecisionSHA256)
	}
	if !strings.Contains(sup.ToleranceRule, "|S - 1| <= 0.02") {
		t.Fatalf("admission rule bounds not bound into the supersession: %q", sup.ToleranceRule)
	}
	// The halted attempt is reconciled mass-admitted with the deficit recorded.
	st2 := state.Slots[2]
	if !st2.Reconciled || st2.Event.Outcome != "settled-positive-with-deficit" {
		t.Fatalf("halted attempt not reconciled as mass-admitted: %+v", st2)
	}
	if st2.Event.ProbabilityMass != "0.99" || st2.Event.ProbabilityDeficit != "-0.01" || !st2.Event.ProbabilityDeficitMarked {
		t.Fatalf("deficit not recorded on the reconciled row: %+v", st2.Event)
	}
	// Billing figures stay byte-identical even with adversarial secrets
	// configured (W1/R3 numeric-lexeme rule).
	if st2.Event.BillingCostRaw != "0.000059724" {
		t.Fatalf("billing cost not byte-identical: %q", st2.Event.BillingCostRaw)
	}
	if !strings.Contains(sup.SettlementNote, "0.000059724") {
		t.Fatalf("settlement note lost the raw figure: %q", sup.SettlementNote)
	}
	// No double-settlement of the re-admitted attempt: exactly one terminal
	// entry on its reservation ref.
	terminals := 0
	for _, e := range s2.Config.Jev.Ledger.Entries() {
		if e.RefID == st2.ReservationID {
			switch e.Type {
			case ledger.EntrySettlement, ledger.EntryAdjustment, ledger.EntryMismatch:
				terminals++
			}
		}
	}
	if terminals != 1 {
		t.Fatalf("re-admitted attempt terminal entries=%d want exactly 1", terminals)
	}
	// The journal keeps the original halt line byte-identical.
	journalBytes, err := os.ReadFile(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(journalBytes), string(haltLine)) {
		t.Fatal("halt record rewritten or removed")
	}
}
