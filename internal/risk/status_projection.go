package risk

import (
	"fmt"
	"math"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

var cockpitMarketOrder = []domain.MarketType{
	domain.MarketTypeStock,
	domain.MarketTypeCrypto,
	domain.MarketTypeOptions,
	domain.MarketTypePolymarket,
	domain.MarketTypeKalshi,
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func isOpenExposureStatus(status domain.TradeDecisionStatus) bool {
	switch status {
	case domain.TradeDecisionStatusPaper, domain.TradeDecisionStatusLive:
		return true
	default:
		return false
	}
}

func cockpitWarningForCircuitBreaker(status CircuitBreakerStatus) string {
	switch status.State {
	case CircuitBreakerPhaseTripped:
		if status.Reason != "" {
			return "circuit breaker tripped: " + status.Reason
		}
		return "circuit breaker tripped"
	case CircuitBreakerPhaseCooldown:
		return "circuit breaker cooling down"
	case CircuitBreakerPhaseOpen, "":
		return ""
	default:
		return "circuit breaker active: " + status.State.String()
	}
}

func cockpitExposureByMarket(decisions []domain.TradeDecision) map[domain.MarketType]*CockpitExposure {
	byMarket := make(map[domain.MarketType]*CockpitExposure, len(cockpitMarketOrder))
	for _, marketType := range cockpitMarketOrder {
		byMarket[marketType] = &CockpitExposure{MarketType: marketType}
	}
	for _, decision := range decisions {
		bucket, ok := byMarket[decision.MarketType]
		if !ok {
			continue
		}
		switch decision.RiskStatus {
		case domain.RiskDecisionApproved:
			bucket.ApprovedDecisions++
			approvedSize := math.Abs(decision.ApprovedSize)
			if isOpenExposureStatus(decision.Status) && isFinite(decision.ApprovedSize) && approvedSize > 0 {
				bucket.OpenPositions++
				bucket.GrossExposure += approvedSize
				if isFinite(decision.NetEV) {
					bucket.NetExpectedValue += decision.NetEV
				}
			}
		case domain.RiskDecisionRejected:
			bucket.RejectedDecisions++
		}
	}
	return byMarket
}

func projectEngineStatus(now time.Time, limits PositionLimits, cb CircuitBreakerStatus, ks KillSwitchStatus, marketKillSwitches map[domain.MarketType]KillSwitchStatus, portfolio *Portfolio) EngineStatus {
	if portfolio != nil {
		openPositions := portfolio.ConcurrentPositions
		totalExposure := portfolio.TotalExposurePct
		limits.CurrentOpenPositions = &openPositions
		limits.CurrentTotalExposurePct = &totalExposure
	}

	status := domain.RiskStatusNormal
	if cb.State == CircuitBreakerPhaseTripped {
		status = domain.RiskStatusBreached
	} else if ks.Active {
		status = domain.RiskStatusWarning
	}

	return EngineStatus{
		RiskStatus:         status,
		CircuitBreaker:     cb,
		KillSwitch:         ks,
		MarketKillSwitches: marketKillSwitches,
		PositionLimits:     limits,
		UpdatedAt:          now,
	}
}

func buildCockpitSummary(decisions []domain.TradeDecision, status *EngineStatus, generatedAt time.Time) CockpitSummary {
	byMarket := cockpitExposureByMarket(decisions)
	result := CockpitSummary{
		GeneratedAt: generatedAt,
		Exposures:   make([]CockpitExposure, 0, len(cockpitMarketOrder)),
		Warnings:    make([]string, 0, 8),
	}

	if len(decisions) == 0 {
		result.Warnings = append(result.Warnings, "no trade decisions available")
	}

	if status != nil {
		result.KillSwitchActive = status.KillSwitch.Active
		result.CircuitBreaker = status.CircuitBreaker.State != "" && status.CircuitBreaker.State != CircuitBreakerPhaseOpen
		if status.KillSwitch.Active {
			result.Warnings = append(result.Warnings, "kill switch active")
		}
		if warning := cockpitWarningForCircuitBreaker(status.CircuitBreaker); warning != "" {
			result.Warnings = append(result.Warnings, warning)
		}
	}

	for _, marketType := range cockpitMarketOrder {
		exposure := *byMarket[marketType]
		result.Exposures = append(result.Exposures, exposure)
		if exposure.ApprovedDecisions == 0 && exposure.RejectedDecisions > 0 {
			result.Warnings = append(result.Warnings, "market "+string(marketType)+" has rejected decisions but no approved exposure")
		}
	}

	return result
}

// BuildCockpitSummary aggregates trade decisions and risk status into a
// deterministic cockpit snapshot suitable for API responses and tests.
func BuildCockpitSummary(decisions []domain.TradeDecision, status *EngineStatus, generatedAt time.Time) CockpitSummary {
	return buildCockpitSummary(decisions, status, generatedAt)
}

// BuildCockpitSummaryWithPositions uses durable open positions as the source
// of truth for exposure while retaining decision counts as separate context.
func BuildCockpitSummaryWithPositions(decisions []domain.TradeDecision, positions []domain.Position, status *EngineStatus, generatedAt time.Time) CockpitSummary {
	result := buildCockpitSummary(decisions, status, generatedAt)
	byMarket := make(map[domain.MarketType]*CockpitExposure, len(result.Exposures))
	for i := range result.Exposures {
		exposure := &result.Exposures[i]
		exposure.OpenPositions = 0
		exposure.MarkedPositions = 0
		exposure.UnmarkedPositions = 0
		exposure.GrossExposure = 0
		exposure.GrossMarkedValue = nil
		exposure.NetExpectedValue = 0
		byMarket[exposure.MarketType] = exposure
	}

	markedByMarket := make(map[domain.MarketType]float64, len(result.Exposures))
	for _, position := range positions {
		exposure, ok := byMarket[position.MarketType]
		if !ok {
			result.ReconciliationStatus = "incomplete"
			result.Warnings = append(result.Warnings, "open position market "+string(position.MarketType)+" is absent from risk aggregation")
			continue
		}
		multiplier := position.ContractMultiplier
		if multiplier == 0 {
			multiplier = 1
		}
		exposure.OpenPositions++
		result.OpenPositions++
		costBasis := math.Abs(position.Quantity * position.AvgEntry * multiplier)
		exposure.GrossExposure += costBasis
		result.GrossCostBasis += costBasis
		if position.CurrentPrice == nil || position.UnrealizedPnL == nil {
			exposure.UnmarkedPositions++
			result.UnmarkedPositions++
			continue
		}
		exposure.MarkedPositions++
		result.MarkedPositions++
		markedByMarket[position.MarketType] += math.Abs(position.Quantity * *position.CurrentPrice * multiplier)
	}
	for marketType, markedValue := range markedByMarket {
		value := markedValue
		byMarket[marketType].GrossMarkedValue = &value
	}

	if result.ReconciliationStatus == "" {
		result.ReconciliationStatus = "complete"
	}
	result.ValuationStatus = "complete"
	if result.UnmarkedPositions > 0 {
		result.ValuationStatus = "partial"
		if result.MarkedPositions == 0 {
			result.ValuationStatus = "unavailable"
		}
		result.Warnings = append(result.Warnings, fmt.Sprintf("valuation incomplete: %d of %d open positions are marked", result.MarkedPositions, result.OpenPositions))
	}
	return result
}
