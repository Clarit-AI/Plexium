package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RateLimitTracker persists daily per-provider usage to a JSON state file
// so the daemon can enforce budget caps across restarts.
type RateLimitTracker struct {
	stateFile string // e.g. .plexium/agent-state.json
}

// UsageState is the top-level structure stored in the state file.
type UsageState struct {
	Date    string                  `json:"date"`    // YYYY-MM-DD
	Records map[string]*UsageRecord `json:"records"` // keyed by provider name
}

// UsageRecord tracks cumulative usage for a single provider on a given day.
type UsageRecord struct {
	Requests int     `json:"requests"`
	Tokens   int     `json:"tokens"`
	CostUSD  float64 `json:"costUSD"`
}

// NewRateLimitTracker creates a tracker that reads/writes state to stateFile.
func NewRateLimitTracker(stateFile string) *RateLimitTracker {
	return &RateLimitTracker{stateFile: stateFile}
}

// Record adds a usage event for the given provider.
func (r *RateLimitTracker) Record(provider string, tokens int, cost float64) error {
	state, err := r.Load()
	if err != nil {
		return err
	}

	// Reset if the date has rolled over.
	today := todayStr()
	if state.Date != today {
		state = &UsageState{
			Date:    today,
			Records: make(map[string]*UsageRecord),
		}
	}

	rec, ok := state.Records[provider]
	if !ok {
		rec = &UsageRecord{}
		state.Records[provider] = rec
	}

	rec.Requests++
	rec.Tokens += tokens
	rec.CostUSD += cost

	return r.Save(state)
}

// GetDailyUsage returns today's usage record for the given provider.
// Returns a zero-value record if no usage has been recorded.
func (r *RateLimitTracker) GetDailyUsage(provider string) (*UsageRecord, error) {
	state, err := r.Load()
	if err != nil {
		return &UsageRecord{}, nil // treat missing file as zero usage
	}

	if state.Date != todayStr() {
		return &UsageRecord{}, nil // stale day → zero
	}

	rec, ok := state.Records[provider]
	if !ok {
		return &UsageRecord{}, nil
	}
	return rec, nil
}

// CanMakeRequest returns true if the provider's daily spend is under budgetUSD.
// A budgetUSD of 0 means unlimited.
func (r *RateLimitTracker) CanMakeRequest(provider string, budgetUSD float64) (bool, error) {
	if budgetUSD <= 0 {
		return true, nil
	}

	rec, err := r.GetDailyUsage(provider)
	if err != nil {
		return false, err
	}

	return rec.CostUSD < budgetUSD, nil
}

// GetBatchingDelay returns how long to wait before making the next request to
// the given provider, based on how close today's usage is to the budget.
// Logic:
//   - Under 80% of budget → 0 (no delay needed)
//   - 80–95% of budget → 2–8 seconds (linearly scaled)
//   - Above 95% of budget → 30 seconds (max delay to avoid hitting the limit)
//   - No recorded usage today → 0
//
// This enables adaptive batching: the daemon can pause before a rate limit is
// hit rather than failing and relying on retry backoff.
func (r *RateLimitTracker) GetBatchingDelay(provider string, budgetUSD float64) time.Duration {
	if budgetUSD <= 0 {
		return 0
	}

	rec, err := r.GetDailyUsage(provider)
	if err != nil || rec.CostUSD == 0 {
		return 0
	}

	usagePct := rec.CostUSD / budgetUSD

	switch {
	case usagePct < 0.80:
		return 0
	case usagePct <= 0.95:
		// Linear scale: 80% → 2s, 95% → 8s
		// delay = 2 + (pct-0.80) * (8-2)/(0.95-0.80) = 2 + (pct-0.80) * 40
		// At pct=0.80 → 2s. At pct=0.95 → 8s.
		delaySeconds := 2 + (usagePct-0.80)*40.0
		return time.Duration(delaySeconds*float64(time.Second))
	default:
		return 30 * time.Second
	}
}

// Load reads the state file from disk. Returns a fresh state if the file
// does not exist or is unreadable.
func (r *RateLimitTracker) Load() (*UsageState, error) {
	data, err := os.ReadFile(r.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &UsageState{
				Date:    todayStr(),
				Records: make(map[string]*UsageRecord),
			}, nil
		}
		return nil, fmt.Errorf("ratelimit: read state: %w", err)
	}

	var state UsageState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("ratelimit: unmarshal state: %w", err)
	}

	if state.Records == nil {
		state.Records = make(map[string]*UsageRecord)
	}

	return &state, nil
}

// Save writes the state to disk, creating parent directories if needed.
func (r *RateLimitTracker) Save(state *UsageState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("ratelimit: marshal state: %w", err)
	}

	dir := filepath.Dir(r.stateFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("ratelimit: mkdir %s: %w", dir, err)
	}

	if err := os.WriteFile(r.stateFile, data, 0o644); err != nil {
		return fmt.Errorf("ratelimit: write state: %w", err)
	}
	return nil
}

// todayStr returns today's date as YYYY-MM-DD.
func todayStr() string {
	return time.Now().Format("2006-01-02")
}
