package polymarket

import (
	"context"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"
)

const defaultCleanerDedupeTTL = 500 * time.Millisecond

type Cleaner struct {
	cfg     Config
	in      <-chan Tick
	out     chan Tick
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	once    sync.Once
	mu      sync.RWMutex
	states  map[string]*cleanerState
	dedupe  map[string]time.Time
	logger  *slog.Logger
	ttl     time.Duration
	metrics Metrics
}

type cleanerState struct {
	firstSeen      time.Time
	lastClean      float64
	warmupCount    int
	warmupJumpSeen bool
	ready          bool
	seenAny        bool
}

func NewCleaner(cfg Config, in <-chan Tick) *Cleaner {
	ctx, cancel := context.WithCancel(context.Background())
	return &Cleaner{
		cfg:     cfg,
		in:      in,
		out:     make(chan Tick, 256),
		ctx:     ctx,
		cancel:  cancel,
		states:  make(map[string]*cleanerState),
		dedupe:  make(map[string]time.Time),
		logger:  cfg.Logger,
		ttl:     defaultCleanerDedupeTTL,
		metrics: cfg.Metrics,
	}
}

func (c *Cleaner) Out() <-chan Tick { return c.out }

func (c *Cleaner) Ready(slug string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	st := c.states[slug]
	return st != nil && st.ready
}

func (c *Cleaner) Start(ctx context.Context) {
	c.once.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		c.ctx, c.cancel = context.WithCancel(ctx)
		c.wg.Add(1)
		go c.run()
	})
}

func (c *Cleaner) Close() {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
}

func (c *Cleaner) run() {
	defer c.wg.Done()
	defer close(c.out)
	for {
		select {
		case <-c.ctx.Done():
			return
		case tk, ok := <-c.in:
			if !ok {
				return
			}
			c.handleTick(tk)
		}
	}
}

func (c *Cleaner) handleTick(tk Tick) {
	select {
	case <-c.ctx.Done():
		return
	default:
	}
	if tk.Slug == "" {
		return
	}
	now := tk.ReceivedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	c.mu.Lock()
	st := c.states[tk.Slug]
	if st == nil {
		st = &cleanerState{firstSeen: now}
		c.states[tk.Slug] = st
	}
	st.seenAny = true
	if !st.ready {
		prev := st.lastClean
		if st.warmupCount > 0 && c.cfg.WarmupMaxJumpUSD > 0 && math.Abs(tk.Price-prev) > c.cfg.WarmupMaxJumpUSD {
			st.firstSeen = now
			st.warmupCount = 0
			st.lastClean = tk.Price
			st.warmupJumpSeen = true
			c.mu.Unlock()
			return
		}
		st.warmupCount++
		st.lastClean = tk.Price
		if st.firstSeen.IsZero() {
			st.firstSeen = now
		}
		if st.warmupCount >= c.cfg.WarmupMinClean && now.Sub(st.firstSeen) >= c.cfg.WarmupDuration && !st.warmupJumpSeen {
			st.ready = true
		}
		c.mu.Unlock()
		return
	}
	lastClean := st.lastClean
	c.mu.Unlock()

	if c.cfg.MaxTickDeltaUSD > 0 && math.Abs(tk.Price-lastClean) > c.cfg.MaxTickDeltaUSD {
		c.incDrop("delta")
		return
	}
	key := dedupeKey(tk)
	now = time.Now().UTC()
	if c.isDuplicate(key, now) {
		c.incDrop("dedupe")
		return
	}

	c.mu.Lock()
	if st := c.states[tk.Slug]; st != nil {
		st.lastClean = tk.Price
	}
	c.mu.Unlock()

	select {
	case <-c.ctx.Done():
		return
	case c.out <- tk:
	default:
		c.incDrop("downstream_full")
	}
}

func (c *Cleaner) isDuplicate(key string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	cutoff := now.Add(-c.ttl)
	for k, ts := range c.dedupe {
		if ts.Before(cutoff) {
			delete(c.dedupe, k)
		}
	}
	if ts, ok := c.dedupe[key]; ok && now.Sub(ts) <= c.ttl {
		return true
	}
	c.dedupe[key] = now
	return false
}

func dedupeKey(tk Tick) string {
	price := math.Round(tk.Price*1e4) / 1e4
	return tk.Slug + "|" + tk.Side + "|" + formatFloat(price) + "|" + formatFloat(tk.Size)
}

func formatFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func (c *Cleaner) incDrop(reason string) {
	if c.metrics != nil {
		c.metrics.IncCounter("polymarket_ws_ticks_dropped_total", map[string]string{"reason": reason})
	}
}
