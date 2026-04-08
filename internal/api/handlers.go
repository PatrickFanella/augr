package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/agent/conversation"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const (
	defaultLimit = 50
	maxLimit     = 100
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// parsePagination extracts limit/offset query params with sane defaults.
func parsePagination(r *http.Request) (limit, offset int) {
	limit = defaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "username and password are required", ErrCodeValidation)
		return
	}

	user, err := s.users.GetByUsername(r.Context(), req.Username)
	if err != nil {
		if isNotFound(err) {
			verifyPasswordAgainstDummyHash(req.Password)
			respondError(w, http.StatusUnauthorized, "invalid username or password", ErrCodeUnauthorized)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to authenticate user", ErrCodeInternal)
		return
	}

	if err := verifyPassword(user.PasswordHash, req.Password); err != nil {
		respondError(w, http.StatusUnauthorized, "invalid username or password", ErrCodeUnauthorized)
		return
	}

	tokenPair, err := s.auth.GenerateTokenPair(user.Username)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate auth tokens", ErrCodeInternal)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	respondJSON(w, http.StatusOK, LoginResponse{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresAt:    tokenPair.ExpiresAt.UTC(),
	})
}

// parseUUID extracts a UUID from a chi URL parameter.
func parseUUID(r *http.Request, param string) (uuid.UUID, error) {
	raw := chi.URLParam(r, param)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, errors.New("invalid id: " + raw)
	}
	return id, nil
}

// --- Strategy handlers ---

func (s *Server) handleListStrategies(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	filter := repository.StrategyFilter{
		Ticker:     q.Get("ticker"),
		MarketType: domain.MarketType(q.Get("market_type")),
		Status:     q.Get("status"),
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
		s.logger.Warn("count strategies", slog.String("error", err.Error()))
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

// --- Pipeline run handlers ---

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	filter := repository.PipelineRunFilter{
		Ticker: q.Get("ticker"),
		Status: domain.PipelineStatus(q.Get("status")),
	}
	if v := q.Get("strategy_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.StrategyID = &id
		}
	}
	if startDateStr := q.Get("start_date"); startDateStr != "" {
		startDate, err := time.Parse(time.RFC3339Nano, startDateStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid start_date", ErrCodeBadRequest)
			return
		}
		filter.StartedAfter = &startDate
	}
	if endDateStr := q.Get("end_date"); endDateStr != "" {
		endDate, err := time.Parse(time.RFC3339Nano, endDateStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid end_date", ErrCodeBadRequest)
			return
		}
		filter.StartedBefore = &endDate
	}

	runs, err := s.runs.List(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list runs", ErrCodeInternal)
		return
	}
	total, err := s.runs.Count(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count pipeline runs", slog.String("error", err.Error()))
	}
	respondListWithTotal(w, runs, total, limit, offset)
}

// findRunByID looks up a pipeline run by ID. The PipelineRunRepository.Get
// method requires a tradeDate which is not available from the URL, so we
// list runs in pages and scan for a match.
func (s *Server) findRunByID(ctx context.Context, id uuid.UUID) (*domain.PipelineRun, error) {
	offset := 0
	for {
		runs, err := s.runs.List(ctx, repository.PipelineRunFilter{}, maxLimit, offset)
		if err != nil {
			return nil, err
		}
		if len(runs) == 0 {
			return nil, nil
		}
		for i := range runs {
			if runs[i].ID == id {
				return &runs[i], nil
			}
		}
		if len(runs) < maxLimit {
			return nil, nil
		}
		offset += maxLimit
	}
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	run, err := s.findRunByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get run", ErrCodeInternal)
		return
	}
	if run == nil {
		respondError(w, http.StatusNotFound, "run not found", ErrCodeNotFound)
		return
	}
	respondJSON(w, http.StatusOK, run)
}

func (s *Server) handleGetRunDecisions(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	limit, offset := parsePagination(r)
	includePrompt := r.URL.Query().Get("include_prompt") == "true"

	decisions, err := s.decisions.GetByRun(r.Context(), id, repository.AgentDecisionFilter{}, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get decisions", ErrCodeInternal)
		return
	}

	type decisionResponse struct {
		domain.AgentDecision
		PromptText string `json:"prompt_text,omitempty"`
	}

	responses := make([]decisionResponse, len(decisions))
	for i, d := range decisions {
		resp := decisionResponse{AgentDecision: d}
		if includePrompt {
			resp.PromptText = d.PromptText
		}
		responses[i] = resp
	}
	respondList(w, responses, limit, offset)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	run, err := s.findRunByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to cancel run", ErrCodeInternal)
		return
	}
	if run == nil {
		respondError(w, http.StatusNotFound, "run not found", ErrCodeNotFound)
		return
	}
	if !run.Status.CanTransitionTo(domain.PipelineStatusCancelled) {
		respondError(w, http.StatusBadRequest, "run cannot be cancelled in its current state", ErrCodeBadRequest)
		return
	}
	update := repository.PipelineRunStatusUpdate{
		Status: domain.PipelineStatusCancelled,
	}
	if err := s.runs.UpdateStatus(r.Context(), id, run.TradeDate, update); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to cancel run", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleGetRunSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.snapshots == nil {
		respondError(w, http.StatusNotImplemented, "snapshots not configured", ErrCodeNotImplemented)
		return
	}
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}

	snapshots, err := s.snapshots.GetByRun(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "run not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get snapshot", ErrCodeInternal)
		return
	}

	grouped := make(map[string]json.RawMessage)
	for _, snap := range snapshots {
		grouped[snap.DataType] = snap.Payload
	}

	respondJSON(w, http.StatusOK, grouped)
}

// --- Portfolio handlers ---

func (s *Server) handleListPositions(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	filter := repository.PositionFilter{
		Ticker: q.Get("ticker"),
		Side:   domain.PositionSide(q.Get("side")),
	}

	positions, err := s.positions.List(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list positions", ErrCodeInternal)
		return
	}
	total, err := s.positions.Count(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count positions", slog.String("error", err.Error()))
	}
	respondListWithTotal(w, positions, total, limit, offset)
}

func (s *Server) handleGetOpenPositions(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	positions, err := s.positions.GetOpen(r.Context(), repository.PositionFilter{}, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list open positions", ErrCodeInternal)
		return
	}
	total, err := s.positions.CountOpen(r.Context(), repository.PositionFilter{})
	if err != nil {
		s.logger.Warn("count open positions", slog.String("error", err.Error()))
	}
	respondListWithTotal(w, positions, total, limit, offset)
}

func (s *Server) handlePortfolioSummary(w http.ResponseWriter, r *http.Request) {
	// Build a portfolio summary from open positions.
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

// --- Order handlers ---

func (s *Server) handleListOrders(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	filter := repository.OrderFilter{
		Ticker: q.Get("ticker"),
		Status: domain.OrderStatus(q.Get("status")),
		Side:   domain.OrderSide(q.Get("side")),
	}

	orders, err := s.orders.List(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list orders", ErrCodeInternal)
		return
	}
	total, err := s.orders.Count(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count orders", slog.String("error", err.Error()))
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

	// Also fetch fills (trades) for this order.
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

// --- Trade handlers ---

func (s *Server) handleListTrades(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	filter := repository.TradeFilter{}

	orderIDStr := q.Get("order_id")
	if orderIDStr != "" {
		orderID, err := uuid.Parse(orderIDStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid order_id", ErrCodeBadRequest)
			return
		}
		filter.OrderID = &orderID
	}

	positionIDStr := q.Get("position_id")
	if positionIDStr != "" {
		positionID, err := uuid.Parse(positionIDStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid position_id", ErrCodeBadRequest)
			return
		}
		filter.PositionID = &positionID
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
	if startDateStr := q.Get("start_date"); startDateStr != "" {
		startDate, err := time.Parse(time.RFC3339, startDateStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid start_date", ErrCodeBadRequest)
			return
		}
		filter.StartDate = &startDate
	}
	if endDateStr := q.Get("end_date"); endDateStr != "" {
		endDate, err := time.Parse(time.RFC3339, endDateStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid end_date", ErrCodeBadRequest)
			return
		}
		filter.EndDate = &endDate
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
		s.logger.Warn("count trades", slog.String("error", err.Error()))
	}
	respondListWithTotal(w, trades, total, limit, offset)
}

// --- Memory handlers ---

func (s *Server) handleListMemories(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	query := q.Get("q")
	filter := repository.MemorySearchFilter{
		AgentRole: domain.AgentRole(q.Get("agent_role")),
	}

	memories, err := s.memories.Search(r.Context(), query, filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to search memories", ErrCodeInternal)
		return
	}
	respondList(w, memories, limit, offset)
}

func (s *Server) handleSearchMemories(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	if body.Query == "" {
		respondError(w, http.StatusBadRequest, "query is required", ErrCodeValidation)
		return
	}
	limit, offset := parsePagination(r)
	memories, err := s.memories.Search(r.Context(), body.Query, repository.MemorySearchFilter{}, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to search memories", ErrCodeInternal)
		return
	}
	respondList(w, memories, limit, offset)
}

func (s *Server) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	if err := s.memories.Delete(r.Context(), id); err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "memory not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to delete memory", ErrCodeInternal)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Risk handlers ---

func (s *Server) handleRiskStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.risk.GetStatus(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get risk status", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, status)
}

func (s *Server) handleKillSwitchToggle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Active bool   `json:"active"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}

	if body.Active {
		if body.Reason == "" {
			respondError(w, http.StatusBadRequest, "reason is required when activating kill switch", ErrCodeValidation)
			return
		}
		if err := s.risk.ActivateKillSwitch(r.Context(), body.Reason); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to activate kill switch", ErrCodeInternal)
			return
		}
		s.writeAuditLog(r.Context(), actorOf(r), "kill_switch.activated", "system", nil,
			map[string]string{"reason": body.Reason})
	} else {
		if err := s.risk.DeactivateKillSwitch(r.Context()); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to deactivate kill switch", ErrCodeInternal)
			return
		}
		s.writeAuditLog(r.Context(), actorOf(r), "kill_switch.deactivated", "system", nil, nil)
	}
	respondJSON(w, http.StatusOK, map[string]bool{"active": body.Active})
}

func (s *Server) handleMarketKillSwitch(w http.ResponseWriter, r *http.Request) {
	marketType := domain.MarketType(chi.URLParam(r, "type"))
	if marketType == "" {
		respondError(w, http.StatusBadRequest, "market type is required", ErrCodeBadRequest)
		return
	}

	switch r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:] {
	case "stop":
		var body struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
			return
		}
		if body.Reason == "" {
			respondError(w, http.StatusBadRequest, "reason is required", ErrCodeValidation)
			return
		}
		if err := s.risk.ActivateMarketKillSwitch(r.Context(), marketType, body.Reason); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to activate market kill switch", ErrCodeInternal)
			return
		}
		s.writeAuditLog(r.Context(), actorOf(r), "market_kill_switch.activated", "market", nil,
			map[string]string{"market_type": string(marketType), "reason": body.Reason})
		respondJSON(w, http.StatusOK, map[string]any{"market_type": marketType, "active": true})
	case "resume":
		if err := s.risk.DeactivateMarketKillSwitch(r.Context(), marketType); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to deactivate market kill switch", ErrCodeInternal)
			return
		}
		s.writeAuditLog(r.Context(), actorOf(r), "market_kill_switch.deactivated", "market", nil,
			map[string]string{"market_type": string(marketType)})
		respondJSON(w, http.StatusOK, map[string]any{"market_type": marketType, "active": false})
	default:
		respondError(w, http.StatusNotFound, "unknown action", ErrCodeNotFound)
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.settings.Get(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get settings", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, settings)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body SettingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}

	settings, err := s.settings.Update(r.Context(), body)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeValidation)
		return
	}
	s.writeAuditLog(r.Context(), actorOf(r), "settings.updated", "settings", nil, nil)
	respondJSON(w, http.StatusOK, settings)
}

// isNotFound checks whether err wraps repository.ErrNotFound.
func isNotFound(err error) bool {
	return errors.Is(err, repository.ErrNotFound)
}

// isUniqueConstraintViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505). Works with pgconn.PgError wrapped by pgx.
func isUniqueConstraintViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

// --- Auth: Refresh token (#417) ---

// handleRegister creates a new user account and returns a token pair.
// The first registered user effectively becomes the local admin; subsequent
// registrations are open unless the caller chooses to gate the route.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "username and password are required", ErrCodeValidation)
		return
	}

	user := &domain.User{
		Username: req.Username,
		Password: req.Password,
	}
	if err := s.users.Create(r.Context(), user); err != nil {
		if isUniqueConstraintViolation(err) {
			respondError(w, http.StatusConflict, "username already taken", ErrCodeConflict)
			return
		}
		s.logger.Error("register: create user", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to create user", ErrCodeInternal)
		return
	}
	s.writeAuditLog(r.Context(), user.Username, "user.registered", "user", &user.ID, nil)

	tokenPair, err := s.auth.GenerateTokenPair(user.Username)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate auth tokens", ErrCodeInternal)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	respondJSON(w, http.StatusCreated, LoginResponse{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresAt:    tokenPair.ExpiresAt.UTC(),
	})
}

func (s *Server) handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	if strings.TrimSpace(body.RefreshToken) == "" {
		respondError(w, http.StatusBadRequest, "refresh_token is required", ErrCodeValidation)
		return
	}

	tokenPair, err := s.auth.RefreshTokenPair(body.RefreshToken)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "invalid or expired refresh token", ErrCodeUnauthorized)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	respondJSON(w, http.StatusOK, LoginResponse{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresAt:    tokenPair.ExpiresAt.UTC(),
	})
}

// --- Strategy lifecycle (#438) ---

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

// --- Events (#462) ---

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	if s.events == nil {
		respondError(w, http.StatusNotImplemented, "events not configured", ErrCodeNotImplemented)
		return
	}
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	filter := repository.AgentEventFilter{
		EventKind: q.Get("event_kind"),
	}
	if v := q.Get("pipeline_run_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.PipelineRunID = &id
		}
	}
	if v := q.Get("strategy_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.StrategyID = &id
		}
	}
	if v := q.Get("agent_role"); v != "" {
		filter.AgentRole = domain.AgentRole(v)
	}
	if v := q.Get("after"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			http.Error(w, "invalid 'after' query parameter: must be RFC3339/RFC3339Nano", http.StatusBadRequest)
			return
		}
		filter.CreatedAfter = &t
	}
	if v := q.Get("before"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			http.Error(w, "invalid 'before' query parameter: must be RFC3339/RFC3339Nano", http.StatusBadRequest)
			return
		}
		filter.CreatedBefore = &t
	}

	events, err := s.events.List(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list events", ErrCodeInternal)
		return
	}
	total, err := s.events.Count(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count events", slog.String("error", err.Error()))
	}
	respondListWithTotal(w, events, total, limit, offset)
}

// --- Conversations (#454, #445) ---

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	if s.conversations == nil {
		respondError(w, http.StatusNotImplemented, "conversations not configured", ErrCodeNotImplemented)
		return
	}
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	filter := repository.ConversationFilter{
		AgentRole: domain.AgentRole(q.Get("agent_role")),
	}
	if v := q.Get("pipeline_run_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.PipelineRunID = &id
		}
	}

	conversations, err := s.conversations.ListConversations(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list conversations", ErrCodeInternal)
		return
	}
	total, err := s.conversations.CountConversations(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count conversations", slog.String("error", err.Error()))
	}
	respondListWithTotal(w, conversations, total, limit, offset)
}

func (s *Server) handleGetConversationMessages(w http.ResponseWriter, r *http.Request) {
	if s.conversations == nil {
		respondError(w, http.StatusNotImplemented, "conversations not configured", ErrCodeNotImplemented)
		return
	}
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}

	conv, err := s.conversations.GetConversation(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "conversation not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get conversation", ErrCodeInternal)
		return
	}

	limit, offset := parsePagination(r)
	messages, err := s.conversations.GetMessages(r.Context(), id, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get messages", ErrCodeInternal)
		return
	}

	// Inject agent pipeline decisions as synthetic assistant messages at the
	// start of the conversation so the agent's analysis appears as if they
	// were a participant in the chat.
	if offset == 0 && s.decisions != nil {
		decisions, decErr := s.decisions.GetByRun(r.Context(), conv.PipelineRunID, repository.AgentDecisionFilter{
			AgentRole: conv.AgentRole,
		}, 20, 0)
		if decErr == nil && len(decisions) > 0 {
			synthetic := make([]domain.ConversationMessage, 0, len(decisions)+len(messages))
			for _, dec := range decisions {
				content := dec.OutputText
				if dec.Phase != "" {
					content = fmt.Sprintf("[%s] %s", dec.Phase, content)
				}
				synthetic = append(synthetic, domain.ConversationMessage{
					ID:             dec.ID,
					ConversationID: id,
					Role:           domain.ConversationMessageRoleAssistant,
					Content:        content,
					CreatedAt:      dec.CreatedAt,
				})
			}
			synthetic = append(synthetic, messages...)
			messages = synthetic
		}
	}

	respondList(w, messages, limit, offset)
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	if s.conversations == nil {
		respondError(w, http.StatusNotImplemented, "conversations not configured", ErrCodeNotImplemented)
		return
	}
	var body struct {
		PipelineRunID uuid.UUID        `json:"pipeline_run_id"`
		AgentRole     domain.AgentRole `json:"agent_role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	if body.PipelineRunID == uuid.Nil {
		respondError(w, http.StatusBadRequest, "pipeline_run_id is required", ErrCodeValidation)
		return
	}
	if body.AgentRole == "" {
		respondError(w, http.StatusBadRequest, "agent_role is required", ErrCodeValidation)
		return
	}

	// Verify pipeline run exists.
	run, err := s.findRunByID(r.Context(), body.PipelineRunID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to verify pipeline run", ErrCodeInternal)
		return
	}
	if run == nil {
		respondError(w, http.StatusBadRequest, "pipeline_run_id does not reference an existing run", ErrCodeValidation)
		return
	}

	// Auto-generate title.
	roleLabel := strings.ReplaceAll(string(body.AgentRole), "_", " ")
	title := fmt.Sprintf("Chat with %s \u2014 %s", titleCase(roleLabel), run.Ticker)

	conv := &domain.Conversation{
		PipelineRunID: body.PipelineRunID,
		AgentRole:     body.AgentRole,
		Title:         title,
	}
	if err := s.conversations.CreateConversation(r.Context(), conv); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create conversation", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusCreated, conv)
}

func (s *Server) handleCreateConversationMessage(w http.ResponseWriter, r *http.Request) {
	if s.conversations == nil {
		respondError(w, http.StatusNotImplemented, "conversations not configured", ErrCodeNotImplemented)
		return
	}
	convID, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}

	conv, err := s.conversations.GetConversation(r.Context(), convID)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "conversation not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get conversation", ErrCodeInternal)
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		respondError(w, http.StatusBadRequest, "content is required", ErrCodeValidation)
		return
	}

	// Save the user message.
	userMsg := &domain.ConversationMessage{
		ConversationID: convID,
		Role:           domain.ConversationMessageRoleUser,
		Content:        body.Content,
	}
	if err := s.conversations.AddMessage(r.Context(), convID, userMsg); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save message", ErrCodeInternal)
		return
	}

	if s.llmProvider == nil {
		respondError(w, http.StatusNotImplemented, "LLM provider not configured", ErrCodeNotImplemented)
		return
	}

	// Fetch conversation history for context.
	history, err := s.conversations.GetMessages(r.Context(), convID, 100, 0)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load history", ErrCodeInternal)
		return
	}

	// Build LLM context using ContextBuilder if dependencies are available.
	var llmMessages []llm.Message
	var systemPrompt string

	if s.snapshots != nil && s.decisions != nil && s.memories != nil {
		cb := conversation.NewContextBuilder(s.decisions, s.snapshots, s.memories, 0)
		builtCtx, buildErr := cb.BuildContext(r.Context(), conversation.ContextInput{
			RunID:               conv.PipelineRunID,
			AgentRole:           conv.AgentRole,
			ConversationHistory: history,
		})
		if buildErr != nil {
			s.logger.Warn("failed to build conversation context, using simple prompt", "error", buildErr)
			systemPrompt = fmt.Sprintf("You are a %s trading agent. Answer questions about your decisions.", conv.AgentRole)
			llmMessages = historyToLLMMessages(history)
		} else {
			systemPrompt = builtCtx.SystemPrompt
			llmMessages = builtCtx.Messages
		}
	} else {
		systemPrompt = fmt.Sprintf("You are a %s trading agent. Answer questions about your decisions.", conv.AgentRole)
		llmMessages = historyToLLMMessages(history)
	}

	messages := append([]llm.Message{{Role: "system", Content: systemPrompt}}, llmMessages...)

	resp, err := s.llmProvider.Complete(r.Context(), llm.CompletionRequest{
		Messages: messages,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "LLM completion failed", ErrCodeInternal)
		return
	}

	assistantMsg := &domain.ConversationMessage{
		ConversationID: convID,
		Role:           domain.ConversationMessageRoleAssistant,
		Content:        resp.Content,
	}
	if err := s.conversations.AddMessage(r.Context(), convID, assistantMsg); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save response", ErrCodeInternal)
		return
	}

	respondJSON(w, http.StatusCreated, assistantMsg)
}

// historyToLLMMessages converts conversation history to LLM message format.
func historyToLLMMessages(history []domain.ConversationMessage) []llm.Message {
	msgs := make([]llm.Message, 0, len(history))
	for _, m := range history {
		msgs = append(msgs, llm.Message{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}
	return msgs
}

// --- Audit log (#455) ---

func (s *Server) handleListAuditLog(w http.ResponseWriter, r *http.Request) {
	if s.auditLog == nil {
		respondError(w, http.StatusNotImplemented, "audit log not configured", ErrCodeNotImplemented)
		return
	}
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	filter := repository.AuditLogFilter{
		EventType:  q.Get("event_type"),
		EntityType: q.Get("entity_type"),
		Actor:      q.Get("actor"),
	}
	if v := q.Get("entity_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid entity_id: must be a UUID", ErrCodeBadRequest)
			return
		}
		filter.EntityID = &id
	}
	if v := q.Get("after"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			http.Error(w, "invalid 'after' query parameter: must be RFC3339/RFC3339Nano", http.StatusBadRequest)
			return
		}
		filter.CreatedAfter = &t
	}
	if v := q.Get("before"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			http.Error(w, "invalid 'before' query parameter: must be RFC3339/RFC3339Nano", http.StatusBadRequest)
			return
		}
		filter.CreatedBefore = &t
	}

	entries, err := s.auditLog.Query(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to query audit log", ErrCodeInternal)
		return
	}
	total, err := s.auditLog.Count(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count audit log", slog.String("error", err.Error()))
	}
	respondListWithTotal(w, entries, total, limit, offset)
}

// handleGetCurrentUser returns the authenticated user's profile.
func (s *Server) handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "not authenticated", ErrCodeUnauthorized)
		return
	}

	user, err := s.users.GetByUsername(r.Context(), principal.Subject)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "user not found", ErrCodeNotFound)
			return
		}
		s.logger.Error("get current user", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to fetch user", ErrCodeInternal)
		return
	}

	respondJSON(w, http.StatusOK, user)
}

// handleUpdateMe changes the authenticated user's password.
func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "not authenticated", ErrCodeUnauthorized)
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	if strings.TrimSpace(req.CurrentPassword) == "" || strings.TrimSpace(req.NewPassword) == "" {
		respondError(w, http.StatusBadRequest, "current_password and new_password are required", ErrCodeValidation)
		return
	}
	if len(req.NewPassword) < 8 {
		respondError(w, http.StatusBadRequest, "new_password must be at least 8 characters", ErrCodeValidation)
		return
	}

	user, err := s.users.GetByUsername(r.Context(), principal.Subject)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "user not found", ErrCodeNotFound)
			return
		}
		s.logger.Error("update me: get user", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to fetch user", ErrCodeInternal)
		return
	}

	if err := verifyPassword(user.PasswordHash, req.CurrentPassword); err != nil {
		respondError(w, http.StatusUnauthorized, "current password is incorrect", ErrCodeUnauthorized)
		return
	}

	newHash, err := hashPassword(req.NewPassword)
	if err != nil {
		s.logger.Error("update me: hash password", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to update password", ErrCodeInternal)
		return
	}

	if err := s.users.UpdatePasswordHash(r.Context(), user.ID, newHash); err != nil {
		s.logger.Error("update me: update password hash", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to update password", ErrCodeInternal)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleListAPIKeys returns all API keys (metadata only, never raw key values).
func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	keys, err := s.auth.ListAPIKeys(r.Context(), limit, offset)
	if err != nil {
		s.logger.Error("list api keys", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to list api keys", ErrCodeInternal)
		return
	}
	respondList(w, keys, limit, offset)
}

// handleCreateAPIKey creates a new API key and returns the plaintext value once.
func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string     `json:"name"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required", ErrCodeValidation)
		return
	}

	plaintext, key, err := s.auth.CreateAPIKey(r.Context(), req.Name, req.ExpiresAt)
	if err != nil {
		s.logger.Error("create api key", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to create api key", ErrCodeInternal)
		return
	}
	s.writeAuditLog(r.Context(), actorOf(r), "api_key.created", "api_key", &key.ID,
		map[string]string{"name": key.Name})

	respondJSON(w, http.StatusCreated, struct {
		Key      string         `json:"key"`
		Metadata *domain.APIKey `json:"metadata"`
	}{Key: plaintext, Metadata: key})
}

// handleRevokeAPIKey marks an API key as revoked.
func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid api key id", ErrCodeBadRequest)
		return
	}

	if err := s.auth.RevokeAPIKey(r.Context(), id); err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "api key not found", ErrCodeNotFound)
			return
		}
		s.logger.Error("revoke api key", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to revoke api key", ErrCodeInternal)
		return
	}
	s.writeAuditLog(r.Context(), actorOf(r), "api_key.revoked", "api_key", &id, nil)

	w.WriteHeader(http.StatusNoContent)
}

// actorOf extracts the authenticated subject name from the request context.
// Returns an empty string when the request is unauthenticated.
func actorOf(r *http.Request) string {
	if p, ok := PrincipalFromContext(r.Context()); ok {
		return p.Subject
	}
	return ""
}

// writeAuditLog persists an audit log entry on a best-effort basis.
// Errors are logged but not propagated to avoid blocking the calling handler.
func (s *Server) writeAuditLog(ctx context.Context, actor, eventType, entityType string, entityID *uuid.UUID, details any) {
	if s.auditLog == nil {
		return
	}
	var raw json.RawMessage
	if details != nil {
		if b, err := json.Marshal(details); err == nil {
			raw = b
		}
	}
	entry := &domain.AuditLogEntry{
		ID:         uuid.New(),
		EventType:  eventType,
		EntityType: entityType,
		EntityID:   entityID,
		Actor:      actor,
		Details:    raw,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.auditLog.Create(ctx, entry); err != nil {
		s.logger.Warn("audit log write failed",
			slog.String("event_type", eventType),
			slog.String("error", err.Error()),
		)
	}
}

// titleCase capitalises the first letter of each whitespace-delimited word.
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
