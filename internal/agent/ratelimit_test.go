package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
