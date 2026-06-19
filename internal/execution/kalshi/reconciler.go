package kalshi

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const kalshiReconcilePageSize = 1000

// ReconcilerDeps configures the read-only Kalshi reconciliation check.
type ReconcilerDeps struct {
	Broker       execution.Broker
	PositionRepo repository.PositionRepository
	Logger       *slog.Logger
}

// DriftRecord describes one read-only reconciliation mismatch.
type DriftRecord struct {
	Kind           string  `json:"kind"`
	Key            string  `json:"key"`
	Ticker         string  `json:"ticker"`
	Side           string  `json:"side"`
	BrokerQuantity float64 `json:"broker_quantity"`
	LocalQuantity  float64 `json:"local_quantity"`
	Description    string  `json:"description"`
}

// Result summarizes a reconciliation run.
type Result struct {
	BrokerPositions  int           `json:"broker_positions"`
	LocalPositions   int           `json:"local_positions"`
	MatchedPositions int           `json:"matched_positions"`
	DriftCount       int           `json:"drift_count"`
	Drifts           []DriftRecord `json:"drifts"`
}

// Reconciler compares live broker positions to local open Kalshi positions.
type Reconciler struct {
	broker       execution.Broker
	positionRepo repository.PositionRepository
	logger       *slog.Logger
}

// NewReconciler constructs a read-only Kalshi reconciler.
func NewReconciler(deps ReconcilerDeps) *Reconciler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{broker: deps.Broker, positionRepo: deps.PositionRepo, logger: logger}
}

// Check compares broker and local Kalshi positions without mutating state.
func (r *Reconciler) Check(ctx context.Context) (Result, error) {
	if r == nil || r.broker == nil {
		return Result{}, fmt.Errorf("kalshi_reconciler: broker is required")
	}
	if r.positionRepo == nil {
		return Result{}, fmt.Errorf("kalshi_reconciler: position repository is required")
	}

	brokerPositions, err := r.broker.GetPositions(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("kalshi_reconciler: get broker positions: %w", err)
	}

	localPositions, err := r.fetchOpenKalshiPositions(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("kalshi_reconciler: get local positions: %w", err)
	}

	result := Result{BrokerPositions: len(brokerPositions), LocalPositions: len(localPositions)}
	brokerIndex := aggregateKalshiPositions(brokerPositions)
	localIndex := aggregateKalshiPositions(localPositions)
	keys := make([]string, 0, len(brokerIndex)+len(localIndex))
	seen := make(map[string]struct{}, len(brokerIndex)+len(localIndex))
	for key := range brokerIndex {
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	for key := range localIndex {
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	for _, key := range keys {
		brokerPos, brokerOK := brokerIndex[key]
		localPos, localOK := localIndex[key]
		switch {
		case brokerOK && localOK:
			if quantitiesEqual(brokerPos.Quantity, localPos.Quantity) {
				result.MatchedPositions++
				continue
			}
			result.addDrift(DriftRecord{
				Kind:           "quantity_mismatch",
				Key:            key,
				Ticker:         brokerPos.Ticker,
				Side:           brokerPos.Side,
				BrokerQuantity: brokerPos.Quantity,
				LocalQuantity:  localPos.Quantity,
				Description:    fmt.Sprintf("quantity mismatch for %s: broker %.6g vs local %.6g", key, brokerPos.Quantity, localPos.Quantity),
			})
		case brokerOK:
			result.addDrift(DriftRecord{
				Kind:           "broker_missing_locally",
				Key:            key,
				Ticker:         brokerPos.Ticker,
				Side:           brokerPos.Side,
				BrokerQuantity: brokerPos.Quantity,
				LocalQuantity:  0,
				Description:    fmt.Sprintf("broker position missing locally for %s", key),
			})
		case localOK:
			result.addDrift(DriftRecord{
				Kind:           "local_missing_on_broker",
				Key:            key,
				Ticker:         localPos.Ticker,
				Side:           localPos.Side,
				BrokerQuantity: 0,
				LocalQuantity:  localPos.Quantity,
				Description:    fmt.Sprintf("local position missing on broker for %s", key),
			})
		}
	}

	if r.logger != nil && result.DriftCount > 0 {
		r.logger.Debug("kalshi reconciliation drift detected", "broker_positions", result.BrokerPositions, "local_positions", result.LocalPositions, "drifts", result.DriftCount)
	}

	return result, nil
}

func (r *Result) addDrift(record DriftRecord) {
	r.Drifts = append(r.Drifts, record)
	r.DriftCount++
}

func (r *Reconciler) fetchOpenKalshiPositions(ctx context.Context) ([]domain.Position, error) {
	var all []domain.Position
	for offset := 0; ; offset += kalshiReconcilePageSize {
		page, err := r.positionRepo.GetOpen(ctx, repository.PositionFilter{}, kalshiReconcilePageSize, offset)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}
		all = append(all, page...)
		if len(page) < kalshiReconcilePageSize {
			break
		}
	}
	return filterOpenKalshiPositions(all), nil
}

func filterOpenKalshiPositions(positions []domain.Position) []domain.Position {
	filtered := make([]domain.Position, 0, len(positions))
	for _, position := range positions {
		if position.MarketType.Normalize() != domain.MarketTypeKalshi {
			continue
		}
		if normalizeTicker(position.Ticker) == "" {
			continue
		}
		if normalizePositionSide(position.Side) == "" {
			continue
		}
		filtered = append(filtered, position)
	}
	return filtered
}

type kalshiAggregate struct {
	Ticker   string
	Side     string
	Quantity float64
}

func aggregateKalshiPositions(positions []domain.Position) map[string]kalshiAggregate {
	result := make(map[string]kalshiAggregate, len(positions))
	for _, position := range positions {
		ticker := normalizeTicker(position.Ticker)
		side := normalizePositionSide(position.Side)
		if ticker == "" || side == "" {
			continue
		}
		key := kalshiPositionKey(ticker, side)
		aggregate := result[key]
		aggregate.Ticker = ticker
		aggregate.Side = side
		aggregate.Quantity += position.Quantity
		result[key] = aggregate
	}
	return result
}

func kalshiPositionKey(ticker, side string) string {
	return ticker + "|" + side
}

func normalizeTicker(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

func normalizePositionSide(side domain.PositionSide) string {
	s := strings.ToLower(strings.TrimSpace(side.String()))
	switch s {
	case domain.PositionSideLong.String(), domain.PositionSideShort.String():
		return s
	default:
		return ""
	}
}

func quantitiesEqual(left, right float64) bool {
	return math.Abs(left-right) <= 1e-9
}
