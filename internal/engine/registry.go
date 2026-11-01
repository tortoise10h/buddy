package engine

import (
	"fmt"
	"sync"
)

// Registry manages available engines (rex, t3, wezterm) and resolves them dynamically.
type Registry struct {
	mu          sync.RWMutex
	engines     map[string]Engine
	defaultName string
}

func NewRegistry(defaultName string) *Registry {
	if defaultName == "" {
		defaultName = "rex"
	}
	return &Registry{
		engines:     make(map[string]Engine),
		defaultName: defaultName,
	}
}

func (r *Registry) Register(name string, e Engine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.engines[name] = e
}

func (r *Registry) Get(name string) (Engine, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	targetName := name
	if targetName == "" {
		targetName = r.defaultName
	}

	e, ok := r.engines[targetName]
	if !ok {
		return nil, fmt.Errorf("unknown engine '%s' (available: %v)", targetName, r.listEngineNamesLocked())
	}
	return e, nil
}

func (r *Registry) listEngineNamesLocked() []string {
	names := make([]string, 0, len(r.engines))
	for name := range r.engines {
		names = append(names, name)
	}
	return names
}
