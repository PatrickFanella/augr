package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiscordNotifier_Notify_Success(t *testing.T) {
	t.Parallel()

	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected application/json, got %s", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	notifier := NewDiscordNotifier("", "", server.URL)
	notifier.httpClient = server.Client()

	alert := Alert{
		Key:        "test_alert",
		Title:      "Test Alert",
		Body:       "Something happened",
		Severity:   SeverityCritical,
		OccurredAt: time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC),
		Metadata:   map[string]string{"strategy": "momentum", "ticker": "AAPL"},
	}

	err := notifier.Notify(context.Background(), alert)
	if err != nil {
		t.Fatalf("Notify() error = %v", err)
	}

	embeds, ok := got["embeds"].([]any)
	if !ok || len(embeds) != 1 {
		t.Fatalf("expected 1 embed, got %v", got["embeds"])
	}
	embed := embeds[0].(map[string]any)

	if embed["title"] != "Test Alert" {
		t.Errorf("title = %v, want Test Alert", embed["title"])
	}
	if embed["description"] != "Something happened" {
		t.Errorf("description = %v, want Something happened", embed["description"])
	}
	// 0xE74C3C = 15158332
	if color, ok := embed["color"].(float64); !ok || int(color) != 0xE74C3C {
		t.Errorf("color = %v, want %d", embed["color"], 0xE74C3C)
	}
	if embed["timestamp"] != "2026-03-30T12:00:00Z" {
		t.Errorf("timestamp = %v, want 2026-03-30T12:00:00Z", embed["timestamp"])
	}

	fields, ok := embed["fields"].([]any)
	if !ok || len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %v", embed["fields"])
	}
	// Sorted: strategy, ticker
	f0 := fields[0].(map[string]any)
	if f0["name"] != "strategy" || f0["value"] != "momentum" {
		t.Errorf("field[0] = %v, want strategy=momentum", f0)
	}
	f1 := fields[1].(map[string]any)
	if f1["name"] != "ticker" || f1["value"] != "AAPL" {
		t.Errorf("field[1] = %v, want ticker=AAPL", f1)
	}
}

func TestDiscordNotifier_Notify_EmptyURL(t *testing.T) {
	t.Parallel()

	notifier := NewDiscordNotifier("", "", "")
	err := notifier.Notify(context.Background(), Alert{
		Title:    "Should not send",
		Severity: SeverityInfo,
	})
	if err != nil {
		t.Fatalf("Notify() with empty URL should return nil, got %v", err)
	}
}

func TestDiscordNotifier_Send_RateLimitRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "0.1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	notifier := NewDiscordNotifier("", "", "")
	notifier.httpClient = server.Client()

	embed := map[string]any{
		"title":       "Rate limited",
		"description": "Retry test",
		"color":       0x3498DB,
	}

	err := notifier.Send(context.Background(), server.URL, embed)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 calls (initial + retry), got %d", got)
	}
}

func TestDiscordNotifier_Send_EmptyURL(t *testing.T) {
	t.Parallel()

	notifier := NewDiscordNotifier("", "", "")
	err := notifier.Send(context.Background(), "", map[string]any{"title": "skip"})
	if err != nil {
		t.Fatalf("Send() with empty URL should return nil, got %v", err)
	}
}

func TestDiscordNotifier_Send_NonSuccessStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	notifier := NewDiscordNotifier("", "", "")
	notifier.httpClient = server.Client()

	err := notifier.Send(context.Background(), server.URL, map[string]any{"title": "fail"})
	if err == nil {
		t.Fatal("Send() expected error for 500, got nil")
	}
}

func TestSeverityColor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		sev  Severity
		want int
	}{
		{SeverityInfo, 0x3498DB},
		{SeverityWarning, 0xF39C12},
		{SeverityCritical, 0xE74C3C},
		{Severity("unknown"), 0x3498DB},
	}
	for _, tc := range cases {
		if got := severityColor(tc.sev); got != tc.want {
			t.Errorf("severityColor(%q) = 0x%X, want 0x%X", tc.sev, got, tc.want)
		}
	}
}
