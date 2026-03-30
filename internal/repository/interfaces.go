package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// ErrNotFound is returned by repository implementations when a requested
// entity does not exist. Callers should check with errors.Is.
var ErrNotFound = errors.New("not found")

// StrategyFilter defines supported filters when listing strategies.
type StrategyFilter struct {
	Ticker     string
	MarketType domain.MarketType
	IsActive   *bool
	IsPaper    *bool
}

// BacktestConfigFilter defines supported filters when listing backtest configurations.
type BacktestConfigFilter struct {
	StrategyID    *uuid.UUID
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
}

// BacktestRunFilter defines supported filters when listing persisted backtest runs.
type BacktestRunFilter struct {
	BacktestConfigID  *uuid.UUID
	PromptVersion     string
	PromptVersionHash string
	RunAfter          *time.Time
	RunBefore         *time.Time
}

// PipelineRunFilter defines supported filters when listing pipeline runs.
type PipelineRunFilter struct {
	StrategyID    *uuid.UUID
	Ticker        string
	Status        domain.PipelineStatus
	TradeDate     *time.Time
	StartedAfter  *time.Time
	StartedBefore *time.Time
}

// PipelineRunStatusUpdate defines the fields that may change when updating run status.
type PipelineRunStatusUpdate struct {
	Status       domain.PipelineStatus
	Signal       *domain.PipelineSignal
	CompletedAt  *time.Time
	ErrorMessage string
}

// AgentDecisionFilter defines supported filters when retrieving agent decisions.
type AgentDecisionFilter struct {
	AgentRole   domain.AgentRole
	Phase       domain.Phase
	RoundNumber *int
}

// OrderFilter defines supported filters when listing or querying orders.
type OrderFilter struct {
	Ticker          string
	Broker          string
	Side            domain.OrderSide
	OrderType       domain.OrderType
	Status          domain.OrderStatus
	SubmittedAfter  *time.Time
	SubmittedBefore *time.Time
}

// PositionFilter defines supported filters when listing or querying positions.
type PositionFilter struct {
	Ticker       string
	Side         domain.PositionSide
	OpenedAfter  *time.Time
	OpenedBefore *time.Time
}

// TradeFilter defines supported filters when retrieving trades.
type TradeFilter struct {
	OrderID    *uuid.UUID
	PositionID *uuid.UUID
	Ticker     *string
	Side       *domain.OrderSide
	StartDate  *time.Time
	EndDate    *time.Time
}

// MemorySearchFilter defines supported filters when searching agent memories.
type MemorySearchFilter struct {
	AgentRole         domain.AgentRole
	PipelineRunID     *uuid.UUID
	MinRelevanceScore *float64
	CreatedAfter      *time.Time
	CreatedBefore     *time.Time
}

// MarketDataCacheKey identifies a cached market data entry.
type MarketDataCacheKey struct {
	Ticker    string
	Provider  string
	DataType  string
	Timeframe string
	DateFrom  *time.Time
	DateTo    *time.Time
}

// MarketDataCacheExpireFilter defines supported filters when expiring cache entries.
type MarketDataCacheExpireFilter struct {
	Ticker        string
	Provider      string
	DataType      string
	ExpiresBefore time.Time
}

// HistoricalOHLCVFilter defines supported filters when listing stored OHLCV bars.
type HistoricalOHLCVFilter struct {
	Ticker    string
	Provider  string
	Timeframe string
	From      time.Time
	To        time.Time
}

// HistoricalOHLCVCoverageFilter defines supported filters when listing fetched
// historical OHLCV coverage ranges.
type HistoricalOHLCVCoverageFilter struct {
	Ticker    string
	Provider  string
	Timeframe string
	From      time.Time
	To        time.Time
}

// AuditLogFilter defines supported filters when querying audit log entries.
type AuditLogFilter struct {
	EventType     string
	EntityType    string
	EntityID      *uuid.UUID
	Actor         string
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
}

// StrategyRepository provides CRUD operations for strategies.
type StrategyRepository interface {
	Create(ctx context.Context, strategy *domain.Strategy) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Strategy, error)
	List(ctx context.Context, filter StrategyFilter, limit, offset int) ([]domain.Strategy, error)
	Update(ctx context.Context, strategy *domain.Strategy) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// BacktestConfigRepository provides CRUD operations for backtest configurations.
type BacktestConfigRepository interface {
	Create(ctx context.Context, config *domain.BacktestConfig) error
	Get(ctx context.Context, id uuid.UUID) (*domain.BacktestConfig, error)
	List(ctx context.Context, filter BacktestConfigFilter, limit, offset int) ([]domain.BacktestConfig, error)
	Update(ctx context.Context, config *domain.BacktestConfig) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// BacktestRunRepository provides access to persisted backtest run results.
type BacktestRunRepository interface {
	Create(ctx context.Context, run *domain.BacktestRun) error
	Get(ctx context.Context, id uuid.UUID) (*domain.BacktestRun, error)
	List(ctx context.Context, filter BacktestRunFilter, limit, offset int) ([]domain.BacktestRun, error)
}

// PipelineRunRepository provides access to pipeline runs.
type PipelineRunRepository interface {
	Create(ctx context.Context, run *domain.PipelineRun) error
	Get(ctx context.Context, id uuid.UUID, tradeDate time.Time) (*domain.PipelineRun, error)
	List(ctx context.Context, filter PipelineRunFilter, limit, offset int) ([]domain.PipelineRun, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, tradeDate time.Time, update PipelineRunStatusUpdate) error
}

// AgentDecisionRepository provides access to agent decisions created during a run.
type AgentDecisionRepository interface {
	Create(ctx context.Context, decision *domain.AgentDecision) error
	GetByRun(ctx context.Context, runID uuid.UUID, filter AgentDecisionFilter, limit, offset int) ([]domain.AgentDecision, error)
}

// OrderRepository provides CRUD operations for orders.
type OrderRepository interface {
	Create(ctx context.Context, order *domain.Order) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Order, error)
	List(ctx context.Context, filter OrderFilter, limit, offset int) ([]domain.Order, error)
	Update(ctx context.Context, order *domain.Order) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetByStrategy(ctx context.Context, strategyID uuid.UUID, filter OrderFilter, limit, offset int) ([]domain.Order, error)
	GetByRun(ctx context.Context, runID uuid.UUID, filter OrderFilter, limit, offset int) ([]domain.Order, error)
}

// PositionRepository provides CRUD operations for positions.
type PositionRepository interface {
	Create(ctx context.Context, position *domain.Position) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Position, error)
	List(ctx context.Context, filter PositionFilter, limit, offset int) ([]domain.Position, error)
	Update(ctx context.Context, position *domain.Position) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetOpen(ctx context.Context, filter PositionFilter, limit, offset int) ([]domain.Position, error)
	GetByStrategy(ctx context.Context, strategyID uuid.UUID, filter PositionFilter, limit, offset int) ([]domain.Position, error)
}

// TradeRepository provides access to executed trades.
type TradeRepository interface {
	Create(ctx context.Context, trade *domain.Trade) error
	List(ctx context.Context, filter TradeFilter, limit, offset int) ([]domain.Trade, error)
	GetByOrder(ctx context.Context, orderID uuid.UUID, filter TradeFilter, limit, offset int) ([]domain.Trade, error)
	GetByPosition(ctx context.Context, positionID uuid.UUID, filter TradeFilter, limit, offset int) ([]domain.Trade, error)
}

// MemoryRepository provides storage and retrieval for agent memories.
type MemoryRepository interface {
	Create(ctx context.Context, memory *domain.AgentMemory) error
	// Search performs full-text search over stored memories using the provided query and filters.
	Search(ctx context.Context, query string, filter MemorySearchFilter, limit, offset int) ([]domain.AgentMemory, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

// MarketDataCacheRepository provides access to cached market data.
type MarketDataCacheRepository interface {
	Get(ctx context.Context, key MarketDataCacheKey) (*domain.MarketData, error)
	// Set stores a cache entry using the expiry already carried on domain.MarketData.ExpiresAt.
	Set(ctx context.Context, data *domain.MarketData) error
	Expire(ctx context.Context, filter MarketDataCacheExpireFilter) error
}

// HistoricalOHLCVRepository provides access to persisted historical OHLCV data.
type HistoricalOHLCVRepository interface {
	UpsertHistoricalOHLCV(ctx context.Context, bars []domain.HistoricalOHLCV) error
	ListHistoricalOHLCV(ctx context.Context, filter HistoricalOHLCVFilter) ([]domain.HistoricalOHLCV, error)
	UpsertHistoricalOHLCVCoverage(ctx context.Context, coverage domain.HistoricalOHLCVCoverage) error
	ListHistoricalOHLCVCoverage(ctx context.Context, filter HistoricalOHLCVCoverageFilter) ([]domain.HistoricalOHLCVCoverage, error)
}

// AuditLogRepository provides append/query access to audit log entries.
type AuditLogRepository interface {
	Create(ctx context.Context, entry *domain.AuditLogEntry) error
	Query(ctx context.Context, filter AuditLogFilter, limit, offset int) ([]domain.AuditLogEntry, error)
}

// APIKeyRepository provides storage for hashed API keys used for programmatic access.
type APIKeyRepository interface {
	Create(ctx context.Context, key *domain.APIKey) error
	GetByPrefix(ctx context.Context, prefix string) (*domain.APIKey, error)
	List(ctx context.Context, limit, offset int) ([]domain.APIKey, error)
	Revoke(ctx context.Context, id uuid.UUID, revokedAt time.Time) error
	TouchLastUsed(ctx context.Context, id uuid.UUID, lastUsedAt time.Time) error
}
