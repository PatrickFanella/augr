package portfolio

import (
	"context"
	"fmt"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/google/uuid"
)

// PaperOrderProcessor executes a validated paper trading plan.
// The implementation must be configured to use a paper broker and must not
// enable live trading.
type PaperOrderProcessor interface {
	ProcessPaperOrder(ctx context.Context, request PaperOrderRequest) (PaperOrderResult, error)
}

type PaperOrderRequest struct {
	Signal      execution.FinalSignal
	Plan        execution.TradingPlan
	StrategyID  uuid.UUID
	RunID       uuid.UUID
	NotionalUSD float64
}

type PaperOrderResult struct {
	OrderID *uuid.UUID
	Status  domain.OrderStatus
	Skipped bool
	Reason  string
}

// PaperExecutorDeps holds the minimal dependencies needed to bridge allocator
// decisions into paper order processing.
type PaperExecutorDeps struct {
	Processor PaperOrderProcessor
}

// PaperExecutionResult describes the allocator decision after paper execution.
type PaperExecutionResult struct {
	Action      domain.AllocationDecisionAction
	Reason      string
	OrderID     *uuid.UUID
	FinalSignal execution.FinalSignal
	TradingPlan execution.TradingPlan
}

// PaperExecutor validates allocator decisions and forwards approved intents to
// the configured paper order processor.
type PaperExecutor struct {
	deps PaperExecutorDeps
}

// NewPaperExecutor constructs a new paper executor.
func NewPaperExecutor(deps PaperExecutorDeps) *PaperExecutor {
	return &PaperExecutor{deps: deps}
}

// ExecutePaperDecision validates the decision/strategy pair and, when allowed,
// submits a paper order intent through the configured processor.
func (e *PaperExecutor) ExecutePaperDecision(ctx context.Context, opportunity domain.Opportunity, decision domain.AllocationDecision, strategy domain.Strategy) (PaperExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return PaperExecutionResult{}, err
	}

	if e == nil || e.deps.Processor == nil {
		return PaperExecutionResult{Action: domain.AllocationDecisionActionExecutionRejected, Reason: "missing_paper_processor"}, nil
	}
	if decision.Action != domain.AllocationDecisionActionShadowSelected && decision.Action != domain.AllocationDecisionActionPaperOrderIntent {
		return e.rejected("invalid_decision_action"), nil
	}
	if !strategy.IsPaper {
		return e.rejected("strategy_not_paper"), nil
	}
	if strings.ToLower(strings.TrimSpace(strategy.Status)) != domain.StrategyStatusActive {
		return e.rejected("strategy_inactive"), nil
	}
	if opportunity.StrategyID != strategy.ID {
		return e.rejected("strategy_mismatch"), nil
	}
	if opportunity.MarketType.Normalize() != strategy.MarketType.Normalize() {
		return e.rejected("market_type_mismatch"), nil
	}
	if opportunity.Signal != domain.PipelineSignalBuy && opportunity.Signal != domain.PipelineSignalSell {
		return e.rejected("unsupported_signal"), nil
	}
	if decision.NotionalUSD <= 0 {
		return e.rejected("missing_notional_usd"), nil
	}
	if opportunity.EntryPrice <= 0 {
		return e.rejected("missing_entry_price"), nil
	}
	if opportunity.MaxLossPct <= 0 {
		return e.rejected("missing_stop_loss"), nil
	}

	stopLoss := paperStopLoss(opportunity.Signal, opportunity.EntryPrice, opportunity.MaxLossPct)
	if stopLoss <= 0 {
		return e.rejected("missing_stop_loss"), nil
	}

	finalSignal := execution.FinalSignal{
		Signal:     opportunity.Signal,
		Confidence: clamp01(opportunity.Confidence),
	}

	plan := execution.TradingPlan{
		Action:       opportunity.Signal,
		MarketType:   opportunity.MarketType.Normalize(),
		Ticker:       strings.TrimSpace(opportunity.Ticker),
		EntryType:    "market",
		EntryPrice:   opportunity.EntryPrice,
		PositionSize: decision.NotionalUSD / opportunity.EntryPrice,
		StopLoss:     stopLoss,
		Confidence:   clamp01(opportunity.Confidence),
		Rationale:    strings.TrimSpace(decisionReason(decision)),
		Side:         paperPlanSide(opportunity),
	}

	runID := uuid.New()
	orderResult, err := e.deps.Processor.ProcessPaperOrder(ctx, PaperOrderRequest{Signal: finalSignal, Plan: plan, StrategyID: strategy.ID, RunID: runID, NotionalUSD: decision.NotionalUSD})
	if err != nil {
		return PaperExecutionResult{
			Action:      domain.AllocationDecisionActionExecutionRejected,
			Reason:      fmt.Sprintf("processor_error:%s", sanitizeReason(err.Error())),
			FinalSignal: finalSignal,
			TradingPlan: plan,
		}, nil
	}
	if orderResult.Skipped || orderResult.OrderID == nil {
		return PaperExecutionResult{Action: domain.AllocationDecisionActionExecutionRejected, Reason: firstReason(orderResult.Reason, "paper_order_not_created"), FinalSignal: finalSignal, TradingPlan: plan}, nil
	}

	return PaperExecutionResult{
		Action:      domain.AllocationDecisionActionExecuted,
		OrderID:     orderResult.OrderID,
		FinalSignal: finalSignal,
		TradingPlan: plan,
	}, nil
}

func paperPlanSide(opportunity domain.Opportunity) string {
	predictionSide := strings.ToUpper(strings.TrimSpace(opportunity.PredictionSide))
	if predictionSide != "" {
		return predictionSide
	}
	return strings.ToUpper(strings.TrimSpace(string(opportunity.Side)))
}

func (e *PaperExecutor) rejected(reason string) PaperExecutionResult {
	return PaperExecutionResult{Action: domain.AllocationDecisionActionExecutionRejected, Reason: reason}
}

func paperStopLoss(signal domain.PipelineSignal, entryPrice, maxLossPct float64) float64 {
	switch signal {
	case domain.PipelineSignalSell:
		return entryPrice * (1 + maxLossPct)
	default:
		return entryPrice * (1 - maxLossPct)
	}
}

func decisionReason(decision domain.AllocationDecision) string {
	if len(decision.Reasons) == 0 {
		return ""
	}
	return strings.Join(decision.Reasons, "; ")
}

func sanitizeReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "unknown"
	}
	return reason
}

func firstReason(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "unknown"
}
