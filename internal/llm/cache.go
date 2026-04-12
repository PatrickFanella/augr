package llm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

const defaultCacheVersion = "v1"

// CacheStats summarizes cache behavior for a single run.
type CacheStats struct {
	Hits     int     `json:"hits"`
	Misses   int     `json:"misses"`
	Requests int     `json:"requests"`
	HitRate  float64 `json:"hit_rate"`
	MissRate float64 `json:"miss_rate"`
}

// ResponseCache stores completion responses by cache key.
type ResponseCache interface {
	Get(key string) (*CompletionResponse, bool)
	Set(key string, response *CompletionResponse)
}

// CacheMetrics records aggregate cache hits and misses.
type CacheMetrics interface {
	RecordLLMCacheHit()
	RecordLLMCacheMiss()
}

// defaultMemoryCacheMaxItems is the default capacity for MemoryResponseCache.
// Entries are evicted (FIFO-approximated) when this limit is reached to bound
// memory growth in long-lived processes that receive mostly-unique prompts.
const defaultMemoryCacheMaxItems = 1000

// MemoryResponseCache is a capacity-bounded in-memory ResponseCache
// implementation. When the cache is full an arbitrary entry is evicted before
// inserting the new one, bounding memory consumption to at most maxItems
// entries.
type MemoryResponseCache struct {
	mu       sync.RWMutex
	items    map[string]*CompletionResponse
	maxItems int
}

// NewMemoryResponseCache returns an empty in-memory response cache bounded to
// defaultMemoryCacheMaxItems entries.
func NewMemoryResponseCache() *MemoryResponseCache {
	return NewMemoryResponseCacheWithLimit(defaultMemoryCacheMaxItems)
}

// NewMemoryResponseCacheWithLimit returns an empty in-memory response cache
// bounded to maxItems entries. If maxItems is <= 0 the default limit is used.
func NewMemoryResponseCacheWithLimit(maxItems int) *MemoryResponseCache {
	if maxItems <= 0 {
		maxItems = defaultMemoryCacheMaxItems
	}
	return &MemoryResponseCache{
		items:    make(map[string]*CompletionResponse),
		maxItems: maxItems,
	}
}

// Get returns a cloned cached response for the given key.
func (c *MemoryResponseCache) Get(key string) (*CompletionResponse, bool) {
	if c == nil {
		return nil, false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	resp, ok := c.items[key]
	if !ok {
		return nil, false
	}

	return cloneCompletionResponse(resp), true
}

// Set stores a cloned response for the given key. If the cache is at capacity
// an arbitrary entry is evicted first to keep the total entry count bounded.
func (c *MemoryResponseCache) Set(key string, response *CompletionResponse) {
	if c == nil || response == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict one arbitrary entry when at capacity to prevent unbounded growth.
	if _, exists := c.items[key]; !exists && len(c.items) >= c.maxItems {
		for k := range c.items {
			delete(c.items, k)
			break
		}
	}

	c.items[key] = cloneCompletionResponse(response)
}

// CacheProvider wraps a Provider with response caching.
type CacheProvider struct {
	provider Provider
	cache    ResponseCache
	version  string
	metrics  CacheMetrics
}

// CachedProvider is a compatibility alias for CacheProvider.
type CachedProvider = CacheProvider

// NewCachedProvider wraps provider with a response cache using the default
// cache version. It returns nil when provider or cache is invalid.
func NewCachedProvider(provider Provider, cache ResponseCache) *CachedProvider {
	wrapped, err := NewCacheProvider(provider, cache, "")
	if err != nil {
		return nil
	}
	return wrapped
}

// NewCacheProvider wraps provider with a response cache. An empty version uses
// the default cache version.
func NewCacheProvider(provider Provider, cache ResponseCache, version string) (*CacheProvider, error) {
	if provider == nil {
		return nil, errors.New("llm: provider is nil")
	}
	if cache == nil {
		return nil, errors.New("llm: cache is nil")
	}

	trimmedVersion := strings.TrimSpace(version)
	if trimmedVersion == "" {
		trimmedVersion = defaultCacheVersion
	}

	return &CacheProvider{
		provider: provider,
		cache:    cache,
		version:  trimmedVersion,
	}, nil
}

// WithCacheMetrics attaches optional cache metrics.
func (c *CacheProvider) WithCacheMetrics(m CacheMetrics) *CacheProvider {
	if c == nil {
		return nil
	}
	c.metrics = m
	return c
}

// Complete returns a cached response when available, otherwise it delegates to
// the wrapped provider and stores the result.
func (c *CacheProvider) Complete(ctx context.Context, request CompletionRequest) (*CompletionResponse, error) {
	if c == nil || c.provider == nil {
		return nil, errors.New("llm: cache provider is nil")
	}

	key, err := cacheKey(request, c.version)
	if err != nil {
		return nil, err
	}

	if resp, ok := c.cache.Get(key); ok {
		recordCacheHit(ctx)
		if c.metrics != nil {
			c.metrics.RecordLLMCacheHit()
		}
		return resp, nil
	}

	recordCacheMiss(ctx)
	if c.metrics != nil {
		c.metrics.RecordLLMCacheMiss()
	}

	resp, err := c.provider.Complete(ctx, request)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("llm: provider returned nil response without error")
	}

	c.cache.Set(key, resp)
	return cloneCompletionResponse(resp), nil
}

type cacheStatsContextKey struct{}

// CacheStatsCollector records per-context cache hits and misses.
type CacheStatsCollector struct {
	mu     sync.Mutex
	hits   int
	misses int
}

// NewCacheStatsCollector returns an empty stats collector.
func NewCacheStatsCollector() *CacheStatsCollector {
	return &CacheStatsCollector{}
}

// WithCacheStatsCollector attaches collector to ctx.
func WithCacheStatsCollector(ctx context.Context, collector *CacheStatsCollector) context.Context {
	if collector == nil {
		return ctx
	}
	return context.WithValue(ctx, cacheStatsContextKey{}, collector)
}

// CacheStatsCollectorFromContext returns the collector stored on ctx, if any.
func CacheStatsCollectorFromContext(ctx context.Context) *CacheStatsCollector {
	if ctx == nil {
		return nil
	}

	collector, _ := ctx.Value(cacheStatsContextKey{}).(*CacheStatsCollector)
	return collector
}

// Snapshot returns the current hit/miss totals and rates.
func (c *CacheStatsCollector) Snapshot() CacheStats {
	if c == nil {
		return CacheStats{}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	requests := c.hits + c.misses
	stats := CacheStats{
		Hits:     c.hits,
		Misses:   c.misses,
		Requests: requests,
	}
	if requests > 0 {
		stats.HitRate = float64(c.hits) / float64(requests)
		stats.MissRate = float64(c.misses) / float64(requests)
	}

	return stats
}

func (c *CacheStatsCollector) recordHit() {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.hits++
}

func (c *CacheStatsCollector) recordMiss() {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.misses++
}

func recordCacheHit(ctx context.Context) {
	if collector := CacheStatsCollectorFromContext(ctx); collector != nil {
		collector.recordHit()
	}
}

func recordCacheMiss(ctx context.Context) {
	if collector := CacheStatsCollectorFromContext(ctx); collector != nil {
		collector.recordMiss()
	}
}

func cacheKey(request CompletionRequest, version string) (string, error) {
	reqBytes, err := json.Marshal(request)
	if err != nil {
		return "", err
	}

	var key bytes.Buffer
	key.Grow(len(reqBytes) + len(version) + 1)
	key.Write(reqBytes)
	key.WriteByte('\n')
	key.WriteString(version)

	sum := sha256.Sum256(key.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

func cloneCompletionResponse(resp *CompletionResponse) *CompletionResponse {
	if resp == nil {
		return nil
	}

	cloned := *resp
	return &cloned
}
