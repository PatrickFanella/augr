package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var _ Notifier = (*WebhookNotifier)(nil)

var _ SignalNotifier = (*WebhookNotifier)(nil)

var _ DecisionNotifier = (*WebhookNotifier)(nil)

// WebhookNotifier delivers alerts as JSON POST requests.
type WebhookNotifier struct {
	url        string
	secret     string
	headers    map[string]string
	httpClient *http.Client
}

// NewWebhookNotifier returns a generic webhook notifier.
func NewWebhookNotifier(rawURL, secret string) *WebhookNotifier {
	return &WebhookNotifier{
		url:        rawURL,
		secret:     secret,
		headers:    map[string]string{},
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Notify sends an alert payload to the configured webhook endpoint.
func (n *WebhookNotifier) Notify(ctx context.Context, alert Alert) error {
	payload := FormatPayload("alert", string(alert.Severity), "", "", map[string]any{
		"key":         alert.Key,
		"title":       alert.Title,
		"body":        alert.Body,
		"occurred_at": alert.OccurredAt.UTC().Format(time.RFC3339),
		"metadata":    alert.Metadata,
		"text":        formatAlertText(alert),
	}, "")

	return n.SendPayload(ctx, payload)
}

// NotifySignal sends a structured trading signal payload to the configured webhook.
func (n *WebhookNotifier) NotifySignal(ctx context.Context, event SignalEvent) error {
	payload := FormatPayload("signal", string(SeverityInfo), uuidString(event.StrategyID), uuidString(event.RunID), map[string]any{
		"strategy_name": event.StrategyName,
		"ticker":        event.Ticker,
		"signal":        event.Signal,
		"confidence":    event.Confidence,
		"reasoning":     event.Reasoning,
		"occurred_at":   event.OccurredAt.UTC().Format(time.RFC3339),
	}, "")

	return n.SendPayload(ctx, payload)
}

// NotifyDecision sends a structured agent decision payload to the configured webhook.
func (n *WebhookNotifier) NotifyDecision(ctx context.Context, event DecisionEvent) error {
	payload := FormatPayload("decision", string(SeverityInfo), uuidString(event.StrategyID), uuidString(event.RunID), map[string]any{
		"agent_role":   event.AgentRole,
		"phase":        event.Phase,
		"summary":      event.OutputSummary,
		"llm_provider": event.LLMProvider,
		"llm_model":    event.LLMModel,
		"latency_ms":   event.LatencyMS,
		"occurred_at":  event.OccurredAt.UTC().Format(time.RFC3339),
	}, "")

	return n.SendPayload(ctx, payload)
}

// SendPayload POSTs any pre-built WebhookPayload to the configured endpoint.
// This supports all event types (signal, decision, alert, etc.).
func (n *WebhookNotifier) SendPayload(ctx context.Context, payload WebhookPayload) error {
	if strings.TrimSpace(n.url) == "" {
		return nil
	}

	return n.send(ctx, payload)
}

// send marshals a payload and POSTs it to the webhook URL.
func (n *WebhookNotifier) send(ctx context.Context, payload WebhookPayload) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(n.secret) != "" {
		req.Header.Set("X-Webhook-Secret", n.secret)
	}
	for key, value := range n.headers {
		req.Header.Set(key, value)
	}

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("webhook returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	return nil
}

// WebhookPayload is the structured JSON body sent to webhook endpoints.
// All event types (signal, decision, alert) share this envelope.
//
// Schema:
//
//	event_type      - string: "signal", "decision", "alert", etc.
//	severity        - string: "info", "warning", "critical"
//	timestamp       - string: ISO 8601 (RFC3339)
//	strategy_id     - string: UUID of the strategy (may be empty)
//	pipeline_run_id - string: UUID of the pipeline run (may be empty)
//	data            - object: event-specific payload
//	callback_url    - string: optional callback URL for interactive webhooks
type WebhookPayload struct {
	EventType     string         `json:"event_type"`
	Severity      string         `json:"severity"`
	Timestamp     string         `json:"timestamp"`
	StrategyID    string         `json:"strategy_id,omitempty"`
	PipelineRunID string         `json:"pipeline_run_id,omitempty"`
	Data          map[string]any `json:"data,omitempty"`
	CallbackURL   string         `json:"callback_url,omitempty"`
}

// FormatPayload builds a WebhookPayload with the standard envelope fields.
func FormatPayload(eventType, severity, strategyID, runID string, data map[string]any, callbackURL string) WebhookPayload {
	return WebhookPayload{
		EventType:     eventType,
		Severity:      severity,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		StrategyID:    strategyID,
		PipelineRunID: runID,
		Data:          data,
		CallbackURL:   callbackURL,
	}
}
