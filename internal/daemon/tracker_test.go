package daemon

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRunner captures command invocations for testing.
type mockRunner struct {
	calls [][]string
	out   []byte
	err   error
}

func (m *mockRunner) Run(name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	m.calls = append(m.calls, call)
	return m.out, m.err
}

// --- NoOpTracker ---

func TestNoOpTracker_CreateIssue(t *testing.T) {
	tr := &NoOpTracker{}
	id, err := tr.CreateIssue("title", "body")
	assert.NoError(t, err)
	assert.Empty(t, id)
}

func TestNoOpTracker_CloseIssue(t *testing.T) {
	assert.NoError(t, (&NoOpTracker{}).CloseIssue("42"))
}

func TestNoOpTracker_AddLabel(t *testing.T) {
	assert.NoError(t, (&NoOpTracker{}).AddLabel("42", "bug"))
}

func TestNoOpTracker_Comment(t *testing.T) {
	assert.NoError(t, (&NoOpTracker{}).Comment("42", "hello"))
}

// --- GitHubIssuesTracker ---

func TestGitHubTracker_CreateIssue(t *testing.T) {
	m := &mockRunner{out: []byte(`{"number":123}`)}
	tr := &GitHubIssuesTracker{owner: "org", repo: "repo", cmd: m}

	id, err := tr.CreateIssue("title", "body")
	require.NoError(t, err)
	assert.Equal(t, "123", id)
	assert.Equal(t, []string{"gh", "issue", "create", "--repo", "org/repo", "--title", "title", "--body", "body", "--json", "number"}, m.calls[0])
}

func TestGitHubTracker_CreateIssue_FallbackURL(t *testing.T) {
	m := &mockRunner{out: []byte("https://github.com/org/repo/issues/99\n")}
	tr := &GitHubIssuesTracker{owner: "org", repo: "repo", cmd: m}

	id, err := tr.CreateIssue("t", "b")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/org/repo/issues/99", id)
}

func TestGitHubTracker_CreateIssue_Error(t *testing.T) {
	m := &mockRunner{err: errors.New("fail"), out: []byte("oops")}
	tr := &GitHubIssuesTracker{owner: "o", repo: "r", cmd: m}

	_, err := tr.CreateIssue("t", "b")
	assert.ErrorContains(t, err, "gh issue create")
}

func TestGitHubTracker_CloseIssue(t *testing.T) {
	m := &mockRunner{out: []byte("ok")}
	tr := &GitHubIssuesTracker{owner: "o", repo: "r", cmd: m}

	err := tr.CloseIssue("42")
	assert.NoError(t, err)
	assert.Equal(t, []string{"gh", "issue", "close", "42", "--repo", "o/r"}, m.calls[0])
}

func TestGitHubTracker_AddLabel(t *testing.T) {
	m := &mockRunner{out: []byte("ok")}
	tr := &GitHubIssuesTracker{owner: "o", repo: "r", cmd: m}

	err := tr.AddLabel("42", "wiki-debt")
	assert.NoError(t, err)
	assert.Equal(t, []string{"gh", "issue", "edit", "42", "--add-label", "wiki-debt", "--repo", "o/r"}, m.calls[0])
}

func TestGitHubTracker_Comment(t *testing.T) {
	m := &mockRunner{out: []byte("ok")}
	tr := &GitHubIssuesTracker{owner: "o", repo: "r", cmd: m}

	err := tr.Comment("42", "hello")
	assert.NoError(t, err)
	assert.Equal(t, []string{"gh", "issue", "comment", "42", "--body", "hello", "--repo", "o/r"}, m.calls[0])
}

// --- LinearTracker ---

func TestLinearTracker_AllMethodsReturnNotImplemented(t *testing.T) {
	tr := &LinearTracker{}
	_, err := tr.CreateIssue("t", "b")
	assert.ErrorIs(t, err, ErrNotImplemented)
	assert.ErrorIs(t, tr.CloseIssue("1"), ErrNotImplemented)
	assert.ErrorIs(t, tr.AddLabel("1", "x"), ErrNotImplemented)
	assert.ErrorIs(t, tr.Comment("1", "x"), ErrNotImplemented)
}

// --- Factory ---

func TestNewTracker_GitHub(t *testing.T) {
	tr := NewTracker("github", "o", "r", "tok")
	_, ok := tr.(*GitHubIssuesTracker)
	assert.True(t, ok)
}

func TestNewTracker_Linear(t *testing.T) {
	tr := NewTracker("linear", "", "", "")
	_, ok := tr.(*LinearTracker)
	assert.True(t, ok)
}

func TestNewTracker_None(t *testing.T) {
	tr := NewTracker("none", "", "", "")
	_, ok := tr.(*NoOpTracker)
	assert.True(t, ok)
}

func TestNewTracker_Empty(t *testing.T) {
	tr := NewTracker("", "", "", "")
	_, ok := tr.(*NoOpTracker)
	assert.True(t, ok)
}

func TestNewTracker_CaseInsensitive(t *testing.T) {
	tr := NewTracker("GitHub", "o", "r", "t")
	_, ok := tr.(*GitHubIssuesTracker)
	assert.True(t, ok)
}

// --- Interface compliance ---

var (
	_ TrackerAdapter = (*NoOpTracker)(nil)
	_ TrackerAdapter = (*GitHubIssuesTracker)(nil)
	_ TrackerAdapter = (*LinearTracker)(nil)
)
