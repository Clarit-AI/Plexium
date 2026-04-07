# Daemon System Architecture

## Daemon Loop

`Daemon.Run()` polls on a configurable interval (default 5 minutes). Each tick:

1. Run all enabled watches in sequence
2. For each finding, dispatch via `handleAction()`
3. Record results as `TickAction` entries

Exits on context cancellation or `Stop()` call.

## Watches

| Watch | What it detects | How |
|-------|----------------|-----|
| Staleness | `.wiki/*.md` files older than threshold | Compare `ModTime` against cutoff (default 7 days) |
| Lint | Wiki health issues | Currently stubbed (logs intent only) |
| Ingest | New files in `.wiki/raw/` | Scan directory for unprocessed files |
| Debt | Excessive WIKI-DEBT entries | Count "WIKI-DEBT" lines in `_log.md` |

## Actions

Each watch has a configured action:

| Action | Behavior |
|--------|----------|
| `log-only` | Record the finding, take no action |
| `create-issue` | Create a GitHub issue via `TrackerAdapter` |
| `auto-sync` | Create worktree, run runner, update status |
| `auto-fix` | Same as auto-sync (used for lint findings) |
| `auto-ingest` | Same as auto-sync (used for ingest findings) |

## Runner System

`RunnerAdapter` shells out to CLI tools in an isolated worktree:

```go
type RunnerAdapter interface {
    Run(ctx context.Context, role string, prompt string, contextPages []string) (*RunResult, error)
}
```

Implementations: ClaudeRunner (`claude --print`), CodexRunner (`codex --quiet`), GeminiRunner (`gemini`), NoOpRunner.

Runner type is configured via `daemon.runner` in config.yml. The `NewRunner` factory creates the appropriate implementation.

## Workspace Manager

`WorkspaceMgr` manages git worktrees under `.plexium/workspaces/`:

- `Create(issueID)` — creates worktree at `wt-{issueID}`, branch `plexium/wt-{issueID}`
- `Cleanup(id)` — removes worktree via `git worktree remove --force`
- `ActiveCount()` — count of worktrees with status="running" (enforces `maxConcurrent`)
- Metadata persisted in `meta.json` per worktree

## Tracker System

`TrackerAdapter` creates issues for wiki findings:

```go
type TrackerAdapter interface {
    CreateIssue(title, body string) (string, error)
    CloseIssue(id string) error
    AddLabel(issueID, label string) error
    Comment(issueID, body string) error
}
```

Implementations: NoOpTracker (silent success), GitHubIssuesTracker (via `gh` CLI), LinearTracker (stub).

## Configuration

```yaml
daemon:
  enabled: true
  pollInterval: 300       # seconds
  maxConcurrent: 2        # parallel worktrees
  runner: claude           # claude | codex | gemini | noop
  runnerModel: ""          # optional model override
  tracker: github          # github | none
```

Runner and tracker are read from config in `cmd/plexium/main.go` and passed to `NewDaemon()`. Empty values fall back to noop/none.
