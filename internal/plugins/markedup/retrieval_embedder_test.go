package markedup

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/plugins"
)

// fakeEmbedder is a deterministic stub that records each Embed call so
// tests can assert the search path actually consulted it. It returns
// canned vectors of a configurable dimensionality so we can verify
// Dimensions() flows through unchanged.
type fakeEmbedder struct {
	dims  int
	calls atomic.Int32
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.calls.Add(1)
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, f.dims)
		for j := range v {
			// Trivial deterministic vector so cosine math is reproducible
			// without making any test depend on the exact value.
			v[j] = float32(i+1) / float32(f.dims)
		}
		out[i] = v
	}
	return out, nil
}
func (f *fakeEmbedder) Dimensions() int { return f.dims }
func (f *fakeEmbedder) Model() string   { return "fake" }

// fakeVectorCache returns "no cached vector" for every page so the
// search engine has to call into the embedder for the query side and
// then skip semantic scoring for the page side. That's enough to
// prove the embedder wiring is live without coupling the test to
// markedup's internal scoring formula.
type fakeVectorCache struct{}

func (fakeVectorCache) LoadVectors(_, _ string) ([]float32, error) {
	return nil, nil
}

func TestRetrievalPlugin_HasEmbedder(t *testing.T) {
	t.Parallel()

	keyword := NewRetrieval(Config{Enabled: true})
	if keyword.HasEmbedder() {
		t.Errorf("plain NewRetrieval should not report embedder presence")
	}

	semantic := NewRetrievalWithEmbedder(
		Config{Enabled: true},
		&fakeEmbedder{dims: 4},
		fakeVectorCache{},
	)
	if !semantic.HasEmbedder() {
		t.Errorf("NewRetrievalWithEmbedder should report embedder presence")
	}

	// Half-configured: embedder set, cache nil — must NOT activate
	// semantic mode (markedup's index.Search needs both).
	half := NewRetrievalWithEmbedder(Config{Enabled: true}, &fakeEmbedder{dims: 4}, nil)
	if half.HasEmbedder() {
		t.Errorf("retrieval with nil cache should not advertise embedder")
	}
}

// TestRetrievalPlugin_SearchInvokesEmbedder verifies that when both
// embedder and vector cache are wired, Search consults the embedder.
// The exact ranking math is markedup's responsibility; we only
// guarantee the wiring path is live.
func TestRetrievalPlugin_SearchInvokesEmbedder(t *testing.T) {
	wikiRoot := seedEnrichedWiki(t)
	emb := &fakeEmbedder{dims: 8}
	p := NewRetrievalWithEmbedder(Config{Enabled: true}, emb, fakeVectorCache{})

	if err := p.Initialize(context.Background(), wikiRoot); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := p.Search(context.Background(), "Auth", plugins.SearchOpts{Limit: 5}); err != nil {
		t.Fatalf("search: %v", err)
	}
	if got := emb.calls.Load(); got == 0 {
		t.Errorf("expected embedder to be invoked when wired; got 0 calls")
	}
}

// TestRetrievalPlugin_SearchKeywordFallback verifies the regression
// guard for the nil-embedder branch: the public Search contract must
// not change when no embedder is supplied.
func TestRetrievalPlugin_SearchKeywordFallback(t *testing.T) {
	wikiRoot := seedEnrichedWiki(t)
	p := NewRetrieval(Config{Enabled: true})
	if err := p.Initialize(context.Background(), wikiRoot); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	results, err := p.Search(context.Background(), "Auth", plugins.SearchOpts{Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("keyword fallback must still return results")
	}
}
