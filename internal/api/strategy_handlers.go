package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
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

	strategy, err := s.strategies.Get(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "strategy not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get strategy", ErrCodeInternal)
		return
	}
	if err := requirePaperStrategy(strategy, "manual run"); err != nil {
		respondError(w, http.StatusConflict, err.Error(), ErrCodeConflict)
		return
	}
	if strategy.Status != domain.StrategyStatusActive {
		respondError(w, http.StatusConflict, "manual run requires status \"active\"", ErrCodeConflict)
		return
	}
	if s.runner == nil {
		respondError(w, http.StatusNotImplemented, "manual strategy runs are not configured", ErrCodeNotImplemented)
		return
	}

	// Run the strategy asynchronously so the HTTP client disconnect does not
	// cancel the pipeline context.  Return 202 Accepted immediately.
	runCtx := context.WithoutCancel(r.Context())
	go func() {
		result, err := s.runner.RunStrategy(runCtx, *strategy)
		if err != nil {
			slog.Error("async strategy run failed", slog.String("strategy_id", id.String()), slog.String("error", err.Error()))
			return
		}
		if result != nil {
			s.BroadcastRunResult(result)
		}
	}()

	s.writeAuditLog(r.Context(), actorOf(r), "strategy.manual_run", "strategy", &id,
		map[string]string{"ticker": strategy.Ticker})
	respondJSON(w, http.StatusAccepted, map[string]string{
		"status":      "accepted",
		"strategy_id": id.String(),
		"message":     "strategy run started",
	})
}

func (s *Server) handleCreateStrategy(w http.ResponseWriter, r *http.Request) {
	var req createStrategyRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	if req.IsPaper != nil && !*req.IsPaper {
		respondError(w, http.StatusBadRequest, "strategy create is paper-only; is_paper must be true when provided", ErrCodeValidation)
		return
	}
	strategy := domain.Strategy{
		Name:         req.Name,
		Description:  req.Description,
		Ticker:       req.Ticker,
		MarketType:   req.MarketType,
		ScheduleCron: req.ScheduleCron,
		Config:       req.Config,
		Status:       domain.StrategyStatusActive,
		SkipNextRun:  false,
		IsPaper:      true,
	}
	if len(strategy.Config) == 0 {
		strategy.Config = domain.StrategyConfig(`{}`)
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
	s.writeAuditLog(r.Context(), actorOf(r), "strategy.created", "strategy", &strategy.ID,
		map[string]any{"ticker": strategy.Ticker, "market_type": strategy.MarketType, "is_paper": strategy.IsPaper})
	respondJSON(w, http.StatusCreated, strategy)
}

type createStrategyRequest struct {
	Name         string                `json:"name"`
	Description  string                `json:"description,omitempty"`
	Ticker       string                `json:"ticker"`
	MarketType   domain.MarketType     `json:"market_type"`
	ScheduleCron string                `json:"schedule_cron,omitempty"`
	Config       domain.StrategyConfig `json:"config,omitempty"`
	IsPaper      *bool                 `json:"is_paper,omitempty"`
}

func (s *Server) handleUpdateStrategy(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	var req updateStrategyRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
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
	if !strategy.IsPaper {
		respondError(w, http.StatusConflict, "only paper strategies can be edited", ErrCodeConflict)
		return
	}
	if req.UpdatedAt != nil && !strategy.UpdatedAt.Equal(*req.UpdatedAt) {
		respondError(w, http.StatusConflict, "strategy changed since it was loaded", ErrCodeConflict)
		return
	}
	before := *strategy
	applyStrategyUpdateRequest(strategy, req)
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
	if err := s.strategies.Update(r.Context(), strategy); err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "strategy not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to update strategy", ErrCodeInternal)
		return
	}
	s.writeAuditLog(r.Context(), actorOf(r), "strategy.updated", "strategy", &id, strategyUpdateAuditDetails(before, *strategy))
	respondJSON(w, http.StatusOK, strategy)
}

type updateStrategyRequest struct {
	Name         *string                `json:"name,omitempty"`
	Description  *string                `json:"description,omitempty"`
	Ticker       *string                `json:"ticker,omitempty"`
	MarketType   *domain.MarketType     `json:"market_type,omitempty"`
	ScheduleCron *string                `json:"schedule_cron,omitempty"`
	Config       *domain.StrategyConfig `json:"config,omitempty"`
	UpdatedAt    *time.Time             `json:"updated_at,omitempty"`
}

func applyStrategyUpdateRequest(strategy *domain.Strategy, req updateStrategyRequest) {
	if req.Name != nil {
		strategy.Name = *req.Name
	}
	if req.Description != nil {
		strategy.Description = *req.Description
	}
	if req.Ticker != nil {
		strategy.Ticker = *req.Ticker
	}
	if req.MarketType != nil {
		strategy.MarketType = *req.MarketType
	}
	if req.ScheduleCron != nil {
		strategy.ScheduleCron = *req.ScheduleCron
	}
	if req.Config != nil {
		strategy.Config = *req.Config
	}
}

func strategyUpdateAuditDetails(before, after domain.Strategy) map[string]any {
	return map[string]any{
		"before": map[string]any{
			"name":          before.Name,
			"description":   before.Description,
			"ticker":        before.Ticker,
			"market_type":   before.MarketType,
			"schedule_cron": before.ScheduleCron,
		},
		"after": map[string]any{
			"name":          after.Name,
			"description":   after.Description,
			"ticker":        after.Ticker,
			"market_type":   after.MarketType,
			"schedule_cron": after.ScheduleCron,
		},
	}
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
	if err := requirePaperStrategy(strategy, "skip-next"); err != nil {
		respondError(w, http.StatusConflict, err.Error(), ErrCodeConflict)
		return
	}
	strategy, err = markPaperSkipNext(r.Context(), s.strategies, id, strategy)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusConflict, "strategy changed before skip-next could be applied", ErrCodeConflict)
			return
		}
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
	if err := requirePaperStrategy(strategy, verb); err != nil {
		respondError(w, http.StatusConflict, err.Error(), ErrCodeConflict)
		return
	}
	strategy, err = transitionPaperStatus(r.Context(), s.strategies, id, fromStatus, toStatus, strategy)
	if err != nil {
		if isNotFound(err) {
			msg := fmt.Sprintf("strategy changed before %s could be applied", verb)
			respondError(w, http.StatusConflict, msg, ErrCodeConflict)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to update strategy", ErrCodeInternal)
		return
	}
	s.writeAuditLog(r.Context(), actorOf(r), "strategy."+verb+"d", "strategy", &id, nil)
	respondJSON(w, http.StatusOK, strategy)
}

func requirePaperStrategy(strategy *domain.Strategy, action string) error {
	if strategy.IsPaper {
		return nil
	}
	return fmt.Errorf("%s is only allowed for paper strategies", action)
}

type paperStatusTransitioner interface {
	TransitionPaperStatus(ctx context.Context, id uuid.UUID, fromStatus, toStatus string) (*domain.Strategy, error)
}

type paperSkipNextMarker interface {
	MarkPaperSkipNext(ctx context.Context, id uuid.UUID) (*domain.Strategy, error)
}

func transitionPaperStatus(ctx context.Context, repo repository.StrategyRepository, id uuid.UUID, fromStatus, toStatus string, current *domain.Strategy) (*domain.Strategy, error) {
	if atomicRepo, ok := repo.(paperStatusTransitioner); ok {
		return atomicRepo.TransitionPaperStatus(ctx, id, fromStatus, toStatus)
	}
	updated := *current
	updated.Status = toStatus
	if err := repo.Update(ctx, &updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

func markPaperSkipNext(ctx context.Context, repo repository.StrategyRepository, id uuid.UUID, current *domain.Strategy) (*domain.Strategy, error) {
	if atomicRepo, ok := repo.(paperSkipNextMarker); ok {
		return atomicRepo.MarkPaperSkipNext(ctx, id)
	}
	updated := *current
	updated.SkipNextRun = true
	if err := repo.Update(ctx, &updated); err != nil {
		return nil, err
	}
	return &updated, nil
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
	if err := agent.ValidateStrategyConfig(cfg); err != nil {
		return err
	}
	if len(cfg.RulesEngine) > 0 {
		if _, err := rules.Parse(cfg.RulesEngine); err != nil {
			return fmt.Errorf("rules_engine: %w", err)
		}
	}

	var rawSections map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawSections); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	if optionsRaw := rawSections["options_rules"]; len(optionsRaw) > 0 {
		if _, err := rules.ParseOptions(optionsRaw); err != nil {
			return err
		}
	}

	return nil
}
