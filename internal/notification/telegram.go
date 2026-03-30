package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TelegramNotifier delivers alerts through the Telegram Bot API.
type TelegramNotifier struct {
	botToken   string
	chatID     string
	apiBaseURL string
	httpClient *http.Client
}

// NewTelegramNotifier returns a Telegram notifier with sane defaults.
func NewTelegramNotifier(botToken, chatID string) *TelegramNotifier {
	return &TelegramNotifier{
		botToken:   botToken,
		chatID:     chatID,
		apiBaseURL: "https://api.telegram.org",
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Notify sends an alert message to the configured Telegram chat.
func (n *TelegramNotifier) Notify(ctx context.Context, alert Alert) error {
	payload, err := json.Marshal(map[string]any{
		"chat_id":                  n.chatID,
		"text":                     formatAlertText(alert),
		"disable_web_page_preview": true,
	})
	if err != nil {
		return fmt.Errorf("marshal telegram payload: %w", err)
	}

	endpoint := strings.TrimRight(n.apiBaseURL, "/") + "/bot" + url.PathEscape(n.botToken) + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send telegram request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("telegram API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	return nil
}
