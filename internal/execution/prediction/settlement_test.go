package prediction

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type settlementDecisionStub struct {
	decisions []domain.TradeDecision
	resolved  []uuid.UUID
}

func (s *settlementDecisionStub) Get(context.Context, uuid.UUID) (*domain.TradeDecision, error) {
	return nil, nil
}
func (s *settlementDecisionStub) List(_ context.Context, f repository.TradeDecisionFilter, _, _ int) ([]domain.TradeDecision, error) {
	var out []domain.TradeDecision
	for _, d := range s.decisions {
		if d.MarketType == f.MarketType && d.Status == f.Status {
			out = append(out, d)
		}
	}
	return out, nil
}
func (s *settlementDecisionStub) ResolvePredictionOutcome(_ context.Context, id uuid.UUID) error {
	s.resolved = append(s.resolved, id)
	for i := range s.decisions {
		if s.decisions[i].ID == id {
			s.decisions[i].Status = domain.TradeDecisionStatusClosed
		}
	}
	return nil
}

type settlementPositionStub struct{ position domain.Position }

func (s *settlementPositionStub) Create(context.Context, *domain.Position) error { return nil }
func (s *settlementPositionStub) Get(context.Context, uuid.UUID) (*domain.Position, error) {
	return nil, nil
}
func (s *settlementPositionStub) List(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}
func (s *settlementPositionStub) Count(context.Context, repository.PositionFilter) (int, error) {
	return 0, nil
}
func (s *settlementPositionStub) Update(_ context.Context, p *domain.Position) error {
	s.position = *p
	return nil
}
func (s *settlementPositionStub) Delete(context.Context, uuid.UUID) error { return nil }
func (s *settlementPositionStub) GetOpen(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}
func (s *settlementPositionStub) CountOpen(context.Context, repository.PositionFilter) (int, error) {
	return 0, nil
}
func (s *settlementPositionStub) GetByStrategy(_ context.Context, _ uuid.UUID, f repository.PositionFilter, _, _ int) ([]domain.Position, error) {
	if s.position.Ticker == f.Ticker && s.position.ClosedAt == nil {
		return []domain.Position{s.position}, nil
	}
	return nil, nil
}

type settlementTradeStub struct{ trades []domain.Trade }

func (s *settlementTradeStub) Create(_ context.Context, trade *domain.Trade) error {
	s.trades = append(s.trades, *trade)
	return nil
}
func (*settlementTradeStub) List(context.Context, repository.TradeFilter, int, int) ([]domain.Trade, error) {
	return nil, nil
}
func (*settlementTradeStub) Count(context.Context, repository.TradeFilter) (int, error) {
	return 0, nil
}
func (*settlementTradeStub) GetByOrder(context.Context, uuid.UUID, repository.TradeFilter, int, int) ([]domain.Trade, error) {
	return nil, nil
}
func (*settlementTradeStub) GetByPosition(context.Context, uuid.UUID, repository.TradeFilter, int, int) ([]domain.Trade, error) {
	return nil, nil
}

type settlementReplayStub struct{ events []domain.ReplayEvent }

func (s *settlementReplayStub) CreateReplayEvent(_ context.Context, e *domain.ReplayEvent) error {
	s.events = append(s.events, *e)
	return nil
}
func (*settlementReplayStub) ListReplayEvents(context.Context, uuid.UUID) ([]domain.ReplayEvent, error) {
	return nil, nil
}

func TestSettlerClosesWinningPaperContractAndIsIdempotent(t *testing.T) {
	strategyID, orderID, decisionID := uuid.New(), uuid.New(), uuid.New()
	decisions := &settlementDecisionStub{decisions: []domain.TradeDecision{{ID: decisionID, StrategyID: &strategyID, PaperOrderID: &orderID, MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-TEST", Outcome: "YES", Status: domain.TradeDecisionStatusPaper}}}
	positions := &settlementPositionStub{position: domain.Position{ID: uuid.New(), StrategyID: &strategyID, MarketType: domain.MarketTypeKalshi, Ticker: "KX-TEST:YES", Side: domain.PositionSideLong, Quantity: 4, AvgEntry: .40}}
	trades, replay := &settlementTradeStub{}, &settlementReplayStub{}
	settler := NewSettler(decisions, positions, trades, replay)
	resolvedAt := time.Date(2026, 7, 12, 15, 0, 0, 0, time.UTC)

	count, err := settler.SettleMarket(context.Background(), domain.MarketTypeKalshi, "KX-TEST", "YES", resolvedAt)
	if err != nil {
		t.Fatalf("SettleMarket() error = %v", err)
	}
	if count != 1 || positions.position.Quantity != 0 || math.Abs(positions.position.RealizedPnL-2.4) > 1e-9 || positions.position.ClosedAt == nil {
		t.Fatalf("settlement result count=%d position=%+v", count, positions.position)
	}
	if len(trades.trades) != 1 || trades.trades[0].Price != 1 || len(replay.events) != 1 || replay.events[0].EventType != domain.ReplayEventTypeOutcomeResolved {
		t.Fatalf("trade/replay missing: trades=%+v events=%+v", trades.trades, replay.events)
	}

	count, err = settler.SettleMarket(context.Background(), domain.MarketTypeKalshi, "KX-TEST", "YES", resolvedAt)
	if err != nil || count != 0 || len(trades.trades) != 1 {
		t.Fatalf("repeat settlement count=%d err=%v trades=%d", count, err, len(trades.trades))
	}
}
