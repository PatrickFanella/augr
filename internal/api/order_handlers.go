package api

import (
	"net/http"

	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func (s *Server) handleListOrders(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	filter := repository.OrderFilter{
		Ticker: q.Get("ticker"),
		Broker: q.Get("broker"),
	}

	if !ParseEnumParam(w, q, "status", &filter.Status) {
		return
	}
	if !ParseEnumParam(w, q, "side", &filter.Side) {
		return
	}
	if !ParseEnumParam(w, q, "order_type", &filter.OrderType) {
		return
	}

	orders, err := s.orders.List(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list orders", ErrCodeInternal)
		return
	}
	total, err := s.orders.Count(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count orders", "error", err.Error())
	}
	respondListWithTotal(w, orders, total, limit, offset)
}

func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	order, err := s.orders.Get(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "order not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get order", ErrCodeInternal)
		return
	}

	fills, fillErr := s.trades.GetByOrder(r.Context(), id, repository.TradeFilter{}, maxLimit, 0)
	if fillErr != nil {
		respondError(w, http.StatusInternalServerError, "failed to get order fills", ErrCodeInternal)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"order": order,
		"fills": fills,
	})
}
