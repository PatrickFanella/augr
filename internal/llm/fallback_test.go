package llm_test

import (
	"bytes"
	"context"
	"errors"
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

	fp := llm.NewFallbackProvider(primary, secondary, discardLogger())

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

	fp := llm.NewFallbackProvider(primary, secondary, discardLogger())

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

	fp := llm.NewFallbackProvider(primary, secondary, discardLogger())

	_, err := fp.Complete(context.Background(), llm.CompletionRequest{})
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

	fp := llm.NewFallbackProvider(primary, secondary, logger)

	_, err := fp.Complete(context.Background(), llm.CompletionRequest{})
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

func TestFallbackProviderImplementsProviderInterface(t *testing.T) {
	t.Parallel()

	var _ llm.Provider = (*llm.FallbackProvider)(nil)
}
