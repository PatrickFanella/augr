package api

import (
	"context"
	"net/http"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// PortfolioSummary reports valuation coverage explicitly so missing marks can
// never be confused with a zero-valued portfolio.
type PortfolioSummary struct {
	OpenPositions        int       `json:"open_positions"`
	MarkedPositions      int       `json:"marked_positions"`
	UnmarkedPositions    int       `json:"unmarked_positions"`
	UnrealizedPnL        *float64  `json:"unrealized_pnl"`
	RealizedPnL          float64   `json:"realized_pnl"`
	TotalPnL             *float64  `json:"total_pnl"`
	GrossCostBasis       float64   `json:"gross_cost_basis"`
	GrossMarkedValue     *float64  `json:"gross_marked_value"`
	ValuationStatus      string    `json:"valuation_status"`
	ValuationGeneratedAt time.Time `json:"valuation_generated_at"`
}

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
	openPositions, openPositionCount, err := s.loadAllOpenPositions(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get portfolio summary", ErrCodeInternal)
		return
	}
	allPositions, err := s.positions.List(r.Context(), repository.PositionFilter{}, maxLimit, 0)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get portfolio summary", ErrCodeInternal)
		return
	}

	var totalUnrealized, totalRealized, grossCostBasis, grossMarkedValue float64
	markedPositions := 0
	for _, p := range openPositions {
		multiplier := p.ContractMultiplier
		if multiplier == 0 {
			multiplier = 1
		}
		grossCostBasis += absFloat(p.Quantity * p.AvgEntry * multiplier)
		if p.CurrentPrice != nil && p.UnrealizedPnL != nil {
			markedPositions++
			totalUnrealized += *p.UnrealizedPnL
			grossMarkedValue += absFloat(p.Quantity * *p.CurrentPrice * multiplier)
		}
	}
	for _, p := range allPositions {
		if p.ClosedAt == nil {
			continue
		}
		totalRealized += p.RealizedPnL
	}
	unmarkedPositions := openPositionCount - markedPositions
	valuationStatus := "complete"
	if unmarkedPositions > 0 {
		valuationStatus = "partial"
		if markedPositions == 0 {
			valuationStatus = "unavailable"
		}
	}
	var unrealizedPnL, totalPnL, markedValue *float64
	if unmarkedPositions == 0 {
		unrealizedPnL = &totalUnrealized
		total := totalRealized + totalUnrealized
		totalPnL = &total
		markedValue = &grossMarkedValue
	}
	summary := PortfolioSummary{
		OpenPositions:        openPositionCount,
		MarkedPositions:      markedPositions,
		UnmarkedPositions:    unmarkedPositions,
		UnrealizedPnL:        unrealizedPnL,
		RealizedPnL:          totalRealized,
		TotalPnL:             totalPnL,
		GrossCostBasis:       grossCostBasis,
		GrossMarkedValue:     markedValue,
		ValuationStatus:      valuationStatus,
		ValuationGeneratedAt: time.Now().UTC(),
	}
	respondJSON(w, http.StatusOK, summary)
}

func (s *Server) loadAllOpenPositions(ctx context.Context) ([]domain.Position, int, error) {
	total, err := s.positions.CountOpen(ctx, repository.PositionFilter{})
	if err != nil {
		return nil, 0, err
	}
	positions := make([]domain.Position, 0, total)
	for offset := 0; offset < total; offset += maxLimit {
		page, err := s.positions.GetOpen(ctx, repository.PositionFilter{}, maxLimit, offset)
		if err != nil {
			return nil, 0, err
		}
		positions = append(positions, page...)
		if len(page) < maxLimit {
			break
		}
	}
	return positions, total, nil
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
