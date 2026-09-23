package pilot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
)

// ObservationEvidence is the private, replayable subset of an adapter
// observation. Raw response bytes are kept in their own bounded file.
type ObservationEvidence struct {
	Status            int                        `json:"status"`
	RequestID         string                     `json:"requestId,omitempty"`
	ResponseModel     string                     `json:"responseModel,omitempty"`
	ResponseProvider  string                     `json:"responseProvider,omitempty"`
	ResponseHeaders   map[string]string          `json:"responseHeaders,omitempty"`
	RequestSHA256     string                     `json:"requestSha256"`
	RequestSent       bool                       `json:"requestSent"`
	ResponseReceived  bool                       `json:"responseReceived"`
	RetryAfter        string                     `json:"retryAfter,omitempty"`
	RawSHA256         string                     `json:"rawSha256"`
	OriginalRawSHA256 string                     `json:"originalRawSha256"`
	EvidenceSanitized bool                       `json:"evidenceSanitized"`
	RawWithheld       bool                       `json:"rawWithheld"`
	EvidenceReason    string                     `json:"evidenceReason,omitempty"`
	RawTruncated      bool                       `json:"rawTruncated"`
	ReadError         string                     `json:"readError,omitempty"`
	StartedAt         time.Time                  `json:"startedAt"`
	EndedAt           time.Time                  `json:"endedAt"`
	DurationNanos     int64                      `json:"durationNanos"`
	Billing           adapter.BillingObservation `json:"billing"`
	Label             string                     `json:"label,omitempty"`
	Confidence        *float64                   `json:"confidence,omitempty"`
	Probabilities     map[string]float64         `json:"probabilities,omitempty"`
}

func evidenceFromObservation(obs adapter.AttemptObservation, raw screenedRaw) ObservationEvidence {
	e := ObservationEvidence{
		Status: obs.Status, RequestID: obs.RequestID, ResponseModel: obs.ResponseModel,
		ResponseProvider: obs.ResponseProvider, ResponseHeaders: obs.ResponseHeaders,
		RequestSHA256: obs.RequestSHA256, RequestSent: obs.RequestSent,
		ResponseReceived: obs.ResponseReceived, RetryAfter: obs.RetryAfter,
		RawSHA256: hashBytes(obs.RawResponse), OriginalRawSHA256: raw.OriginalSHA256,
		EvidenceSanitized: raw.Sanitized, RawWithheld: raw.Withheld, EvidenceReason: raw.Reason,
		RawTruncated: obs.RawTruncated,
		ReadError:    obs.ReadError, StartedAt: obs.StartedAt, EndedAt: obs.EndedAt,
		DurationNanos: int64(obs.Duration), Billing: obs.Billing,
	}
	if obs.Decision != nil {
		e.Label = obs.Decision.Choice
		e.Confidence = obs.Decision.Confidence
		e.Probabilities = obs.Decision.Probabilities
	}
	if obs.Chat != nil {
		if label, ok := obs.Chat.Content["label"].(string); ok {
			e.Label = label
		}
	}
	return e
}

func persistPrivateFile(dir, name string, data []byte) (string, string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return "", "", err
	}
	full := filepath.Join(dir, name)
	f, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", "", err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", "", err
	}
	if closeErr != nil {
		return "", "", closeErr
	}
	if err := syncDirectory(dir); err != nil {
		return "", "", err
	}
	return name, hashBytes(data), nil
}

func persistAttemptEvidence(dir string, slot Slot, obs adapter.AttemptObservation, raw screenedRaw) (rawPath, rawHash, observationPath, observationHash string, err error) {
	prefix := fmt.Sprintf("%04d-%s-%s", slot.Ordinal, slot.FixtureID, slot.Arm)
	rawPath, rawHash, err = persistPrivateFile(dir, prefix+".response", obs.RawResponse)
	if err != nil {
		return "", "", "", "", err
	}
	evidence := evidenceFromObservation(obs, raw)
	evidence.RawSHA256 = rawHash
	b, err := json.Marshal(evidence)
	if err != nil {
		return "", "", "", "", err
	}
	observationPath, observationHash, err = persistPrivateFile(dir, prefix+".observation.json", b)
	return
}

func loadAttemptEvidence(dir string, event JournalEvent) (*ObservationEvidence, error) {
	if event.ResponsePath == "" || event.ResponseSHA == "" || event.ObservationPath == "" || event.ObservationSHA == "" {
		return nil, fmt.Errorf("pilot: recorded observation is missing evidence bindings")
	}
	raw, err := os.ReadFile(filepath.Join(dir, event.ResponsePath))
	if err != nil {
		return nil, fmt.Errorf("read raw evidence %s: %w", event.ResponsePath, err)
	}
	if hashBytes(raw) != event.ResponseSHA {
		return nil, fmt.Errorf("raw evidence hash mismatch for %s", event.ResponsePath)
	}
	b, err := os.ReadFile(filepath.Join(dir, event.ObservationPath))
	if err != nil {
		return nil, fmt.Errorf("read observation evidence %s: %w", event.ObservationPath, err)
	}
	if hashBytes(b) != event.ObservationSHA {
		return nil, fmt.Errorf("observation evidence hash mismatch for %s", event.ObservationPath)
	}
	var evidence ObservationEvidence
	if err := json.Unmarshal(b, &evidence); err != nil {
		return nil, fmt.Errorf("decode observation evidence: %w", err)
	}
	if evidence.RawSHA256 != event.ResponseSHA {
		return nil, fmt.Errorf("observation/raw evidence binding mismatch")
	}
	return &evidence, nil
}
