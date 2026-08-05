package portfolio

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/google/uuid"
)

type AllocatorMode string

const (
	AllocatorModeDiagnostics AllocatorMode = "diagnostics"
	AllocatorModeQueue       AllocatorMode = "queue"
	AllocatorModeShadow      AllocatorMode = "shadow"
	AllocatorModePaper       AllocatorMode = "paper"
)

type AllocatorConfig struct {
	Mode                   AllocatorMode
	PaperOnly              bool
	TargetGrossExposurePct float64
	HardGrossExposurePct   float64
	CashReservePct         float64
	MaxNewOrdersPerRun     int
	MaxNewOrdersPerDay     int
	MaxPerPositionPct      map[domain.MarketType]float64
	MaxPerMarketPct        map[domain.MarketType]float64
	MinScoreByMarket       map[domain.MarketType]float64
	MinEdgePctByMarket     map[domain.MarketType]float64
	MinLiquidityUSD        map[domain.MarketType]float64
	MaxSpreadPct           map[domain.MarketType]float64
	Now                    func() time.Time
}

type PortfolioState struct {
	Equity         float64
	BuyingPower    float64
	GrossExposure  float64
	MarketExposure map[domain.MarketType]float64
	OpenTickers    map[string]bool
	NewOrdersToday int
}

type AllocationSummary struct {
	Evaluated        int            `json:"evaluated"`
	Eligible         int            `json:"eligible"`
	Selected         int            `json:"selected"`
	Rejected         int            `json:"rejected"`
	RejectedByReason map[string]int `json:"rejected_by_reason"`
	SelectedByMarket map[string]int `json:"selected_by_market"`
	SelectedNotional float64        `json:"selected_notional_usd"`
}

type AllocationResult struct {
	Decisions []domain.AllocationDecision `json:"decisions"`
	Summary   AllocationSummary           `json:"summary"`
}

type scoredOpportunity struct {
	opp      domain.Opportunity
	score    float64
	reasons  []string
	notional float64
	action   domain.AllocationDecisionAction
}

const (
	reasonNotQueued               = "not_queued"
	reasonExpired                 = "expired"
	reasonOptionsDisabled         = "options_disabled"
	reasonBelowMinScore           = "below_min_score"
	reasonBelowMinEdge            = "below_min_edge"
	reasonBelowMinLiquidity       = "below_min_liquidity"
	reasonAboveMaxSpread          = "above_max_spread"
	reasonDuplicateTicker         = "duplicate_ticker"
	reasonNearMarketCap           = "near_market_cap"
	reasonCashReservePressure     = "cash_reserve_pressure"
	reasonTargetExposureExceeded  = "target_gross_exposure_exceeded"
	reasonHardExposureExceeded    = "hard_gross_exposure_exceeded"
	reasonMarketExposureExceeded  = "market_exposure_exceeded"
	reasonCashReserveInsufficient = "cash_reserve_insufficient"
	reasonMaxOrdersPerRun         = "max_orders_per_run"
	reasonMaxOrdersPerDay         = "max_orders_per_day"
	reasonSizingZero              = "sizing_zero"
	reasonBudgetClamped           = "budget_clamped"
)

func DefaultAllocatorConfig() AllocatorConfig {
	return AllocatorConfig{
		Mode:                   AllocatorModeShadow,
		PaperOnly:              true,
		TargetGrossExposurePct: 0.35,
		HardGrossExposurePct:   0.50,
		CashReservePct:         0.20,
		MaxNewOrdersPerRun:     2,
		MaxNewOrdersPerDay:     5,
		MaxPerPositionPct: map[domain.MarketType]float64{
			domain.MarketTypeStock:      0.02,
			domain.MarketTypeCrypto:     0.01,
			domain.MarketTypeKalshi:     0.01,
			domain.MarketTypePolymarket: 0.01,
			domain.MarketTypeOptions:    0,
		},
		MaxPerMarketPct: map[domain.MarketType]float64{
			domain.MarketTypeStock:      0.50,
			domain.MarketTypeCrypto:     0.05,
			domain.MarketTypeKalshi:     0.10,
			domain.MarketTypePolymarket: 0.05,
			domain.MarketTypeOptions:    0,
		},
		MinScoreByMarket: map[domain.MarketType]float64{
			domain.MarketTypeStock:      65,
			domain.MarketTypeCrypto:     70,
			domain.MarketTypeKalshi:     70,
			domain.MarketTypePolymarket: 75,
			domain.MarketTypeOptions:    100,
		},
		MinEdgePctByMarket: map[domain.MarketType]float64{
			domain.MarketTypeStock:      0.015,
			domain.MarketTypeCrypto:     0.03,
			domain.MarketTypeKalshi:     0.04,
			domain.MarketTypePolymarket: 0.05,
			domain.MarketTypeOptions:    1,
		},
		MinLiquidityUSD: map[domain.MarketType]float64{
			domain.MarketTypeStock:      500000,
			domain.MarketTypeCrypto:     100000,
			domain.MarketTypeKalshi:     1000,
			domain.MarketTypePolymarket: 2500,
			domain.MarketTypeOptions:    math.Inf(1),
		},
		MaxSpreadPct: map[domain.MarketType]float64{
			domain.MarketTypeStock:      0.01,
			domain.MarketTypeCrypto:     0.02,
			domain.MarketTypeKalshi:     0.08,
			domain.MarketTypePolymarket: 0.08,
			domain.MarketTypeOptions:    0,
		},
		Now: time.Now,
	}
}

func AllocateShadow(opportunities []domain.Opportunity, state PortfolioState, cfg AllocatorConfig) AllocationResult {
	cfg = applyAllocatorDefaults(cfg)
	now := cfg.Now()

	result := AllocationResult{
		Summary: AllocationSummary{
			RejectedByReason: map[string]int{},
			SelectedByMarket: map[string]int{},
		},
	}

	eligible := make([]scoredOpportunity, 0, len(opportunities))
	decisions := make([]scoredOpportunity, 0, len(opportunities))

	for _, opp := range opportunities {
		result.Summary.Evaluated++

		if !strings.EqualFold(opp.Status.String(), domain.OpportunityStatusQueued.String()) {
			decisions = append(decisions, scoredOpportunity{opp: opp, score: -1, reasons: []string{reasonNotQueued}, action: domain.AllocationDecisionActionShadowRejected})
			incrementStringReason(result.Summary.RejectedByReason, reasonNotQueued)
			result.Summary.Rejected++
			continue
		}
		if isExpired(opp.ExpiresAt, now) {
			decisions = append(decisions, scoredOpportunity{opp: opp, score: -1, reasons: []string{reasonExpired}, action: domain.AllocationDecisionActionShadowRejected})
			incrementStringReason(result.Summary.RejectedByReason, reasonExpired)
			result.Summary.Rejected++
			continue
		}

		result.Summary.Eligible++
		score, reasons := scoreOpportunity(opp, state, cfg, now)
		if opp.MarketType == domain.MarketTypeOptions {
			reasons = append(reasons, reasonOptionsDisabled)
		}
		minScore := lookup(cfg.MinScoreByMarket, opp.MarketType)
		minEdge := lookup(cfg.MinEdgePctByMarket, opp.MarketType)
		minLiquidity := lookup(cfg.MinLiquidityUSD, opp.MarketType)
		maxSpread := lookup(cfg.MaxSpreadPct, opp.MarketType)

		if score < minScore {
			reasons = append(reasons, reasonBelowMinScore)
		}
		if opp.EdgePct < minEdge {
			reasons = append(reasons, reasonBelowMinEdge)
		}
		if opp.LiquidityUSD < minLiquidity {
			reasons = append(reasons, reasonBelowMinLiquidity)
		}
		if maxSpread > 0 && opp.SpreadPct > maxSpread {
			reasons = append(reasons, reasonAboveMaxSpread)
		}

		if len(reasons) > 0 {
			decisions = append(decisions, scoredOpportunity{opp: opp, score: score, reasons: uniqueStrings(reasons), action: domain.AllocationDecisionActionShadowRejected})
			result.Summary.Rejected++
			for _, reason := range uniqueStrings(reasons) {
				incrementStringReason(result.Summary.RejectedByReason, reason)
			}
			continue
		}

		eligible = append(eligible, scoredOpportunity{opp: opp, score: score})
	}

	sort.SliceStable(eligible, func(i, j int) bool {
		if eligible[i].score != eligible[j].score {
			return eligible[i].score > eligible[j].score
		}
		if eligible[i].opp.Ticker != eligible[j].opp.Ticker {
			return strings.ToLower(eligible[i].opp.Ticker) < strings.ToLower(eligible[j].opp.Ticker)
		}
		return eligible[i].opp.ID.String() < eligible[j].opp.ID.String()
	})

	selectedCount := 0
	selectedNotional := 0.0
	for _, item := range eligible {
		reasons := make([]string, 0, 4)
		if cfg.MaxNewOrdersPerRun > 0 && selectedCount >= cfg.MaxNewOrdersPerRun {
			reasons = append(reasons, reasonMaxOrdersPerRun)
		}
		if cfg.MaxNewOrdersPerDay > 0 && state.NewOrdersToday+selectedCount >= cfg.MaxNewOrdersPerDay {
			reasons = append(reasons, reasonMaxOrdersPerDay)
		}
		if len(reasons) == 0 {
			notional, sizingReasons := sizeOpportunity(item.opp, item.score, state, cfg)
			reasons = append(reasons, sizingReasons...)
			if notional > 0 {
				item.notional = notional
				item.action = domain.AllocationDecisionActionShadowSelected
				item.reasons = append([]string{fmt.Sprintf("score=%.1f", item.score), fmt.Sprintf("multiplier=%.2f", scoreMultiplier(item.score))}, reasons...)
				selectedCount++
				selectedNotional += notional
				result.Summary.Selected++
				result.Summary.SelectedNotional += notional
				incrementMarket(result.Summary.SelectedByMarket, item.opp.MarketType)
				decisions = append(decisions, item)
				continue
			}
			reasons = append(reasons, reasonSizingZero)
		}
		decisions = append(decisions, scoredOpportunity{opp: item.opp, score: item.score, reasons: uniqueStrings(reasons), action: domain.AllocationDecisionActionShadowRejected})
		result.Summary.Rejected++
		for _, reason := range uniqueStrings(reasons) {
			incrementStringReason(result.Summary.RejectedByReason, reason)
		}
	}

	sort.SliceStable(decisions, func(i, j int) bool {
		if decisions[i].score != decisions[j].score {
			return decisions[i].score > decisions[j].score
		}
		if decisions[i].action != decisions[j].action {
			return decisions[i].action == domain.AllocationDecisionActionShadowSelected
		}
		if decisions[i].opp.Ticker != decisions[j].opp.Ticker {
			return strings.ToLower(decisions[i].opp.Ticker) < strings.ToLower(decisions[j].opp.Ticker)
		}
		return decisions[i].opp.ID.String() < decisions[j].opp.ID.String()
	})

	result.Decisions = make([]domain.AllocationDecision, 0, len(decisions))
	for _, item := range decisions {
		result.Decisions = append(result.Decisions, toAllocationDecision(item))
	}
	result.Summary.SelectedNotional = selectedNotional
	return result
}

func applyAllocatorDefaults(cfg AllocatorConfig) AllocatorConfig {
	defaults := DefaultAllocatorConfig()
	if isZeroAllocatorConfig(cfg) {
		return defaults
	}

	if cfg.Mode == "" {
		cfg.Mode = defaults.Mode
	}
	if cfg.Now == nil {
		cfg.Now = defaults.Now
	}
	if cfg.TargetGrossExposurePct == 0 {
		cfg.TargetGrossExposurePct = defaults.TargetGrossExposurePct
	}
	if cfg.HardGrossExposurePct == 0 {
		cfg.HardGrossExposurePct = defaults.HardGrossExposurePct
	}
	if cfg.CashReservePct == 0 {
		cfg.CashReservePct = defaults.CashReservePct
	}
	if cfg.MaxNewOrdersPerRun == 0 {
		cfg.MaxNewOrdersPerRun = defaults.MaxNewOrdersPerRun
	}
	if cfg.MaxNewOrdersPerDay == 0 {
		cfg.MaxNewOrdersPerDay = defaults.MaxNewOrdersPerDay
	}
	cfg.MaxPerPositionPct = mergeMarketMap(cfg.MaxPerPositionPct, defaults.MaxPerPositionPct)
	cfg.MaxPerMarketPct = mergeMarketMap(cfg.MaxPerMarketPct, defaults.MaxPerMarketPct)
	cfg.MinScoreByMarket = mergeMarketMap(cfg.MinScoreByMarket, defaults.MinScoreByMarket)
	cfg.MinEdgePctByMarket = mergeMarketMap(cfg.MinEdgePctByMarket, defaults.MinEdgePctByMarket)
	cfg.MinLiquidityUSD = mergeMarketMap(cfg.MinLiquidityUSD, defaults.MinLiquidityUSD)
	cfg.MaxSpreadPct = mergeMarketMap(cfg.MaxSpreadPct, defaults.MaxSpreadPct)
	return cfg
}

func isZeroAllocatorConfig(cfg AllocatorConfig) bool {
	return cfg.Mode == "" && !cfg.PaperOnly && cfg.TargetGrossExposurePct == 0 && cfg.HardGrossExposurePct == 0 && cfg.CashReservePct == 0 && cfg.MaxNewOrdersPerRun == 0 && cfg.MaxNewOrdersPerDay == 0 && cfg.MaxPerPositionPct == nil && cfg.MaxPerMarketPct == nil && cfg.MinScoreByMarket == nil && cfg.MinEdgePctByMarket == nil && cfg.MinLiquidityUSD == nil && cfg.MaxSpreadPct == nil && cfg.Now == nil
}

func mergeMarketMap[T ~float64](provided, defaults map[domain.MarketType]T) map[domain.MarketType]T {
	out := make(map[domain.MarketType]T, len(defaults))
	for k, v := range defaults {
		out[k] = v
	}
	for k, v := range provided {
		out[k] = v
	}
	return out
}

func lookup(m map[domain.MarketType]float64, market domain.MarketType) float64 {
	if m == nil {
		return 0
	}
	if v, ok := m[market]; ok {
		return v
	}
	return 0
}

func isExpired(expiresAt, now time.Time) bool {
	if expiresAt.IsZero() {
		return false
	}
	return !now.Before(expiresAt)
}

func scoreOpportunity(opp domain.Opportunity, state PortfolioState, cfg AllocatorConfig, now time.Time) (float64, []string) {
	market := opp.MarketType
	edgeMin := lookup(cfg.MinEdgePctByMarket, market)
	liqMin := lookup(cfg.MinLiquidityUSD, market)
	spreadMax := lookup(cfg.MaxSpreadPct, market)
	marketCap := opp.MarketCapUSD
	marketExposure := lookupMarketExposure(state.MarketExposure, market)
	marketCapPct := lookup(cfg.MaxPerMarketPct, market)
	if marketCapPct < 0 {
		marketCapPct = 0
	}

	edgeScore := ratioScore(opp.EdgePct, edgeMin)
	confidenceScore := clamp01(opp.Confidence) * 100
	liquidityScore := 0.0
	if !math.IsInf(liqMin, 0) && liqMin > 0 {
		liquidityScore = ratioScore(opp.LiquidityUSD, liqMin)
	} else if opp.LiquidityUSD > 0 {
		liquidityScore = 100
	}
	spreadScore := 100.0
	if spreadMax > 0 {
		spreadScore = clamp01(1-(opp.SpreadPct/spreadMax)) * 100
	}

	diversificationScore := 100.0
	if isOpenTicker(state.OpenTickers, opp.Ticker) {
		diversificationScore -= 40
	}
	if state.Equity > 0 && marketCapPct > 0 {
		marketPressure := (marketExposure / state.Equity) / marketCapPct
		if marketPressure > 0.8 {
			diversificationScore -= 30 * clamp01((marketPressure-0.8)/0.2)
		}
	}

	freshnessScore := 100.0
	if !opp.CreatedAt.IsZero() && !opp.ExpiresAt.IsZero() && opp.ExpiresAt.After(opp.CreatedAt) && !now.Before(opp.CreatedAt) {
		ttl := opp.ExpiresAt.Sub(opp.CreatedAt)
		age := now.Sub(opp.CreatedAt)
		freshnessScore = clamp01(1-(float64(age)/float64(ttl))) * 100
	}

	score := edgeScore*0.35 + confidenceScore*0.20 + liquidityScore*0.15 + spreadScore*0.10 + diversificationScore*0.10 + freshnessScore*0.10

	penalties := 0.0
	reasons := make([]string, 0, 3)
	if isOpenTicker(state.OpenTickers, opp.Ticker) {
		penalties += 14
		reasons = append(reasons, reasonDuplicateTicker)
	}
	if marketCap > 0 {
		base := state.Equity * lookup(cfg.MaxPerPositionPct, market)
		if base <= 0 {
			base = opp.ProposedNotional
		}
		marketCapPressure := clamp01(base / marketCap)
		if marketCapPressure > 0.01 {
			penalties += 20 * clamp01(marketCapPressure/0.02)
			reasons = append(reasons, reasonNearMarketCap)
		}
	}
	if state.BuyingPower > 0 && state.Equity > 0 {
		reserve := state.Equity * cfg.CashReservePct
		if state.BuyingPower < reserve*1.25 {
			penalties += 8 * clamp01(1-(state.BuyingPower/reserve))
			reasons = append(reasons, reasonCashReservePressure)
		}
	}

	score = clampScore(score - penalties)
	return score, uniqueStrings(reasons)
}

func sizeOpportunity(opp domain.Opportunity, score float64, state PortfolioState, cfg AllocatorConfig) (float64, []string) {
	market := opp.MarketType
	multiplier := scoreMultiplier(score)
	if multiplier <= 0 {
		return 0, []string{reasonBelowMinScore}
	}
	perPosition := lookup(cfg.MaxPerPositionPct, market)
	base := state.Equity * perPosition * multiplier
	if opp.MarketType == domain.MarketTypeKalshi || opp.MarketType == domain.MarketTypePolymarket {
		base = math.Min(base, 25)
	}

	reasons := make([]string, 0, 4)

	remainingTarget := state.Equity*cfg.TargetGrossExposurePct - state.GrossExposure
	if remainingTarget <= 0 {
		return 0, []string{reasonTargetExposureExceeded}
	}
	remainingHard := state.Equity*cfg.HardGrossExposurePct - state.GrossExposure
	if remainingHard <= 0 {
		return 0, []string{reasonHardExposureExceeded}
	}
	remainingMarket := state.Equity*lookup(cfg.MaxPerMarketPct, market) - lookupMarketExposure(state.MarketExposure, market)
	if remainingMarket <= 0 {
		return 0, []string{reasonMarketExposureExceeded}
	}
	remainingBuyingPower := state.BuyingPower - state.Equity*cfg.CashReservePct
	if remainingBuyingPower <= 0 {
		return 0, []string{reasonCashReserveInsufficient}
	}
	if opp.MarketCapUSD > 0 {
		marketCapCap := opp.MarketCapUSD * 0.02
		if marketCapCap > 0 {
			base = math.Min(base, marketCapCap)
		}
	}

	final := minPositive(base, remainingTarget, remainingHard, remainingMarket, remainingBuyingPower)
	if final <= 0 {
		return 0, []string{reasonSizingZero}
	}
	if final < base {
		reasons = append(reasons, reasonBudgetClamped)
	}
	return final, uniqueStrings(reasons)
}

func scoreMultiplier(score float64) float64 {
	switch {
	case score >= 85:
		return 1
	case score >= 75:
		return 0.75
	case score >= 65:
		return 0.5
	default:
		return 0
	}
}

func toAllocationDecision(item scoredOpportunity) domain.AllocationDecision {
	opportunityID := item.opp.ID
	strategyID := item.opp.StrategyID
	decision := domain.AllocationDecision{
		Mode:        domain.AllocationDecisionModeShadow,
		Action:      item.action,
		Score:       clampScore(item.score),
		NotionalUSD: item.notional,
		Quantity:    0,
		Reasons:     append([]string(nil), item.reasons...),
	}
	if opportunityID != uuid.Nil {
		decision.OpportunityID = &opportunityID
	}
	if strategyID != uuid.Nil {
		decision.StrategyID = &strategyID
	}
	return decision
}

func clamp01(v float64) float64 {
	switch {
	case math.IsNaN(v):
		return 0
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}

func clampScore(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func ratioScore(value, threshold float64) float64 {
	if threshold <= 0 {
		return 100
	}
	return clamp01(value/threshold) * 100
}

func lookupMarketExposure(m map[domain.MarketType]float64, market domain.MarketType) float64 {
	if m == nil {
		return 0
	}
	return m[market]
}

func isOpenTicker(open map[string]bool, ticker string) bool {
	if len(open) == 0 {
		return false
	}
	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return false
	}
	if open[ticker] {
		return true
	}
	if open[strings.ToUpper(ticker)] {
		return true
	}
	if open[strings.ToLower(ticker)] {
		return true
	}
	return false
}

func minPositive(values ...float64) float64 {
	minimum := math.Inf(1)
	for _, v := range values {
		if v <= 0 || math.IsNaN(v) {
			return 0
		}
		if v < minimum {
			minimum = v
		}
	}
	if math.IsInf(minimum, 1) {
		return 0
	}
	return minimum
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func incrementMarket(m map[string]int, market domain.MarketType) {
	m[market.String()]++
}

func incrementStringReason(m map[string]int, reason string) {
	m[reason]++
}
