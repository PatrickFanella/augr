package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/automation"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/signal"
	"github.com/go-chi/chi/v5"
)

type PolymarketMarketDataFetcher interface {
	GetMarketData(ctx context.Context, slug string) (*agent.PredictionMarketData, error)
}

// PublishPolymarketEvent sends a polymarket event over the websocket hub.
func (s *Server) PublishPolymarketEvent(eventType EventType, data any) {
	if s.hub != nil {
		s.hub.BroadcastPolymarket(WSMessage{Type: eventType, Data: data, Timestamp: time.Now().UTC()})
	}
}

func parseInt(raw string, def int) int {
	if raw == "" {
		return def
	}
	if v, err := strconv.Atoi(raw); err == nil {
		return v
	}
	return def
}

func parseFloat(raw string) float64 {
	if raw == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(raw, 64)
	return v
}

func decodeJSONBody[T any](w http.ResponseWriter, r *http.Request, dst *T) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body", ErrCodeBadRequest)
		return false
	}
	return true
}

func respondRepoError(w http.ResponseWriter, err error) {
	if err == repository.ErrNotFound {
		respondError(w, http.StatusNotFound, "not found", ErrCodeNotFound)
		return
	}
	respondError(w, http.StatusInternalServerError, err.Error(), ErrCodeInternal)
}

// handlers
func (s *Server) handleListPolymarketAccounts(w http.ResponseWriter, r *http.Request) {
	if s.polymarketAccountRepo == nil {
		respondError(w, http.StatusServiceUnavailable, "polymarket accounts not configured", ErrCodeNotImplemented)
		return
	}
	q := r.URL.Query()
	f := repository.PolymarketAccountFilter{MinWinRate: parseFloat(q.Get("min_win_rate")), MinResolved: parseInt(q.Get("min_resolved"), 0), MinVolume: parseFloat(q.Get("min_volume")), MinTrades: parseInt(q.Get("min_trades"), 0), Sort: q.Get("sort"), Limit: parseInt(q.Get("limit"), 100), Offset: parseInt(q.Get("offset"), 0)}
	if tracked := q.Get("tracked"); tracked != "" {
		v := tracked == "true"
		f.Tracked = &v
	}
	items, err := s.polymarketAccountRepo.ListAccounts(r.Context(), f)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error(), ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items, "limit": f.Limit, "offset": f.Offset, "total": len(items)})
}

func (s *Server) handleGetPolymarketAccount(w http.ResponseWriter, r *http.Request) {
	if s.polymarketAccountRepo == nil {
		respondError(w, http.StatusServiceUnavailable, "polymarket accounts not configured", ErrCodeNotImplemented)
		return
	}
	acc, err := s.polymarketAccountRepo.GetAccount(r.Context(), chi.URLParam(r, "address"))
	if err != nil {
		respondRepoError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, acc)
}

func (s *Server) handleListPolymarketAccountTrades(w http.ResponseWriter, r *http.Request) {
	if s.polymarketAccountRepo == nil {
		respondError(w, http.StatusServiceUnavailable, "polymarket accounts not configured", ErrCodeNotImplemented)
		return
	}
	from, _ := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	to, _ := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	lim := parseInt(r.URL.Query().Get("limit"), 200)
	trades, err := s.polymarketAccountRepo.ListTradesByAccount(r.Context(), chi.URLParam(r, "address"), from, to, lim)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error(), ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": trades, "limit": lim, "offset": 0})
}

func (s *Server) handlePatchPolymarketAccountTracked(w http.ResponseWriter, r *http.Request) {
	if s.polymarketAccountRepo == nil {
		respondError(w, http.StatusServiceUnavailable, "polymarket accounts not configured", ErrCodeNotImplemented)
		return
	}
	var body struct {
		Tracked bool `json:"tracked"`
	}
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if err := s.polymarketAccountRepo.SetTracked(r.Context(), chi.URLParam(r, "address"), body.Tracked); err != nil {
		respondRepoError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleListPolymarketRecentTrades(w http.ResponseWriter, r *http.Request) {
	if s.polymarketAccountRepo == nil {
		respondError(w, http.StatusServiceUnavailable, "polymarket accounts not configured", ErrCodeNotImplemented)
		return
	}
	lim := parseInt(r.URL.Query().Get("limit"), 100)
	trades, err := s.polymarketAccountRepo.ListRecentTrades(r.Context(), lim)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error(), ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": trades, "limit": lim, "offset": 0})
}

func (s *Server) handleListPolymarketRecentSignals(w http.ResponseWriter, r *http.Request) {
	if s.signalStore == nil {
		respondError(w, http.StatusServiceUnavailable, "signal store not configured", ErrCodeNotImplemented)
		return
	}
	signals := s.signalStore.ListSignals(parseInt(r.URL.Query().Get("min_urgency"), 0), parseInt(r.URL.Query().Get("limit"), 100), 0)
	out := make([]signal.StoredSignal, 0, len(signals))
	for _, sig := range signals {
		if sig.Source == "polymarket-clob" || sig.Source == "polymarket-whale" {
			out = append(out, sig)
		}
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out, "total": len(out)})
}

func (s *Server) handleGetPolymarketMarket(w http.ResponseWriter, r *http.Request) {
	fetcher := s.polymarketClient
	if fetcher == nil {
		respondError(w, http.StatusServiceUnavailable, "polymarket client not configured", ErrCodeNotImplemented)
		return
	}
	market, err := fetcher.GetMarketData(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error(), ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, market)
}

func (s *Server) handleListPolymarketWatched(w http.ResponseWriter, r *http.Request) {
	if s.polymarketWatchedRepo == nil {
		respondError(w, http.StatusServiceUnavailable, "watched markets not configured", ErrCodeNotImplemented)
		return
	}
	items, err := s.polymarketWatchedRepo.List(r.Context(), false)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error(), ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, items)
}

func (s *Server) handleAddPolymarketWatched(w http.ResponseWriter, r *http.Request) {
	if s.polymarketWatchedRepo == nil {
		respondError(w, http.StatusServiceUnavailable, "watched markets not configured", ErrCodeNotImplemented)
		return
	}
	var body struct {
		Slug string `json:"slug"`
		Note string `json:"note"`
	}
	if !decodeJSONBody(w, r, &body) {
		return
	}
	_ = s.polymarketWatchedRepo.Add(r.Context(), &domain.PolymarketWatchedMarket{Slug: body.Slug, Enabled: true, AddedAt: time.Now().UTC(), Note: body.Note})
	respondJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (s *Server) handleDeletePolymarketWatched(w http.ResponseWriter, r *http.Request) {
	if s.polymarketWatchedRepo == nil {
		respondError(w, http.StatusServiceUnavailable, "watched markets not configured", ErrCodeNotImplemented)
		return
	}
	_ = s.polymarketWatchedRepo.Remove(r.Context(), chi.URLParam(r, "slug"))
	respondJSON(w, http.StatusNoContent, nil)
}

func (s *Server) handlePatchPolymarketWatched(w http.ResponseWriter, r *http.Request) {
	if s.polymarketWatchedRepo == nil {
		respondError(w, http.StatusServiceUnavailable, "watched markets not configured", ErrCodeNotImplemented)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSONBody(w, r, &body) {
		return
	}
	_ = s.polymarketWatchedRepo.SetEnabled(r.Context(), chi.URLParam(r, "slug"), body.Enabled)
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleGetPolymarketJobsStatus(w http.ResponseWriter, _ *http.Request) {
	if s.automation == nil {
		respondError(w, http.StatusServiceUnavailable, "automation not configured", ErrCodeNotImplemented)
		return
	}
	var out []automation.JobStatus
	for _, st := range s.automation.Status() {
		if strings.HasPrefix(st.Name, "polymarket_") {
			out = append(out, st)
		}
	}
	respondJSON(w, http.StatusOK, out)
}
