package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// handleRunDiscovery triggers an on-demand strategy discovery run.
func (s *Server) handleRunDiscovery(w http.ResponseWriter, r *http.Request) {
	if s.discoveryDeps == nil {
		respondError(w, http.StatusServiceUnavailable, "discovery not configured", ErrCodeInternal)
		return
	}

	var req struct {
		Tickers    []string `json:"tickers"`
		MarketType string   `json:"market_type,omitempty"`
		DryRun     bool     `json:"dry_run,omitempty"`
		MaxWinners int      `json:"max_winners,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	if len(req.Tickers) == 0 {
		respondError(w, http.StatusBadRequest, "tickers list is required", ErrCodeValidation)
		return
	}

	marketType := domain.MarketTypeStock
	if req.MarketType != "" {
		marketType = domain.MarketType(req.MarketType)
	}
	maxWinners := 3
	if req.MaxWinners > 0 {
		maxWinners = req.MaxWinners
	}

	cfg := discovery.DiscoveryConfig{
		Screener: discovery.ScreenerConfig{
			Tickers:    req.Tickers,
			MinADV:     100000,
			MinATR:     0.5,
			MarketType: marketType,
		},
		Generator: discovery.GeneratorConfig{
			Provider:   s.llmProvider,
			Model:      "",
			MaxRetries: 3,
		},
		Sweep: discovery.SweepConfig{
			InitialCash: 100000,
			Variations:  20,
		},
		Scoring:    discovery.DefaultScoringConfig(),
		Validation: discovery.ValidationConfig{},
		MaxWinners: maxWinners,
		DryRun:     req.DryRun,
	}

	startedAt := time.Now()
	result, err := discovery.RunDiscovery(r.Context(), cfg, *s.discoveryDeps)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "discovery failed: "+err.Error(), ErrCodeInternal)
		return
	}

	// Persist the run if we have the discovery runs table
	if s.discoveryRunRepo != nil {
		configJSON, _ := json.Marshal(cfg)
		resultJSON, _ := json.Marshal(result)
		s.discoveryRunRepo.Create(r.Context(), configJSON, resultJSON, startedAt, result.Duration, result.Candidates, result.Deployed)
	}

	respondJSON(w, http.StatusOK, result)
}

// handleListDiscoveryRuns returns past discovery runs.
func (s *Server) handleListDiscoveryRuns(w http.ResponseWriter, r *http.Request) {
	if s.discoveryRunRepo == nil {
		respondError(w, http.StatusServiceUnavailable, "discovery not configured", ErrCodeInternal)
		return
	}

	limit, offset := parsePagination(r)
	runs, err := s.discoveryRunRepo.List(r.Context(), limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list discovery runs", ErrCodeInternal)
		return
	}
	total, err := s.discoveryRunRepo.Count(r.Context())
	if err != nil {
		s.logger.Warn("failed to count discovery runs", "error", err)
	}
	respondListWithTotal(w, runs, total, limit, offset)
}
