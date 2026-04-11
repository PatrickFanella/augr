package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/service"
)

// --- BacktestConfig CRUD handlers ---

func (s *Server) handleListBacktestConfigs(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	filter := repository.BacktestConfigFilter{}
	if v := r.URL.Query().Get("strategy_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid strategy_id", ErrCodeBadRequest)
			return
		}
		filter.StrategyID = &id
	}

	configs, err := s.backtestConfigs.List(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list backtest configs", ErrCodeInternal)
		return
	}
	total, err := s.backtestConfigs.Count(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count backtest configs", "error", err)
	}
	respondListWithTotal(w, configs, total, limit, offset)
}

func (s *Server) handleCreateBacktestConfig(w http.ResponseWriter, r *http.Request) {
	var config domain.BacktestConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	if err := config.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeValidation)
		return
	}
	if err := validateScheduleCron(config.ScheduleCron); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeValidation)
		return
	}
	config.ID = uuid.New()
	if err := s.backtestConfigs.Create(r.Context(), &config); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create backtest config", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusCreated, config)
}

func (s *Server) handleGetBacktestConfig(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	config, err := s.backtestConfigs.Get(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "backtest config not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get backtest config", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, config)
}

func (s *Server) handleUpdateBacktestConfig(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	var config domain.BacktestConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	config.ID = id
	if err := config.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeValidation)
		return
	}
	if err := validateScheduleCron(config.ScheduleCron); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeValidation)
		return
	}
	if err := s.backtestConfigs.Update(r.Context(), &config); err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "backtest config not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to update backtest config", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, config)
}

func (s *Server) handleDeleteBacktestConfig(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	if err := s.backtestConfigs.Delete(r.Context(), id); err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "backtest config not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to delete backtest config", ErrCodeInternal)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Run a backtest ---

func (s *Server) handleRunBacktestConfig(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}

	run, err := s.backtestSvc.RunBacktest(r.Context(), id, actorOf(r))
	if err != nil {
		if svcErr, ok := err.(*service.ServiceError); ok {
			code := ErrCodeInternal
			switch svcErr.Status {
			case 400:
				code = ErrCodeBadRequest
			case 404:
				code = ErrCodeNotFound
			case 422:
				code = ErrCodeValidation
			}
			respondError(w, svcErr.Status, svcErr.Message, code)
			return
		}
		respondError(w, http.StatusInternalServerError, "backtest failed", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusCreated, run)
}

// --- BacktestRun list/get handlers ---

func (s *Server) handleListBacktestRuns(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	filter := repository.BacktestRunFilter{}
	if v := r.URL.Query().Get("backtest_config_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid backtest_config_id", ErrCodeBadRequest)
			return
		}
		filter.BacktestConfigID = &id
	}

	runs, err := s.backtestRuns.List(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list backtest runs", ErrCodeInternal)
		return
	}
	total, err := s.backtestRuns.Count(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count backtest runs", "error", err)
	}
	respondListWithTotal(w, runs, total, limit, offset)
}

func (s *Server) handleGetBacktestRun(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	run, err := s.backtestRuns.Get(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "backtest run not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get backtest run", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, run)
}
