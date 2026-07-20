package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// TradeDecisionJournalRepo implements repository.TradeDecisionJournalRepository using PostgreSQL.
type TradeDecisionJournalRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that TradeDecisionJournalRepo satisfies the repository interface.
var _ repository.TradeDecisionJournalRepository = (*TradeDecisionJournalRepo)(nil)

// NewTradeDecisionJournalRepo returns a repository backed by the given pool.
func NewTradeDecisionJournalRepo(pool *pgxpool.Pool) *TradeDecisionJournalRepo {
	return &TradeDecisionJournalRepo{pool: pool}
}

const tradeDecisionSelectSQL = `SELECT id, strategy_id, pipeline_run_id, market_type, instrument_key,
		external_market_id, side, outcome, fair_value::double precision,
		executable_price::double precision, spread::double precision,
		depth::double precision, gross_ev::double precision, net_ev::double precision,
		kelly_fraction::double precision, proposed_size::double precision,
		approved_size::double precision, risk_status, risk_reasons, evidence,
		features, regime_tags, prompt_text, llm_provider, llm_model,
		prompt_tokens, completion_tokens, latency_ms, cost_usd::double precision,
	paper_order_id, live_order_id, status, created_at,
		updated_at
	 FROM trade_decisions`

// Create inserts a new trade decision and populates the generated ID and timestamps.
func (r *TradeDecisionJournalRepo) Create(ctx context.Context, decision *domain.TradeDecision) error {
	evidence, err := marshalTradeDecisionJSON(decision.Evidence)
	if err != nil {
		return err
	}
	features, err := marshalTradeDecisionJSON(decision.Features)
	if err != nil {
		return err
	}

	row := r.pool.QueryRow(ctx,
		`INSERT INTO trade_decisions (
			strategy_id, pipeline_run_id, market_type, instrument_key, external_market_id,
			side, outcome, fair_value, executable_price, spread, depth, gross_ev,
			net_ev, kelly_fraction, proposed_size, approved_size, risk_status,
			risk_reasons, evidence, features, regime_tags, prompt_text, llm_provider,
			llm_model, prompt_tokens, completion_tokens, latency_ms, cost_usd,
			paper_order_id, live_order_id, status
		)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
		         $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27,
		         $28, $29, $30, $31)
		 RETURNING id, created_at, updated_at`,
		decision.StrategyID,
		decision.PipelineRunID,
		decision.MarketType,
		decision.InstrumentKey,
		nullString(decision.ExternalMarketID),
		decision.Side,
		nullString(decision.Outcome),
		decision.FairValue,
		decision.ExecutablePrice,
		decision.Spread,
		decision.Depth,
		decision.GrossEV,
		decision.NetEV,
		decision.KellyFraction,
		decision.ProposedSize,
		decision.ApprovedSize,
		decision.RiskStatus,
		stringSliceOrEmpty(decision.RiskReasons),
		evidence,
		features,
		stringSliceOrEmpty(decision.RegimeTags),
		nullString(decision.PromptText),
		nullString(decision.LLMProvider),
		nullString(decision.LLMModel),
		nullableInt(decision.PromptTokens),
		nullableInt(decision.CompletionTokens),
		nullableInt(decision.LatencyMS),
		nullableFloat(decision.CostUSD),
		decision.PaperOrderID,
		decision.LiveOrderID,
		decision.Status,
	)

	if err := row.Scan(&decision.ID, &decision.CreatedAt, &decision.UpdatedAt); err != nil {
		return fmt.Errorf("postgres: create trade decision: %w", err)
	}

	return nil
}

// Get retrieves a trade decision by ID.
func (r *TradeDecisionJournalRepo) Get(ctx context.Context, id uuid.UUID) (*domain.TradeDecision, error) {
	row := r.pool.QueryRow(ctx, tradeDecisionSelectSQL+` WHERE id = $1`, id)
	decision, err := scanTradeDecision(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: get trade decision %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get trade decision: %w", err)
	}
	return decision, nil
}

// List returns trade decisions matching the provided filter with pagination.
func (r *TradeDecisionJournalRepo) List(ctx context.Context, filter repository.TradeDecisionFilter, limit, offset int) ([]domain.TradeDecision, error) {
	query, args := buildTradeDecisionListQuery(filter, limit, offset)
	return r.list(ctx, query, args, "list trade decisions")
}

// Count returns the number of trade decisions matching the filter.
func (r *TradeDecisionJournalRepo) Count(ctx context.Context, filter repository.TradeDecisionFilter) (int, error) {
	query, args := buildTradeDecisionCountQuery(filter)
	var total int
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("postgres: count trade decisions: %w", err)
	}
	return total, nil
}

func (r *TradeDecisionJournalRepo) CountByStatus(ctx context.Context, filter repository.TradeDecisionFilter) (map[domain.TradeDecisionStatus]int, error) {
	query, args := buildTradeDecisionFilteredQuery("SELECT status, COUNT(*) FROM trade_decisions", filter, 0, 0, false)
	query += " GROUP BY status ORDER BY status"
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: count trade decisions by status: %w", err)
	}
	defer rows.Close()
	out := map[domain.TradeDecisionStatus]int{}
	for rows.Next() {
		var key string
		var total int
		if err := rows.Scan(&key, &total); err != nil {
			return nil, fmt.Errorf("postgres: count trade decisions by status scan: %w", err)
		}
		out[domain.TradeDecisionStatus(key)] = total
	}
	return out, rows.Err()
}

func (r *TradeDecisionJournalRepo) CountByNoActionReason(ctx context.Context, filter repository.TradeDecisionFilter) (map[string]int, error) {
	filtered, args := buildTradeDecisionFilteredQuery(`SELECT
			id, risk_reasons, status, evidence
			FROM trade_decisions`, filter, 0, 0, false)
	query := `SELECT
		COALESCE(SUM(CASE WHEN risk_reasons @> ARRAY['hold_signal']::text[] OR status = 'hold' THEN 1 ELSE 0 END), 0) AS hold_signal,
		COALESCE(SUM(CASE WHEN risk_reasons @> ARRAY['risk_rejected']::text[] OR evidence::text ILIKE '%risk%' THEN 1 ELSE 0 END), 0) AS risk_rejected,
		COALESCE(SUM(CASE WHEN risk_reasons @> ARRAY['sizing_zero']::text[] OR evidence::text ILIKE '%size%0%' THEN 1 ELSE 0 END), 0) AS sizing_zero,
		COALESCE(SUM(CASE WHEN risk_reasons @> ARRAY['sell_without_position']::text[] THEN 1 ELSE 0 END), 0) AS sell_without_position,
		COALESCE(SUM(CASE WHEN risk_reasons @> ARRAY['kill_switch']::text[] THEN 1 ELSE 0 END), 0) AS kill_switch,
		COALESCE(SUM(CASE WHEN risk_reasons @> ARRAY['live_gate_denied']::text[] THEN 1 ELSE 0 END), 0) AS live_gate_denied,
		COALESCE(SUM(CASE WHEN risk_reasons @> ARRAY['missing_data']::text[] THEN 1 ELSE 0 END), 0) AS missing_data,
		COALESCE(SUM(CASE WHEN COALESCE(array_length(risk_reasons,1),0)=0 AND evidence IS NULL THEN 1 ELSE 0 END), 0) AS unknown
		FROM (` + filtered + `) td`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: count trade decisions reasons: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	if rows.Next() {
		var hold, risk, size, sell, kill, live, missing, unknown int
		if err := rows.Scan(&hold, &risk, &size, &sell, &kill, &live, &missing, &unknown); err != nil {
			return nil, fmt.Errorf("postgres: count trade decisions reasons scan: %w", err)
		}
		out["hold_signal"] = hold
		out["risk_rejected"] = risk
		out["sizing_zero"] = size
		out["sell_without_position"] = sell
		out["kill_switch"] = kill
		out["live_gate_denied"] = live
		out["missing_data"] = missing
		out["unknown"] = unknown
	}
	return out, rows.Err()
}

// AttachPaperOrder links a paper order to the trade decision.
func (r *TradeDecisionJournalRepo) AttachPaperOrder(ctx context.Context, decisionID, orderID uuid.UUID) error {
	return r.attachOrder(ctx, decisionID, orderID, "paper_order_id", domain.TradeDecisionStatusPaper)
}

// AttachLiveOrder links a live order to the trade decision.
func (r *TradeDecisionJournalRepo) AttachLiveOrder(ctx context.Context, decisionID, orderID uuid.UUID) error {
	return r.attachOrder(ctx, decisionID, orderID, "live_order_id", domain.TradeDecisionStatusLive)
}

// ResolvePredictionOutcome marks a paper event-market decision closed after
// its provider outcome has been durably settled.
func (r *TradeDecisionJournalRepo) ResolvePredictionOutcome(ctx context.Context, decisionID uuid.UUID) error {
	var updatedID uuid.UUID
	err := r.pool.QueryRow(ctx, `UPDATE trade_decisions SET status = $2, updated_at = NOW()
		WHERE id = $1 AND status = $3 RETURNING id`, decisionID, domain.TradeDecisionStatusClosed, domain.TradeDecisionStatusPaper).Scan(&updatedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("postgres: resolve prediction decision %s: %w", decisionID, ErrNotFound)
		}
		return fmt.Errorf("postgres: resolve prediction decision: %w", err)
	}
	return nil
}

func (r *TradeDecisionJournalRepo) attachOrder(ctx context.Context, decisionID, orderID uuid.UUID, column string, status domain.TradeDecisionStatus) error {
	query, args := buildTradeDecisionAttachQuery(column, decisionID, orderID, status)
	var updatedID uuid.UUID
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&updatedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("postgres: attach %s order to trade decision %s: %w", column, decisionID, ErrNotFound)
		}
		return fmt.Errorf("postgres: attach %s order to trade decision: %w", column, err)
	}
	return nil
}

func (r *TradeDecisionJournalRepo) list(ctx context.Context, query string, args []any, op string) ([]domain.TradeDecision, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: %s: %w", op, err)
	}
	defer rows.Close()

	var decisions []domain.TradeDecision
	for rows.Next() {
		decision, err := scanTradeDecision(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: %s scan: %w", op, err)
		}
		decisions = append(decisions, *decision)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: %s rows: %w", op, err)
	}

	return decisions, nil
}

func scanTradeDecision(sc scanner) (*domain.TradeDecision, error) {
	var (
		decision         domain.TradeDecision
		strategyID       *uuid.UUID
		pipelineRunID    *uuid.UUID
		externalID       *string
		outcome          *string
		promptText       *string
		llmProvider      *string
		llmModel         *string
		promptTokens     *int
		completionTokens *int
		latencyMS        *int
		costUSD          *float64
		riskReasons      []string
		evidence         []byte
		features         []byte
		regimeTags       []string
		paperOrderID     *uuid.UUID
		liveOrderID      *uuid.UUID
	)

	if err := sc.Scan(
		&decision.ID,
		&strategyID,
		&pipelineRunID,
		&decision.MarketType,
		&decision.InstrumentKey,
		&externalID,
		&decision.Side,
		&outcome,
		&decision.FairValue,
		&decision.ExecutablePrice,
		&decision.Spread,
		&decision.Depth,
		&decision.GrossEV,
		&decision.NetEV,
		&decision.KellyFraction,
		&decision.ProposedSize,
		&decision.ApprovedSize,
		&decision.RiskStatus,
		&riskReasons,
		&evidence,
		&features,
		&regimeTags,
		&promptText,
		&llmProvider,
		&llmModel,
		&promptTokens,
		&completionTokens,
		&latencyMS,
		&costUSD,
		&paperOrderID,
		&liveOrderID,
		&decision.Status,
		&decision.CreatedAt,
		&decision.UpdatedAt,
	); err != nil {
		return nil, err
	}

	decision.StrategyID = strategyID
	decision.PipelineRunID = pipelineRunID
	if externalID != nil {
		decision.ExternalMarketID = *externalID
	}
	if outcome != nil {
		decision.Outcome = *outcome
	}
	decision.RiskReasons = riskReasons
	decision.Evidence = json.RawMessage(evidence)
	decision.Features = json.RawMessage(features)
	decision.RegimeTags = regimeTags
	if promptText != nil {
		decision.PromptText = *promptText
	}
	if llmProvider != nil {
		decision.LLMProvider = *llmProvider
	}
	if llmModel != nil {
		decision.LLMModel = *llmModel
	}
	decision.PromptTokens = promptTokens
	decision.CompletionTokens = completionTokens
	decision.LatencyMS = latencyMS
	decision.CostUSD = costUSD
	decision.PaperOrderID = paperOrderID
	decision.LiveOrderID = liveOrderID

	return &decision, nil
}

func buildTradeDecisionCountQuery(filter repository.TradeDecisionFilter) (string, []any) {
	query, args := buildTradeDecisionFilteredQuery("SELECT COUNT(*) FROM trade_decisions", filter, 0, 0, false)
	return query, args
}

func buildTradeDecisionListQuery(filter repository.TradeDecisionFilter, limit, offset int) (string, []any) {
	query, args := buildTradeDecisionFilteredQuery(tradeDecisionSelectSQL, filter, limit, offset, true)
	return query, args
}

func buildTradeDecisionAttachQuery(column string, decisionID, orderID uuid.UUID, status domain.TradeDecisionStatus) (string, []any) {
	query := fmt.Sprintf(`UPDATE trade_decisions SET %s = $2, status = $3, updated_at = NOW() WHERE id = $1 RETURNING id`, column)
	return query, []any{decisionID, orderID, status}
}

func buildTradeDecisionFilteredQuery(base string, filter repository.TradeDecisionFilter, limit, offset int, includePagination bool) (string, []any) {
	var (
		conditions []string
		args       []any
		argIdx     int
	)

	nextArg := func(v any) string {
		argIdx++
		args = append(args, v)
		return fmt.Sprintf("$%d", argIdx)
	}

	if filter.StrategyID != nil {
		conditions = append(conditions, "strategy_id = "+nextArg(*filter.StrategyID))
	}
	if filter.InstrumentKey != "" {
		conditions = append(conditions, "instrument_key = "+nextArg(filter.InstrumentKey))
	}
	if filter.MarketType != "" {
		conditions = append(conditions, "market_type = "+nextArg(filter.MarketType))
	}
	if filter.Status != "" {
		conditions = append(conditions, "status = "+nextArg(filter.Status))
	}
	if filter.CreatedAfter != nil {
		conditions = append(conditions, "created_at >= "+nextArg(*filter.CreatedAfter))
	}
	if filter.CreatedBefore != nil {
		conditions = append(conditions, "created_at <= "+nextArg(*filter.CreatedBefore))
	}

	query := base
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	if includePagination {
		query += " ORDER BY created_at DESC, id DESC"
		query += fmt.Sprintf(" LIMIT %s OFFSET %s", nextArg(limit), nextArg(offset))
	}

	return query, args
}

func marshalTradeDecisionJSON(data json.RawMessage) ([]byte, error) {
	if len(data) == 0 {
		return []byte("{}"), nil
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("postgres: trade decision json is not valid")
	}
	return data, nil
}

func stringSliceOrEmpty(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return values
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}
