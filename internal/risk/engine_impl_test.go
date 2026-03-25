package risk

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func newTestEngine() *RiskEngineImpl {
	e := NewRiskEngine(DefaultPositionLimits(), DefaultCircuitBreakerConfig(), nil, nil)
	// Disable file and env mechanisms by default so existing tests are unaffected.
	e.fileExistsFunc = func(string) bool { return false }
	e.getEnvFunc = func(string) string { return "" }
	return e
}

func TestCheckPreTrade_Approved(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	order := &domain.Order{
		Ticker:   "AAPL",
		Quantity: 10,
		Side:     domain.OrderSideBuy,
	}
	portfolio := Portfolio{
		TotalExposurePct:    0.50,
		ConcurrentPositions: 5,
	}

	approved, reason, err := engine.CheckPreTrade(context.Background(), order, portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !approved {
		t.Fatalf("expected approved, got rejected: %s", reason)
	}
	if reason != "" {
		t.Fatalf("expected empty reason, got %q", reason)
	}
}

func TestCheckPreTrade_KillSwitchActive(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	if err := engine.ActivateKillSwitch(context.Background(), "manual halt"); err != nil {
		t.Fatalf("unexpected error activating kill switch: %v", err)
	}

	order := &domain.Order{Ticker: "AAPL", Quantity: 10}
	approved, reason, err := engine.CheckPreTrade(context.Background(), order, Portfolio{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected when kill switch active")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestCheckPreTrade_CircuitBreakerTripped(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	if err := engine.TripCircuitBreaker(context.Background(), "loss limit"); err != nil {
		t.Fatalf("unexpected error tripping circuit breaker: %v", err)
	}

	order := &domain.Order{Ticker: "AAPL", Quantity: 10}
	approved, reason, err := engine.CheckPreTrade(context.Background(), order, Portfolio{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected when circuit breaker tripped")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestCheckPreTrade_InvalidOrder(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()

	// Nil order.
	approved, reason, err := engine.CheckPreTrade(context.Background(), nil, Portfolio{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected for nil order")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason for nil order")
	}

	// Empty ticker.
	approved, reason, err = engine.CheckPreTrade(context.Background(), &domain.Order{Quantity: 10}, Portfolio{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected for empty ticker")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason for empty ticker")
	}

	// Zero quantity.
	approved, reason, err = engine.CheckPreTrade(context.Background(), &domain.Order{Ticker: "AAPL", Quantity: 0}, Portfolio{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected for zero quantity")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason for zero quantity")
	}
}

func TestCheckPositionLimits_WithinLimits(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	portfolio := Portfolio{
		TotalExposurePct:    0.50,
		ConcurrentPositions: 3,
		PositionExposureBySymbol: map[string]float64{
			"AAPL": 0.10,
			"GOOG": 0.10,
			"MSFT": 0.10,
		},
		MarketExposurePct: map[domain.MarketType]float64{
			domain.MarketTypeStock: 0.30,
		},
	}

	approved, reason, err := engine.CheckPositionLimits(context.Background(), "AAPL", 0.05, portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !approved {
		t.Fatalf("expected approved, got rejected: %s", reason)
	}
}

func TestCheckPositionLimits_ExceedsPositionSize(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	portfolio := Portfolio{
		TotalExposurePct:    0.50,
		ConcurrentPositions: 3,
		PositionExposureBySymbol: map[string]float64{
			"AAPL": 0.15,
		},
	}

	// Adding 0.10 to existing 0.15 = 0.25, exceeds MaxPerPositionPct of 0.20.
	approved, reason, err := engine.CheckPositionLimits(context.Background(), "AAPL", 0.10, portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected for exceeding position size")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestCheckPositionLimits_ExceedsTotalExposure(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	portfolio := Portfolio{
		TotalExposurePct:    0.95,
		ConcurrentPositions: 3,
		PositionExposureBySymbol: map[string]float64{
			"AAPL": 0.10,
		},
	}

	// Adding 0.10 to total 0.95 = 1.05, exceeds MaxTotalPct of 1.00.
	approved, reason, err := engine.CheckPositionLimits(context.Background(), "AAPL", 0.10, portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected for exceeding total exposure")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestCheckPositionLimits_ExceedsConcurrentPositions(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	portfolio := Portfolio{
		TotalExposurePct:    0.50,
		ConcurrentPositions: 10,
		PositionExposureBySymbol: map[string]float64{
			"AAPL": 0.05, "GOOG": 0.05, "MSFT": 0.05, "AMZN": 0.05, "META": 0.05,
			"TSLA": 0.05, "NVDA": 0.05, "AMD": 0.05, "INTC": 0.05, "ORCL": 0.05,
		},
	}

	// New ticker when already at max concurrent positions.
	approved, reason, err := engine.CheckPositionLimits(context.Background(), "IBM", 0.05, portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected for exceeding concurrent positions")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestCheckPositionLimits_ExceedsMarketExposure(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	portfolio := Portfolio{
		TotalExposurePct:    0.60,
		ConcurrentPositions: 3,
		PositionExposureBySymbol: map[string]float64{
			"AAPL": 0.10,
		},
		MarketExposurePct: map[domain.MarketType]float64{
			domain.MarketTypeStock: 0.55, // Exceeds MaxPerMarketPct of 0.50.
		},
	}

	approved, reason, err := engine.CheckPositionLimits(context.Background(), "AAPL", 0.05, portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected for exceeding market exposure")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestCheckPositionLimits_MarketExposurePushedOverByQuantity(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	// The caller computes post-trade market exposure (0.49 + 0.02 = 0.51)
	// and passes it in MarketExposurePct.
	portfolio := Portfolio{
		TotalExposurePct:    0.50,
		ConcurrentPositions: 3,
		PositionExposureBySymbol: map[string]float64{
			"AAPL": 0.10,
		},
		MarketExposurePct: map[domain.MarketType]float64{
			domain.MarketTypeStock: 0.51, // Post-trade: was 0.49, +0.02 pushed over 0.50 limit.
		},
	}

	approved, reason, err := engine.CheckPositionLimits(context.Background(), "AAPL", 0.02, portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected when post-trade market exposure exceeds limit")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestCheckPositionLimits_ExceedsPolymarketExposure(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	portfolio := Portfolio{
		TotalExposurePct:    0.10,
		ConcurrentPositions: 1,
		PositionExposureBySymbol: map[string]float64{
			"POLY-ELECTION": 0.04,
		},
		MarketExposurePct: map[domain.MarketType]float64{
			domain.MarketTypePolymarket: 0.06, // Exceeds polymarket limit of 0.05.
		},
	}

	approved, reason, err := engine.CheckPositionLimits(context.Background(), "POLY-ELECTION", 0.01, portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected for exceeding Polymarket exposure")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestCheckPositionLimits_ExistingPositionBypassesConcurrentCheck(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	portfolio := Portfolio{
		TotalExposurePct:    0.50,
		ConcurrentPositions: 10,
		PositionExposureBySymbol: map[string]float64{
			"AAPL": 0.05, "GOOG": 0.05, "MSFT": 0.05, "AMZN": 0.05, "META": 0.05,
			"TSLA": 0.05, "NVDA": 0.05, "AMD": 0.05, "INTC": 0.05, "ORCL": 0.05,
		},
	}

	// Adding to an existing position should not be blocked by concurrent limit.
	approved, reason, err := engine.CheckPositionLimits(context.Background(), "AAPL", 0.05, portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !approved {
		t.Fatalf("expected approved for existing position, got rejected: %s", reason)
	}
}

func TestCheckPositionLimits_InvalidInputs(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	portfolio := Portfolio{TotalExposurePct: 0.10, ConcurrentPositions: 1}

	// Empty ticker.
	approved, reason, err := engine.CheckPositionLimits(context.Background(), "", 0.05, portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected for empty ticker")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason for empty ticker")
	}

	// Zero quantity.
	approved, reason, err = engine.CheckPositionLimits(context.Background(), "AAPL", 0, portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected for zero quantity")
	}

	// Negative quantity.
	approved, reason, err = engine.CheckPositionLimits(context.Background(), "AAPL", -0.05, portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected for negative quantity")
	}

	// NaN quantity.
	approved, reason, err = engine.CheckPositionLimits(context.Background(), "AAPL", math.NaN(), portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected for NaN quantity")
	}

	// Inf quantity.
	approved, reason, err = engine.CheckPositionLimits(context.Background(), "AAPL", math.Inf(1), portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected for Inf quantity")
	}
}

func TestGetStatus_Normal(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	status, err := engine.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.RiskStatus != domain.RiskStatusNormal {
		t.Fatalf("expected normal status, got %q", status.RiskStatus)
	}
	if status.CircuitBreaker.State != CircuitBreakerPhaseOpen {
		t.Fatalf("expected open circuit breaker, got %q", status.CircuitBreaker.State)
	}
	if status.KillSwitch.Active {
		t.Fatal("expected kill switch inactive")
	}
	if status.PositionLimits.MaxConcurrent != 10 {
		t.Fatalf("expected max concurrent 10, got %d", status.PositionLimits.MaxConcurrent)
	}
}

func TestGetStatus_Breached(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	if err := engine.TripCircuitBreaker(context.Background(), "test"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	status, err := engine.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.RiskStatus != domain.RiskStatusBreached {
		t.Fatalf("expected breached status, got %q", status.RiskStatus)
	}
}

func TestGetStatus_Warning(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	if err := engine.ActivateKillSwitch(context.Background(), "manual"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	status, err := engine.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.RiskStatus != domain.RiskStatusWarning {
		t.Fatalf("expected warning status, got %q", status.RiskStatus)
	}
}

func TestCircuitBreakerLifecycle(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	ctx := context.Background()

	// Trip.
	if err := engine.TripCircuitBreaker(ctx, "loss limit"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status, _ := engine.GetStatus(ctx)
	if status.CircuitBreaker.State != CircuitBreakerPhaseTripped {
		t.Fatalf("expected tripped, got %q", status.CircuitBreaker.State)
	}

	// Reset.
	if err := engine.ResetCircuitBreaker(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status, _ = engine.GetStatus(ctx)
	if status.CircuitBreaker.State != CircuitBreakerPhaseOpen {
		t.Fatalf("expected open after reset, got %q", status.CircuitBreaker.State)
	}
}

func TestKillSwitchLifecycle(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	ctx := context.Background()

	// Activate.
	if err := engine.ActivateKillSwitch(ctx, "emergency"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	active, err := engine.IsKillSwitchActive(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !active {
		t.Fatal("expected kill switch active")
	}

	// Deactivate.
	if err := engine.DeactivateKillSwitch(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	active, err = engine.IsKillSwitchActive(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active {
		t.Fatal("expected kill switch inactive")
	}
}

func TestInterfaceCompliance(t *testing.T) {
	t.Parallel()

	var _ RiskEngine = (*RiskEngineImpl)(nil)
}

func TestDefaultPositionLimits(t *testing.T) {
	t.Parallel()

	limits := DefaultPositionLimits()
	if limits.MaxPerPositionPct != 0.20 {
		t.Fatalf("expected 0.20, got %f", limits.MaxPerPositionPct)
	}
	if limits.MaxTotalPct != 1.00 {
		t.Fatalf("expected 1.00, got %f", limits.MaxTotalPct)
	}
	if limits.MaxConcurrent != 10 {
		t.Fatalf("expected 10, got %d", limits.MaxConcurrent)
	}
	if limits.MaxPerMarketPct != 0.50 {
		t.Fatalf("expected 0.50, got %f", limits.MaxPerMarketPct)
	}
}

func TestUpdateMetrics_DailyLossTripsBreaker(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	ctx := context.Background()

	// Loss of 4% exceeds the 3% threshold.
	err := engine.UpdateMetrics(ctx, -0.04, 0.0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	status, _ := engine.GetStatus(ctx)
	if status.CircuitBreaker.State != CircuitBreakerPhaseTripped {
		t.Fatalf("expected tripped, got %q", status.CircuitBreaker.State)
	}
	if status.CircuitBreaker.Reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestUpdateMetrics_DailyLossBelowThreshold(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	ctx := context.Background()

	// Loss of 2% is within the 3% threshold.
	err := engine.UpdateMetrics(ctx, -0.02, 0.0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	status, _ := engine.GetStatus(ctx)
	if status.CircuitBreaker.State != CircuitBreakerPhaseOpen {
		t.Fatalf("expected open, got %q", status.CircuitBreaker.State)
	}
}

func TestUpdateMetrics_DrawdownTripsBreaker(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	ctx := context.Background()

	// Drawdown of 12% exceeds the 10% threshold.
	err := engine.UpdateMetrics(ctx, 0.0, 0.12, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	status, _ := engine.GetStatus(ctx)
	if status.CircuitBreaker.State != CircuitBreakerPhaseTripped {
		t.Fatalf("expected tripped, got %q", status.CircuitBreaker.State)
	}
	if status.CircuitBreaker.Reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestUpdateMetrics_ConsecutiveLossesTripsBreaker(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	ctx := context.Background()

	// 6 consecutive losses exceeds the threshold of 5.
	err := engine.UpdateMetrics(ctx, 0.0, 0.0, 6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	status, _ := engine.GetStatus(ctx)
	if status.CircuitBreaker.State != CircuitBreakerPhaseTripped {
		t.Fatalf("expected tripped, got %q", status.CircuitBreaker.State)
	}
	if status.CircuitBreaker.Reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestUpdateMetrics_DoesNotTripWhenAlreadyTripped(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	ctx := context.Background()

	// Trip manually first.
	if err := engine.TripCircuitBreaker(ctx, "manual"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// UpdateMetrics should not overwrite the existing trip.
	err := engine.UpdateMetrics(ctx, -0.10, 0.0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	status, _ := engine.GetStatus(ctx)
	if status.CircuitBreaker.Reason != "manual" {
		t.Fatalf("expected original reason 'manual', got %q", status.CircuitBreaker.Reason)
	}
}

func TestCooldownAutoResets(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	ctx := context.Background()

	// Use a controllable clock.
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	engine.nowFunc = func() time.Time { return now }

	// Trip the circuit breaker; cooldown = 15 minutes by default.
	if err := engine.TripCircuitBreaker(ctx, "loss limit"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Still tripped before cooldown expires.
	now = now.Add(14 * time.Minute)
	status, _ := engine.GetStatus(ctx)
	if status.CircuitBreaker.State != CircuitBreakerPhaseTripped {
		t.Fatalf("expected tripped during cooldown, got %q", status.CircuitBreaker.State)
	}

	// After cooldown expires, should auto-reset.
	now = now.Add(2 * time.Minute) // total 16 min > 15 min cooldown
	status, _ = engine.GetStatus(ctx)
	if status.CircuitBreaker.State != CircuitBreakerPhaseOpen {
		t.Fatalf("expected open after cooldown, got %q", status.CircuitBreaker.State)
	}
}

func TestCooldownAutoResets_CheckPreTrade(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	ctx := context.Background()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	engine.nowFunc = func() time.Time { return now }

	if err := engine.TripCircuitBreaker(ctx, "loss limit"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	order := &domain.Order{Ticker: "AAPL", Quantity: 10}

	// Rejected before cooldown.
	approved, _, err := engine.CheckPreTrade(ctx, order, Portfolio{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected rejected during cooldown")
	}

	// After cooldown, should be approved.
	now = now.Add(16 * time.Minute)
	approved, reason, err := engine.CheckPreTrade(ctx, order, Portfolio{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !approved {
		t.Fatalf("expected approved after cooldown, got rejected: %s", reason)
	}
}

func TestGetStatus_UsesInjectedClockForUpdatedAt(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	now := time.Date(2026, 3, 25, 9, 15, 0, 0, time.UTC)
	engine.SetNowFunc(func() time.Time { return now })

	status, err := engine.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	if !status.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %s, want %s", status.UpdatedAt, now)
	}
}

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultCircuitBreakerConfig()
	if cfg.MaxDailyLossPct != 0.03 {
		t.Fatalf("expected 0.03, got %f", cfg.MaxDailyLossPct)
	}
	if cfg.MaxDrawdownPct != 0.10 {
		t.Fatalf("expected 0.10, got %f", cfg.MaxDrawdownPct)
	}
	if cfg.MaxConsecutiveLosses != 5 {
		t.Fatalf("expected 5, got %d", cfg.MaxConsecutiveLosses)
	}
	if cfg.CooldownDuration != 15*time.Minute {
		t.Fatalf("expected 15m, got %v", cfg.CooldownDuration)
	}
}

func TestKillSwitch_APIToggle(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	ctx := context.Background()

	// Initially inactive.
	active, err := engine.IsKillSwitchActive(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active {
		t.Fatal("expected kill switch inactive initially")
	}

	// Activate via API toggle.
	if err := engine.ActivateKillSwitch(ctx, "emergency halt"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	active, err = engine.IsKillSwitchActive(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !active {
		t.Fatal("expected kill switch active after API activation")
	}

	// Verify mechanism is recorded.
	status, err := engine.GetStatus(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.KillSwitch.Active {
		t.Fatal("expected kill switch active in status")
	}
	if len(status.KillSwitch.Mechanisms) != 1 || status.KillSwitch.Mechanisms[0] != KillSwitchMechanismAPI {
		t.Fatalf("expected [api_toggle] mechanism, got %v", status.KillSwitch.Mechanisms)
	}
	if status.KillSwitch.Reason != "emergency halt" {
		t.Fatalf("expected reason 'emergency halt', got %q", status.KillSwitch.Reason)
	}

	// Deactivate via API toggle.
	if err := engine.DeactivateKillSwitch(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	active, err = engine.IsKillSwitchActive(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active {
		t.Fatal("expected kill switch inactive after API deactivation")
	}
}

func TestKillSwitch_FileFlagDetection(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	ctx := context.Background()

	// Simulate file flag present.
	engine.fileExistsFunc = func(path string) bool {
		return path == defaultKillSwitchFilePath
	}

	active, err := engine.IsKillSwitchActive(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !active {
		t.Fatal("expected kill switch active when file flag present")
	}

	// Verify mechanism is reported in status.
	status, err := engine.GetStatus(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.KillSwitch.Active {
		t.Fatal("expected kill switch active in status")
	}
	foundFile := false
	for _, m := range status.KillSwitch.Mechanisms {
		if m == KillSwitchMechanismFile {
			foundFile = true
		}
	}
	if !foundFile {
		t.Fatalf("expected file_flag mechanism, got %v", status.KillSwitch.Mechanisms)
	}

	// Simulate file flag removed.
	engine.fileExistsFunc = func(string) bool { return false }

	active, err = engine.IsKillSwitchActive(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active {
		t.Fatal("expected kill switch inactive when file flag removed")
	}
}

func TestKillSwitch_EnvVarDetection(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	ctx := context.Background()

	// Simulate env var set.
	engine.getEnvFunc = func(key string) string {
		if key == killSwitchEnvVar {
			return "true"
		}
		return ""
	}

	active, err := engine.IsKillSwitchActive(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !active {
		t.Fatal("expected kill switch active when env var set")
	}

	// Verify mechanism is reported in status.
	status, err := engine.GetStatus(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.KillSwitch.Active {
		t.Fatal("expected kill switch active in status")
	}
	foundEnv := false
	for _, m := range status.KillSwitch.Mechanisms {
		if m == KillSwitchMechanismEnvVar {
			foundEnv = true
		}
	}
	if !foundEnv {
		t.Fatalf("expected env_var mechanism, got %v", status.KillSwitch.Mechanisms)
	}

	// Non-"true" value should not activate.
	engine.getEnvFunc = func(key string) string {
		if key == killSwitchEnvVar {
			return "false"
		}
		return ""
	}
	active, err = engine.IsKillSwitchActive(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active {
		t.Fatal("expected kill switch inactive when env var is not 'true'")
	}
}

func TestKillSwitch_AnyMechanismBlocksTrading(t *testing.T) {
	t.Parallel()

	order := &domain.Order{Ticker: "AAPL", Quantity: 10}
	portfolio := Portfolio{}
	ctx := context.Background()

	tests := []struct {
		name       string
		setupAPI   bool
		setupFile  bool
		setupEnv   bool
		wantActive bool
	}{
		{"no mechanisms", false, false, false, false},
		{"API only", true, false, false, true},
		{"file only", false, true, false, true},
		{"env only", false, false, true, true},
		{"API and file", true, true, false, true},
		{"API and env", true, false, true, true},
		{"file and env", false, true, true, true},
		{"all three", true, true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			engine := newTestEngine()
			if tt.setupAPI {
				if err := engine.ActivateKillSwitch(ctx, "api halt"); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			engine.fileExistsFunc = func(string) bool { return tt.setupFile }
			engine.getEnvFunc = func(string) string {
				if tt.setupEnv {
					return "true"
				}
				return ""
			}

			// IsKillSwitchActive should reflect the combined state.
			active, err := engine.IsKillSwitchActive(ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if active != tt.wantActive {
				t.Fatalf("IsKillSwitchActive = %v, want %v", active, tt.wantActive)
			}

			// CheckPreTrade should block when any mechanism is active.
			approved, reason, err := engine.CheckPreTrade(ctx, order, portfolio)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantActive {
				if approved {
					t.Fatal("expected trade rejected when kill switch active")
				}
				if reason == "" {
					t.Fatal("expected non-empty reason when kill switch active")
				}
			} else {
				if !approved {
					t.Fatalf("expected trade approved, got rejected: %s", reason)
				}
			}
		})
	}
}

func TestKillSwitch_FileFlagCustomPath(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	ctx := context.Background()

	customPath := "/var/run/kill_trading"
	engine.killSwitchFilePath = customPath
	engine.fileExistsFunc = func(path string) bool {
		return path == customPath
	}

	active, err := engine.IsKillSwitchActive(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !active {
		t.Fatal("expected kill switch active with custom file path")
	}
}

func TestKillSwitch_DeactivateAPIDoesNotAffectFileOrEnv(t *testing.T) {
	t.Parallel()

	engine := newTestEngine()
	ctx := context.Background()

	// Activate API and simulate file flag.
	if err := engine.ActivateKillSwitch(ctx, "api halt"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	engine.fileExistsFunc = func(string) bool { return true }

	// Deactivate API toggle.
	if err := engine.DeactivateKillSwitch(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Kill switch should still be active due to file flag.
	active, err := engine.IsKillSwitchActive(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !active {
		t.Fatal("expected kill switch still active due to file flag after API deactivation")
	}

	// Trade should still be blocked.
	order := &domain.Order{Ticker: "AAPL", Quantity: 10}
	approved, _, err := engine.CheckPreTrade(ctx, order, Portfolio{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Fatal("expected trade rejected when file flag still active")
	}
}
