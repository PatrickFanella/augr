package notification

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFormatPayload_Signal(t *testing.T) {
	data := map[string]any{
		"signal":   "buy",
		"ticker":   "AAPL",
		"strength": 0.85,
	}
	p := FormatPayload("signal", "info", "strat-123", "run-456", data, "")

	if p.EventType != "signal" {
		t.Errorf("EventType = %q, want %q", p.EventType, "signal")
	}
	if p.Severity != "info" {
		t.Errorf("Severity = %q, want %q", p.Severity, "info")
	}
	if p.StrategyID != "strat-123" {
		t.Errorf("StrategyID = %q, want %q", p.StrategyID, "strat-123")
	}
	if p.PipelineRunID != "run-456" {
		t.Errorf("PipelineRunID = %q, want %q", p.PipelineRunID, "run-456")
	}
	if p.CallbackURL != "" {
		t.Errorf("CallbackURL = %q, want empty", p.CallbackURL)
	}

	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// Verify JSON contains expected keys.
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if raw["event_type"] != "signal" {
		t.Errorf("JSON event_type = %v, want %q", raw["event_type"], "signal")
	}
	if _, ok := raw["callback_url"]; ok {
		t.Error("callback_url should be omitted when empty")
	}
	dataMap, ok := raw["data"].(map[string]any)
	if !ok {
		t.Fatal("data field missing or wrong type")
	}
	if dataMap["signal"] != "buy" {
		t.Errorf("data.signal = %v, want %q", dataMap["signal"], "buy")
	}
}

func TestFormatPayload_Decision(t *testing.T) {
	data := map[string]any{
		"action":   "execute",
		"cost_usd": 0.003,
		"model":    "gpt-4o",
	}
	p := FormatPayload("decision", "warning", "strat-abc", "run-def", data, "https://example.com/callback")

	if p.EventType != "decision" {
		t.Errorf("EventType = %q, want %q", p.EventType, "decision")
	}
	if p.Severity != "warning" {
		t.Errorf("Severity = %q, want %q", p.Severity, "warning")
	}
	if p.CallbackURL != "https://example.com/callback" {
		t.Errorf("CallbackURL = %q, want %q", p.CallbackURL, "https://example.com/callback")
	}

	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if raw["callback_url"] != "https://example.com/callback" {
		t.Errorf("JSON callback_url = %v, want callback URL", raw["callback_url"])
	}
}

func TestFormatPayload_Alert(t *testing.T) {
	// Empty optional fields should be omitted from JSON.
	p := FormatPayload("alert", "critical", "", "", nil, "")

	if p.EventType != "alert" {
		t.Errorf("EventType = %q, want %q", p.EventType, "alert")
	}
	if p.Severity != "critical" {
		t.Errorf("Severity = %q, want %q", p.Severity, "critical")
	}

	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// omitempty fields should not appear.
	for _, key := range []string{"strategy_id", "pipeline_run_id", "data", "callback_url"} {
		if _, ok := raw[key]; ok {
			t.Errorf("%s should be omitted when zero/nil, but was present", key)
		}
	}

	// Required fields should always appear.
	for _, key := range []string{"event_type", "severity", "timestamp"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("%s should always be present", key)
		}
	}
}

func TestFormatPayload_Timestamp(t *testing.T) {
	before := time.Now().UTC().Add(-2 * time.Second)
	p := FormatPayload("signal", "info", "", "", nil, "")
	after := time.Now().UTC().Add(2 * time.Second)

	ts, err := time.Parse(time.RFC3339, p.Timestamp)
	if err != nil {
		t.Fatalf("Timestamp %q is not valid RFC3339: %v", p.Timestamp, err)
	}
	if ts.Before(before) || ts.After(after) {
		t.Errorf("Timestamp %v not within expected window [%v, %v]", ts, before, after)
	}
}

func TestWebhookPayload_JSONRoundTrip(t *testing.T) {
	original := FormatPayload("signal", "info", "strat-1", "run-2", map[string]any{
		"ticker": "MSFT",
		"price":  425.50,
	}, "https://example.com/hook")

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded WebhookPayload
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.EventType != original.EventType {
		t.Errorf("EventType = %q, want %q", decoded.EventType, original.EventType)
	}
	if decoded.Severity != original.Severity {
		t.Errorf("Severity = %q, want %q", decoded.Severity, original.Severity)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp = %q, want %q", decoded.Timestamp, original.Timestamp)
	}
	if decoded.StrategyID != original.StrategyID {
		t.Errorf("StrategyID = %q, want %q", decoded.StrategyID, original.StrategyID)
	}
	if decoded.PipelineRunID != original.PipelineRunID {
		t.Errorf("PipelineRunID = %q, want %q", decoded.PipelineRunID, original.PipelineRunID)
	}
	if decoded.CallbackURL != original.CallbackURL {
		t.Errorf("CallbackURL = %q, want %q", decoded.CallbackURL, original.CallbackURL)
	}
	if decoded.Data["ticker"] != original.Data["ticker"] {
		t.Errorf("Data[ticker] = %v, want %v", decoded.Data["ticker"], original.Data["ticker"])
	}
}
