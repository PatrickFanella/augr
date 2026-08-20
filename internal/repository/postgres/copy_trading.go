package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CopyTradingRepo struct{ pool *pgxpool.Pool }

var _ repository.CopyTradingRepository = (*CopyTradingRepo)(nil)

func NewCopyTradingRepo(pool *pgxpool.Pool) *CopyTradingRepo { return &CopyTradingRepo{pool: pool} }

func (r *CopyTradingRepo) CreateLeader(ctx context.Context, leader *domain.CopyLeader) error {
	if leader.Metadata == nil {
		leader.Metadata = json.RawMessage(`{}`)
	}
	return r.pool.QueryRow(ctx, `INSERT INTO copy_leaders (entity_type,display_name,sec_cik,identity_status,metadata) VALUES ($1,$2,NULLIF($3,''),$4,$5) RETURNING id,created_at,updated_at`, leader.EntityType, leader.DisplayName, leader.SECCIK, leader.IdentityStatus, leader.Metadata).Scan(&leader.ID, &leader.CreatedAt, &leader.UpdatedAt)
}

const copyLeaderSelect = `SELECT id,entity_type,display_name,COALESCE(sec_cik,''),identity_status,metadata,created_at,updated_at FROM copy_leaders`

func (r *CopyTradingRepo) GetLeader(ctx context.Context, id uuid.UUID) (*domain.CopyLeader, error) {
	var leader domain.CopyLeader
	if err := r.pool.QueryRow(ctx, copyLeaderSelect+` WHERE id=$1`, id).Scan(&leader.ID, &leader.EntityType, &leader.DisplayName, &leader.SECCIK, &leader.IdentityStatus, &leader.Metadata, &leader.CreatedAt, &leader.UpdatedAt); err != nil {
		return nil, copyRepoNotFound("leader", err)
	}
	return &leader, nil
}

func (r *CopyTradingRepo) ListLeaders(ctx context.Context, filter repository.CopyLeaderFilter, limit, offset int) ([]domain.CopyLeader, error) {
	where, args := copyLeaderWhere(filter)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, copyLeaderSelect+where+fmt.Sprintf(` ORDER BY display_name,id LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list copy leaders: %w", err)
	}
	defer rows.Close()
	items := make([]domain.CopyLeader, 0)
	for rows.Next() {
		var item domain.CopyLeader
		if err := rows.Scan(&item.ID, &item.EntityType, &item.DisplayName, &item.SECCIK, &item.IdentityStatus, &item.Metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan copy leader: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *CopyTradingRepo) CountLeaders(ctx context.Context, filter repository.CopyLeaderFilter) (int, error) {
	where, args := copyLeaderWhere(filter)
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM copy_leaders`+where, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("postgres: count copy leaders: %w", err)
	}
	return total, nil
}

func (r *CopyTradingRepo) UpdateLeaderIdentityStatus(ctx context.Context, id uuid.UUID, status domain.CopyIdentityStatus) error {
	tag, err := r.pool.Exec(ctx, `UPDATE copy_leaders SET identity_status=$2,updated_at=NOW() WHERE id=$1`, id, status)
	if err != nil {
		return fmt.Errorf("postgres: update copy leader identity: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("copy leader: %w", repository.ErrNotFound)
	}
	return nil
}

func copyLeaderWhere(filter repository.CopyLeaderFilter) (string, []any) {
	clauses := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if filter.EntityType != "" {
		args = append(args, filter.EntityType)
		clauses = append(clauses, fmt.Sprintf("entity_type=$%d", len(args)))
	}
	if strings.TrimSpace(filter.Query) != "" {
		args = append(args, "%"+strings.TrimSpace(filter.Query)+"%")
		clauses = append(clauses, fmt.Sprintf("(display_name ILIKE $%d OR COALESCE(sec_cik,'') ILIKE $%d)", len(args), len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func (r *CopyTradingRepo) CreateSource(ctx context.Context, source *domain.CopyLeaderSource) error {
	if source.Metadata == nil {
		source.Metadata = json.RawMessage(`{}`)
	}
	if source.Checkpoint == nil {
		source.Checkpoint = json.RawMessage(`{}`)
	}
	return r.pool.QueryRow(ctx, `INSERT INTO copy_leader_sources (leader_id,provider,source_type,external_key,status,metadata,checkpoint) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id,created_at,updated_at`, source.LeaderID, source.Provider, source.SourceType, source.ExternalKey, source.Status, source.Metadata, source.Checkpoint).Scan(&source.ID, &source.CreatedAt, &source.UpdatedAt)
}

const copySourceSelect = `SELECT id,leader_id,provider,source_type,external_key,status,metadata,checkpoint,last_observed_at,created_at,updated_at FROM copy_leader_sources`

func scanCopySource(row pgx.Row) (*domain.CopyLeaderSource, error) {
	var source domain.CopyLeaderSource
	if err := row.Scan(&source.ID, &source.LeaderID, &source.Provider, &source.SourceType, &source.ExternalKey, &source.Status, &source.Metadata, &source.Checkpoint, &source.LastObservedAt, &source.CreatedAt, &source.UpdatedAt); err != nil {
		return nil, err
	}
	return &source, nil
}

func (r *CopyTradingRepo) GetSource(ctx context.Context, id uuid.UUID) (*domain.CopyLeaderSource, error) {
	source, err := scanCopySource(r.pool.QueryRow(ctx, copySourceSelect+` WHERE id=$1`, id))
	if err != nil {
		return nil, copyRepoNotFound("source", err)
	}
	return source, nil
}

func (r *CopyTradingRepo) ListSourcesByLeader(ctx context.Context, leaderID uuid.UUID) ([]domain.CopyLeaderSource, error) {
	rows, err := r.pool.Query(ctx, copySourceSelect+` WHERE leader_id=$1 ORDER BY created_at,id`, leaderID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list copy sources: %w", err)
	}
	defer rows.Close()
	items := make([]domain.CopyLeaderSource, 0)
	for rows.Next() {
		var source domain.CopyLeaderSource
		if err := rows.Scan(&source.ID, &source.LeaderID, &source.Provider, &source.SourceType, &source.ExternalKey, &source.Status, &source.Metadata, &source.Checkpoint, &source.LastObservedAt, &source.CreatedAt, &source.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, source)
	}
	return items, rows.Err()
}

func (r *CopyTradingRepo) UpdateSourceObserved(ctx context.Context, id uuid.UUID, observedAt time.Time, checkpoint json.RawMessage) error {
	if checkpoint == nil {
		checkpoint = json.RawMessage(`{}`)
	}
	tag, err := r.pool.Exec(ctx, `UPDATE copy_leader_sources SET last_observed_at=$2,checkpoint=$3,updated_at=NOW() WHERE id=$1`, id, observedAt, checkpoint)
	if err != nil {
		return fmt.Errorf("postgres: update copy source: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("copy source: %w", repository.ErrNotFound)
	}
	return nil
}

func (r *CopyTradingRepo) Save13FSnapshot(ctx context.Context, observation *domain.CopySourceObservation, snapshot *domain.CopyPortfolioSnapshot) (bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if observation.NormalizedPayload == nil {
		observation.NormalizedPayload = json.RawMessage(`{}`)
	}
	row := tx.QueryRow(ctx, `INSERT INTO copy_source_observations (source_id,provider_observation_id,observation_kind,schema_version,effective_at,published_at,observed_at,amendment_number,supersedes_id,status,content_hash,normalized_payload,source_url) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT (source_id,provider_observation_id,content_hash) DO NOTHING RETURNING id,created_at`, observation.SourceID, observation.ProviderObservationID, observation.ObservationKind, observation.SchemaVersion, observation.EffectiveAt, observation.PublishedAt, observation.ObservedAt, observation.AmendmentNumber, observation.SupersedesID, observation.Status, observation.ContentHash, observation.NormalizedPayload, observation.SourceURL)
	if err := row.Scan(&observation.ID, &observation.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("postgres: create copy observation: %w", err)
	}
	if observation.SupersedesID != nil {
		if _, err := tx.Exec(ctx, `UPDATE copy_source_observations SET status='superseded' WHERE id=$1 AND source_id=$2`, *observation.SupersedesID, observation.SourceID); err != nil {
			return false, fmt.Errorf("postgres: supersede copy observation: %w", err)
		}
	}
	if err := tx.QueryRow(ctx, `INSERT INTO copy_portfolio_snapshots (observation_id,report_period,total_disclosed_value,holding_count) VALUES ($1,$2,$3,$4) RETURNING id,created_at`, observation.ID, snapshot.ReportPeriod, snapshot.TotalDisclosedValue, snapshot.HoldingCount).Scan(&snapshot.ID, &snapshot.CreatedAt); err != nil {
		return false, fmt.Errorf("postgres: create copy snapshot: %w", err)
	}
	for i := range snapshot.Holdings {
		h := &snapshot.Holdings[i]
		h.SnapshotID = snapshot.ID
		if err := tx.QueryRow(ctx, `INSERT INTO copy_portfolio_holdings (snapshot_id,issuer_name,title_of_class,cusip,figi,disclosed_value,shares_or_principal,amount_type,put_call,investment_discretion,voting_sole,voting_shared,voting_none) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id,created_at`, h.SnapshotID, h.IssuerName, h.TitleOfClass, h.CUSIP, h.FIGI, h.DisclosedValue, h.SharesOrPrincipal, h.AmountType, h.PutCall, h.InvestmentDiscretion, h.VotingSole, h.VotingShared, h.VotingNone).Scan(&h.ID, &h.CreatedAt); err != nil {
			return false, fmt.Errorf("postgres: create copy holding: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

const copyObservationSelect = `SELECT id,source_id,provider_observation_id,observation_kind,schema_version,effective_at,published_at,observed_at,amendment_number,supersedes_id,status,content_hash,normalized_payload,source_url,created_at FROM copy_source_observations`

func scanCopyObservation(row pgx.Row) (*domain.CopySourceObservation, error) {
	var item domain.CopySourceObservation
	if err := row.Scan(&item.ID, &item.SourceID, &item.ProviderObservationID, &item.ObservationKind, &item.SchemaVersion, &item.EffectiveAt, &item.PublishedAt, &item.ObservedAt, &item.AmendmentNumber, &item.SupersedesID, &item.Status, &item.ContentHash, &item.NormalizedPayload, &item.SourceURL, &item.CreatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *CopyTradingRepo) GetObservation(ctx context.Context, id uuid.UUID) (*domain.CopySourceObservation, error) {
	item, err := scanCopyObservation(r.pool.QueryRow(ctx, copyObservationSelect+` WHERE id=$1`, id))
	if err != nil {
		return nil, copyRepoNotFound("observation", err)
	}
	return item, nil
}

func (r *CopyTradingRepo) GetLatest13FSnapshot(ctx context.Context, sourceID uuid.UUID) (*domain.CopySourceObservation, *domain.CopyPortfolioSnapshot, error) {
	observation, err := scanCopyObservation(r.pool.QueryRow(ctx, copyObservationSelect+` WHERE source_id=$1 AND observation_kind='portfolio_snapshot' AND status='active' ORDER BY effective_at DESC,published_at DESC,created_at DESC LIMIT 1`, sourceID))
	if err != nil {
		return nil, nil, copyRepoNotFound("snapshot observation", err)
	}
	var snapshot domain.CopyPortfolioSnapshot
	if err := r.pool.QueryRow(ctx, `SELECT id,observation_id,report_period,total_disclosed_value::double precision,holding_count,created_at FROM copy_portfolio_snapshots WHERE observation_id=$1`, observation.ID).Scan(&snapshot.ID, &snapshot.ObservationID, &snapshot.ReportPeriod, &snapshot.TotalDisclosedValue, &snapshot.HoldingCount, &snapshot.CreatedAt); err != nil {
		return nil, nil, copyRepoNotFound("snapshot", err)
	}
	rows, err := r.pool.Query(ctx, `SELECT id,snapshot_id,issuer_name,title_of_class,cusip,figi,disclosed_value::double precision,shares_or_principal::double precision,amount_type,put_call,investment_discretion,voting_sole::double precision,voting_shared::double precision,voting_none::double precision,created_at FROM copy_portfolio_holdings WHERE snapshot_id=$1 ORDER BY disclosed_value DESC,id`, snapshot.ID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var h domain.CopyPortfolioHolding
		if err := rows.Scan(&h.ID, &h.SnapshotID, &h.IssuerName, &h.TitleOfClass, &h.CUSIP, &h.FIGI, &h.DisclosedValue, &h.SharesOrPrincipal, &h.AmountType, &h.PutCall, &h.InvestmentDiscretion, &h.VotingSole, &h.VotingShared, &h.VotingNone, &h.CreatedAt); err != nil {
			return nil, nil, err
		}
		snapshot.Holdings = append(snapshot.Holdings, h)
	}
	return observation, &snapshot, rows.Err()
}

func (r *CopyTradingRepo) UpsertInstrumentMapping(ctx context.Context, mapping *domain.CopyInstrumentMapping) error {
	return r.pool.QueryRow(ctx, `INSERT INTO copy_instrument_mappings (provider,identifier_type,identifier_value,instrument_key,ticker,confidence,mapping_method) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (provider,identifier_type,identifier_value) WHERE valid_to IS NULL DO UPDATE SET instrument_key=EXCLUDED.instrument_key,ticker=EXCLUDED.ticker,confidence=EXCLUDED.confidence,mapping_method=EXCLUDED.mapping_method,updated_at=NOW() RETURNING id,valid_from,valid_to,created_at,updated_at`, mapping.Provider, mapping.IdentifierType, mapping.IdentifierValue, mapping.InstrumentKey, mapping.Ticker, mapping.Confidence, mapping.MappingMethod).Scan(&mapping.ID, &mapping.ValidFrom, &mapping.ValidTo, &mapping.CreatedAt, &mapping.UpdatedAt)
}

func (r *CopyTradingRepo) ListInstrumentMappings(ctx context.Context, provider, identifierType string, identifierValues []string) ([]domain.CopyInstrumentMapping, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,provider,identifier_type,identifier_value,instrument_key,ticker,confidence,mapping_method,valid_from,valid_to,created_at,updated_at FROM copy_instrument_mappings WHERE provider=$1 AND identifier_type=$2 AND valid_to IS NULL AND identifier_value=ANY($3) ORDER BY identifier_value`, provider, identifierType, identifierValues)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.CopyInstrumentMapping, 0)
	for rows.Next() {
		var item domain.CopyInstrumentMapping
		if err := rows.Scan(&item.ID, &item.Provider, &item.IdentifierType, &item.IdentifierValue, &item.InstrumentKey, &item.Ticker, &item.Confidence, &item.MappingMethod, &item.ValidFrom, &item.ValidTo, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const copySubscriptionSelect = `SELECT id,leader_id,source_id,legacy_strategy_id,origin_type,origin_id,status,is_paper,method,capital_budget::double precision,cash_buffer_pct::double precision,top_n,min_source_weight::double precision,max_position_weight::double precision,max_turnover_pct::double precision,min_price::double precision,min_avg_dollar_volume::double precision,max_spread_bps,stock_allowlist,stock_blocklist,created_by,created_at,updated_at,stopped_at FROM copy_subscriptions`

func scanCopySubscription(row pgx.Row) (*domain.CopySubscription, error) {
	var item domain.CopySubscription
	if err := row.Scan(&item.ID, &item.LeaderID, &item.SourceID, &item.LegacyStrategyID, &item.OriginType, &item.OriginID, &item.Status, &item.IsPaper, &item.Method, &item.CapitalBudget, &item.CashBufferPct, &item.TopN, &item.MinSourceWeight, &item.MaxPositionWeight, &item.MaxTurnoverPct, &item.MinPrice, &item.MinAvgDollarVolume, &item.MaxSpreadBPS, &item.StockAllowlist, &item.StockBlocklist, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt, &item.StoppedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *CopyTradingRepo) CreateSubscription(ctx context.Context, s *domain.CopySubscription) error {
	return r.pool.QueryRow(ctx, `INSERT INTO copy_subscriptions (id,leader_id,source_id,legacy_strategy_id,origin_type,origin_id,status,is_paper,method,capital_budget,cash_buffer_pct,top_n,min_source_weight,max_position_weight,max_turnover_pct,min_price,min_avg_dollar_volume,max_spread_bps,stock_allowlist,stock_blocklist,created_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21) RETURNING created_at,updated_at`, s.ID, s.LeaderID, s.SourceID, s.LegacyStrategyID, s.OriginType, s.OriginID, s.Status, s.IsPaper, s.Method, s.CapitalBudget, s.CashBufferPct, s.TopN, s.MinSourceWeight, s.MaxPositionWeight, s.MaxTurnoverPct, s.MinPrice, s.MinAvgDollarVolume, s.MaxSpreadBPS, s.StockAllowlist, s.StockBlocklist, s.CreatedBy).Scan(&s.CreatedAt, &s.UpdatedAt)
}

func (r *CopyTradingRepo) GetSubscription(ctx context.Context, id uuid.UUID) (*domain.CopySubscription, error) {
	item, err := scanCopySubscription(r.pool.QueryRow(ctx, copySubscriptionSelect+` WHERE id=$1`, id))
	if err != nil {
		return nil, copyRepoNotFound("subscription", err)
	}
	return item, nil
}

func copySubscriptionWhere(filter repository.CopySubscriptionFilter) (string, []any) {
	clauses := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if filter.LeaderID != nil {
		args = append(args, *filter.LeaderID)
		clauses = append(clauses, fmt.Sprintf("leader_id=$%d", len(args)))
	}
	if filter.SourceID != nil {
		args = append(args, *filter.SourceID)
		clauses = append(clauses, fmt.Sprintf("source_id=$%d", len(args)))
	}
	if filter.Status != "" {
		args = append(args, filter.Status)
		clauses = append(clauses, fmt.Sprintf("status=$%d", len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func (r *CopyTradingRepo) ListSubscriptions(ctx context.Context, filter repository.CopySubscriptionFilter, limit, offset int) ([]domain.CopySubscription, error) {
	where, args := copySubscriptionWhere(filter)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, copySubscriptionSelect+where+fmt.Sprintf(` ORDER BY created_at DESC,id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.CopySubscription, 0)
	for rows.Next() {
		item, err := scanCopySubscription(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *CopyTradingRepo) CountSubscriptions(ctx context.Context, filter repository.CopySubscriptionFilter) (int, error) {
	where, args := copySubscriptionWhere(filter)
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM copy_subscriptions`+where, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (r *CopyTradingRepo) UpdateSubscription(ctx context.Context, s *domain.CopySubscription) error {
	return r.pool.QueryRow(ctx, `UPDATE copy_subscriptions SET status=$2,method=$3,capital_budget=$4,cash_buffer_pct=$5,top_n=$6,min_source_weight=$7,max_position_weight=$8,max_turnover_pct=$9,min_price=$10,min_avg_dollar_volume=$11,max_spread_bps=$12,stock_allowlist=$13,stock_blocklist=$14,stopped_at=$15,updated_at=NOW() WHERE id=$1 RETURNING updated_at`, s.ID, s.Status, s.Method, s.CapitalBudget, s.CashBufferPct, s.TopN, s.MinSourceWeight, s.MaxPositionWeight, s.MaxTurnoverPct, s.MinPrice, s.MinAvgDollarVolume, s.MaxSpreadBPS, s.StockAllowlist, s.StockBlocklist, s.StoppedAt).Scan(&s.UpdatedAt)
}

const copyIntentSelect = `SELECT id,subscription_id,origin_type,origin_id,source_observation_id,pipeline_run_id,instrument_key,ticker,side,target_weight::double precision,target_value::double precision,attributed_current_value::double precision,requested_notional::double precision,executable_price::double precision,calculation_version,calculation,policy_status,policy_reasons,risk_status,risk_reasons,order_id,status,created_at,updated_at FROM copy_trade_intents`

func scanCopyIntent(row pgx.Row) (*domain.CopyTradeIntent, error) {
	var item domain.CopyTradeIntent
	if err := row.Scan(&item.ID, &item.SubscriptionID, &item.OriginType, &item.OriginID, &item.SourceObservationID, &item.PipelineRunID, &item.InstrumentKey, &item.Ticker, &item.Side, &item.TargetWeight, &item.TargetValue, &item.AttributedCurrentValue, &item.RequestedNotional, &item.ExecutablePrice, &item.CalculationVersion, &item.Calculation, &item.PolicyStatus, &item.PolicyReasons, &item.RiskStatus, &item.RiskReasons, &item.OrderID, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *CopyTradingRepo) CreateIntent(ctx context.Context, intent *domain.CopyTradeIntent) (bool, error) {
	if intent.Calculation == nil {
		intent.Calculation = json.RawMessage(`{}`)
	}
	if intent.PolicyReasons == nil {
		intent.PolicyReasons = []string{}
	}
	if intent.RiskReasons == nil {
		intent.RiskReasons = []string{}
	}
	err := r.pool.QueryRow(ctx, `INSERT INTO copy_trade_intents (id,subscription_id,origin_type,origin_id,source_observation_id,pipeline_run_id,instrument_key,ticker,side,target_weight,target_value,attributed_current_value,requested_notional,executable_price,calculation_version,calculation,policy_status,policy_reasons,risk_status,risk_reasons,order_id,status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22) ON CONFLICT (subscription_id,source_observation_id,instrument_key,calculation_version) DO NOTHING RETURNING created_at,updated_at`, intent.ID, intent.SubscriptionID, intent.OriginType, intent.OriginID, intent.SourceObservationID, intent.PipelineRunID, intent.InstrumentKey, intent.Ticker, intent.Side, intent.TargetWeight, intent.TargetValue, intent.AttributedCurrentValue, intent.RequestedNotional, intent.ExecutablePrice, intent.CalculationVersion, intent.Calculation, intent.PolicyStatus, intent.PolicyReasons, intent.RiskStatus, intent.RiskReasons, intent.OrderID, intent.Status).Scan(&intent.CreatedAt, &intent.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *CopyTradingRepo) ListIntents(ctx context.Context, subscriptionID uuid.UUID, limit, offset int) ([]domain.CopyTradeIntent, error) {
	rows, err := r.pool.Query(ctx, copyIntentSelect+` WHERE subscription_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, subscriptionID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.CopyTradeIntent, 0)
	for rows.Next() {
		item, err := scanCopyIntent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *CopyTradingRepo) UpdateIntent(ctx context.Context, intent *domain.CopyTradeIntent) error {
	return r.pool.QueryRow(ctx, `UPDATE copy_trade_intents SET pipeline_run_id=$2,executable_price=$3,calculation=$4,policy_status=$5,policy_reasons=$6,risk_status=$7,risk_reasons=$8,order_id=$9,status=$10,updated_at=NOW() WHERE id=$1 RETURNING updated_at`, intent.ID, intent.PipelineRunID, intent.ExecutablePrice, intent.Calculation, intent.PolicyStatus, intent.PolicyReasons, intent.RiskStatus, intent.RiskReasons, intent.OrderID, intent.Status).Scan(&intent.UpdatedAt)
}

func copyRepoNotFound(entity string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("copy %s: %w", entity, repository.ErrNotFound)
	}
	return fmt.Errorf("postgres: get copy %s: %w", entity, err)
}
