package signal

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// repositoryStrategyProvider adapts repository.StrategyRepository to StrategyProvider.
// It lists all active strategies and enriches each with thesis watch terms.
type repositoryStrategyProvider struct {
	repo repository.StrategyRepository
}

// NewRepositoryStrategyProvider returns a StrategyProvider backed by the given
// strategy repository. Wrap the result with NewStrategyProviderWithCache to
// avoid the N+1 query on every hub process() call.
func NewRepositoryStrategyProvider(repo repository.StrategyRepository) StrategyProvider {
	return &repositoryStrategyProvider{repo: repo}
}

func (p *repositoryStrategyProvider) ListActiveWithThesis(ctx context.Context) ([]StrategyWithThesis, error) {
	strategies, err := p.repo.List(ctx, repository.StrategyFilter{Status: "active"}, 500, 0)
	if err != nil {
		return nil, err
	}
	result := make([]StrategyWithThesis, 0, len(strategies))
	for _, s := range strategies {
		sw := StrategyWithThesis{ID: s.ID, Ticker: s.Ticker}
		// Best-effort: load stored thesis to extract watch terms.
		raw, err := p.repo.GetThesisRaw(ctx, s.ID)
		if err == nil && len(raw) > 0 {
			var t agent.Thesis
			if json.Unmarshal(raw, &t) == nil {
				sw.WatchTerms = t.WatchTerms
			}
		}
		result = append(result, sw)
	}
	return result, nil
}

// cachedStrategyProvider wraps a StrategyProvider with a TTL-based cache.
// This eliminates the N+1 query pattern in the hub's hot process() path:
// the inner provider is called at most once per TTL window (default 5 min).
type cachedStrategyProvider struct {
	inner   StrategyProvider
	ttl     time.Duration
	mu      sync.RWMutex
	cached  []StrategyWithThesis
	builtAt time.Time
}

// NewStrategyProviderWithCache wraps provider with a read-through cache.
// Pass ttl=0 to use the default of 5 minutes.
func NewStrategyProviderWithCache(provider StrategyProvider, ttl time.Duration) StrategyProvider {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &cachedStrategyProvider{inner: provider, ttl: ttl}
}

func (c *cachedStrategyProvider) ListActiveWithThesis(ctx context.Context) ([]StrategyWithThesis, error) {
	c.mu.RLock()
	if c.cached != nil && time.Since(c.builtAt) < c.ttl {
		result := c.cached
		c.mu.RUnlock()
		return result, nil
	}
	c.mu.RUnlock()

	result, err := c.inner.ListActiveWithThesis(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.cached = result
	c.builtAt = time.Now()
	c.mu.Unlock()
	return result, nil
}
