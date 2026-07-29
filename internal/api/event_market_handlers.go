package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/polymarketdiscovery"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// EventMarketProviderSummary is a compact read model for a provider's event-market state.
type EventMarketProviderSummary struct {
	Provider         string     `json:"provider"`
	WatchedMarkets   int        `json:"watched_markets"`
	ActivePaper      int        `json:"active_paper"`
	LastRunStatus    string     `json:"last_run_status"`
	LiveTradingReady bool       `json:"live_trading_ready"`
	DataEnvironment  string     `json:"data_environment,omitempty"`
	DataStatus       string     `json:"data_status,omitempty"`
	DataCapturedAt   *time.Time `json:"data_captured_at,omitempty"`
	DataAgeSeconds   *int64     `json:"data_age_seconds,omitempty"`
}

// EventMarketsSummaryResponse is the response envelope for the shared event-market summary endpoint.
type EventMarketsSummaryResponse struct {
	Providers []EventMarketProviderSummary `json:"providers"`
}

func (s *Server) handleGetEventMarketsSummary(w http.ResponseWriter, r *http.Request) {
	providers := make([]EventMarketProviderSummary, 0, 2)

	kalshi, ok, err := s.buildKalshiEventMarketSummary(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error(), ErrCodeInternal)
		return
	}
	if ok {
		providers = append(providers, kalshi)
	}

	polymarket, ok, err := s.buildPolymarketEventMarketSummary(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error(), ErrCodeInternal)
		return
	}
	if ok {
		providers = append(providers, polymarket)
	}

	if len(providers) == 0 {
		respondError(w, http.StatusServiceUnavailable, "event market summary dependencies are not configured", ErrCodeNotImplemented)
		return
	}

	respondJSON(w, http.StatusOK, EventMarketsSummaryResponse{Providers: providers})
}

func (s *Server) buildKalshiEventMarketSummary(ctx context.Context) (EventMarketProviderSummary, bool, error) {
	if s.kalshiWatchedRepo == nil || s.kalshiSnapshotsRepo == nil || s.kalshiDiscoveryRuns == nil || s.strategies == nil {
		return EventMarketProviderSummary{}, false, nil
	}

	watched, err := s.kalshiWatchedRepo.ListEnabled(ctx)
	if err != nil {
		return EventMarketProviderSummary{}, true, fmt.Errorf("failed to list kalshi watched markets: %w", err)
	}

	isPaper := true
	activePaper, err := s.strategies.Count(ctx, repository.StrategyFilter{
		MarketType: domain.MarketTypeKalshi,
		Status:     domain.StrategyStatusActive,
		IsPaper:    &isPaper,
	})
	if err != nil {
		return EventMarketProviderSummary{}, true, fmt.Errorf("failed to count kalshi paper strategies: %w", err)
	}

	lastRunStatus := "not_started"
	runs, err := s.kalshiDiscoveryRuns.ListLatest(ctx, 1)
	if err != nil {
		return EventMarketProviderSummary{}, true, fmt.Errorf("failed to list kalshi discovery runs: %w", err)
	}
	if len(runs) > 0 {
		lastRunStatus = runs[0].Status
	} else if active, err := s.kalshiDiscoveryRuns.GetActive(ctx); err == nil {
		lastRunStatus = active.Status
	} else if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return EventMarketProviderSummary{}, true, fmt.Errorf("failed to get active kalshi discovery run: %w", err)
	}
	dataEnvironment := "unknown"
	dataStatus := "unavailable"
	var dataCapturedAt *time.Time
	var dataAgeSeconds *int64
	snapshots, err := s.kalshiSnapshotsRepo.ListRecent(ctx, 1)
	if err != nil {
		return EventMarketProviderSummary{}, true, fmt.Errorf("failed to list kalshi market snapshots: %w", err)
	}
	if len(snapshots) > 0 {
		capturedAt := snapshots[0].CapturedAt
		age := int64(time.Since(capturedAt).Seconds())
		if age < 0 {
			age = 0
		}
		dataCapturedAt = &capturedAt
		dataAgeSeconds = &age
		dataEnvironment = snapshots[0].Environment
		dataStatus = "current"
		if age > int64((8 * time.Hour).Seconds()) {
			dataStatus = "stale"
		}
	}

	return EventMarketProviderSummary{
		Provider:         "kalshi",
		WatchedMarkets:   len(watched),
		ActivePaper:      activePaper,
		LastRunStatus:    lastRunStatus,
		LiveTradingReady: false,
		DataEnvironment:  dataEnvironment,
		DataStatus:       dataStatus,
		DataCapturedAt:   dataCapturedAt,
		DataAgeSeconds:   dataAgeSeconds,
	}, true, nil
}

func (s *Server) buildPolymarketEventMarketSummary(ctx context.Context) (EventMarketProviderSummary, bool, error) {
	if s.polymarketWatchedRepo == nil || s.strategies == nil {
		return EventMarketProviderSummary{}, false, nil
	}

	watched, err := s.polymarketWatchedRepo.List(ctx, true)
	if err != nil {
		return EventMarketProviderSummary{}, true, fmt.Errorf("failed to list polymarket watched markets: %w", err)
	}

	isPaper := true
	activePaper, err := s.strategies.Count(ctx, repository.StrategyFilter{
		MarketType: domain.MarketTypePolymarket,
		Status:     domain.StrategyStatusActive,
		IsPaper:    &isPaper,
	})
	if err != nil {
		return EventMarketProviderSummary{}, true, fmt.Errorf("failed to count polymarket paper strategies: %w", err)
	}

	lastRunStatus := "not_started"
	if status := s.polymarketDiscoveryStatus(); status != "" {
		lastRunStatus = status
	}

	return EventMarketProviderSummary{
		Provider:         "polymarket",
		WatchedMarkets:   len(watched),
		ActivePaper:      activePaper,
		LastRunStatus:    lastRunStatus,
		LiveTradingReady: false,
	}, true, nil
}

func (s *Server) polymarketDiscoveryStatus() string {
	if s.automation != nil {
		for _, status := range s.automation.Status() {
			if status.Name != "polymarket_strategy_discovery" {
				continue
			}
			if status.Running {
				return "running"
			}
			switch {
			case status.LastResult == "success":
				return "completed"
			case status.LastResult == "failed":
				return "failed"
			case strings.HasPrefix(status.LastResult, "ok "):
				return "completed"
			case strings.HasPrefix(status.LastResult, "error"):
				return "failed"
			case strings.TrimSpace(status.LastResult) != "":
				return status.LastResult
			case status.LastRun != nil:
				return "completed"
			default:
				return "not_started"
			}
		}
	}
	if polymarketdiscovery.LastResult() != nil {
		return "completed"
	}
	return ""
}
