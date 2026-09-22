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
	EventIntent     EventType = "attempt-intent"
	EventReserved   EventType = "reserved"
	EventSend       EventType = "send-started"
	EventObserved   EventType = "observed"
	EventReconciled EventType = "reconciled"
	EventSkipped    EventType = "not-run"
	EventHalt       EventType = "halt"
)

type JournalEvent struct {
	Sequence       int64     `json:"sequence"`
	At             time.Time `json:"at"`
	Type           EventType `json:"type"`
	SlotOrdinal    int       `json:"slotOrdinal,omitempty"`
	FixtureID      string    `json:"fixtureId,omitempty"`
	Arm            Arm       `json:"arm,omitempty"`
	PayloadSHA     string    `json:"payloadSha256,omitempty"`
	ReservationID  string    `json:"reservationId,omitempty"`
	Reserved       int64     `json:"reservedMicrodollars,omitempty"`
	RequestSent    bool      `json:"requestSent,omitempty"`
	ResponseSeen   bool      `json:"responseReceived,omitempty"`
	Status         int       `json:"status,omitempty"`
	ResponseSHA    string    `json:"responseSha256,omitempty"`
	ResponsePath   string    `json:"responsePath,omitempty"`
	BillingCostRaw string    `json:"billingCostRaw,omitempty"`
	Outcome        string    `json:"outcome,omitempty"`
	Label          string    `json:"label,omitempty"`
	Error          string    `json:"error,omitempty"`
}

type SlotState struct {
	Intent, Reserved, Send, Observed, Reconciled bool
	ReservationID                                string
	ReservedAmount                               int64
	Event                                        JournalEvent
}
type ReplayState struct {
	Sequence   int64
	Halted     bool
	HaltReason string
	Slots      map[int]SlotState
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
		var e JournalEvent
		if err := json.Unmarshal(s.Bytes(), &e); err != nil {
			return state, fmt.Errorf("pilot: torn/invalid journal at sequence %d: %w", state.Sequence+1, err)
		}
		if e.Sequence != state.Sequence+1 {
			return state, fmt.Errorf("pilot: journal sequence %d after %d", e.Sequence, state.Sequence)
		}
		if err := applyEvent(&state, e); err != nil {
			return state, err
		}
		state.Sequence = e.Sequence
	}
	if err := s.Err(); err != nil {
		return state, err
	}
	return state, nil
}

func applyEvent(s *ReplayState, e JournalEvent) error {
	if e.Type == EventHalt {
		s.Halted = true
		s.HaltReason = e.Error
		return nil
	}
	if e.SlotOrdinal <= 0 {
		return errors.New("pilot: journal event missing slot ordinal")
	}
	st := s.Slots[e.SlotOrdinal]
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
	case EventReconciled:
		if !st.Observed || st.Reconciled {
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
