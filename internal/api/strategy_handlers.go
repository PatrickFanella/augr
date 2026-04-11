package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func (s *Server) handleListStrategies(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	filter := repository.StrategyFilter{
		Ticker: q.Get("ticker"),
	}

	if !ParseEnumParam(w, q, "market_type", &filter.MarketType) {
		return
	}

	if status := q.Get("status"); status != "" {
		switch status {
		case domain.StrategyStatusActive, domain.StrategyStatusPaused, domain.StrategyStatusInactive:
		default:
			respondError(w, http.StatusBadRequest, "invalid status", ErrCodeBadRequest)
			return
		}
		filter.Status = status
	}

	if v := q.Get("is_paper"); v != "" {
		b := v == "true"
		filter.IsPaper = &b
	}

	strategies, err := s.strategies.List(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list strategies", ErrCodeInternal)
		return
	}
	total, err := s.strategies.Count(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count strategies", "error", err.Error())
	}
	respondListWithTotal(w, strategies, total, limit, offset)
}

func (s *Server) handleGetStrategy(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	strategy, err := s.strategies.Get(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "strategy not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get strategy", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, strategy)
}

func (s *Server) handleRunStrategy(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	if s.runner == nil {
		respondError(w, http.StatusNotImplemented, "manual strategy runs are not configured", ErrCodeNotImplemented)
		return
	}

	strategy, err := s.strategies.Get(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "strategy not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get strategy", ErrCodeInternal)
		return
	}

	result, err := s.runner.RunStrategy(r.Context(), *strategy)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to run strategy", ErrCodeInternal)
		return
	}
	if result == nil {
		respondError(w, http.StatusInternalServerError, "strategy run returned no result", ErrCodeInternal)
		return
	}

	s.BroadcastRunResult(result)
	s.writeAuditLog(r.Context(), actorOf(r), "strategy.manual_run", "strategy", &id,
		map[string]string{"ticker": strategy.Ticker})
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleCreateStrategy(w http.ResponseWriter, r *http.Request) {
	var strategy domain.Strategy
	if err := json.NewDecoder(r.Body).Decode(&strategy); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	if err := strategy.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeValidation)
		return
	}
	if err := validateStrategyConfigPayload(strategy.Config); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeValidation)
		return
	}
	if err := validateScheduleCron(strategy.ScheduleCron); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeValidation)
		return
	}
	strategy.ID = uuid.New()
	if err := s.strategies.Create(r.Context(), &strategy); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create strategy", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusCreated, strategy)
}

func (s *Server) handleUpdateStrategy(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	var strategy domain.Strategy
	if err := json.NewDecoder(r.Body).Decode(&strategy); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	strategy.ID = id
	if err := strategy.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeValidation)
		return
	}
	if err := validateStrategyConfigPayload(strategy.Config); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeValidation)
		return
	}
	if err := validateScheduleCron(strategy.ScheduleCron); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeValidation)
		return
	}
	if err := s.strategies.Update(r.Context(), &strategy); err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "strategy not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to update strategy", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, strategy)
}

func (s *Server) handleDeleteStrategy(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	if err := s.strategies.Delete(r.Context(), id); err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "strategy not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to delete strategy", ErrCodeInternal)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePauseStrategy(w http.ResponseWriter, r *http.Request) {
	s.handleStrategyTransition(w, r, domain.StrategyStatusActive, domain.StrategyStatusPaused, "pause")
}

func (s *Server) handleResumeStrategy(w http.ResponseWriter, r *http.Request) {
	s.handleStrategyTransition(w, r, domain.StrategyStatusPaused, domain.StrategyStatusActive, "resume")
}

func (s *Server) handleSkipNextStrategy(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	strategy, err := s.strategies.Get(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "strategy not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get strategy", ErrCodeInternal)
		return
	}
	if strategy.Status != domain.StrategyStatusActive {
		respondError(w, http.StatusConflict, "skip-next requires status \"active\"", ErrCodeConflict)
		return
	}
	strategy.SkipNextRun = true
	if err := s.strategies.Update(r.Context(), strategy); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update strategy", ErrCodeInternal)
		return
	}
	s.writeAuditLog(r.Context(), actorOf(r), "strategy.skip_next", "strategy", &id, nil)
	respondJSON(w, http.StatusOK, strategy)
}

func (s *Server) handleStrategyTransition(w http.ResponseWriter, r *http.Request, fromStatus, toStatus, verb string) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	strategy, err := s.strategies.Get(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "strategy not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get strategy", ErrCodeInternal)
		return
	}
	if strategy.Status != fromStatus {
		msg := fmt.Sprintf("cannot %s: strategy status is %q, must be %q", verb, strategy.Status, fromStatus)
		respondError(w, http.StatusConflict, msg, ErrCodeConflict)
		return
	}
	strategy.Status = toStatus
	if err := s.strategies.Update(r.Context(), strategy); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update strategy", ErrCodeInternal)
		return
	}
	s.writeAuditLog(r.Context(), actorOf(r), "strategy."+verb+"d", "strategy", &id, nil)
	respondJSON(w, http.StatusOK, strategy)
}

func validateScheduleCron(expr string) error {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}
	if _, err := cron.ParseStandard(expr); err != nil {
		return fmt.Errorf("invalid schedule_cron %q: %w", expr, err)
	}
	return nil
}

func validateStrategyConfigPayload(raw domain.StrategyConfig) error {
	if len(raw) == 0 {
		return nil
	}

	var cfg agent.StrategyConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	return agent.ValidateStrategyConfig(cfg)
}
