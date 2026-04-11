package api

import (
	"net/http"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func (s *Server) handleListTrades(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	filter := repository.TradeFilter{}

	if !ParseUUIDParam(w, q, "order_id", &filter.OrderID) {
		return
	}
	if !ParseUUIDParam(w, q, "position_id", &filter.PositionID) {
		return
	}

	if filter.OrderID != nil && filter.PositionID != nil {
		respondError(w, http.StatusBadRequest, "order_id and position_id cannot be combined", ErrCodeBadRequest)
		return
	}

	if ticker := q.Get("ticker"); ticker != "" {
		filter.Ticker = &ticker
	}
	if side := q.Get("side"); side != "" {
		orderSide := domain.OrderSide(side)
		if !orderSide.IsValid() {
			respondError(w, http.StatusBadRequest, "invalid side", ErrCodeBadRequest)
			return
		}
		filter.Side = &orderSide
	}
	if !ParseTimeParam(w, q, "start_date", time.RFC3339, &filter.StartDate) {
		return
	}
	if !ParseTimeParam(w, q, "end_date", time.RFC3339, &filter.EndDate) {
		return
	}

	var (
		trades []domain.Trade
		err    error
	)

	switch {
	case filter.OrderID != nil:
		trades, err = s.trades.GetByOrder(r.Context(), *filter.OrderID, filter, limit, offset)
	case filter.PositionID != nil:
		trades, err = s.trades.GetByPosition(r.Context(), *filter.PositionID, filter, limit, offset)
	default:
		trades, err = s.trades.List(r.Context(), filter, limit, offset)
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list trades", ErrCodeInternal)
		return
	}

	if trades == nil {
		trades = []domain.Trade{}
	}

	total, err := s.trades.Count(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count trades", "error", err.Error())
	}
	respondListWithTotal(w, trades, total, limit, offset)
}
