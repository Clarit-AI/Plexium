package pilot

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type EventType string

const (
	EventInit         EventType = "run-init"
	EventIntent       EventType = "attempt-intent"
	EventReserved     EventType = "reserved"
	EventSend         EventType = "send-started"
	EventObserved     EventType = "observed"
	EventReconciled   EventType = "reconciled"
	EventSkipped      EventType = "not-run"
	EventHalt         EventType = "halt"
	EventSupersession EventType = "halt-supersession"
)

type JournalEvent struct {
	Sequence         int64     `json:"sequence"`
	At               time.Time `json:"at"`
	Type             EventType `json:"type"`
	SlotOrdinal      int       `json:"slotOrdinal,omitempty"`
	FixtureID        string    `json:"fixtureId,omitempty"`
	Arm              Arm       `json:"arm,omitempty"`
	PayloadSHA       string    `json:"payloadSha256,omitempty"`
	ReservationID    string    `json:"reservationId,omitempty"`
	Reserved         int64     `json:"reservedMicrodollars,omitempty"`
	RequestSent      bool      `json:"requestSent,omitempty"`
	ResponseSeen     bool      `json:"responseReceived,omitempty"`
	Status           int       `json:"status,omitempty"`
	ResponseSHA      string    `json:"responseSha256,omitempty"`
	ResponsePath     string    `json:"responsePath,omitempty"`
	ObservationPath  string    `json:"observationPath,omitempty"`
	ObservationSHA   string    `json:"observationSha256,omitempty"`
	InventoryHash    string    `json:"inventoryHash,omitempty"`
	AuthorizationRef string    `json:"authorizationReference,omitempty"`
	CombinedCap      int64     `json:"combinedCapMicrodollars,omitempty"`
	ContractSHA      string    `json:"executionContractSha256,omitempty"`
	AllocationID     string    `json:"allocationId,omitempty"`
	AllocationSHA    string    `json:"allocationSha256,omitempty"`
	BillingCostRaw   string    `json:"billingCostRaw,omitempty"`
	Outcome          string    `json:"outcome,omitempty"`
	Label            string    `json:"label,omitempty"`
	Error            string    `json:"error,omitempty"`
	// RateTolerance records a Decision-5 tolerated known-discrepancy
	// classification on the outcome row (figures verbatim).
	RateTolerance string `json:"rateTolerance,omitempty"`
	// Halt-supersession binding (append-only auditable resume).
	DecisionSHA256          string `json:"decisionSha256,omitempty"`
	DecisionReference       string `json:"decisionReference,omitempty"`
	ToleranceRule           string `json:"toleranceRule,omitempty"`
	SupersededEventSequence int64  `json:"supersededEventSequence,omitempty"`
	SupersededEventSHA256   string `json:"supersededEventSha256,omitempty"`
	TopUpNote               string `json:"topUpNote,omitempty"`
}

type SlotState struct {
	Intent, Reserved, Send, Observed, Reconciled bool
	ReservationID                                string
	ReservedAmount                               int64
	Event                                        JournalEvent
	Observation                                  *JournalEvent
}
type RunBinding struct {
	InventoryHash, AuthorizationRef, ContractSHA string
	AllocationID, AllocationSHA                  string
	CombinedCap                                  int64
}
type ReplayState struct {
	Sequence   int64
	Halted     bool
	HaltReason string
	// HaltSequence/HaltSHA256 identify the durable halt event (the hash is
	// over the journal line exactly as persisted).
	HaltSequence int64
	HaltSHA256   string
	// Supersession is the Decision-5 superseding record when present.
	Supersession *JournalEvent
	Slots        map[int]SlotState
	Binding      *RunBinding
}

type Journal struct {
	file  *os.File
	path  string
	state ReplayState
}

func OpenJournal(path string) (*Journal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	_, statErr := os.Stat(path)
	isNew := errors.Is(statErr, os.ErrNotExist)
	state, err := ReplayJournal(path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = f.Close()
		return nil, err
	}
	if isNew {
		if err := syncDirectory(filepath.Dir(path)); err != nil {
			_ = f.Close()
			return nil, err
		}
	}
	return &Journal{file: f, path: path, state: state}, nil
}

func syncDirectory(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func ReplayJournal(path string) (ReplayState, error) {
	state := ReplayState{Slots: map[int]SlotState{}}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64<<10), 4<<20)
	for s.Scan() {
		line := append([]byte(nil), s.Bytes()...)
		var e JournalEvent
		if err := json.Unmarshal(line, &e); err != nil {
			return state, fmt.Errorf("pilot: torn/invalid journal at sequence %d: %w", state.Sequence+1, err)
		}
		if e.Sequence != state.Sequence+1 {
			return state, fmt.Errorf("pilot: journal sequence %d after %d", e.Sequence, state.Sequence)
		}
		if err := applyEvent(&state, e); err != nil {
			return state, err
		}
		if e.Type == EventHalt {
			state.HaltSHA256 = hashBytes(line)
		}
		state.Sequence = e.Sequence
	}
	if err := s.Err(); err != nil {
		return state, err
	}
	return state, nil
}

func applyEvent(s *ReplayState, e JournalEvent) error {
	if e.Type == EventInit {
		if s.Sequence != 0 || s.Binding != nil || e.InventoryHash == "" || e.AuthorizationRef == "" || e.CombinedCap <= 0 || e.ContractSHA == "" || e.AllocationID == "" || e.AllocationSHA == "" {
			return errors.New("pilot: invalid or duplicate run-init")
		}
		s.Binding = &RunBinding{InventoryHash: e.InventoryHash, AuthorizationRef: e.AuthorizationRef, CombinedCap: e.CombinedCap, ContractSHA: e.ContractSHA, AllocationID: e.AllocationID, AllocationSHA: e.AllocationSHA}
		return nil
	}
	if e.Type == EventHalt {
		if s.Binding == nil {
			return errors.New("pilot: halt before run-init")
		}
		s.Halted = true
		s.HaltReason = e.Error
		s.HaltSequence = e.Sequence
		return nil
	}
	if e.Type == EventSupersession {
		if s.Binding == nil {
			return errors.New("pilot: supersession before run-init")
		}
		if !s.Halted {
			return errors.New("pilot: supersession without a halted event")
		}
		if s.Supersession != nil {
			return errors.New("pilot: duplicate halt supersession")
		}
		if e.SupersededEventSequence != s.HaltSequence || e.SupersededEventSHA256 == "" || e.SupersededEventSHA256 != s.HaltSHA256 {
			return errors.New("pilot: supersession does not bind the halted event")
		}
		if e.DecisionSHA256 == "" || e.ToleranceRule == "" {
			return errors.New("pilot: supersession missing decision or tolerance-rule binding")
		}
		copy := e
		s.Supersession = &copy
		return nil
	}
	if s.Binding == nil {
		return errors.New("pilot: journal transition before run-init")
	}
	if e.SlotOrdinal <= 0 {
		return errors.New("pilot: journal event missing slot ordinal")
	}
	st := s.Slots[e.SlotOrdinal]
	if st.Intent && (st.Event.FixtureID != e.FixtureID || st.Event.Arm != e.Arm || st.Event.PayloadSHA != e.PayloadSHA) {
		return fmt.Errorf("pilot: slot %d identity changed across journal transitions", e.SlotOrdinal)
	}
	switch e.Type {
	case EventIntent:
		if st.Intent {
			return fmt.Errorf("pilot: duplicate intent for slot %d", e.SlotOrdinal)
		}
		st.Intent = true
	case EventReserved:
		if !st.Intent || st.Reserved || e.ReservationID == "" {
			return fmt.Errorf("pilot: invalid reservation transition for slot %d", e.SlotOrdinal)
		}
		st.Reserved = true
		st.ReservationID = e.ReservationID
		st.ReservedAmount = e.Reserved
	case EventSend:
		if !st.Reserved || st.Send {
			return fmt.Errorf("pilot: invalid send transition for slot %d", e.SlotOrdinal)
		}
		st.Send = true
	case EventObserved:
		if !st.Send || st.Observed {
			return fmt.Errorf("pilot: invalid observation transition for slot %d", e.SlotOrdinal)
		}
		st.Observed = true
		copy := e
		st.Observation = &copy
	case EventReconciled:
		if !st.Observed || st.Reconciled || e.ReservationID != st.ReservationID {
			return fmt.Errorf("pilot: invalid reconcile transition for slot %d", e.SlotOrdinal)
		}
		st.Reconciled = true
	case EventSkipped:
		if st.Intent || st.Reconciled {
			return fmt.Errorf("pilot: invalid not-run transition for slot %d", e.SlotOrdinal)
		}
		st.Reconciled = true
	default:
		return fmt.Errorf("pilot: unknown journal event %q", e.Type)
	}
	st.Event = e
	s.Slots[e.SlotOrdinal] = st
	return nil
}

func (j *Journal) BindRun(binding RunBinding) error {
	if j.state.Sequence == 0 {
		return j.Append(JournalEvent{Type: EventInit, InventoryHash: binding.InventoryHash, AuthorizationRef: binding.AuthorizationRef, CombinedCap: binding.CombinedCap, ContractSHA: binding.ContractSHA, AllocationID: binding.AllocationID, AllocationSHA: binding.AllocationSHA})
	}
	if j.state.Binding == nil {
		return errors.New("pilot: existing journal has no run-init binding")
	}
	if *j.state.Binding != binding {
		return errors.New("pilot: execution authorization/shared-cap/contract drift")
	}
	return nil
}

func (j *Journal) Append(e JournalEvent) error {
	e.Sequence = j.state.Sequence + 1
	e.At = time.Now().UTC()
	probe := j.state
	probe.Slots = make(map[int]SlotState, len(j.state.Slots))
	for k, v := range j.state.Slots {
		probe.Slots[k] = v
	}
	if err := applyEvent(&probe, e); err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := j.file.Write(append(b, '\n')); err != nil {
		return err
	}
	if err := j.file.Sync(); err != nil {
		return err
	}
	probe.Sequence = e.Sequence
	j.state = probe
	return nil
}
func (j *Journal) State() ReplayState { return j.state }
func (j *Journal) Close() error {
	if j.file == nil {
		return nil
	}
	return j.file.Close()
}
