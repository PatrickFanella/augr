package llm

import (
	"errors"
	"fmt"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/registry"
)

var (
	// ErrProviderNotFound indicates that no provider has been registered for a name.
	ErrProviderNotFound = errors.New("llm provider not found")
	// ErrModelTierNotConfigured indicates that a provider was found but has no model for the requested tier.
	ErrModelTierNotConfigured = errors.New("llm model tier not configured")
)

// ProviderRegistration stores a provider along with the models assigned to each tier.
type ProviderRegistration struct {
	Provider Provider
	Models   map[ModelTier]string
}

// Registry stores providers by name and their tier-specific model mappings.
type Registry struct {
	inner *registry.Registry[string, ProviderRegistration]
}

// NewRegistry constructs an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		inner: registry.New[string, ProviderRegistration](normalizeProviderName),
	}
}

// Register stores a provider registration under the provided name.
func (r *Registry) Register(name string, provider Provider, models map[ModelTier]string) error {
	if r == nil {
		return errors.New("llm registry is nil")
	}

	normalizedName := normalizeProviderName(name)
	if normalizedName == "" {
		return errors.New("llm provider name is required")
	}
	if provider == nil {
		return errors.New("llm provider is required")
	}

	copiedModels := make(map[ModelTier]string, len(models))
	for tier, model := range models {
		trimmedModel := strings.TrimSpace(model)
		if trimmedModel == "" {
			return fmt.Errorf("llm model name is required for tier %s", tier)
		}

		copiedModels[tier] = trimmedModel
	}
	if len(copiedModels) == 0 {
		return errors.New("llm models are required")
	}

	r.inner.Register(normalizedName, ProviderRegistration{
		Provider: provider,
		Models:   copiedModels,
	})

	return nil
}

// Get returns the registered provider entry for a name.
func (r *Registry) Get(name string) (ProviderRegistration, bool) {
	if r == nil {
		return ProviderRegistration{}, false
	}

	entry, ok := r.inner.Get(name)
	if !ok {
		return ProviderRegistration{}, false
	}

	return cloneRegistration(entry), true
}

// Resolve returns the provider and model configured for the requested provider name and tier.
func (r *Registry) Resolve(name string, tier ModelTier) (Provider, string, error) {
	entry, ok := r.Get(name)
	if !ok {
		return nil, "", fmt.Errorf("%w: %s", ErrProviderNotFound, normalizeProviderName(name))
	}

	model := strings.TrimSpace(entry.Models[tier])
	if model == "" {
		return nil, "", fmt.Errorf("%w: provider=%s tier=%s", ErrModelTierNotConfigured, normalizeProviderName(name), tier)
	}

	return entry.Provider, model, nil
}

func cloneRegistration(entry ProviderRegistration) ProviderRegistration {
	copiedModels := make(map[ModelTier]string, len(entry.Models))
	for tier, model := range entry.Models {
		copiedModels[tier] = model
	}

	return ProviderRegistration{
		Provider: entry.Provider,
		Models:   copiedModels,
	}
}

func normalizeProviderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
