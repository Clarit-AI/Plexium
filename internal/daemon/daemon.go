package daemon

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Clarit-AI/Plexium/internal/agent"
	"github.com/Clarit-AI/Plexium/internal/config"
)

// ---------------------------------------------------------------------------
// Configuration types
// ---------------------------------------------------------------------------

// DaemonOpts configures the daemon's poll loop and watch definitions.
type DaemonOpts struct {
	RepoRoot      string
	PollInterval  time.Duration // default 5m
	MaxConcurrent int           // default 2
	RunnerName    string
	ExecutionMode string
	Config        *config.Config
	Watches       WatchOpts
}

// WatchOpts groups the individual watch definitions.
type WatchOpts struct {
	Staleness WatchDef
	Lint      WatchDef
	Ingest    WatchDef
	Debt      WatchDef
}

// WatchDef defines a single watch: whether it is enabled, what action to take,
// and an optional threshold (e.g. "7d" for staleness, "10" for debt count).
type WatchDef struct {
	Enabled   bool
	Action    string // auto-sync | auto-fix | auto-ingest | create-issue | log-only
	Threshold string // e.g. "7d", "10"
}

// TickAction records one action taken during a daemon tick.
type TickAction struct {
	Watch   string `json:"watch"`  // staleness | lint | ingest | debt
	Action  string `json:"action"` // what was done
	Target  string `json:"target"` // file/page affected
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// Daemon
// ---------------------------------------------------------------------------

// Daemon ties together the workspace manager, issue tracker, and runner into
// an autonomous poll loop that detects staleness, lint issues, ingest
// candidates, and wiki-debt, then takes the configured action.
type Daemon struct {
	config        DaemonOpts
	workspace     *WorkspaceMgr
	tracker       TrackerAdapter
	runner        RunnerAdapter
	cascade       *agent.ProviderCascade
	rateTracker   *agent.RateLimitTracker
	pollInterval  time.Duration
	maxConcurrent int
	stopCh        chan struct{}
	stopOnce      sync.Once
}

// NewDaemon creates a Daemon with sensible defaults for zero-value fields.
func NewDaemon(opts DaemonOpts, workspace *WorkspaceMgr, tracker TrackerAdapter, runner RunnerAdapter, cascade *agent.ProviderCascade, rateTracker *agent.RateLimitTracker) *Daemon {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 5 * time.Minute
	}
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 2
	}
	return &Daemon{
		config:        opts,
		workspace:     workspace,
		tracker:       tracker,
		runner:        runner,
		cascade:       cascade,
		rateTracker:   rateTracker,
		pollInterval:  opts.PollInterval,
		maxConcurrent: opts.MaxConcurrent,
		stopCh:        make(chan struct{}),
	}
}

// Run starts the poll loop. It executes one tick immediately, then ticks on
// every PollInterval. It exits when ctx is cancelled or Stop() is called.
func (d *Daemon) Run(ctx context.Context) error {
	d.writeLifecycleSnapshot("running")
	d.runTick()

	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-d.stopCh:
			return nil
		case <-ticker.C:
			d.runTick()
		}
	}
}

// Stop signals the daemon to exit its Run loop.
func (d *Daemon) Stop() {
	d.stopOnce.Do(func() {
		close(d.stopCh)
	})
}

// tick runs all enabled watches and returns the actions taken.
func (d *Daemon) tick() []TickAction {
	jobs, actions := d.discoverJobs()
	if len(jobs) > 0 && d.canExecuteJobs() {
		actions = append(actions, d.executeJob(context.Background(), jobs[0]))
	}
	return actions
}

func (d *Daemon) runTick() {
	startedAt := time.Now()
	d.writeTickStarted(startedAt)
	actions := d.tick()
	d.writeTickCompleted(startedAt, actions)
}

// countDebtEntries counts lines containing "WIKI-DEBT" in the given file.
func countDebtEntries(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "WIKI-DEBT") {
			count++
		}
	}
	return count, scanner.Err()
}

// ---------------------------------------------------------------------------
// Action dispatch
// ---------------------------------------------------------------------------

// handleAction dispatches the configured action for a given watch finding.
// For create-issue it uses the TrackerAdapter; for auto-* actions it uses the
// runner within a workspace (respecting maxConcurrent).
func (d *Daemon) handleAction(watch, action, target string) TickAction {
	ta := TickAction{Watch: watch, Action: action, Target: target}

	switch action {
	case "log-only":
		ta.Success = true

	case "create-issue":
		title := fmt.Sprintf("[plexium/%s] %s", watch, target)
		body := fmt.Sprintf("Daemon detected %s issue: %s", watch, target)
		_, err := d.tracker.CreateIssue(title, body)
		if err != nil {
			ta.Success = false
			ta.Error = err.Error()
		} else {
			ta.Success = true
		}

	case "auto-sync", "auto-fix", "auto-ingest":
		active, err := d.workspace.ActiveCount()
		if err != nil {
			ta.Success = false
			ta.Error = fmt.Sprintf("workspace active count: %v", err)
			return ta
		}
		if active >= d.maxConcurrent {
			ta.Success = false
			ta.Error = fmt.Sprintf("max concurrent workspaces reached (%d/%d)", active, d.maxConcurrent)
			return ta
		}

		issueID := fmt.Sprintf("daemon-%s-%d", watch, time.Now().UnixMilli())
		wt, err := d.workspace.Create(issueID)
		if err != nil {
			ta.Success = false
			ta.Error = fmt.Sprintf("workspace create: %v", err)
			return ta
		}

		prompt := fmt.Sprintf("Run %s on target: %s", action, target)
		_, runErr := d.runner.Run(context.Background(), watch, prompt, nil, wt.Path)

		if runErr != nil {
			_ = d.workspace.UpdateStatus(wt.ID, "failed")
			ta.Success = false
			ta.Error = runErr.Error()
		} else {
			_ = d.workspace.UpdateStatus(wt.ID, "completed")
			ta.Success = true
		}

		_ = d.workspace.Cleanup(wt.ID)

	default:
		ta.Success = false
		ta.Error = fmt.Sprintf("unknown action: %s", action)
	}

	return ta
}

// ---------------------------------------------------------------------------
// Threshold parsing helpers
// ---------------------------------------------------------------------------

// parseDuration parses a threshold string like "7d", "24h", "30m" into a
// time.Duration. Falls back to defaultVal on parse failure.
func parseDuration(s string, defaultVal time.Duration) time.Duration {
	if s == "" {
		return defaultVal
	}

	// Handle day suffix (not natively supported by time.ParseDuration).
	if strings.HasSuffix(s, "d") {
		trimmed := strings.TrimSuffix(s, "d")
		var days int
		if _, err := fmt.Sscanf(trimmed, "%d", &days); err == nil && days > 0 {
			return time.Duration(days) * 24 * time.Hour
		}
		return defaultVal
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultVal
	}
	return d
}

// parseIntThreshold parses a string as an integer. Falls back to defaultVal.
func parseIntThreshold(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err == nil && n > 0 {
		return n
	}
	return defaultVal
}

func (d *Daemon) writeLifecycleSnapshot(state string) {
	snapshot := d.loadOrInitSnapshot()
	if snapshot.StartedAt.IsZero() {
		snapshot.StartedAt = time.Now()
	}
	d.applySnapshotDefaults(snapshot)
	snapshot.State = state
	d.refreshJobCounts(snapshot)
	_ = saveStatusSnapshot(d.config.RepoRoot, snapshot)
}

func (d *Daemon) writeTickStarted(startedAt time.Time) {
	snapshot := d.loadOrInitSnapshot()
	if snapshot.StartedAt.IsZero() {
		snapshot.StartedAt = startedAt
	}
	d.applySnapshotDefaults(snapshot)
	snapshot.State = "ticking"
	snapshot.LastTickStartedAt = startedAt
	d.refreshJobCounts(snapshot)
	_ = saveStatusSnapshot(d.config.RepoRoot, snapshot)
}

func (d *Daemon) writeTickCompleted(startedAt time.Time, actions []TickAction) {
	snapshot := d.loadOrInitSnapshot()

	completedAt := time.Now()
	failures := 0
	recorded := make([]RecordedTickAction, 0, len(actions))
	for _, action := range actions {
		if !action.Success {
			failures++
		}
		recorded = append(recorded, RecordedTickAction{
			At:      completedAt,
			Watch:   action.Watch,
			Action:  action.Action,
			Target:  action.Target,
			Success: action.Success,
			Error:   action.Error,
		})
	}

	d.applySnapshotDefaults(snapshot)
	snapshot.State = "idle"
	if snapshot.StartedAt.IsZero() {
		snapshot.StartedAt = startedAt
	}
	snapshot.LastTickStartedAt = startedAt
	snapshot.LastTickCompletedAt = completedAt
	snapshot.LastTickDurationMs = completedAt.Sub(startedAt).Milliseconds()
	snapshot.LastTickActionCount = len(actions)
	snapshot.LastTickFailureCount = failures
	snapshot.TickCount++
	snapshot.RecentActions = append(recorded, snapshot.RecentActions...)
	if len(snapshot.RecentActions) > maxRecentActions {
		snapshot.RecentActions = snapshot.RecentActions[:maxRecentActions]
	}
	d.refreshJobCounts(snapshot)
	_ = saveStatusSnapshot(d.config.RepoRoot, snapshot)
}

func (d *Daemon) loadOrInitSnapshot() *StatusSnapshot {
	snapshot, _ := LoadStatusSnapshot(d.config.RepoRoot)
	if snapshot == nil {
		snapshot = &StatusSnapshot{}
	}
	return snapshot
}

func (d *Daemon) canExecuteJobs() bool {
	switch normalizeExecutionMode(d.config.ExecutionMode) {
	case executionModeProviderPrimary:
		return d.cascade != nil
	default:
		return d.runner != nil && strings.TrimSpace(d.config.RunnerName) != "" && d.config.RunnerName != "noop"
	}
}

func (d *Daemon) applySnapshotDefaults(snapshot *StatusSnapshot) {
	snapshot.Runner = d.config.RunnerName
	snapshot.ExecutionMode = normalizeExecutionMode(d.config.ExecutionMode)
	snapshot.PollIntervalSeconds = int(d.pollInterval / time.Second)
	snapshot.MaxConcurrent = d.maxConcurrent
	snapshot.Watches = []WatchSnapshot{
		{Name: "staleness", Enabled: d.config.Watches.Staleness.Enabled, Action: d.config.Watches.Staleness.Action, Threshold: d.config.Watches.Staleness.Threshold},
		{Name: "lint", Enabled: d.config.Watches.Lint.Enabled, Action: d.config.Watches.Lint.Action, Threshold: d.config.Watches.Lint.Threshold},
		{Name: "ingest", Enabled: d.config.Watches.Ingest.Enabled, Action: d.config.Watches.Ingest.Action, Threshold: d.config.Watches.Ingest.Threshold},
		{Name: "debt", Enabled: d.config.Watches.Debt.Enabled, Action: d.config.Watches.Debt.Action, Threshold: d.config.Watches.Debt.Threshold},
	}
}

func (d *Daemon) refreshJobCounts(snapshot *StatusSnapshot) {
	worktrees, err := d.workspace.List()
	if err != nil {
		return
	}
	counts := JobCountsSnapshot{}
	for _, wt := range worktrees {
		switch wt.Status {
		case "running":
			counts.Running++
		case "completed":
			counts.Completed++
		case "failed":
			counts.Failed++
		case jobStateAttentionNeeded:
			counts.AttentionNeeded++
		}
	}
	if snapshot.CurrentJob != nil && snapshot.CurrentJob.State == jobStateRunning && counts.Running == 0 {
		counts.Running = 1
	}
	snapshot.JobCounts = counts
}

func cloneJobSnapshot(job *DaemonJobSnapshot) *DaemonJobSnapshot {
	if job == nil {
		return nil
	}
	cp := *job
	if job.ChangedFiles != nil {
		cp.ChangedFiles = append([]string{}, job.ChangedFiles...)
	}
	if job.AppliedFiles != nil {
		cp.AppliedFiles = append([]string{}, job.AppliedFiles...)
	}
	return &cp
}

func (d *Daemon) persistCurrentJob(job *DaemonJobSnapshot) {
	snapshot := d.loadOrInitSnapshot()
	d.applySnapshotDefaults(snapshot)
	snapshot.State = "working"
	snapshot.CurrentActor = job.PrimaryActor
	snapshot.DelegatedActor = job.DelegatedActor
	snapshot.CurrentJob = cloneJobSnapshot(job)
	d.refreshJobCounts(snapshot)
	_ = saveStatusSnapshot(d.config.RepoRoot, snapshot)
}

func (d *Daemon) persistJobPhase(job *DaemonJobSnapshot, phase string) {
	job.Phase = phase
	snapshot := d.loadOrInitSnapshot()
	d.applySnapshotDefaults(snapshot)
	snapshot.State = "working"
	snapshot.CurrentActor = job.PrimaryActor
	snapshot.DelegatedActor = job.DelegatedActor
	snapshot.CurrentJob = cloneJobSnapshot(job)
	d.refreshJobCounts(snapshot)
	_ = saveStatusSnapshot(d.config.RepoRoot, snapshot)
}

func (d *Daemon) persistCompletedJob(job *DaemonJobSnapshot) {
	snapshot := d.loadOrInitSnapshot()
	d.applySnapshotDefaults(snapshot)
	snapshot.State = "idle"
	snapshot.CurrentActor = job.PrimaryActor
	snapshot.DelegatedActor = job.DelegatedActor
	snapshot.CurrentJob = nil
	snapshot.LastCompletedJob = cloneJobSnapshot(job)
	d.refreshJobCounts(snapshot)
	_ = saveStatusSnapshot(d.config.RepoRoot, snapshot)
}

func (d *Daemon) persistFailedJob(job *DaemonJobSnapshot) {
	snapshot := d.loadOrInitSnapshot()
	d.applySnapshotDefaults(snapshot)
	snapshot.State = "idle"
	snapshot.CurrentActor = job.PrimaryActor
	snapshot.DelegatedActor = job.DelegatedActor
	snapshot.CurrentJob = nil
	snapshot.LastFailure = cloneJobSnapshot(job)
	d.refreshJobCounts(snapshot)
	_ = saveStatusSnapshot(d.config.RepoRoot, snapshot)
}

func (d *Daemon) persistAttentionJob(job *DaemonJobSnapshot) {
	snapshot := d.loadOrInitSnapshot()
	d.applySnapshotDefaults(snapshot)
	snapshot.State = "idle"
	snapshot.CurrentActor = job.PrimaryActor
	snapshot.DelegatedActor = job.DelegatedActor
	snapshot.CurrentJob = nil
	snapshot.LastCompletedJob = cloneJobSnapshot(job)
	d.refreshJobCounts(snapshot)
	_ = saveStatusSnapshot(d.config.RepoRoot, snapshot)
}
