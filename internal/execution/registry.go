package execution

import (
	"errors"
	"fmt"
	"sync"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/registry"
)

// ErrBrokerNotFound indicates that no broker has been registered for a market type.
var ErrBrokerNotFound = errors.New("execution broker not found")

// Registry stores brokers by market type.
type Registry struct {
	once  sync.Once
	inner *registry.Registry[domain.MarketType, Broker]
}

// NewRegistry constructs an empty broker registry.
func NewRegistry() *Registry {
	return &Registry{
		inner: registry.New[domain.MarketType, Broker](normalizeMarketType),
	}
}

func (r *Registry) ensureInner() {
	r.once.Do(func() {
		if r.inner == nil {
			r.inner = registry.New[domain.MarketType, Broker](normalizeMarketType)
		}
	})
}

// Register stores a broker registration under the provided market type.
func (r *Registry) Register(marketType domain.MarketType, broker Broker) error {
	if r == nil {
		return errors.New("execution registry is nil")
	}

	normalizedMarketType := normalizeMarketType(marketType)
	if normalizedMarketType == "" {
		return errors.New("execution market type is required")
	}
	if broker == nil {
		return errors.New("execution broker is required")
	}

	r.ensureInner()
	r.inner.Register(marketType, broker)
	return nil
}

// Get returns the registered broker for a market type.
func (r *Registry) Get(marketType domain.MarketType) (Broker, bool) {
	if r == nil {
		return nil, false
	}

	r.ensureInner()
	return r.inner.Get(marketType)
}

// Resolve returns the broker configured for the requested market type.
func (r *Registry) Resolve(marketType domain.MarketType) (Broker, error) {
	broker, ok := r.Get(marketType)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrBrokerNotFound, normalizeMarketType(marketType))
	}

	return broker, nil
}

func normalizeMarketType(marketType domain.MarketType) domain.MarketType {
	return marketType.Normalize()
}
