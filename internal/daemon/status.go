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
	State               string              `json:"state"`
	Runner              string              `json:"runner"`
	PollIntervalSeconds int                 `json:"pollIntervalSeconds"`
	MaxConcurrent       int                 `json:"maxConcurrent"`
	StartedAt           time.Time           `json:"startedAt,omitempty"`
	LastTickStartedAt   time.Time           `json:"lastTickStartedAt,omitempty"`
	LastTickCompletedAt time.Time           `json:"lastTickCompletedAt,omitempty"`
	LastTickDurationMs  int64               `json:"lastTickDurationMs"`
	LastTickActionCount int                 `json:"lastTickActionCount"`
	LastTickFailureCount int                `json:"lastTickFailureCount"`
	TickCount           int                 `json:"tickCount"`
	Watches             []WatchSnapshot     `json:"watches,omitempty"`
	RecentActions       []RecordedTickAction `json:"recentActions,omitempty"`
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
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("daemon status: write: %w", err)
	}
	return nil
}

