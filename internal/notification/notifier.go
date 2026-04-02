package notification

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	ChannelTelegram  = "telegram"
	ChannelEmail     = "email"
	ChannelWebhook   = "webhook"
	ChannelPagerDuty = "pagerduty"
	ChannelDiscord   = "discord"
)

// Severity describes the urgency of an alert.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Alert is the message delivered through one or more notification channels.
type Alert struct {
	Key        string
	Title      string
	Body       string
	Severity   Severity
	OccurredAt time.Time
	Metadata   map[string]string
}

// Notifier delivers alerts to an external channel.
type Notifier interface {
	Notify(context.Context, Alert) error
}

func normalizeChannels(channels []string) []string {
	normalized := make([]string, 0, len(channels))
	for _, channel := range channels {
		channel = strings.ToLower(strings.TrimSpace(channel))
		if channel == "" || slices.Contains(normalized, channel) {
			continue
		}
		normalized = append(normalized, channel)
	}
	return normalized
}

func formatAlertText(alert Alert) string {
	timestamp := alert.OccurredAt.UTC()
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s\n", strings.ToUpper(string(alert.Severity)), alert.Title)
	fmt.Fprintf(&b, "Time: %s\n", timestamp.Format(time.RFC3339))
	if alert.Body != "" {
		b.WriteString(alert.Body)
		b.WriteByte('\n')
	}

	if len(alert.Metadata) > 0 {
		keys := make([]string, 0, len(alert.Metadata))
		for key := range alert.Metadata {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			fmt.Fprintf(&b, "%s: %s\n", key, alert.Metadata[key])
		}
	}

	return strings.TrimRight(b.String(), "\n")
}
