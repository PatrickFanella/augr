package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/notification"
)

func TestNewNotificationManager_DiscordAlertDispatch(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := config.Config{
		Notifications: config.NotificationConfig{
			Discord: config.DiscordNotificationConfig{
				AlertWebhookURL: server.URL,
			},
			Alerts: config.AlertRulesConfig{
				KillSwitch: config.ImmediateAlertRuleConfig{Channels: []string{notification.ChannelDiscord}},
			},
		},
	}

	manager := newNotificationManager(cfg)
	if manager == nil {
		t.Fatal("newNotificationManager() = nil")
	}

	if err := manager.RecordKillSwitchToggle(context.Background(), true, "manual test", time.Now()); err != nil {
		t.Fatalf("RecordKillSwitchToggle() error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("discord requests = %d, want 1", requests.Load())
	}
}

func TestNewNotificationManager_SkipsDiscordWhenUnconfigured(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Notifications: config.NotificationConfig{
			Alerts: config.AlertRulesConfig{
				KillSwitch: config.ImmediateAlertRuleConfig{Channels: []string{notification.ChannelDiscord}},
			},
		},
	}

	manager := newNotificationManager(cfg)
	if manager == nil {
		t.Fatal("newNotificationManager() = nil")
	}

	if err := manager.RecordKillSwitchToggle(context.Background(), true, "manual test", time.Now()); err == nil {
		t.Fatal("RecordKillSwitchToggle() error = nil, want missing discord notifier error")
	}
}
