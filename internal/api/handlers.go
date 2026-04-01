package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
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
	respondList(w, strategies, limit, offset)
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

	s.broadcastRunResult(result)
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
	respondList(w, runs, limit, offset)
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
	decisions, err := s.decisions.GetByRun(r.Context(), id, repository.AgentDecisionFilter{}, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get decisions", ErrCodeInternal)
		return
	}
	respondList(w, decisions, limit, offset)
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
	respondList(w, positions, limit, offset)
}

func (s *Server) handleGetOpenPositions(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	positions, err := s.positions.GetOpen(r.Context(), repository.PositionFilter{}, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list open positions", ErrCodeInternal)
		return
	}
	respondList(w, positions, limit, offset)
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
	respondList(w, orders, limit, offset)
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

	respondList(w, trades, limit, offset)
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
	} else {
		if err := s.risk.DeactivateKillSwitch(r.Context()); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to deactivate kill switch", ErrCodeInternal)
			return
		}
	}
	respondJSON(w, http.StatusOK, map[string]bool{"active": body.Active})
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
	respondJSON(w, http.StatusOK, settings)
}

// isNotFound checks whether err wraps repository.ErrNotFound.
func isNotFound(err error) bool {
	return errors.Is(err, repository.ErrNotFound)
}
