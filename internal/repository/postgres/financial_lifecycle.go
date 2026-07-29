package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func polymarketPositionTicker(slug, side string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	side = strings.ToUpper(strings.TrimSpace(side))
	if side == "" {
		return slug
	}
	if existingSlug, existingSide, ok := strings.Cut(slug, ":"); ok {
		existingSlug = strings.TrimSpace(existingSlug)
		existingSide = strings.ToUpper(strings.TrimSpace(existingSide))
		if existingSlug != "" && (existingSide == "YES" || existingSide == "NO") {
			return existingSlug + ":" + existingSide
		}
	}
	return slug + ":" + side
}

func normalizedPositionTicker(marketType domain.MarketType, ticker, predictionSide string) string {
	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return ""
	}
	switch marketType.Normalize() {
	case domain.MarketTypePolymarket, domain.MarketTypeKalshi:
		return polymarketPositionTicker(ticker, predictionSide)
	default:
		return ticker
	}
}

func realizedPnL(side domain.PositionSide, avgEntry, fillPrice, quantity float64) float64 {
	if side == domain.PositionSideLong {
		return (fillPrice - avgEntry) * quantity
	}
	return (avgEntry - fillPrice) * quantity
}

func (db *DB) ApplyOrderFill(ctx context.Context, input repository.OrderFillInput) (repository.OrderFillResult, error) {
	if input.Order == nil || input.Order.ID == uuid.Nil || input.Order.StrategyID == nil || input.Order.Ticker == "" || input.Order.MarketType == "" || input.Trade == nil || input.IdempotencyKey == "" || input.FillIntent.Quantity <= 0 || math.IsNaN(input.FillIntent.ExecutionPrice) || math.IsInf(input.FillIntent.ExecutionPrice, 0) || input.FillIntent.ExecutionPrice < 0 {
		return repository.OrderFillResult{}, fmt.Errorf("postgres: invalid order fill input")
	}
	if input.Order.Quantity <= 0 || input.Order.Side == "" || input.Order.Status == "" {
		return repository.OrderFillResult{}, fmt.Errorf("postgres: invalid order fill input")
	}
	if input.Now.IsZero() {
		return repository.OrderFillResult{}, fmt.Errorf("postgres: invalid order fill input")
	}

	tx, err := db.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return repository.OrderFillResult{}, fmt.Errorf("postgres: begin order fill tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var existingOrderID uuid.UUID
	var existingPositionID *uuid.UUID
	var existingTradeID uuid.UUID
	var existingFillQuantity float64
	var existingFillPrice float64
	var existingCreatedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT order_id, position_id, trade_id, fill_quantity, fill_price, created_at FROM financial_fill_idempotency WHERE idempotency_key = $1 FOR UPDATE`, input.IdempotencyKey).Scan(&existingOrderID, &existingPositionID, &existingTradeID, &existingFillQuantity, &existingFillPrice, &existingCreatedAt); err == nil {
		if existingOrderID != input.Order.ID || !numeric8Equal(existingFillQuantity, input.FillIntent.Quantity) || !numeric8Equal(existingFillPrice, input.FillIntent.ExecutionPrice) {
			return repository.OrderFillResult{}, fmt.Errorf("postgres: idempotency key %s reused with mismatched payload", input.IdempotencyKey)
		}
		if err := tx.Commit(ctx); err != nil {
			return repository.OrderFillResult{}, fmt.Errorf("postgres: commit replayed order fill: %w", err)
		}
		return repository.OrderFillResult{OrderID: existingOrderID, PositionID: existingPositionID, TradeID: existingTradeID, CreatedAt: existingCreatedAt, Replayed: true}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return repository.OrderFillResult{}, fmt.Errorf("postgres: select order fill idempotency: %w", err)
	}

	var persistedStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1 FOR UPDATE`, input.Order.ID).Scan(&persistedStatus); err != nil {
		return repository.OrderFillResult{}, fmt.Errorf("postgres: lock order: %w", err)
	}
	if persistedStatus != string(domain.OrderStatusSubmitted) && persistedStatus != string(domain.OrderStatusPartial) && persistedStatus != string(domain.OrderStatusFilled) {
		return repository.OrderFillResult{}, fmt.Errorf("postgres: order %s status %s not fill-compatible", input.Order.ID, persistedStatus)
	}
	order := input.Order
	fillPrice := input.FillIntent.ExecutionPrice
	if fillPrice == 0 && order.FilledAvgPrice != nil {
		fillPrice = *order.FilledAvgPrice
	}

	order.FilledQuantity = input.FillIntent.Quantity
	now := input.Now.UTC()
	order.FilledAt = &now
	order.Status = domain.OrderStatusFilled
	if _, err := tx.Exec(ctx, `UPDATE orders SET filled_quantity = $1, filled_avg_price = $2, status = $3, filled_at = $4 WHERE id = $5`, order.FilledQuantity, fillPrice, order.Status, order.FilledAt, order.ID); err != nil {
		return repository.OrderFillResult{}, fmt.Errorf("postgres: update filled order: %w", err)
	}

	marketType := order.MarketType.Normalize()
	if marketType == "" {
		marketType = domain.MarketTypeStock
	}
	positionTicker := normalizedPositionTicker(marketType, order.Ticker, order.PredictionSide)

	var position *domain.Position
	var positionID *uuid.UUID
	if order.Side == domain.OrderSideSell {
		rows, err := tx.Query(ctx, `SELECT p.id, p.strategy_id, s.market_type, p.ticker, p.side, p.quantity::double precision, p.avg_entry::double precision,
			p.current_price::double precision, p.unrealized_pnl::double precision, p.realized_pnl::double precision, p.stop_loss::double precision,
			p.take_profit::double precision, p.opened_at, p.closed_at, p.asset_class, p.underlying_ticker, p.option_type, p.strike::double precision,
			p.expiry, p.contract_multiplier::double precision, p.leg_group_id, p.delta::double precision, p.gamma::double precision, p.theta::double precision, p.vega::double precision
			FROM positions p LEFT JOIN strategies s ON s.id = p.strategy_id
			WHERE p.strategy_id = $1 AND p.ticker = $2 AND p.side = $3 AND p.closed_at IS NULL AND p.quantity > 0
			ORDER BY p.opened_at ASC, p.id ASC FOR UPDATE OF p`, *order.StrategyID, positionTicker, domain.PositionSideLong)
		if err != nil {
			return repository.OrderFillResult{}, fmt.Errorf("postgres: lock polymarket position: %w", err)
		}
		defer rows.Close()
		var (
			matchedPositions []*domain.Position
			totalAvailable   float64
		)
		for rows.Next() {
			matchedPosition, scanErr := scanPosition(rows)
			if scanErr != nil {
				return repository.OrderFillResult{}, fmt.Errorf("postgres: scan polymarket position: %w", scanErr)
			}
			totalAvailable += matchedPosition.Quantity
			matchedPositions = append(matchedPositions, matchedPosition)
		}
		if err := rows.Err(); err != nil {
			return repository.OrderFillResult{}, fmt.Errorf("postgres: iterate polymarket positions: %w", err)
		}
		if len(matchedPositions) == 0 {
			return repository.OrderFillResult{}, fmt.Errorf("postgres: sell fill has no open position for %s", positionTicker)
		}
		if totalAvailable < input.FillIntent.Quantity {
			return repository.OrderFillResult{}, fmt.Errorf("postgres: polymarket sell fill quantity %.8f exceeds open long quantity %.8f for %s", input.FillIntent.Quantity, totalAvailable, positionTicker)
		}

		remaining := input.FillIntent.Quantity
		updatedIDs := make([]uuid.UUID, 0, len(matchedPositions))
		closedIDs := make([]uuid.UUID, 0, len(matchedPositions))
		if len(matchedPositions) == 1 {
			position = matchedPositions[0]
			positionID = &position.ID
		}
		for _, matchedPosition := range matchedPositions {
			if remaining <= 0 {
				break
			}
			consume := math.Min(matchedPosition.Quantity, remaining)
			currentPrice := fillPrice
			matchedPosition.CurrentPrice = &currentPrice
			matchedPosition.RealizedPnL += realizedPnL(matchedPosition.Side, matchedPosition.AvgEntry, fillPrice, consume)
			matchedPosition.Quantity -= consume
			if matchedPosition.Quantity == 0 {
				closedAt := now
				matchedPosition.ClosedAt = &closedAt
				closedIDs = append(closedIDs, matchedPosition.ID)
			}
			if _, err := tx.Exec(ctx, `UPDATE positions SET quantity = $1, current_price = $2, realized_pnl = $3, closed_at = $4 WHERE id = $5`, matchedPosition.Quantity, matchedPosition.CurrentPrice, matchedPosition.RealizedPnL, matchedPosition.ClosedAt, matchedPosition.ID); err != nil {
				return repository.OrderFillResult{}, fmt.Errorf("postgres: update polymarket position: %w", err)
			}
			updatedIDs = append(updatedIDs, matchedPosition.ID)
			remaining -= consume
		}
		if remaining > 0 {
			return repository.OrderFillResult{}, fmt.Errorf("postgres: sell fill did not consume enough quantity")
		}
		if len(updatedIDs) > 1 {
			position = nil
			positionID = nil
		}
		if _, err := tx.Exec(ctx, `UPDATE trade_decisions SET status = $1, updated_at = $2
			WHERE status = $3 AND (
				paper_order_id = $4 OR
				(cardinality($5::uuid[]) > 0 AND paper_order_id IN (
					SELECT DISTINCT t.order_id FROM trades t WHERE t.position_id = ANY($5::uuid[])
				))
			)`, domain.TradeDecisionStatusClosed, now, domain.TradeDecisionStatusPaper, order.ID, closedIDs); err != nil {
			return repository.OrderFillResult{}, fmt.Errorf("postgres: close prediction exit decisions: %w", err)
		}
	} else {
		positionSide := domain.PositionSideLong
		position = &domain.Position{ID: uuid.New(), StrategyID: order.StrategyID, MarketType: marketType, Ticker: positionTicker, Side: positionSide, Quantity: input.FillIntent.Quantity, AvgEntry: fillPrice, OpenedAt: now}
		if input.StopLoss != nil {
			position.StopLoss = input.StopLoss
		}
		if input.TakeProfit != nil {
			position.TakeProfit = input.TakeProfit
		}
		if err := tx.QueryRow(ctx, `INSERT INTO positions (id, strategy_id, ticker, side, quantity, avg_entry, stop_loss, take_profit, opened_at, asset_class, underlying_ticker, contract_multiplier)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id, opened_at`, position.ID, position.StrategyID, position.Ticker, position.Side, position.Quantity, position.AvgEntry, position.StopLoss, position.TakeProfit, position.OpenedAt, position.AssetClass, nullString(position.UnderlyingTicker), position.ContractMultiplier).Scan(&position.ID, &position.OpenedAt); err != nil {
			return repository.OrderFillResult{}, fmt.Errorf("postgres: create position: %w", err)
		}
	}

	trade := input.Trade
	trade.OrderID = &order.ID
	if position != nil {
		trade.PositionID = &position.ID
		if positionID == nil {
			positionID = &position.ID
		}
	} else {
		trade.PositionID = nil
	}
	trade.Ticker = order.Ticker
	trade.Side = order.Side
	trade.Quantity = input.FillIntent.Quantity
	trade.Price = fillPrice
	trade.ExecutedAt = now
	trade.CreatedAt = now
	if err := tx.QueryRow(ctx, `INSERT INTO trades (external_id, order_id, position_id, ticker, side, quantity, price, fee, executed_at, asset_class, open_close, contract_multiplier, premium)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id, created_at`, nullString(trade.ExternalID), trade.OrderID, trade.PositionID, trade.Ticker, trade.Side, trade.Quantity, trade.Price, trade.Fee, trade.ExecutedAt, trade.AssetClass, nullString(trade.OpenClose), trade.ContractMultiplier, trade.Premium).Scan(&trade.ID, &trade.CreatedAt); err != nil {
		return repository.OrderFillResult{}, fmt.Errorf("postgres: create trade: %w", err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO financial_fill_idempotency (idempotency_key, order_id, position_id, trade_id, fill_quantity, fill_price) VALUES ($1,$2,$3,$4,$5,$6)`, input.IdempotencyKey, order.ID, positionID, trade.ID, input.FillIntent.Quantity, input.FillIntent.ExecutionPrice); err != nil {
		return repository.OrderFillResult{}, fmt.Errorf("postgres: finalize fill idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return repository.OrderFillResult{}, fmt.Errorf("postgres: commit order fill: %w", err)
	}
	return repository.OrderFillResult{OrderID: order.ID, PositionID: positionID, Position: position, TradeID: trade.ID, CreatedAt: trade.CreatedAt}, nil
}

func (db *DB) SettlePredictionDecision(ctx context.Context, input repository.PredictionDecisionSettlementInput) (repository.PredictionDecisionSettlementResult, error) {
	if input.Decision == nil || input.Decision.ID == uuid.Nil || input.Decision.StrategyID == nil || input.Decision.PaperOrderID == nil || input.IdempotencyKey == "" || input.PositionTicker == "" || input.ResolvedAt.IsZero() || math.IsNaN(input.Payout) || math.IsInf(input.Payout, 0) || input.Payout < 0 || input.Payout > 1 {
		return repository.PredictionDecisionSettlementResult{}, fmt.Errorf("postgres: invalid settlement input")
	}
	tx, err := db.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return repository.PredictionDecisionSettlementResult{}, fmt.Errorf("postgres: begin settlement tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var decisionID uuid.UUID
	var idempotencyKey string
	var positionID *uuid.UUID
	var tradeID uuid.UUID
	var replayEventID *uuid.UUID
	var payout float64
	var resolvedAt time.Time
	var createdAt time.Time
	if err := tx.QueryRow(ctx, `SELECT idempotency_key, decision_id, position_id, trade_id, replay_event_id, payout, resolved_at, created_at FROM prediction_settlement_idempotency WHERE idempotency_key = $1 OR decision_id = $2`, input.IdempotencyKey, input.Decision.ID).Scan(&idempotencyKey, &decisionID, &positionID, &tradeID, &replayEventID, &payout, &resolvedAt, &createdAt); err == nil {
		if idempotencyKey != input.IdempotencyKey || decisionID != input.Decision.ID || math.IsNaN(payout) || math.IsInf(payout, 0) || !numeric8Equal(payout, input.Payout) || !resolvedAt.Equal(input.ResolvedAt.UTC()) {
			return repository.PredictionDecisionSettlementResult{}, fmt.Errorf("postgres: idempotency mismatch for decision %s", input.Decision.ID)
		}
		if err := tx.Commit(ctx); err != nil {
			return repository.PredictionDecisionSettlementResult{}, fmt.Errorf("postgres: commit replayed settlement: %w", err)
		}
		return repository.PredictionDecisionSettlementResult{DecisionID: decisionID, PositionID: positionID, TradeID: tradeID, ReplayEventID: replayEventID, CreatedAt: createdAt, Replayed: true}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return repository.PredictionDecisionSettlementResult{}, fmt.Errorf("postgres: select settlement idempotency: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT p.id, p.strategy_id, p.quantity::double precision, p.avg_entry::double precision, p.realized_pnl::double precision FROM positions p INNER JOIN trades t ON t.position_id = p.id AND t.order_id = $1 WHERE p.closed_at IS NULL AND p.quantity > 0 FOR UPDATE OF p`, input.Decision.PaperOrderID)
	if err != nil {
		return repository.PredictionDecisionSettlementResult{}, fmt.Errorf("postgres: lock settlement position: %w", err)
	}
	defer rows.Close()
	var positions []domain.Position
	for rows.Next() {
		var position domain.Position
		var strategyID uuid.UUID
		if err := rows.Scan(&position.ID, &strategyID, &position.Quantity, &position.AvgEntry, &position.RealizedPnL); err != nil {
			return repository.PredictionDecisionSettlementResult{}, fmt.Errorf("postgres: scan settlement position: %w", err)
		}
		position.StrategyID = &strategyID
		positions = append(positions, position)
	}
	if err := rows.Err(); err != nil {
		return repository.PredictionDecisionSettlementResult{}, fmt.Errorf("postgres: iterate settlement positions: %w", err)
	}
	if len(positions) != 1 {
		return repository.PredictionDecisionSettlementResult{}, fmt.Errorf("postgres: expected exactly one open position for decision %s, got %d", input.Decision.ID, len(positions))
	}
	position := positions[0]
	quantity := position.Quantity
	position.Quantity = 0
	position.CurrentPrice = &input.Payout
	position.RealizedPnL += (input.Payout - position.AvgEntry) * quantity
	position.UnrealizedPnL = nil
	closedAt := input.ResolvedAt.UTC()
	position.ClosedAt = &closedAt
	if _, err := tx.Exec(ctx, `UPDATE positions SET quantity = 0, current_price = $1, realized_pnl = $2, unrealized_pnl = NULL, closed_at = $3 WHERE id = $4`, input.Payout, position.RealizedPnL, closedAt, position.ID); err != nil {
		return repository.PredictionDecisionSettlementResult{}, fmt.Errorf("postgres: update position: %w", err)
	}
	tradeID = uuid.New()
	if err := tx.QueryRow(ctx, `INSERT INTO trades (id, order_id, position_id, ticker, side, quantity, price, executed_at, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8) RETURNING id`, tradeID, input.Decision.PaperOrderID, position.ID, input.PositionTicker, domain.OrderSideSell, quantity, input.Payout, closedAt).Scan(&tradeID); err != nil {
		return repository.PredictionDecisionSettlementResult{}, fmt.Errorf("postgres: insert payout trade: %w", err)
	}
	if tag, err := tx.Exec(ctx, `UPDATE trade_decisions SET status = $2, updated_at = NOW() WHERE id = $1 AND status = $3`, input.Decision.ID, domain.TradeDecisionStatusClosed, domain.TradeDecisionStatusPaper); err != nil {
		return repository.PredictionDecisionSettlementResult{}, fmt.Errorf("postgres: update decision: %w", err)
	} else if tag.RowsAffected() != 1 {
		return repository.PredictionDecisionSettlementResult{}, fmt.Errorf("postgres: update decision: expected 1 row, got %d", tag.RowsAffected())
	}
	replayEventID = func() *uuid.UUID { id := uuid.New(); return &id }()
	payload, _ := json.Marshal(map[string]any{"decision_id": input.Decision.ID, "position_id": position.ID, "trade_id": tradeID, "payout": input.Payout, "resolved_at": closedAt, "position_ticker": input.PositionTicker})
	if err := tx.QueryRow(ctx, `INSERT INTO replay_events (id, trade_decision_id, event_type, source, payload, occurred_at) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`, *replayEventID, input.Decision.ID, domain.ReplayEventTypeOutcomeResolved, "prediction_settler", payload, closedAt).Scan(replayEventID); err != nil {
		return repository.PredictionDecisionSettlementResult{}, fmt.Errorf("postgres: insert replay event: %w", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO prediction_settlement_idempotency (idempotency_key, decision_id, position_id, trade_id, replay_event_id, payout, resolved_at) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING created_at`, input.IdempotencyKey, input.Decision.ID, position.ID, tradeID, replayEventID, input.Payout, input.ResolvedAt.UTC()).Scan(&createdAt); err != nil {
		return repository.PredictionDecisionSettlementResult{}, fmt.Errorf("postgres: finalize settlement idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return repository.PredictionDecisionSettlementResult{}, fmt.Errorf("postgres: commit settlement: %w", err)
	}
	return repository.PredictionDecisionSettlementResult{DecisionID: input.Decision.ID, PositionID: &position.ID, TradeID: tradeID, ReplayEventID: replayEventID, CreatedAt: createdAt}, nil
}

func numeric8Equal(left, right float64) bool {
	return math.Round(left*1e8) == math.Round(right*1e8)
}
