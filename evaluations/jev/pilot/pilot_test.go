package pilot

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
	"github.com/Clarit-AI/Plexium/evaluations/jev/ledger"
	"github.com/Clarit-AI/Plexium/evaluations/jev/loader"
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

func TestBuildInventoryIsGoldFreeAndDeterministic(t *testing.T) {
	inv, err := BuildInventory("../review-pilot/fixtures.jsonl", "../review-pilot/fixtures.manifest.json", BuildConfig{
		JevRequestModel: "typesafe/jev-1.13", NanoRequestModel: "openai/gpt-4.1-nano", NanoProvider: "OpenAI",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Entries) != 24 || len(inv.Schedule) != 48 {
		t.Fatalf("counts = %d entries, %d slots", len(inv.Entries), len(inv.Schedule))
	}
	b, err := MarshalInventory(inv)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"expectedLabel", "rationaleEvidence", "supportingSpans", "reviewStatus", "challengeCategories", "candidateGeneration"} {
		if strings.Contains(string(b), `"`+forbidden+`"`) {
			t.Fatalf("inventory leaks gold field %q", forbidden)
		}
	}
	inv2, err := BuildInventory("../review-pilot/fixtures.jsonl", "../review-pilot/fixtures.manifest.json", BuildConfig{
		JevRequestModel: "typesafe/jev-1.13", NanoRequestModel: "openai/gpt-4.1-nano", NanoProvider: "OpenAI",
	})
	if err != nil {
		t.Fatal(err)
	}
	if inv.InventoryHash != inv2.InventoryHash {
		t.Fatalf("nondeterministic inventory hashes %s != %s", inv.InventoryHash, inv2.InventoryHash)
	}
	frozen, err := os.ReadFile("request-inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	generated, err := MarshalInventory(inv)
	if err != nil {
		t.Fatal(err)
	}
	generated = append(generated, '\n')
	if !bytes.Equal(frozen, generated) {
		t.Fatal("checked-in request inventory is not the deterministic build output")
	}
	seen := map[string]int{}
	for _, s := range inv.Schedule {
		seen[s.FixtureID]++
	}
	for id, n := range seen {
		if n != 2 {
			t.Fatalf("fixture %s scheduled %d times", id, n)
		}
	}
}

func TestInventoryMarshalReloadValidateRoundTrip(t *testing.T) {
	inv, err := BuildInventory("../review-pilot/fixtures.jsonl", "../review-pilot/fixtures.manifest.json", BuildConfig{JevRequestModel: "typesafe/jev-1.13", NanoRequestModel: "openai/gpt-4.1-nano", NanoProvider: "OpenAI"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := MarshalInventory(inv)
	if err != nil {
		t.Fatal(err)
	}
	var reloaded Inventory
	if err := json.Unmarshal(b, &reloaded); err != nil {
		t.Fatal(err)
	}
	if err := ValidateInventory(&reloaded); err != nil {
		t.Fatalf("prepare/write/reload inventory rejected itself: %v", err)
	}
	reloaded.Entries[0].Payloads[0].Body[0] ^= 1
	if err := ValidateInventory(&reloaded); err == nil {
		t.Fatal("payload byte mutation accepted")
	}
}

func TestGoldMetadataMutationCannotChangeProjectedInput(t *testing.T) {
	loaded, err := loader.Load("../review-pilot/fixtures.jsonl", "../review-pilot/fixtures.manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	original := loaded.Fixtures[0]
	mutated := original
	mutated.ExpectedLabel = "canary"
	mutated.Rationale = "canary"
	mutated.RationaleEvidence = "canary"
	mutated.SupportingSpans = nil
	mutated.Reviewer = "canary"
	mutated.ReviewStatus = protocol.ReviewDisputed
	mutated.Author = "canary"
	mutated.ChallengeCategories = nil
	mutated.CandidateGeneration = "canary"
	mutated.CandidateSource = "canary"
	if !reflect.DeepEqual(projectInput(original), projectInput(mutated)) {
		t.Fatal("gold/review metadata changed live input projection")
	}
}

func TestJournalReplayReconstructsStateAndRejectsTornTail(t *testing.T) {
	p := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := OpenJournal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.BindRun(RunBinding{InventoryHash: "ih", AuthorizationRef: "auth", CombinedCap: 20, ContractSHA: "contract"}); err != nil {
		t.Fatal(err)
	}
	base := JournalEvent{SlotOrdinal: 1, FixtureID: "x", Arm: ArmJev, PayloadSHA: "abc"}
	for _, e := range []JournalEvent{withType(base, EventIntent), withType(base, EventReserved), withType(base, EventSend), withType(base, EventObserved), withType(base, EventReconciled)} {
		if e.Type == EventReserved || e.Type == EventReconciled {
			e.ReservationID = "res-1"
		}
		if err := j.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := ReplayJournal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Slots[1].Reconciled || s.Sequence != 6 {
		t.Fatalf("bad replay: %+v", s)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"sequence":7`)
	_ = f.Close()
	if _, err := ReplayJournal(p); err == nil {
		t.Fatal("torn journal tail accepted")
	}
}

func TestJournalRejectsSlotIdentityMutation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := OpenJournal(p)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if err := j.BindRun(RunBinding{InventoryHash: "ih", AuthorizationRef: "auth", CombinedCap: 20, ContractSHA: "contract"}); err != nil {
		t.Fatal(err)
	}
	base := JournalEvent{Type: EventIntent, SlotOrdinal: 1, FixtureID: "x", Arm: ArmJev, PayloadSHA: "p"}
	if err := j.Append(base); err != nil {
		t.Fatal(err)
	}
	changed := JournalEvent{Type: EventReserved, SlotOrdinal: 1, FixtureID: "other", Arm: ArmJev, PayloadSHA: "p", ReservationID: "r", Reserved: 1}
	if err := j.Append(changed); err == nil {
		t.Fatal("journal accepted changed slot identity")
	}
}

func TestRestartAfterSendRefusesResendAndPreservesReservation(t *testing.T) {
	r, closeAll := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev, Repetition: 1, PayloadSHA: "p"}}, 20, 10, 10)
	defer closeAll()
	b := r.Config.Jev
	if err := r.Journal.BindRun(RunBinding{InventoryHash: r.Config.InventoryHash, AuthorizationRef: r.Config.AuthorizationRef, CombinedCap: int64(r.Config.CombinedCap), ContractSHA: r.Config.ContractSHA}); err != nil {
		t.Fatal(err)
	}
	base := JournalEvent{SlotOrdinal: 1, FixtureID: "x", Arm: ArmJev, PayloadSHA: r.Inventory.Schedule[0].PayloadSHA}
	if err := r.Journal.Append(withType(base, EventIntent)); err != nil {
		t.Fatal(err)
	}
	ref, res, err := b.Ledger.Reserve(context.Background(), 1, "entity-type", "sg", b.InputBound, b.OutputBound)
	if err != nil {
		t.Fatal(err)
	}
	re := withType(base, EventReserved)
	re.ReservationID = ref
	re.Reserved = int64(res)
	if err := r.Journal.Append(re); err != nil {
		t.Fatal(err)
	}
	if err := r.Journal.Append(withType(base, EventSend)); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls.Add(1)
		return adapter.AttemptObservation{}, nil
	}
	_, err = r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "never resend") {
		t.Fatalf("Run error=%v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("resend count=%d", calls.Load())
	}
	if b.Ledger.Balance() != res {
		t.Fatalf("reservation reset: balance=%d want=%d", b.Ledger.Balance(), res)
	}
}

func TestOrphanLedgerReservationRefusesAnySend(t *testing.T) {
	r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev}, {Ordinal: 2, FixtureID: "x", Arm: ArmNano}}, 20, 10, 10)
	defer done()
	if _, _, err := r.Config.Jev.Ledger.Reserve(context.Background(), 1, "entity-type", "sg", 1, 1); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls.Add(1)
		return zeroObservation("jev"), nil
	}
	r.Attempts[ArmNano] = r.Attempts[ArmJev]
	if _, err := r.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "orphan") {
		t.Fatalf("orphan reservation admitted: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("sent %d calls with orphan reservation", calls.Load())
	}
	if !r.Journal.State().Halted {
		t.Fatal("orphan reservation did not durably halt journal")
	}
}

func TestUsageBoundsHaltIndependentOfCost(t *testing.T) {
	for _, tc := range []struct {
		name          string
		cost          string
		input, output string
	}{{"zero-input", "0", "999", "1"}, {"zero-output", "0", "1", "999"}, {"positive-input", "0.000001", "999", "1"}, {"positive-output", "0.000001", "1", "999"}} {
		t.Run(tc.name, func(t *testing.T) {
			r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev}, {Ordinal: 2, FixtureID: "x", Arm: ArmNano}}, 20, 10, 10)
			defer done()
			var calls atomic.Int32
			r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
				calls.Add(1)
				o := zeroObservation("jev")
				o.Billing.Cost = decimal(tc.cost)
				o.Billing.InputTokens = decimal(tc.input)
				o.Billing.OutputTokens = decimal(tc.output)
				return o, nil
			}
			r.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
				calls.Add(1)
				return zeroObservation("nano"), nil
			}
			_, err := r.Run(context.Background())
			if err == nil || !strings.Contains(err.Error(), "usage exceeds bounds") {
				t.Fatalf("bound violation admitted: %v", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("calls=%d, want 1", calls.Load())
			}
			if r.Config.Jev.Ledger.Balance() != r.Config.Jev.Reservation {
				t.Fatal("bound violation released reservation")
			}
		})
	}
}

func TestModelPinFailureWithValidBillingHaltsBothArms(t *testing.T) {
	for _, cost := range []string{"0", "0.000001"} {
		t.Run(cost, func(t *testing.T) {
			r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev}, {Ordinal: 2, FixtureID: "x", Arm: ArmNano}}, 20, 10, 10)
			defer done()
			var calls atomic.Int32
			r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
				calls.Add(1)
				o := zeroObservation("jev")
				o.Billing.Cost = decimal(cost)
				o.Decision = nil
				return o, &adapter.TransportError{Code: "model-pin", Message: "wrong pin"}
			}
			r.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
				calls.Add(1)
				return zeroObservation("nano"), nil
			}
			_, err := r.Run(context.Background())
			if err == nil || !strings.Contains(err.Error(), "adapter contract failure") {
				t.Fatalf("pin failure continued: %v", err)
			}
			if calls.Load() != 1 || !r.Journal.State().Halted {
				t.Fatalf("calls=%d halted=%t", calls.Load(), r.Journal.State().Halted)
			}
			want := r.Config.Jev.Reservation
			if cost != "0" {
				want = 1
			}
			if r.Config.Jev.Ledger.Balance() != want {
				t.Fatalf("balance=%d want=%d", r.Config.Jev.Ledger.Balance(), want)
			}
		})
	}
}

func TestPartialReadEvidenceSurvivesHaltAndReplay(t *testing.T) {
	r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev}}, 20, 10, 10)
	defer done()
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		o := zeroObservation("jev")
		o.RawResponse = []byte(`{"id":`)
		o.ReadError = "unexpected EOF"
		o.Billing.Error = "response body incomplete"
		o.Decision = nil
		return o, &adapter.TransportError{Code: "transport", Message: "response body read failed"}
	}
	if _, err := r.Run(context.Background()); err == nil {
		t.Fatal("partial read accepted")
	}
	state, err := ReplayJournal(r.Journal.path)
	if err != nil {
		t.Fatal(err)
	}
	out, err := ReplayOutcomes(r.Inventory, state, r.Config.EvidenceDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Observation == nil || out[0].Observation.ReadError != "unexpected EOF" || out[0].Observation.Billing.Error == "" {
		t.Fatalf("partial read evidence lost: %+v", out)
	}
}

func TestResumeRejectsContractDriftAndMissingEvidence(t *testing.T) {
	for _, kind := range []string{"authorization", "combined-cap", "contract", "raw", "observation"} {
		t.Run(kind, func(t *testing.T) {
			r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev}}, 20, 10, 10)
			defer done()
			r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) { return zeroObservation("jev"), nil }
			if _, err := r.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "authorization":
				r.Config.AuthorizationRef = "changed"
			case "combined-cap":
				r.Config.CombinedCap = 21
			case "contract":
				r.Config.ContractSHA = "changed"
			case "raw":
				files, _ := filepath.Glob(filepath.Join(r.Config.EvidenceDir, "*.response"))
				if len(files) != 1 {
					t.Fatalf("raw files=%v", files)
				}
				if err := os.Remove(files[0]); err != nil {
					t.Fatal(err)
				}
			case "observation":
				files, _ := filepath.Glob(filepath.Join(r.Config.EvidenceDir, "*.observation.json"))
				if len(files) != 1 {
					t.Fatalf("observation files=%v", files)
				}
				if err := os.Remove(files[0]); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := r.Run(context.Background()); err == nil {
				t.Fatalf("resume accepted %s drift", kind)
			}
		})
	}
}

func TestObservationEvidenceRoundTripAndProjection(t *testing.T) {
	r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev}}, 20, 10, 10)
	defer done()
	confidence := 0.75
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		o := zeroObservation("jev")
		o.RequestID = "req-1"
		o.ResponseModel = "pin"
		o.ResponseProvider = "P"
		o.ResponseHeaders = map[string]string{"X-Request-Id": "req-1"}
		o.Duration = 12 * time.Millisecond
		o.StartedAt = time.Unix(10, 0)
		o.EndedAt = o.StartedAt.Add(o.Duration)
		o.Decision.Confidence = &confidence
		o.Decision.Probabilities = map[string]float64{"document": 1}
		return o, nil
	}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := ReplayJournal(r.Journal.path)
	if err != nil {
		t.Fatal(err)
	}
	out, err := ReplayOutcomes(r.Inventory, state, r.Config.EvidenceDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Observation == nil || out[0].Observation.RequestID != "req-1" || out[0].Observation.Billing.Cost.Raw != "0" {
		t.Fatalf("evidence=%+v", out)
	}
	fixtures := []protocol.Fixture{{ID: "x", Task: protocol.TaskEntityType, SourceGroup: "sg", Split: protocol.SplitTuning, ExpectedLabel: "document"}}
	preds, err := Project(fixtures, out, ArmJev)
	if err != nil {
		t.Fatal(err)
	}
	p := preds[0]
	if p.Attempts != 1 || p.Confidence == nil || *p.Confidence != confidence || p.CostUSD == nil || *p.CostUSD != 0 || p.LatencyMS == nil || *p.LatencyMS != 12 {
		t.Fatalf("prediction lost evidence: %+v", p)
	}
}

func TestScoringCorpusMustMatchInventoryDigests(t *testing.T) {
	inv, err := BuildInventory("../review-pilot/fixtures.jsonl", "../review-pilot/fixtures.manifest.json", BuildConfig{JevRequestModel: "j", NanoRequestModel: "n", NanoProvider: "P"})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loader.Load("../review-pilot/fixtures.jsonl", "../review-pilot/fixtures.manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateScoringCorpus(inv, loaded, "../review-pilot/fixtures.manifest.json"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "fixtures.jsonl")
	manifestPath := filepath.Join(dir, "fixtures.manifest.json")
	f, err := os.OpenFile(fixturePath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for i, item := range loaded.Fixtures {
		if i == 0 {
			for _, label := range item.AllowedLabels {
				if label != item.ExpectedLabel {
					item.ExpectedLabel = label
					break
				}
			}
		}
		if err := enc.Encode(item); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	m, err := loader.BuildManifest(fixturePath, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.WriteManifest(manifestPath, m); err != nil {
		t.Fatal(err)
	}
	altered, err := loader.Load(fixturePath, manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateScoringCorpus(inv, altered, manifestPath); err == nil {
		t.Fatal("internally valid changed-label corpus accepted")
	}
}

func TestSubcapStopsOnlyOneArm(t *testing.T) {
	slots := []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev, PayloadSHA: "p"}, {Ordinal: 2, FixtureID: "x", Arm: ArmNano, PayloadSHA: "p"}}
	r, closeAll := newTestRunner(t, slots, 11, 1, 10)
	defer closeAll()
	var jev, nano atomic.Int32
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		jev.Add(1)
		return zeroObservation("jev"), nil
	}
	r.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		nano.Add(1)
		return zeroObservation("nano"), nil
	}
	out, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if jev.Load() != 0 || nano.Load() != 1 {
		t.Fatalf("calls jev=%d nano=%d", jev.Load(), nano.Load())
	}
	if len(out) != 2 || out[0].Error != "arm-subcap-exhausted" || out[1].Billing != "known-zero-reservation-retained" {
		t.Fatalf("outcomes=%+v", out)
	}
	if r.Config.Nano.Ledger.Balance() != r.Config.Nano.Reservation {
		t.Fatalf("zero billing released reservation: balance=%d", r.Config.Nano.Ledger.Balance())
	}
}

func TestSharedCapStopsBothBeforeSend(t *testing.T) {
	slots := []Slot{
		{Ordinal: 1, FixtureID: "seed-a", Arm: ArmNano},
		{Ordinal: 2, FixtureID: "seed-b", Arm: ArmNano},
		{Ordinal: 3, FixtureID: "target", Arm: ArmJev},
	}
	r, closeAll := newTestRunner(t, slots, 4, 2, 2)
	defer closeAll()
	if err := r.Journal.BindRun(RunBinding{InventoryHash: r.Config.InventoryHash, AuthorizationRef: r.Config.AuthorizationRef, CombinedCap: int64(r.Config.CombinedCap), ContractSHA: r.Config.ContractSHA}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		slot := r.Inventory.Schedule[i]
		ordinal := slot.Ordinal
		base := JournalEvent{SlotOrdinal: ordinal, FixtureID: slot.FixtureID, Arm: slot.Arm, PayloadSHA: slot.PayloadSHA}
		if err := r.Journal.Append(withType(base, EventIntent)); err != nil {
			t.Fatal(err)
		}
		ref, res, err := r.Config.Nano.Ledger.Reserve(context.Background(), ordinal, "entity-type", "sg", 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		re := withType(base, EventReserved)
		re.ReservationID = ref
		re.Reserved = int64(res)
		if err := r.Journal.Append(re); err != nil {
			t.Fatal(err)
		}
		if err := r.Journal.Append(withType(base, EventSend)); err != nil {
			t.Fatal(err)
		}
		obs := zeroObservation("nano")
		rp, rh, op, oh, err := persistAttemptEvidence(r.Config.EvidenceDir, slot, obs)
		if err != nil {
			t.Fatal(err)
		}
		oe := withType(base, EventObserved)
		oe.ResponsePath = rp
		oe.ResponseSHA = rh
		oe.ObservationPath = op
		oe.ObservationSHA = oh
		oe.RequestSent = true
		oe.ResponseSeen = true
		oe.BillingCostRaw = "0"
		if err := r.Journal.Append(oe); err != nil {
			t.Fatal(err)
		}
		final := withType(base, EventReconciled)
		final.ReservationID = ref
		final.Outcome = "zero-known-reservation-retained"
		final.BillingCostRaw = "0"
		if err := r.Journal.Append(final); err != nil {
			t.Fatal(err)
		}
	}
	var calls atomic.Int32
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls.Add(1)
		return zeroObservation("jev"), nil
	}
	_, err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "shared-cap-exhausted") {
		t.Fatalf("error=%v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("request sent after shared cap exhausted")
	}
}

func TestMutatedJournalOrdinalRefusesBeforeResend(t *testing.T) {
	r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev}}, 20, 10, 10)
	defer done()
	var calls atomic.Int32
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls.Add(1)
		return zeroObservation("jev"), nil
	}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := r.Journal.path
	if err := r.Journal.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rewritten bytes.Buffer
	for _, line := range bytes.Split(bytes.TrimSpace(b), []byte("\n")) {
		var event JournalEvent
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		if event.SlotOrdinal == 1 {
			event.SlotOrdinal = 99
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		rewritten.Write(encoded)
		rewritten.WriteByte('\n')
	}
	if err := os.WriteFile(path, rewritten.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	r.Journal = j
	if _, err := r.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "absent from frozen inventory") {
		t.Fatalf("mutated ordinal admitted: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("mutated ordinal caused resend: calls=%d", calls.Load())
	}
}

func TestJournalReservationMustMatchLedgerAttemptIdentity(t *testing.T) {
	r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev}}, 20, 10, 10)
	defer done()
	if err := r.Journal.BindRun(RunBinding{InventoryHash: r.Config.InventoryHash, AuthorizationRef: r.Config.AuthorizationRef, CombinedCap: int64(r.Config.CombinedCap), ContractSHA: r.Config.ContractSHA}); err != nil {
		t.Fatal(err)
	}
	slot := r.Inventory.Schedule[0]
	base := JournalEvent{SlotOrdinal: slot.Ordinal, FixtureID: slot.FixtureID, Arm: slot.Arm, PayloadSHA: slot.PayloadSHA}
	if err := r.Journal.Append(withType(base, EventIntent)); err != nil {
		t.Fatal(err)
	}
	ref, reserved, err := r.Config.Jev.Ledger.Reserve(context.Background(), 99, "entity-type", "sg", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	event := withType(base, EventReserved)
	event.ReservationID = ref
	event.Reserved = int64(reserved)
	if err := r.Journal.Append(event); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		calls.Add(1)
		return zeroObservation("jev"), nil
	}
	if _, err := r.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "ledger reservation identity mismatch") {
		t.Fatalf("mismatched ledger attempt admitted: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("mismatched ledger identity caused send: %d", calls.Load())
	}
}

func TestReplayInvalidCostIsEvidenceNotKnownSpend(t *testing.T) {
	r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev}}, 20, 10, 10)
	defer done()
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		o := zeroObservation("jev")
		o.Billing.Cost = decimal("0.005")
		o.Billing.Cost.Valid = false
		o.Billing.Error = "response schema not admitted"
		o.Decision = nil
		return o, &adapter.TransportError{Code: "schema", Message: "rejected"}
	}
	if _, err := r.Run(context.Background()); err == nil {
		t.Fatal("invalid billing did not halt")
	}
	out, err := ReplayOutcomes(r.Inventory, r.Journal.State(), r.Config.EvidenceDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].KnownCost || out[0].CostMicrodollars != 0 || out[0].Observation == nil || out[0].Observation.Billing.Cost.Raw != "0.005" || out[0].Observation.Billing.Cost.Valid {
		t.Fatalf("invalid billing misclassified or discarded: %+v", out)
	}
}

func TestPositiveOverrunReconcilesLedgerBeforeHalt(t *testing.T) {
	for _, tokens := range []string{"1", "999"} {
		t.Run(tokens, func(t *testing.T) {
			r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev}}, 20, 10, 10)
			defer done()
			r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
				o := zeroObservation("jev")
				o.Billing.Cost = decimal("0.000009")
				o.Billing.InputTokens = decimal(tokens)
				return o, nil
			}
			out, err := r.Run(context.Background())
			if err == nil {
				t.Fatal("positive overrun admitted")
			}
			if r.Config.Jev.Ledger.Balance() != 9 {
				t.Fatalf("ledger balance=%d want 9", r.Config.Jev.Ledger.Balance())
			}
			entries := r.Config.Jev.Ledger.Entries()
			last := entries[len(entries)-1]
			if (tokens == "999" && last.Type != ledger.EntryMismatch) || (tokens == "1" && last.Type != ledger.EntryAdjustment) {
				t.Fatalf("overrun anomaly missing: %+v", last)
			}
			if len(out) != 1 || !out[0].KnownCost || out[0].CostMicrodollars != 9 {
				t.Fatalf("known billed overrun lost: %+v", out)
			}
		})
	}
}

func TestMissingBillingHaltsBothArms(t *testing.T) {
	slots := []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev, PayloadSHA: "p"}, {Ordinal: 2, FixtureID: "x", Arm: ArmNano, PayloadSHA: "p"}}
	r, closeAll := newTestRunner(t, slots, 20, 10, 10)
	defer closeAll()
	var second atomic.Int32
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		return adapter.AttemptObservation{RequestSent: true, ResponseReceived: true, Decision: &adapter.Decision{Choice: "document"}}, nil
	}
	r.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		second.Add(1)
		return zeroObservation("nano"), nil
	}
	_, err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "billing") {
		t.Fatalf("error=%v", err)
	}
	if second.Load() != 0 {
		t.Fatal("second arm sent after billing halt")
	}
	if !r.Journal.State().Halted {
		t.Fatal("halt not durable")
	}
	if r.Config.Jev.Ledger.Balance() != r.Config.Jev.Reservation {
		t.Fatalf("missing billing released reservation: balance=%d", r.Config.Jev.Ledger.Balance())
	}
}

func TestRunnerUsesAcceptedOneShotAdapterOverHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","model":"jev-pin","answers":{"verdict":{"type":"choice","choice":"document","confidence":0.8,"probabilities":{"document":1}}},"usage":{"input_tokens":1,"output_tokens":1,"cost":0}}`))
	}))
	defer server.Close()
	input := GoldFreeInput{FixtureID: "x", Task: protocol.TaskEntityType, SourceGroup: "sg", Question: "type?", AllowedLabels: []string{"document"}}
	body, err := buildJevPayload(input, "jev-alias")
	if err != nil {
		t.Fatal(err)
	}
	client, err := adapter.NewClient(adapter.Config{Endpoint: server.URL + "/api/alpha/decisions", Model: "jev-alias", ResponseModel: "jev-pin", Timeout: time.Second, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev}}, 20, 10, 10)
	defer done()
	r.Inventory.Entries[0].Input = input
	inputBytes, _ := json.Marshal(input)
	inputHash := hashBytes(inputBytes)
	r.Inventory.Entries[0].Payloads[0] = Payload{Arm: ArmJev, Body: body, SHA256: hashBytes(body), InputHash: inputHash}
	r.Inventory.Entries[0].Payloads[1].InputHash = inputHash
	r.Inventory.Schedule[0].PayloadSHA = hashBytes(body)
	canonical, _ := inventoryHashBytes(r.Inventory)
	r.Inventory.InventoryHash = hashBytes(canonical)
	r.Config.InventoryHash = r.Inventory.InventoryHash
	r.Attempts[ArmJev] = client.SubmitDecisionsOnce
	out, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Label != "document" || out[0].Billing != "known-zero-reservation-retained" || out[0].Observation == nil {
		t.Fatalf("runner outcome=%+v", out)
	}
}

func TestRunnerHaltsOnOneShotWrongPinWithBillingEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","model":"wrong-pin","provider":"P","answers":{"verdict":{"type":"choice","choice":"document"}},"usage":{"input_tokens":1,"output_tokens":1,"cost":0}}`))
	}))
	defer server.Close()
	input := GoldFreeInput{FixtureID: "x", Task: protocol.TaskEntityType, SourceGroup: "sg", Question: "type?", AllowedLabels: []string{"document"}}
	body, err := buildJevPayload(input, "jev-alias")
	if err != nil {
		t.Fatal(err)
	}
	client, err := adapter.NewClient(adapter.Config{Endpoint: server.URL + "/api/alpha/decisions", Model: "jev-alias", ResponseModel: "jev-pin", ResponseProvider: "P", Timeout: time.Second, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev}, {Ordinal: 2, FixtureID: "x", Arm: ArmNano}}, 20, 10, 10)
	defer done()
	inputBytes, _ := json.Marshal(input)
	inputHash := hashBytes(inputBytes)
	r.Inventory.Entries[0].Input = input
	r.Inventory.Entries[0].Payloads[0] = Payload{Arm: ArmJev, Body: body, SHA256: hashBytes(body), InputHash: inputHash}
	r.Inventory.Entries[0].Payloads[1].InputHash = inputHash
	r.Inventory.Schedule[0].PayloadSHA = hashBytes(body)
	canonical, _ := inventoryHashBytes(r.Inventory)
	r.Inventory.InventoryHash = hashBytes(canonical)
	r.Config.InventoryHash = r.Inventory.InventoryHash
	var nanoCalls atomic.Int32
	r.Attempts[ArmJev] = client.SubmitDecisionsOnce
	r.Attempts[ArmNano] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		nanoCalls.Add(1)
		return zeroObservation("nano"), nil
	}
	_, err = r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "adapter contract failure") {
		t.Fatalf("wrong pin admitted: %v", err)
	}
	if nanoCalls.Load() != 0 || !r.Journal.State().Halted {
		t.Fatalf("nano calls=%d halted=%t", nanoCalls.Load(), r.Journal.State().Halted)
	}
}

func TestProjectKeepsOperationalFailureLabelEmpty(t *testing.T) {
	fixtures := []protocol.Fixture{{ID: "x", Task: protocol.TaskEntityType, SourceGroup: "sg", Split: protocol.SplitTuning, ExpectedLabel: "document"}}
	outcomes := []Outcome{{Slot: Slot{FixtureID: "x", Arm: ArmJev}, Error: "missing billing"}}
	preds, err := Project(fixtures, outcomes, ArmJev)
	if err != nil {
		t.Fatal(err)
	}
	if len(preds) != 1 || preds[0].PredictedLabel != "" || preds[0].ErrorMessage == "" || preds[0].Abstained || preds[0].Attempts != 0 {
		t.Fatalf("prediction=%+v", preds)
	}
}

func newTestRunner(t *testing.T, slots []Slot, combined, jevSub, nanoSub ledger.MicroUnit) (*Runner, func()) {
	t.Helper()
	dir := t.TempDir()
	mk := func(name string, cap ledger.MicroUnit) (*ledger.Ledger, ArmBudget) {
		out := ledger.MicroUnit(1_000_000)
		bounds := ledger.TokenBounds{MaxInputTokens: 1, MaxOutputTokens: 1}
		cfg := ledger.LedgerConfig{Path: filepath.Join(dir, name+".jsonl"), AuthorizedCap: cap, RunID: "run-" + name, Model: name, ManifestKey: ledger.ManifestKey{FixtureFileSHA: "f", ProtocolVersion: protocol.ProtocolVersion, ModelPin: name, RatesVersion: "test", TokenBoundsHash: ledger.ComputeTokenBoundsHash(bounds, 0, 0, 0, 0)}, TokenBounds: bounds, RetryPolicy: ledger.ReservationRetryPolicy{}, RateIn: 1_000_000, RateOut: &out}
		l, err := ledger.Open(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return l, ArmBudget{Ledger: l, Subcap: cap, Reservation: 2, InputBound: 1, OutputBound: 1, RateIn: 1_000_000, RateOut: out}
	}
	jl, jb := mk("jev", max(jevSub, 10))
	nl, nb := mk("nano", max(nanoSub, 10))
	jb.Subcap = jevSub
	nb.Subcap = nanoSub
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
	r := &Runner{Inventory: inv, Config: ExecutionConfig{InventoryHash: inv.InventoryHash, AuthorizationRef: "test-authorization", CombinedCap: combined, LiveContractsVerified: true, RunLockPath: filepath.Join(dir, "run.lock"), EvidenceDir: filepath.Join(dir, "evidence"), ContractSHA: "test-contract", Jev: jb, Nano: nb}, Journal: j, Attempts: map[Arm]AttemptFunc{}}
	return r, func() { _ = j.Close(); _ = jl.Close(); _ = nl.Close() }
}

func zeroObservation(arm string) adapter.AttemptObservation {
	f := decimal
	o := adapter.AttemptObservation{RequestSent: true, ResponseReceived: true, Billing: adapter.BillingObservation{Cost: f("0"), InputTokens: f("1"), OutputTokens: f("1")}}
	if arm == "jev" {
		o.Decision = &adapter.Decision{Choice: "document"}
	} else {
		o.Chat = &adapter.ChatObservation{Content: map[string]any{"label": "document"}}
	}
	return o
}

func decimal(raw string) adapter.DecimalField {
	return adapter.DecimalField{Present: true, Valid: true, Raw: raw, Number: json.Number(raw)}
}
