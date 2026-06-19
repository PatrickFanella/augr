package portfolio

import (
	"context"
	"errors"
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

var ErrPaperProcessorUnavailable = errors.New("portfolio: paper processor unavailable")

// PaperOrderManagerProcessor adapts the existing order manager to allocator
// paper execution. It always uses an in-memory paper broker and never enables
// live trading.
type PaperOrderManagerProcessor struct {
	deps PaperOrderManagerProcessorDeps
}

type PaperOrderManagerProcessorDeps struct {
	RiskEngine       risk.RiskEngine
	PositionRepo     repository.PositionRepository
	OrderRepo        repository.OrderRepository
	TradeRepo        repository.TradeRepository
	AuditLogRepo     repository.AuditLogRepository
	AgentEventRepo   repository.AgentEventRepository
	DecisionRecorder execution.DecisionRecorder
	Metrics          execution.OrderMetricsRecorder
	Logger           *slog.Logger
	InitialBalance   float64
	FractionPct      float64
}

func NewPaperOrderManagerProcessor(deps PaperOrderManagerProcessorDeps) *PaperOrderManagerProcessor {
	return &PaperOrderManagerProcessor{deps: deps}
}

func (p *PaperOrderManagerProcessor) ProcessPaperOrder(ctx context.Context, request PaperOrderRequest) (PaperOrderResult, error) {
	if p == nil {
		return PaperOrderResult{}, ErrPaperProcessorUnavailable
	}
	initialBalance := p.deps.InitialBalance
	if initialBalance <= 0 {
		initialBalance = 100_000
	}
	if request.NotionalUSD <= 0 {
		return PaperOrderResult{Skipped: true, Reason: "missing_notional_usd"}, nil
	}
	fractionPct := request.NotionalUSD / initialBalance
	if fractionPct <= 0 {
		return PaperOrderResult{Skipped: true, Reason: "missing_fraction_pct"}, nil
	}
	manager := execution.NewOrderManager(
		paper.NewPaperBroker(initialBalance, 0, 0),
		"paper",
		p.deps.RiskEngine,
		p.deps.PositionRepo,
		p.deps.OrderRepo,
		p.deps.TradeRepo,
		p.deps.AuditLogRepo,
		p.deps.AgentEventRepo,
		execution.SizingConfig{Method: execution.PositionSizingMethodFixedFractional, FractionPct: fractionPct},
		p.deps.Logger,
	).WithLiveTrading(false)
	if p.deps.Metrics != nil {
		manager = manager.WithMetrics(p.deps.Metrics)
	}
	if p.deps.DecisionRecorder != nil {
		manager = manager.WithDecisionRecorder(p.deps.DecisionRecorder)
	}
	if err := manager.ProcessSignal(ctx, request.Signal, request.Plan, request.StrategyID, request.RunID); err != nil {
		return PaperOrderResult{}, err
	}
	if p.deps.OrderRepo == nil {
		return PaperOrderResult{Skipped: true, Reason: "missing_order_repo"}, nil
	}
	orders, err := p.deps.OrderRepo.GetByRun(ctx, request.RunID, repository.OrderFilter{}, 10, 0)
	if err != nil {
		return PaperOrderResult{}, err
	}
	if len(orders) == 0 {
		return PaperOrderResult{Skipped: true, Reason: "paper_order_not_created"}, nil
	}
	order := orders[0]
	return PaperOrderResult{OrderID: &order.ID, Status: order.Status, Skipped: false}, nil
}

// Compile-time assertion that the processor stays on the paper execution boundary.
var _ PaperOrderProcessor = (*PaperOrderManagerProcessor)(nil)

// Avoid an unused import regression when domain constants move; this also keeps
// the file colocated with portfolio market semantics.
var _ = domain.MarketTypeStock
