package portfolio

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/google/uuid"
)

type paperProcessorStub struct {
	called int
	signal execution.FinalSignal
	plan   execution.TradingPlan
	strat  uuid.UUID
	runID  uuid.UUID
	err    error
	result PaperOrderResult
}

func (p *paperProcessorStub) ProcessPaperOrder(_ context.Context, req PaperOrderRequest) (PaperOrderResult, error) {
	p.called++
	p.signal = req.Signal
	p.plan = req.Plan
	p.strat = req.StrategyID
	p.runID = req.RunID
	if p.result.OrderID == nil && !p.result.Skipped && p.err == nil {
		id := uuid.New()
		p.result.OrderID = &id
		p.result.Status = domain.OrderStatusFilled
	}
	return p.result, p.err
}

func TestPaperExecutorRejectsInvalidPreconditions(t *testing.T) {
	t.Parallel()

	baseOpportunity := domain.Opportunity{
		StrategyID:       uuid.New(),
		MarketType:       domain.MarketTypeStock,
		Ticker:           "AAPL",
		Side:             domain.OrderSideBuy,
		PredictionSide:   "YES",
		Signal:           domain.PipelineSignalBuy,
		Confidence:       0.8,
		EntryPrice:       100,
		MaxLossPct:       0.05,
		SelectedNotional: 2500,
	}
	baseDecision := domain.AllocationDecision{
		Mode:        domain.AllocationDecisionModePaper,
		Action:      domain.AllocationDecisionActionPaperOrderIntent,
		NotionalUSD: 2500,
		Reasons:     []string{"score=91.0"},
	}
	baseStrategy := domain.Strategy{
		ID:         baseOpportunity.StrategyID,
		Ticker:     "AAPL",
		MarketType: domain.MarketTypeStock,
		Status:     domain.StrategyStatusActive,
		IsPaper:    true,
	}

	tests := []struct {
		name        string
		opportunity domain.Opportunity
		decision    domain.AllocationDecision
		strategy    domain.Strategy
		wantReason  string
	}{
		{name: "invalid action", opportunity: baseOpportunity, decision: domain.AllocationDecision{Action: domain.AllocationDecisionActionShadowRejected}, strategy: baseStrategy, wantReason: "invalid_decision_action"},
		{name: "non paper strategy", opportunity: baseOpportunity, decision: baseDecision, strategy: func() domain.Strategy { s := baseStrategy; s.IsPaper = false; return s }(), wantReason: "strategy_not_paper"},
		{name: "inactive strategy", opportunity: baseOpportunity, decision: baseDecision, strategy: func() domain.Strategy { s := baseStrategy; s.Status = domain.StrategyStatusInactive; return s }(), wantReason: "strategy_inactive"},
		{name: "strategy mismatch", opportunity: baseOpportunity, decision: baseDecision, strategy: func() domain.Strategy { s := baseStrategy; s.ID = uuid.New(); return s }(), wantReason: "strategy_mismatch"},
		{name: "market mismatch", opportunity: baseOpportunity, decision: baseDecision, strategy: func() domain.Strategy { s := baseStrategy; s.MarketType = domain.MarketTypeCrypto; return s }(), wantReason: "market_type_mismatch"},
		{name: "missing notional", opportunity: baseOpportunity, decision: func() domain.AllocationDecision { d := baseDecision; d.NotionalUSD = 0; return d }(), strategy: baseStrategy, wantReason: "missing_notional_usd"},
		{name: "missing entry price", opportunity: func() domain.Opportunity { o := baseOpportunity; o.EntryPrice = 0; return o }(), decision: baseDecision, strategy: baseStrategy, wantReason: "missing_entry_price"},
		{name: "missing stop loss", opportunity: func() domain.Opportunity { o := baseOpportunity; o.MaxLossPct = 0; return o }(), decision: baseDecision, strategy: baseStrategy, wantReason: "missing_stop_loss"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			processor := &paperProcessorStub{}
			exec := NewPaperExecutor(PaperExecutorDeps{Processor: processor})
			result, err := exec.ExecutePaperDecision(context.Background(), tt.opportunity, tt.decision, tt.strategy)
			if err != nil {
				t.Fatalf("ExecutePaperDecision() error = %v", err)
			}
			if result.Action != domain.AllocationDecisionActionExecutionRejected {
				t.Fatalf("action = %s, want execution_rejected", result.Action)
			}
			if result.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", result.Reason, tt.wantReason)
			}
			if processor.called != 0 {
				t.Fatalf("processor called %d times, want 0", processor.called)
			}
		})
	}
}

func TestPaperExecutorExecutesValidPaperDecision(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	processor := &paperProcessorStub{}
	exec := NewPaperExecutor(PaperExecutorDeps{Processor: processor})
	opportunity := domain.Opportunity{
		StrategyID:       strategyID,
		MarketType:       domain.MarketTypeStock,
		Ticker:           "AAPL",
		Side:             domain.OrderSideBuy,
		PredictionSide:   "YES",
		Signal:           domain.PipelineSignalBuy,
		Confidence:       0.91,
		EntryPrice:       100,
		MaxLossPct:       0.05,
		SelectedNotional: 2500,
	}
	decision := domain.AllocationDecision{
		Mode:        domain.AllocationDecisionModePaper,
		Action:      domain.AllocationDecisionActionPaperOrderIntent,
		NotionalUSD: 2500,
		Reasons:     []string{"score=91.0", "multiplier=1.00"},
	}
	strategy := domain.Strategy{
		ID:         strategyID,
		Ticker:     "AAPL",
		MarketType: domain.MarketTypeStock,
		Status:     domain.StrategyStatusActive,
		IsPaper:    true,
	}

	result, err := exec.ExecutePaperDecision(context.Background(), opportunity, decision, strategy)
	if err != nil {
		t.Fatalf("ExecutePaperDecision() error = %v", err)
	}
	if result.Action != domain.AllocationDecisionActionExecuted {
		t.Fatalf("action = %s, want executed", result.Action)
	}
	if result.Reason != "" {
		t.Fatalf("reason = %q, want empty", result.Reason)
	}
	if result.OrderID == nil {
		t.Fatal("OrderID = nil, want created paper order id")
	}
	if processor.called != 1 {
		t.Fatalf("processor called %d times, want 1", processor.called)
	}
	if processor.signal.Signal != domain.PipelineSignalBuy || processor.plan.Action != domain.PipelineSignalBuy {
		t.Fatalf("unexpected signal/plan action: %+v %+v", processor.signal, processor.plan)
	}
	if processor.plan.EntryType != "market" || processor.plan.EntryPrice != 100 {
		t.Fatalf("unexpected entry plan: %+v", processor.plan)
	}
	if processor.plan.Side != "YES" {
		t.Fatalf("plan side = %q, want YES", processor.plan.Side)
	}
	if math.Abs(processor.plan.StopLoss-95) > 1e-9 {
		t.Fatalf("stop loss = %v, want 95", processor.plan.StopLoss)
	}
	if processor.plan.PositionSize != 25 {
		t.Fatalf("position size = %v, want 25", processor.plan.PositionSize)
	}
	if processor.strat != strategyID {
		t.Fatalf("strategy id = %s, want %s", processor.strat, strategyID)
	}
}

func TestPaperExecutorConvertsProcessorErrorToExecutionRejected(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	processor := &paperProcessorStub{err: errors.New("boom")}
	exec := NewPaperExecutor(PaperExecutorDeps{Processor: processor})
	result, err := exec.ExecutePaperDecision(context.Background(), domain.Opportunity{
		StrategyID: strategyID,
		MarketType: domain.MarketTypeStock,
		Ticker:     "AAPL",
		Side:       domain.OrderSideBuy,
		Signal:     domain.PipelineSignalBuy,
		Confidence: 0.91,
		EntryPrice: 100,
		MaxLossPct: 0.05,
	}, domain.AllocationDecision{
		Mode:        domain.AllocationDecisionModePaper,
		Action:      domain.AllocationDecisionActionPaperOrderIntent,
		NotionalUSD: 2500,
	}, domain.Strategy{
		ID:         strategyID,
		Ticker:     "AAPL",
		MarketType: domain.MarketTypeStock,
		Status:     domain.StrategyStatusActive,
		IsPaper:    true,
	})
	if err != nil {
		t.Fatalf("ExecutePaperDecision() error = %v", err)
	}
	if result.Action != domain.AllocationDecisionActionExecutionRejected {
		t.Fatalf("action = %s, want execution_rejected", result.Action)
	}
	if result.Reason != "processor_error:boom" {
		t.Fatalf("reason = %q, want processor_error:boom", result.Reason)
	}
}
