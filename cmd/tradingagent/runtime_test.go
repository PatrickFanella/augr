package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/notification"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/google/uuid"
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

type stubDecisionRepo struct {
	decisions []domain.AgentDecision
}

func (s *stubDecisionRepo) Create(context.Context, *domain.AgentDecision) error { return nil }

func (s *stubDecisionRepo) GetByRun(context.Context, uuid.UUID, repository.AgentDecisionFilter, int, int) ([]domain.AgentDecision, error) {
	return s.decisions, nil
}

func TestSmokeStrategyRunnerDispatchNotifications_RoutesSignalAndDecisionsToDiscord(t *testing.T) {
	t.Parallel()

	var signalRequests atomic.Int32
	signalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		signalRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer signalServer.Close()

	var decisionRequests atomic.Int32
	decisionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		decisionRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer decisionServer.Close()

	runner := &smokeStrategyRunner{
		decisionRepo: &stubDecisionRepo{decisions: []domain.AgentDecision{
			{AgentRole: domain.AgentRoleTrader, Phase: domain.PhaseTrading, OutputText: `{"action":"buy"}`, CreatedAt: time.Date(2026, 4, 2, 15, 0, 0, 0, time.UTC)},
			{AgentRole: domain.AgentRoleRiskManager, Phase: domain.PhaseRiskDebate, OutputText: `{"action":"buy","confidence":0.92}`, CreatedAt: time.Date(2026, 4, 2, 15, 1, 0, 0, time.UTC)},
		}},
		notificationManager: newNotificationManager(config.Config{
			Notifications: config.NotificationConfig{
				Discord: config.DiscordNotificationConfig{
					SignalWebhookURL:   signalServer.URL,
					DecisionWebhookURL: decisionServer.URL,
				},
			},
		}),
	}

	runID := uuid.New()
	strategy := domain.Strategy{ID: uuid.New(), Name: "Momentum", Ticker: "AAPL"}
	state := &agent.PipelineState{
		TradingPlan: agent.TradingPlan{Ticker: "AAPL", Rationale: "Breakout confirmed."},
		FinalSignal: agent.FinalSignal{Signal: domain.PipelineSignalBuy, Confidence: 0.92},
	}
	completedAt := time.Date(2026, 4, 2, 15, 2, 0, 0, time.UTC)

	if err := runner.dispatchNotifications(context.Background(), strategy, &domain.PipelineRun{ID: runID, CompletedAt: &completedAt}, state); err != nil {
		t.Fatalf("dispatchNotifications() error = %v", err)
	}

	if signalRequests.Load() != 1 {
		t.Fatalf("signal requests = %d, want 1", signalRequests.Load())
	}
	if decisionRequests.Load() != 2 {
		t.Fatalf("decision requests = %d, want 2", decisionRequests.Load())
	}
}
