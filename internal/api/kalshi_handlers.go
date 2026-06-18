package api

import (
	"errors"
	"net/http"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type KalshiSummaryResponse struct {
	WatchedMarkets  []domain.KalshiWatchedMarket  `json:"watched_markets"`
	LatestSnapshots []domain.KalshiMarketSnapshot `json:"latest_snapshots"`
	Discovery       KalshiDiscoverySummary        `json:"discovery"`
	Strategies      KalshiStrategySummary         `json:"strategies"`
}

type KalshiDiscoverySummary struct {
	LastRun *domain.KalshiDiscoveryRun `json:"last_run"`
	Status  string                     `json:"status"`
}

type KalshiStrategySummary struct {
	ActivePaper int `json:"active_paper"`
}

func (s *Server) handleGetKalshiSummary(w http.ResponseWriter, r *http.Request) {
	if s.kalshiWatchedRepo == nil || s.kalshiSnapshotsRepo == nil || s.kalshiDiscoveryRuns == nil || s.strategies == nil {
		respondError(w, http.StatusServiceUnavailable, "kalshi summary dependencies are not configured", ErrCodeNotImplemented)
		return
	}

	watched, err := s.kalshiWatchedRepo.ListEnabled(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list kalshi watched markets", ErrCodeInternal)
		return
	}

	snapshots, err := s.kalshiSnapshotsRepo.ListRecent(r.Context(), 10)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list kalshi market snapshots", ErrCodeInternal)
		return
	}

	runs, err := s.kalshiDiscoveryRuns.ListLatest(r.Context(), 1)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list kalshi discovery runs", ErrCodeInternal)
		return
	}

	discovery := KalshiDiscoverySummary{Status: "not_started"}
	if len(runs) > 0 {
		discovery.LastRun = &runs[0]
		discovery.Status = runs[0].Status
	} else if active, err := s.kalshiDiscoveryRuns.GetActive(r.Context()); err == nil {
		discovery.LastRun = active
		discovery.Status = active.Status
	} else if err != nil && !errors.Is(err, repository.ErrNotFound) {
		respondError(w, http.StatusInternalServerError, "failed to get active kalshi discovery run", ErrCodeInternal)
		return
	}

	isPaper := true
	activePaper, err := s.strategies.Count(r.Context(), repository.StrategyFilter{
		MarketType: domain.MarketTypeKalshi,
		Status:     domain.StrategyStatusActive,
		IsPaper:    &isPaper,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to count kalshi paper strategies", ErrCodeInternal)
		return
	}

	respondJSON(w, http.StatusOK, KalshiSummaryResponse{
		WatchedMarkets:  watched,
		LatestSnapshots: snapshots,
		Discovery:       discovery,
		Strategies:      KalshiStrategySummary{ActivePaper: activePaper},
	})
}
