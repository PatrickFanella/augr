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
	decisions  []domain.TradeDecision
	resolved   []uuid.UUID
	lastFilter repository.TradeDecisionFilter
	lastLimit  int
}

func (s *settlementDecisionStub) Get(_ context.Context, id uuid.UUID) (*domain.TradeDecision, error) {
	for i := range s.decisions {
		if s.decisions[i].ID == id {
			d := s.decisions[i]
			return &d, nil
		}
	}
	return nil, nil
}

func (s *settlementDecisionStub) List(_ context.Context, f repository.TradeDecisionFilter, limit, _ int) ([]domain.TradeDecision, error) {
	s.lastFilter = f
	s.lastLimit = limit
	var out []domain.TradeDecision
	for _, d := range s.decisions {
		if d.MarketType == f.MarketType && d.Status == f.Status {
			if f.InstrumentKey != "" && d.InstrumentKey != f.InstrumentKey {
				continue
			}
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
func (s *settlementPositionStub) CreateAlpacaOwned(context.Context, *domain.Position) error {
	return nil
}

func (s *settlementPositionStub) Get(context.Context, uuid.UUID) (*domain.Position, error) {
	if s.position.ID != uuid.Nil {
		p := s.position
		return &p, nil
	}
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

func (s *settlementPositionStub) ListOpenAlpacaOwned(context.Context, int, int) ([]domain.Position, error) {
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

func (s *settlementTradeStub) GetByOrder(_ context.Context, orderID uuid.UUID, _ repository.TradeFilter, _, _ int) ([]domain.Trade, error) {
	var out []domain.Trade
	for _, trade := range s.trades {
		if trade.OrderID != nil && *trade.OrderID == orderID {
			out = append(out, trade)
		}
	}
	return out, nil
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
	trades := &settlementTradeStub{trades: []domain.Trade{{ID: uuid.New(), OrderID: &orderID, PositionID: &positions.position.ID, Ticker: "KX-TEST", Side: domain.OrderSideBuy, Quantity: 4, Price: .40}}}
	replay := &settlementReplayStub{}
	settler := NewSettler(nil, decisions, positions, trades, replay)
	resolvedAt := time.Date(2026, 7, 12, 15, 0, 0, 0, time.UTC)

	count, err := settler.SettleMarket(context.Background(), domain.MarketTypeKalshi, "KX-TEST", "YES", resolvedAt)
	if err != nil {
		t.Fatalf("SettleMarket() error = %v", err)
	}
	if count != 1 || positions.position.Quantity != 0 || math.Abs(positions.position.RealizedPnL-2.4) > 1e-9 || positions.position.ClosedAt == nil {
		t.Fatalf("settlement result count=%d position=%+v", count, positions.position)
	}
	_ = trades
	_ = replay

	count, err = settler.SettleMarket(context.Background(), domain.MarketTypeKalshi, "KX-TEST", "YES", resolvedAt)
	if err != nil || count != 0 {
		t.Fatalf("repeat settlement count=%d err=%v", count, err)
	}
}

type atomicLifecycleStub struct{ called int }

func (s *atomicLifecycleStub) ApplyOrderFill(context.Context, repository.OrderFillInput) (repository.OrderFillResult, error) {
	s.called++
	return repository.OrderFillResult{}, nil
}

func (s *atomicLifecycleStub) SettlePredictionDecision(context.Context, repository.PredictionDecisionSettlementInput) (repository.PredictionDecisionSettlementResult, error) {
	s.called++
	return repository.PredictionDecisionSettlementResult{DecisionID: uuid.New()}, nil
}

func TestSettlerUsesAtomicLifecycleWhenAvailable(t *testing.T) {
	strategyID, orderID, decisionID := uuid.New(), uuid.New(), uuid.New()
	decision := &domain.TradeDecision{ID: decisionID, StrategyID: &strategyID, PaperOrderID: &orderID, MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-TEST", Outcome: "YES", Status: domain.TradeDecisionStatusPaper}
	legacyDecisions := &settlementDecisionStub{decisions: []domain.TradeDecision{*decision}}
	legacyPositions := &settlementPositionStub{position: domain.Position{ID: uuid.New(), StrategyID: &strategyID, Ticker: "KX-TEST:YES", Side: domain.PositionSideLong, Quantity: 4, AvgEntry: .40}}
	legacyTrades := &settlementTradeStub{trades: []domain.Trade{{ID: uuid.New(), OrderID: &orderID, PositionID: &legacyPositions.position.ID, Ticker: "KX-TEST", Side: domain.OrderSideBuy, Quantity: 4, Price: .40}}}
	legacyReplay := &settlementReplayStub{}
	atomicRepo := &atomicLifecycleStub{}
	settler := NewSettler(atomicRepo, legacyDecisions, legacyPositions, legacyTrades, legacyReplay)
	settler.now = func() time.Time { return time.Unix(0, 0) }
	if _, err := settler.SettleMarket(context.Background(), domain.MarketTypeKalshi, "KX-TEST", "YES", time.Unix(0, 0)); err != nil {
		t.Fatalf("SettleMarket() error = %v", err)
	}
	if atomicRepo.called != 1 || len(legacyTrades.trades) != 1 || len(legacyReplay.events) != 0 || len(legacyDecisions.resolved) != 0 {
		t.Fatalf("expected atomic path only, got atomic=%d trades=%d replay=%d resolved=%d", atomicRepo.called, len(legacyTrades.trades), len(legacyReplay.events), len(legacyDecisions.resolved))
	}
}

func TestSettlerPreviewMarketCountsWithoutMutation(t *testing.T) {
	strategyID, orderID, posID := uuid.New(), uuid.New(), uuid.New()
	decisions := &settlementDecisionStub{decisions: []domain.TradeDecision{{ID: uuid.New(), StrategyID: &strategyID, PaperOrderID: &orderID, MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-TEST", Outcome: "YES", Status: domain.TradeDecisionStatusPaper}, {ID: uuid.New(), StrategyID: &strategyID, PaperOrderID: &orderID, MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-OTHER", Outcome: "YES", Status: domain.TradeDecisionStatusPaper}, {ID: uuid.New(), StrategyID: &strategyID, PaperOrderID: &orderID, MarketType: domain.MarketTypePolymarket, InstrumentKey: "KX-TEST", Outcome: "YES", Status: domain.TradeDecisionStatusPaper}}}
	positions := &settlementPositionStub{position: domain.Position{ID: posID, StrategyID: &strategyID, Ticker: "KX-TEST:YES", Side: domain.PositionSideLong, Quantity: 1, AvgEntry: .50}}
	trades := &settlementTradeStub{trades: []domain.Trade{{ID: uuid.New(), OrderID: &orderID, PositionID: &posID, Ticker: "KX-TEST", Side: domain.OrderSideBuy, Quantity: 1, Price: .50}}}
	replay := &settlementReplayStub{}
	settler := NewSettler(nil, decisions, positions, trades, replay)
	count, err := settler.PreviewMarket(context.Background(), domain.MarketTypeKalshi, "KX-TEST")
	if err != nil {
		t.Fatalf("PreviewMarket() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("PreviewMarket() = %d, want 1", count)
	}
	if len(decisions.resolved) != 0 {
		t.Fatalf("PreviewMarket mutated decisions: %#v", decisions.resolved)
	}
	if positions.position.Quantity != 1 || len(trades.trades) != 1 {
		t.Fatalf("PreviewMarket mutated linked repos: position=%+v trades=%+v", positions.position, trades.trades)
	}
}

func TestSettlerPreviewRejectsInvalidCandidateLinkage(t *testing.T) {
	strategyID, orderID, posID := uuid.New(), uuid.New(), uuid.New()
	tests := []struct {
		name      string
		decision  domain.TradeDecision
		positions *settlementPositionStub
		trades    *settlementTradeStub
	}{
		{name: "missing ids", decision: domain.TradeDecision{MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-TEST", Outcome: "YES", Status: domain.TradeDecisionStatusPaper}, positions: &settlementPositionStub{}, trades: &settlementTradeStub{}},
		{name: "missing outcome", decision: domain.TradeDecision{ID: uuid.New(), StrategyID: &strategyID, PaperOrderID: &orderID, MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-TEST", Status: domain.TradeDecisionStatusPaper}, positions: &settlementPositionStub{}, trades: &settlementTradeStub{}},
		{name: "missing position", decision: domain.TradeDecision{ID: uuid.New(), StrategyID: &strategyID, PaperOrderID: &orderID, MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-TEST", Outcome: "YES", Status: domain.TradeDecisionStatusPaper}, positions: &settlementPositionStub{}, trades: &settlementTradeStub{trades: []domain.Trade{{ID: uuid.New(), OrderID: &orderID, PositionID: &posID, Ticker: "KX-TEST", Side: domain.OrderSideBuy, Quantity: 1, Price: .50}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settler := NewSettler(nil, &settlementDecisionStub{decisions: []domain.TradeDecision{tt.decision}}, tt.positions, tt.trades, nil)
			if _, err := settler.PreviewMarket(context.Background(), domain.MarketTypeKalshi, "KX-TEST"); err == nil {
				t.Fatal("PreviewMarket() error = nil, want validation failure")
			}
		})
	}
}

func TestSettlerPendingMarketsBoundsAndSorts(t *testing.T) {
	decisions := make([]domain.TradeDecision, 0, 3)
	for _, key := range []string{"KX-B", "KX-A", "KX-B"} {
		decisions = append(decisions, domain.TradeDecision{ID: uuid.New(), MarketType: domain.MarketTypeKalshi, InstrumentKey: key, Status: domain.TradeDecisionStatusPaper})
	}
	stub := &settlementDecisionStub{decisions: decisions}
	settler := NewSettler(nil, stub, &settlementPositionStub{}, &settlementTradeStub{}, nil)
	markets, err := settler.PendingMarkets(context.Background(), domain.MarketTypeKalshi)
	if err != nil {
		t.Fatalf("PendingMarkets() error = %v", err)
	}
	if got := len(markets); got != 2 || markets[0] != "KX-A" || markets[1] != "KX-B" {
		t.Fatalf("markets=%v", markets)
	}
	if stub.lastFilter.InstrumentKey != "" || stub.lastLimit != pendingMarketCap+1 {
		t.Fatalf("filter=%#v limit=%d", stub.lastFilter, stub.lastLimit)
	}
}

func TestSettlerMatchPaperDecisionsUsesExactInstrumentFilter(t *testing.T) {
	strategyID, orderID := uuid.New(), uuid.New()
	positionID := uuid.New()
	position := &settlementPositionStub{position: domain.Position{ID: positionID, StrategyID: &strategyID, Ticker: "KX-A:YES", Side: domain.PositionSideLong, Quantity: 1, AvgEntry: .5}}
	trades := &settlementTradeStub{trades: []domain.Trade{{ID: uuid.New(), OrderID: &orderID, PositionID: &positionID, Ticker: "KX-A", Side: domain.OrderSideBuy, Quantity: 1, Price: .5}}}
	stub := &settlementDecisionStub{decisions: []domain.TradeDecision{{ID: uuid.New(), StrategyID: &strategyID, PaperOrderID: &orderID, MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-A", Outcome: "YES", Status: domain.TradeDecisionStatusPaper}, {ID: uuid.New(), StrategyID: &strategyID, PaperOrderID: &orderID, MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-B", Outcome: "YES", Status: domain.TradeDecisionStatusPaper}}}
	settler := NewSettler(nil, stub, position, trades, nil)
	count, err := settler.PreviewMarket(context.Background(), domain.MarketTypeKalshi, "KX-A")
	if err != nil {
		t.Fatalf("PreviewMarket() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("count=%d", count)
	}
	if stub.lastFilter.InstrumentKey != "KX-A" {
		t.Fatalf("last filter=%#v", stub.lastFilter)
	}
}

func TestSettlerPendingMarketsRejectsOverCap(t *testing.T) {
	decisions := make([]domain.TradeDecision, pendingMarketCap+1)
	for i := range decisions {
		decisions[i] = domain.TradeDecision{ID: uuid.New(), MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-" + uuid.NewString(), Status: domain.TradeDecisionStatusPaper}
	}
	settler := NewSettler(nil, &settlementDecisionStub{decisions: decisions}, &settlementPositionStub{}, &settlementTradeStub{}, nil)
	if _, err := settler.PendingMarkets(context.Background(), domain.MarketTypeKalshi); err == nil {
		t.Fatal("PendingMarkets() error = nil, want cap error")
	}
}

func TestSettlerMatchPaperDecisionsRejectsOverCap(t *testing.T) {
	strategyID, orderID := uuid.New(), uuid.New()
	decisions := make([]domain.TradeDecision, marketDecisionCap+1)
	for i := range decisions {
		decisions[i] = domain.TradeDecision{ID: uuid.New(), StrategyID: &strategyID, PaperOrderID: &orderID, MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-A", Outcome: "YES", Status: domain.TradeDecisionStatusPaper}
	}
	settler := NewSettler(nil, &settlementDecisionStub{decisions: decisions}, &settlementPositionStub{}, &settlementTradeStub{}, nil)
	if _, err := settler.PreviewMarket(context.Background(), domain.MarketTypeKalshi, "KX-A"); err == nil {
		t.Fatal("PreviewMarket() error = nil, want cap error")
	}
}

func TestSettlerPreviewAndExactSettlementUsesImmutableDecisionIDs(t *testing.T) {
	strategyID, orderID := uuid.New(), uuid.New()
	decisions := &settlementDecisionStub{decisions: []domain.TradeDecision{{ID: uuid.New(), StrategyID: &strategyID, PaperOrderID: &orderID, MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-A", Outcome: "YES", Status: domain.TradeDecisionStatusPaper}}}
	positions := &settlementPositionStub{position: domain.Position{ID: uuid.New(), StrategyID: &strategyID, Ticker: "KX-A:YES", Side: domain.PositionSideLong, Quantity: 1, AvgEntry: .5}}
	trades := &settlementTradeStub{trades: []domain.Trade{{ID: uuid.New(), OrderID: &orderID, PositionID: &positions.position.ID, Ticker: "KX-A", Side: domain.OrderSideBuy, Quantity: 1, Price: .5}}}
	replay := &settlementReplayStub{}
	settler := NewSettler(nil, decisions, positions, trades, replay)
	preview, err := settler.SettlePreview(context.Background(), domain.MarketTypeKalshi, "KX-A")
	if err != nil || preview.Count != 1 || len(preview.DecisionIDs) != 1 {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	decisions.decisions = append(decisions.decisions, domain.TradeDecision{ID: uuid.New(), StrategyID: &strategyID, PaperOrderID: &orderID, MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-A", Outcome: "YES", Status: domain.TradeDecisionStatusPaper})
	count, err := settler.SettleDecisions(context.Background(), domain.MarketTypeKalshi, "KX-A", "YES", time.Unix(0, 0), preview.DecisionIDs)
	if err != nil || count != 1 {
		t.Fatalf("SettleDecisions() count=%d err=%v", count, err)
	}
	if len(decisions.resolved) != 1 || decisions.resolved[0] != preview.DecisionIDs[0] {
		t.Fatalf("resolved=%v", decisions.resolved)
	}
}

func TestSettlerExactSettlementRejectsChangedDecision(t *testing.T) {
	strategyID, orderID := uuid.New(), uuid.New()
	decision := domain.TradeDecision{ID: uuid.New(), StrategyID: &strategyID, PaperOrderID: &orderID, MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-A", Outcome: "YES", Status: domain.TradeDecisionStatusPaper}
	decisions := &settlementDecisionStub{decisions: []domain.TradeDecision{decision}}
	positions := &settlementPositionStub{position: domain.Position{ID: uuid.New(), StrategyID: &strategyID, Ticker: "KX-A:YES", Side: domain.PositionSideLong, Quantity: 1, AvgEntry: .5}}
	trades := &settlementTradeStub{trades: []domain.Trade{{ID: uuid.New(), OrderID: &orderID, PositionID: &positions.position.ID, Ticker: "KX-A", Side: domain.OrderSideBuy, Quantity: 1, Price: .5}}}
	settler := NewSettler(nil, decisions, positions, trades, nil)
	preview, _ := settler.SettlePreview(context.Background(), domain.MarketTypeKalshi, "KX-A")
	decisions.decisions[0].Status = domain.TradeDecisionStatusClosed
	if _, err := settler.SettleDecisions(context.Background(), domain.MarketTypeKalshi, "KX-A", "YES", time.Unix(0, 0), preview.DecisionIDs); err == nil {
		t.Fatal("expected rejection")
	}
}
