package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/internal/daemon"
)

func TestFormatAgentStatus_ShowsDaemonActivityAndUnlimitedBudget(t *testing.T) {
	status := &agentStatusPayload{
		Enabled:          true,
		BudgetConfigured: false,
		Providers: []providerStatusView{{
			Name:              "openrouter",
			Type:              "openai-compatible",
			Model:             "google/gemma-4-31b-it",
			CapabilityProfile: "balanced",
			Available:         true,
			Health:            "ready",
			Requests:          2,
			Tokens:            512,
			CostUSD:           0.0,
		}},
		Daemon: daemonStatusView{
			Running:              true,
			PID:                  1234,
			State:                "idle",
			Runner:               "claude",
			PollIntervalSeconds:  300,
			MaxConcurrent:        2,
			LastTickCompletedAt:  time.Now().Add(-15 * time.Second),
			LastTickActionCount:  1,
			LastTickFailureCount: 0,
			LastTickDurationMs:   12,
			Watches: []daemon.WatchSnapshot{
				{Name: "staleness", Enabled: true, Action: "auto-sync", Threshold: "7d"},
			},
			RecentActions: []daemon.RecordedTickAction{
				{At: time.Now().Add(-15 * time.Second), Watch: "staleness", Action: "auto-sync", Target: "page.md", Success: true},
			},
			Worktrees: daemonWorktreeSummary{Active: 0, Completed: 1, Failed: 0},
		},
	}

	rendered := formatAgentStatus(status)
	if !strings.Contains(rendered, "Daemon: running (PID 1234)") {
		t.Fatalf("expected daemon line, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Daemon runner: claude") {
		t.Fatalf("expected daemon runner line, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Activity: no active maintenance jobs right now") {
		t.Fatalf("expected activity summary, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Recent activity:") {
		t.Fatalf("expected recent activity section, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Daily provider budget: unlimited (not configured)") {
		t.Fatalf("expected unlimited budget line, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "usage today: 2 requests, 512 tokens, $0.0000") {
		t.Fatalf("expected provider usage line, got:\n%s", rendered)
	}
}

func TestSummarizeDaemonActivity_PassiveChecks(t *testing.T) {
	summary := summarizeDaemonActivity(daemonStatusView{
		Running: true,
		RecentActions: []daemon.RecordedTickAction{
			{Action: "log-only"},
			{Action: "log-only"},
		},
	})
	if summary != "no active maintenance jobs right now; recent ticks only ran passive checks" {
		t.Fatalf("unexpected summary: %s", summary)
	}
}

func TestSummarizeDaemonActivity_CurrentJob(t *testing.T) {
	summary := summarizeDaemonActivity(daemonStatusView{
		Running:        true,
		CurrentActor:   "openrouter",
		DelegatedActor: "codex",
		CurrentJob: &daemon.DaemonJobSnapshot{
			Type:   "bootstrap",
			Target: ".wiki/",
			Phase:  "documenting",
		},
	})
	if summary != "openrouter -> codex is documenting .wiki/" {
		t.Fatalf("unexpected summary: %s", summary)
	}
}
