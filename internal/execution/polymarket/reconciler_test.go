package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/google/uuid"
)

type reconcilerBrokerStub struct {
	positions []domain.Position
	err       error
}

func (s *reconcilerBrokerStub) SubmitOrder(context.Context, *domain.Order) (string, error) {
	return "", nil
}

func (s *reconcilerBrokerStub) CancelOrder(context.Context, string) error { return nil }

func (s *reconcilerBrokerStub) GetOrderStatus(context.Context, string) (domain.OrderStatus, error) {
	return "", nil
}

func (s *reconcilerBrokerStub) GetPositions(context.Context) ([]domain.Position, error) {
	return append([]domain.Position(nil), s.positions...), s.err
}

func (s *reconcilerBrokerStub) GetAccountBalance(context.Context) (execution.Balance, error) {
	return execution.Balance{}, nil
}

type reconcilerPositionRepoStub struct {
	positions []domain.Position
	err       error
}

func (r *reconcilerPositionRepoStub) Create(context.Context, *domain.Position) error { return nil }
func (r *reconcilerPositionRepoStub) CreateAlpacaOwned(context.Context, *domain.Position) error {
	return nil
}

func (r *reconcilerPositionRepoStub) Get(context.Context, uuid.UUID) (*domain.Position, error) {
	return nil, nil
}

func (r *reconcilerPositionRepoStub) List(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (r *reconcilerPositionRepoStub) Count(context.Context, repository.PositionFilter) (int, error) {
	return 0, nil
}
func (r *reconcilerPositionRepoStub) Update(context.Context, *domain.Position) error { return nil }
func (r *reconcilerPositionRepoStub) Delete(context.Context, uuid.UUID) error        { return nil }
func (r *reconcilerPositionRepoStub) GetOpen(_ context.Context, _ repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
	if r.err != nil {
		return nil, r.err
	}
	if offset >= len(r.positions) {
		return nil, nil
	}
	end := offset + limit
	if limit <= 0 || end > len(r.positions) {
		end = len(r.positions)
	}
	return append([]domain.Position(nil), r.positions[offset:end]...), nil
}

func (r *reconcilerPositionRepoStub) ListOpenAlpacaOwned(context.Context, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (r *reconcilerPositionRepoStub) CountOpen(context.Context, repository.PositionFilter) (int, error) {
	return len(r.positions), nil
}

func (r *reconcilerPositionRepoStub) GetByStrategy(context.Context, uuid.UUID, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}

type reconcilerAuditRepoStub struct {
	mu      sync.Mutex
	entries []*domain.AuditLogEntry
}

func (r *reconcilerAuditRepoStub) Create(_ context.Context, entry *domain.AuditLogEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *entry
	r.entries = append(r.entries, &cp)
	return nil
}

func (r *reconcilerAuditRepoStub) Query(context.Context, repository.AuditLogFilter, int, int) ([]domain.AuditLogEntry, error) {
	return nil, nil
}

func (r *reconcilerAuditRepoStub) Count(context.Context, repository.AuditLogFilter) (int, error) {
	return 0, nil
}

type reconcilerMetricsStub struct {
	mu     sync.Mutex
	drifts map[string]int
}

func (m *reconcilerMetricsStub) IncDrift(driftType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.drifts == nil {
		m.drifts = make(map[string]int)
	}
	m.drifts[driftType]++
}

func TestReconcilerNoDriftSameState(t *testing.T) {
	t.Parallel()

	reconciler, auditRepo, _ := newReconcilerTestHarness([]domain.Position{
		{Ticker: "market-one:YES", Side: domain.PositionSideLong, Quantity: 10},
	}, []domain.Position{
		{MarketType: domain.MarketTypePolymarket, Ticker: "market-one", Side: domain.PositionSideLong, Quantity: 10},
	})

	if _, err := reconciler.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if got := len(auditRepo.entries); got != 0 {
		t.Fatalf("audit entries = %d, want 0", got)
	}
}

func TestReconcilerRepeatedRunDoesNotDuplicateAudit(t *testing.T) {
	t.Parallel()

	reconciler, auditRepo, _ := newReconcilerTestHarness(nil, []domain.Position{
		{Ticker: "market-two:NO", Side: domain.PositionSideLong, Quantity: 3},
	})

	if _, err := reconciler.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	if got := len(auditRepo.entries); got != 1 {
		t.Fatalf("audit entries = %d, want 1", got)
	}
}

func TestReconcilerLocalExtraDrift(t *testing.T) {
	t.Parallel()

	reconciler, auditRepo, metrics := newReconcilerTestHarness([]domain.Position{
		{Ticker: "market-three:YES", Side: domain.PositionSideLong, Quantity: 4},
	}, nil)

	if _, err := reconciler.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	assertSingleDrift(t, auditRepo.entries, "external_missing_locally", "market-three:YES", 0, 4)
	assertMetricCount(t, metrics, "external_missing_locally", 1)
}

func TestReconcilerExternalExtraDrift(t *testing.T) {
	t.Parallel()

	reconciler, auditRepo, _ := newReconcilerTestHarness(nil, []domain.Position{
		{MarketType: domain.MarketTypePolymarket, Ticker: "market-four", Side: domain.PositionSideShort, Quantity: 6},
	})

	if _, err := reconciler.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	assertSingleDrift(t, auditRepo.entries, "local_missing_externally", "market-four:NO", 6, 0)
}

func TestReconcilerQuantityMismatch(t *testing.T) {
	t.Parallel()

	reconciler, auditRepo, _ := newReconcilerTestHarness([]domain.Position{
		{Ticker: "market-five:yes", Side: domain.PositionSideLong, Quantity: 2},
	}, []domain.Position{
		{MarketType: domain.MarketTypePolymarket, Ticker: "market-five", Side: domain.PositionSideLong, Quantity: 3},
	})

	if _, err := reconciler.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	assertSingleDrift(t, auditRepo.entries, "quantity_mismatch", "market-five:YES", 3, 2)
}

func TestReconcilerKeyNormalizationYesNo(t *testing.T) {
	t.Parallel()

	reconciler, auditRepo, _ := newReconcilerTestHarness([]domain.Position{
		{Ticker: "market-six:yes", Side: domain.PositionSideLong, Quantity: 1},
		{Ticker: "market-seven:no", Side: domain.PositionSideLong, Quantity: 2},
	}, []domain.Position{
		{MarketType: domain.MarketTypePolymarket, Ticker: "market-six", Side: domain.PositionSideLong, Quantity: 1},
		{MarketType: domain.MarketTypePolymarket, Ticker: "market-seven", Side: domain.PositionSideShort, Quantity: 2},
	})

	if _, err := reconciler.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if got := len(auditRepo.entries); got != 0 {
		t.Fatalf("audit entries = %d, want 0", got)
	}
}

func TestReconcilerIgnoresNonPolymarketLocalPositions(t *testing.T) {
	t.Parallel()

	reconciler, auditRepo, _ := newReconcilerTestHarness(nil, []domain.Position{
		{MarketType: domain.MarketTypeStock, Ticker: "AAPL", Side: domain.PositionSideLong, Quantity: 10},
		{MarketType: domain.MarketTypeCrypto, Ticker: "BTC-USD", Side: domain.PositionSideShort, Quantity: 1},
	})

	if _, err := reconciler.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if got := len(auditRepo.entries); got != 0 {
		t.Fatalf("audit entries = %d, want 0", got)
	}
}

func TestReconcilerPaginatesLocalOpenPositions(t *testing.T) {
	t.Parallel()

	local := make([]domain.Position, 1001)
	broker := make([]domain.Position, 1001)
	for i := range local {
		slug := fmt.Sprintf("market-page-%04d", i)
		local[i] = domain.Position{Ticker: slug + ":YES", Side: domain.PositionSideLong, Quantity: 1}
		broker[i] = domain.Position{Ticker: slug, Side: domain.PositionSideLong, Quantity: 1}
	}
	reconciler, auditRepo, _ := newReconcilerTestHarness(broker, local)

	if _, err := reconciler.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if got := len(auditRepo.entries); got != 0 {
		t.Fatalf("audit entries = %d, want 0", got)
	}
}

func newReconcilerTestHarness(brokerPositions, localPositions []domain.Position) (*Reconciler, *reconcilerAuditRepoStub, *reconcilerMetricsStub) {
	auditRepo := &reconcilerAuditRepoStub{}
	metrics := &reconcilerMetricsStub{}
	reconciler := NewReconciler(ReconcilerDeps{
		Broker:       &reconcilerBrokerStub{positions: brokerPositions},
		PositionRepo: &reconcilerPositionRepoStub{positions: localPositions},
		AuditLogRepo: auditRepo,
		Metrics:      metrics,
	})
	return reconciler, auditRepo, metrics
}

func assertSingleDrift(t *testing.T, entries []*domain.AuditLogEntry, wantType, wantKey string, wantLocal, wantExternal float64) {
	t.Helper()
	if got := len(entries); got != 1 {
		t.Fatalf("audit entries = %d, want 1", got)
	}
	if entries[0].EventType != polymarketPositionDriftEventType {
		t.Fatalf("event type = %q, want %q", entries[0].EventType, polymarketPositionDriftEventType)
	}
	var payload map[string]any
	if err := json.Unmarshal(entries[0].Details, &payload); err != nil {
		t.Fatalf("unmarshal audit details: %v", err)
	}
	if got := payload["drift_type"]; got != wantType {
		t.Fatalf("drift_type = %v, want %q", got, wantType)
	}
	if got := payload["key"]; got != wantKey {
		t.Fatalf("key = %v, want %q", got, wantKey)
	}
	if got := payload["local_quantity"]; got != wantLocal {
		t.Fatalf("local_quantity = %v, want %v", got, wantLocal)
	}
	if got := payload["external_quantity"]; got != wantExternal {
		t.Fatalf("external_quantity = %v, want %v", got, wantExternal)
	}
}

func assertMetricCount(t *testing.T, metrics *reconcilerMetricsStub, key string, want int) {
	t.Helper()
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	if got := metrics.drifts[key]; got != want {
		t.Fatalf("metric %q = %d, want %d", key, got, want)
	}
}
