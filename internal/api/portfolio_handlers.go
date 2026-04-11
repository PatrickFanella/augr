package api

import (
	"net/http"

	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func (s *Server) handleListPositions(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	filter := repository.PositionFilter{
		Ticker: q.Get("ticker"),
	}
	if !ParseEnumParam(w, q, "side", &filter.Side) {
		return
	}

	positions, err := s.positions.List(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list positions", ErrCodeInternal)
		return
	}
	total, err := s.positions.Count(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count positions", "error", err.Error())
	}
	respondListWithTotal(w, positions, total, limit, offset)
}

func (s *Server) handleGetOpenPositions(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()
	filter := repository.PositionFilter{
		Ticker: q.Get("ticker"),
	}
	if !ParseEnumParam(w, q, "side", &filter.Side) {
		return
	}
	positions, err := s.positions.GetOpen(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list open positions", ErrCodeInternal)
		return
	}
	total, err := s.positions.CountOpen(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count open positions", "error", err.Error())
	}
	respondListWithTotal(w, positions, total, limit, offset)
}

func (s *Server) handlePortfolioSummary(w http.ResponseWriter, r *http.Request) {
	positions, err := s.positions.GetOpen(r.Context(), repository.PositionFilter{}, maxLimit, 0)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get portfolio summary", ErrCodeInternal)
		return
	}

	var totalUnrealized, totalRealized float64
	for _, p := range positions {
		if p.UnrealizedPnL != nil {
			totalUnrealized += *p.UnrealizedPnL
		}
		totalRealized += p.RealizedPnL
	}
	summary := map[string]any{
		"open_positions": len(positions),
		"unrealized_pnl": totalUnrealized,
		"realized_pnl":   totalRealized,
	}
	respondJSON(w, http.StatusOK, summary)
}
