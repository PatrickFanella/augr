package tui

import (
	"strings"
	"testing"
	"time"

	internalapi "github.com/PatrickFanella/get-rich-quick/internal/api"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

func TestRenderIncludesDashboardSections(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 29, 16, 21, 0, 0, time.UTC)
	price := 187.25
	runID := uuid.New()
	view := Render(Snapshot{
		Portfolio: PortfolioSummary{
			OpenPositions: 2,
			UnrealizedPnL: 123.45,
			RealizedPnL:   67.89,
		},
		Strategies: []domain.Strategy{{
			ID:      uuid.New(),
			Name:    "AAPL Trend",
			Ticker:  "AAPL",
			Status:  domain.StrategyStatusActive,
			IsPaper: true,
		}},
		Positions: []domain.Position{{
			ID:           uuid.New(),
			Ticker:       "AAPL",
			Side:         domain.PositionSideLong,
			Quantity:     5,
			AvgEntry:     180,
			CurrentPrice: &price,
		}},
		Risk: risk.EngineStatus{
			RiskStatus: domain.RiskStatusNormal,
			CircuitBreaker: risk.CircuitBreakerStatus{
				State: risk.CircuitBreakerPhaseOpen,
			},
			PositionLimits: risk.PositionLimits{
				MaxConcurrent: 10,
				MaxTotalPct:   1,
			},
		},
		LatestRun: &domain.PipelineRun{
			ID:        runID,
			Ticker:    "AAPL",
			Status:    domain.PipelineStatusRunning,
			StartedAt: now,
		},
		Settings: internalapi.SettingsResponse{
			LLM: internalapi.LLMSettingsResponse{
				DefaultProvider: "openai",
				QuickThinkModel: "gpt-5-mini",
				DeepThinkModel:  "gpt-5.4",
			},
			System: internalapi.SystemInfo{
				Environment: "test",
				Version:     "dev",
			},
		},
		Activity: []ActivityItem{{
			OccurredAt: now,
			Title:      "Pipeline started",
			Details:    "ticker=AAPL",
		}},
	}, 120, 34)

	for _, want := range []string{
		"Trading Agent Dashboard",
		"Dashboard",
		"Strategies",
		"Portfolio",
		"Config",
		"Portfolio summary",
		"Risk status",
		"Pipeline run",
		"Live activity feed",
		"AAPL",
		runID.String(),
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestModelAppliesWebSocketEventsToActivityAndProgress(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	model := NewModel(Snapshot{
		LatestRun: &domain.PipelineRun{
			ID:     runID,
			Ticker: "AAPL",
			Status: domain.PipelineStatusRunning,
		},
	}, 120, 34)

	next, _ := model.Update(wsEventMsg{event: internalapi.WSMessage{
		Type:      internalapi.EventSignal,
		RunID:     runID,
		Timestamp: time.Now().UTC(),
		Data:      map[string]any{"ticker": "AAPL", "signal": "buy"},
	}})
	updated := next.(Model)
	if updated.runProgress < 80 {
		t.Fatalf("runProgress = %d, want at least 80", updated.runProgress)
	}
	if len(updated.snapshot.Activity) == 0 {
		t.Fatal("expected activity feed to receive websocket event")
	}

	final, _ := updated.Update(tea.KeyMsg{Type: tea.KeyRight})
	shifted := final.(Model)
	if shifted.activeTab != 1 {
		t.Fatalf("activeTab = %d, want %d", shifted.activeTab, 1)
	}
}
