package daemon

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Configuration types
// ---------------------------------------------------------------------------

// DaemonOpts configures the daemon's poll loop and watch definitions.
type DaemonOpts struct {
	RepoRoot      string
	PollInterval  time.Duration // default 5m
	MaxConcurrent int           // default 2
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
	Watch   string `json:"watch"`            // staleness | lint | ingest | debt
	Action  string `json:"action"`           // what was done
	Target  string `json:"target"`           // file/page affected
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
	pollInterval  time.Duration
	maxConcurrent int
	stopCh        chan struct{}
}

// NewDaemon creates a Daemon with sensible defaults for zero-value fields.
func NewDaemon(opts DaemonOpts, workspace *WorkspaceMgr, tracker TrackerAdapter, runner RunnerAdapter) *Daemon {
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
		pollInterval:  opts.PollInterval,
		maxConcurrent: opts.MaxConcurrent,
		stopCh:        make(chan struct{}),
	}
}

// Run starts the poll loop. It executes one tick immediately, then ticks on
// every PollInterval. It exits when ctx is cancelled or Stop() is called.
func (d *Daemon) Run(ctx context.Context) error {
	// Initial tick.
	d.tick()

	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-d.stopCh:
			return nil
		case <-ticker.C:
			d.tick()
		}
	}
}

// Stop signals the daemon to exit its Run loop.
func (d *Daemon) Stop() {
	select {
	case d.stopCh <- struct{}{}:
	default:
	}
}

// tick runs all enabled watches and returns the actions taken.
func (d *Daemon) tick() []TickAction {
	var actions []TickAction

	if d.config.Watches.Staleness.Enabled {
		actions = append(actions, d.checkStaleness()...)
	}
	if d.config.Watches.Lint.Enabled {
		actions = append(actions, d.checkLint()...)
	}
	if d.config.Watches.Ingest.Enabled {
		actions = append(actions, d.checkIngest()...)
	}
	if d.config.Watches.Debt.Enabled {
		actions = append(actions, d.checkDebt()...)
	}

	return actions
}

// ---------------------------------------------------------------------------
// Watch: staleness
// ---------------------------------------------------------------------------

// checkStaleness scans .wiki/ for markdown files whose modification time
// exceeds the configured threshold.
func (d *Daemon) checkStaleness() []TickAction {
	threshold := parseDuration(d.config.Watches.Staleness.Threshold, 7*24*time.Hour)
	cutoff := time.Now().Add(-threshold)
	wikiRoot := filepath.Join(d.config.RepoRoot, ".wiki")

	var actions []TickAction

	entries, err := os.ReadDir(wikiRoot)
	if err != nil {
		return []TickAction{{
			Watch: "staleness", Action: "scan", Target: wikiRoot,
			Success: false, Error: fmt.Sprintf("readdir: %v", err),
		}}
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if strings.HasPrefix(e.Name(), "_") {
			continue // skip control files (_schema.md, _index.md, _log.md)
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			action := d.handleAction("staleness", d.config.Watches.Staleness.Action, e.Name())
			actions = append(actions, action)
		}
	}

	return actions
}

// ---------------------------------------------------------------------------
// Watch: lint (stub — delegates to runner)
// ---------------------------------------------------------------------------

func (d *Daemon) checkLint() []TickAction {
	// In a full implementation this would run `plexium lint --deterministic`
	// and parse the output. For now it logs the intent.
	return []TickAction{{
		Watch:   "lint",
		Action:  d.config.Watches.Lint.Action,
		Target:  ".wiki/",
		Success: true,
	}}
}

// ---------------------------------------------------------------------------
// Watch: ingest
// ---------------------------------------------------------------------------

// checkIngest looks for files in .wiki/raw/ that need ingestion.
func (d *Daemon) checkIngest() []TickAction {
	rawDir := filepath.Join(d.config.RepoRoot, ".wiki", "raw")
	entries, err := os.ReadDir(rawDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no raw dir → nothing to ingest
		}
		return []TickAction{{
			Watch: "ingest", Action: "scan", Target: rawDir,
			Success: false, Error: fmt.Sprintf("readdir: %v", err),
		}}
	}

	var actions []TickAction
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		action := d.handleAction("ingest", d.config.Watches.Ingest.Action, e.Name())
		actions = append(actions, action)
	}
	return actions
}

// ---------------------------------------------------------------------------
// Watch: debt
// ---------------------------------------------------------------------------

// checkDebt counts lines containing "WIKI-DEBT" in _log.md and fires if the
// count exceeds the threshold.
func (d *Daemon) checkDebt() []TickAction {
	logPath := filepath.Join(d.config.RepoRoot, ".wiki", "_log.md")
	count, err := countDebtEntries(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []TickAction{{
			Watch: "debt", Action: "scan", Target: logPath,
			Success: false, Error: fmt.Sprintf("read log: %v", err),
		}}
	}

	maxDebt := parseIntThreshold(d.config.Watches.Debt.Threshold, 10)
	if count >= maxDebt {
		action := d.handleAction("debt", d.config.Watches.Debt.Action,
			fmt.Sprintf("WIKI-DEBT count=%d (threshold=%d)", count, maxDebt))
		return []TickAction{action}
	}

	return nil
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
		_, runErr := d.runner.Run(context.Background(), watch, prompt, nil)

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
