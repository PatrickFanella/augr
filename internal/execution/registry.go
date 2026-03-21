package execution

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

var (
	// ErrBrokerNotFound indicates that no broker has been registered for a market type.
	ErrBrokerNotFound = errors.New("execution broker not found")
)

// Registry stores brokers by market type.
type Registry struct {
	mu      sync.RWMutex
	brokers map[domain.MarketType]Broker
}

// NewRegistry constructs an empty broker registry.
func NewRegistry() *Registry {
	return &Registry{
		brokers: make(map[domain.MarketType]Broker),
	}
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

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.brokers == nil {
		r.brokers = make(map[domain.MarketType]Broker)
	}

	r.brokers[normalizedMarketType] = broker

	return nil
}

// Get returns the registered broker for a market type.
func (r *Registry) Get(marketType domain.MarketType) (Broker, bool) {
	if r == nil {
		return nil, false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	broker, ok := r.brokers[normalizeMarketType(marketType)]
	return broker, ok
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
	return domain.MarketType(strings.ToLower(strings.TrimSpace(marketType.String())))
}
