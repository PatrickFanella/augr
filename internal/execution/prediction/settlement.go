package prediction

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const settlementPageSize = 1000

type settlementDecisionRepository interface {
	Get(context.Context, uuid.UUID) (*domain.TradeDecision, error)
	List(context.Context, repository.TradeDecisionFilter, int, int) ([]domain.TradeDecision, error)
	ResolvePredictionOutcome(context.Context, uuid.UUID) error
}

// Settler durably cash-settles side-qualified paper event contracts.
type Settler struct {
	decisions settlementDecisionRepository
	positions repository.PositionRepository
	trades    repository.TradeRepository
	replay    repository.ReplayEventRepository
	now       func() time.Time
}

func NewSettler(decisions settlementDecisionRepository, positions repository.PositionRepository, trades repository.TradeRepository, replay repository.ReplayEventRepository) *Settler {
	return &Settler{decisions: decisions, positions: positions, trades: trades, replay: replay, now: time.Now}
}

// SettleMarket settles every still-open paper decision for one resolved market.
// Repeated calls are safe because only paper_ordered decisions are selected.
func (s *Settler) SettleMarket(ctx context.Context, marketType domain.MarketType, instrument, winningOutcome string, resolvedAt time.Time) (int, error) {
	if s == nil || s.decisions == nil || s.positions == nil || s.trades == nil || s.replay == nil {
		return 0, fmt.Errorf("prediction settlement: all repositories are required")
	}
	marketType = marketType.Normalize()
	if marketType != domain.MarketTypePolymarket && marketType != domain.MarketTypeKalshi {
		return 0, fmt.Errorf("prediction settlement: unsupported market type %q", marketType)
	}
	instrument = strings.TrimSpace(instrument)
	winner := NormalizeOutcomeSide(winningOutcome)
	if instrument == "" || winner == "" {
		return 0, fmt.Errorf("prediction settlement: instrument and YES/NO winner are required")
	}
	if resolvedAt.IsZero() {
		resolvedAt = s.now().UTC()
	}

	matches := make([]domain.TradeDecision, 0)
	for offset := 0; ; offset += settlementPageSize {
		decisions, err := s.decisions.List(ctx, repository.TradeDecisionFilter{MarketType: marketType, Status: domain.TradeDecisionStatusPaper}, settlementPageSize, offset)
		if err != nil {
			return 0, fmt.Errorf("prediction settlement: list decisions: %w", err)
		}
		for i := range decisions {
			if strings.EqualFold(strings.TrimSpace(decisions[i].InstrumentKey), instrument) {
				matches = append(matches, decisions[i])
			}
		}
		if len(decisions) < settlementPageSize {
			break
		}
	}
	settled := 0
	for i := range matches {
		if err := s.settleDecision(ctx, &matches[i], winner, resolvedAt); err != nil {
			return settled, err
		}
		settled++
	}
	return settled, nil
}

func (s *Settler) settleDecision(ctx context.Context, decision *domain.TradeDecision, winner string, resolvedAt time.Time) error {
	if decision.StrategyID == nil || decision.PaperOrderID == nil {
		return fmt.Errorf("prediction settlement: decision %s lacks strategy or paper order", decision.ID)
	}
	held := NormalizeOutcomeSide(decision.Outcome)
	if held == "" {
		return fmt.Errorf("prediction settlement: decision %s lacks held outcome", decision.ID)
	}
	key := strings.TrimSpace(decision.InstrumentKey) + ":" + held
	positions, err := s.positions.GetByStrategy(ctx, *decision.StrategyID, repository.PositionFilter{Ticker: key, Side: domain.PositionSideLong}, 10, 0)
	if err != nil {
		return fmt.Errorf("prediction settlement: load %s position: %w", key, err)
	}
	var position *domain.Position
	for i := range positions {
		if positions[i].ClosedAt == nil && positions[i].Quantity > 0 {
			position = &positions[i]
			break
		}
	}
	if position == nil {
		return fmt.Errorf("prediction settlement: open position %s not found", key)
	}

	quantity := position.Quantity
	payout := 0.0
	if held == winner {
		payout = 1
	}
	position.CurrentPrice = &payout
	position.RealizedPnL += (payout - position.AvgEntry) * quantity
	position.UnrealizedPnL = nil
	position.Quantity = 0
	closedAt := resolvedAt.UTC()
	position.ClosedAt = &closedAt
	if err := s.positions.Update(ctx, position); err != nil {
		return fmt.Errorf("prediction settlement: close position: %w", err)
	}

	trade := &domain.Trade{ID: uuid.New(), OrderID: decision.PaperOrderID, PositionID: &position.ID, Ticker: decision.InstrumentKey, Side: domain.OrderSideSell, Quantity: quantity, Price: payout, ExecutedAt: closedAt, CreatedAt: closedAt}
	if err := s.trades.Create(ctx, trade); err != nil {
		return fmt.Errorf("prediction settlement: record payout trade: %w", err)
	}
	if err := s.decisions.ResolvePredictionOutcome(ctx, decision.ID); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"instrument": decision.InstrumentKey, "held_outcome": held, "winning_outcome": winner, "payout": payout, "quantity": quantity, "realized_pnl": position.RealizedPnL, "position_id": position.ID, "trade_id": trade.ID})
	return s.replay.CreateReplayEvent(ctx, &domain.ReplayEvent{TradeDecisionID: decision.ID, EventType: domain.ReplayEventTypeOutcomeResolved, Source: "prediction_settler", Payload: payload, OccurredAt: closedAt})
}
