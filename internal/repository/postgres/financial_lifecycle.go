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

// ApplyOptionFills atomically persists a complete single-leg fill or all legs
// of a spread. Each order receives a stable idempotency record in the existing
// financial-fill ledger, and a mixed partial replay is rejected.
func (db *DB) ApplyOptionFills(ctx context.Context, inputs []repository.OptionFillInput) ([]repository.OptionFillResult, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("postgres: option fills are required")
	}
	seenOrders := make(map[uuid.UUID]struct{}, len(inputs))
	for _, input := range inputs {
		if err := validateOptionFillInput(input); err != nil {
			return nil, err
		}
		if _, exists := seenOrders[input.Order.ID]; exists {
			return nil, fmt.Errorf("postgres: duplicate option fill order %s", input.Order.ID)
		}
		seenOrders[input.Order.ID] = struct{}{}
	}

	tx, err := db.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("postgres: begin option fill tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	results := make([]repository.OptionFillResult, len(inputs))
	replayed := 0
	for index, input := range inputs {
		key := "option_fill:v1:" + input.Order.ID.String()
		var existingOrderID uuid.UUID
		var existingPositionID *uuid.UUID
		var existingTradeID uuid.UUID
		var existingQuantity, existingPrice, existingFee, existingPremium float64
		var existingFilledAt time.Time
		var existingExitReason string
		err := tx.QueryRow(ctx, `SELECT f.order_id, f.position_id, f.trade_id, f.fill_quantity, f.fill_price,
			COALESCE(t.fee,0)::double precision, COALESCE(t.premium,0)::double precision,
			t.executed_at, COALESCE(t.exit_reason,'')
			FROM financial_fill_idempotency f JOIN trades t ON t.id=f.trade_id
			WHERE f.idempotency_key=$1 FOR UPDATE OF f`, key).Scan(
			&existingOrderID, &existingPositionID, &existingTradeID, &existingQuantity, &existingPrice,
			&existingFee, &existingPremium, &existingFilledAt, &existingExitReason,
		)
		switch {
		case err == nil:
			positionMismatch := input.PositionID != nil && (existingPositionID == nil || *existingPositionID != *input.PositionID)
			if existingOrderID != input.Order.ID || existingPositionID == nil || positionMismatch || !numeric8Equal(existingQuantity, input.FillQuantity) || !numeric8Equal(existingPrice, input.FillPrice) || !numeric8Equal(existingFee, input.Fee) || !numeric8Equal(existingPremium, input.Premium) || !existingFilledAt.Equal(input.FilledAt.UTC()) || existingExitReason != strings.TrimSpace(input.ExitReason) {
				return nil, fmt.Errorf("postgres: option fill idempotency mismatch for order %s", input.Order.ID)
			}
			results[index] = repository.OptionFillResult{OrderID: existingOrderID, PositionID: *existingPositionID, TradeID: existingTradeID}
			replayed++
		case errors.Is(err, pgx.ErrNoRows):
			// Persisted below after the batch replay state is known.
		default:
			return nil, fmt.Errorf("postgres: select option fill idempotency: %w", err)
		}
	}
	if replayed != 0 {
		if replayed != len(inputs) {
			return nil, fmt.Errorf("postgres: partial option fill replay detected")
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("postgres: commit replayed option fills: %w", err)
		}
		return results, nil
	}

	for index, input := range inputs {
		result, err := applyOptionFillTx(ctx, tx, input)
		if err != nil {
			return nil, err
		}
		results[index] = result
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres: commit option fills: %w", err)
	}
	return results, nil
}

func validateOptionFillInput(input repository.OptionFillInput) error {
	order := input.Order
	if order == nil || order.ID == uuid.Nil || order.StrategyID == nil || order.Ticker == "" || strings.TrimSpace(order.ExternalID) == "" || strings.TrimSpace(order.Broker) == "" || order.SubmittedAt == nil || order.MarketType.Normalize() != domain.MarketTypeOptions || order.AssetClass != domain.AssetClassOption || order.PositionIntent == nil {
		return fmt.Errorf("postgres: invalid option fill input")
	}
	if order.Status != domain.OrderStatusFilled || order.FilledAvgPrice == nil || order.FilledAt == nil || !numeric8Equal(order.Quantity, input.FillQuantity) || !numeric8Equal(order.FilledQuantity, input.FillQuantity) || !numeric8Equal(*order.FilledAvgPrice, input.FillPrice) || !order.FilledAt.UTC().Equal(input.FilledAt.UTC()) || order.SubmittedAt.After(*order.FilledAt) {
		return fmt.Errorf("postgres: option order does not contain the reported fill")
	}
	if input.FilledAt.IsZero() || input.FillQuantity <= 0 || input.FillPrice < 0 || input.Fee < 0 || input.Premium < 0 || math.IsNaN(input.FillQuantity) || math.IsInf(input.FillQuantity, 0) || math.IsNaN(input.FillPrice) || math.IsInf(input.FillPrice, 0) || math.IsNaN(input.Fee) || math.IsInf(input.Fee, 0) || math.IsNaN(input.Premium) || math.IsInf(input.Premium, 0) {
		return fmt.Errorf("postgres: invalid option fill accounting")
	}
	validOptionType := order.OptionType != nil && (*order.OptionType == domain.OptionTypeCall || *order.OptionType == domain.OptionTypePut)
	if !validOptionType || order.Strike == nil || *order.Strike <= 0 || math.IsNaN(*order.Strike) || math.IsInf(*order.Strike, 0) || order.Expiry == nil || order.Expiry.IsZero() || strings.TrimSpace(order.UnderlyingTicker) == "" || order.ContractMultiplier <= 0 || math.IsNaN(order.ContractMultiplier) || math.IsInf(order.ContractMultiplier, 0) || !numeric8Equal(input.Premium, input.FillPrice*input.FillQuantity*order.ContractMultiplier) {
		return fmt.Errorf("postgres: option fill has incomplete or inconsistent contract accounting")
	}
	switch *order.PositionIntent {
	case domain.PositionIntentBuyToOpen:
		if order.Side != domain.OrderSideBuy || input.PositionID != nil || strings.TrimSpace(input.ExitReason) != "" {
			return fmt.Errorf("postgres: invalid buy-to-open fill")
		}
	case domain.PositionIntentSellToOpen:
		if order.Side != domain.OrderSideSell || input.PositionID != nil || strings.TrimSpace(input.ExitReason) != "" {
			return fmt.Errorf("postgres: invalid sell-to-open fill")
		}
	case domain.PositionIntentBuyToClose:
		if order.Side != domain.OrderSideBuy || input.PositionID == nil || strings.TrimSpace(input.ExitReason) == "" {
			return fmt.Errorf("postgres: invalid buy-to-close fill")
		}
	case domain.PositionIntentSellToClose:
		if order.Side != domain.OrderSideSell || input.PositionID == nil || strings.TrimSpace(input.ExitReason) == "" {
			return fmt.Errorf("postgres: invalid sell-to-close fill")
		}
	default:
		return fmt.Errorf("postgres: invalid option position intent")
	}
	return nil
}

func applyOptionFillTx(ctx context.Context, tx pgx.Tx, input repository.OptionFillInput) (repository.OptionFillResult, error) {
	order := input.Order
	var (
		persistedStrategyID         *uuid.UUID
		persistedTicker             string
		persistedMarketType         domain.MarketType
		persistedSide               domain.OrderSide
		persistedStatus             domain.OrderStatus
		persistedQuantity           float64
		persistedAssetClass         domain.AssetClass
		persistedUnderlyingTicker   string
		persistedOptionType         *domain.OptionType
		persistedStrike             *float64
		persistedExpiry             *time.Time
		persistedContractMultiplier float64
		persistedPositionIntent     *domain.PositionIntent
		persistedLegGroupID         *uuid.UUID
	)
	if err := tx.QueryRow(ctx, `SELECT strategy_id,ticker,market_type,side,status,quantity::double precision,asset_class,
		COALESCE(underlying_ticker,''),option_type,strike::double precision,expiry,
		contract_multiplier::double precision,position_intent,leg_group_id
		FROM orders WHERE id=$1 FOR UPDATE`, order.ID).Scan(
		&persistedStrategyID, &persistedTicker, &persistedMarketType, &persistedSide, &persistedStatus,
		&persistedQuantity, &persistedAssetClass, &persistedUnderlyingTicker, &persistedOptionType,
		&persistedStrike, &persistedExpiry, &persistedContractMultiplier, &persistedPositionIntent, &persistedLegGroupID,
	); err != nil {
		return repository.OptionFillResult{}, fmt.Errorf("postgres: lock option order: %w", err)
	}
	switch persistedStatus {
	case domain.OrderStatusPending, domain.OrderStatusSubmitted, domain.OrderStatusPartial:
	default:
		return repository.OptionFillResult{}, fmt.Errorf("postgres: option order %s status %s not fill-compatible", order.ID, persistedStatus)
	}
	metadataMatches := persistedStrategyID != nil && *persistedStrategyID == *order.StrategyID &&
		persistedTicker == order.Ticker && persistedMarketType.Normalize() == domain.MarketTypeOptions &&
		persistedSide == order.Side && numeric8Equal(persistedQuantity, input.FillQuantity) &&
		persistedAssetClass == domain.AssetClassOption && persistedUnderlyingTicker == order.UnderlyingTicker &&
		persistedOptionType != nil && *persistedOptionType == *order.OptionType &&
		persistedStrike != nil && numeric8Equal(*persistedStrike, *order.Strike) &&
		persistedExpiry != nil && persistedExpiry.UTC().Equal(order.Expiry.UTC()) &&
		numeric8Equal(persistedContractMultiplier, order.ContractMultiplier) &&
		persistedPositionIntent != nil && *persistedPositionIntent == *order.PositionIntent &&
		((persistedLegGroupID == nil && order.LegGroupID == nil) || (persistedLegGroupID != nil && order.LegGroupID != nil && *persistedLegGroupID == *order.LegGroupID))
	if !metadataMatches {
		return repository.OptionFillResult{}, fmt.Errorf("postgres: persisted option order %s metadata does not match fill", order.ID)
	}
	filledAt := input.FilledAt.UTC()
	if _, err := tx.Exec(ctx, `UPDATE orders SET external_id=$1, broker=$2, submitted_at=$3,
		filled_quantity=$4, filled_avg_price=$5, status=$6, filled_at=$7 WHERE id=$8`,
		nullString(order.ExternalID), nullString(order.Broker), order.SubmittedAt,
		input.FillQuantity, input.FillPrice, domain.OrderStatusFilled, filledAt, order.ID,
	); err != nil {
		return repository.OptionFillResult{}, fmt.Errorf("postgres: update filled option order: %w", err)
	}

	var positionID uuid.UUID
	openClose := "open"
	if input.PositionID == nil {
		if order.OptionType == nil || order.Strike == nil || order.Expiry == nil || order.UnderlyingTicker == "" || order.ContractMultiplier <= 0 {
			return repository.OptionFillResult{}, fmt.Errorf("postgres: opening option fill lacks contract metadata")
		}
		positionSide := domain.PositionSideLong
		if *order.PositionIntent == domain.PositionIntentSellToOpen {
			positionSide = domain.PositionSideShort
		}
		positionID = uuid.New()
		var delta, gamma, theta, vega *float64
		if order.OptionGreeks != nil {
			delta, gamma, theta, vega = &order.OptionGreeks.Delta, &order.OptionGreeks.Gamma, &order.OptionGreeks.Theta, &order.OptionGreeks.Vega
		}
		if _, err := tx.Exec(ctx, `INSERT INTO positions
			(id,strategy_id,ticker,side,quantity,avg_entry,opened_at,asset_class,underlying_ticker,option_type,strike,expiry,contract_multiplier,leg_group_id,delta,gamma,theta,vega)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
			positionID, order.StrategyID, order.Ticker, positionSide, input.FillQuantity, input.FillPrice, filledAt,
			domain.AssetClassOption, order.UnderlyingTicker, order.OptionType, order.Strike, order.Expiry,
			order.ContractMultiplier, order.LegGroupID, delta, gamma, theta, vega,
		); err != nil {
			return repository.OptionFillResult{}, fmt.Errorf("postgres: create option position: %w", err)
		}
	} else {
		openClose = "close"
		positionID = *input.PositionID
		var (
			strategyID         *uuid.UUID
			ticker             string
			side               domain.PositionSide
			quantity           float64
			avgEntry           float64
			realizedPnL        float64
			contractMultiplier float64
			assetClass         domain.AssetClass
			closedAt           *time.Time
			underlyingTicker   string
			optionType         *domain.OptionType
			strike             *float64
			expiry             *time.Time
			legGroupID         *uuid.UUID
		)
		if err := tx.QueryRow(ctx, `SELECT strategy_id,ticker,side,quantity::double precision,avg_entry::double precision,
			COALESCE(realized_pnl,0)::double precision,COALESCE(NULLIF(contract_multiplier,0),100)::double precision,asset_class,closed_at,
			COALESCE(underlying_ticker,''),option_type,strike::double precision,expiry,leg_group_id
			FROM positions WHERE id=$1 FOR UPDATE`, positionID).Scan(
			&strategyID, &ticker, &side, &quantity, &avgEntry, &realizedPnL, &contractMultiplier, &assetClass, &closedAt,
			&underlyingTicker, &optionType, &strike, &expiry, &legGroupID,
		); err != nil {
			return repository.OptionFillResult{}, fmt.Errorf("postgres: lock option close position: %w", err)
		}
		contractMatches := optionType != nil && *optionType == *order.OptionType &&
			strike != nil && numeric8Equal(*strike, *order.Strike) &&
			expiry != nil && expiry.UTC().Equal(order.Expiry.UTC()) &&
			numeric8Equal(contractMultiplier, order.ContractMultiplier) &&
			((legGroupID == nil && order.LegGroupID == nil) || (legGroupID != nil && order.LegGroupID != nil && *legGroupID == *order.LegGroupID))
		if strategyID == nil || *strategyID != *order.StrategyID || ticker != order.Ticker || underlyingTicker != order.UnderlyingTicker || assetClass != domain.AssetClassOption || closedAt != nil || !numeric8Equal(quantity, input.FillQuantity) || !contractMatches {
			return repository.OptionFillResult{}, fmt.Errorf("postgres: option close position does not match full fill")
		}
		realizedDelta := (input.FillPrice - avgEntry) * quantity * contractMultiplier
		if *order.PositionIntent == domain.PositionIntentBuyToClose {
			if side != domain.PositionSideShort {
				return repository.OptionFillResult{}, fmt.Errorf("postgres: buy-to-close requires a short option position")
			}
			realizedDelta = (avgEntry - input.FillPrice) * quantity * contractMultiplier
		} else if side != domain.PositionSideLong {
			return repository.OptionFillResult{}, fmt.Errorf("postgres: sell-to-close requires a long option position")
		}
		if _, err := tx.Exec(ctx, `UPDATE positions SET quantity=0,current_price=$1,realized_pnl=$2,
			unrealized_pnl=NULL,closed_at=$3 WHERE id=$4`, input.FillPrice, realizedPnL+realizedDelta-input.Fee, filledAt, positionID); err != nil {
			return repository.OptionFillResult{}, fmt.Errorf("postgres: close option position: %w", err)
		}
	}

	tradeID := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO trades
		(id,external_id,order_id,position_id,ticker,side,quantity,price,fee,executed_at,created_at,asset_class,open_close,contract_multiplier,premium,exit_reason)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10,$11,$12,$13,$14,$15)`,
		tradeID, nullString(order.ExternalID), order.ID, positionID, order.Ticker, order.Side,
		input.FillQuantity, input.FillPrice, input.Fee, filledAt, domain.AssetClassOption, openClose,
		order.ContractMultiplier, input.Premium, nullString(strings.TrimSpace(input.ExitReason)),
	); err != nil {
		return repository.OptionFillResult{}, fmt.Errorf("postgres: create option fill trade: %w", err)
	}
	key := "option_fill:v1:" + order.ID.String()
	if _, err := tx.Exec(ctx, `INSERT INTO financial_fill_idempotency
		(idempotency_key,order_id,position_id,trade_id,fill_quantity,fill_price) VALUES ($1,$2,$3,$4,$5,$6)`,
		key, order.ID, positionID, tradeID, input.FillQuantity, input.FillPrice,
	); err != nil {
		return repository.OptionFillResult{}, fmt.Errorf("postgres: finalize option fill idempotency: %w", err)
	}
	return repository.OptionFillResult{OrderID: order.ID, PositionID: positionID, TradeID: tradeID}, nil
}

// SettleOptionPosition atomically closes one expired option position and
// creates its linked cash-settlement trade. The locked database row is the
// source of truth for quantity, side, entry price, and contract multiplier.
func (db *DB) SettleOptionPosition(ctx context.Context, input repository.OptionPositionSettlementInput) (repository.OptionPositionSettlementResult, error) {
	if input.PositionID == uuid.Nil || input.SettledAt.IsZero() || input.SettlementPrice < 0 || math.IsNaN(input.SettlementPrice) || math.IsInf(input.SettlementPrice, 0) {
		return repository.OptionPositionSettlementResult{}, fmt.Errorf("postgres: invalid option settlement input")
	}
	if (input.SettlementPrice == 0 && input.ExitReason != "expired_worthless") || (input.SettlementPrice > 0 && input.ExitReason != "exercise_cash_settled") {
		return repository.OptionPositionSettlementResult{}, fmt.Errorf("postgres: invalid option settlement reason")
	}

	tx, err := db.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return repository.OptionPositionSettlementResult{}, fmt.Errorf("postgres: begin option settlement tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		ticker             string
		side               domain.PositionSide
		quantity           float64
		avgEntry           float64
		realizedPnL        float64
		contractMultiplier float64
		closedAt           *time.Time
		assetClass         domain.AssetClass
		expiry             *time.Time
	)
	if err := tx.QueryRow(ctx, `SELECT ticker, side, quantity::double precision, avg_entry::double precision,
		COALESCE(realized_pnl, 0)::double precision, COALESCE(NULLIF(contract_multiplier, 0), 100)::double precision,
		closed_at, asset_class, expiry
		FROM positions WHERE id = $1 FOR UPDATE`, input.PositionID).Scan(
		&ticker, &side, &quantity, &avgEntry, &realizedPnL, &contractMultiplier, &closedAt, &assetClass, &expiry,
	); err != nil {
		return repository.OptionPositionSettlementResult{}, fmt.Errorf("postgres: lock option settlement position: %w", err)
	}
	if assetClass != domain.AssetClassOption || closedAt != nil || quantity <= 0 || expiry == nil {
		return repository.OptionPositionSettlementResult{}, fmt.Errorf("postgres: option settlement position is not eligible")
	}
	settledAt := input.SettledAt.UTC()
	settlementDay := time.Date(settledAt.Year(), settledAt.Month(), settledAt.Day(), 0, 0, 0, 0, time.UTC)
	expiryDay := time.Date(expiry.UTC().Year(), expiry.UTC().Month(), expiry.UTC().Day(), 0, 0, 0, 0, time.UTC)
	if expiryDay.After(settlementDay) {
		return repository.OptionPositionSettlementResult{}, fmt.Errorf("postgres: option settlement position has not expired")
	}

	realizedDelta := (input.SettlementPrice - avgEntry) * quantity * contractMultiplier
	tradeSide := domain.OrderSideSell
	if side == domain.PositionSideShort {
		realizedDelta = (avgEntry - input.SettlementPrice) * quantity * contractMultiplier
		tradeSide = domain.OrderSideBuy
	} else if side != domain.PositionSideLong {
		return repository.OptionPositionSettlementResult{}, fmt.Errorf("postgres: option settlement position has invalid side")
	}
	if _, err := tx.Exec(ctx, `UPDATE positions
		SET quantity = 0, current_price = $1, realized_pnl = $2, unrealized_pnl = NULL, closed_at = $3
		WHERE id = $4`, input.SettlementPrice, realizedPnL+realizedDelta, settledAt, input.PositionID); err != nil {
		return repository.OptionPositionSettlementResult{}, fmt.Errorf("postgres: close option settlement position: %w", err)
	}

	tradeID := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO trades
		(id, position_id, ticker, side, quantity, price, executed_at, created_at, asset_class, open_close, contract_multiplier, premium, exit_reason)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$7,$8,'close',$9,$10,$11)`,
		tradeID, input.PositionID, ticker, tradeSide, quantity, input.SettlementPrice, settledAt,
		domain.AssetClassOption, contractMultiplier, input.SettlementPrice*quantity*contractMultiplier, input.ExitReason,
	); err != nil {
		return repository.OptionPositionSettlementResult{}, fmt.Errorf("postgres: create option settlement trade: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return repository.OptionPositionSettlementResult{}, fmt.Errorf("postgres: commit option settlement: %w", err)
	}
	return repository.OptionPositionSettlementResult{PositionID: input.PositionID, TradeID: tradeID}, nil
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
