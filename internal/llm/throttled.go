package llm

import "context"

// ThrottledProvider wraps a Provider with a concurrency limiter.
// All callers share the same semaphore, preventing Ollama (or any
// serial LLM backend) from being overwhelmed by concurrent requests.
type ThrottledProvider struct {
	inner Provider
	sem   chan struct{}
}

// NewThrottledProvider creates a provider that allows at most maxConcurrent
// simultaneous LLM calls. Additional calls block until a slot is available.
func NewThrottledProvider(inner Provider, maxConcurrent int) *ThrottledProvider {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &ThrottledProvider{
		inner: inner,
		sem:   make(chan struct{}, maxConcurrent),
	}
}

func (t *ThrottledProvider) Complete(ctx context.Context, request CompletionRequest) (*CompletionResponse, error) {
	select {
	case t.sem <- struct{}{}:
		defer func() { <-t.sem }()
		return t.inner.Complete(ctx, request)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
