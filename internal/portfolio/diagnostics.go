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
	ActiveStrategiesByMarket map[domain.MarketType]int
	OpenPositionsByMarket    map[domain.MarketType]int
	BuyingPower              float64
	Equity                   float64
	GrossExposure            float64
	TargetGrossExposurePct   float64
}

type DiagnosticsSummary struct {
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
		RunCountsBySignal:        map[string]int{},
		RunCountsByStatus:        map[string]int{},
		DecisionCountsByStatus:   map[string]int{},
		NoActionReasons:          map[string]int{},
		ActiveStrategiesByMarket: map[string]int{},
		OpenPositionsByMarket:    map[string]int{},
		Warnings:                 []string{},
	}

	for _, run := range input.StrategyRuns {
		signal := normalizeToken(run.Signal)
		status := normalizeToken(run.Status)
		increment(summary.RunCountsBySignal, signal)
		increment(summary.RunCountsByStatus, status)
		if signal == string(domain.PipelineSignalHold) {
			incrementReason(summary.NoActionReasons, NoActionReasonHoldSignal)
		}
	}

	for _, decision := range input.TradeDecisions {
		status := normalizeToken(decision.Status)
		signal := normalizeToken(decision.Signal)
		increment(summary.DecisionCountsByStatus, status)

		reasons := classifyDecisionReasons(decision)
		if signal == string(domain.PipelineSignalHold) {
			reasons = append(reasons, NoActionReasonHoldSignal)
		}
		if len(reasons) == 0 {
			reasons = []NoActionReason{NoActionReasonUnknown}
		}
		seen := map[NoActionReason]struct{}{}
		for _, reason := range reasons {
			if _, ok := seen[reason]; ok {
				continue
			}
			seen[reason] = struct{}{}
			incrementReason(summary.NoActionReasons, reason)
		}
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

func classifyDecisionReasons(decision DecisionDiagnostic) []NoActionReason {
	seen := map[NoActionReason]struct{}{}
	add := func(reason NoActionReason) {
		seen[reason] = struct{}{}
	}

	for _, reason := range decision.RiskReasons {
		if classified, ok := classifyReasonText(reason); ok {
			add(classified)
		}
	}

	for key, value := range decision.Evidence {
		if classified, ok := classifyEvidence(key, value); ok {
			add(classified)
		}
	}

	if statusReason := classifyStatusReason(decision.Status); statusReason != "" {
		add(statusReason)
	}

	if len(seen) == 0 {
		return nil
	}

	out := make([]NoActionReason, 0, len(seen))
	for reason := range seen {
		out = append(out, reason)
	}
	return out
}

func classifyStatusReason(status string) NoActionReason {
	s := normalizeToken(status)
	switch s {
	case "rejected":
		return NoActionReasonRiskRejected
	case "hold", "held":
		return NoActionReasonHoldSignal
	case "paper_ordered", "live_ordered", "closed":
		return ""
	default:
		return ""
	}
}

func classifyReasonText(value string) (NoActionReason, bool) {
	v := normalizeToken(value)
	if v == "" {
		return "", false
	}
	switch {
	case v == "hold" || strings.Contains(v, "hold_signal"):
		return NoActionReasonHoldSignal, true
	case strings.Contains(v, "risk_rejected") || v == "risk" || strings.Contains(v, "undefined_risk_rejected"):
		return NoActionReasonRiskRejected, true
	case strings.Contains(v, "sizing_zero") || strings.Contains(v, "size_zero") || strings.Contains(v, "zero_size") || strings.Contains(v, "approved_size_zero") || strings.Contains(v, "size=0"):
		return NoActionReasonSizingZero, true
	case strings.Contains(v, "sell_without_position") || strings.Contains(v, "no_position") || strings.Contains(v, "sell_no_position") || strings.Contains(v, "flat"):
		return NoActionReasonSellWithoutPos, true
	case strings.Contains(v, "kill_switch") || strings.Contains(v, "killswitch"):
		return NoActionReasonKillSwitch, true
	case strings.Contains(v, "live_gate") || strings.Contains(v, "livegate") || strings.Contains(v, "live_disabled") || strings.Contains(v, "live_denied") || strings.Contains(v, "live_blocked"):
		return NoActionReasonLiveGateDenied, true
	case strings.Contains(v, "missing_data") || strings.Contains(v, "insufficient_data") || strings.Contains(v, "data_missing") || strings.Contains(v, "no_data"):
		return NoActionReasonMissingData, true
	default:
		return "", false
	}
}

func classifyEvidence(key string, value any) (NoActionReason, bool) {
	k := normalizeToken(key)
	if reason, ok := classifyReasonText(k); ok {
		return reason, true
	}

	switch v := value.(type) {
	case bool:
		if !v {
			switch {
			case strings.Contains(k, "kill_switch") || strings.Contains(k, "killswitch"):
				return NoActionReasonKillSwitch, true
			case strings.Contains(k, "live_gate") || strings.Contains(k, "livegate") || strings.Contains(k, "live"):
				return NoActionReasonLiveGateDenied, true
			case strings.Contains(k, "missing") || strings.Contains(k, "data"):
				return NoActionReasonMissingData, true
			}
		}
	case string:
		if reason, ok := classifyReasonText(v); ok {
			return reason, true
		}
	case float64:
		if v == 0 && strings.Contains(k, "size") {
			return NoActionReasonSizingZero, true
		}
		if v == 0 && strings.Contains(k, "position") && strings.Contains(k, "sell") {
			return NoActionReasonSellWithoutPos, true
		}
	case int:
		if v == 0 && strings.Contains(k, "size") {
			return NoActionReasonSizingZero, true
		}
		if v == 0 && strings.Contains(k, "position") && strings.Contains(k, "sell") {
			return NoActionReasonSellWithoutPos, true
		}
	}

	if value == nil {
		if strings.Contains(k, "missing") || strings.Contains(k, "data") {
			return NoActionReasonMissingData, true
		}
	}

	return "", false
}

func normalizeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func increment(m map[string]int, key string) {
	if key == "" {
		key = string(NoActionReasonUnknown)
	}
	m[key]++
}

func incrementReason(m map[string]int, key NoActionReason) {
	m[string(key)]++
}
