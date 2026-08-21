package copytrading

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

type OrderManagerExecutorDeps struct {
	Broker             *paper.PaperBroker
	Risk               risk.RiskEngine
	Positions          repository.PositionRepository
	Orders             repository.OrderRepository
	Trades             repository.TradeRepository
	FinancialLifecycle repository.FinancialLifecycleRepository
	Audit              repository.AuditLogRepository
	Events             repository.AgentEventRepository
	DecisionRecorder   execution.DecisionRecorder
	Metrics            execution.OrderMetricsRecorder
	Logger             *slog.Logger
}

// OrderManagerExecutor adapts copy intents to Augr's existing risk and paper
// order lifecycle. It never enables live trading.
type OrderManagerExecutor struct{ deps OrderManagerExecutorDeps }

func NewOrderManagerExecutor(deps OrderManagerExecutorDeps) *OrderManagerExecutor {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &OrderManagerExecutor{deps: deps}
}

func (e *OrderManagerExecutor) ExecuteCopyOrder(ctx context.Context, request PaperOrderRequest) (PaperOrderResult, error) {
	if e == nil || e.deps.Broker == nil || e.deps.Risk == nil || e.deps.Positions == nil || e.deps.Orders == nil || e.deps.Trades == nil {
		return PaperOrderResult{}, fmt.Errorf("copy paper executor dependencies are unavailable")
	}
	if request.Intent.ExecutablePrice == nil || *request.Intent.ExecutablePrice <= 0 || request.Intent.RequestedNotional <= 0 {
		return PaperOrderResult{}, fmt.Errorf("copy intent has no executable price or notional")
	}
	balance, err := e.deps.Broker.GetAccountBalance(ctx)
	if err != nil {
		return PaperOrderResult{}, err
	}
	if balance.Equity <= 0 {
		return PaperOrderResult{}, fmt.Errorf("paper account equity is not positive")
	}
	fraction := request.Intent.RequestedNotional / balance.Equity
	manager := execution.NewOrderManager(e.deps.Broker, "paper", e.deps.Risk, e.deps.Positions, e.deps.Orders, e.deps.Trades, e.deps.Audit, e.deps.Events, execution.SizingConfig{Method: execution.PositionSizingMethodFixedFractional, FractionPct: fraction}, e.deps.Logger).
		WithFinancialLifecycleRepo(e.deps.FinancialLifecycle).
		WithDecisionRecorder(e.deps.DecisionRecorder).
		WithLiveTrading(false)
	if e.deps.Metrics != nil {
		manager = manager.WithMetrics(e.deps.Metrics)
	}
	signal := domain.PipelineSignalBuy
	if request.Intent.Side == domain.OrderSideSell {
		signal = domain.PipelineSignalSell
	}
	price := *request.Intent.ExecutablePrice
	if request.Subscription.LegacyStrategyID == nil {
		return PaperOrderResult{}, fmt.Errorf("origin-native copy execution handoff is not configured")
	}
	if err := manager.ProcessSignal(ctx, execution.FinalSignal{Signal: signal, Confidence: 1}, execution.TradingPlan{Action: signal, MarketType: domain.MarketTypeStock, Ticker: request.Intent.Ticker, EntryType: "limit", EntryPrice: price, ReferencePrice: price, PositionSize: request.Intent.RequestedNotional / price, Confidence: 1, Rationale: "deterministic copy-subscription rebalance"}, *request.Subscription.LegacyStrategyID, request.Run.ID); err != nil {
		return PaperOrderResult{}, err
	}
	orders, err := e.deps.Orders.GetByRun(ctx, request.Run.ID, repository.OrderFilter{Ticker: request.Intent.Ticker, Side: request.Intent.Side}, 10, 0)
	if err != nil {
		return PaperOrderResult{}, err
	}
	if len(orders) == 0 {
		return PaperOrderResult{}, fmt.Errorf("copy order was not persisted")
	}
	order := orders[0]
	return PaperOrderResult{OrderID: &order.ID, Status: order.Status}, nil
}

var _ PaperOrderExecutor = (*OrderManagerExecutor)(nil)
