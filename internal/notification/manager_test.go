package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/google/uuid"
)

type recordingNotifier struct {
	alerts []Alert
}

func (n *recordingNotifier) Notify(_ context.Context, alert Alert) error {
	n.alerts = append(n.alerts, alert)
	return nil
}

type recordingStructuredNotifier struct {
	recordingNotifier
	signals   []SignalEvent
	decisions []DecisionEvent
}

func (n *recordingStructuredNotifier) NotifySignal(_ context.Context, event SignalEvent) error {
	n.signals = append(n.signals, event)
	return nil
}

func (n *recordingStructuredNotifier) NotifyDecision(_ context.Context, event DecisionEvent) error {
	n.decisions = append(n.decisions, event)
	return nil
}

func TestManagerPipelineFailureThresholdAndDedup(t *testing.T) {
	t.Parallel()

	telegram := &recordingNotifier{}
	email := &recordingNotifier{}
	manager := NewManager(testAlertRules(), map[string]Notifier{
		ChannelTelegram: telegram,
		ChannelEmail:    email,
	})

	now := time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if err := manager.RecordPipelineResult(context.Background(), false, now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("RecordPipelineResult() error = %v", err)
		}
	}
	if len(telegram.alerts) != 0 || len(email.alerts) != 0 {
		t.Fatal("alerts fired before consecutive failure threshold was reached")
	}

	if err := manager.RecordPipelineResult(context.Background(), false, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("RecordPipelineResult() third failure error = %v", err)
	}
	if len(telegram.alerts) != 1 || len(email.alerts) != 1 {
		t.Fatalf("alert counts = telegram:%d email:%d, want 1 each", len(telegram.alerts), len(email.alerts))
	}

	if err := manager.RecordPipelineResult(context.Background(), false, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("RecordPipelineResult() fourth failure error = %v", err)
	}
	if len(telegram.alerts) != 1 || len(email.alerts) != 1 {
		t.Fatal("repeated failures should be deduplicated until a success resets state")
	}

	if err := manager.RecordPipelineResult(context.Background(), true, now.Add(4*time.Minute)); err != nil {
		t.Fatalf("RecordPipelineResult() success error = %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := manager.RecordPipelineResult(context.Background(), false, now.Add(time.Duration(5+i)*time.Minute)); err != nil {
			t.Fatalf("RecordPipelineResult() reset cycle error = %v", err)
		}
	}
	if len(telegram.alerts) != 2 || len(email.alerts) != 2 {
		t.Fatalf("alert counts after reset = telegram:%d email:%d, want 2 each", len(telegram.alerts), len(email.alerts))
	}
}

func TestManagerLLMProviderDownUsesRollingErrorRate(t *testing.T) {
	t.Parallel()

	telegram := &recordingNotifier{}
	manager := NewManager(testAlertRules(), map[string]Notifier{
		ChannelTelegram: telegram,
	})

	now := time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC)
	samples := []bool{false, false, true, false}
	for i, success := range samples {
		if err := manager.RecordLLMRequest(context.Background(), "openai", success, now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("RecordLLMRequest() error = %v", err)
		}
	}

	if len(telegram.alerts) != 1 {
		t.Fatalf("len(telegram.alerts) = %d, want 1", len(telegram.alerts))
	}

	recoveryTimes := []time.Duration{4, 5, 6, 7, 8, 9}
	for _, minute := range recoveryTimes {
		if err := manager.RecordLLMRequest(context.Background(), "openai", true, now.Add(minute*time.Minute)); err != nil {
			t.Fatalf("RecordLLMRequest() recovery error = %v", err)
		}
	}

	for i := 0; i < 4; i++ {
		if err := manager.RecordLLMRequest(context.Background(), "openai", false, now.Add(time.Duration(10+i)*time.Minute)); err != nil {
			t.Fatalf("RecordLLMRequest() second outage error = %v", err)
		}
	}

	if len(telegram.alerts) != 2 {
		t.Fatalf("len(telegram.alerts) after recovery = %d, want 2", len(telegram.alerts))
	}
}

func TestManagerRoutesHighLatencyAndDatabaseLossAlerts(t *testing.T) {
	t.Parallel()

	email := &recordingNotifier{}
	pagerDuty := &recordingNotifier{}
	manager := NewManager(testAlertRules(), map[string]Notifier{
		ChannelEmail:     email,
		ChannelPagerDuty: pagerDuty,
	})

	now := time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC)
	if err := manager.RecordPipelineLatency(context.Background(), 121*time.Second, now); err != nil {
		t.Fatalf("RecordPipelineLatency() error = %v", err)
	}
	if len(email.alerts) != 1 {
		t.Fatalf("len(email.alerts) after high latency = %d, want 1", len(email.alerts))
	}
	if len(pagerDuty.alerts) != 0 {
		t.Fatalf("len(pagerDuty.alerts) after high latency = %d, want 0", len(pagerDuty.alerts))
	}

	dbErr := errors.New("dial tcp: connection refused")
	if err := manager.RecordDBConnectionState(context.Background(), false, dbErr, now.Add(time.Minute)); err != nil {
		t.Fatalf("RecordDBConnectionState() outage error = %v", err)
	}
	if len(email.alerts) != 2 || len(pagerDuty.alerts) != 1 {
		t.Fatalf("alert counts after db outage = email:%d pagerduty:%d, want 2 and 1", len(email.alerts), len(pagerDuty.alerts))
	}

	if err := manager.RecordDBConnectionState(context.Background(), false, dbErr, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("RecordDBConnectionState() repeated outage error = %v", err)
	}
	if len(email.alerts) != 2 || len(pagerDuty.alerts) != 1 {
		t.Fatal("database outage alerts should deduplicate until connectivity recovers")
	}

	if err := manager.RecordDBConnectionState(context.Background(), true, nil, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("RecordDBConnectionState() recovery error = %v", err)
	}
	if err := manager.RecordDBConnectionState(context.Background(), false, dbErr, now.Add(4*time.Minute)); err != nil {
		t.Fatalf("RecordDBConnectionState() second outage error = %v", err)
	}
	if len(email.alerts) != 3 || len(pagerDuty.alerts) != 2 {
		t.Fatalf("alert counts after second outage = email:%d pagerduty:%d, want 3 and 2", len(email.alerts), len(pagerDuty.alerts))
	}
}

func TestManagerRoutesImmediateTelegramAlerts(t *testing.T) {
	t.Parallel()

	telegram := &recordingNotifier{}
	manager := NewManager(testAlertRules(), map[string]Notifier{
		ChannelTelegram: telegram,
	})
	now := time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC)

	if err := manager.RecordCircuitBreakerTrip(context.Background(), "daily loss exceeded threshold", now); err != nil {
		t.Fatalf("RecordCircuitBreakerTrip() error = %v", err)
	}
	if err := manager.RecordKillSwitchToggle(context.Background(), true, "manual activation", now.Add(time.Minute)); err != nil {
		t.Fatalf("RecordKillSwitchToggle() error = %v", err)
	}

	if len(telegram.alerts) != 2 {
		t.Fatalf("len(telegram.alerts) = %d, want 2", len(telegram.alerts))
	}
}

func TestManagerRoutesSignalsAndDecisionsToWebhookAndDiscord(t *testing.T) {
	t.Parallel()

	webhook := &recordingStructuredNotifier{}
	discord := &recordingStructuredNotifier{}
	pagerDuty := &recordingStructuredNotifier{}
	manager := NewManager(testAlertRules(), map[string]Notifier{
		ChannelWebhook:   webhook,
		ChannelDiscord:   discord,
		ChannelPagerDuty: pagerDuty,
	})

	runID := uuid.New()
	strategyID := uuid.New()
	occurredAt := time.Date(2026, 4, 2, 14, 15, 0, 0, time.UTC)

	if err := manager.RecordSignal(context.Background(), SignalEvent{
		StrategyID:   strategyID,
		StrategyName: "Momentum",
		RunID:        runID,
		Ticker:       "AAPL",
		Signal:       domain.PipelineSignalBuy,
		Confidence:   0.91,
		Reasoning:    "Breakout confirmed.",
		OccurredAt:   occurredAt,
	}); err != nil {
		t.Fatalf("RecordSignal() error = %v", err)
	}

	if err := manager.RecordDecision(context.Background(), DecisionEvent{
		StrategyID:    strategyID,
		RunID:         runID,
		AgentRole:     domain.AgentRoleTrader,
		Phase:         domain.PhaseTrading,
		OutputSummary: `{"action":"buy"}`,
		LLMProvider:   "openai",
		LLMModel:      "gpt-4.1",
		LatencyMS:     842,
		OccurredAt:    occurredAt.Add(time.Minute),
	}); err != nil {
		t.Fatalf("RecordDecision() error = %v", err)
	}

	if len(webhook.signals) != 1 || len(discord.signals) != 1 {
		t.Fatalf("signal counts = webhook:%d discord:%d, want 1 each", len(webhook.signals), len(discord.signals))
	}
	if len(webhook.decisions) != 1 || len(discord.decisions) != 1 {
		t.Fatalf("decision counts = webhook:%d discord:%d, want 1 each", len(webhook.decisions), len(discord.decisions))
	}
	if len(pagerDuty.signals) != 0 || len(pagerDuty.decisions) != 0 {
		t.Fatalf("pagerduty structured notifications = signals:%d decisions:%d, want 0", len(pagerDuty.signals), len(pagerDuty.decisions))
	}

	if got := webhook.signals[0].Signal; got != domain.PipelineSignalBuy {
		t.Fatalf("webhook signal = %q, want %q", got, domain.PipelineSignalBuy)
	}
	if got := discord.decisions[0].AgentRole; got != domain.AgentRoleTrader {
		t.Fatalf("discord decision role = %q, want %q", got, domain.AgentRoleTrader)
	}
}

func testAlertRules() config.AlertRulesConfig {
	return config.AlertRulesConfig{
		PipelineFailure: config.PipelineFailureAlertRuleConfig{
			Threshold: 3,
			Channels:  []string{ChannelTelegram, ChannelEmail},
		},
		CircuitBreaker: config.ImmediateAlertRuleConfig{
			Channels: []string{ChannelTelegram},
		},
		LLMProviderDown: config.LLMProviderDownAlertRuleConfig{
			ErrorRateThreshold: 0.5,
			Window:             5 * time.Minute,
			Channels:           []string{ChannelTelegram},
		},
		HighLatency: config.HighLatencyAlertRuleConfig{
			Threshold: 120 * time.Second,
			Channels:  []string{ChannelEmail},
		},
		KillSwitch: config.ImmediateAlertRuleConfig{
			Channels: []string{ChannelTelegram},
		},
		DBConnection: config.ImmediateAlertRuleConfig{
			Channels: []string{ChannelEmail, ChannelPagerDuty},
		},
	}
}
