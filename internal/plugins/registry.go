package plugins

import (
	"fmt"
	"sync"
)

// Registry manages all registered plugins by type.
type Registry struct {
	mu        sync.RWMutex
	plugins   map[string]Plugin
	pipeline  []PipelinePlugin
	retrieval map[string]RetrievalPlugin
	watches   map[string]WatchPlugin
}

// NewRegistry creates an empty plugin registry.
func NewRegistry() *Registry {
	return &Registry{
		plugins:   make(map[string]Plugin),
		retrieval: make(map[string]RetrievalPlugin),
		watches:   make(map[string]WatchPlugin),
	}
}

// Register adds a plugin to the registry. Returns an error if a plugin
// with the same name is already registered.
func (r *Registry) Register(p Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := p.Name()
	if _, exists := r.plugins[name]; exists {
		return fmt.Errorf("plugin %q already registered", name)
	}

	r.plugins[name] = p

	switch tp := p.(type) {
	case PipelinePlugin:
		r.pipeline = append(r.pipeline, tp)
	case RetrievalPlugin:
		r.retrieval[name] = tp
	case WatchPlugin:
		r.watches[name] = tp
	}

	return nil
}

// Get returns a plugin by name.
func (r *Registry) Get(name string) (Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[name]
	return p, ok
}

// All returns all registered plugins.
func (r *Registry) All() []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Plugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		out = append(out, p)
	}
	return out
}

// PipelinePlugins returns pipeline plugins that run at the given stage,
// in registration order.
func (r *Registry) PipelinePlugins(stage PipelineStage) []PipelinePlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []PipelinePlugin
	for _, p := range r.pipeline {
		if p.Stage() == stage {
			out = append(out, p)
		}
	}
	return out
}

// RetrievalPlugins returns all registered retrieval plugins.
func (r *Registry) RetrievalPlugins() []RetrievalPlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RetrievalPlugin, 0, len(r.retrieval))
	for _, p := range r.retrieval {
		out = append(out, p)
	}
	return out
}

// WatchPlugins returns all registered watch plugins.
func (r *Registry) WatchPlugins() []WatchPlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]WatchPlugin, 0, len(r.watches))
	for _, p := range r.watches {
		out = append(out, p)
	}
	return out
}

// PluginCount returns the total number of registered plugins.
func (r *Registry) PluginCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.plugins)
}
