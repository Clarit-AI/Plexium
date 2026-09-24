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
	ContractSHA           string
	AllocationID          string
	AllocationSHA         string
	ScreeningSecrets      []string
	Jev, Nano             ArmBudget
}

type Outcome struct {
	Slot             Slot
	Label            string
	Error            string
	Billing          string
	BillingRaw       string
	CostMicrodollars ledger.MicroUnit
	KnownCost        bool
	RequestSent      bool
	Status           int
	Observation      *ObservationEvidence
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
	if r.Config.ContractSHA == "" || r.Config.AllocationID == "" || r.Config.AllocationSHA == "" {
		return errors.New("pilot: execution contract and allocation binding required")
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
	if err := r.Journal.BindRun(RunBinding{InventoryHash: r.Config.InventoryHash, AuthorizationRef: r.Config.AuthorizationRef, CombinedCap: int64(r.Config.CombinedCap), ContractSHA: r.Config.ContractSHA, AllocationID: r.Config.AllocationID, AllocationSHA: r.Config.AllocationSHA}); err != nil {
		return nil, err
	}
	state := r.Journal.State()
	if state.Halted {
		return nil, fmt.Errorf("pilot: journal halted: %s", state.HaltReason)
	}
	if err := r.validateReplayState(state, 0); err != nil {
		return nil, r.halt(err.Error())
	}
	for ordinal, st := range state.Slots {
		if st.Intent && !st.Reconciled {
			return nil, fmt.Errorf("pilot: slot %d is incomplete; uncertain attempts are never resent", ordinal)
		}
	}
	var outcomes []Outcome
	armStopped := map[Arm]bool{}
	for _, slot := range r.Inventory.Schedule {
		if err := r.validateReplayState(r.Journal.State(), 0); err != nil {
			return outcomes, r.halt(err.Error())
		}
		if st := r.Journal.State().Slots[slot.Ordinal]; st.Reconciled {
			continue
		}
		if armStopped[slot.Arm] {
			out := Outcome{Slot: slot, Error: "arm-subcap-exhausted"}
			outcomes = append(outcomes, out)
			if err := r.appendSlotEvent(slot, JournalEvent{Type: EventSkipped, SlotOrdinal: slot.Ordinal, FixtureID: slot.FixtureID, Arm: slot.Arm, PayloadSHA: slot.PayloadSHA, Outcome: "not-run", Error: out.Error}); err != nil {
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
			if err := r.appendSlotEvent(slot, JournalEvent{Type: EventSkipped, SlotOrdinal: slot.Ordinal, FixtureID: slot.FixtureID, Arm: slot.Arm, PayloadSHA: slot.PayloadSHA, Outcome: "not-run", Error: out.Error}); err != nil {
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
		if err := r.appendSlotEvent(slot, withType(base, EventIntent)); err != nil {
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
		if err := r.appendSlotEvent(slot, re); err != nil {
			return outcomes, err
		}
		if err := r.validateReplayState(r.Journal.State(), slot.Ordinal); err != nil {
			return outcomes, r.halt(err.Error())
		}
		if err := r.appendSlotEvent(slot, withType(base, EventSend)); err != nil {
			return outcomes, err
		}
		fn := r.Attempts[slot.Arm]
		if fn == nil {
			return outcomes, r.halt("missing arm attempt function")
		}
		obs, attemptErr := fn(ctx, payload.Body)
		obs, screenedRaw := screenObservation(obs, r.Config.ScreeningSecrets)
		responsePath, responseSHA, observationPath, observationSHA, persistErr := persistAttemptEvidence(r.Config.EvidenceDir, slot, obs, screenedRaw)
		if persistErr != nil {
			return outcomes, r.halt("persist response evidence: " + persistErr.Error())
		}
		oe := withType(base, EventObserved)
		oe.RequestSent = obs.RequestSent
		oe.ResponseSeen = obs.ResponseReceived
		oe.Status = obs.Status
		oe.ResponseSHA = responseSHA
		oe.ResponsePath = responsePath
		oe.ObservationPath = observationPath
		oe.ObservationSHA = observationSHA
		oe.BillingCostRaw = obs.Billing.Cost.Raw
		if attemptErr != nil {
			oe.Error = sanitizeTextPreservingNumbers(attemptErr.Error(), r.Config.ScreeningSecrets)
		}
		if err := r.appendSlotEvent(slot, oe); err != nil {
			return outcomes, err
		}
		evidence := evidenceFromObservation(obs, screenedRaw)
		out := Outcome{Slot: slot, RequestSent: obs.RequestSent, Status: obs.Status, Observation: &evidence}
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
		out.KnownCost = true
		usageExceeded := in > budget.InputBound || outTokens > budget.OutputBound
		if cost > 0 {
			if _, err := budget.Ledger.Settle(ctx, ref, cost, in, outTokens, budget.RateIn, budget.RateOut); err != nil {
				if usageExceeded {
					out.Error = fmt.Sprintf("observed usage exceeds bounds: input %d/%d output %d/%d: %v", in, budget.InputBound, outTokens, budget.OutputBound, err)
				} else {
					out.Error = err.Error()
				}
				outcomes = append(outcomes, out)
				return outcomes, r.halt(out.Error)
			}
		}
		if usageExceeded {
			out.Error = fmt.Sprintf("observed usage exceeds bounds: input %d/%d output %d/%d", in, budget.InputBound, outTokens, budget.OutputBound)
			outcomes = append(outcomes, out)
			return outcomes, r.halt(out.Error)
		}
		if attemptErr != nil {
			out.Error = sanitizeTextPreservingNumbers(attemptErr.Error(), r.Config.ScreeningSecrets)
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
		haltForContract := mustHaltAttempt(attemptErr) || (attemptErr == nil && out.Label == "")
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
		if err := r.appendSlotEvent(slot, final); err != nil {
			return outcomes, err
		}
		outcomes = append(outcomes, out)
		if haltForContract {
			return outcomes, r.halt("adapter contract failure: " + out.Error)
		}
	}
	return outcomes, nil
}

func (r *Runner) halt(reason string) error {
	reason = sanitizeTextPreservingNumbers(reason, r.Config.ScreeningSecrets)
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

func (r *Runner) appendSlotEvent(slot Slot, event JournalEvent) error {
	if event.SlotOrdinal != slot.Ordinal || event.FixtureID != slot.FixtureID || event.Arm != slot.Arm || event.PayloadSHA != slot.PayloadSHA {
		return fmt.Errorf("pilot: journal event does not match inventory slot %d", slot.Ordinal)
	}
	// BillingCostRaw is a numeric lexeme (decimal string); never sanitize numeric fields
	event.Label = sanitizeText(event.Label, r.Config.ScreeningSecrets)
	event.Error = sanitizeTextPreservingNumbers(event.Error, r.Config.ScreeningSecrets)
	if err := r.Journal.Append(event); err != nil {
		return err
	}
	return r.validateReplayState(r.Journal.State(), slot.Ordinal)
}

func (r *Runner) validateReplayState(state ReplayState, allowPendingOrdinal int) error {
	type reservation struct {
		entry ledger.Entry
	}
	refs := map[Arm]map[string]reservation{ArmJev: {}, ArmNano: {}}
	terminalRefs := map[Arm]map[string]bool{ArmJev: {}, ArmNano: {}}
	journalRefs := map[Arm]map[string]int{ArmJev: {}, ArmNano: {}}
	schedule := make(map[int]Slot, len(r.Inventory.Schedule))
	for _, slot := range r.Inventory.Schedule {
		schedule[slot.Ordinal] = slot
	}
	for _, arm := range []Arm{ArmJev, ArmNano} {
		budget := r.budget(arm)
		if budget == nil || budget.Ledger == nil {
			continue
		}
		for _, entry := range budget.Ledger.Entries() {
			if entry.Type == ledger.EntryReservation {
				refs[arm][entry.ID] = reservation{entry: entry}
			}
			if entry.RefID != "" && (entry.Type == ledger.EntrySettlement || entry.Type == ledger.EntryAdjustment || entry.Type == ledger.EntryMismatch) {
				terminalRefs[arm][entry.RefID] = true
			}
		}
	}
	for ordinal, st := range state.Slots {
		slot, ok := schedule[ordinal]
		if !ok {
			return fmt.Errorf("pilot: journal slot %d is absent from frozen inventory", ordinal)
		}
		if st.Event.SlotOrdinal != slot.Ordinal || st.Event.FixtureID != slot.FixtureID || st.Event.Arm != slot.Arm || st.Event.PayloadSHA != slot.PayloadSHA {
			return fmt.Errorf("pilot: journal identity mismatch for inventory slot %d", ordinal)
		}
		if !st.Reserved {
			continue
		}
		reserved, ok := refs[slot.Arm][st.ReservationID]
		if !ok || int64(reserved.entry.Amount) != st.ReservedAmount {
			return fmt.Errorf("pilot: journal/ledger reservation mismatch for slot %d", ordinal)
		}
		inventoryEntry, _, err := r.lookup(slot)
		if err != nil {
			return fmt.Errorf("pilot: inventory lookup for slot %d: %w", ordinal, err)
		}
		if reserved.entry.Attempt != slot.Ordinal || reserved.entry.Task != string(inventoryEntry.Input.Task) || reserved.entry.SourceGroup != inventoryEntry.Input.SourceGroup {
			return fmt.Errorf("pilot: ledger reservation identity mismatch for slot %d", ordinal)
		}
		journalRefs[slot.Arm][st.ReservationID] = ordinal
		if !st.Reconciled && ordinal != allowPendingOrdinal {
			return fmt.Errorf("pilot: slot %d has an uncertain outcome; never resend", ordinal)
		}
		if st.Reconciled && st.Event.Outcome == "settled-positive" && !terminalRefs[st.Event.Arm][st.ReservationID] {
			return fmt.Errorf("pilot: slot %d journal settlement missing from ledger", ordinal)
		}
		if st.Reconciled && st.Event.Outcome == "zero-known-reservation-retained" && terminalRefs[st.Event.Arm][st.ReservationID] {
			return fmt.Errorf("pilot: slot %d zero-retained reservation unexpectedly terminal in ledger", ordinal)
		}
		if st.Observed {
			if st.Observation == nil {
				return fmt.Errorf("pilot: slot %d lost observation binding", ordinal)
			}
			evidence, err := loadAttemptEvidence(r.Config.EvidenceDir, *st.Observation)
			if err != nil {
				return fmt.Errorf("pilot: slot %d evidence invalid: %w", ordinal, err)
			}
			if evidence.RequestSHA256 != "" && evidence.RequestSHA256 != slot.PayloadSHA {
				return fmt.Errorf("pilot: slot %d request digest does not match frozen payload", ordinal)
			}
		}
	}
	for arm, armRefs := range refs {
		for ref := range armRefs {
			if _, ok := journalRefs[arm][ref]; !ok {
				return fmt.Errorf("pilot: orphan %s ledger reservation %s has no journal outcome; never resend", arm, ref)
			}
		}
	}
	return nil
}
func mustHaltAttempt(err error) bool {
	if err == nil {
		return false
	}
	var transportErr *adapter.TransportError
	if !errors.As(err, &transportErr) {
		return true
	}
	switch transportErr.Code {
	case "model-pin", "provider-pin", "schema", "auth", "cancelled", "payload":
		return true
	}
	return false
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
func CostMicrodollars(raw string) (ledger.MicroUnit, error) { return decimalMicrodollars(raw) }
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

func ReplayOutcomes(inv *Inventory, state ReplayState, evidenceDir string) ([]Outcome, error) {
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
			errText = state.HaltReason
			if errText == "" {
				errText = "incomplete-attempt-never-resend"
			}
		}
		billing := ""
		if e.Outcome == "zero-known-reservation-retained" {
			billing = "known-zero-reservation-retained"
		}
		if e.Outcome == "settled-positive" {
			billing = "known-positive"
		}
		var cost ledger.MicroUnit
		knownCost := false
		var evidence *ObservationEvidence
		if st.Observed {
			if st.Observation == nil {
				return nil, fmt.Errorf("pilot: slot %d lost observation binding", slot.Ordinal)
			}
			loaded, err := loadAttemptEvidence(evidenceDir, *st.Observation)
			if err != nil {
				return nil, err
			}
			evidence = loaded
			if parsedCost, _, _, _, billingErr := validatedBilling(loaded.Billing); billingErr == nil {
				cost = parsedCost
				knownCost = true
			}
		}
		outcomes = append(outcomes, Outcome{Slot: slot, Label: e.Label, Error: errText, Billing: billing, BillingRaw: e.BillingCostRaw, CostMicrodollars: cost, KnownCost: knownCost, RequestSent: e.RequestSent, Status: e.Status, Observation: evidence})
	}
	return outcomes, nil
}
