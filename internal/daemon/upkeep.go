package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/convert"
	"github.com/Clarit-AI/Plexium/internal/integrations/pageindex"
	"github.com/Clarit-AI/Plexium/internal/lint"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	plexsync "github.com/Clarit-AI/Plexium/internal/sync"
)

const (
	executionModeCodingAgentPrimary = "coding-agent-primary"
	executionModeProviderPrimary    = "provider-primary"

	jobTypeBootstrap = "bootstrap"
	jobTypeRepoDrift = "repo-drift"
	jobTypeRawIngest = "raw-ingest"
	jobTypeDebt      = "debt"
	jobTypeLint      = "lint"

	jobStateQueued          = "queued"
	jobStateRunning         = "running"
	jobStateCompleted       = "completed"
	jobStateFailed          = "failed"
	jobStateAttentionNeeded = "attention_needed"

	jobPhaseQueued      = "queued"
	jobPhasePreparing   = "preparing"
	jobPhaseRetrieving  = "retrieving"
	jobPhasePlanning    = "planning"
	jobPhaseDocumenting = "documenting"
	jobPhaseValidating  = "validating"
	jobPhaseApplying    = "applying"
	jobPhaseCompleted   = "completed"
)

type upkeepJob struct {
	ID      string
	Type    string
	Target  string
	Reason  string
	Payload string
}

type providerWriteFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type providerExecutionPlan struct {
	Summary        string              `json:"summary"`
	DelegateToCLI  bool                `json:"delegateToCli"`
	DelegatePrompt string              `json:"delegatePrompt"`
	Files          []providerWriteFile `json:"files"`
}

func (d *Daemon) discoverJobs() ([]*upkeepJob, []TickAction) {
	var jobs []*upkeepJob
	var actions []TickAction

	if d.hasEnabledWatches() && d.needsBootstrap() {
		jobs = append(jobs, &upkeepJob{
			ID:     fmt.Sprintf("bootstrap-%d", time.Now().UnixMilli()),
			Type:   jobTypeBootstrap,
			Target: ".wiki/",
			Reason: "wiki is missing, minimal, or still scaffold-level",
		})
		actions = append(actions, TickAction{Watch: "bootstrap", Action: "queue", Target: ".wiki/", Success: true})
	}

	if d.config.Watches.Ingest.Enabled {
		rawDir := filepath.Join(d.config.RepoRoot, ".wiki", "raw")
		entries, err := os.ReadDir(rawDir)
		if err != nil {
			if !os.IsNotExist(err) {
				actions = append(actions, TickAction{Watch: "ingest", Action: "scan", Target: rawDir, Success: false, Error: err.Error()})
			}
		} else {
			for _, entry := range entries {
				if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
					continue
				}
				info, infoErr := entry.Info()
				if infoErr != nil || !info.Mode().IsRegular() {
					continue
				}
				if d.config.Watches.Ingest.Action == "auto-ingest" && d.canExecuteJobs() {
					jobs = append(jobs, &upkeepJob{
						ID:      fmt.Sprintf("ingest-%d", time.Now().UnixNano()),
						Type:    jobTypeRawIngest,
						Target:  filepath.ToSlash(filepath.Join(".wiki", "raw", entry.Name())),
						Reason:  "new raw source detected",
						Payload: entry.Name(),
					})
					actions = append(actions, TickAction{Watch: "ingest", Action: "queue", Target: entry.Name(), Success: true})
				} else {
					actions = append(actions, d.handleAction("ingest", d.config.Watches.Ingest.Action, entry.Name()))
				}
			}
		}
	}

	if d.config.Watches.Staleness.Enabled {
		if d.config.Config == nil {
			legacyJobs, legacyActions := d.detectLegacyStalenessJobs()
			jobs = append(jobs, legacyJobs...)
			actions = append(actions, legacyActions...)
		} else if driftJob, driftAction := d.detectRepoDriftJob(); driftJob != nil || driftAction.Watch != "" {
			if driftJob != nil {
				jobs = append(jobs, driftJob)
			}
			actions = append(actions, driftAction)
		}
	}
	if debtJob, debtAction := d.detectDebtJob(); debtJob != nil || debtAction.Watch != "" {
		if debtJob != nil {
			jobs = append(jobs, debtJob)
		}
		actions = append(actions, debtAction)
	}
	if lintJob, lintAction := d.detectLintJob(); lintJob != nil || lintAction.Watch != "" {
		if lintJob != nil {
			jobs = append(jobs, lintJob)
		}
		actions = append(actions, lintAction)
	}

	return prioritizeJobs(jobs), actions
}

func prioritizeJobs(jobs []*upkeepJob) []*upkeepJob {
	sort.SliceStable(jobs, func(i, j int) bool {
		return jobPriority(jobs[i].Type) < jobPriority(jobs[j].Type)
	})
	return jobs
}

func jobPriority(jobType string) int {
	switch jobType {
	case jobTypeBootstrap:
		return 0
	case jobTypeRawIngest:
		return 1
	case jobTypeRepoDrift:
		return 2
	case jobTypeDebt:
		return 3
	case jobTypeLint:
		return 4
	default:
		return 100
	}
}

func (d *Daemon) needsBootstrap() bool {
	if d.config.Config == nil {
		return false
	}
	wikiRoot := filepath.Join(d.config.RepoRoot, ".wiki")
	entries, err := os.ReadDir(wikiRoot)
	if err != nil {
		return true
	}

	managedPages := 0
	starterSignals := 0
	for _, entry := range entries {
		if entry.IsDir() {
			if entry.Name() == "architecture" {
				if _, err := os.Stat(filepath.Join(wikiRoot, "architecture", "overview.md")); err == nil {
					starterSignals++
				}
			}
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		managedPages++
		if entry.Name() == "Home.md" || entry.Name() == "onboarding.md" {
			starterSignals++
		}
	}
	if managedPages == 0 {
		return true
	}
	if managedPages <= 3 && starterSignals >= 2 {
		return true
	}

	mgr, err := manifest.NewManager(manifest.DefaultPath(d.config.RepoRoot))
	if err != nil {
		return false
	}
	m, err := mgr.Load()
	if err != nil {
		return false
	}
	return len(m.Pages) == 0 && starterSignals >= 2
}

func (d *Daemon) detectRepoDriftJob() (*upkeepJob, TickAction) {
	if !d.config.Watches.Staleness.Enabled {
		return nil, TickAction{}
	}
	result, err := plexsync.Run(plexsync.Options{
		RepoRoot: d.config.RepoRoot,
		Config:   d.config.Config,
		DryRun:   true,
	})
	if err != nil {
		return nil, TickAction{Watch: "staleness", Action: "scan", Target: ".wiki/", Success: false, Error: err.Error()}
	}
	if result.StalePages == 0 && len(result.PagesAffected) == 0 {
		return nil, TickAction{}
	}

	target := ".wiki/"
	if len(result.PagesAffected) > 0 {
		target = strings.Join(limitStrings(result.PagesAffected, 3), ", ")
	}
	if d.config.Watches.Staleness.Action != "auto-sync" {
		return nil, d.handleAction("staleness", d.config.Watches.Staleness.Action, target)
	}
	return &upkeepJob{
			ID:      fmt.Sprintf("drift-%d", time.Now().UnixMilli()),
			Type:    jobTypeRepoDrift,
			Target:  target,
			Reason:  fmt.Sprintf("manifest/source drift detected (%d stale pages)", result.StalePages),
			Payload: strings.Join(result.PagesAffected, "\n"),
		},
		TickAction{Watch: "staleness", Action: "queue", Target: target, Success: true}
}

func (d *Daemon) detectDebtJob() (*upkeepJob, TickAction) {
	if !d.config.Watches.Debt.Enabled {
		return nil, TickAction{}
	}
	logPath := filepath.Join(d.config.RepoRoot, ".wiki", "_log.md")
	count, err := countDebtEntries(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, TickAction{}
		}
		return nil, TickAction{Watch: "debt", Action: "scan", Target: logPath, Success: false, Error: err.Error()}
	}
	maxDebt := parseIntThreshold(d.config.Watches.Debt.Threshold, 10)
	if count < maxDebt {
		return nil, TickAction{}
	}
	target := fmt.Sprintf("WIKI-DEBT count=%d (threshold=%d)", count, maxDebt)
	if d.config.Watches.Debt.Action != "auto-fix" {
		return nil, d.handleAction("debt", d.config.Watches.Debt.Action, target)
	}
	return &upkeepJob{
			ID:     fmt.Sprintf("debt-%d", time.Now().UnixMilli()),
			Type:   jobTypeDebt,
			Target: target,
			Reason: "wiki debt threshold exceeded",
		},
		TickAction{Watch: "debt", Action: "queue", Target: target, Success: true}
}

func (d *Daemon) detectLintJob() (*upkeepJob, TickAction) {
	if !d.config.Watches.Lint.Enabled {
		return nil, TickAction{}
	}
	if d.config.Config == nil {
		if d.config.Watches.Lint.Action == "log-only" {
			return nil, TickAction{Watch: "lint", Action: "log-only", Target: ".wiki/", Success: true}
		}
		return nil, TickAction{}
	}
	report, err := lint.NewLinter(d.config.RepoRoot, d.config.Config).RunDeterministic()
	if err != nil {
		return nil, TickAction{Watch: "lint", Action: "scan", Target: ".wiki/", Success: false, Error: err.Error()}
	}
	if report.Summary.Errors == 0 && report.Summary.Warnings == 0 {
		if d.config.Watches.Lint.Action == "log-only" {
			return nil, TickAction{Watch: "lint", Action: "log-only", Target: ".wiki/", Success: true}
		}
		return nil, TickAction{Watch: "lint", Action: "scan", Target: ".wiki/", Success: true}
	}
	target := fmt.Sprintf("%d errors, %d warnings", report.Summary.Errors, report.Summary.Warnings)
	if d.config.Watches.Lint.Action != "auto-fix" {
		return nil, d.handleAction("lint", d.config.Watches.Lint.Action, target)
	}
	return &upkeepJob{
			ID:      fmt.Sprintf("lint-%d", time.Now().UnixMilli()),
			Type:    jobTypeLint,
			Target:  target,
			Reason:  "lint findings require wiki maintenance",
			Payload: target,
		},
		TickAction{Watch: "lint", Action: "queue", Target: target, Success: true}
}

func (d *Daemon) hasEnabledWatches() bool {
	return d.config.Watches.Staleness.Enabled || d.config.Watches.Lint.Enabled || d.config.Watches.Ingest.Enabled || d.config.Watches.Debt.Enabled
}

func (d *Daemon) detectLegacyStalenessJobs() ([]*upkeepJob, []TickAction) {
	threshold := parseDuration(d.config.Watches.Staleness.Threshold, 7*24*time.Hour)
	cutoff := time.Now().Add(-threshold)
	wikiRoot := filepath.Join(d.config.RepoRoot, ".wiki")

	entries, err := os.ReadDir(wikiRoot)
	if err != nil {
		return nil, []TickAction{{
			Watch:   "staleness",
			Action:  "scan",
			Target:  wikiRoot,
			Success: false,
			Error:   fmt.Sprintf("readdir: %v", err),
		}}
	}

	var jobs []*upkeepJob
	var actions []TickAction
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		target := entry.Name()
		if d.config.Watches.Staleness.Action == "auto-sync" && d.canExecuteJobs() {
			jobs = append(jobs, &upkeepJob{
				ID:     fmt.Sprintf("stale-%d", time.Now().UnixNano()),
				Type:   jobTypeRepoDrift,
				Target: target,
				Reason: "wiki page exceeded staleness threshold",
			})
			actions = append(actions, TickAction{Watch: "staleness", Action: "queue", Target: target, Success: true})
			continue
		}
		actions = append(actions, d.handleAction("staleness", d.config.Watches.Staleness.Action, target))
	}
	return jobs, actions
}

func (d *Daemon) executeJob(ctx context.Context, job *upkeepJob) TickAction {
	action := TickAction{Watch: job.Type, Action: "execute", Target: job.Target}

	active, err := d.workspace.ActiveCount()
	if err != nil {
		action.Error = err.Error()
		return action
	}
	if active >= d.maxConcurrent {
		action.Error = fmt.Sprintf("max concurrent workspaces reached (%d/%d)", active, d.maxConcurrent)
		return action
	}

	wt, err := d.workspace.Create(job.ID)
	if err != nil {
		action.Error = err.Error()
		return action
	}

	jobSnapshot := &DaemonJobSnapshot{
		ID:            job.ID,
		Type:          job.Type,
		Target:        job.Target,
		State:         jobStateRunning,
		Phase:         jobPhaseQueued,
		WorkspacePath: wt.Path,
		StartedAt:     time.Now(),
	}
	d.persistCurrentJob(jobSnapshot)

	contextPages, err := d.buildContextPages(job)
	if err != nil {
		_ = d.workspace.UpdateStatus(wt.ID, jobStateFailed)
		jobSnapshot.State = jobStateFailed
		jobSnapshot.Error = err.Error()
		d.persistFailedJob(jobSnapshot)
		action.Error = err.Error()
		return action
	}

	var result *DaemonJobSnapshot
	switch normalizeExecutionMode(d.config.ExecutionMode) {
	case executionModeProviderPrimary:
		result, err = d.runProviderPrimary(ctx, jobSnapshot, job, wt.Path, contextPages)
	default:
		result, err = d.runCodingPrimary(ctx, jobSnapshot, job, wt.Path, contextPages)
	}
	if err != nil {
		_ = d.workspace.UpdateStatus(wt.ID, jobStateFailed)
		if result == nil {
			result = jobSnapshot
		}
		result.State = jobStateFailed
		result.Error = err.Error()
		result.CompletedAt = time.Now()
		d.persistFailedJob(result)
		action.Error = err.Error()
		return action
	}

	changedFiles, err := collectWorkspaceChanges(wt.Path)
	if err != nil {
		_ = d.workspace.UpdateStatus(wt.ID, jobStateFailed)
		result.State = jobStateFailed
		result.Error = err.Error()
		result.CompletedAt = time.Now()
		d.persistFailedJob(result)
		action.Error = err.Error()
		return action
	}
	result.ChangedFiles = changedFiles
	d.persistJobPhase(result, jobPhaseValidating)

	if len(changedFiles) == 0 {
		_ = d.workspace.UpdateStatus(wt.ID, jobStateFailed)
		result.State = jobStateFailed
		result.Error = "upkeep job completed without wiki changes"
		result.CompletedAt = time.Now()
		d.persistFailedJob(result)
		action.Error = result.Error
		return action
	}

	if err := d.updateManifestForWorkspace(job, wt.Path, changedFiles); err != nil {
		_ = d.workspace.UpdateStatus(wt.ID, jobStateFailed)
		result.State = jobStateFailed
		result.Error = err.Error()
		result.CompletedAt = time.Now()
		d.persistFailedJob(result)
		action.Error = err.Error()
		return action
	}

	changedFiles, err = collectWorkspaceChanges(wt.Path)
	if err != nil {
		_ = d.workspace.UpdateStatus(wt.ID, jobStateFailed)
		result.State = jobStateFailed
		result.Error = err.Error()
		result.CompletedAt = time.Now()
		d.persistFailedJob(result)
		action.Error = err.Error()
		return action
	}
	result.ChangedFiles = changedFiles

	d.persistJobPhase(result, jobPhaseApplying)
	appliedFiles, applyOutcome, attentionNeeded, applyErr := applyWorkspaceChanges(d.config.RepoRoot, wt.Path, changedFiles)
	result.AppliedFiles = appliedFiles
	result.ApplyOutcome = applyOutcome
	result.CompletedAt = time.Now()
	if applyErr != nil {
		_ = d.workspace.UpdateStatus(wt.ID, jobStateFailed)
		result.State = jobStateFailed
		result.Error = applyErr.Error()
		d.persistFailedJob(result)
		action.Error = applyErr.Error()
		return action
	}
	if attentionNeeded {
		_ = d.workspace.UpdateStatus(wt.ID, jobStateAttentionNeeded)
		result.State = jobStateAttentionNeeded
		d.persistAttentionJob(result)
		action.Success = true
		action.Action = "attention_needed"
		return action
	}

	_ = d.workspace.UpdateStatus(wt.ID, jobStateCompleted)
	result.State = jobStateCompleted
	result.Phase = jobPhaseCompleted
	d.persistCompletedJob(result)
	action.Success = true
	action.Target = strings.Join(limitStrings(result.AppliedFiles, 3), ", ")
	if action.Target == "" {
		action.Target = job.Target
	}
	return action
}

func (d *Daemon) buildContextPages(job *upkeepJob) ([]string, error) {
	wikiRoot := filepath.Join(d.config.RepoRoot, ".wiki")
	retriever := pageindex.NewRetriever(wikiRoot)
	query := job.Target
	switch job.Type {
	case jobTypeBootstrap:
		query = "repository architecture overview"
	case jobTypeRepoDrift:
		query = "stale wiki pages source drift"
	case jobTypeRawIngest:
		query = job.Payload
	case jobTypeDebt:
		query = "wiki debt outstanding items"
	case jobTypeLint:
		query = "wiki lint findings"
	}
	result, err := retriever.Retrieve(query)
	if err != nil {
		return nil, err
	}
	pages := make([]string, 0, len(result.Pages))
	for _, page := range result.Pages {
		pages = append(pages, page.Path)
		if len(pages) >= 5 {
			break
		}
	}
	return pages, nil
}

func (d *Daemon) runCodingPrimary(ctx context.Context, snapshot *DaemonJobSnapshot, job *upkeepJob, workdir string, contextPages []string) (*DaemonJobSnapshot, error) {
	d.persistJobPhase(snapshot, jobPhaseRetrieving)
	snapshot.PrimaryActor = strings.TrimSpace(d.config.RunnerName)
	if snapshot.PrimaryActor == "" {
		snapshot.PrimaryActor = "runner"
	}
	d.persistJobPhase(snapshot, jobPhaseDocumenting)

	prompt := buildRunnerJobPrompt(d.config.Config, job, contextPages)
	if _, err := d.runner.Run(ctx, "documenter", prompt, contextPages, workdir); err != nil {
		return snapshot, err
	}
	snapshot.Summary = fmt.Sprintf("%s executed wiki upkeep job", snapshot.PrimaryActor)
	return snapshot, nil
}

func (d *Daemon) runProviderPrimary(ctx context.Context, snapshot *DaemonJobSnapshot, job *upkeepJob, workdir string, contextPages []string) (*DaemonJobSnapshot, error) {
	if d.cascade == nil {
		return snapshot, fmt.Errorf("provider-primary execution requires an assistive provider")
	}

	d.persistJobPhase(snapshot, jobPhasePlanning)
	providerPrompt := buildProviderJobPrompt(d.config.Config, job, contextPages, d.config.RepoRoot)
	completion, err := d.cascade.Complete(ctx, providerPrompt)
	if err != nil {
		return snapshot, err
	}
	if d.rateTracker != nil {
		_ = d.rateTracker.Record(completion.Provider, completion.TokensUsed, completion.CostUSD)
	}
	snapshot.PrimaryActor = completion.Provider
	d.persistJobPhase(snapshot, jobPhasePlanning)

	var plan providerExecutionPlan
	if err := json.Unmarshal([]byte(strings.TrimSpace(completion.Response)), &plan); err != nil {
		plan.Summary = strings.TrimSpace(completion.Response)
		plan.DelegateToCLI = true
	}
	if plan.Summary != "" {
		snapshot.Summary = plan.Summary
	}

	if plan.DelegateToCLI && d.config.RunnerName != "" && d.config.RunnerName != "noop" {
		snapshot.DelegatedActor = d.config.RunnerName
		d.persistJobPhase(snapshot, jobPhaseDocumenting)
		delegatePrompt := plan.DelegatePrompt
		if strings.TrimSpace(delegatePrompt) == "" {
			delegatePrompt = buildRunnerJobPrompt(d.config.Config, job, contextPages)
		}
		if _, err := d.runner.Run(ctx, "documenter", delegatePrompt, contextPages, workdir); err != nil {
			return snapshot, err
		}
		return snapshot, nil
	}

	if len(plan.Files) == 0 {
		return snapshot, fmt.Errorf("provider-primary execution did not produce file updates")
	}

	d.persistJobPhase(snapshot, jobPhaseDocumenting)
	for _, file := range plan.Files {
		if err := writeProviderFile(workdir, file); err != nil {
			return snapshot, err
		}
	}
	return snapshot, nil
}

func writeProviderFile(workdir string, file providerWriteFile) error {
	cleanPath := filepath.Clean(file.Path)
	if strings.HasPrefix(cleanPath, "..") {
		return fmt.Errorf("provider attempted unsafe path %q", file.Path)
	}
	if !strings.HasPrefix(cleanPath, ".wiki/") && cleanPath != ".wiki" && !strings.HasPrefix(cleanPath, ".plexium/reports/") {
		return fmt.Errorf("provider attempted to write unsupported path %q", file.Path)
	}
	absPath := filepath.Join(workdir, cleanPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(absPath, []byte(file.Content), 0o644)
}

func collectWorkspaceChanges(workdir string) ([]string, error) {
	cmd := exec.Command("git", "-C", workdir, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
	seen := make(map[string]bool)
	var changed []string
	for _, line := range lines {
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "" || len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}
		if !seen[path] {
			seen[path] = true
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed, nil
}

func applyWorkspaceChanges(repoRoot, workdir string, changedFiles []string) ([]string, string, bool, error) {
	var applied []string
	for _, rel := range changedFiles {
		if !isAllowedApplyPath(rel) {
			return nil, fmt.Sprintf("left in workspace for review (%s outside allowed apply scope)", rel), true, nil
		}
		src := filepath.Join(workdir, rel)
		dst := filepath.Join(repoRoot, rel)
		if _, err := os.Stat(src); err != nil {
			if os.IsNotExist(err) {
				if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
					return nil, "", false, err
				}
				applied = append(applied, rel)
				continue
			}
			return nil, "", false, err
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return nil, "", false, err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, "", false, err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return nil, "", false, err
		}
		applied = append(applied, rel)
	}
	if len(applied) == 0 {
		return nil, "no allowed wiki files to apply", true, nil
	}
	return applied, fmt.Sprintf("applied %d file(s) back to repo", len(applied)), false, nil
}

func isAllowedApplyPath(path string) bool {
	return strings.HasPrefix(path, ".wiki/") || path == ".wiki" || path == ".plexium/manifest.json" || strings.HasPrefix(path, ".plexium/reports/")
}

func (d *Daemon) updateManifestForWorkspace(job *upkeepJob, workdir string, changedFiles []string) error {
	mgr, err := manifest.NewManager(manifest.DefaultPath(workdir))
	if err != nil {
		return err
	}
	suggestedSources, _ := d.suggestSourceMappings()
	headCommit := gitOutput(workdir, "rev-parse", "HEAD")
	now := time.Now().UTC().Format(time.RFC3339)

	for _, changed := range changedFiles {
		if !strings.HasPrefix(changed, ".wiki/") || !strings.HasSuffix(changed, ".md") {
			continue
		}
		if strings.HasPrefix(filepath.Base(changed), "_") {
			continue
		}

		page, _ := mgr.GetPage(changed)
		sourceFiles := []manifest.SourceFile{}
		if page != nil {
			sourceFiles = page.SourceFiles
		} else if suggestion, ok := suggestedSources[changed]; ok {
			sourceFiles = suggestion
		} else if job.Type == jobTypeRawIngest && job.Payload != "" {
			sourceFiles = []manifest.SourceFile{{Path: filepath.ToSlash(filepath.Join(".wiki", "raw", job.Payload))}}
		}

		for i := range sourceFiles {
			if sourceFiles[i].Path == "" {
				continue
			}
			hash, err := manifest.ComputeHash(filepath.Join(workdir, sourceFiles[i].Path))
			if err == nil {
				sourceFiles[i].Hash = hash
			}
			sourceFiles[i].LastProcessedCommit = headCommit
		}

		content, err := os.ReadFile(filepath.Join(workdir, changed))
		if err != nil {
			if os.IsNotExist(err) {
				if err := mgr.RemovePage(changed); err != nil {
					return err
				}
				continue
			}
			return err
		}
		entry := manifest.PageEntry{
			WikiPath:    changed,
			Title:       extractTitle(string(content), changed),
			Ownership:   "managed",
			Section:     inferSection(changed),
			SourceFiles: sourceFiles,
			LastUpdated: now,
			UpdatedBy:   "plexium-daemon",
		}
		if err := mgr.UpsertPage(entry); err != nil {
			return err
		}
	}

	if headCommit != "" {
		_ = mgr.UpdateProcessedCommit(headCommit)
	}
	return nil
}

func (d *Daemon) suggestSourceMappings() (map[string][]manifest.SourceFile, error) {
	if d.config.Config == nil {
		return nil, nil
	}
	pipeline := convert.NewPipeline(convert.PipelineOptions{
		RepoRoot: d.config.RepoRoot,
		Config:   d.config.Config,
		DryRun:   true,
		Depth:    "shallow",
	})
	result, err := pipeline.Run()
	if err != nil {
		return nil, err
	}
	mappings := make(map[string][]manifest.SourceFile, len(result.Pages))
	for _, page := range result.Pages {
		sourceFiles := make([]manifest.SourceFile, 0, len(page.SourceFiles))
		for _, source := range page.SourceFiles {
			hash, err := manifest.ComputeHash(filepath.Join(d.config.RepoRoot, source))
			if err != nil {
				continue
			}
			sourceFiles = append(sourceFiles, manifest.SourceFile{Path: source, Hash: hash})
		}
		mappings[page.WikiPath] = sourceFiles
	}
	return mappings, nil
}

func extractTitle(content, wikiPath string) string {
	if strings.HasPrefix(content, "---\n") {
		rest := content[4:]
		if idx := strings.Index(rest, "\n---\n"); idx >= 0 {
			frontmatter := rest[:idx]
			for _, line := range strings.Split(frontmatter, "\n") {
				if strings.HasPrefix(line, "title:") {
					title := strings.TrimSpace(strings.TrimPrefix(line, "title:"))
					title = strings.Trim(title, "\"'")
					if title != "" {
						return title
					}
				}
			}
		}
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	base := filepath.Base(wikiPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func inferSection(wikiPath string) string {
	rel := strings.TrimPrefix(wikiPath, ".wiki/")
	parts := strings.Split(rel, "/")
	if len(parts) <= 1 {
		return "Root"
	}
	return strings.Title(parts[0])
}

func buildRunnerJobPrompt(cfg *config.Config, job *upkeepJob, contextPages []string) string {
	var b strings.Builder
	b.WriteString("You are maintaining a persistent LLM wiki for this repository.\n")
	b.WriteString("Follow the LLM wiki pattern: update or create real wiki pages, preserve the knowledge base, and make the wiki more useful after this run.\n")
	b.WriteString("Only edit wiki-maintenance surfaces: .wiki/**, .plexium/manifest.json, and .plexium/reports/** when needed.\n")
	b.WriteString("Update pages directly; do not stop at analysis.\n\n")
	fmt.Fprintf(&b, "Job type: %s\n", job.Type)
	fmt.Fprintf(&b, "Target: %s\n", job.Target)
	fmt.Fprintf(&b, "Reason: %s\n", job.Reason)
	if len(contextPages) > 0 {
		b.WriteString("\nRelevant wiki pages:\n")
		for _, page := range contextPages {
			b.WriteString("- ")
			b.WriteString(page)
			b.WriteString("\n")
		}
	}
	if cfg != nil {
		fmt.Fprintf(&b, "\nWiki root: %s\n", cfg.Wiki.Root)
	}
	b.WriteString("\nRequired workflow:\n")
	b.WriteString("1. Retrieve the needed wiki and source context.\n")
	b.WriteString("2. Update existing pages and create missing pages if needed.\n")
	b.WriteString("3. Keep `_log.md` and navigational context coherent when your changes warrant it.\n")
	b.WriteString("4. Finish with actual file edits in the workspace.\n")
	return b.String()
}

func buildProviderJobPrompt(cfg *config.Config, job *upkeepJob, contextPages []string, repoRoot string) string {
	var b strings.Builder
	b.WriteString("You are the primary upkeep orchestrator for an LLM-maintained wiki.\n")
	b.WriteString("Return ONLY valid JSON with this schema:\n")
	b.WriteString(`{"summary":"...","delegateToCli":false,"delegatePrompt":"...","files":[{"path":".wiki/path.md","content":"..."}]}`)
	b.WriteString("\n")
	b.WriteString("If the task is too broad or needs a headless coding agent session, set delegateToCli=true and provide delegatePrompt.\n")
	b.WriteString("Otherwise, directly author the wiki file updates in `files`.\n")
	b.WriteString("Never write outside `.wiki/` or `.plexium/reports/`.\n\n")
	fmt.Fprintf(&b, "Job type: %s\nTarget: %s\nReason: %s\n", job.Type, job.Target, job.Reason)
	if len(contextPages) > 0 {
		b.WriteString("\nRelevant wiki pages:\n")
		for _, page := range contextPages {
			b.WriteString("- ")
			b.WriteString(page)
			b.WriteString("\n")
			content, err := os.ReadFile(filepath.Join(repoRoot, ".wiki", page))
			if err == nil {
				b.WriteString("```md\n")
				b.WriteString(truncateString(string(content), 1800))
				b.WriteString("\n```\n")
			}
		}
	}
	if job.Type == jobTypeRawIngest && job.Payload != "" {
		rawPath := filepath.Join(repoRoot, ".wiki", "raw", job.Payload)
		if data, err := os.ReadFile(rawPath); err == nil {
			b.WriteString("\nRaw source content:\n```md\n")
			b.WriteString(truncateString(string(data), 3500))
			b.WriteString("\n```\n")
		}
	}
	readmePath := filepath.Join(repoRoot, "README.md")
	if data, err := os.ReadFile(readmePath); err == nil {
		b.WriteString("\nRepository README excerpt:\n```md\n")
		b.WriteString(truncateString(string(data), 2500))
		b.WriteString("\n```\n")
	}
	if cfg != nil {
		fmt.Fprintf(&b, "\nWiki root: %s\n", cfg.Wiki.Root)
	}
	return b.String()
}

func truncateString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func gitOutput(workdir string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", workdir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func limitStrings(values []string, max int) []string {
	if len(values) <= max {
		return values
	}
	return values[:max]
}

func normalizeExecutionMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case executionModeProviderPrimary:
		return executionModeProviderPrimary
	default:
		return executionModeCodingAgentPrimary
	}
}
