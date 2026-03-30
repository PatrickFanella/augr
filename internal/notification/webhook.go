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
	payload, err := json.Marshal(map[string]any{
		"key":         alert.Key,
		"title":       alert.Title,
		"body":        alert.Body,
		"severity":    alert.Severity,
		"occurred_at": alert.OccurredAt.UTC().Format(time.RFC3339),
		"metadata":    alert.Metadata,
		"text":        formatAlertText(alert),
	})
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(payload))
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
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("webhook returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	return nil
}
