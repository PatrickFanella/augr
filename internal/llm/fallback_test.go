package llm_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

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

func TestFallbackProviderDoesNotFallBackOnDeadlineExceeded(t *testing.T) {
	t.Parallel()

	primary := newMockProvider(nil, []error{fmt.Errorf("timeout: %w", context.DeadlineExceeded)})
	secondary := newMockProvider([]*llm.CompletionResponse{{Content: "secondary"}}, []error{nil})

	fp, err := llm.NewFallbackProvider(primary, secondary, discardLogger())
	if err != nil {
		t.Fatalf("NewFallbackProvider() error = %v", err)
	}

	_, err = fp.Complete(context.Background(), llm.CompletionRequest{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Complete() error = %v, want context.DeadlineExceeded", err)
	}
	if secondary.calls.Load() != 0 {
		t.Errorf("secondary calls = %d, want 0 (no fallback on deadline exceeded)", secondary.calls.Load())
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
