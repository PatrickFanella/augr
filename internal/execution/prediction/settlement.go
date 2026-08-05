package prediction

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const (
	pendingMarketCap  = 5000
	marketDecisionCap = 1000
)

type settlementDecisionRepository interface {
	Get(context.Context, uuid.UUID) (*domain.TradeDecision, error)
	List(context.Context, repository.TradeDecisionFilter, int, int) ([]domain.TradeDecision, error)
	ResolvePredictionOutcome(context.Context, uuid.UUID) error
}

type SettlementPreview struct {
	Instrument  string
	Count       int
	DecisionIDs []uuid.UUID
}

func (p *SettlementPreview) GetInstrument() string {
	if p == nil {
		return ""
	}
	return p.Instrument
}

func (p *SettlementPreview) GetDecisionIDs() []uuid.UUID {
	if p == nil {
		return nil
	}
	return append([]uuid.UUID(nil), p.DecisionIDs...)
}

// Settler durably cash-settles side-qualified paper event contracts.
type Settler struct {
	financialLifecycle repository.FinancialLifecycleRepository
	decisions          settlementDecisionRepository
	positions          repository.PositionRepository
	trades             repository.TradeRepository
	replay             repository.ReplayEventRepository
	now                func() time.Time
}

func NewSettler(financialLifecycle repository.FinancialLifecycleRepository, decisions settlementDecisionRepository, positions repository.PositionRepository, trades repository.TradeRepository, replay repository.ReplayEventRepository) *Settler {
	return &Settler{financialLifecycle: financialLifecycle, decisions: decisions, positions: positions, trades: trades, replay: replay, now: time.Now}
}

// SettleMarket settles every still-open paper decision for one resolved market.
// Repeated calls are safe because only paper_ordered decisions are selected.
func (s *Settler) SettleMarket(ctx context.Context, marketType domain.MarketType, instrument, winningOutcome string, resolvedAt time.Time) (int, error) {
	return s.settleMarket(ctx, marketType, instrument, winningOutcome, resolvedAt, true)
}

func (s *Settler) PreviewMarket(ctx context.Context, marketType domain.MarketType, instrument string) (int, error) {
	preview, err := s.SettlePreview(ctx, marketType, instrument)
	if err != nil {
		return 0, err
	}
	return preview.Count, nil
}

func (s *Settler) SettlePreview(ctx context.Context, marketType domain.MarketType, instrument string) (*SettlementPreview, error) {
	matches, err := s.matchPaperDecisions(ctx, marketType, instrument)
	if err != nil {
		return nil, err
	}
	if _, err := s.validateMatches(ctx, matches, false); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(matches))
	for i := range matches {
		ids = append(ids, matches[i].ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	return &SettlementPreview{Instrument: strings.ToUpper(strings.TrimSpace(instrument)), Count: len(ids), DecisionIDs: append([]uuid.UUID(nil), ids...)}, nil
}

func (s *Settler) SettleDecisions(ctx context.Context, marketType domain.MarketType, instrument, winningOutcome string, resolvedAt time.Time, decisionIDs []uuid.UUID) (int, error) {
	if len(decisionIDs) == 0 {
		return 0, nil
	}
	marketType = marketType.Normalize()
	instrument = strings.TrimSpace(instrument)
	winner := NormalizeOutcomeSide(winningOutcome)
	if winner == "" {
		return 0, fmt.Errorf("prediction settlement: instrument and YES/NO winner are required")
	}
	if s.now == nil {
		s.now = time.Now
	}
	if resolvedAt.IsZero() {
		resolvedAt = s.now().UTC()
	}
	settled := 0
	for _, id := range decisionIDs {
		decision, err := s.decisions.Get(ctx, id)
		if err != nil {
			return settled, fmt.Errorf("prediction settlement: get decision %s: %w", id, err)
		}
		if decision == nil || decision.Status != domain.TradeDecisionStatusPaper || decision.MarketType.Normalize() != marketType || strings.TrimSpace(decision.InstrumentKey) != instrument {
			return settled, fmt.Errorf("prediction settlement: decision %s changed before settlement", id)
		}
		if err := s.settleDecision(ctx, decision, winner, resolvedAt); err != nil {
			return settled, err
		}
		settled++
	}
	return settled, nil
}

func (s *Settler) PendingMarkets(ctx context.Context, marketType domain.MarketType) ([]string, error) {
	if s == nil || s.decisions == nil {
		return nil, fmt.Errorf("prediction settlement: repositories are required")
	}
	marketType = marketType.Normalize()
	if marketType != domain.MarketTypeKalshi && marketType != domain.MarketTypePolymarket {
		return nil, fmt.Errorf("prediction settlement: unsupported market type %q", marketType)
	}
	decisions, err := s.decisions.List(ctx, repository.TradeDecisionFilter{MarketType: marketType, Status: domain.TradeDecisionStatusPaper}, pendingMarketCap+1, 0)
	if err != nil {
		return nil, fmt.Errorf("prediction settlement: list decisions: %w", err)
	}
	if len(decisions) > pendingMarketCap {
		return nil, fmt.Errorf("prediction settlement: pending market cap exceeded")
	}
	seen := make(map[string]struct{}, len(decisions))
	out := make([]string, 0, len(decisions))
	for i := range decisions {
		instrument := strings.ToUpper(strings.TrimSpace(decisions[i].InstrumentKey))
		if instrument == "" {
			continue
		}
		if _, ok := seen[instrument]; ok {
			continue
		}
		seen[instrument] = struct{}{}
		out = append(out, instrument)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Settler) settleMarket(ctx context.Context, marketType domain.MarketType, instrument, winningOutcome string, resolvedAt time.Time, mutate bool) (int, error) {
	if s == nil || s.decisions == nil || (mutate && s.financialLifecycle == nil && (s.positions == nil || s.trades == nil || s.replay == nil)) {
		return 0, fmt.Errorf("prediction settlement: repositories are required")
	}
	marketType = marketType.Normalize()
	if marketType != domain.MarketTypePolymarket && marketType != domain.MarketTypeKalshi {
		return 0, fmt.Errorf("prediction settlement: unsupported market type %q", marketType)
	}
	instrument = strings.TrimSpace(instrument)
	if instrument == "" {
		return 0, fmt.Errorf("prediction settlement: instrument is required")
	}
	winner := NormalizeOutcomeSide(winningOutcome)
	if mutate && winner == "" {
		return 0, fmt.Errorf("prediction settlement: instrument and YES/NO winner are required")
	}
	if resolvedAt.IsZero() {
		resolvedAt = s.now().UTC()
	}
	matches, err := s.matchPaperDecisions(ctx, marketType, instrument)
	if err != nil {
		return 0, err
	}
	validated, err := s.validateMatches(ctx, matches, mutate)
	if err != nil {
		return 0, err
	}
	settled := 0
	for i := range validated {
		if mutate {
			if err := s.settleDecision(ctx, &validated[i], winner, resolvedAt); err != nil {
				return settled, err
			}
		}
		settled++
	}
	return settled, nil
}

func (s *Settler) validateMatches(ctx context.Context, matches []domain.TradeDecision, mutate bool) ([]domain.TradeDecision, error) {
	validated := make([]domain.TradeDecision, 0, len(matches))
	for i := range matches {
		decision := matches[i]
		if decision.ID == uuid.Nil {
			return nil, fmt.Errorf("prediction settlement: invalid decision identifiers")
		}
		if decision.StrategyID == nil || decision.PaperOrderID == nil {
			return nil, fmt.Errorf("prediction settlement: decision %s lacks strategy or paper order", decision.ID)
		}
		held := NormalizeOutcomeSide(decision.Outcome)
		if held == "" {
			return nil, fmt.Errorf("prediction settlement: decision %s lacks held outcome", decision.ID)
		}
		if err := s.validateCandidateLinkage(ctx, &decision, held, mutate); err != nil {
			return nil, err
		}
		validated = append(validated, decision)
	}
	return validated, nil
}

func (s *Settler) validateCandidateLinkage(ctx context.Context, decision *domain.TradeDecision, held string, mutate bool) error {
	if decision.PaperOrderID == nil {
		return fmt.Errorf("prediction settlement: decision %s lacks paper order", decision.ID)
	}
	if s.trades == nil || s.positions == nil {
		return fmt.Errorf("prediction settlement: repositories are required")
	}
	trades, err := s.trades.GetByOrder(ctx, *decision.PaperOrderID, repository.TradeFilter{}, 10, 0)
	if err != nil {
		return fmt.Errorf("prediction settlement: load paper order %s trades: %w", decision.PaperOrderID.String(), err)
	}
	var opening *domain.Trade
	for i := range trades {
		if trades[i].OrderID != nil && *trades[i].OrderID == *decision.PaperOrderID {
			opening = &trades[i]
			break
		}
	}
	if opening == nil || opening.PositionID == nil {
		return fmt.Errorf("prediction settlement: opening trade for paper order %s not found", decision.PaperOrderID.String())
	}
	position, err := s.positions.Get(ctx, *opening.PositionID)
	if err != nil {
		return fmt.Errorf("prediction settlement: load position %s: %w", opening.PositionID.String(), err)
	}
	if position == nil || position.ClosedAt != nil || position.Quantity <= 0 {
		return fmt.Errorf("prediction settlement: open position %s not found", opening.PositionID.String())
	}
	if !strings.EqualFold(strings.TrimSpace(position.Ticker), strings.TrimSpace(decision.InstrumentKey)+":"+held) {
		return fmt.Errorf("prediction settlement: position %s does not match decision %s", position.ID, decision.ID)
	}
	if mutate && position.ID == uuid.Nil {
		return fmt.Errorf("prediction settlement: invalid position identifiers")
	}
	return nil
}

func (s *Settler) matchPaperDecisions(ctx context.Context, marketType domain.MarketType, instrument string) ([]domain.TradeDecision, error) {
	decisions, err := s.decisions.List(ctx, repository.TradeDecisionFilter{MarketType: marketType, Status: domain.TradeDecisionStatusPaper, InstrumentKey: instrument}, marketDecisionCap+1, 0)
	if err != nil {
		return nil, fmt.Errorf("prediction settlement: list decisions: %w", err)
	}
	if len(decisions) > marketDecisionCap {
		return nil, fmt.Errorf("prediction settlement: market decision cap exceeded")
	}
	matches := make([]domain.TradeDecision, 0, len(decisions))
	matches = append(matches, decisions...)
	return matches, nil
}

func (s *Settler) settleDecision(ctx context.Context, decision *domain.TradeDecision, winner string, resolvedAt time.Time) error {
	if decision.StrategyID == nil || decision.PaperOrderID == nil {
		return fmt.Errorf("prediction settlement: decision %s lacks strategy or paper order", decision.ID)
	}
	held := NormalizeOutcomeSide(decision.Outcome)
	if held == "" {
		return fmt.Errorf("prediction settlement: decision %s lacks held outcome", decision.ID)
	}
	if s.financialLifecycle != nil {
		return s.settleDecisionAtomic(ctx, decision, held, winner, resolvedAt)
	}
	return s.settleDecisionLegacy(ctx, decision, held, winner, resolvedAt)
}

func (s *Settler) settleDecisionAtomic(ctx context.Context, decision *domain.TradeDecision, held, winner string, resolvedAt time.Time) error {
	if decision.ID == uuid.Nil || decision.StrategyID == nil || decision.PaperOrderID == nil {
		return fmt.Errorf("prediction settlement: invalid decision identifiers")
	}
	positionTicker := strings.TrimSpace(decision.InstrumentKey)
	if positionTicker == "" {
		return fmt.Errorf("prediction settlement: decision %s lacks instrument key", decision.ID)
	}
	positionTicker += ":" + held
	payout := 0.0
	if held == winner {
		payout = 1
	}
	if math.IsNaN(payout) || math.IsInf(payout, 0) || payout < 0 || payout > 1 {
		return fmt.Errorf("prediction settlement: invalid payout %.8f", payout)
	}
	_, err := s.financialLifecycle.SettlePredictionDecision(ctx, repository.PredictionDecisionSettlementInput{IdempotencyKey: "prediction_settlement:v1:" + decision.ID.String(), Decision: decision, PositionTicker: positionTicker, Payout: payout, ResolvedAt: resolvedAt})
	return err
}

func (s *Settler) settleDecisionLegacy(ctx context.Context, decision *domain.TradeDecision, held, winner string, resolvedAt time.Time) error {
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
