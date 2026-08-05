package polymarket

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	marketdata "github.com/PatrickFanella/get-rich-quick/internal/marketdata/polymarket"
)

type fakeBroker struct {
	prepareTmpl *OrderTemplate
	sendCalls   atomic.Int32
	sendErr     error
	lastTmpl    *OrderTemplate
	lastOrder   *domain.Order
	mu          sync.Mutex
}

func (f *fakeBroker) PrepareTemplate(order *domain.Order) (*OrderTemplate, error) {
	f.mu.Lock()
	copyOrder := *order
	f.lastOrder = &copyOrder
	f.mu.Unlock()
	if f.prepareTmpl != nil {
		return f.prepareTmpl.Clone(), nil
	}
	return NewOrderTemplate([]byte(strings.Repeat("a", 32)), "POST", "https://example.com/v1/orders", []byte(`{}`))
}

func (f *fakeBroker) SendTemplate(_ context.Context, tmpl *OrderTemplate) (*createOrderResponse, error) {
	f.sendCalls.Add(1)
	f.mu.Lock()
	f.lastTmpl = tmpl
	f.mu.Unlock()
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	return &createOrderResponse{ID: "ok"}, nil
}

func TestStopGuard_LongStopBelowFires(t *testing.T) {
	broker := &fakeBroker{}
	g, err := NewStopGuard(StopGuardConfig{Broker: broker})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.RegisterEntry(Position{ID: "1", Slug: "slug-a", Side: "BUY", EntryPx: 0.50, Size: 1, StopPx: 0.45}); err != nil {
		t.Fatal(err)
	}
	g.OnTick(context.Background(), marketdata.Tick{Slug: "slug-a", Side: "YES", Price: 0.46, ReceivedAt: time.Now()})
	if got := broker.sendCalls.Load(); got != 0 {
		t.Fatalf("expected no fire, got %d", got)
	}
	g.OnTick(context.Background(), marketdata.Tick{Slug: "slug-a", Side: "YES", Price: 0.44, ReceivedAt: time.Now()})
	if got := broker.sendCalls.Load(); got != 1 {
		t.Fatalf("expected one fire, got %d", got)
	}
}

func TestStopGuard_LongTakeProfitFires(t *testing.T) {
	broker := &fakeBroker{}
	g, err := NewStopGuard(StopGuardConfig{Broker: broker})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.RegisterEntry(Position{ID: "1", Slug: "slug-a", Side: "BUY", EntryPx: 0.50, Size: 1, TakeProfitPx: 0.55}); err != nil {
		t.Fatal(err)
	}
	g.OnTick(context.Background(), marketdata.Tick{Slug: "slug-a", Side: "YES", Price: 0.54, ReceivedAt: time.Now()})
	if got := broker.sendCalls.Load(); got != 0 {
		t.Fatalf("expected no fire, got %d", got)
	}
	g.OnTick(context.Background(), marketdata.Tick{Slug: "slug-a", Side: "YES", Price: 0.56, ReceivedAt: time.Now()})
	if got := broker.sendCalls.Load(); got != 1 {
		t.Fatalf("expected one fire, got %d", got)
	}
}

func TestStopGuard_DuplicateTickCrossingThresholdFiresOnce(t *testing.T) {
	broker := &fakeBroker{}
	g, err := NewStopGuard(StopGuardConfig{Broker: broker})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.RegisterEntry(Position{ID: "1", Slug: "slug-a", Side: "BUY", EntryPx: 0.50, Size: 1, StopPx: 0.45}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.OnTick(context.Background(), marketdata.Tick{Slug: "slug-a", Side: "YES", Price: 0.44, ReceivedAt: time.Now()})
		}()
	}
	wg.Wait()
	if got := broker.sendCalls.Load(); got != 1 {
		t.Fatalf("expected exactly one fire, got %d", got)
	}
}

func TestStopGuard_DuplicateRegistrationIsIdempotent(t *testing.T) {
	broker := &fakeBroker{}
	g, err := NewStopGuard(StopGuardConfig{Broker: broker})
	if err != nil {
		t.Fatal(err)
	}
	pos := Position{ID: "1", Slug: "slug-a", Side: "BUY", EntryPx: 0.50, Size: 1, StopPx: 0.45, TakeProfitPx: 0.55}
	if err := g.RegisterEntry(pos); err != nil {
		t.Fatal(err)
	}
	if err := g.RegisterEntry(pos); err != nil {
		t.Fatal(err)
	}
	if got := g.Active(); got != 1 {
		t.Fatalf("expected one active guard after duplicate registration, got %d", got)
	}
	g.OnTick(context.Background(), marketdata.Tick{Slug: "slug-a", Side: "YES", Price: 0.56, ReceivedAt: time.Now()})
	if got := broker.sendCalls.Load(); got != 1 {
		t.Fatalf("expected one fire after duplicate registration, got %d", got)
	}
}

func TestStopGuard_CancelPreventsFire(t *testing.T) {
	broker := &fakeBroker{}
	g, err := NewStopGuard(StopGuardConfig{Broker: broker})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.RegisterEntry(Position{ID: "1", Slug: "slug-a", Side: "BUY", EntryPx: 0.50, Size: 1, StopPx: 0.45}); err != nil {
		t.Fatal(err)
	}
	g.Cancel("1")
	g.OnTick(context.Background(), marketdata.Tick{Slug: "slug-a", Side: "YES", Price: 0.44, ReceivedAt: time.Now()})
	if got := broker.sendCalls.Load(); got != 0 {
		t.Fatalf("expected no fire after cancel, got %d", got)
	}
}

func TestStopGuard_OtherSlugIgnored(t *testing.T) {
	broker := &fakeBroker{}
	g, err := NewStopGuard(StopGuardConfig{Broker: broker})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.RegisterEntry(Position{ID: "1", Slug: "slug-a", Side: "BUY", EntryPx: 0.50, Size: 1, StopPx: 0.45}); err != nil {
		t.Fatal(err)
	}
	g.OnTick(context.Background(), marketdata.Tick{Slug: "slug-b", Side: "YES", Price: 0.44, ReceivedAt: time.Now()})
	if got := broker.sendCalls.Load(); got != 0 {
		t.Fatalf("expected no fire on other slug, got %d", got)
	}
}

func TestStopGuard_RegisterPositionPreservesNoOutcomeIntent(t *testing.T) {
	broker := &fakeBroker{}
	g, err := NewStopGuard(StopGuardConfig{Broker: broker})
	if err != nil {
		t.Fatal(err)
	}
	stop := 0.40
	pos := domain.Position{ID: uuidFromString(t, "00000000-0000-0000-0000-000000000001"), Ticker: "slug-a:NO", Side: domain.PositionSideLong, Quantity: 2, AvgEntry: 0.50, StopLoss: &stop}
	if err := g.RegisterPosition(pos); err != nil {
		t.Fatal(err)
	}
	if broker.lastOrder == nil {
		t.Fatal("expected prepared order")
	}
	if broker.lastOrder.PredictionSide != "NO" || broker.lastOrder.PolymarketIntent != "ORDER_INTENT_SELL_SHORT" {
		t.Fatalf("unexpected NO close order: %+v", broker.lastOrder)
	}
	g.OnTick(context.Background(), marketdata.Tick{Slug: "slug-a", Side: "YES", Price: 0.39, ReceivedAt: time.Now()})
	if got := broker.sendCalls.Load(); got != 0 {
		t.Fatalf("YES tick fired NO guard, got %d", got)
	}
	g.OnTick(context.Background(), marketdata.Tick{Slug: "slug-a", Side: "NO", Price: 0.39, ReceivedAt: time.Now()})
	if got := broker.sendCalls.Load(); got != 1 {
		t.Fatalf("NO tick did not fire NO guard, got %d", got)
	}
}

func TestStopGuard_SendFailureRearmsGuard(t *testing.T) {
	broker := &fakeBroker{sendErr: errors.New("temporary")}
	g, err := NewStopGuard(StopGuardConfig{Broker: broker})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.RegisterEntry(Position{ID: "1", Slug: "slug-a", Side: "BUY", OutcomeSide: "YES", EntryPx: 0.50, Size: 1, StopPx: 0.45}); err != nil {
		t.Fatal(err)
	}
	g.OnTick(context.Background(), marketdata.Tick{Slug: "slug-a", Side: "YES", Price: 0.44, ReceivedAt: time.Now()})
	if got := broker.sendCalls.Load(); got != 1 {
		t.Fatalf("send calls = %d, want first attempt", got)
	}
	if got := g.Active(); got != 1 {
		t.Fatalf("active guards = %d, want guard retained after send failure", got)
	}
	broker.sendErr = nil
	g.OnTick(context.Background(), marketdata.Tick{Slug: "slug-a", Side: "YES", Price: 0.43, ReceivedAt: time.Now()})
	if got := broker.sendCalls.Load(); got != 2 {
		t.Fatalf("send calls = %d, want retry", got)
	}
	if got := g.Active(); got != 0 {
		t.Fatalf("active guards = %d, want removed after successful retry", got)
	}
}

func uuidFromString(t *testing.T, raw string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
