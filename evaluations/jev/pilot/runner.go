package pilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strconv"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
	"github.com/Clarit-AI/Plexium/evaluations/jev/ledger"
)

type AttemptFunc func(context.Context, []byte) (adapter.AttemptObservation, error)

type ArmBudget struct {
	Ledger      *ledger.Ledger
	Subcap      ledger.MicroUnit
	Reservation ledger.MicroUnit
	InputBound  int64
	OutputBound int64
	RateIn      ledger.MicroUnit
	RateOut     ledger.MicroUnit
}

type ExecutionConfig struct {
	InventoryHash         string
	AuthorizationRef      string
	CombinedCap           ledger.MicroUnit
	LiveContractsVerified bool
	RunLockPath           string
	EvidenceDir           string
	Jev, Nano             ArmBudget
}

type Outcome struct {
	Slot             Slot
	Label            string
	Error            string
	Billing          string
	BillingRaw       string
	CostMicrodollars ledger.MicroUnit
	RequestSent      bool
	Status           int
}

type Runner struct {
	Inventory *Inventory
	Config    ExecutionConfig
	Journal   *Journal
	Attempts  map[Arm]AttemptFunc
	lock      *os.File
}

func (r *Runner) Validate() error {
	if r.Inventory == nil || r.Journal == nil {
		return errors.New("pilot: inventory and journal required")
	}
	if r.Config.InventoryHash == "" || r.Config.InventoryHash != r.Inventory.InventoryHash {
		return errors.New("pilot: inventory hash mismatch")
	}
	if err := ValidateInventory(r.Inventory); err != nil {
		return err
	}
	if r.Config.AuthorizationRef == "" || r.Config.CombinedCap <= 0 {
		return errors.New("pilot: explicit authorization reference and positive combined cap required")
	}
	if !r.Config.LiveContractsVerified {
		return errors.New("pilot: live contracts are not verified")
	}
	if r.Config.Jev.Subcap+r.Config.Nano.Subcap > r.Config.CombinedCap {
		return errors.New("pilot: arm subcaps exceed shared cap")
	}
	if r.Config.RunLockPath == "" || r.Config.EvidenceDir == "" {
		return errors.New("pilot: run lock path and private evidence directory required")
	}
	return nil
}

func (r *Runner) Run(ctx context.Context) ([]Outcome, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(r.Config.RunLockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("pilot: acquire shared run lock: %w", err)
	}
	r.lock = lock
	defer func() { _ = lock.Close(); _ = os.Remove(r.Config.RunLockPath) }()
	state := r.Journal.State()
	if state.Halted {
		return nil, fmt.Errorf("pilot: journal halted: %s", state.HaltReason)
	}
	if err := r.validateReplayReservations(state); err != nil {
		return nil, err
	}
	for ordinal, st := range state.Slots {
		if st.Intent && !st.Reconciled {
			return nil, fmt.Errorf("pilot: slot %d is incomplete; uncertain attempts are never resent", ordinal)
		}
	}
	var outcomes []Outcome
	armStopped := map[Arm]bool{}
	for _, slot := range r.Inventory.Schedule {
		if st := r.Journal.State().Slots[slot.Ordinal]; st.Reconciled {
			continue
		}
		if armStopped[slot.Arm] {
			out := Outcome{Slot: slot, Error: "arm-subcap-exhausted"}
			outcomes = append(outcomes, out)
			if err := r.Journal.Append(JournalEvent{Type: EventSkipped, SlotOrdinal: slot.Ordinal, FixtureID: slot.FixtureID, Arm: slot.Arm, PayloadSHA: slot.PayloadSHA, Outcome: "not-run", Error: out.Error}); err != nil {
				return outcomes, err
			}
			continue
		}
		budget := r.budget(slot.Arm)
		if budget == nil || budget.Ledger == nil {
			return outcomes, r.halt("missing arm ledger")
		}
		if r.Config.Jev.Ledger.Halted() || r.Config.Nano.Ledger.Halted() {
			return outcomes, r.halt("ledger halted")
		}
		if budget.Ledger.Balance()+budget.Reservation > budget.Subcap {
			armStopped[slot.Arm] = true
			out := Outcome{Slot: slot, Error: "arm-subcap-exhausted"}
			outcomes = append(outcomes, out)
			if err := r.Journal.Append(JournalEvent{Type: EventSkipped, SlotOrdinal: slot.Ordinal, FixtureID: slot.FixtureID, Arm: slot.Arm, PayloadSHA: slot.PayloadSHA, Outcome: "not-run", Error: out.Error}); err != nil {
				return outcomes, err
			}
			continue
		}
		if r.Config.Jev.Ledger.Balance()+r.Config.Nano.Ledger.Balance()+budget.Reservation > r.Config.CombinedCap {
			return outcomes, r.halt("shared-cap-exhausted")
		}
		entry, payload, err := r.lookup(slot)
		if err != nil {
			return outcomes, r.halt(err.Error())
		}
		base := JournalEvent{SlotOrdinal: slot.Ordinal, FixtureID: slot.FixtureID, Arm: slot.Arm, PayloadSHA: payload.SHA256}
		if err := r.Journal.Append(withType(base, EventIntent)); err != nil {
			return outcomes, err
		}
		ref, reserved, err := budget.Ledger.Reserve(ctx, slot.Ordinal, string(entry.Input.Task), entry.Input.SourceGroup, budget.InputBound, budget.OutputBound)
		if err != nil {
			return outcomes, r.halt(err.Error())
		}
		if reserved != budget.Reservation {
			return outcomes, r.halt(fmt.Sprintf("reservation drift: got %d want %d", reserved, budget.Reservation))
		}
		re := withType(base, EventReserved)
		re.ReservationID = ref
		re.Reserved = int64(reserved)
		if err := r.Journal.Append(re); err != nil {
			return outcomes, err
		}
		if err := r.Journal.Append(withType(base, EventSend)); err != nil {
			return outcomes, err
		}
		fn := r.Attempts[slot.Arm]
		if fn == nil {
			return outcomes, r.halt("missing arm attempt function")
		}
		obs, attemptErr := fn(ctx, payload.Body)
		responsePath, persistErr := r.persistEvidence(slot, obs.RawResponse)
		if persistErr != nil {
			return outcomes, r.halt("persist response evidence: " + persistErr.Error())
		}
		oe := withType(base, EventObserved)
		oe.RequestSent = obs.RequestSent
		oe.ResponseSeen = obs.ResponseReceived
		oe.Status = obs.Status
		oe.ResponseSHA = obs.RawSHA256
		oe.ResponsePath = responsePath
		oe.BillingCostRaw = obs.Billing.Cost.Raw
		if attemptErr != nil {
			oe.Error = attemptErr.Error()
		}
		if err := r.Journal.Append(oe); err != nil {
			return outcomes, err
		}
		out := Outcome{Slot: slot, RequestSent: obs.RequestSent, Status: obs.Status}
		if obs.RequestSent && !obs.ResponseReceived {
			out.Error = "uncertain-delivery"
			outcomes = append(outcomes, out)
			return outcomes, r.halt("uncertain delivery; fixture will not be resent")
		}
		cost, in, outTokens, billing, err := validatedBilling(obs.Billing)
		if err != nil {
			out.Error = err.Error()
			outcomes = append(outcomes, out)
			return outcomes, r.halt(err.Error())
		}
		out.Billing = billing
		out.BillingRaw = obs.Billing.Cost.Raw
		out.CostMicrodollars = cost
		if cost > 0 {
			if _, err := budget.Ledger.Settle(ctx, ref, cost, in, outTokens, budget.RateIn, budget.RateOut); err != nil {
				out.Error = err.Error()
				outcomes = append(outcomes, out)
				return outcomes, r.halt(err.Error())
			}
		}
		if attemptErr != nil {
			out.Error = attemptErr.Error()
		} else if obs.Decision != nil {
			out.Label = obs.Decision.Choice
		} else if obs.Chat != nil {
			if v, ok := obs.Chat.Content["label"].(string); ok {
				out.Label = v
			}
		}
		if out.Label == "" && out.Error == "" {
			out.Error = "validated response contained no label"
		}
		final := withType(base, EventReconciled)
		final.ReservationID = ref
		final.BillingCostRaw = obs.Billing.Cost.Raw
		final.Label = out.Label
		final.Error = out.Error
		final.RequestSent = obs.RequestSent
		final.ResponseSeen = obs.ResponseReceived
		final.Status = obs.Status
		if cost == 0 {
			final.Outcome = "zero-known-reservation-retained"
		} else {
			final.Outcome = "settled-positive"
		}
		if err := r.Journal.Append(final); err != nil {
			return outcomes, err
		}
		outcomes = append(outcomes, out)
	}
	return outcomes, nil
}

func (r *Runner) persistEvidence(slot Slot, raw []byte) (string, error) {
	if err := os.MkdirAll(r.Config.EvidenceDir, 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(r.Config.EvidenceDir, 0700); err != nil {
		return "", err
	}
	path := fmt.Sprintf("%04d-%s-%s.response", slot.Ordinal, slot.FixtureID, slot.Arm)
	full := r.Config.EvidenceDir + string(os.PathSeparator) + path
	f, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err := syncDirectory(r.Config.EvidenceDir); err != nil {
		return "", err
	}
	return path, nil
}

func (r *Runner) halt(reason string) error {
	_ = r.Journal.Append(JournalEvent{Type: EventHalt, Error: reason})
	return errors.New(reason)
}
func (r *Runner) budget(a Arm) *ArmBudget {
	if a == ArmJev {
		return &r.Config.Jev
	}
	if a == ArmNano {
		return &r.Config.Nano
	}
	return nil
}
func (r *Runner) lookup(s Slot) (InventoryEntry, Payload, error) {
	for _, e := range r.Inventory.Entries {
		if e.Input.FixtureID == s.FixtureID {
			p, ok := payloadFor(e, s.Arm)
			if !ok {
				return e, p, errors.New("payload missing")
			}
			if p.SHA256 != s.PayloadSHA || hashBytes(p.Body) != p.SHA256 {
				return e, p, errors.New("payload hash drift")
			}
			return e, p, nil
		}
	}
	return InventoryEntry{}, Payload{}, errors.New("fixture missing")
}

func (r *Runner) validateReplayReservations(state ReplayState) error {
	refs := map[Arm]map[string]ledger.MicroUnit{ArmJev: {}, ArmNano: {}}
	for _, arm := range []Arm{ArmJev, ArmNano} {
		budget := r.budget(arm)
		if budget == nil || budget.Ledger == nil {
			continue
		}
		for _, entry := range budget.Ledger.Entries() {
			if entry.Type == ledger.EntryReservation {
				refs[arm][entry.ID] = entry.Amount
			}
		}
	}
	for ordinal, st := range state.Slots {
		if !st.Reserved {
			continue
		}
		amount, ok := refs[st.Event.Arm][st.ReservationID]
		if !ok || int64(amount) != st.ReservedAmount {
			return fmt.Errorf("pilot: journal/ledger reservation mismatch for slot %d", ordinal)
		}
	}
	return nil
}
func withType(e JournalEvent, t EventType) JournalEvent { e.Type = t; return e }

func validatedBilling(b adapter.BillingObservation) (ledger.MicroUnit, int64, int64, string, error) {
	if b.Error != "" {
		return 0, 0, 0, "", fmt.Errorf("billing invalid: %s", b.Error)
	}
	for name, f := range map[string]adapter.DecimalField{"cost": b.Cost, "input_tokens": b.InputTokens, "output_tokens": b.OutputTokens} {
		if !f.Present || f.Null || !f.Valid {
			return 0, 0, 0, "", fmt.Errorf("billing %s missing/null/invalid", name)
		}
	}
	cost, err := decimalMicrodollars(b.Cost.Raw)
	if err != nil {
		return 0, 0, 0, "", err
	}
	in, err := decimalInteger(b.InputTokens.Raw)
	if err != nil {
		return 0, 0, 0, "", err
	}
	out, err := decimalInteger(b.OutputTokens.Raw)
	if err != nil {
		return 0, 0, 0, "", err
	}
	status := "known-positive"
	if cost == 0 {
		status = "known-zero-reservation-retained"
	}
	return cost, in, out, status, nil
}
func decimalMicrodollars(raw string) (ledger.MicroUnit, error) {
	r, ok := new(big.Rat).SetString(raw)
	if !ok || r.Sign() < 0 {
		return 0, fmt.Errorf("invalid billing cost %q", raw)
	}
	r.Mul(r, big.NewRat(1_000_000, 1))
	q, rem := new(big.Int).QuoRem(r.Num(), r.Denom(), new(big.Int))
	if rem.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return 0, errors.New("billing cost overflow")
	}
	return ledger.MicroUnit(q.Int64()), nil
}
func decimalInteger(raw string) (int64, error) {
	if raw == "" {
		return 0, errors.New("missing token count")
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid token count %q", raw)
	}
	return n, nil
}

func MarshalInventory(inv *Inventory) ([]byte, error) { return json.MarshalIndent(inv, "", "  ") }

func ReplayOutcomes(inv *Inventory, state ReplayState) []Outcome {
	outcomes := make([]Outcome, 0, len(inv.Schedule))
	for _, slot := range inv.Schedule {
		st, ok := state.Slots[slot.Ordinal]
		if !ok {
			reason := "not-run"
			if state.Halted {
				reason = "not-run: " + state.HaltReason
			}
			outcomes = append(outcomes, Outcome{Slot: slot, Error: reason})
			continue
		}
		e := st.Event
		errText := e.Error
		if !st.Reconciled && errText == "" {
			errText = "incomplete-attempt-never-resend"
		}
		billing := ""
		if e.Outcome == "zero-known-reservation-retained" {
			billing = "known-zero-reservation-retained"
		}
		if e.Outcome == "settled-positive" {
			billing = "known-positive"
		}
		cost, _ := decimalMicrodollars(e.BillingCostRaw)
		outcomes = append(outcomes, Outcome{Slot: slot, Label: e.Label, Error: errText, Billing: billing, BillingRaw: e.BillingCostRaw, CostMicrodollars: cost, RequestSent: e.RequestSent, Status: e.Status})
	}
	return outcomes
}
