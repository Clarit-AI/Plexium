package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// --- test doubles ---

type stubPlugin struct {
	name    string
	version string
	desc    string
	ptype   PluginType
}

func (s *stubPlugin) Name() string        { return s.name }
func (s *stubPlugin) Version() string     { return s.version }
func (s *stubPlugin) Description() string { return s.desc }
func (s *stubPlugin) Type() PluginType    { return s.ptype }

type stubPipelinePlugin struct {
	stubPlugin
	stage     PipelineStage
	processed bool
}

func (s *stubPipelinePlugin) Stage() PipelineStage { return s.stage }
func (s *stubPipelinePlugin) Process(_ context.Context, _ *PipelineData) error {
	s.processed = true
	return nil
}

type stubRetrievalPlugin struct {
	stubPlugin
	initialized bool
	lastQuery   string
}

func (s *stubRetrievalPlugin) Initialize(_ context.Context, _ string) error {
	s.initialized = true
	return nil
}
func (s *stubRetrievalPlugin) Search(_ context.Context, query string, _ SearchOpts) ([]PluginSearchResult, error) {
	s.lastQuery = query
	return []PluginSearchResult{{PagePath: "test.md", Title: "Test", Score: 1.0}}, nil
}
func (s *stubRetrievalPlugin) MCPTools() []MCPToolDef {
	return []MCPToolDef{{Name: "test_search", Description: "test"}}
}
func (s *stubRetrievalPlugin) HandleMCPCall(_ context.Context, _ string, _ json.RawMessage) (any, error) {
	return map[string]string{"ok": "true"}, nil
}

type stubWatchPlugin struct {
	stubPlugin
	ran bool
}

func (s *stubWatchPlugin) ShouldRun(_ context.Context, _ string) (bool, error) { return true, nil }
func (s *stubWatchPlugin) Run(_ context.Context, _ string) error {
	s.ran = true
	return nil
}

// --- tests ---

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()
	p := &stubPlugin{name: "test", version: "1.0", ptype: PluginTypeAdapter}

	if err := r.Register(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.PluginCount() != 1 {
		t.Fatalf("expected 1 plugin, got %d", r.PluginCount())
	}
}

func TestRegistry_DuplicateReject(t *testing.T) {
	r := NewRegistry()
	p := &stubPlugin{name: "test", ptype: PluginTypeAdapter}
	_ = r.Register(p)

	err := r.Register(p)
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()
	p := &stubPlugin{name: "test", ptype: PluginTypeAdapter}
	_ = r.Register(p)

	got, ok := r.Get("test")
	if !ok {
		t.Fatal("expected to find plugin")
	}
	if got.Name() != "test" {
		t.Fatalf("expected name 'test', got %q", got.Name())
	}

	_, ok = r.Get("missing")
	if ok {
		t.Fatal("expected not to find missing plugin")
	}
}

func TestRegistry_PipelinePlugins(t *testing.T) {
	r := NewRegistry()
	pp1 := &stubPipelinePlugin{
		stubPlugin: stubPlugin{name: "p1", ptype: PluginTypePipeline},
		stage:      StageAfterWrite,
	}
	pp2 := &stubPipelinePlugin{
		stubPlugin: stubPlugin{name: "p2", ptype: PluginTypePipeline},
		stage:      StageAfterIngest,
	}
	_ = r.Register(pp1)
	_ = r.Register(pp2)

	afterWrite := r.PipelinePlugins(StageAfterWrite)
	if len(afterWrite) != 1 {
		t.Fatalf("expected 1 after-write plugin, got %d", len(afterWrite))
	}
	if afterWrite[0].Name() != "p1" {
		t.Fatalf("expected p1, got %s", afterWrite[0].Name())
	}

	afterIngest := r.PipelinePlugins(StageAfterIngest)
	if len(afterIngest) != 1 {
		t.Fatalf("expected 1 after-ingest plugin, got %d", len(afterIngest))
	}

	afterLint := r.PipelinePlugins(StageAfterLint)
	if len(afterLint) != 0 {
		t.Fatalf("expected 0 after-lint plugins, got %d", len(afterLint))
	}
}

func TestRegistry_RetrievalPlugins(t *testing.T) {
	r := NewRegistry()
	rp := &stubRetrievalPlugin{
		stubPlugin: stubPlugin{name: "retriever", ptype: PluginTypeRetrieval},
	}
	_ = r.Register(rp)

	rps := r.RetrievalPlugins()
	if len(rps) != 1 {
		t.Fatalf("expected 1 retrieval plugin, got %d", len(rps))
	}
	if rps[0].Name() != "retriever" {
		t.Fatalf("expected 'retriever', got %q", rps[0].Name())
	}
}

func TestRegistry_WatchPlugins(t *testing.T) {
	r := NewRegistry()
	wp := &stubWatchPlugin{
		stubPlugin: stubPlugin{name: "watcher", ptype: PluginTypeWatch},
	}
	_ = r.Register(wp)

	wps := r.WatchPlugins()
	if len(wps) != 1 {
		t.Fatalf("expected 1 watch plugin, got %d", len(wps))
	}
}

func TestRegistry_All(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&stubPlugin{name: "a", ptype: PluginTypeAdapter})
	_ = r.Register(&stubPipelinePlugin{
		stubPlugin: stubPlugin{name: "b", ptype: PluginTypePipeline},
		stage:      StageAfterWrite,
	})

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(all))
	}
}

func TestRegistry_TypeMismatchRejected(t *testing.T) {
	r := NewRegistry()
	// stubPlugin declares Pipeline type but doesn't implement PipelinePlugin.
	bogus := &stubPlugin{name: "bogus", ptype: PluginTypePipeline}

	err := r.Register(bogus)
	if err == nil {
		t.Fatal("expected error for Type/interface mismatch")
	}
}

func TestRegistry_UnknownTypeRejected(t *testing.T) {
	r := NewRegistry()
	bogus := &stubPlugin{name: "bogus", ptype: PluginType("mystery")}

	err := r.Register(bogus)
	if err == nil {
		t.Fatal("expected error for unknown plugin type")
	}
}

func TestRegistry_ConcurrentRegisterAndRead(t *testing.T) {
	r := NewRegistry()
	const writers = 10
	const perWriter = 20

	var writersWG sync.WaitGroup
	var readersWG sync.WaitGroup

	// Writers: each registers a distinct batch of plugins.
	for w := 0; w < writers; w++ {
		writersWG.Add(1)
		go func(wID int) {
			defer writersWG.Done()
			for i := 0; i < perWriter; i++ {
				p := &stubPipelinePlugin{
					stubPlugin: stubPlugin{
						name:  fmt.Sprintf("p-%d-%d", wID, i),
						ptype: PluginTypePipeline,
					},
					stage: StageAfterWrite,
				}
				if err := r.Register(p); err != nil {
					t.Errorf("register failed: %v", err)
					return
				}
			}
		}(w)
	}

	// Readers: concurrently read registry state until writers finish.
	readerStop := make(chan struct{})
	for i := 0; i < 4; i++ {
		readersWG.Add(1)
		go func() {
			defer readersWG.Done()
			for {
				select {
				case <-readerStop:
					return
				default:
					_ = r.PluginCount()
					_, _ = r.Get("p-0-0")
					_ = r.PipelinePlugins(StageAfterWrite)
					_ = r.All()
				}
			}
		}()
	}

	writersWG.Wait()
	close(readerStop)
	readersWG.Wait()

	expected := writers * perWriter
	if got := r.PluginCount(); got != expected {
		t.Fatalf("expected %d plugins, got %d", expected, got)
	}

	// Spot-check retrievability across the full set.
	for w := 0; w < writers; w++ {
		for i := 0; i < perWriter; i++ {
			name := fmt.Sprintf("p-%d-%d", w, i)
			if _, ok := r.Get(name); !ok {
				t.Fatalf("plugin %q not retrievable after concurrent register", name)
			}
		}
	}
}

func TestRegistry_EmptyIsValid(t *testing.T) {
	r := NewRegistry()
	if r.PluginCount() != 0 {
		t.Fatal("expected 0 plugins")
	}
	if len(r.PipelinePlugins(StageAfterWrite)) != 0 {
		t.Fatal("expected no pipeline plugins")
	}
	if len(r.RetrievalPlugins()) != 0 {
		t.Fatal("expected no retrieval plugins")
	}
	if len(r.WatchPlugins()) != 0 {
		t.Fatal("expected no watch plugins")
	}
}
