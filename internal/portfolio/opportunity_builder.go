package portfolio

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/google/uuid"
)

const (
	defaultStockTTL       = 24 * time.Hour
	defaultEventMarketTTL = 6 * time.Hour
)

type OpportunityBuilderConfig struct {
	StockTTL       time.Duration
	EventMarketTTL time.Duration
	Now            func() time.Time
}

type OpportunityBuildInput struct {
	Strategy          domain.Strategy
	Run               *domain.PipelineRun
	Decision          *domain.TradeDecision
	Signal            domain.PipelineSignal
	PredictionSide    string
	Confidence        float64
	EdgePct           float64
	ExpectedReturnPct float64
	MaxLossPct        float64
	EntryPrice        float64
	LiquidityUSD      float64
	SpreadPct         float64
	ProposedNotional  float64
	Reason            string
	Evidence          json.RawMessage
}

func BuildOpportunity(input OpportunityBuildInput, cfg OpportunityBuilderConfig) (*domain.Opportunity, NoActionReason, error) {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	stockTTL := cfg.StockTTL
	if stockTTL <= 0 {
		stockTTL = defaultStockTTL
	}
	eventTTL := cfg.EventMarketTTL
	if eventTTL <= 0 {
		eventTTL = defaultEventMarketTTL
	}

	strategyStatus := strings.ToLower(strings.TrimSpace(input.Strategy.Status))
	marketType := input.Strategy.MarketType.Normalize()
	ticker := strings.TrimSpace(input.Strategy.Ticker)
	if input.Strategy.ID == uuid.Nil || ticker == "" || !marketType.IsValid() || strategyStatus != domain.StrategyStatusActive {
		if strategyStatus != domain.StrategyStatusActive {
			return nil, NoActionReasonUnknown, fmt.Errorf("strategy must be active")
		}
		return nil, NoActionReasonUnknown, fmt.Errorf("strategy must include id, market type, and ticker")
	}

	if input.Signal == domain.PipelineSignalHold {
		return nil, NoActionReasonHoldSignal, nil
	}
	if input.Signal != domain.PipelineSignalBuy && input.Signal != domain.PipelineSignalSell {
		return nil, NoActionReasonUnknown, fmt.Errorf("unsupported signal: %q", input.Signal)
	}

	side := orderSideFromSignal(input.Signal)
	if input.Decision != nil && input.Decision.Side.IsValid() {
		side = input.Decision.Side
	}

	createdAt := now().UTC()
	opportunity := &domain.Opportunity{
		StrategyID:        input.Strategy.ID,
		MarketType:        marketType,
		Ticker:            ticker,
		Side:              side,
		PredictionSide:    strings.ToUpper(strings.TrimSpace(input.PredictionSide)),
		Signal:            input.Signal,
		Status:            domain.OpportunityStatusQueued,
		Confidence:        clampFloat(input.Confidence, 0, 1),
		EdgePct:           clampMin(input.EdgePct, 0),
		ExpectedReturnPct: clampMin(input.ExpectedReturnPct, 0),
		MaxLossPct:        clampMin(input.MaxLossPct, 0),
		EntryPrice:        clampMin(input.EntryPrice, 0),
		LiquidityUSD:      clampMin(input.LiquidityUSD, 0),
		SpreadPct:         clampMin(input.SpreadPct, 0),
		ProposedNotional:  clampMin(input.ProposedNotional, 0),
		Reason:            input.Reason,
		Evidence:          normalizeEvidence(input.Evidence),
		CreatedAt:         createdAt,
		UpdatedAt:         createdAt,
	}

	if input.Run != nil {
		runID := input.Run.ID
		opportunity.PipelineRunID = &runID
	} else if input.Decision != nil && input.Decision.PipelineRunID != nil {
		runID := *input.Decision.PipelineRunID
		opportunity.PipelineRunID = &runID
	}

	switch opportunity.MarketType {
	case domain.MarketTypeStock, domain.MarketTypeCrypto, domain.MarketTypeOptions:
		opportunity.ExpiresAt = createdAt.Add(stockTTL)
	case domain.MarketTypeKalshi, domain.MarketTypePolymarket:
		opportunity.ExpiresAt = createdAt.Add(eventTTL)
	default:
		return nil, NoActionReasonUnknown, fmt.Errorf("unsupported market type: %q", input.Strategy.MarketType)
	}

	opportunity.DedupeKey = dedupeKey(createdAt, input.Strategy.ID, opportunity.MarketType, opportunity.Ticker, side, input.Signal)
	return opportunity, "", nil
}

func orderSideFromSignal(signal domain.PipelineSignal) domain.OrderSide {
	switch signal {
	case domain.PipelineSignalSell:
		return domain.OrderSideSell
	default:
		return domain.OrderSideBuy
	}
}

func clampMin(value, min float64) float64 {
	if value < min || value != value {
		return min
	}
	return value
}

func clampFloat(value, min, max float64) float64 {
	if value != value {
		return min
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func normalizeEvidence(evidence json.RawMessage) json.RawMessage {
	if len(evidence) == 0 {
		return json.RawMessage("{}")
	}
	return evidence
}

func dedupeKey(now time.Time, strategyID uuid.UUID, marketType domain.MarketType, ticker string, side domain.OrderSide, signal domain.PipelineSignal) string {
	return strings.ToLower(fmt.Sprintf("%s:%s:%s:%s:%s:%s",
		now.UTC().Format("2006-01-02"),
		strategyID,
		marketType.Normalize(),
		ticker,
		side,
		signal,
	))
}
