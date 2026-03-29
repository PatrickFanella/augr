package integration

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discard{}, nil))
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// TestIntegration_RiskEngine_CircuitBreakerTripAndReset validates that the
// circuit breaker trips when daily loss exceeds the threshold, blocks trades,
// and can be manually reset.
func TestIntegration_RiskEngine_CircuitBreakerTripAndReset(t *testing.T) {
	db := newTestDB(t)
	r := newRepos(db)
	ctx := context.Background()

	cbConfig := risk.CircuitBreakerConfig{
		MaxDailyLossPct:      0.03,
		MaxDrawdownPct:       0.10,
		MaxConsecutiveLosses: 5,
		CooldownDuration:     15 * time.Minute,
	}

	// Use a real position repo so the engine is wired to a live DB,
	// validating that the risk engine functions correctly in an
	// integrated environment.
	engine := risk.NewRiskEngine(risk.DefaultPositionLimits(), cbConfig, r.Position, discardLogger())

	// Disable file and env kill switch mechanisms for test isolation.
	engine.SetFileExistsFunc(func(string) bool { return false })
	engine.SetGetEnvFunc(func(string) string { return "" })

	order := &domain.Order{
		Ticker:   "AAPL",
		Quantity: 10,
		Side:     domain.OrderSideBuy,
	}
	portfolio := risk.Portfolio{
		TotalExposurePct:         0.05,
		ConcurrentPositions:      1,
		PositionExposureBySymbol: map[string]float64{"AAPL": 0.05},
	}

	approved, _, err := engine.CheckPreTrade(ctx, order, portfolio)
	if err != nil {
		t.Fatalf("CheckPreTrade() error: %v", err)
	}
	if !approved {
		t.Fatal("expected pre-trade check to pass before trip")
	}

	// 2. Trip via UpdateMetrics with excessive daily loss.
	if err := engine.UpdateMetrics(ctx, -0.05, 0.0, 0); err != nil {
		t.Fatalf("UpdateMetrics() error: %v", err)
	}

	// 3. Verify circuit breaker is tripped.
	status, err := engine.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus() error: %v", err)
	}
	if status.CircuitBreaker.State != risk.CircuitBreakerPhaseTripped {
		t.Fatalf("expected tripped, got %q", status.CircuitBreaker.State)
	}
	if status.RiskStatus != domain.RiskStatusBreached {
		t.Fatalf("expected breached risk status, got %q", status.RiskStatus)
	}

	// 4. Pre-trade check should now be rejected.
	approved, reason, err := engine.CheckPreTrade(ctx, order, portfolio)
	if err != nil {
		t.Fatalf("CheckPreTrade() error: %v", err)
	}
	if approved {
		t.Fatal("expected pre-trade check to fail when circuit breaker is tripped")
	}
	if !strings.Contains(reason, "circuit breaker tripped") {
		t.Fatalf("expected rejection reason to mention circuit breaker, got %q", reason)
	}

	// 5. Manually reset the circuit breaker.
	if err := engine.ResetCircuitBreaker(ctx); err != nil {
		t.Fatalf("ResetCircuitBreaker() error: %v", err)
	}

	status, err = engine.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus() after reset error: %v", err)
	}
	if status.CircuitBreaker.State != risk.CircuitBreakerPhaseOpen {
		t.Fatalf("expected open after reset, got %q", status.CircuitBreaker.State)
	}

	// 6. Pre-trade check should pass again.
	approved, _, err = engine.CheckPreTrade(ctx, order, portfolio)
	if err != nil {
		t.Fatalf("CheckPreTrade() after reset error: %v", err)
	}
	if !approved {
		t.Fatal("expected pre-trade check to pass after reset")
	}
}

// TestIntegration_RiskEngine_CircuitBreakerCooldownAutoReset validates that
// the circuit breaker auto-resets after the cooldown period expires.
func TestIntegration_RiskEngine_CircuitBreakerCooldownAutoReset(t *testing.T) {
	db := newTestDB(t)
	r := newRepos(db)
	ctx := context.Background()

	cooldown := 100 * time.Millisecond
	cbConfig := risk.CircuitBreakerConfig{
		MaxDailyLossPct:      0.03,
		MaxDrawdownPct:       0.10,
		MaxConsecutiveLosses: 5,
		CooldownDuration:     cooldown,
	}

	now := time.Now()
	engine := risk.NewRiskEngine(risk.DefaultPositionLimits(), cbConfig, r.Position, discardLogger())
	engine.SetNowFunc(func() time.Time { return now })
	engine.SetFileExistsFunc(func(string) bool { return false })
	engine.SetGetEnvFunc(func(string) string { return "" })

	// Trip via direct method.
	if err := engine.TripCircuitBreaker(ctx, "excessive drawdown"); err != nil {
		t.Fatalf("TripCircuitBreaker() error: %v", err)
	}

	// Verify tripped.
	status, _ := engine.GetStatus(ctx)
	if status.CircuitBreaker.State != risk.CircuitBreakerPhaseTripped {
		t.Fatalf("expected tripped, got %q", status.CircuitBreaker.State)
	}

	// Advance time past cooldown.
	now = now.Add(cooldown + time.Second)
	engine.SetNowFunc(func() time.Time { return now })

	// Next status check should auto-reset.
	status, err := engine.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus() after cooldown error: %v", err)
	}
	if status.CircuitBreaker.State != risk.CircuitBreakerPhaseOpen {
		t.Fatalf("expected open after cooldown, got %q", status.CircuitBreaker.State)
	}
}

// TestIntegration_RiskEngine_DrawdownTrip validates circuit breaker trips
// on excessive drawdown.
func TestIntegration_RiskEngine_DrawdownTrip(t *testing.T) {
	db := newTestDB(t)
	r := newRepos(db)
	ctx := context.Background()

	cbConfig := risk.CircuitBreakerConfig{
		MaxDailyLossPct:      0.03,
		MaxDrawdownPct:       0.10,
		MaxConsecutiveLosses: 5,
		CooldownDuration:     15 * time.Minute,
	}

	engine := risk.NewRiskEngine(risk.DefaultPositionLimits(), cbConfig, r.Position, discardLogger())
	engine.SetFileExistsFunc(func(string) bool { return false })
	engine.SetGetEnvFunc(func(string) string { return "" })

	// Drawdown of 12% exceeds the 10% threshold.
	if err := engine.UpdateMetrics(ctx, 0.0, 0.12, 0); err != nil {
		t.Fatalf("UpdateMetrics() error: %v", err)
	}

	status, _ := engine.GetStatus(ctx)
	if status.CircuitBreaker.State != risk.CircuitBreakerPhaseTripped {
		t.Fatalf("expected tripped on drawdown, got %q", status.CircuitBreaker.State)
	}
	if !strings.Contains(status.CircuitBreaker.Reason, "drawdown") {
		t.Fatalf("expected drawdown in reason, got %q", status.CircuitBreaker.Reason)
	}
}

// TestIntegration_RiskEngine_ConsecutiveLossesTrip validates circuit breaker
// trips on excessive consecutive losses.
func TestIntegration_RiskEngine_ConsecutiveLossesTrip(t *testing.T) {
	db := newTestDB(t)
	r := newRepos(db)
	ctx := context.Background()

	cbConfig := risk.CircuitBreakerConfig{
		MaxDailyLossPct:      0.03,
		MaxDrawdownPct:       0.10,
		MaxConsecutiveLosses: 5,
		CooldownDuration:     15 * time.Minute,
	}

	engine := risk.NewRiskEngine(risk.DefaultPositionLimits(), cbConfig, r.Position, discardLogger())
	engine.SetFileExistsFunc(func(string) bool { return false })
	engine.SetGetEnvFunc(func(string) string { return "" })

	// 6 consecutive losses exceeds the 5 threshold.
	if err := engine.UpdateMetrics(ctx, 0.0, 0.0, 6); err != nil {
		t.Fatalf("UpdateMetrics() error: %v", err)
	}

	status, _ := engine.GetStatus(ctx)
	if status.CircuitBreaker.State != risk.CircuitBreakerPhaseTripped {
		t.Fatalf("expected tripped on consecutive losses, got %q", status.CircuitBreaker.State)
	}
	if !strings.Contains(status.CircuitBreaker.Reason, "consecutive losses") {
		t.Fatalf("expected consecutive losses in reason, got %q", status.CircuitBreaker.Reason)
	}
}

// TestIntegration_RiskEngine_KillSwitchBlocksTrades validates that activating
// the kill switch blocks pre-trade checks.
func TestIntegration_RiskEngine_KillSwitchBlocksTrades(t *testing.T) {
	db := newTestDB(t)
	r := newRepos(db)
	ctx := context.Background()

	engine := risk.NewRiskEngine(risk.DefaultPositionLimits(), risk.DefaultCircuitBreakerConfig(), r.Position, discardLogger())
	engine.SetFileExistsFunc(func(string) bool { return false })
	engine.SetGetEnvFunc(func(string) string { return "" })

	order := &domain.Order{
		Ticker:   "AAPL",
		Quantity: 10,
		Side:     domain.OrderSideBuy,
	}
	portfolio := risk.Portfolio{
		TotalExposurePct:         0.05,
		ConcurrentPositions:      1,
		PositionExposureBySymbol: map[string]float64{"AAPL": 0.05},
	}

	// Activate kill switch.
	if err := engine.ActivateKillSwitch(ctx, "emergency halt"); err != nil {
		t.Fatalf("ActivateKillSwitch() error: %v", err)
	}

	active, err := engine.IsKillSwitchActive(ctx)
	if err != nil {
		t.Fatalf("IsKillSwitchActive() error: %v", err)
	}
	if !active {
		t.Fatal("expected kill switch to be active")
	}

	// Pre-trade should be blocked.
	approved, reason, err := engine.CheckPreTrade(ctx, order, portfolio)
	if err != nil {
		t.Fatalf("CheckPreTrade() error: %v", err)
	}
	if approved {
		t.Fatal("expected pre-trade to be blocked by kill switch")
	}
	if !strings.Contains(reason, "kill switch") {
		t.Fatalf("expected kill switch in reason, got %q", reason)
	}

	// Deactivate kill switch.
	if err := engine.DeactivateKillSwitch(ctx); err != nil {
		t.Fatalf("DeactivateKillSwitch() error: %v", err)
	}

	// Pre-trade should pass again.
	approved, _, err = engine.CheckPreTrade(ctx, order, portfolio)
	if err != nil {
		t.Fatalf("CheckPreTrade() after deactivation error: %v", err)
	}
	if !approved {
		t.Fatal("expected pre-trade to pass after kill switch deactivation")
	}
}

// TestIntegration_RiskEngine_PositionLimits validates position size checks
// against the real position repository.
func TestIntegration_RiskEngine_PositionLimits(t *testing.T) {
	db := newTestDB(t)
	r := newRepos(db)
	ctx := context.Background()

	limits := risk.PositionLimits{
		MaxPerPositionPct: 0.20,
		MaxTotalPct:       0.50,
		MaxConcurrent:     3,
		MaxPerMarketPct:   0.50,
	}

	engine := risk.NewRiskEngine(limits, risk.DefaultCircuitBreakerConfig(), r.Position, discardLogger())
	engine.SetFileExistsFunc(func(string) bool { return false })
	engine.SetGetEnvFunc(func(string) string { return "" })

	// Within limits.
	approved, _, err := engine.CheckPositionLimits(ctx, "AAPL", 0.10, risk.Portfolio{
		TotalExposurePct:         0.05,
		ConcurrentPositions:      1,
		PositionExposureBySymbol: map[string]float64{"AAPL": 0.05},
	})
	if err != nil {
		t.Fatalf("CheckPositionLimits() error: %v", err)
	}
	if !approved {
		t.Fatal("expected position limit check to pass within limits")
	}

	// Exceeds per-position limit.
	approved, reason, err := engine.CheckPositionLimits(ctx, "AAPL", 0.10, risk.Portfolio{
		TotalExposurePct:         0.15,
		ConcurrentPositions:      1,
		PositionExposureBySymbol: map[string]float64{"AAPL": 0.15},
	})
	if err != nil {
		t.Fatalf("CheckPositionLimits() error: %v", err)
	}
	if approved {
		t.Fatal("expected position limit check to fail exceeding per-position limit")
	}
	if !strings.Contains(reason, "position size") {
		t.Fatalf("expected per-position rejection, got %q", reason)
	}

	// Exceeds max concurrent positions.
	approved, reason, err = engine.CheckPositionLimits(ctx, "GOOG", 0.05, risk.Portfolio{
		TotalExposurePct:    0.30,
		ConcurrentPositions: 3,
		PositionExposureBySymbol: map[string]float64{
			"AAPL": 0.10, "MSFT": 0.10, "TSLA": 0.10,
		},
	})
	if err != nil {
		t.Fatalf("CheckPositionLimits() error: %v", err)
	}
	if approved {
		t.Fatal("expected rejection for max concurrent positions")
	}
	if !strings.Contains(reason, "concurrent positions") {
		t.Fatalf("expected concurrent positions in reason, got %q", reason)
	}
}
