package kalshi

import (
	"context"
	"errors"
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
	panic("unexpected SubmitOrder call")
}

func (s *reconcilerBrokerStub) CancelOrder(context.Context, string) error {
	panic("unexpected CancelOrder call")
}

func (s *reconcilerBrokerStub) GetOrderStatus(context.Context, string) (domain.OrderStatus, error) {
	panic("unexpected GetOrderStatus call")
}

func (s *reconcilerBrokerStub) GetPositions(context.Context) ([]domain.Position, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]domain.Position(nil), s.positions...), nil
}

func (s *reconcilerBrokerStub) GetAccountBalance(context.Context) (execution.Balance, error) {
	panic("unexpected GetAccountBalance call")
}

type reconcilerPositionRepoStub struct {
	positions []domain.Position
	err       error
}

func (r *reconcilerPositionRepoStub) Create(context.Context, *domain.Position) error {
	panic("unexpected Create call")
}

func (r *reconcilerPositionRepoStub) Get(context.Context, uuid.UUID) (*domain.Position, error) {
	panic("unexpected Get call")
}

func (r *reconcilerPositionRepoStub) List(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	panic("unexpected List call")
}

func (r *reconcilerPositionRepoStub) Count(context.Context, repository.PositionFilter) (int, error) {
	panic("unexpected Count call")
}

func (r *reconcilerPositionRepoStub) Update(context.Context, *domain.Position) error {
	panic("unexpected Update call")
}

func (r *reconcilerPositionRepoStub) Delete(context.Context, uuid.UUID) error {
	panic("unexpected Delete call")
}

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

func (r *reconcilerPositionRepoStub) CountOpen(context.Context, repository.PositionFilter) (int, error) {
	panic("unexpected CountOpen call")
}

func (r *reconcilerPositionRepoStub) GetByStrategy(context.Context, uuid.UUID, repository.PositionFilter, int, int) ([]domain.Position, error) {
	panic("unexpected GetByStrategy call")
}

func TestReconcilerNoDriftWhenBrokerAndLocalMatch(t *testing.T) {
	t.Parallel()

	reconciler := newReconcilerTestHarness([]domain.Position{
		{Ticker: "kx-alpha", Side: domain.PositionSideLong, Quantity: 3},
	}, []domain.Position{
		{MarketType: domain.MarketTypeKalshi, Ticker: "KX-ALPHA", Side: domain.PositionSideLong, Quantity: 3},
	})

	result, err := reconciler.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if result.DriftCount != 0 || len(result.Drifts) != 0 {
		t.Fatalf("result = %#v, want no drift", result)
	}
	if result.BrokerPositions != 1 || result.LocalPositions != 1 || result.MatchedPositions != 1 {
		t.Fatalf("result counts = %#v", result)
	}
}

func TestReconcilerReportsBrokerMissingLocally(t *testing.T) {
	t.Parallel()

	reconciler := newReconcilerTestHarness([]domain.Position{
		{Ticker: "KX-BROKER", Side: domain.PositionSideShort, Quantity: 4},
	}, nil)

	result, err := reconciler.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if result.DriftCount != 1 || len(result.Drifts) != 1 {
		t.Fatalf("result = %#v, want one drift", result)
	}
	if got := result.Drifts[0]; got.Kind != "broker_missing_locally" || got.Key != "KX-BROKER|short" || got.BrokerQuantity != 4 || got.LocalQuantity != 0 {
		t.Fatalf("drift = %#v", got)
	}
}

func TestReconcilerReportsLocalMissingOnBroker(t *testing.T) {
	t.Parallel()

	reconciler := newReconcilerTestHarness(nil, []domain.Position{
		{MarketType: domain.MarketTypeKalshi, Ticker: "KX-LOCAL", Side: domain.PositionSideLong, Quantity: 2},
	})

	result, err := reconciler.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if result.DriftCount != 1 || len(result.Drifts) != 1 {
		t.Fatalf("result = %#v, want one drift", result)
	}
	if got := result.Drifts[0]; got.Kind != "local_missing_on_broker" || got.Key != "KX-LOCAL|long" || got.BrokerQuantity != 0 || got.LocalQuantity != 2 {
		t.Fatalf("drift = %#v", got)
	}
}

func TestReconcilerReportsQuantityMismatch(t *testing.T) {
	t.Parallel()

	reconciler := newReconcilerTestHarness([]domain.Position{
		{Ticker: "KX-MISMATCH", Side: domain.PositionSideLong, Quantity: 2},
	}, []domain.Position{
		{MarketType: domain.MarketTypeKalshi, Ticker: "KX-MISMATCH", Side: domain.PositionSideLong, Quantity: 5},
	})

	result, err := reconciler.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if result.DriftCount != 1 || len(result.Drifts) != 1 {
		t.Fatalf("result = %#v, want one drift", result)
	}
	if got := result.Drifts[0]; got.Kind != "quantity_mismatch" || got.Key != "KX-MISMATCH|long" || got.BrokerQuantity != 2 || got.LocalQuantity != 5 {
		t.Fatalf("drift = %#v", got)
	}
}

func TestReconcilerPropagatesBrokerAndRepoErrors(t *testing.T) {
	t.Parallel()

	brokerErr := errors.New("broker boom")
	reconciler := &Reconciler{broker: &reconcilerBrokerStub{err: brokerErr}, positionRepo: &reconcilerPositionRepoStub{}}
	if _, err := reconciler.Check(context.Background()); !errors.Is(err, brokerErr) {
		t.Fatalf("Check() broker error = %v, want %v", err, brokerErr)
	}

	repoErr := errors.New("repo boom")
	reconciler = &Reconciler{broker: &reconcilerBrokerStub{}, positionRepo: &reconcilerPositionRepoStub{err: repoErr}}
	if _, err := reconciler.Check(context.Background()); !errors.Is(err, repoErr) {
		t.Fatalf("Check() repo error = %v, want %v", err, repoErr)
	}
}

func newReconcilerTestHarness(brokerPositions, localPositions []domain.Position) *Reconciler {
	return NewReconciler(ReconcilerDeps{
		Broker:       &reconcilerBrokerStub{positions: brokerPositions},
		PositionRepo: &reconcilerPositionRepoStub{positions: localPositions},
	})
}
