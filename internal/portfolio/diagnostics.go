package portfolio

import (
	"math"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type NoActionReason string

const (
	NoActionReasonHoldSignal     NoActionReason = "hold_signal"
	NoActionReasonRiskRejected   NoActionReason = "risk_rejected"
	NoActionReasonSizingZero     NoActionReason = "sizing_zero"
	NoActionReasonSellWithoutPos NoActionReason = "sell_without_position"
	NoActionReasonKillSwitch     NoActionReason = "kill_switch"
	NoActionReasonLiveGateDenied NoActionReason = "live_gate_denied"
	NoActionReasonMissingData    NoActionReason = "missing_data"
	NoActionReasonUnknown        NoActionReason = "unknown"
)

type RunDiagnostic struct {
	Status     string
	Signal     string
	MarketType domain.MarketType
}

type DecisionDiagnostic struct {
	Status      string
	Signal      string
	RiskReasons []string
	Evidence    map[string]any
}

type DiagnosticsInput struct {
	StrategyRuns             []RunDiagnostic
	TradeDecisions           []DecisionDiagnostic
	TotalStrategyRuns        int
	TotalTradeDecisions      int
	TotalStrategies          int
	TotalOpenPositions       int
	SampleStrategyRuns       int
	SampleTradeDecisions     int
	SampleStrategies         int
	SampleOpenPositions      int
	RunCountsBySignal        map[string]int
	RunCountsByStatus        map[string]int
	DecisionCountsByStatus   map[string]int
	NoActionReasons          map[string]int
	ActiveStrategiesByMarket map[domain.MarketType]int
	OpenPositionsByMarket    map[domain.MarketType]int
	BuyingPower              float64
	Equity                   float64
	AccountBalanceAvailable  bool
	GrossExposure            float64
	TargetGrossExposurePct   float64
}

type DiagnosticsSummary struct {
	TotalStrategyRuns         int            `json:"total_strategy_runs"`
	TotalTradeDecisions       int            `json:"total_trade_decisions"`
	TotalStrategies           int            `json:"total_strategies"`
	TotalOpenPositions        int            `json:"total_open_positions"`
	SampleStrategyRuns        int            `json:"sample_strategy_runs"`
	SampleTradeDecisions      int            `json:"sample_trade_decisions"`
	SampleStrategies          int            `json:"sample_strategies"`
	SampleOpenPositions       int            `json:"sample_open_positions"`
	RunCountsBySignal         map[string]int `json:"run_counts_by_signal"`
	RunCountsByStatus         map[string]int `json:"run_counts_by_status"`
	DecisionCountsByStatus    map[string]int `json:"decision_counts_by_status"`
	NoActionReasons           map[string]int `json:"no_action_reasons"`
	ActiveStrategiesByMarket  map[string]int `json:"active_strategies_by_market"`
	OpenPositionsByMarket     map[string]int `json:"open_positions_by_market"`
	BuyingPowerUtilizationPct float64        `json:"buying_power_utilization_pct"`
	GrossExposurePct          float64        `json:"gross_exposure_pct"`
	TargetGrossExposurePct    float64        `json:"target_gross_exposure_pct"`
	UtilizationGapPct         float64        `json:"utilization_gap_pct"`
	Warnings                  []string       `json:"warnings"`
}

func BuildDiagnosticsSummary(input DiagnosticsInput) DiagnosticsSummary {
	summary := DiagnosticsSummary{
		TotalStrategyRuns:        input.TotalStrategyRuns,
		TotalTradeDecisions:      input.TotalTradeDecisions,
		TotalStrategies:          input.TotalStrategies,
		TotalOpenPositions:       input.TotalOpenPositions,
		SampleStrategyRuns:       input.SampleStrategyRuns,
		SampleTradeDecisions:     input.SampleTradeDecisions,
		SampleStrategies:         input.SampleStrategies,
		SampleOpenPositions:      input.SampleOpenPositions,
		RunCountsBySignal:        map[string]int{},
		RunCountsByStatus:        map[string]int{},
		DecisionCountsByStatus:   map[string]int{},
		NoActionReasons:          map[string]int{},
		ActiveStrategiesByMarket: map[string]int{},
		OpenPositionsByMarket:    map[string]int{},
		Warnings:                 []string{},
	}

	for k, v := range input.RunCountsBySignal {
		summary.RunCountsBySignal[normalizeToken(k)] = v
	}
	for k, v := range input.RunCountsByStatus {
		summary.RunCountsByStatus[normalizeToken(k)] = v
	}
	for k, v := range input.DecisionCountsByStatus {
		summary.DecisionCountsByStatus[normalizeToken(k)] = v
	}
	for k, v := range input.NoActionReasons {
		summary.NoActionReasons[normalizeToken(k)] = v
	}

	for market, count := range input.ActiveStrategiesByMarket {
		key := normalizeToken(market.String())
		if key == "" {
			key = string(NoActionReasonUnknown)
		}
		summary.ActiveStrategiesByMarket[key] = count
	}
	for market, count := range input.OpenPositionsByMarket {
		key := normalizeToken(market.String())
		if key == "" {
			key = string(NoActionReasonUnknown)
		}
		summary.OpenPositionsByMarket[key] = count
	}

	if input.Equity <= 0 {
		if !input.AccountBalanceAvailable {
			target := input.TargetGrossExposurePct
			if target <= 0 {
				target = 0.35
			}
			summary.TargetGrossExposurePct = target
			summary.UtilizationGapPct = target
			return summary
		}
		summary.Warnings = append(summary.Warnings, "equity_non_positive")
	} else {
		summary.BuyingPowerUtilizationPct = 1 - (input.BuyingPower / input.Equity)
		summary.GrossExposurePct = input.GrossExposure / input.Equity
	}

	target := input.TargetGrossExposurePct
	if target <= 0 {
		target = 0.35
	}
	summary.TargetGrossExposurePct = target
	if summary.GrossExposurePct < 0 {
		summary.GrossExposurePct = 0
	}
	if summary.BuyingPowerUtilizationPct < 0 {
		summary.BuyingPowerUtilizationPct = 0
	}
	summary.UtilizationGapPct = math.Max(target-summary.GrossExposurePct, 0)

	return summary
}

func normalizeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
