package connectors

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrConnectorDuplicate = errors.New("connectors: connector id already registered")
	ErrConnectorNotFound  = errors.New("connectors: connector not found")
)

// Registry is immutable-after-registration in normal composition. It validates
// manifests before exposing connectors to workflows.
type Registry struct {
	mu        sync.RWMutex
	entries   map[string]Connector
	manifests map[string]Manifest
}

func NewRegistry(values ...Connector) (*Registry, error) {
	registry := &Registry{entries: make(map[string]Connector), manifests: make(map[string]Manifest)}
	for _, connector := range values {
		if err := registry.Register(connector); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (registry *Registry) Register(extension Connector) error {
	if registry == nil || extension == nil {
		return ErrInvalidManifest
	}
	manifest := extension.Manifest()
	if err := manifest.Validate(); err != nil {
		return err
	}
	manifest = manifest.Canonical()
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.entries[manifest.ID]; exists {
		return ErrConnectorDuplicate
	}
	registry.entries[manifest.ID] = extension
	registry.manifests[manifest.ID] = manifest
	return nil
}

func (registry *Registry) Connector(id string) (Connector, Manifest, error) {
	if registry == nil || !manifestIDPattern.MatchString(id) {
		return nil, Manifest{}, ErrConnectorNotFound
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	connector, ok := registry.entries[id]
	if !ok {
		return nil, Manifest{}, ErrConnectorNotFound
	}
	return connector, registry.manifests[id], nil
}

func (registry *Registry) Manifests() []Manifest {
	if registry == nil {
		return nil
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	values := make([]Manifest, 0, len(registry.manifests))
	for _, manifest := range registry.manifests {
		values = append(values, manifest)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	return values
}
