package polymarket

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

type poolConnection interface {
	Dial(context.Context) error
	Run(context.Context) error
	Close() error
}

type connFactory func(id int, cfg Config, ticks chan<- Tick, books chan<- BookSnapshot, dropped *atomic.Uint64) poolConnection

type member struct {
	mu         sync.Mutex
	id         int
	conn       poolConnection
	ticks      chan Tick
	books      chan BookSnapshot
	jitterMS   float64
	lastTickAt time.Time
	cancel     context.CancelFunc
}

type Pool struct {
	cfg     Config
	factory connFactory
	ticks   chan Tick
	books   chan BookSnapshot
	dropped atomic.Uint64
	mu      sync.Mutex
	members map[int]*member
	nextID  int
	started atomic.Bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

type PoolStats struct {
	Members     int
	AvgJitterMS float64
	Dropped     uint64
}

func NewPool(cfg Config) *Pool { return newPoolWithFactory(cfg, nil) }

func newPoolWithFactory(cfg Config, factory connFactory) *Pool {
	if factory == nil {
		factory = func(id int, cfg Config, ticks chan<- Tick, books chan<- BookSnapshot, dropped *atomic.Uint64) poolConnection {
			return newConnection(id, cfg, ticks, books, dropped)
		}
	}
	return &Pool{cfg: cfg, factory: factory, members: make(map[int]*member)}
}

func (p *Pool) Start(ctx context.Context) error {
	if err := p.cfg.Validate(); err != nil {
		return err
	}
	p.mu.Lock()
	if p.started.Load() {
		p.mu.Unlock()
		return nil
	}
	p.ticks = make(chan Tick, max(1, 10*p.cfg.ConnectionsPerFeed))
	p.books = make(chan BookSnapshot, max(1, 2*p.cfg.ConnectionsPerFeed))
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.started.Store(true)
	p.mu.Unlock()

	for i := 0; i < p.cfg.ConnectionsPerFeed; i++ {
		if err := p.startMember(p.ctx); err != nil {
			p.Close()
			return err
		}
		if i+1 < p.cfg.ConnectionsPerFeed && p.cfg.StaggerStartup > 0 {
			select {
			case <-time.After(p.cfg.StaggerStartup):
			case <-p.ctx.Done():
				return p.ctx.Err()
			}
		}
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.pruneLoop()
	}()
	return nil
}

func (p *Pool) startMember(ctx context.Context) error {
	p.mu.Lock()
	id := p.nextID
	p.nextID++
	ticks := make(chan Tick, max(1, 4))
	books := make(chan BookSnapshot, max(1, 2))
	dropped := &p.dropped
	conn := p.factory(id, p.cfg, ticks, books, dropped)
	memberCtx, cancel := context.WithCancel(ctx)
	m := &member{id: id, conn: conn, ticks: ticks, books: books, cancel: cancel}
	p.members[id] = m
	p.mu.Unlock()

	if err := conn.Dial(memberCtx); err != nil {
		cancel()
		p.mu.Lock()
		delete(p.members, id)
		p.mu.Unlock()
		return err
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.runMember(memberCtx, m)
	}()
	return nil
}

func (p *Pool) runMember(ctx context.Context, m *member) {
	defer func() { _ = m.conn.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = m.conn.Run(ctx)
	}()
	for m.ticks != nil || m.books != nil {
		select {
		case <-ctx.Done():
			return
		case tk, ok := <-m.ticks:
			if !ok {
				m.ticks = nil
				continue
			}
			p.observeTick(m, tk)
		case bs, ok := <-m.books:
			if !ok {
				m.books = nil
				continue
			}
			select {
			case p.books <- bs:
			default:
				p.dropped.Add(1)
			}
		case <-done:
			return
		}
	}
	p.mu.Lock()
	if cur, ok := p.members[m.id]; ok && cur == m {
		delete(p.members, m.id)
	}
	p.mu.Unlock()
}

func (p *Pool) observeTick(m *member, tk Tick) {
	now := tk.ReceivedAt
	if now.IsZero() {
		now = time.Now()
	}
	if !m.lastTickAt.IsZero() {
		delta := float64(now.Sub(m.lastTickAt).Milliseconds())
		if delta < 0 {
			delta = 0
		}
		alpha := p.cfg.JitterEMAAlpha
		m.mu.Lock()
		m.jitterMS = (1-alpha)*m.jitterMS + alpha*delta
		m.mu.Unlock()
	}
	m.mu.Lock()
	m.lastTickAt = now
	m.mu.Unlock()
	tk.ConnID = m.id
	select {
	case p.ticks <- tk:
	default:
		p.dropped.Add(1)
	}
}

func (p *Pool) pruneLoop() {
	if p.cfg.PruneInterval <= 0 {
		return
	}
	t := time.NewTicker(p.cfg.PruneInterval)
	defer t.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-t.C:
			p.pruneOnce()
		}
	}
}

func (p *Pool) pruneOnce() {
	p.mu.Lock()
	if len(p.members) == 0 {
		p.mu.Unlock()
		return
	}
	list := make([]*member, 0, len(p.members))
	for _, m := range p.members {
		list = append(list, m)
	}
	p.mu.Unlock()
	sortMembersByJitterDesc(list)
	count := int(math.Ceil(p.cfg.PruneFraction * float64(len(list))))
	if count < 1 {
		count = 1
	}
	if count > len(list) {
		count = len(list)
	}
	for i := 0; i < count; i++ {
		p.replaceMember(list[i].id)
	}
}

func sortMembersByJitterDesc(ms []*member) {
	for i := 0; i < len(ms); i++ {
		for j := i + 1; j < len(ms); j++ {
			ms[i].mu.Lock()
			ji := ms[i].jitterMS
			ms[i].mu.Unlock()
			ms[j].mu.Lock()
			jj := ms[j].jitterMS
			ms[j].mu.Unlock()
			if jj > ji {
				ms[i], ms[j] = ms[j], ms[i]
			}
		}
	}
}

func (p *Pool) replaceMember(id int) {
	p.mu.Lock()
	m, ok := p.members[id]
	if ok {
		delete(p.members, id)
	}
	p.mu.Unlock()
	if !ok {
		return
	}
	m.cancel()
	_ = m.conn.Close()
	_ = p.startMember(p.ctx)
}

func (p *Pool) Ticks() <-chan Tick         { return p.ticks }
func (p *Pool) Books() <-chan BookSnapshot { return p.books }
func (p *Pool) Dropped() uint64            { return p.dropped.Load() }

func (p *Pool) Stats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	var sum float64
	for _, m := range p.members {
		m.mu.Lock()
		sum += m.jitterMS
		m.mu.Unlock()
	}
	avg := 0.0
	if len(p.members) > 0 {
		avg = sum / float64(len(p.members))
	}
	return PoolStats{Members: len(p.members), AvgJitterMS: avg, Dropped: p.dropped.Load()}
}

func (p *Pool) Close() {
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Lock()
	for _, m := range p.members {
		m.cancel()
		_ = m.conn.Close()
	}
	p.members = map[int]*member{}
	p.mu.Unlock()
	p.wg.Wait()
	if p.ticks != nil {
		close(p.ticks)
	}
	if p.books != nil {
		close(p.books)
	}
}
