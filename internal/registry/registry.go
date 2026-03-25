package registry

import (
	"errors"
	"fmt"
	"sync"
)

// Registry is a thread-safe key-value store with optional key normalization.
type Registry[K comparable, V any] struct {
	mu        sync.RWMutex
	entries   map[K]V
	normalize func(K) K
}

// New creates a Registry. If normalize is nil, keys are stored as-is.
func New[K comparable, V any](normalize func(K) K) *Registry[K, V] {
	return &Registry[K, V]{
		entries:   make(map[K]V),
		normalize: normalize,
	}
}

// Register stores a value under the given key.
func (r *Registry[K, V]) Register(key K, value V) {
	k := r.normalizeKey(key)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[k] = value
}

// Get returns the value for key and whether it was found.
func (r *Registry[K, V]) Get(key K) (V, bool) {
	k := r.normalizeKey(key)
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.entries[k]
	return v, ok
}

// Resolve returns the value for key or an error if not found.
func (r *Registry[K, V]) Resolve(key K, notFoundErr error) (V, error) {
	v, ok := r.Get(key)
	if !ok {
		var zero V
		if notFoundErr == nil {
			notFoundErr = errors.New("not found")
		}
		return zero, fmt.Errorf("%w: %v", notFoundErr, key)
	}
	return v, nil
}

func (r *Registry[K, V]) normalizeKey(key K) K {
	if r.normalize != nil {
		return r.normalize(key)
	}
	return key
}
