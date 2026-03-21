package llm_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// httpError implements the statusCoder interface used by RetryProvider.
type httpError struct {
	code int
	msg  string
}

func (e *httpError) Error() string  { return e.msg }
func (e *httpError) StatusCode() int { return e.code }
func (e *httpError) Unwrap() error  { return nil }

// mockProvider is a configurable Provider for testing retry and fallback logic.
type mockProvider struct {
	responses []*llm.CompletionResponse
	errors    []error
	calls     atomic.Int32
}

func (m *mockProvider) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	i := int(m.calls.Add(1)) - 1
	if i >= len(m.errors) {
		return m.responses[len(m.responses)-1], nil
	}
	return m.responses[i], m.errors[i]
}

func newMockProvider(responses []*llm.CompletionResponse, errs []error) *mockProvider {
	// Pad responses with nil if shorter than errors.
	for len(responses) < len(errs) {
		responses = append(responses, nil)
	}
	return &mockProvider{responses: responses, errors: errs}
}

// immediateTimerFn returns a timer function that fires immediately for testing.
func immediateTimerFn() func(time.Duration) (<-chan time.Time, func() bool) {
	return func(_ time.Duration) (<-chan time.Time, func() bool) {
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch, func() bool { return false }
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discard{}, nil))
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// --- RetryProvider Tests ---

func TestRetryProviderSucceedsOnFirstAttempt(t *testing.T) {
	t.Parallel()

	want := &llm.CompletionResponse{
		Content: "hello",
		Usage:   llm.CompletionUsage{PromptTokens: 10, CompletionTokens: 5},
	}
	mock := newMockProvider([]*llm.CompletionResponse{want}, []error{nil})

	rp := llm.NewRetryProvider(mock, discardLogger())
	rp.SetTimerFn(immediateTimerFn())

	got, err := rp.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete() error = %v, want nil", err)
	}
	if got.Content != "hello" {
		t.Errorf("Complete() content = %q, want %q", got.Content, "hello")
	}
	if mock.calls.Load() != 1 {
		t.Errorf("Complete() calls = %d, want 1", mock.calls.Load())
	}
}

func TestRetryProviderRetriesOnTransientError(t *testing.T) {
	t.Parallel()

	want := &llm.CompletionResponse{Content: "ok", Usage: llm.CompletionUsage{PromptTokens: 5, CompletionTokens: 3}}
	mock := newMockProvider(
		[]*llm.CompletionResponse{nil, want},
		[]error{&httpError{code: 500, msg: "server error"}, nil},
	)

	rp := llm.NewRetryProvider(mock, discardLogger())
	rp.SetTimerFn(immediateTimerFn())

	got, err := rp.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete() error = %v, want nil", err)
	}
	if got.Content != "ok" {
		t.Errorf("Complete() content = %q, want %q", got.Content, "ok")
	}
	if mock.calls.Load() != 2 {
		t.Errorf("Complete() calls = %d, want 2", mock.calls.Load())
	}
}

func TestRetryProviderRetriesOnRateLimit(t *testing.T) {
	t.Parallel()

	want := &llm.CompletionResponse{Content: "ok"}
	mock := newMockProvider(
		[]*llm.CompletionResponse{nil, want},
		[]error{&httpError{code: 429, msg: "rate limited"}, nil},
	)

	rp := llm.NewRetryProvider(mock, discardLogger())
	rp.SetTimerFn(immediateTimerFn())

	got, err := rp.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete() error = %v, want nil", err)
	}
	if got.Content != "ok" {
		t.Errorf("Complete() content = %q, want %q", got.Content, "ok")
	}
}

func TestRetryProviderRetriesOnTimeout(t *testing.T) {
	t.Parallel()

	want := &llm.CompletionResponse{Content: "ok"}
	mock := newMockProvider(
		[]*llm.CompletionResponse{nil, want},
		[]error{fmt.Errorf("timed out: %w", context.DeadlineExceeded), nil},
	)

	rp := llm.NewRetryProvider(mock, discardLogger())
	rp.SetTimerFn(immediateTimerFn())

	got, err := rp.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete() error = %v, want nil", err)
	}
	if got.Content != "ok" {
		t.Errorf("Complete() content = %q, want %q", got.Content, "ok")
	}
}

func TestRetryProviderDoesNotRetryOnBadRequest(t *testing.T) {
	t.Parallel()

	mock := newMockProvider(nil, []error{&httpError{code: 400, msg: "bad request"}})

	rp := llm.NewRetryProvider(mock, discardLogger())
	rp.SetTimerFn(immediateTimerFn())

	_, err := rp.Complete(context.Background(), llm.CompletionRequest{})
	if err == nil {
		t.Fatal("Complete() error = nil, want non-nil")
	}
	if mock.calls.Load() != 1 {
		t.Errorf("Complete() calls = %d, want 1 (no retry)", mock.calls.Load())
	}
}

func TestRetryProviderDoesNotRetryOnAuthError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code int
	}{
		{"unauthorized", 401},
		{"forbidden", 403},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mock := newMockProvider(nil, []error{&httpError{code: tc.code, msg: tc.name}})

			rp := llm.NewRetryProvider(mock, discardLogger())
			rp.SetTimerFn(immediateTimerFn())

			_, err := rp.Complete(context.Background(), llm.CompletionRequest{})
			if err == nil {
				t.Fatal("Complete() error = nil, want non-nil")
			}
			if mock.calls.Load() != 1 {
				t.Errorf("Complete() calls = %d, want 1 (no retry)", mock.calls.Load())
			}
		})
	}
}

func TestRetryProviderDoesNotRetryOnContextCanceled(t *testing.T) {
	t.Parallel()

	mock := newMockProvider(nil, []error{context.Canceled})

	rp := llm.NewRetryProvider(mock, discardLogger())
	rp.SetTimerFn(immediateTimerFn())

	_, err := rp.Complete(context.Background(), llm.CompletionRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Complete() error = %v, want context.Canceled", err)
	}
	if mock.calls.Load() != 1 {
		t.Errorf("Complete() calls = %d, want 1", mock.calls.Load())
	}
}

func TestRetryProviderDoesNotRetryOnUnknownError(t *testing.T) {
	t.Parallel()

	mock := newMockProvider(nil, []error{errors.New("unknown error")})

	rp := llm.NewRetryProvider(mock, discardLogger(), llm.WithMaxAttempts(3))
	rp.SetTimerFn(immediateTimerFn())

	_, err := rp.Complete(context.Background(), llm.CompletionRequest{})
	if err == nil {
		t.Fatal("Complete() error = nil, want non-nil")
	}
	if mock.calls.Load() != 1 {
		t.Errorf("Complete() calls = %d, want 1 (no retry on unknown error)", mock.calls.Load())
	}
}

func TestRetryProviderReturnsLastErrorAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	mock := newMockProvider(nil, []error{
		&httpError{code: 500, msg: "fail-1"},
		&httpError{code: 500, msg: "fail-2"},
		&httpError{code: 500, msg: "fail-3"},
	})

	rp := llm.NewRetryProvider(mock, discardLogger(), llm.WithMaxAttempts(3))
	rp.SetTimerFn(immediateTimerFn())

	_, err := rp.Complete(context.Background(), llm.CompletionRequest{})
	if err == nil {
		t.Fatal("Complete() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "fail-3") {
		t.Errorf("Complete() error = %q, want last error", err.Error())
	}
	if mock.calls.Load() != 3 {
		t.Errorf("Complete() calls = %d, want 3", mock.calls.Load())
	}
}

func TestRetryProviderAggregatesUsageAcrossRetries(t *testing.T) {
	t.Parallel()

	// First attempt returns partial usage with an error.
	partialResp := &llm.CompletionResponse{
		Usage: llm.CompletionUsage{PromptTokens: 10, CompletionTokens: 0},
	}
	successResp := &llm.CompletionResponse{
		Content: "ok",
		Usage:   llm.CompletionUsage{PromptTokens: 10, CompletionTokens: 5},
	}

	mock := &mockProvider{
		responses: []*llm.CompletionResponse{partialResp, successResp},
		errors:    []error{&httpError{code: 500, msg: "server error"}, nil},
	}

	rp := llm.NewRetryProvider(mock, discardLogger())
	rp.SetTimerFn(immediateTimerFn())

	got, err := rp.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete() error = %v, want nil", err)
	}
	if got.Usage.PromptTokens != 20 {
		t.Errorf("Complete() prompt tokens = %d, want 20", got.Usage.PromptTokens)
	}
	if got.Usage.CompletionTokens != 5 {
		t.Errorf("Complete() completion tokens = %d, want 5", got.Usage.CompletionTokens)
	}
}

func TestRetryProviderRespectsContextCancellationBetweenRetries(t *testing.T) {
	t.Parallel()

	mock := newMockProvider(nil, []error{
		&httpError{code: 500, msg: "fail"},
		&httpError{code: 500, msg: "fail"},
	})

	ctx, cancel := context.WithCancel(context.Background())

	rp := llm.NewRetryProvider(mock, discardLogger(), llm.WithMaxAttempts(3))
	// Cancel context when timer fires to simulate cancellation between retries.
	rp.SetTimerFn(func(_ time.Duration) (<-chan time.Time, func() bool) {
		cancel()
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch, func() bool { return false }
	})

	_, err := rp.Complete(ctx, llm.CompletionRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Complete() error = %v, want context.Canceled", err)
	}
}

func TestRetryProviderWithCustomMaxAttempts(t *testing.T) {
	t.Parallel()

	mock := newMockProvider(nil, []error{
		&httpError{code: 502, msg: "fail-1"},
		&httpError{code: 502, msg: "fail-2"},
		&httpError{code: 502, msg: "fail-3"},
		&httpError{code: 502, msg: "fail-4"},
		&httpError{code: 502, msg: "fail-5"},
	})

	rp := llm.NewRetryProvider(mock, discardLogger(), llm.WithMaxAttempts(5))
	rp.SetTimerFn(immediateTimerFn())

	_, err := rp.Complete(context.Background(), llm.CompletionRequest{})
	if err == nil {
		t.Fatal("Complete() error = nil, want non-nil")
	}
	if mock.calls.Load() != 5 {
		t.Errorf("Complete() calls = %d, want 5", mock.calls.Load())
	}
}

func TestRetryProviderNilProviderReturnsError(t *testing.T) {
	t.Parallel()

	rp := llm.NewRetryProvider(nil, discardLogger())

	_, err := rp.Complete(context.Background(), llm.CompletionRequest{})
	if err == nil {
		t.Fatal("Complete() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("Complete() error = %q, want nil provider error", err.Error())
	}
}

func TestRetryProviderNilResponseWithoutErrorReturnsError(t *testing.T) {
	t.Parallel()

	// Provider returns (nil, nil).
	mock := newMockProvider([]*llm.CompletionResponse{nil}, []error{nil})

	rp := llm.NewRetryProvider(mock, discardLogger())
	rp.SetTimerFn(immediateTimerFn())

	_, err := rp.Complete(context.Background(), llm.CompletionRequest{})
	if err == nil {
		t.Fatal("Complete() error = nil, want non-nil for nil response")
	}
	if !strings.Contains(err.Error(), "nil response") {
		t.Errorf("Complete() error = %q, want nil response error", err.Error())
	}
}

func TestRetryProviderImplementsProviderInterface(t *testing.T) {
	t.Parallel()

	var _ llm.Provider = (*llm.RetryProvider)(nil)
}
