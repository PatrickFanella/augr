package llm_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// --- helpers ---

// trackingProvider records call count + last request, returns canned response.
type trackingProvider struct {
	response *llm.CompletionResponse
	err      error
	calls    atomic.Int32
}

func (p *trackingProvider) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	p.calls.Add(1)
	return p.response, p.err
}

type stubCacheMetrics struct {
	hits   atomic.Int32
	misses atomic.Int32
}

func (s *stubCacheMetrics) RecordLLMCacheHit()  { s.hits.Add(1) }
func (s *stubCacheMetrics) RecordLLMCacheMiss() { s.misses.Add(1) }

type stubChainBudgetMetrics struct{ calls atomic.Int32 }

func (s *stubChainBudgetMetrics) RecordLLMBudgetExhausted() { s.calls.Add(1) }

// --- NewProviderChain tests ---

func TestProviderChain_PrimaryOnly(t *testing.T) {
	t.Parallel()

	want := &llm.CompletionResponse{Content: "ok", Usage: llm.CompletionUsage{PromptTokens: 5, CompletionTokens: 3}}
	p := &trackingProvider{response: want}

	chain := llm.NewProviderChain(p, discardLogger())

	got, err := chain.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != "ok" {
		t.Errorf("content = %q, want %q", got.Content, "ok")
	}
	if p.calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", p.calls.Load())
	}
}

func TestProviderChain_WithThrottle(t *testing.T) {
	t.Parallel()

	want := &llm.CompletionResponse{Content: "throttled"}
	p := &trackingProvider{response: want}

	chain := llm.NewProviderChain(p, discardLogger(), llm.WithThrottle(2))

	got, err := chain.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != "throttled" {
		t.Errorf("content = %q, want %q", got.Content, "throttled")
	}
}

func TestProviderChain_WithThrottleClamp(t *testing.T) {
	t.Parallel()

	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	start := make(chan struct{})
	attempting := make(chan struct{}, 2)
	entered := make(chan struct{}, 2)
	block := make(chan struct{})

	p := llm.ProviderFunc(func(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
		cur := inFlight.Add(1)
		entered <- struct{}{}
		for {
			prev := maxInFlight.Load()
			if cur <= prev || maxInFlight.CompareAndSwap(prev, cur) {
				break
			}
		}
		<-block
		inFlight.Add(-1)
		return &llm.CompletionResponse{Content: "ok"}, nil
	})

	chain := llm.NewProviderChain(p, discardLogger(), llm.WithThrottle(0))

	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			<-start
			attempting <- struct{}{}
			_, _ = chain.Complete(context.Background(), llm.CompletionRequest{})
		}()
	}

	close(start)
	<-attempting
	<-attempting
	<-entered
	if inFlight.Load() != 1 {
		t.Fatalf("in-flight before release = %d, want 1", inFlight.Load())
	}
	close(block)
	<-entered
	wg.Wait()

	if maxInFlight.Load() != 1 {
		t.Errorf("max in-flight = %d, want 1 (throttle should clamp to 1)", maxInFlight.Load())
	}
}

func TestProviderChain_WithCache(t *testing.T) {
	t.Parallel()

	want := &llm.CompletionResponse{Content: "cached", Usage: llm.CompletionUsage{PromptTokens: 1}}
	p := &trackingProvider{response: want}
	cache := llm.NewMemoryResponseCache()
	cm := &stubCacheMetrics{}

	chain := llm.NewProviderChain(p, discardLogger(),
		llm.WithCache(cache),
		llm.WithChainCacheMetrics(cm),
	)

	req := llm.CompletionRequest{Model: "test", Messages: []llm.Message{{Role: "user", Content: "hello"}}}

	// First call: miss
	got, err := chain.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("call 1: %v", err)
	}
	if got.Content != "cached" {
		t.Errorf("call 1 content = %q, want %q", got.Content, "cached")
	}

	// Second call: hit (provider not called again)
	got2, err := chain.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("call 2: %v", err)
	}
	if got2.Content != "cached" {
		t.Errorf("call 2 content = %q, want %q", got2.Content, "cached")
	}
	if p.calls.Load() != 1 {
		t.Errorf("provider calls = %d, want 1 (cache should serve second)", p.calls.Load())
	}
	if cm.hits.Load() < 1 {
		t.Errorf("cache hits = %d, want ≥1", cm.hits.Load())
	}
}

func TestProviderChain_WithFallback(t *testing.T) {
	t.Parallel()

	primary := &trackingProvider{err: errors.New("primary down")}
	secondary := &trackingProvider{response: &llm.CompletionResponse{Content: "fallback"}}

	chain := llm.NewProviderChain(primary, discardLogger(), llm.WithFallback(secondary))

	got, err := chain.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != "fallback" {
		t.Errorf("content = %q, want %q", got.Content, "fallback")
	}
	if secondary.calls.Load() != 1 {
		t.Errorf("secondary calls = %d, want 1", secondary.calls.Load())
	}
}

func TestProviderChain_WithRetry(t *testing.T) {
	t.Parallel()

	// Fail once with 429, succeed on retry
	retryable := &httpError{code: 429, msg: "rate limited"}
	want := &llm.CompletionResponse{Content: "retried", Usage: llm.CompletionUsage{PromptTokens: 2}}

	mock := newMockProvider(
		[]*llm.CompletionResponse{nil, want},
		[]error{retryable, nil},
	)
	chain := llm.NewProviderChain(mock, discardLogger(),
		llm.WithRetry(3),
		llm.WithRetryBaseDelay(1*time.Millisecond),
	)

	got, err := chain.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != "retried" {
		t.Errorf("content = %q, want %q", got.Content, "retried")
	}
	if mock.calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", mock.calls.Load())
	}
}

func TestProviderChain_WithRetryMetrics(t *testing.T) {
	t.Parallel()

	mock := newMockProvider(
		[]*llm.CompletionResponse{nil, {Content: "ok"}},
		[]error{&httpError{code: 500, msg: "transient"}, nil},
	)
	retryStub := &stubRetryMetrics{}

	chain := llm.NewProviderChain(mock, discardLogger(),
		llm.WithRetry(2),
		llm.WithRetryBaseDelay(1*time.Millisecond),
		llm.WithChainRetryMetrics(retryStub),
	)

	got, err := chain.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != "ok" {
		t.Fatalf("content = %q, want %q", got.Content, "ok")
	}
	if retryStub.calls.Load() != 1 {
		t.Fatalf("retry metrics calls = %d, want 1", retryStub.calls.Load())
	}
}

func TestProviderChain_WithBudgetExhausted(t *testing.T) {
	t.Parallel()

	p := &trackingProvider{response: &llm.CompletionResponse{Content: "should not reach"}}
	budget := llm.NewBudget(1, 0) // 1 request max

	chain := llm.NewProviderChain(p, discardLogger(), llm.WithBudget(budget))

	// First call succeeds, uses up budget
	_, err := chain.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("call 1: %v", err)
	}

	// Second call rejected
	_, err = chain.Complete(context.Background(), llm.CompletionRequest{})
	if !errors.Is(err, llm.ErrBudgetExhausted) {
		t.Errorf("call 2 error = %v, want ErrBudgetExhausted", err)
	}
	if p.calls.Load() != 1 {
		t.Errorf("provider calls = %d, want 1 (budget should block second)", p.calls.Load())
	}
}

func TestProviderChain_BudgetExhaustedNotRetried(t *testing.T) {
	t.Parallel()

	p := &trackingProvider{response: &llm.CompletionResponse{Content: "ok"}}
	budget := llm.NewBudget(1, 0)

	// Chain with retry + budget. Budget is outermost, so exhaustion bypasses retry.
	chain := llm.NewProviderChain(p, discardLogger(),
		llm.WithRetry(3),
		llm.WithRetryBaseDelay(1*time.Millisecond),
		llm.WithBudget(budget),
	)

	// Exhaust budget
	_, _ = chain.Complete(context.Background(), llm.CompletionRequest{})

	// Next call: budget error, not retried
	_, err := chain.Complete(context.Background(), llm.CompletionRequest{})
	if !errors.Is(err, llm.ErrBudgetExhausted) {
		t.Errorf("error = %v, want ErrBudgetExhausted", err)
	}
	// Provider should only have been called once (the first success)
	if p.calls.Load() != 1 {
		t.Errorf("provider calls = %d, want 1", p.calls.Load())
	}
}

func TestProviderChain_WithBudgetMetrics(t *testing.T) {
	t.Parallel()

	p := &trackingProvider{response: &llm.CompletionResponse{Content: "ok"}}
	budget := llm.NewBudget(1, 0)
	budgetMetrics := &stubChainBudgetMetrics{}

	chain := llm.NewProviderChain(p, discardLogger(),
		llm.WithBudget(budget),
		llm.WithChainBudgetMetrics(budgetMetrics),
	)

	_, err := chain.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("call 1: %v", err)
	}
	if budgetMetrics.calls.Load() != 0 {
		t.Fatalf("budget metrics calls after success = %d, want 0", budgetMetrics.calls.Load())
	}

	_, err = chain.Complete(context.Background(), llm.CompletionRequest{})
	if !errors.Is(err, llm.ErrBudgetExhausted) {
		t.Fatalf("call 2 error = %v, want ErrBudgetExhausted", err)
	}
	if budgetMetrics.calls.Load() != 1 {
		t.Fatalf("budget metrics calls after rejection = %d, want 1", budgetMetrics.calls.Load())
	}
}

func TestProviderChain_WithCallTimeout(t *testing.T) {
	t.Parallel()

	var sawDeadline atomic.Bool
	slow := llm.ProviderFunc(func(ctx context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
		if _, ok := ctx.Deadline(); ok {
			sawDeadline.Store(true)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})

	chain := llm.NewProviderChain(slow, discardLogger(),
		llm.WithCallTimeout(50*time.Millisecond),
	)

	_, err := chain.Complete(context.Background(), llm.CompletionRequest{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want DeadlineExceeded", err)
	}
	if !sawDeadline.Load() {
		t.Error("expected provider context to include a deadline")
	}
}

func TestProviderChain_FullStack(t *testing.T) {
	t.Parallel()

	// Full chain: budget → timeout → throttle → retry → fallback → cache → primary
	primary := &trackingProvider{
		response: &llm.CompletionResponse{
			Content: "full-stack",
			Usage:   llm.CompletionUsage{PromptTokens: 10, CompletionTokens: 5},
		},
	}
	secondary := &trackingProvider{response: &llm.CompletionResponse{Content: "backup"}}
	cache := llm.NewMemoryResponseCache()
	budget := llm.NewBudget(100, 0)

	chain := llm.NewProviderChain(primary, discardLogger(),
		llm.WithThrottle(4),
		llm.WithRetry(2),
		llm.WithRetryBaseDelay(1*time.Millisecond),
		llm.WithFallback(secondary),
		llm.WithCache(cache),
		llm.WithBudget(budget),
		llm.WithCallTimeout(5*time.Second),
	)

	got, err := chain.Complete(context.Background(), llm.CompletionRequest{
		Model:    "test",
		Messages: []llm.Message{{Role: "user", Content: "full test"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != "full-stack" {
		t.Errorf("content = %q, want %q", got.Content, "full-stack")
	}

	stats := budget.Stats()
	if stats.Requests != 1 {
		t.Errorf("budget requests = %d, want 1", stats.Requests)
	}
}

func TestProviderChain_SubsetOptions(t *testing.T) {
	t.Parallel()

	// Only throttle + cache, no retry/fallback/budget/timeout
	p := &trackingProvider{
		response: &llm.CompletionResponse{Content: "subset"},
	}

	chain := llm.NewProviderChain(p, discardLogger(),
		llm.WithThrottle(2),
		llm.WithCache(llm.NewMemoryResponseCache()),
	)

	got, err := chain.Complete(context.Background(), llm.CompletionRequest{
		Model:    "m",
		Messages: []llm.Message{{Role: "user", Content: "x"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != "subset" {
		t.Errorf("content = %q, want %q", got.Content, "subset")
	}
}
