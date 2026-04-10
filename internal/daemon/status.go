package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const maxRecentActions = 10

type StatusSnapshot struct {
	State                string               `json:"state"`
	Runner               string               `json:"runner"`
	ExecutionMode        string               `json:"executionMode,omitempty"`
	CurrentActor         string               `json:"currentActor,omitempty"`
	DelegatedActor       string               `json:"delegatedActor,omitempty"`
	PollIntervalSeconds  int                  `json:"pollIntervalSeconds"`
	MaxConcurrent        int                  `json:"maxConcurrent"`
	StartedAt            time.Time            `json:"startedAt,omitempty"`
	LastTickStartedAt    time.Time            `json:"lastTickStartedAt,omitempty"`
	LastTickCompletedAt  time.Time            `json:"lastTickCompletedAt,omitempty"`
	LastTickDurationMs   int64                `json:"lastTickDurationMs"`
	LastTickActionCount  int                  `json:"lastTickActionCount"`
	LastTickFailureCount int                  `json:"lastTickFailureCount"`
	TickCount            int                  `json:"tickCount"`
	Watches              []WatchSnapshot      `json:"watches,omitempty"`
	RecentActions        []RecordedTickAction `json:"recentActions,omitempty"`
	JobCounts            JobCountsSnapshot    `json:"jobCounts"`
	CurrentJob           *DaemonJobSnapshot   `json:"currentJob,omitempty"`
	LastCompletedJob     *DaemonJobSnapshot   `json:"lastCompletedJob,omitempty"`
	LastFailure          *DaemonJobSnapshot   `json:"lastFailure,omitempty"`
}

type WatchSnapshot struct {
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Action    string `json:"action,omitempty"`
	Threshold string `json:"threshold,omitempty"`
}

type RecordedTickAction struct {
	At      time.Time `json:"at"`
	Watch   string    `json:"watch"`
	Action  string    `json:"action"`
	Target  string    `json:"target"`
	Success bool      `json:"success"`
	Error   string    `json:"error,omitempty"`
}

type JobCountsSnapshot struct {
	Queued          int `json:"queued"`
	Running         int `json:"running"`
	Completed       int `json:"completed"`
	Failed          int `json:"failed"`
	AttentionNeeded int `json:"attentionNeeded"`
}

type DaemonJobSnapshot struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	Target         string    `json:"target"`
	Phase          string    `json:"phase"`
	State          string    `json:"state"`
	WorkspacePath  string    `json:"workspacePath,omitempty"`
	PrimaryActor   string    `json:"primaryActor,omitempty"`
	DelegatedActor string    `json:"delegatedActor,omitempty"`
	Summary        string    `json:"summary,omitempty"`
	ApplyOutcome   string    `json:"applyOutcome,omitempty"`
	ChangedFiles   []string  `json:"changedFiles,omitempty"`
	AppliedFiles   []string  `json:"appliedFiles,omitempty"`
	StartedAt      time.Time `json:"startedAt,omitempty"`
	CompletedAt    time.Time `json:"completedAt,omitempty"`
	RetryAt        time.Time `json:"retryAt,omitempty"`
	Error          string    `json:"error,omitempty"`
}

func statusFilePath(repoRoot string) string {
	return filepath.Join(repoRoot, ".plexium", "daemon-status.json")
}

func LoadStatusSnapshot(repoRoot string) (*StatusSnapshot, error) {
	data, err := os.ReadFile(statusFilePath(repoRoot))
	if err != nil {
		return nil, err
	}

	var snapshot StatusSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("daemon status: unmarshal: %w", err)
	}
	return &snapshot, nil
}

func saveStatusSnapshot(repoRoot string, snapshot *StatusSnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("daemon status: snapshot is nil")
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("daemon status: marshal: %w", err)
	}

	path := statusFilePath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("daemon status: mkdir: %w", err)
	}
	tmpPath := path + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("daemon status: write temp: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("daemon status: write temp: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("daemon status: sync temp: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("daemon status: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("daemon status: replace: %w", err)
	}
	if err := syncDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("daemon status: sync dir: %w", err)
	}
	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
