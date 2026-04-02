package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/google/uuid"
)

// Compile-time interface check.
var _ Notifier = (*DiscordNotifier)(nil)

// DiscordNotifier delivers alerts as Discord embeds via webhooks.
type DiscordNotifier struct {
	signalWebhookURL   string
	decisionWebhookURL string
	alertWebhookURL    string
	httpClient         *http.Client
}

// NewDiscordNotifier returns a notifier that posts to Discord webhooks.
func NewDiscordNotifier(signalURL, decisionURL, alertURL string) *DiscordNotifier {
	return &DiscordNotifier{
		signalWebhookURL:   signalURL,
		decisionWebhookURL: decisionURL,
		alertWebhookURL:    alertURL,
		httpClient:         &http.Client{Timeout: 10 * time.Second},
	}
}

// severityColor maps alert severity to a Discord embed color integer.
func severityColor(s Severity) int {
	switch s {
	case SeverityWarning:
		return 0xF39C12
	case SeverityCritical:
		return 0xE74C3C
	default: // info or unknown
		return 0x3498DB
	}
}

// Notify sends an alert embed to the alert webhook. It implements the Notifier
// interface. Errors are logged but not returned so notifications never block
// the caller (fire-and-forget).
func (n *DiscordNotifier) Notify(ctx context.Context, alert Alert) error {
	if strings.TrimSpace(n.alertWebhookURL) == "" {
		return nil
	}

	fields := metadataFields(alert.Metadata)

	embed := map[string]any{
		"title":       alert.Title,
		"description": alert.Body,
		"color":       severityColor(alert.Severity),
		"timestamp":   alert.OccurredAt.UTC().Format(time.RFC3339),
	}
	if len(fields) > 0 {
		embed["fields"] = fields
	}

	if err := n.Send(ctx, n.alertWebhookURL, embed); err != nil {
		slog.Error("discord notification failed", "err", err, "alert_key", alert.Key)
	}
	return nil
}

// metadataFields converts alert metadata into Discord embed field objects.
// Keys are sorted for deterministic output.
func metadataFields(md map[string]string) []map[string]any {
	if len(md) == 0 {
		return nil
	}
	keys := make([]string, 0, len(md))
	for k := range md {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fields := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		fields = append(fields, map[string]any{
			"name":   k,
			"value":  md[k],
			"inline": true,
		})
	}
	return fields
}

// Send posts a single embed to the given Discord webhook URL. It handles 429
// rate-limit responses by respecting the Retry-After header and retrying up to
// maxAttempts times before returning an error.
func (n *DiscordNotifier) Send(ctx context.Context, webhookURL string, embed map[string]any) error {
	if strings.TrimSpace(webhookURL) == "" {
		return nil
	}

	payload, err := json.Marshal(map[string]any{
		"embeds": []map[string]any{embed},
	})
	if err != nil {
		return fmt.Errorf("discord: marshal payload: %w", err)
	}

	const maxAttempts = 5
	for attempt := range maxAttempts {
		resp, err := n.doPost(ctx, webhookURL, payload)
		if err != nil {
			return err
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			_ = resp.Body.Close()

			if attempt == maxAttempts-1 {
				return fmt.Errorf("discord: still rate limited after %d attempts", maxAttempts)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryAfter):
				continue
			}
		}

		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode >= http.StatusBadRequest {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			return fmt.Errorf("discord: webhook returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		return nil
	}

	return fmt.Errorf("discord: still rate limited after %d attempts", maxAttempts)
}

// doPost sends a JSON POST to url with the given body bytes.
func (n *DiscordNotifier) doPost(ctx context.Context, url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("discord: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discord: send request: %w", err)
	}
	return resp, nil
}

// parseRetryAfter interprets the Retry-After header value as fractional seconds.
// Returns a minimum of 1 second on parse failure.
func parseRetryAfter(val string) time.Duration {
	val = strings.TrimSpace(val)
	if val == "" {
		return time.Second
	}
	secs, err := strconv.ParseFloat(val, 64)
	if err != nil || secs <= 0 {
		return time.Second
	}
	return time.Duration(secs * float64(time.Second))
}

// truncate shortens s to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// signalColor maps a pipeline signal to a Discord embed color.
func signalColor(signal domain.PipelineSignal) int {
	switch signal {
	case domain.PipelineSignalBuy:
		return 0x2ECC71
	case domain.PipelineSignalSell:
		return 0xE74C3C
	default:
		return 0x95A5A6
	}
}

// alertSeverityColor maps a severity string to a Discord embed color.
func alertSeverityColor(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 0xE74C3C
	case "warning":
		return 0xE67E22
	default:
		return 0x3498DB
	}
}

// FormatSignalEmbed builds a Discord embed for a pipeline trading signal.
func FormatSignalEmbed(strategyName, ticker string, signal domain.PipelineSignal, confidence float64, reasoning string, runID uuid.UUID, occurredAt time.Time) map[string]any {
	return map[string]any{
		"title": fmt.Sprintf("Signal: %s", strings.ToUpper(string(signal))),
		"color": signalColor(signal),
		"fields": []map[string]any{
			{"name": "Strategy", "value": strategyName, "inline": true},
			{"name": "Ticker", "value": ticker, "inline": true},
			{"name": "Confidence", "value": fmt.Sprintf("%.1f%%", confidence*100), "inline": true},
			{"name": "Reasoning", "value": truncate(reasoning, 1024), "inline": false},
		},
		"footer":    map[string]any{"text": fmt.Sprintf("Run %s", runID.String()[:8])},
		"timestamp": occurredAt.UTC().Format(time.RFC3339),
	}
}

// FormatDecisionEmbed builds a Discord embed for an agent decision.
func FormatDecisionEmbed(agentRole, phase, outputSummary, llmModel string, latencyMS int, runID uuid.UUID, occurredAt time.Time) map[string]any {
	return map[string]any{
		"title": fmt.Sprintf("Decision: %s", agentRole),
		"color": 0x3498DB,
		"fields": []map[string]any{
			{"name": "Phase", "value": phase, "inline": true},
			{"name": "Model", "value": llmModel, "inline": true},
			{"name": "Latency", "value": fmt.Sprintf("%dms", latencyMS), "inline": true},
			{"name": "Output", "value": truncate(outputSummary, 1024), "inline": false},
		},
		"footer":    map[string]any{"text": fmt.Sprintf("Run %s", runID.String()[:8])},
		"timestamp": occurredAt.UTC().Format(time.RFC3339),
	}
}

// FormatAlertEmbed builds a Discord embed for a system alert.
func FormatAlertEmbed(eventType, severity, reason string, details map[string]any, occurredAt time.Time) map[string]any {
	fields := []map[string]any{
		{"name": "Severity", "value": strings.ToUpper(severity), "inline": true},
	}

	if len(details) > 0 {
		keys := make([]string, 0, len(details))
		for k := range details {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fields = append(fields, map[string]any{
				"name":   k,
				"value":  fmt.Sprintf("%v", details[k]),
				"inline": true,
			})
		}
	}

	return map[string]any{
		"title":       fmt.Sprintf("Alert: %s", eventType),
		"description": reason,
		"color":       alertSeverityColor(severity),
		"fields":      fields,
		"timestamp":   occurredAt.UTC().Format(time.RFC3339),
	}
}
