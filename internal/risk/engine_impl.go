package risk

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// DefaultPositionLimits returns the default position limits.
func DefaultPositionLimits() PositionLimits {
	return PositionLimits{
		MaxPerPositionPct: 0.20,
		MaxTotalPct:       1.00,
		MaxConcurrent:     10,
		MaxPerMarketPct:   0.50,
	}
}

// engineState holds the mutable risk engine state protected by a mutex.
type engineState struct {
	mu  sync.RWMutex
	cb  CircuitBreakerStatus
	ks  KillSwitchStatus
	mks map[domain.MarketType]KillSwitchStatus // per-market kill switches
}

// RiskEngineImpl is the concrete implementation of RiskEngine.
type RiskEngineImpl struct {
	limits                PositionLimits
	pmLimits              PolymarketLimits
	cbConfig              CircuitBreakerConfig
	positionRepo          repository.PositionRepository
	logger                *slog.Logger
	state                 engineState
	nowMu                 sync.RWMutex
	nowFunc               func() time.Time // for testability; defaults to time.Now
	killSwitchFilePath    string           // file flag path; defaults to defaultKillSwitchFilePath
	ksMu                  sync.RWMutex
	fileExistsFunc        func(string) bool   // for testability; defaults to defaultFileExists
	getEnvFunc            func(string) string // for testability; defaults to os.Getenv
	portfolioSnapshotMu   sync.RWMutex
	portfolioSnapshotFunc func(context.Context) (Portfolio, error)
	persister             StatePersister // optional; nil = no DB persistence
}

// defaultFileExists checks whether the given path exists on the filesystem.
// For safety-critical kill switch behavior, any error other than "not exists"
// (e.g., permission denied, transient I/O error) is treated as if the file exists.
func defaultFileExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return true
}

// NewRiskEngine creates a new RiskEngineImpl.
func NewRiskEngine(limits PositionLimits, cbConfig CircuitBreakerConfig, positionRepo repository.PositionRepository, logger *slog.Logger) *RiskEngineImpl {
	if logger == nil {
		logger = slog.Default()
	}
	return &RiskEngineImpl{
		limits:             limits,
		pmLimits:           DefaultPolymarketLimits(),
		cbConfig:           cbConfig,
		positionRepo:       positionRepo,
		logger:             logger,
		nowFunc:            time.Now,
		killSwitchFilePath: defaultKillSwitchFilePath,
		fileExistsFunc:     defaultFileExists,
		getEnvFunc:         os.Getenv,
		state: engineState{
			cb:  CircuitBreakerStatus{State: CircuitBreakerPhaseOpen},
			ks:  KillSwitchStatus{Active: false},
			mks: make(map[domain.MarketType]KillSwitchStatus),
		},
	}
}

// WithPolymarketLimits sets prediction-market-specific risk limits on the engine.
// Call this after NewRiskEngine before the engine handles any requests.
func (e *RiskEngineImpl) WithPolymarketLimits(limits PolymarketLimits) *RiskEngineImpl {
	e.pmLimits = limits
	return e
}

// CheckPolymarketPreConditions validates liquidity, spread, and resolution
// timeline conditions for a prediction market entry. Returns false with a
// reason string when any limit is breached.
func CheckPolymarketPreConditions(limits PolymarketLimits, liquidity, spreadMid float64, daysToResolution int, positionUSDC float64) (bool, string) {
	if limits.MinLiquidity > 0 && liquidity < limits.MinLiquidity {
		return false, fmt.Sprintf("polymarket: liquidity $%.0f below minimum $%.0f USDC", liquidity, limits.MinLiquidity)
	}
	if limits.MaxSpreadPct > 0 && spreadMid > limits.MaxSpreadPct {
		return false, fmt.Sprintf("polymarket: bid-ask spread %.1f%% exceeds max %.1f%%", spreadMid*100, limits.MaxSpreadPct*100)
	}
	if limits.MinDaysToResolution > 0 && daysToResolution < limits.MinDaysToResolution {
		return false, fmt.Sprintf("polymarket: market resolves in %d day(s), minimum is %d", daysToResolution, limits.MinDaysToResolution)
	}
	if limits.MaxPositionUSDC > 0 && positionUSDC > limits.MaxPositionUSDC {
		return false, fmt.Sprintf("polymarket: position size $%.0f exceeds cap $%.0f USDC", positionUSDC, limits.MaxPositionUSDC)
	}
	return true, ""
}

// SetNowFunc overrides the risk engine time source, allowing backtests to
// evaluate cooldowns and status timestamps against simulated time.
func (e *RiskEngineImpl) SetNowFunc(now func() time.Time) {
	if e == nil || now == nil {
		return
	}

	e.nowMu.Lock()
	defer e.nowMu.Unlock()

	e.nowFunc = now
}

// SetFileExistsFunc overrides the file-existence check used by the kill
// switch, enabling deterministic test behavior without touching the
// filesystem.
func (e *RiskEngineImpl) SetFileExistsFunc(fn func(string) bool) {
	if e == nil || fn == nil {
		return
	}
	e.ksMu.Lock()
	defer e.ksMu.Unlock()
	e.fileExistsFunc = fn
}

// SetGetEnvFunc overrides the environment-variable lookup used by the kill
// switch, enabling deterministic test behavior without mutating the process
// environment.
func (e *RiskEngineImpl) SetGetEnvFunc(fn func(string) string) {
	if e == nil || fn == nil {
		return
	}
	e.ksMu.Lock()
	defer e.ksMu.Unlock()
	e.getEnvFunc = fn
}

// SetPortfolioSnapshotFunc overrides how GetStatus derives live portfolio
// utilization for status responses.
func (e *RiskEngineImpl) SetPortfolioSnapshotFunc(fn func(context.Context) (Portfolio, error)) {
	if e == nil {
		return
	}
	e.portfolioSnapshotMu.Lock()
	defer e.portfolioSnapshotMu.Unlock()
	e.portfolioSnapshotFunc = fn
}

func (e *RiskEngineImpl) currentTime() time.Time {
	if e == nil {
		return time.Now()
	}

	e.nowMu.RLock()
	defer e.nowMu.RUnlock()

	if e.nowFunc == nil {
		return time.Now()
	}

	return e.nowFunc()
}

// checkCooldownLocked checks if the circuit breaker cooldown has expired and
// auto-resets to open. Must be called with e.state.mu held for writing.
// Returns true if the breaker was auto-reset so the caller can log outside
// the critical section.
func (e *RiskEngineImpl) checkCooldownLocked() bool {
	if e.state.cb.State != CircuitBreakerPhaseTripped {
		return false
	}
	if e.state.cb.CooldownEnd == nil {
		return false
	}
	if e.currentTime().Before(*e.state.cb.CooldownEnd) {
		return false
	}
	e.state.cb = CircuitBreakerStatus{State: CircuitBreakerPhaseOpen}
	return true
}

// tripLocked transitions the circuit breaker from open to tripped under the
// write lock. It is a no-op when the breaker is already tripped, preserving
// the original reason/timestamp. Must be called with e.state.mu held for
// writing. Returns true if the state was changed so the caller can log
// outside the critical section.
func (e *RiskEngineImpl) tripLocked(reason string) bool {
	if e.state.cb.State == CircuitBreakerPhaseTripped {
		return false
	}
	now := e.currentTime()
	cooldownEnd := now.Add(e.cbConfig.CooldownDuration)
	e.state.cb = CircuitBreakerStatus{
		State:       CircuitBreakerPhaseTripped,
		Reason:      reason,
		TrippedAt:   &now,
		CooldownEnd: &cooldownEnd,
	}
	return true
}

// CheckPreTrade evaluates whether an order should be allowed before submission.
func (e *RiskEngineImpl) CheckPreTrade(ctx context.Context, order *domain.Order, _ Portfolio) (bool, string, error) {
	if order == nil {
		return false, "order is nil", nil
	}
	if order.Ticker == "" {
		return false, "order ticker is required", nil
	}
	if order.Quantity <= 0 {
		return false, "order quantity must be positive", nil
	}
	reduceOnly := order.IsReduceOnly()

	e.state.mu.Lock()
	cooldownReset := e.checkCooldownLocked()
	apiKS := e.state.ks
	cb := e.state.cb
	e.state.mu.Unlock()

	if cooldownReset {
		e.logger.InfoContext(ctx, "circuit breaker auto-reset after cooldown")
	}

	ksActive, _ := e.isKillSwitchActiveUnlocked(apiKS)
	if ksActive && !reduceOnly {
		reason := apiKS.Reason
		if reason == "" {
			reason = "external mechanism"
		}
		return false, fmt.Sprintf("kill switch is active: %s", reason), nil
	}

	// Per-market kill switch check.
	if order != nil && order.MarketType != "" {
		mks, hasMKS := e.activeMarketKillSwitchSnapshot(order.MarketType)
		if hasMKS && mks.Active && !reduceOnly {
			reason := mks.Reason
			if reason == "" {
				reason = "market kill switch active"
			}
			return false, fmt.Sprintf("%s market kill switch is active: %s", order.MarketType, reason), nil
		}
	}

	if cb.State == CircuitBreakerPhaseTripped && !reduceOnly {
		return false, fmt.Sprintf("circuit breaker tripped: %s", cb.Reason), nil
	}

	e.logger.InfoContext(ctx, "pre-trade check passed",
		slog.String("ticker", order.Ticker),
		slog.Float64("quantity", order.Quantity),
		slog.Bool("reduce_only", reduceOnly),
	)
	return true, "", nil
}

// CheckPositionLimits evaluates whether adding quantity for ticker stays within limits.
// The quantity parameter represents the additional position exposure as a fraction of
// the portfolio (e.g. 0.10 = 10%).
func (e *RiskEngineImpl) CheckPositionLimits(ctx context.Context, ticker string, quantity float64, portfolio Portfolio) (bool, string, error) {
	allowed, reason := newMarketExposurePolicy(e.limits, e.pmLimits).check(ticker, quantity, portfolio)
	if !allowed {
		return false, reason, nil
	}

	e.logger.InfoContext(ctx, "position limits check passed",
		slog.String("ticker", ticker),
		slog.Float64("quantity", quantity),
	)
	return true, "", nil
}

// GetStatus returns the current engine state.
func (e *RiskEngineImpl) GetStatus(ctx context.Context) (EngineStatus, error) {
	e.state.mu.Lock()
	cooldownReset := e.checkCooldownLocked()
	apiKS := e.state.ks
	cb := e.state.cb
	limits := e.limits
	e.state.mu.Unlock()

	e.portfolioSnapshotMu.RLock()
	portfolioSnapshotFunc := e.portfolioSnapshotFunc
	e.portfolioSnapshotMu.RUnlock()

	var portfolioSnapshot *Portfolio
	if portfolioSnapshotFunc != nil {
		portfolio, err := portfolioSnapshotFunc(ctx)
		if err != nil {
			return EngineStatus{}, fmt.Errorf("risk: portfolio snapshot: %w", err)
		}
		portfolioSnapshot = &portfolio
	}

	ks := e.buildKillSwitchStatus(apiKS)
	es := projectEngineStatus(e.currentTime(), limits, cb, ks, e.snapshotMarketKillSwitches(), portfolioSnapshot)

	if cooldownReset {
		e.logger.InfoContext(ctx, "circuit breaker auto-reset after cooldown")
	}

	return es, nil
}

// TripCircuitBreaker activates the circuit breaker. It is a no-op if the
// breaker is already tripped, preserving the original reason and timestamps.
func (e *RiskEngineImpl) TripCircuitBreaker(ctx context.Context, reason string) error {
	e.state.mu.Lock()
	tripped := e.tripLocked(reason)
	var cooldownEnd time.Time
	if tripped && e.state.cb.CooldownEnd != nil {
		cooldownEnd = *e.state.cb.CooldownEnd
	}
	e.state.mu.Unlock()

	if tripped {
		e.logger.WarnContext(ctx, "circuit breaker tripped",
			slog.String("reason", reason),
			slog.Time("cooldown_end", cooldownEnd),
		)
	}
	return nil
}

// ResetCircuitBreaker resets the circuit breaker to open state.
func (e *RiskEngineImpl) ResetCircuitBreaker(ctx context.Context) error {
	e.state.mu.Lock()
	e.state.cb = CircuitBreakerStatus{State: CircuitBreakerPhaseOpen}
	e.state.mu.Unlock()

	e.logger.InfoContext(ctx, "circuit breaker reset")
	return nil
}

// IsKillSwitchActive returns whether any kill switch mechanism is active
// (API toggle, file flag, or environment variable).
func (e *RiskEngineImpl) IsKillSwitchActive(_ context.Context) (bool, error) {
	e.state.mu.RLock()
	apiKS := e.state.ks
	e.state.mu.RUnlock()

	active, _ := e.isKillSwitchActiveUnlocked(apiKS)
	return active, nil
}

// ActivateKillSwitch activates the kill switch via the API toggle mechanism.
func (e *RiskEngineImpl) ActivateKillSwitch(ctx context.Context, reason string) error {
	e.state.mu.Lock()
	e.activateKillSwitchLocked(reason)
	snapshot := e.buildPersistedStateLocked()
	e.state.mu.Unlock()

	e.logger.WarnContext(ctx, "kill switch activated",
		slog.String("reason", reason),
		slog.String("mechanism", KillSwitchMechanismAPI.String()),
	)
	return e.saveState(ctx, snapshot)
}

// DeactivateKillSwitch deactivates the API toggle mechanism of the kill switch.
// Note: file flag and env var mechanisms are not affected by this call.
func (e *RiskEngineImpl) DeactivateKillSwitch(ctx context.Context) error {
	e.state.mu.Lock()
	e.deactivateKillSwitchLocked()
	snapshot := e.buildPersistedStateLocked()
	e.state.mu.Unlock()

	e.logger.InfoContext(ctx, "kill switch deactivated",
		slog.String("mechanism", KillSwitchMechanismAPI.String()),
	)
	return e.saveState(ctx, snapshot)
}

// IsMarketKillSwitchActive returns whether the kill switch is active for the
// given market type.
func (e *RiskEngineImpl) IsMarketKillSwitchActive(_ context.Context, marketType domain.MarketType) (bool, error) {
	mks, ok := e.activeMarketKillSwitchSnapshot(marketType)
	return ok && mks.Active, nil
}

// ActivateMarketKillSwitch activates the kill switch for a specific market type,
// halting all new orders for that market while leaving other markets unaffected.
func (e *RiskEngineImpl) ActivateMarketKillSwitch(ctx context.Context, marketType domain.MarketType, reason string) error {
	e.state.mu.Lock()
	e.activateMarketKillSwitchLocked(marketType, reason)
	snapshot := e.buildPersistedStateLocked()
	e.state.mu.Unlock()

	e.logger.WarnContext(ctx, "market kill switch activated",
		slog.String("market_type", string(marketType)),
		slog.String("reason", reason),
	)
	return e.saveState(ctx, snapshot)
}

// DeactivateMarketKillSwitch clears the kill switch for the given market type.
func (e *RiskEngineImpl) DeactivateMarketKillSwitch(ctx context.Context, marketType domain.MarketType) error {
	e.state.mu.Lock()
	e.deactivateMarketKillSwitchLocked(marketType)
	snapshot := e.buildPersistedStateLocked()
	e.state.mu.Unlock()

	e.logger.InfoContext(ctx, "market kill switch deactivated",
		slog.String("market_type", string(marketType)),
	)
	return e.saveState(ctx, snapshot)
}

// UpdateMetrics evaluates post-trade metrics and auto-trips the circuit breaker
// when any threshold is exceeded. dailyPnL is a signed fraction (negative = loss),
// totalDrawdown is a positive fraction representing decline from peak, and
// consecutiveLosses is the running count of consecutive losing trades.
// The check and trip are performed atomically under one lock to avoid TOCTOU races.
func (e *RiskEngineImpl) UpdateMetrics(ctx context.Context, dailyPnL, totalDrawdown float64, consecutiveLosses int) error {
	e.state.mu.Lock()
	cooldownReset := e.checkCooldownLocked()

	// Only auto-trip if currently open.
	if e.state.cb.State != CircuitBreakerPhaseOpen {
		e.state.mu.Unlock()
		if cooldownReset {
			e.logger.InfoContext(ctx, "circuit breaker auto-reset after cooldown")
		}
		return nil
	}

	var reason string
	switch {
	case dailyPnL < -e.cbConfig.MaxDailyLossPct:
		reason = fmt.Sprintf(
			"daily loss %.2f%% exceeds max %.2f%%",
			-dailyPnL*100, e.cbConfig.MaxDailyLossPct*100,
		)
	case totalDrawdown > e.cbConfig.MaxDrawdownPct:
		reason = fmt.Sprintf(
			"drawdown %.2f%% exceeds max %.2f%%",
			totalDrawdown*100, e.cbConfig.MaxDrawdownPct*100,
		)
	case consecutiveLosses > e.cbConfig.MaxConsecutiveLosses:
		reason = fmt.Sprintf(
			"consecutive losses %d exceeds max %d",
			consecutiveLosses, e.cbConfig.MaxConsecutiveLosses,
		)
	}

	var tripped bool
	var cooldownEnd time.Time
	if reason != "" {
		tripped = e.tripLocked(reason)
		if tripped && e.state.cb.CooldownEnd != nil {
			cooldownEnd = *e.state.cb.CooldownEnd
		}
	}
	e.state.mu.Unlock()

	if cooldownReset {
		e.logger.InfoContext(ctx, "circuit breaker auto-reset after cooldown")
	}
	if tripped {
		e.logger.WarnContext(ctx, "circuit breaker tripped",
			slog.String("reason", reason),
			slog.Time("cooldown_end", cooldownEnd),
		)
	}

	return nil
}
