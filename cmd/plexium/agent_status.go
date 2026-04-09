package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/internal/agent"
	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/daemon"
)

type providerStatusView struct {
	Name              string  `json:"name"`
	Type              string  `json:"type"`
	Model             string  `json:"model,omitempty"`
	CapabilityProfile string  `json:"capabilityProfile,omitempty"`
	Available         bool    `json:"available"`
	Health            string  `json:"health"`
	CostUSD           float64 `json:"dailyCostUSD"`
	Requests          int     `json:"dailyRequests"`
	Tokens            int     `json:"dailyTokens"`
}

type daemonWorktreeSummary struct {
	Active    int `json:"active"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Total     int `json:"total"`
}

type daemonStatusView struct {
	Running              bool                        `json:"running"`
	PID                  int                         `json:"pid,omitempty"`
	State                string                      `json:"state,omitempty"`
	Runner               string                      `json:"runner,omitempty"`
	PollIntervalSeconds  int                         `json:"pollIntervalSeconds,omitempty"`
	MaxConcurrent        int                         `json:"maxConcurrent,omitempty"`
	TickCount            int                         `json:"tickCount,omitempty"`
	LastTickStartedAt    time.Time                   `json:"lastTickStartedAt,omitempty"`
	LastTickCompletedAt  time.Time                   `json:"lastTickCompletedAt,omitempty"`
	LastTickDurationMs   int64                       `json:"lastTickDurationMs,omitempty"`
	LastTickActionCount  int                         `json:"lastTickActionCount,omitempty"`
	LastTickFailureCount int                         `json:"lastTickFailureCount,omitempty"`
	Watches              []daemon.WatchSnapshot      `json:"watches,omitempty"`
	RecentActions        []daemon.RecordedTickAction `json:"recentActions,omitempty"`
	Worktrees            daemonWorktreeSummary       `json:"worktrees"`
}

type agentStatusPayload struct {
	Enabled          bool                 `json:"enabled"`
	BudgetConfigured bool                 `json:"budgetConfigured"`
	BudgetUSD        float64              `json:"dailyBudgetUSD"`
	Providers        []providerStatusView `json:"providers"`
	Daemon           daemonStatusView     `json:"daemon"`
}

func collectAgentStatus(repoRoot string, cfg *config.Config) (*agentStatusPayload, error) {
	tracker := agent.NewRateLimitTracker(filepath.Join(repoRoot, ".plexium", "agent-state.json"))
	status := &agentStatusPayload{
		Enabled:          cfg != nil && cfg.AssistiveAgent.Enabled,
		BudgetConfigured: cfg != nil && cfg.AssistiveAgent.Budget.DailyUSD > 0,
	}
	if cfg != nil {
		status.BudgetUSD = cfg.AssistiveAgent.Budget.DailyUSD
		status.Providers = buildProviderStatuses(cfg, tracker)
	}
	status.Daemon = buildDaemonStatusView(repoRoot, cfg)
	return status, nil
}

func buildProviderStatuses(cfg *config.Config, tracker *agent.RateLimitTracker) []providerStatusView {
	if cfg == nil {
		return nil
	}

	var statuses []providerStatusView
	for _, pc := range cfg.AssistiveAgent.Providers {
		if !pc.Enabled {
			continue
		}
		usage, _ := tracker.GetDailyUsage(pc.Name)
		health := "ready"
		available := true
		if err := providerHealth(pc); err != nil {
			available = false
			health = err.Error()
		}
		statuses = append(statuses, providerStatusView{
			Name:              pc.Name,
			Type:              pc.Type,
			Model:             pc.Model,
			CapabilityProfile: pc.CapabilityProfile,
			Available:         available,
			Health:            health,
			CostUSD:           usage.CostUSD,
			Requests:          usage.Requests,
			Tokens:            usage.Tokens,
		})
	}
	return statuses
}

func providerHealth(pc config.ProviderConfig) error {
	switch pc.Type {
	case "ollama":
		return agent.NewOllamaProvider(pc.Endpoint, pc.Model, agent.DefaultOllamaHTTPPost).HealthCheck()
	case "openai-compatible":
		apiKey := loadAPIKey(pc.APIKeyEnv)
		return agent.NewOpenRouterProvider(pc.Endpoint, pc.Model, apiKey, 0, agent.DefaultOpenRouterHTTPPost).HealthCheck()
	case "inherit":
		return (&agent.InheritProvider{}).HealthCheck()
	default:
		return fmt.Errorf("unknown provider type %q", pc.Type)
	}
}

func buildDaemonStatusView(repoRoot string, cfg *config.Config) daemonStatusView {
	view := daemonStatusView{}
	pidFile := filepath.Join(repoRoot, ".plexium", "daemon.pid")
	if pid, err := readPIDFile(pidFile); err == nil && processAlive(pid) {
		view.Running = true
		view.PID = pid
	}

	if snapshot, err := daemon.LoadStatusSnapshot(repoRoot); err == nil {
		view.State = snapshot.State
		view.Runner = snapshot.Runner
		view.PollIntervalSeconds = snapshot.PollIntervalSeconds
		view.MaxConcurrent = snapshot.MaxConcurrent
		view.TickCount = snapshot.TickCount
		view.LastTickStartedAt = snapshot.LastTickStartedAt
		view.LastTickCompletedAt = snapshot.LastTickCompletedAt
		view.LastTickDurationMs = snapshot.LastTickDurationMs
		view.LastTickActionCount = snapshot.LastTickActionCount
		view.LastTickFailureCount = snapshot.LastTickFailureCount
		view.Watches = snapshot.Watches
		view.RecentActions = snapshot.RecentActions
	}

	if cfg != nil {
		if view.Runner == "" {
			view.Runner = cfg.Daemon.Runner
		}
		if view.PollIntervalSeconds == 0 {
			view.PollIntervalSeconds = cfg.Daemon.PollInterval
		}
		if view.MaxConcurrent == 0 {
			view.MaxConcurrent = cfg.Daemon.MaxConcurrent
		}
		if len(view.Watches) == 0 {
			view.Watches = []daemon.WatchSnapshot{
				{Name: "staleness", Enabled: cfg.Daemon.Watches.Staleness.Enabled, Action: cfg.Daemon.Watches.Staleness.Action, Threshold: cfg.Daemon.Watches.Staleness.Threshold},
				{Name: "lint", Enabled: cfg.Daemon.Watches.Lint.Enabled, Action: cfg.Daemon.Watches.Lint.Action, Threshold: cfg.Daemon.Watches.Lint.Threshold},
				{Name: "ingest", Enabled: cfg.Daemon.Watches.Ingest.Enabled, Action: cfg.Daemon.Watches.Ingest.Action, Threshold: cfg.Daemon.Watches.Ingest.Threshold},
				{Name: "debt", Enabled: cfg.Daemon.Watches.Debt.Enabled, Action: cfg.Daemon.Watches.Debt.Action, Threshold: cfg.Daemon.Watches.Debt.Threshold},
			}
		}
	}
	if view.Runner == "" {
		view.Runner = "noop"
	}
	if view.PollIntervalSeconds == 0 {
		view.PollIntervalSeconds = 300
	}
	if view.MaxConcurrent == 0 {
		view.MaxConcurrent = 2
	}

	worktrees, err := daemon.NewWorkspaceMgr(repoRoot).List()
	if err == nil {
		view.Worktrees.Total = len(worktrees)
		for _, wt := range worktrees {
			switch wt.Status {
			case "running":
				view.Worktrees.Active++
			case "completed":
				view.Worktrees.Completed++
			case "failed":
				view.Worktrees.Failed++
			}
		}
	}

	if view.Running && view.Worktrees.Active > 0 {
		view.State = "working"
	} else if view.Running && view.State == "" {
		view.State = "idle"
	}

	return view
}

func formatAgentStatus(status *agentStatusPayload) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Assistive Agent: %s\n", map[bool]string{true: "enabled", false: "disabled"}[status.Enabled])

	if status.Daemon.Running {
		fmt.Fprintf(&b, "Daemon: running (PID %d)\n", status.Daemon.PID)
	} else {
		b.WriteString("Daemon: stopped\n")
	}
	if status.Daemon.Runner != "" {
		fmt.Fprintf(&b, "  Daemon runner: %s\n", status.Daemon.Runner)
	}
	if status.Daemon.State != "" {
		fmt.Fprintf(&b, "  State: %s\n", status.Daemon.State)
	}
	fmt.Fprintf(&b, "  Activity: %s\n", summarizeDaemonActivity(status.Daemon))
	if status.Daemon.PollIntervalSeconds > 0 || status.Daemon.MaxConcurrent > 0 {
		fmt.Fprintf(&b, "  Cadence: every %s, max %d concurrent worktrees\n", time.Duration(status.Daemon.PollIntervalSeconds)*time.Second, status.Daemon.MaxConcurrent)
	}
	fmt.Fprintf(&b, "  Worktrees: %d active, %d completed, %d failed\n", status.Daemon.Worktrees.Active, status.Daemon.Worktrees.Completed, status.Daemon.Worktrees.Failed)
	if !status.Daemon.LastTickCompletedAt.IsZero() {
		fmt.Fprintf(&b, "  Last tick: %s (%d actions, %d failures, %dms)\n",
			humanizeTimeAgo(status.Daemon.LastTickCompletedAt),
			status.Daemon.LastTickActionCount,
			status.Daemon.LastTickFailureCount,
			status.Daemon.LastTickDurationMs,
		)
	} else {
		b.WriteString("  Last tick: no daemon activity recorded yet\n")
	}
	if watches := formatWatchSummary(status.Daemon.Watches); watches != "" {
		fmt.Fprintf(&b, "  Watches: %s\n", watches)
	}
	if len(status.Daemon.RecentActions) > 0 {
		b.WriteString("  Recent activity:\n")
		for _, action := range status.Daemon.RecentActions {
			line := fmt.Sprintf("    %s — %s %s %s", humanizeTimeAgo(action.At), action.Watch, action.Action, action.Target)
			if action.Success {
				line += " (ok)"
			} else {
				line += fmt.Sprintf(" (failed: %s)", action.Error)
			}
			b.WriteString(line + "\n")
		}
	} else {
		b.WriteString("  Recent activity: none recorded yet\n")
	}

	if status.BudgetConfigured {
		totalCost := 0.0
		for _, provider := range status.Providers {
			totalCost += provider.CostUSD
		}
		pct := 0.0
		if status.BudgetUSD > 0 {
			pct = (totalCost / status.BudgetUSD) * 100
		}
		fmt.Fprintf(&b, "\nDaily provider budget: $%.2f configured (%.1f%% used)\n", status.BudgetUSD, pct)
	} else {
		b.WriteString("\nDaily provider budget: unlimited (not configured)\n")
	}

	b.WriteString("\nProviders:\n")
	if len(status.Providers) == 0 {
		b.WriteString("  none configured\n")
		return b.String()
	}
	for _, provider := range status.Providers {
		fmt.Fprintf(&b, "  %s: available=%v, health=%s, model=%s, profile=%s\n",
			provider.Name,
			provider.Available,
			provider.Health,
			emptyIfUnset(provider.Model),
			emptyIfUnset(provider.CapabilityProfile),
		)
		fmt.Fprintf(&b, "    usage today: %d requests, %d tokens, $%.4f\n", provider.Requests, provider.Tokens, provider.CostUSD)
	}

	return b.String()
}

func summarizeDaemonActivity(status daemonStatusView) string {
	if !status.Running {
		return "daemon is not running"
	}
	if status.Worktrees.Active > 0 {
		return fmt.Sprintf("processing %d active worktree(s)", status.Worktrees.Active)
	}
	if len(status.RecentActions) == 0 {
		if status.LastTickCompletedAt.IsZero() {
			return "waiting for the first daemon tick"
		}
		return "no active maintenance jobs right now"
	}
	passiveOnly := true
	for _, action := range status.RecentActions {
		if action.Action != "log-only" {
			passiveOnly = false
			break
		}
	}
	if passiveOnly {
		return "no active maintenance jobs right now; recent ticks only ran passive checks"
	}
	return "no active maintenance jobs right now"
}

func formatWatchSummary(watches []daemon.WatchSnapshot) string {
	if len(watches) == 0 {
		return "none enabled"
	}
	parts := make([]string, 0, len(watches))
	for _, watch := range watches {
		if !watch.Enabled {
			continue
		}
		part := fmt.Sprintf("%s=%s", watch.Name, watch.Action)
		if watch.Threshold != "" {
			part += fmt.Sprintf(" (%s)", watch.Threshold)
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "none enabled"
	}
	return strings.Join(parts, ", ")
}

func humanizeTimeAgo(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t).Round(time.Second)
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%s ago", d)
}

func emptyIfUnset(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unset"
	}
	return value
}

func marshalAgentStatus(status *agentStatusPayload) string {
	data, _ := json.MarshalIndent(status, "", "  ")
	return string(data)
}
