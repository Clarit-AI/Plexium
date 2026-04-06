package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestTracker(t *testing.T) *RateLimitTracker {
	t.Helper()
	tmpDir := t.TempDir()
	return NewRateLimitTracker(filepath.Join(tmpDir, "agent-state.json"))
}

// ---------------------------------------------------------------------------
// Record
// ---------------------------------------------------------------------------

func TestRateLimitTracker_Record(t *testing.T) {
	tracker := newTestTracker(t)

	err := tracker.Record("ollama", 100, 0.0)
	require.NoError(t, err)

	rec, err := tracker.GetDailyUsage("ollama")
	require.NoError(t, err)
	assert.Equal(t, 1, rec.Requests)
	assert.Equal(t, 100, rec.Tokens)
	assert.Equal(t, 0.0, rec.CostUSD)
}

func TestRateLimitTracker_Record_Accumulates(t *testing.T) {
	tracker := newTestTracker(t)

	require.NoError(t, tracker.Record("openrouter", 50, 0.05))
	require.NoError(t, tracker.Record("openrouter", 75, 0.10))

	rec, err := tracker.GetDailyUsage("openrouter")
	require.NoError(t, err)
	assert.Equal(t, 2, rec.Requests)
	assert.Equal(t, 125, rec.Tokens)
	assert.InDelta(t, 0.15, rec.CostUSD, 0.0001)
}

func TestRateLimitTracker_Record_MultipleProviders(t *testing.T) {
	tracker := newTestTracker(t)

	require.NoError(t, tracker.Record("ollama", 100, 0.0))
	require.NoError(t, tracker.Record("openrouter", 50, 0.05))

	ollamaRec, err := tracker.GetDailyUsage("ollama")
	require.NoError(t, err)
	assert.Equal(t, 1, ollamaRec.Requests)

	orRec, err := tracker.GetDailyUsage("openrouter")
	require.NoError(t, err)
	assert.Equal(t, 1, orRec.Requests)
	assert.InDelta(t, 0.05, orRec.CostUSD, 0.0001)
}

// ---------------------------------------------------------------------------
// GetDailyUsage
// ---------------------------------------------------------------------------

func TestRateLimitTracker_GetDailyUsage_NoFile(t *testing.T) {
	tracker := NewRateLimitTracker("/tmp/nonexistent-plexium-test/state.json")

	rec, err := tracker.GetDailyUsage("ollama")
	require.NoError(t, err)
	assert.Equal(t, 0, rec.Requests)
	assert.Equal(t, 0, rec.Tokens)
}

func TestRateLimitTracker_GetDailyUsage_UnknownProvider(t *testing.T) {
	tracker := newTestTracker(t)
	require.NoError(t, tracker.Record("ollama", 10, 0.0))

	rec, err := tracker.GetDailyUsage("unknown")
	require.NoError(t, err)
	assert.Equal(t, 0, rec.Requests)
}

func TestRateLimitTracker_GetDailyUsage_StaleDate(t *testing.T) {
	tracker := newTestTracker(t)

	// Write state with yesterday's date.
	state := &UsageState{
		Date: "2020-01-01",
		Records: map[string]*UsageRecord{
			"ollama": {Requests: 99, Tokens: 9999, CostUSD: 1.0},
		},
	}
	require.NoError(t, tracker.Save(state))

	rec, err := tracker.GetDailyUsage("ollama")
	require.NoError(t, err)
	assert.Equal(t, 0, rec.Requests, "stale date should return zero usage")
}

// ---------------------------------------------------------------------------
// CanMakeRequest
// ---------------------------------------------------------------------------

func TestRateLimitTracker_CanMakeRequest_UnderBudget(t *testing.T) {
	tracker := newTestTracker(t)
	require.NoError(t, tracker.Record("openrouter", 50, 0.05))

	ok, err := tracker.CanMakeRequest("openrouter", 1.00)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestRateLimitTracker_CanMakeRequest_OverBudget(t *testing.T) {
	tracker := newTestTracker(t)
	require.NoError(t, tracker.Record("openrouter", 1000, 1.50))

	ok, err := tracker.CanMakeRequest("openrouter", 1.00)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestRateLimitTracker_CanMakeRequest_ZeroBudgetMeansUnlimited(t *testing.T) {
	tracker := newTestTracker(t)
	require.NoError(t, tracker.Record("openrouter", 1000, 999.99))

	ok, err := tracker.CanMakeRequest("openrouter", 0)
	require.NoError(t, err)
	assert.True(t, ok, "zero budget means unlimited")
}

func TestRateLimitTracker_CanMakeRequest_NoUsage(t *testing.T) {
	tracker := newTestTracker(t)

	ok, err := tracker.CanMakeRequest("openrouter", 1.00)
	require.NoError(t, err)
	assert.True(t, ok)
}

// ---------------------------------------------------------------------------
// Load / Save round-trip
// ---------------------------------------------------------------------------

func TestRateLimitTracker_LoadSave_RoundTrip(t *testing.T) {
	tracker := newTestTracker(t)

	original := &UsageState{
		Date: todayStr(),
		Records: map[string]*UsageRecord{
			"ollama":     {Requests: 5, Tokens: 500, CostUSD: 0.0},
			"openrouter": {Requests: 3, Tokens: 300, CostUSD: 0.30},
		},
	}
	require.NoError(t, tracker.Save(original))

	loaded, err := tracker.Load()
	require.NoError(t, err)
	assert.Equal(t, original.Date, loaded.Date)
	assert.Equal(t, original.Records["ollama"].Requests, loaded.Records["ollama"].Requests)
	assert.Equal(t, original.Records["openrouter"].CostUSD, loaded.Records["openrouter"].CostUSD)
}

func TestRateLimitTracker_Load_MissingFile(t *testing.T) {
	tracker := NewRateLimitTracker("/tmp/nonexistent-plexium-test/state.json")

	state, err := tracker.Load()
	require.NoError(t, err)
	assert.Equal(t, todayStr(), state.Date)
	assert.NotNil(t, state.Records)
	assert.Empty(t, state.Records)
}

func TestRateLimitTracker_Save_CreatesDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "deep", "nested", "state.json")
	tracker := NewRateLimitTracker(stateFile)

	state := &UsageState{Date: todayStr(), Records: map[string]*UsageRecord{}}
	require.NoError(t, tracker.Save(state))

	data, err := os.ReadFile(stateFile)
	require.NoError(t, err)

	var loaded UsageState
	require.NoError(t, json.Unmarshal(data, &loaded))
	assert.Equal(t, todayStr(), loaded.Date)
}

// ---------------------------------------------------------------------------
// Daily reset on Record
// ---------------------------------------------------------------------------

func TestRateLimitTracker_Record_DailyReset(t *testing.T) {
	tracker := newTestTracker(t)

	// Seed with yesterday's data.
	old := &UsageState{
		Date: "2020-01-01",
		Records: map[string]*UsageRecord{
			"ollama": {Requests: 100, Tokens: 10000, CostUSD: 5.0},
		},
	}
	require.NoError(t, tracker.Save(old))

	// Record new usage — should trigger a daily reset.
	require.NoError(t, tracker.Record("ollama", 10, 0.0))

	rec, err := tracker.GetDailyUsage("ollama")
	require.NoError(t, err)
	assert.Equal(t, 1, rec.Requests, "should have reset to 1 after daily rollover")
	assert.Equal(t, 10, rec.Tokens)
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNewRateLimitTracker(t *testing.T) {
	tracker := NewRateLimitTracker("/path/to/state.json")
	assert.Equal(t, "/path/to/state.json", tracker.stateFile)
}

// ---------------------------------------------------------------------------
// GetBatchingDelay
// ---------------------------------------------------------------------------

func TestGetBatchingDelay_UnlimitedBudget(t *testing.T) {
	tracker := newTestTracker(t)
	delay := tracker.GetBatchingDelay("ollama", 0)
	assert.Equal(t, time.Duration(0), delay)
}

func TestGetBatchingDelay_NoUsage(t *testing.T) {
	tracker := newTestTracker(t)
	delay := tracker.GetBatchingDelay("ollama", 10.0)
	assert.Equal(t, time.Duration(0), delay, "no usage → no delay")
}

func TestGetBatchingDelay_Under80Pct(t *testing.T) {
	tracker := newTestTracker(t)
	require.NoError(t, tracker.Record("ollama", 100, 3.0)) // $3 of $10 budget (30%)

	delay := tracker.GetBatchingDelay("ollama", 10.0)
	assert.Equal(t, time.Duration(0), delay, "under 80% → no delay")
}

func TestGetBatchingDelay_80to95Pct(t *testing.T) {
	tracker := newTestTracker(t)
	require.NoError(t, tracker.Record("ollama", 1000, 8.5)) // $8.5 of $10 budget (85%)

	delay := tracker.GetBatchingDelay("ollama", 10.0)
	// 85% is between 80% (2s) and 95% (8s); expect roughly 4.7s (allow 3-6s range for floating point)
	assert.True(t, delay >= 3*time.Second && delay <= 6*time.Second,
		"expected ~4-5s delay at 85%% usage, got %v", delay)
}

func TestGetBatchingDelay_Above95Pct(t *testing.T) {
	tracker := newTestTracker(t)
	require.NoError(t, tracker.Record("ollama", 1000, 9.8)) // $9.8 of $10 budget (98%)

	delay := tracker.GetBatchingDelay("ollama", 10.0)
	assert.Equal(t, 30*time.Second, delay, "above 95%% → max 30s delay")
}

func TestGetBatchingDelay_AtExactly80Pct(t *testing.T) {
	tracker := newTestTracker(t)
	require.NoError(t, tracker.Record("ollama", 100, 8.0)) // 80% of $10

	delay := tracker.GetBatchingDelay("ollama", 10.0)
	assert.True(t, delay >= 1*time.Second && delay <= 3*time.Second,
		"at 80%% → expected ~2s, got %v", delay)
}

func TestGetBatchingDelay_AtExactly95Pct(t *testing.T) {
	tracker := newTestTracker(t)
	require.NoError(t, tracker.Record("ollama", 100, 9.5)) // 95% of $10

	delay := tracker.GetBatchingDelay("ollama", 10.0)
	assert.True(t, delay >= 7*time.Second && delay <= 9*time.Second,
		"at 95%% → expected ~8s, got %v", delay)
}

func TestGetBatchingDelay_DifferentProvider(t *testing.T) {
	tracker := newTestTracker(t)
	require.NoError(t, tracker.Record("openrouter", 500, 8.0)) // openrouter is at 80%

	// ollama has no usage → 0 delay; openrouter at 80% → 2s
	ollamaDelay := tracker.GetBatchingDelay("ollama", 10.0)
	openrouterDelay := tracker.GetBatchingDelay("openrouter", 10.0)

	assert.Equal(t, time.Duration(0), ollamaDelay)
	assert.Equal(t, 2*time.Second, openrouterDelay)
}
