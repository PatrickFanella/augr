package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/portfolio"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

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
)

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
	input.Equity = grossExposure
	input.BuyingPower = 0
	warnings = append(warnings, portfolioDiagnosticsWarningAccountBal)

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
