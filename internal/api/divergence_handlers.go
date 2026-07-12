package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
)

type DivergenceSource interface {
	DivergenceFor(ctx context.Context, strategyID string) (backtest.Divergence, error)
}

type divergenceHandlers struct{ src DivergenceSource }

func (h *divergenceHandlers) get(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.src == nil {
		respondError(w, http.StatusServiceUnavailable, "divergence source unavailable", ErrCodeInternal)
		return
	}
	strategyID := r.URL.Query().Get("strategy_id")
	if strategyID == "" {
		respondError(w, http.StatusBadRequest, "missing strategy_id", ErrCodeBadRequest)
		return
	}
	div, err := h.src.DivergenceFor(r.Context(), strategyID)
	if err != nil {
		if errors.Is(err, backtest.ErrDivergenceNotFound) {
			respondError(w, http.StatusNotFound, "divergence not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get divergence", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"strategy_id":   div.StrategyID,
		"backtest":      div.Backtest,
		"live":          div.Live,
		"tolerance":     effectiveTolerance(div),
		"max_abs_delta": div.MaxAbsDelta(),
		"status":        div.Status(),
	})
}

func effectiveTolerance(d backtest.Divergence) float64 {
	if d.Tolerance > 0 {
		return d.Tolerance
	}
	return backtest.DefaultDivergenceTolerance
}

func (s *Server) handleGetBacktestDivergence(w http.ResponseWriter, r *http.Request) {
	(&divergenceHandlers{src: s.divergenceSrc}).get(w, r)
}
