package pilot

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
	"github.com/Clarit-AI/Plexium/evaluations/jev/ledger"
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

func TestJournalReplayReconstructsStateAndRejectsTornTail(t *testing.T) {
	p := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := OpenJournal(p)
	if err != nil {
		t.Fatal(err)
	}
	base := JournalEvent{SlotOrdinal: 1, FixtureID: "x", Arm: ArmJev, PayloadSHA: "abc"}
	for _, e := range []JournalEvent{withType(base, EventIntent), withType(base, EventReserved), withType(base, EventSend), withType(base, EventObserved), withType(base, EventReconciled)} {
		if e.Type == EventReserved {
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
	if !s.Slots[1].Reconciled || s.Sequence != 5 {
		t.Fatalf("bad replay: %+v", s)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"sequence":6`)
	_ = f.Close()
	if _, err := ReplayJournal(p); err == nil {
		t.Fatal("torn journal tail accepted")
	}
}

func TestRestartAfterSendRefusesResendAndPreservesReservation(t *testing.T) {
	r, closeAll := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev, Repetition: 1, PayloadSHA: "p"}}, 20, 10, 10)
	defer closeAll()
	b := r.Config.Jev
	base := JournalEvent{SlotOrdinal: 1, FixtureID: "x", Arm: ArmJev, PayloadSHA: "p"}
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
	if err == nil || !strings.Contains(err.Error(), "never resent") {
		t.Fatalf("Run error=%v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("resend count=%d", calls.Load())
	}
	if b.Ledger.Balance() != res {
		t.Fatalf("reservation reset: balance=%d want=%d", b.Ledger.Balance(), res)
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
	r, closeAll := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev, PayloadSHA: "p"}}, 4, 2, 2)
	defer closeAll()
	for i := 0; i < 2; i++ {
		if _, _, err := r.Config.Nano.Ledger.Reserve(context.Background(), 99+i, "entity-type", "sg", 1, 1); err != nil {
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
	obs, err := client.SubmitDecisionsOnce(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	if !obs.RequestSent || obs.Decision == nil || obs.Decision.Choice != "document" || obs.Billing.Cost.Raw != "0" {
		t.Fatalf("observation=%+v", obs)
	}
}

func TestProjectKeepsOperationalFailureLabelEmpty(t *testing.T) {
	fixtures := []protocol.Fixture{{ID: "x", Task: protocol.TaskEntityType, SourceGroup: "sg", Split: protocol.SplitTuning, ExpectedLabel: "document"}}
	outcomes := []Outcome{{Slot: Slot{FixtureID: "x", Arm: ArmJev}, Error: "missing billing"}}
	preds, err := Project(fixtures, outcomes, ArmJev)
	if err != nil {
		t.Fatal(err)
	}
	if len(preds) != 1 || preds[0].PredictedLabel != "" || preds[0].ErrorMessage == "" || preds[0].Abstained {
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
	input := GoldFreeInput{FixtureID: "x", Task: protocol.TaskEntityType, SourceGroup: "sg"}
	inputBytes, _ := json.Marshal(input)
	inputHash := hashBytes(inputBytes)
	entry := InventoryEntry{Input: input, Payloads: []Payload{{Arm: ArmJev, Body: json.RawMessage(`{}`), SHA256: payloadHash, InputHash: inputHash}, {Arm: ArmNano, Body: json.RawMessage(`{}`), SHA256: payloadHash, InputHash: inputHash}}}
	inv := &Inventory{Entries: []InventoryEntry{entry}, Schedule: slots}
	canonical, _ := inventoryHashBytes(inv)
	inv.InventoryHash = hashBytes(canonical)
	r := &Runner{Inventory: inv, Config: ExecutionConfig{InventoryHash: inv.InventoryHash, AuthorizationRef: "test-authorization", CombinedCap: combined, LiveContractsVerified: true, RunLockPath: filepath.Join(dir, "run.lock"), EvidenceDir: filepath.Join(dir, "evidence"), Jev: jb, Nano: nb}, Journal: j, Attempts: map[Arm]AttemptFunc{}}
	return r, func() { _ = j.Close(); _ = jl.Close(); _ = nl.Close() }
}

func zeroObservation(arm string) adapter.AttemptObservation {
	f := func(raw string) adapter.DecimalField {
		return adapter.DecimalField{Present: true, Valid: true, Raw: raw, Number: json.Number(raw)}
	}
	o := adapter.AttemptObservation{RequestSent: true, ResponseReceived: true, Billing: adapter.BillingObservation{Cost: f("0"), InputTokens: f("1"), OutputTokens: f("1")}}
	if arm == "jev" {
		o.Decision = &adapter.Decision{Choice: "document"}
	} else {
		o.Chat = &adapter.ChatObservation{Content: map[string]any{"label": "document"}}
	}
	return o
}
