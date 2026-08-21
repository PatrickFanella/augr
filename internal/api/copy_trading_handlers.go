package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (s *Server) requireCopyTrading(w http.ResponseWriter) bool {
	if s.copyTrading != nil {
		return true
	}
	respondError(w, http.StatusServiceUnavailable, "copy trading is not configured", ErrCodeNotImplemented)
	return false
}

func (s *Server) respondCopyTradingError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrNotFound) {
		respondError(w, http.StatusNotFound, err.Error(), ErrCodeNotFound)
		return
	}
	message := err.Error()
	if strings.Contains(message, "required") || strings.Contains(message, "invalid") || strings.Contains(message, "supports") || strings.Contains(message, "must be") || strings.Contains(message, "only") || strings.Contains(message, "does not belong") {
		respondError(w, http.StatusUnprocessableEntity, message, ErrCodeValidation)
		return
	}
	respondError(w, http.StatusInternalServerError, message, ErrCodeInternal)
}

func (s *Server) handleListCopyLeaders(w http.ResponseWriter, r *http.Request) {
	if !s.requireCopyTrading(w) {
		return
	}
	limit, offset := parsePagination(r)
	filter := repository.CopyLeaderFilter{EntityType: domain.CopyLeaderEntityType(r.URL.Query().Get("entity_type")), Query: r.URL.Query().Get("q")}
	items, total, err := s.copyTrading.ListLeaders(r.Context(), filter, limit, offset)
	if err != nil {
		s.respondCopyTradingError(w, err)
		return
	}
	respondListWithTotal(w, items, total, limit, offset)
}

func (s *Server) handleCreateCopyLeader(w http.ResponseWriter, r *http.Request) {
	if !s.requireCopyTrading(w) {
		return
	}
	var leader domain.CopyLeader
	if !decodeJSONBody(w, r, &leader) {
		return
	}
	if err := s.copyTrading.CreateLeader(r.Context(), &leader); err != nil {
		s.respondCopyTradingError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, leader)
}

func (s *Server) handleGetCopyLeader(w http.ResponseWriter, r *http.Request) {
	if !s.requireCopyTrading(w) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid leader id", ErrCodeBadRequest)
		return
	}
	detail, err := s.copyTrading.GetLeader(r.Context(), id)
	if err != nil {
		s.respondCopyTradingError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, detail)
}

func (s *Server) handleAddCopySource(w http.ResponseWriter, r *http.Request) {
	if !s.requireCopyTrading(w) {
		return
	}
	leaderID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid leader id", ErrCodeBadRequest)
		return
	}
	var source domain.CopyLeaderSource
	if !decodeJSONBody(w, r, &source) {
		return
	}
	source.LeaderID = leaderID
	if err := s.copyTrading.AddSource(r.Context(), &source); err != nil {
		s.respondCopyTradingError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, source)
}

func (s *Server) handleRefreshCopySource(w http.ResponseWriter, r *http.Request) {
	if !s.requireCopyTrading(w) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid source id", ErrCodeBadRequest)
		return
	}
	result, err := s.copyTrading.RefreshSource(r.Context(), id)
	if err != nil {
		s.respondCopyTradingError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleUpsertCopyMapping(w http.ResponseWriter, r *http.Request) {
	if !s.requireCopyTrading(w) {
		return
	}
	var mapping domain.CopyInstrumentMapping
	if !decodeJSONBody(w, r, &mapping) {
		return
	}
	if err := s.copyTrading.UpsertMapping(r.Context(), &mapping); err != nil {
		s.respondCopyTradingError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, mapping)
}

func (s *Server) handleListCopySubscriptions(w http.ResponseWriter, r *http.Request) {
	if !s.requireCopyTrading(w) {
		return
	}
	limit, offset := parsePagination(r)
	filter := repository.CopySubscriptionFilter{Status: domain.CopySubscriptionStatus(r.URL.Query().Get("status"))}
	if value := r.URL.Query().Get("leader_id"); value != "" {
		id, err := uuid.Parse(value)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid leader_id", ErrCodeBadRequest)
			return
		}
		filter.LeaderID = &id
	}
	items, total, err := s.copyTrading.ListSubscriptions(r.Context(), filter, limit, offset)
	if err != nil {
		s.respondCopyTradingError(w, err)
		return
	}
	respondListWithTotal(w, items, total, limit, offset)
}

func decodeCopySubscription(w http.ResponseWriter, r *http.Request) (*domain.CopySubscription, bool) {
	defaults := domain.DefaultCopySubscription()
	if err := json.NewDecoder(r.Body).Decode(&defaults); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body", ErrCodeBadRequest)
		return nil, false
	}
	return &defaults, true
}

func (s *Server) handleCreateCopySubscription(w http.ResponseWriter, r *http.Request) {
	if !s.requireCopyTrading(w) {
		return
	}
	subscription, ok := decodeCopySubscription(w, r)
	if !ok {
		return
	}
	subscription.CreatedBy = actorOf(r)
	if subscription.CreatedBy == "" {
		subscription.CreatedBy = "system"
	}
	if err := s.copyTrading.CreateSubscription(r.Context(), subscription); err != nil {
		s.respondCopyTradingError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, subscription)
}

func (s *Server) handleGetCopySubscription(w http.ResponseWriter, r *http.Request) {
	if !s.requireCopyTrading(w) {
		return
	}
	id, ok := copyID(w, r)
	if !ok {
		return
	}
	item, err := s.copyTrading.GetSubscription(r.Context(), id)
	if err != nil {
		s.respondCopyTradingError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, item)
}

func (s *Server) handleUpdateCopySubscription(w http.ResponseWriter, r *http.Request) {
	if !s.requireCopyTrading(w) {
		return
	}
	id, ok := copyID(w, r)
	if !ok {
		return
	}
	replacement, decoded := decodeCopySubscription(w, r)
	if !decoded {
		return
	}
	item, err := s.copyTrading.UpdateSubscription(r.Context(), id, replacement)
	if err != nil {
		s.respondCopyTradingError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, item)
}

func (s *Server) handlePreviewCopySubscription(w http.ResponseWriter, r *http.Request) {
	if !s.requireCopyTrading(w) {
		return
	}
	id, ok := copyID(w, r)
	if !ok {
		return
	}
	preview, err := s.copyTrading.Preview(r.Context(), id)
	if err != nil {
		s.respondCopyTradingError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, preview)
}

func (s *Server) handleActivateCopySubscription(w http.ResponseWriter, r *http.Request) {
	s.handleCopyStatus(w, r, domain.CopySubscriptionPaperActive)
}

func (s *Server) handlePauseCopySubscription(w http.ResponseWriter, r *http.Request) {
	s.handleCopyStatus(w, r, domain.CopySubscriptionPaused)
}

func (s *Server) handleResumeCopySubscription(w http.ResponseWriter, r *http.Request) {
	s.handleCopyStatus(w, r, domain.CopySubscriptionPaperActive)
}

func (s *Server) handleStopCopySubscription(w http.ResponseWriter, r *http.Request) {
	s.handleCopyStatus(w, r, domain.CopySubscriptionStopped)
}

func (s *Server) handleCopyStatus(w http.ResponseWriter, r *http.Request, status domain.CopySubscriptionStatus) {
	if !s.requireCopyTrading(w) {
		return
	}
	id, ok := copyID(w, r)
	if !ok {
		return
	}
	item, err := s.copyTrading.SetStatus(r.Context(), id, status)
	if err != nil {
		s.respondCopyTradingError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, item)
}

func (s *Server) handleRebalanceCopySubscription(w http.ResponseWriter, r *http.Request) {
	if !s.requireCopyTrading(w) {
		return
	}
	id, ok := copyID(w, r)
	if !ok {
		return
	}
	result, err := s.copyTrading.Rebalance(r.Context(), id)
	if err != nil {
		s.respondCopyTradingError(w, err)
		return
	}
	respondJSON(w, http.StatusAccepted, result)
}

func (s *Server) handleListCopyIntents(w http.ResponseWriter, r *http.Request) {
	if !s.requireCopyTrading(w) {
		return
	}
	id, ok := copyID(w, r)
	if !ok {
		return
	}
	limit, offset := parsePagination(r)
	items, err := s.copyTrading.ListIntents(r.Context(), id, limit, offset)
	if err != nil {
		s.respondCopyTradingError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items, "limit": limit, "offset": offset, "total": len(items)})
}

func copyID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid subscription id", ErrCodeBadRequest)
		return uuid.Nil, false
	}
	return id, true
}
