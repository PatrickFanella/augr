package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const (
	polymarketPositionDriftEventType = "polymarket_position_drift"
	reconcilePositionPageSize        = 1000
)

type ReconcilerMetrics interface {
	IncDrift(driftType string)
}

type ReconcilerDeps struct {
	Broker       execution.Broker
	PositionRepo repository.PositionRepository
	AuditLogRepo repository.AuditLogRepository
	Metrics      ReconcilerMetrics
	Logger       *slog.Logger
}

type ReconcileSummary struct {
	BrokerPositions int
	LocalPositions  int
	Drifts          int
}

// Map returns the summary as a status-friendly map.
func (s ReconcileSummary) Map() map[string]int {
	return map[string]int{
		"broker_positions": s.BrokerPositions,
		"local_positions":  s.LocalPositions,
		"drifts":           s.Drifts,
	}
}

type reconciledPosition struct {
	key      string
	slug     string
	side     string
	quantity float64
	count    int
}

type Reconciler struct {
	broker       execution.Broker
	positionRepo repository.PositionRepository
	auditLogRepo repository.AuditLogRepository
	metrics      ReconcilerMetrics
	logger       *slog.Logger

	mu   sync.Mutex
	seen map[string]struct{}
}

func NewReconciler(deps ReconcilerDeps) *Reconciler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{
		broker:       deps.Broker,
		positionRepo: deps.PositionRepo,
		auditLogRepo: deps.AuditLogRepo,
		metrics:      deps.Metrics,
		logger:       logger,
		seen:         make(map[string]struct{}),
	}
}

func (r *Reconciler) Reconcile(ctx context.Context) (ReconcileSummary, error) {
	if r == nil || r.broker == nil {
		return ReconcileSummary{}, fmt.Errorf("polymarket_reconcile: broker is required")
	}
	if r.positionRepo == nil {
		return ReconcileSummary{}, fmt.Errorf("polymarket_reconcile: position repository is required")
	}

	brokerPositions, err := r.broker.GetPositions(ctx)
	if err != nil {
		return ReconcileSummary{}, fmt.Errorf("polymarket_reconcile: fetch broker positions: %w", err)
	}
	localPositions, err := r.fetchAllOpenPositions(ctx)
	if err != nil {
		return ReconcileSummary{}, fmt.Errorf("polymarket_reconcile: fetch local positions: %w", err)
	}

	summary := ReconcileSummary{BrokerPositions: len(brokerPositions), LocalPositions: len(localPositions)}
	brokerIndex := aggregatePolymarketPositions(brokerPositions)
	localIndex := aggregateLocalPolymarketPositions(localPositions)

	for key, local := range localIndex {
		broker, ok := brokerIndex[key]
		if !ok {
			if err := r.recordDrift(ctx, "local_missing_externally", key, local.slug, local.side, local.quantity, 0); err != nil {
				return summary, err
			}
			summary.Drifts++
			continue
		}
		if !quantitiesClose(local.quantity, broker.quantity) {
			if err := r.recordDrift(ctx, "quantity_mismatch", key, local.slug, local.side, local.quantity, broker.quantity); err != nil {
				return summary, err
			}
			summary.Drifts++
		}
	}

	for key, broker := range brokerIndex {
		if _, ok := localIndex[key]; ok {
			continue
		}
		if err := r.recordDrift(ctx, "external_missing_locally", key, broker.slug, broker.side, 0, broker.quantity); err != nil {
			return summary, err
		}
		summary.Drifts++
	}

	return summary, nil
}

func (r *Reconciler) fetchAllOpenPositions(ctx context.Context) ([]domain.Position, error) {
	var all []domain.Position
	for offset := 0; ; offset += reconcilePositionPageSize {
		page, err := r.positionRepo.GetOpen(ctx, repository.PositionFilter{}, reconcilePositionPageSize, offset)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < reconcilePositionPageSize {
			return all, nil
		}
	}
}

func aggregatePolymarketPositions(positions []domain.Position) map[string]reconciledPosition {
	result := make(map[string]reconciledPosition, len(positions))
	for _, position := range positions {
		key, slug, side, ok := polymarketPositionKey(position)
		if !ok {
			continue
		}
		current := result[key]
		current.key = key
		current.slug = slug
		current.side = side
		current.quantity += position.Quantity
		current.count++
		result[key] = current
	}
	return result
}

func aggregateLocalPolymarketPositions(positions []domain.Position) map[string]reconciledPosition {
	result := make(map[string]reconciledPosition, len(positions))
	for _, position := range positions {
		if !isLocalPolymarketPosition(position) {
			continue
		}
		key, slug, side, ok := polymarketPositionKey(position)
		if !ok {
			continue
		}
		current := result[key]
		current.key = key
		current.slug = slug
		current.side = side
		current.quantity += position.Quantity
		current.count++
		result[key] = current
	}
	return result
}

func isLocalPolymarketPosition(position domain.Position) bool {
	if position.MarketType.Normalize() == domain.MarketTypePolymarket {
		return true
	}
	_, _, ok := sideQualifiedPolymarketTicker(position.Ticker)
	return ok
}

func polymarketPositionKey(position domain.Position) (key, slug, side string, ok bool) {
	slug, side, ok = polymarketPositionIdentity(position)
	if !ok {
		return "", "", "", false
	}
	return slug + ":" + side, slug, side, true
}

func polymarketPositionIdentity(position domain.Position) (slug, side string, ok bool) {
	ticker := strings.TrimSpace(position.Ticker)
	if ticker == "" {
		return "", "", false
	}

	if qualifiedSlug, qualifiedSide, qualified := sideQualifiedPolymarketTicker(ticker); qualified {
		return qualifiedSlug, qualifiedSide, true
	}

	slug = ticker
	side = polymarketOutcomeFromPositionSide(position.Side)
	if side == "" {
		return "", "", false
	}
	return slug, side, true
}

func sideQualifiedPolymarketTicker(ticker string) (slug, side string, ok bool) {
	idx := strings.LastIndex(ticker, ":")
	if idx < 0 {
		return "", "", false
	}
	slug = strings.TrimSpace(ticker[:idx])
	side = polymarketOutcomeToken(strings.TrimSpace(ticker[idx+1:]))
	if slug == "" || side == "" {
		return "", "", false
	}
	return slug, side, true
}

func polymarketOutcomeToken(token string) string {
	switch strings.ToUpper(strings.TrimSpace(token)) {
	case "YES", "LONG", "BUY", "Y":
		return "YES"
	case "NO", "SHORT", "SELL", "N":
		return "NO"
	default:
		return ""
	}
}

func polymarketOutcomeFromPositionSide(side domain.PositionSide) string {
	switch side {
	case domain.PositionSideLong:
		return "YES"
	case domain.PositionSideShort:
		return "NO"
	default:
		return ""
	}
}

func quantitiesClose(left, right float64) bool {
	return math.Abs(left-right) <= 1e-9
}

func (r *Reconciler) recordDrift(ctx context.Context, driftType, key, slug, side string, localQty, externalQty float64) error {
	if r.metrics != nil {
		r.metrics.IncDrift(driftType)
	}

	signature := driftSignature(driftType, key, localQty, externalQty)
	r.mu.Lock()
	if _, seen := r.seen[signature]; seen {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	if r.auditLogRepo == nil {
		return nil
	}

	details := map[string]any{
		"drift_type":        driftType,
		"key":               key,
		"slug":              slug,
		"side":              side,
		"local_quantity":    localQty,
		"external_quantity": externalQty,
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("polymarket_reconcile: marshal audit details: %w", err)
	}

	if err := r.auditLogRepo.Create(ctx, &domain.AuditLogEntry{
		EventType:  polymarketPositionDriftEventType,
		EntityType: "polymarket_position",
		Actor:      "polymarket_reconciler",
		Details:    payload,
	}); err != nil {
		return fmt.Errorf("polymarket_reconcile: create audit log: %w", err)
	}

	r.mu.Lock()
	r.seen[signature] = struct{}{}
	r.mu.Unlock()

	return nil
}

func driftSignature(driftType, key string, localQty, externalQty float64) string {
	return fmt.Sprintf("%s|%s|%g|%g", driftType, key, localQty, externalQty)
}
