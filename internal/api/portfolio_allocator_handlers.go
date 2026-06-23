package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/portfolio"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type AccountBalanceSource interface {
	GetAccountBalance(ctx context.Context) (execution.Balance, error)
}

const (
	portfolioDiagnosticsRunsLimit           = 200
	portfolioDiagnosticsDecisionsLimit      = 200
	portfolioDiagnosticsPositionsLimit      = 200
	portfolioDiagnosticsStrategyLookupLimit = maxLimit
	portfolioDiagnosticsWarningRuns         = "pipeline_runs_unavailable"
	portfolioDiagnosticsWarningDecisions    = "trade_decisions_unavailable"
	portfolioDiagnosticsWarningStrategies   = "strategies_unavailable"
	portfolioDiagnosticsWarningPositions    = "positions_unavailable"
	portfolioDiagnosticsWarningUnknownOpen  = "open_positions_market_unknown"
	portfolioDiagnosticsWarningAccountBal   = "account_balance_unavailable"
	portfolioAllocatorRecentLimit           = 10
	portfolioAllocatorWarningOpportunities  = "opportunities_unavailable"
	portfolioAllocatorWarningDecisions      = "allocation_decisions_unavailable"
)

type portfolioAllocatorSummaryResponse struct {
	OpportunityCountsByStatus map[string]int              `json:"opportunity_counts_by_status"`
	RecentDecisions           []domain.AllocationDecision `json:"recent_decisions"`
	Warnings                  []string                    `json:"warnings,omitempty"`
}

func (s *Server) handleGetPortfolioAllocatorDiagnostics(w http.ResponseWriter, r *http.Request) {
	input, warnings, err := s.buildPortfolioDiagnosticsInput(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to build portfolio diagnostics", ErrCodeInternal)
		return
	}

	summary := portfolio.BuildDiagnosticsSummary(input)
	summary.Warnings = append(summary.Warnings, warnings...)
	respondJSON(w, http.StatusOK, summary)
}

func (s *Server) handleListPortfolioAllocatorOpportunities(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()
	filter := repository.OpportunityFilter{}
	if status := strings.TrimSpace(q.Get("status")); status != "" {
		filter.Status = domain.OpportunityStatus(status)
	}
	if marketType := strings.TrimSpace(q.Get("market_type")); marketType != "" {
		filter.MarketType = domain.MarketType(marketType)
	}
	if ticker := strings.TrimSpace(q.Get("ticker")); ticker != "" {
		filter.Ticker = ticker
	}
	if !ParseUUIDParam(w, q, "strategy_id", &filter.StrategyID) {
		return
	}
	if !ParseTimeParam(w, q, "expires_before", time.RFC3339, &filter.ExpiresBefore) {
		return
	}
	if !ParseTimeParam(w, q, "created_after", time.RFC3339, &filter.CreatedAfter) {
		return
	}

	if s.opportunities == nil {
		respondListWithTotal(w, []domain.Opportunity{}, 0, limit, offset)
		return
	}

	opps, err := s.opportunities.List(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list opportunities", ErrCodeInternal)
		return
	}
	total, err := s.opportunities.Count(r.Context(), filter)
	if err != nil {
		s.logger.Warn("portfolio allocator opportunities count", "error", err)
	}
	respondListWithTotal(w, opps, total, limit, offset)
}

func (s *Server) handleListPortfolioAllocatorDecisions(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()
	filter := repository.AllocationDecisionFilter{}
	if mode := strings.TrimSpace(q.Get("mode")); mode != "" {
		filter.Mode = domain.AllocationDecisionMode(mode)
	}
	if action := strings.TrimSpace(q.Get("action")); action != "" {
		filter.Action = domain.AllocationDecisionAction(action)
	}
	if !ParseUUIDParam(w, q, "strategy_id", &filter.StrategyID) {
		return
	}
	if !ParseUUIDParam(w, q, "opportunity_id", &filter.OpportunityID) {
		return
	}
	if !ParseTimeParam(w, q, "created_after", time.RFC3339, &filter.CreatedAfter) {
		return
	}

	if s.allocatorDecisions == nil {
		respondListWithTotal(w, []domain.AllocationDecision{}, 0, limit, offset)
		return
	}

	decisions, err := s.allocatorDecisions.List(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list allocation decisions", ErrCodeInternal)
		return
	}
	total, err := s.allocatorDecisions.Count(r.Context(), filter)
	if err != nil {
		s.logger.Warn("portfolio allocator decisions count", "error", err)
	}
	respondListWithTotal(w, decisions, total, limit, offset)
}

func (s *Server) handleGetPortfolioAllocatorSummary(w http.ResponseWriter, r *http.Request) {
	resp := portfolioAllocatorSummaryResponse{
		OpportunityCountsByStatus: map[string]int{},
		RecentDecisions:           []domain.AllocationDecision{},
	}
	warnings := make([]string, 0, 2)

	if s.opportunities == nil {
		warnings = append(warnings, portfolioAllocatorWarningOpportunities)
	} else {
		for _, status := range []domain.OpportunityStatus{
			domain.OpportunityStatusQueued,
			domain.OpportunityStatusSelected,
			domain.OpportunityStatusRejected,
			domain.OpportunityStatusExpired,
			domain.OpportunityStatusExecuted,
		} {
			count, err := s.opportunities.Count(r.Context(), repository.OpportunityFilter{Status: status})
			if err != nil {
				respondError(w, http.StatusInternalServerError, "failed to build portfolio allocator summary", ErrCodeInternal)
				return
			}
			resp.OpportunityCountsByStatus[status.String()] = count
		}
	}

	if s.allocatorDecisions == nil {
		warnings = append(warnings, portfolioAllocatorWarningDecisions)
	} else {
		decisions, err := s.allocatorDecisions.List(r.Context(), repository.AllocationDecisionFilter{Mode: domain.AllocationDecisionModeShadow}, portfolioAllocatorRecentLimit, 0)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to build portfolio allocator summary", ErrCodeInternal)
			return
		}
		resp.RecentDecisions = decisions
	}

	resp.Warnings = warnings
	respondJSON(w, http.StatusOK, resp)
}

func (s *Server) buildPortfolioDiagnosticsInput(ctx context.Context) (portfolio.DiagnosticsInput, []string, error) {
	input := portfolio.DiagnosticsInput{
		ActiveStrategiesByMarket: map[domain.MarketType]int{},
		OpenPositionsByMarket:    map[domain.MarketType]int{},
	}
	warnings := make([]string, 0, 4)

	if s.runs != nil {
		runs, err := s.runs.List(ctx, repository.PipelineRunFilter{}, portfolioDiagnosticsRunsLimit, 0)
		if err != nil {
			return input, warnings, fmt.Errorf("list pipeline runs: %w", err)
		}
		for _, run := range runs {
			input.StrategyRuns = append(input.StrategyRuns, portfolio.RunDiagnostic{
				Status:     string(run.Status),
				Signal:     string(run.Signal),
				MarketType: domain.MarketType(""),
			})
		}
	} else {
		warnings = append(warnings, portfolioDiagnosticsWarningRuns)
	}

	if s.tradeDecisions != nil {
		decisions, err := s.tradeDecisions.List(ctx, repository.TradeDecisionFilter{}, portfolioDiagnosticsDecisionsLimit, 0)
		if err != nil {
			return input, warnings, fmt.Errorf("list trade decisions: %w", err)
		}
		for _, decision := range decisions {
			input.TradeDecisions = append(input.TradeDecisions, portfolio.DecisionDiagnostic{
				Status:      string(decision.Status),
				Signal:      string(decision.Side),
				RiskReasons: append([]string(nil), decision.RiskReasons...),
				Evidence:    decodeDiagnosticEvidence(decision.Evidence),
			})
		}
	} else {
		warnings = append(warnings, portfolioDiagnosticsWarningDecisions)
	}

	var strategyMarkets map[uuid.UUID]domain.MarketType
	if s.strategies != nil {
		activeStrategies, err := s.strategies.List(ctx, repository.StrategyFilter{Status: domain.StrategyStatusActive}, portfolioDiagnosticsStrategyLookupLimit, 0)
		if err != nil {
			return input, warnings, fmt.Errorf("list active strategies: %w", err)
		}
		for _, strategy := range activeStrategies {
			input.ActiveStrategiesByMarket[strategy.MarketType]++
		}

		allStrategies, err := s.strategies.List(ctx, repository.StrategyFilter{}, portfolioDiagnosticsStrategyLookupLimit, 0)
		if err != nil {
			return input, warnings, fmt.Errorf("list strategies for lookup: %w", err)
		}
		strategyMarkets = make(map[uuid.UUID]domain.MarketType, len(allStrategies))
		for _, strategy := range allStrategies {
			strategyMarkets[strategy.ID] = strategy.MarketType
		}
	} else {
		warnings = append(warnings, portfolioDiagnosticsWarningStrategies)
	}

	var positions []domain.Position
	if s.positions != nil {
		var err error
		positions, err = s.positions.GetOpen(ctx, repository.PositionFilter{}, portfolioDiagnosticsPositionsLimit, 0)
		if err != nil {
			return input, warnings, fmt.Errorf("list open positions: %w", err)
		}
	} else {
		warnings = append(warnings, portfolioDiagnosticsWarningPositions)
	}

	unknownPositions := 0
	var grossExposure float64
	for _, position := range positions {
		grossExposure += positionExposure(position)

		if strategyMarkets != nil && position.StrategyID != nil {
			if market, ok := strategyMarkets[*position.StrategyID]; ok {
				input.OpenPositionsByMarket[market]++
				continue
			}
		}
		unknownPositions++
	}
	if unknownPositions > 0 {
		input.OpenPositionsByMarket[domain.MarketType("")] += unknownPositions
		warnings = append(warnings, portfolioDiagnosticsWarningUnknownOpen)
	}

	input.GrossExposure = grossExposure
	if s.accountBalance != nil {
		balance, err := s.accountBalance.GetAccountBalance(ctx)
		if err != nil {
			s.logger.Warn("portfolio allocator diagnostics account balance unavailable", "error", err)
			warnings = append(warnings, portfolioDiagnosticsWarningAccountBal)
		} else {
			input.BuyingPower = balance.BuyingPower
			input.Equity = balance.Equity
			input.AccountBalanceAvailable = true
		}
	} else {
		warnings = append(warnings, portfolioDiagnosticsWarningAccountBal)
	}

	return input, warnings, nil
}

func decodeDiagnosticEvidence(raw json.RawMessage) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func positionExposure(position domain.Position) float64 {
	price := position.CurrentPrice
	if price == nil || *price <= 0 {
		if position.AvgEntry > 0 {
			p := position.AvgEntry
			price = &p
		}
	}
	if price == nil || *price <= 0 || position.Quantity <= 0 {
		return 0
	}
	return position.Quantity * *price
}
