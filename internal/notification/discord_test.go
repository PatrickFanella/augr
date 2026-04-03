package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/google/uuid"
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

func TestFormatSignalEmbed(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("aabbccdd-1122-3344-5566-778899aabbcc")
	ts := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)

	embed := FormatSignalEmbed("Momentum", "AAPL", domain.PipelineSignalBuy, 0.85, "strong trend", runID, ts)
	if embed["title"] != "Signal: BUY" {
		t.Errorf("title = %v, want Signal: BUY", embed["title"])
	}
	if color, ok := embed["color"].(int); !ok || color != 0x2ECC71 {
		t.Errorf("buy color = %v, want 0x2ECC71", embed["color"])
	}

	embed = FormatSignalEmbed("Momentum", "TSLA", domain.PipelineSignalSell, 0.6, "downtrend", runID, ts)
	if color, ok := embed["color"].(int); !ok || color != 0xE74C3C {
		t.Errorf("sell color = %v, want 0xE74C3C", embed["color"])
	}
	if embed["title"] != "Signal: SELL" {
		t.Errorf("title = %v, want Signal: SELL", embed["title"])
	}

	embed = FormatSignalEmbed("Momentum", "GOOG", domain.PipelineSignalHold, 0.5, "", runID, ts)
	if color, ok := embed["color"].(int); !ok || color != 0x95A5A6 {
		t.Errorf("hold color = %v, want 0x95A5A6", embed["color"])
	}
	if embed["title"] != "Signal: HOLD" {
		t.Errorf("title = %v, want Signal: HOLD", embed["title"])
	}

	fields := embed["fields"].([]map[string]any)
	if len(fields) != 4 {
		t.Fatalf("field count = %d, want 4", len(fields))
	}
	if fields[0]["value"] != "Momentum" {
		t.Errorf("Strategy field = %v, want Momentum", fields[0]["value"])
	}
	if fields[1]["value"] != "GOOG" {
		t.Errorf("Ticker field = %v, want GOOG", fields[1]["value"])
	}
	if fields[2]["value"] != "50.0%" {
		t.Errorf("Confidence field = %v, want 50.0%%", fields[2]["value"])
	}
	if fields[3]["value"] != "" {
		t.Errorf("Reasoning field = %v, want empty string", fields[3]["value"])
	}

	longReasoning := strings.Repeat("A", 2000)
	embed = FormatSignalEmbed("Momentum", "MSFT", domain.PipelineSignalBuy, 0.9, longReasoning, runID, ts)
	reasoningField := embed["fields"].([]map[string]any)[3]["value"].(string)
	if len(reasoningField) != 1024 {
		t.Errorf("truncated reasoning len = %d, want 1024", len(reasoningField))
	}
	if !strings.HasSuffix(reasoningField, "...") {
		t.Error("truncated reasoning should end with ...")
	}
}

func TestFormatDecisionEmbed(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("11223344-5566-7788-99aa-bbccddeeff00")
	ts := time.Date(2026, 4, 1, 11, 30, 0, 0, time.UTC)

	embed := FormatDecisionEmbed("Analyst", "research", "AAPL looks bullish", "gpt-4o", 1234, runID, ts)

	if embed["title"] != "Decision: Analyst" {
		t.Errorf("title = %v, want Decision: Analyst", embed["title"])
	}
	if color, ok := embed["color"].(int); !ok || color != 0x3498DB {
		t.Errorf("color = %v, want 0x3498DB", embed["color"])
	}

	fields := embed["fields"].([]map[string]any)
	if len(fields) != 4 {
		t.Fatalf("field count = %d, want 4", len(fields))
	}
	if fields[0]["value"] != "research" {
		t.Errorf("Phase = %v, want research", fields[0]["value"])
	}
	if fields[1]["value"] != "gpt-4o" {
		t.Errorf("Model = %v, want gpt-4o", fields[1]["value"])
	}
	if fields[2]["value"] != "1234ms" {
		t.Errorf("Latency = %v, want 1234ms", fields[2]["value"])
	}
	if fields[3]["value"] != "AAPL looks bullish" {
		t.Errorf("Output = %v, want AAPL looks bullish", fields[3]["value"])
	}

	footer := embed["footer"].(map[string]any)
	if footer["text"] != "Run 11223344" {
		t.Errorf("footer = %v, want Run 11223344", footer["text"])
	}
	if embed["timestamp"] != "2026-04-01T11:30:00Z" {
		t.Errorf("timestamp = %v, want 2026-04-01T11:30:00Z", embed["timestamp"])
	}
}

func TestFormatAlertEmbed(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	embed := FormatAlertEmbed("pipeline_failure", "critical", "Pipeline crashed", nil, ts)
	if color, ok := embed["color"].(int); !ok || color != 0xE74C3C {
		t.Errorf("critical color = %v, want 0xE74C3C", embed["color"])
	}
	if embed["title"] != "Alert: pipeline_failure" {
		t.Errorf("title = %v, want Alert: pipeline_failure", embed["title"])
	}
	if embed["description"] != "Pipeline crashed" {
		t.Errorf("description = %v, want Pipeline crashed", embed["description"])
	}

	fields := embed["fields"].([]map[string]any)
	if len(fields) != 1 {
		t.Fatalf("field count = %d, want 1 (severity only)", len(fields))
	}
	if fields[0]["name"] != "Severity" || fields[0]["value"] != "CRITICAL" {
		t.Errorf("severity field = %v, want Severity=CRITICAL", fields[0])
	}

	details := map[string]any{"ticker": "AAPL", "error_count": 3}
	embed = FormatAlertEmbed("data_stale", "warning", "Data is stale", details, ts)
	if color, ok := embed["color"].(int); !ok || color != 0xE67E22 {
		t.Errorf("warning color = %v, want 0xE67E22", embed["color"])
	}
	fields = embed["fields"].([]map[string]any)
	if len(fields) != 3 {
		t.Fatalf("field count = %d, want 3", len(fields))
	}
	if fields[0]["name"] != "Severity" || fields[0]["value"] != "WARNING" {
		t.Errorf("first field = %v, want Severity=WARNING", fields[0])
	}
	if fields[1]["name"] != "error_count" || fields[1]["value"] != "3" {
		t.Errorf("field[1] = %v, want error_count=3", fields[1])
	}
	if fields[2]["name"] != "ticker" || fields[2]["value"] != "AAPL" {
		t.Errorf("field[2] = %v, want ticker=AAPL", fields[2])
	}

	embed = FormatAlertEmbed("health_check", "info", "All good", nil, ts)
	if color, ok := embed["color"].(int); !ok || color != 0x3498DB {
		t.Errorf("info color = %v, want 0x3498DB", embed["color"])
	}
	if embed["timestamp"] != "2026-04-01T12:00:00Z" {
		t.Errorf("timestamp = %v, want 2026-04-01T12:00:00Z", embed["timestamp"])
	}
}
