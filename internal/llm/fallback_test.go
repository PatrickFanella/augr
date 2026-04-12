package llm_test

import (
	"bytes"
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

type metricsRecorderStub struct {
	reasons []string
}

func (m *metricsRecorderStub) RecordLLMFallback(reason string) {
	m.reasons = append(m.reasons, reason)
}

type contextInspectingProvider struct {
	response      *llm.CompletionResponse
	err           error
	calls         atomic.Int32
	ctxExpired    atomic.Bool
	hasDeadline   atomic.Bool
	deadlineAfter atomic.Int64
}

func (p *contextInspectingProvider) Complete(ctx context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	p.calls.Add(1)
	p.ctxExpired.Store(ctx.Err() != nil)
	if deadline, ok := ctx.Deadline(); ok {
		p.hasDeadline.Store(true)
		p.deadlineAfter.Store(int64(time.Until(deadline)))
	}
	return p.response, p.err
}

// --- FallbackProvider Tests ---

func TestFallbackProviderReturnsPrimaryOnSuccess(t *testing.T) {
	t.Parallel()

	want := &llm.CompletionResponse{Content: "primary"}
	primary := newMockProvider([]*llm.CompletionResponse{want}, []error{nil})
	secondary := newMockProvider([]*llm.CompletionResponse{{Content: "secondary"}}, []error{nil})

	fp, err := llm.NewFallbackProvider(primary, secondary, discardLogger())
	if err != nil {
		t.Fatalf("NewFallbackProvider() error = %v", err)
	}

	got, err := fp.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete() error = %v, want nil", err)
	}
	if got.Content != "primary" {
		t.Errorf("Complete() content = %q, want %q", got.Content, "primary")
	}
	if secondary.calls.Load() != 0 {
		t.Errorf("secondary calls = %d, want 0", secondary.calls.Load())
	}
}

func TestFallbackProviderFallsBackOnPrimaryFailure(t *testing.T) {
	t.Parallel()

	primary := newMockProvider(nil, []error{errors.New("primary down")})
	want := &llm.CompletionResponse{Content: "secondary"}
	secondary := newMockProvider([]*llm.CompletionResponse{want}, []error{nil})

	fp, err := llm.NewFallbackProvider(primary, secondary, discardLogger())
	if err != nil {
		t.Fatalf("NewFallbackProvider() error = %v", err)
	}

	got, err := fp.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete() error = %v, want nil", err)
	}
	if got.Content != "secondary" {
		t.Errorf("Complete() content = %q, want %q", got.Content, "secondary")
	}
	if primary.calls.Load() != 1 {
		t.Errorf("primary calls = %d, want 1", primary.calls.Load())
	}
	if secondary.calls.Load() != 1 {
		t.Errorf("secondary calls = %d, want 1", secondary.calls.Load())
	}
}

func TestFallbackProviderReturnsBothErrors(t *testing.T) {
	t.Parallel()

	primary := newMockProvider(nil, []error{errors.New("primary fail")})
	secondary := newMockProvider(nil, []error{errors.New("secondary fail")})

	fp, err := llm.NewFallbackProvider(primary, secondary, discardLogger())
	if err != nil {
		t.Fatalf("NewFallbackProvider() error = %v", err)
	}

	_, err = fp.Complete(context.Background(), llm.CompletionRequest{})
	if err == nil {
		t.Fatal("Complete() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "secondary fail") {
		t.Errorf("Complete() error = %q, want secondary error", err.Error())
	}
}

func TestFallbackProviderLogsFallbackEvent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	primary := newMockProvider(nil, []error{errors.New("primary fail")})
	secondary := newMockProvider([]*llm.CompletionResponse{{Content: "ok"}}, []error{nil})

	fp, err := llm.NewFallbackProvider(primary, secondary, logger)
	if err != nil {
		t.Fatalf("NewFallbackProvider() error = %v", err)
	}

	_, err = fp.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete() error = %v, want nil", err)
	}

	logged := buf.String()
	if !strings.Contains(logged, "primary provider failed") {
		t.Errorf("log output = %q, want fallback event logged", logged)
	}
	if !strings.Contains(logged, "primary fail") {
		t.Errorf("log output = %q, want error detail logged", logged)
	}
}

func TestFallbackProviderDoesNotFallBackOnContextCanceled(t *testing.T) {
	t.Parallel()

	primary := newMockProvider(nil, []error{context.Canceled})
	secondary := newMockProvider([]*llm.CompletionResponse{{Content: "secondary"}}, []error{nil})

	fp, err := llm.NewFallbackProvider(primary, secondary, discardLogger())
	if err != nil {
		t.Fatalf("NewFallbackProvider() error = %v", err)
	}

	_, err = fp.Complete(context.Background(), llm.CompletionRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Complete() error = %v, want context.Canceled", err)
	}
	if secondary.calls.Load() != 0 {
		t.Errorf("secondary calls = %d, want 0 (no fallback on context cancel)", secondary.calls.Load())
	}
}

func TestFallbackProviderFallsBackOnDeadlineExceeded(t *testing.T) {
	t.Parallel()

	primary := newMockProvider(nil, []error{fmt.Errorf("timeout: %w", context.DeadlineExceeded)})
	secondary := &contextInspectingProvider{response: &llm.CompletionResponse{Content: "secondary"}}
	metrics := &metricsRecorderStub{}

	fp, err := llm.NewFallbackProvider(primary, secondary, discardLogger())
	if err != nil {
		t.Fatalf("NewFallbackProvider() error = %v", err)
	}
	fp.WithMetrics(metrics)

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)

	got, err := fp.Complete(ctx, llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete() error = %v, want nil", err)
	}
	if got.Content != "secondary" {
		t.Fatalf("Complete() content = %q, want %q", got.Content, "secondary")
	}
	if secondary.calls.Load() != 1 {
		t.Fatalf("secondary calls = %d, want 1", secondary.calls.Load())
	}
	if secondary.ctxExpired.Load() {
		t.Fatal("secondary context was already expired, want fresh context")
	}
	if !secondary.hasDeadline.Load() {
		t.Fatal("secondary context missing deadline")
	}
	if got := time.Duration(secondary.deadlineAfter.Load()); got <= 0 {
		t.Fatalf("secondary deadline remaining = %v, want > 0", got)
	}
	if len(metrics.reasons) != 1 || metrics.reasons[0] != "deadline_exceeded" {
		t.Fatalf("metrics reasons = %v, want [deadline_exceeded]", metrics.reasons)
	}
}

func TestFallbackProviderRecordsProviderErrorMetric(t *testing.T) {
	t.Parallel()

	primary := newMockProvider(nil, []error{errors.New("primary down")})
	secondary := newMockProvider([]*llm.CompletionResponse{{Content: "secondary"}}, []error{nil})
	metrics := &metricsRecorderStub{}

	fp, err := llm.NewFallbackProvider(primary, secondary, discardLogger())
	if err != nil {
		t.Fatalf("NewFallbackProvider() error = %v", err)
	}
	fp.WithMetrics(metrics)

	_, err = fp.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete() error = %v, want nil", err)
	}
	if len(metrics.reasons) != 1 || metrics.reasons[0] != "provider_error" {
		t.Fatalf("metrics reasons = %v, want [provider_error]", metrics.reasons)
	}
}

func TestNewFallbackProviderRejectsNilPrimary(t *testing.T) {
	t.Parallel()

	secondary := newMockProvider([]*llm.CompletionResponse{{Content: "ok"}}, []error{nil})

	_, err := llm.NewFallbackProvider(nil, secondary, discardLogger())
	if err == nil {
		t.Fatal("NewFallbackProvider() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "primary") {
		t.Errorf("NewFallbackProvider() error = %q, want primary nil error", err.Error())
	}
}

func TestNewFallbackProviderRejectsNilSecondary(t *testing.T) {
	t.Parallel()

	primary := newMockProvider([]*llm.CompletionResponse{{Content: "ok"}}, []error{nil})

	_, err := llm.NewFallbackProvider(primary, nil, discardLogger())
	if err == nil {
		t.Fatal("NewFallbackProvider() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "secondary") {
		t.Errorf("NewFallbackProvider() error = %q, want secondary nil error", err.Error())
	}
}

func TestFallbackProviderImplementsProviderInterface(t *testing.T) {
	t.Parallel()

	var _ llm.Provider = (*llm.FallbackProvider)(nil)
}
