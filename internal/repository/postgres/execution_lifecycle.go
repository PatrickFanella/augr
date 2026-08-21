package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// ExecutionLifecycleRepo persists the common append-only execution graph. It
// does not submit orders or activate any provider.
type ExecutionLifecycleRepo struct{ pool *pgxpool.Pool }

var _ repository.ExecutionLifecycleRepository = (*ExecutionLifecycleRepo)(nil)

// NewExecutionLifecycleRepo returns a common lifecycle repository backed by
// pool.
func NewExecutionLifecycleRepo(pool *pgxpool.Pool) *ExecutionLifecycleRepo {
	return &ExecutionLifecycleRepo{pool: pool}
}

// ProposeExecutionIntent atomically inserts an immutable intent and its one
// required proposed event. Exact retries converge on the existing aggregate.
func (repo *ExecutionLifecycleRepo) ProposeExecutionIntent(
	ctx context.Context,
	aggregate *lifecycle.Aggregate,
) (*lifecycle.Aggregate, error) {
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: propose execution intent: repository pool is required")
	}
	if aggregate == nil || aggregate.State != lifecycle.StateProposed || len(aggregate.Events) != 1 ||
		aggregate.Events[0].Kind != lifecycle.EventIntentProposed || aggregate.Order != nil ||
		aggregate.Binding != nil || len(aggregate.Fills) != 0 {
		return nil, fmt.Errorf("postgres: propose execution intent: one proposed lifecycle is required")
	}
	if _, err := lifecycle.Replay(aggregate.Intent.AccountID, aggregate.Intent, []lifecycle.Transition{{Event: aggregate.Events[0]}}); err != nil {
		return nil, fmt.Errorf("postgres: propose execution intent: %w", err)
	}

	databaseTransaction, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("postgres: begin execution proposal: %w", err)
	}
	defer func() { _ = databaseTransaction.Rollback(ctx) }()

	var persistedID uuid.UUID
	err = databaseTransaction.QueryRow(ctx, `INSERT INTO execution_intents (
		id, account_id, environment, instrument_id, idempotency_key,
		desired_quantity_delta, decision_quote_snapshot_id, decision_at,
		origin_type, origin_id, strategy_version_id, metadata, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	ON CONFLICT (account_id, idempotency_key) DO NOTHING
	RETURNING id`,
		aggregate.Intent.ID,
		aggregate.Intent.AccountID,
		aggregate.Intent.Environment,
		aggregate.Intent.InstrumentID,
		aggregate.Intent.IdempotencyKey,
		aggregate.Intent.DesiredQuantityDelta.String(),
		aggregate.Intent.DecisionQuoteSnapshotID,
		aggregate.Intent.DecisionAt,
		aggregate.Intent.OriginType,
		aggregate.Intent.OriginID,
		aggregate.Intent.StrategyVersionID,
		jsonForStorage(aggregate.Intent.Metadata),
		aggregate.Intent.CreatedAt,
	).Scan(&persistedID)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = databaseTransaction.Rollback(ctx)
		existing, loadErr := repo.FindExecutionLifecycleByIdempotencyKey(
			ctx,
			aggregate.Intent.AccountID,
			aggregate.Intent.IdempotencyKey,
		)
		if loadErr != nil {
			return nil, fmt.Errorf("postgres: load replayed execution proposal: %w", loadErr)
		}
		if !lifecycle.SameIntentPayload(&existing.Intent, &aggregate.Intent) ||
			!lifecycle.SameEventPayload(&existing.Events[0], &aggregate.Events[0]) {
			return nil, fmt.Errorf("postgres: execution intent key reused with mismatched payload: %w", repository.ErrIdempotencyConflict)
		}
		return existing, nil
	}
	if err != nil {
		return nil, executionLifecycleWriteError("insert execution intent", err)
	}
	if persistedID != aggregate.Intent.ID {
		return nil, fmt.Errorf("postgres: execution intent insert returned %s, want %s", persistedID, aggregate.Intent.ID)
	}
	if err := insertExecutionLifecycleEvent(ctx, databaseTransaction, &aggregate.Events[0]); err != nil {
		return nil, executionLifecycleWriteError("insert execution proposal event", err)
	}
	if err := databaseTransaction.Commit(ctx); err != nil {
		return nil, executionLifecycleWriteError("commit execution proposal", err)
	}
	return repo.GetExecutionLifecycle(ctx, aggregate.Intent.AccountID, aggregate.Intent.ID)
}

// ApplyExecutionTransition appends one non-fill transition after reloading and
// row-locking the latest aggregate. A writer that observed an older stream
// retries its read after the lock rather than applying stale state.
func (repo *ExecutionLifecycleRepo) ApplyExecutionTransition(
	ctx context.Context,
	accountID uuid.UUID,
	transition *lifecycle.Transition,
) (*lifecycle.Aggregate, error) {
	if transition == nil {
		return nil, fmt.Errorf("postgres: apply execution transition: transition is required")
	}
	if transition.Event.Kind == lifecycle.EventFillAcknowledged || transition.Event.Kind == lifecycle.EventFillRecorded ||
		transition.Fill != nil || transition.Normalization != nil {
		return nil, fmt.Errorf("postgres: apply execution transition: fill transitions require ApplyExecutionFill")
	}
	return repo.applyExecutionTransition(ctx, accountID, transition, false)
}

// ApplyExecutionFill atomically appends the normalization, ledger postings,
// optional first binding, fill, and lifecycle event under one intent lock.
func (repo *ExecutionLifecycleRepo) ApplyExecutionFill(
	ctx context.Context,
	accountID uuid.UUID,
	transition *lifecycle.Transition,
) (*lifecycle.Aggregate, error) {
	if transition == nil || transition.Fill == nil || transition.Normalization == nil ||
		(transition.Event.Kind != lifecycle.EventFillAcknowledged && transition.Event.Kind != lifecycle.EventFillRecorded) {
		return nil, fmt.Errorf("postgres: apply execution fill: complete fill transition is required")
	}
	return repo.applyExecutionTransition(ctx, accountID, transition, true)
}

func (repo *ExecutionLifecycleRepo) applyExecutionTransition(
	ctx context.Context,
	accountID uuid.UUID,
	transition *lifecycle.Transition,
	fill bool,
) (*lifecycle.Aggregate, error) {
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: apply execution transition: repository pool is required")
	}
	if accountID == uuid.Nil || transition.Event.IntentID == uuid.Nil || transition.Event.AccountID != accountID {
		return nil, fmt.Errorf("postgres: apply execution transition: account and intent identity are required")
	}
	for attempt := 0; attempt < 16; attempt++ {
		aggregate, err := repo.GetExecutionLifecycle(ctx, accountID, transition.Event.IntentID)
		if err != nil {
			return nil, err
		}
		if existing := findLifecycleEvent(aggregate.Events, transition.Event.ID); existing != nil {
			matches, matchErr := repo.transitionReplayMatches(ctx, aggregate, transition, existing)
			if matchErr != nil {
				return nil, matchErr
			}
			if !matches {
				return nil, fmt.Errorf("postgres: execution transition identity reused with mismatched payload: %w", repository.ErrIdempotencyConflict)
			}
			return aggregate, nil
		}

		databaseTransaction, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return nil, fmt.Errorf("postgres: begin execution transition: %w", err)
		}
		locked := false
		func() {
			defer func() {
				if !locked {
					_ = databaseTransaction.Rollback(ctx)
				}
			}()
			var lockedAccountID uuid.UUID
			if err = databaseTransaction.QueryRow(ctx, `SELECT account_id FROM execution_intents WHERE id = $1 FOR UPDATE`, transition.Event.IntentID).Scan(&lockedAccountID); err != nil {
				return
			}
			if lockedAccountID != accountID {
				err = repository.ErrNotFound
				return
			}
			var latestEventID uuid.UUID
			if err = databaseTransaction.QueryRow(ctx, `SELECT id FROM execution_lifecycle_events
				WHERE intent_id = $1 ORDER BY ingest_sequence DESC LIMIT 1`, transition.Event.IntentID).Scan(&latestEventID); err != nil {
				return
			}
			if len(aggregate.Events) == 0 || aggregate.Events[len(aggregate.Events)-1].ID != latestEventID {
				err = errExecutionLifecycleReload
				return
			}
			if _, err = lifecycle.ApplyTransition(aggregate, transition); err != nil {
				err = fmt.Errorf("postgres: validate execution transition: %w", err)
				return
			}
			if fill {
				err = repo.insertExecutionFillTransition(ctx, databaseTransaction, transition)
			} else {
				err = insertExecutionNonFillTransition(ctx, databaseTransaction, transition)
			}
			if err != nil {
				return
			}
			if err = databaseTransaction.Commit(ctx); err != nil {
				return
			}
			locked = true
		}()
		if errors.Is(err, errExecutionLifecycleReload) {
			continue
		}
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return nil, err
			}
			return nil, executionLifecycleWriteError("apply execution transition", err)
		}
		return repo.GetExecutionLifecycle(ctx, accountID, transition.Event.IntentID)
	}
	return nil, fmt.Errorf("postgres: execution transition did not converge after serialized reloads")
}

var errExecutionLifecycleReload = errors.New("execution lifecycle changed before lock")

func insertExecutionNonFillTransition(ctx context.Context, tx pgx.Tx, transition *lifecycle.Transition) error {
	switch transition.Event.Kind {
	case lifecycle.EventOrderRouted:
		if transition.Order == nil {
			return fmt.Errorf("routed transition has no order")
		}
		if err := insertExecutionOrder(ctx, tx, transition.Order); err != nil {
			return err
		}
	case lifecycle.EventOrderWorking:
		if transition.Binding == nil {
			return fmt.Errorf("working transition has no binding")
		}
		if err := insertExecutionBinding(ctx, tx, transition.Binding); err != nil {
			return err
		}
	}
	return insertExecutionLifecycleEvent(ctx, tx, &transition.Event)
}

func (repo *ExecutionLifecycleRepo) insertExecutionFillTransition(
	ctx context.Context,
	tx pgx.Tx,
	transition *lifecycle.Transition,
) error {
	if _, err := insertEconomicNormalizationAggregate(ctx, tx, transition.Normalization); err != nil {
		return err
	}
	if transition.Binding != nil {
		if err := insertExecutionBinding(ctx, tx, transition.Binding); err != nil {
			return err
		}
	}
	if err := insertExecutionFill(ctx, tx, transition.Fill); err != nil {
		return err
	}
	return insertExecutionLifecycleEvent(ctx, tx, &transition.Event)
}

// GetExecutionLifecycle reloads and replays the entire immutable graph.
func (repo *ExecutionLifecycleRepo) GetExecutionLifecycle(
	ctx context.Context,
	accountID, intentID uuid.UUID,
) (*lifecycle.Aggregate, error) {
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: get execution lifecycle: repository pool is required")
	}
	readTransaction, err := repo.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: begin execution lifecycle read: %w", err)
	}
	defer func() { _ = readTransaction.Rollback(ctx) }()

	intent, err := scanExecutionIntent(readTransaction.QueryRow(ctx, executionIntentSelectSQL+`
		WHERE account_id = $1 AND id = $2`, accountID, intentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get execution intent %s: %w", intentID, err)
	}

	order, err := loadExecutionOrder(ctx, readTransaction, intentID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	binding, err := loadExecutionBinding(ctx, readTransaction, order)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	fills, err := loadExecutionFills(ctx, readTransaction, intentID)
	if err != nil {
		return nil, err
	}
	events, err := loadExecutionEvents(ctx, readTransaction, intentID)
	if err != nil {
		return nil, err
	}
	if err := readTransaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres: commit execution lifecycle read: %w", err)
	}

	fillByID := make(map[uuid.UUID]lifecycle.Fill, len(fills))
	for _, fill := range fills {
		fillByID[fill.ID] = fill
	}
	transitions := make([]lifecycle.Transition, 0, len(events))
	for _, event := range events {
		transition := lifecycle.Transition{Event: event}
		switch event.Kind {
		case lifecycle.EventOrderRouted:
			transition.Order = order
		case lifecycle.EventOrderWorking:
			transition.Binding = binding
		case lifecycle.EventFillAcknowledged, lifecycle.EventFillRecorded:
			if event.FillID == nil {
				return nil, fmt.Errorf("postgres: loaded fill event %s has no fill ID", event.ID)
			}
			fillValue, ok := fillByID[*event.FillID]
			if !ok {
				return nil, fmt.Errorf("postgres: loaded fill event %s has no fill row", event.ID)
			}
			transition.Fill = &fillValue
			if event.Kind == lifecycle.EventFillAcknowledged {
				transition.Binding = binding
			}
			transition.Normalization, err = NewLedgerRepo(repo.pool).getEconomicNormalizationByID(ctx, fillValue.NormalizationID)
			if err != nil {
				return nil, fmt.Errorf("postgres: load execution fill normalization %s: %w", fillValue.NormalizationID, err)
			}
		}
		transitions = append(transitions, transition)
	}
	aggregate, err := lifecycle.Replay(accountID, *intent, transitions)
	if err != nil {
		return nil, fmt.Errorf("postgres: replay execution lifecycle %s: %w", intentID, err)
	}
	return aggregate, nil
}

// FindExecutionLifecycleByIdempotencyKey resolves the durable account-scoped
// intent identity before loading the full aggregate.
func (repo *ExecutionLifecycleRepo) FindExecutionLifecycleByIdempotencyKey(
	ctx context.Context,
	accountID uuid.UUID,
	idempotencyKey string,
) (*lifecycle.Aggregate, error) {
	var intentID uuid.UUID
	if err := repo.pool.QueryRow(ctx, `SELECT id FROM execution_intents
		WHERE account_id = $1 AND idempotency_key = $2`, accountID, strings.TrimSpace(idempotencyKey)).Scan(&intentID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: find execution intent by idempotency key: %w", err)
	}
	return repo.GetExecutionLifecycle(ctx, accountID, intentID)
}

// ListExecutionRecoveryCandidates returns only lifecycles whose latest state
// is routed, working, or partially filled. It never creates a replacement.
func (repo *ExecutionLifecycleRepo) ListExecutionRecoveryCandidates(
	ctx context.Context,
	accountID uuid.UUID,
	limit int,
) ([]*lifecycle.Aggregate, error) {
	if limit <= 0 || limit > 1000 {
		return nil, fmt.Errorf("postgres: list execution recovery candidates: limit must be between 1 and 1000")
	}
	rows, err := repo.pool.Query(ctx, `SELECT intent_id FROM (
		SELECT DISTINCT ON (intent_id) intent_id, next_state, ingest_sequence
		FROM execution_lifecycle_events
		WHERE account_id = $1
		ORDER BY intent_id, ingest_sequence DESC
	) AS latest
	WHERE next_state IN ('routed', 'working', 'partially_filled')
	ORDER BY ingest_sequence, intent_id
	LIMIT $2`, accountID, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: list execution recovery candidates: %w", err)
	}
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	result := make([]*lifecycle.Aggregate, 0, len(ids))
	for _, id := range ids {
		aggregate, err := repo.GetExecutionLifecycle(ctx, accountID, id)
		if err != nil {
			return nil, err
		}
		result = append(result, aggregate)
	}
	return result, nil
}

func (repo *ExecutionLifecycleRepo) transitionReplayMatches(
	ctx context.Context,
	aggregate *lifecycle.Aggregate,
	transition *lifecycle.Transition,
	existing *lifecycle.Event,
) (bool, error) {
	if !lifecycle.SameEventPayload(existing, &transition.Event) {
		return false, nil
	}
	switch existing.Kind {
	case lifecycle.EventOrderRouted:
		return lifecycle.SameOrderPayload(aggregate.Order, transition.Order), nil
	case lifecycle.EventOrderWorking:
		return sameExecutionBindingPayload(aggregate.Binding, transition.Binding), nil
	case lifecycle.EventFillAcknowledged, lifecycle.EventFillRecorded:
		if existing.FillID == nil || transition.Fill == nil {
			return false, nil
		}
		var persisted *lifecycle.Fill
		for index := range aggregate.Fills {
			if aggregate.Fills[index].ID == *existing.FillID {
				persisted = &aggregate.Fills[index]
				break
			}
		}
		if !lifecycle.SameFillPayload(persisted, transition.Fill) {
			return false, nil
		}
		if existing.Kind == lifecycle.EventFillAcknowledged && !sameExecutionBindingPayload(aggregate.Binding, transition.Binding) {
			return false, nil
		}
		if transition.Normalization == nil {
			return false, nil
		}
		normalization, err := NewLedgerRepo(repo.pool).getEconomicNormalizationByID(ctx, persisted.NormalizationID)
		if err != nil {
			return false, err
		}
		return ledger.SameEconomicNormalizationPayload(normalization, transition.Normalization), nil
	default:
		return transition.Order == nil && transition.Binding == nil && transition.Fill == nil && transition.Normalization == nil, nil
	}
}

func insertExecutionOrder(ctx context.Context, tx pgx.Tx, order *lifecycle.Order) error {
	if order == nil {
		return fmt.Errorf("execution order is required")
	}
	_, err := tx.Exec(ctx, `INSERT INTO execution_orders (
		id, intent_id, account_id, instrument_id, idempotency_key,
		client_order_id, side, order_type, time_in_force, quantity,
		limit_price, stop_price, venue, venue_contract_id,
		route_quote_snapshot_id, routed_at, policy_kind, policy_version, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		order.ID, order.IntentID, order.AccountID, order.InstrumentID, order.IdempotencyKey,
		order.ClientOrderID, order.Side, order.OrderType, order.TimeInForce, order.Quantity.String(),
		nullableExecutionDecimal(order.LimitPrice), nullableExecutionDecimal(order.StopPrice), order.Venue,
		order.VenueContractID, order.RouteQuoteSnapshotID, order.RoutedAt, order.PolicyKind,
		order.PolicyVersion, order.CreatedAt,
	)
	return err
}

func insertExecutionBinding(ctx context.Context, tx pgx.Tx, binding *lifecycle.OrderBinding) error {
	if binding == nil {
		return fmt.Errorf("execution binding is required")
	}
	_, err := tx.Exec(ctx, `INSERT INTO execution_order_bindings (
		id, order_id, account_id, venue, external_order_id, created_at
	) VALUES ($1,$2,$3,$4,$5,$6)`,
		binding.ID, binding.OrderID, binding.AccountID, binding.Venue, binding.ExternalOrderID, binding.CreatedAt,
	)
	return err
}

func insertExecutionFill(ctx context.Context, tx pgx.Tx, fill *lifecycle.Fill) error {
	if fill == nil {
		return fmt.Errorf("execution fill is required")
	}
	_, err := tx.Exec(ctx, `INSERT INTO execution_fills (
		id, intent_id, order_id, account_id, instrument_id, venue_contract_id,
		economic_source_event_id, normalization_id, ledger_transaction_id,
		side, quantity, price, source, source_namespace, source_event_id,
		source_revision, effective_at, received_at, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		fill.ID, fill.IntentID, fill.OrderID, fill.AccountID, fill.InstrumentID, fill.VenueContractID,
		fill.EconomicSourceEventID, fill.NormalizationID, fill.LedgerTransactionID, fill.Side,
		fill.Quantity.String(), fill.Price.String(), fill.Source, fill.SourceNamespace, fill.SourceEventID,
		fill.SourceRevision, fill.EffectiveAt, fill.ReceivedAt, fill.CreatedAt,
	)
	return err
}

func insertExecutionLifecycleEvent(ctx context.Context, tx pgx.Tx, event *lifecycle.Event) error {
	if event == nil {
		return fmt.Errorf("execution lifecycle event is required")
	}
	_, err := tx.Exec(ctx, `INSERT INTO execution_lifecycle_events (
		id, intent_id, order_id, binding_id, fill_id, kind, observation_class,
		observation_discriminator, prior_state, next_state, account_id,
		environment, origin_type, origin_id, strategy_version_id, policy_kind,
		policy_version, quantity_delta, cumulative_fill_quantity, quote_snapshot_id,
		source, source_namespace, source_event_id, source_revision, source_at,
		received_at, actor, reason_code, reason, evidence_bytes, evidence_sha256,
		evidence, original_fill_id, original_source_event_id, created_at
	) VALUES (
		$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,
		$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32::JSONB,$33,$34,$35
	)`,
		event.ID, event.IntentID, event.OrderID, event.BindingID, event.FillID, event.Kind,
		event.ObservationClass, event.ObservationDiscriminator, event.PriorState, event.NextState,
		event.AccountID, event.Environment, event.OriginType, event.OriginID, event.StrategyVersionID,
		event.PolicyKind, event.PolicyVersion, nullableExecutionDecimal(event.QuantityDelta),
		nullableExecutionDecimal(event.CumulativeFillQuantity), event.QuoteSnapshotID, event.Source,
		event.SourceNamespace, event.SourceEventID, event.SourceRevision, event.SourceAt, event.ReceivedAt,
		event.Actor, event.ReasonCode, event.Reason, []byte(event.Evidence), event.EvidenceSHA256,
		string(event.Evidence), event.OriginalFillID, event.OriginalSourceEventID, event.CreatedAt,
	)
	return err
}

const executionIntentSelectSQL = `SELECT
	id, account_id, environment, instrument_id, idempotency_key,
	desired_quantity_delta::TEXT, decision_quote_snapshot_id, decision_at,
	origin_type, origin_id, strategy_version_id, metadata, created_at
FROM execution_intents `

func scanExecutionIntent(row accountRow) (*lifecycle.Intent, error) {
	var intent lifecycle.Intent
	var quantity string
	var metadata []byte
	if err := row.Scan(
		&intent.ID, &intent.AccountID, &intent.Environment, &intent.InstrumentID,
		&intent.IdempotencyKey, &quantity, &intent.DecisionQuoteSnapshotID,
		&intent.DecisionAt, &intent.OriginType, &intent.OriginID,
		&intent.StrategyVersionID, &metadata, &intent.CreatedAt,
	); err != nil {
		return nil, err
	}
	parsed, err := decimal.NewFromString(quantity)
	if err != nil {
		return nil, fmt.Errorf("parse execution intent quantity %q: %w", quantity, err)
	}
	intent.DesiredQuantityDelta = parsed
	intent.Metadata = append(json.RawMessage(nil), metadata...)
	intent.DecisionAt = intent.DecisionAt.UTC().Truncate(time.Microsecond)
	intent.CreatedAt = intent.CreatedAt.UTC().Truncate(time.Microsecond)
	if err := intent.Validate(); err != nil {
		return nil, fmt.Errorf("validate loaded execution intent: %w", err)
	}
	return &intent, nil
}

type executionLifecycleQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadExecutionOrder(
	ctx context.Context,
	queryer executionLifecycleQueryer,
	intentID uuid.UUID,
) (*lifecycle.Order, error) {
	var order lifecycle.Order
	var quantity string
	var limitPrice, stopPrice *string
	err := queryer.QueryRow(ctx, `SELECT
		id, intent_id, account_id, instrument_id, idempotency_key, client_order_id,
		side, order_type, time_in_force, quantity::TEXT, limit_price::TEXT,
		stop_price::TEXT, venue, venue_contract_id, route_quote_snapshot_id,
		routed_at, policy_kind, policy_version, created_at
	FROM execution_orders WHERE intent_id = $1`, intentID).Scan(
		&order.ID, &order.IntentID, &order.AccountID, &order.InstrumentID,
		&order.IdempotencyKey, &order.ClientOrderID, &order.Side, &order.OrderType,
		&order.TimeInForce, &quantity, &limitPrice, &stopPrice, &order.Venue,
		&order.VenueContractID, &order.RouteQuoteSnapshotID, &order.RoutedAt,
		&order.PolicyKind, &order.PolicyVersion, &order.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: load execution order: %w", err)
	}
	if order.Quantity, err = decimal.NewFromString(quantity); err != nil {
		return nil, err
	}
	if order.LimitPrice, err = parseOptionalEconomicDecimal(limitPrice); err != nil {
		return nil, err
	}
	if order.StopPrice, err = parseOptionalEconomicDecimal(stopPrice); err != nil {
		return nil, err
	}
	order.RoutedAt = order.RoutedAt.UTC().Truncate(time.Microsecond)
	order.CreatedAt = order.CreatedAt.UTC().Truncate(time.Microsecond)
	if err := order.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: validate loaded execution order: %w", err)
	}
	return &order, nil
}

func loadExecutionBinding(
	ctx context.Context,
	queryer executionLifecycleQueryer,
	order *lifecycle.Order,
) (*lifecycle.OrderBinding, error) {
	if order == nil {
		return nil, repository.ErrNotFound
	}
	var binding lifecycle.OrderBinding
	err := queryer.QueryRow(ctx, `SELECT id, order_id, account_id, venue, external_order_id, created_at
		FROM execution_order_bindings WHERE order_id = $1`, order.ID).Scan(
		&binding.ID, &binding.OrderID, &binding.AccountID, &binding.Venue,
		&binding.ExternalOrderID, &binding.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: load execution binding: %w", err)
	}
	binding.CreatedAt = binding.CreatedAt.UTC().Truncate(time.Microsecond)
	if err := binding.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: validate loaded execution binding: %w", err)
	}
	return &binding, nil
}

func loadExecutionFills(
	ctx context.Context,
	queryer executionLifecycleQueryer,
	intentID uuid.UUID,
) ([]lifecycle.Fill, error) {
	rows, err := queryer.Query(ctx, `SELECT
		id, intent_id, order_id, account_id, instrument_id, venue_contract_id,
		economic_source_event_id, normalization_id, ledger_transaction_id, side,
		quantity::TEXT, price::TEXT, source, source_namespace, source_event_id,
		source_revision, effective_at, received_at, created_at
	FROM execution_fills WHERE intent_id = $1 ORDER BY created_at, id`, intentID)
	if err != nil {
		return nil, fmt.Errorf("postgres: load execution fills: %w", err)
	}
	defer rows.Close()
	result := make([]lifecycle.Fill, 0)
	for rows.Next() {
		var fill lifecycle.Fill
		var quantity, price string
		if err := rows.Scan(
			&fill.ID, &fill.IntentID, &fill.OrderID, &fill.AccountID, &fill.InstrumentID,
			&fill.VenueContractID, &fill.EconomicSourceEventID, &fill.NormalizationID,
			&fill.LedgerTransactionID, &fill.Side, &quantity, &price, &fill.Source,
			&fill.SourceNamespace, &fill.SourceEventID, &fill.SourceRevision,
			&fill.EffectiveAt, &fill.ReceivedAt, &fill.CreatedAt,
		); err != nil {
			return nil, err
		}
		if fill.Quantity, err = decimal.NewFromString(quantity); err != nil {
			return nil, err
		}
		if fill.Price, err = decimal.NewFromString(price); err != nil {
			return nil, err
		}
		fill.EffectiveAt = fill.EffectiveAt.UTC().Truncate(time.Microsecond)
		fill.ReceivedAt = fill.ReceivedAt.UTC().Truncate(time.Microsecond)
		fill.CreatedAt = fill.CreatedAt.UTC().Truncate(time.Microsecond)
		if err := fill.Validate(); err != nil {
			return nil, fmt.Errorf("postgres: validate loaded execution fill: %w", err)
		}
		result = append(result, fill)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func loadExecutionEvents(
	ctx context.Context,
	queryer executionLifecycleQueryer,
	intentID uuid.UUID,
) ([]lifecycle.Event, error) {
	rows, err := queryer.Query(ctx, `SELECT
		id, ingest_sequence, intent_id, order_id, binding_id, fill_id, kind,
		observation_class, observation_discriminator, prior_state, next_state,
		account_id, environment, origin_type, origin_id, strategy_version_id,
		policy_kind, policy_version, quantity_delta::TEXT,
		cumulative_fill_quantity::TEXT, quote_snapshot_id, source,
		source_namespace, source_event_id, source_revision, source_at, received_at,
		actor, reason_code, reason, evidence_bytes, evidence_sha256,
		original_fill_id, original_source_event_id, created_at
	FROM execution_lifecycle_events WHERE intent_id = $1 ORDER BY ingest_sequence`, intentID)
	if err != nil {
		return nil, fmt.Errorf("postgres: load execution events: %w", err)
	}
	defer rows.Close()
	result := make([]lifecycle.Event, 0)
	for rows.Next() {
		var event lifecycle.Event
		var quantity, cumulative *string
		var evidence []byte
		if err := rows.Scan(
			&event.ID, &event.IngestSequence, &event.IntentID, &event.OrderID,
			&event.BindingID, &event.FillID, &event.Kind, &event.ObservationClass,
			&event.ObservationDiscriminator, &event.PriorState, &event.NextState,
			&event.AccountID, &event.Environment, &event.OriginType, &event.OriginID,
			&event.StrategyVersionID, &event.PolicyKind, &event.PolicyVersion,
			&quantity, &cumulative, &event.QuoteSnapshotID, &event.Source,
			&event.SourceNamespace, &event.SourceEventID, &event.SourceRevision,
			&event.SourceAt, &event.ReceivedAt, &event.Actor, &event.ReasonCode,
			&event.Reason, &evidence, &event.EvidenceSHA256, &event.OriginalFillID,
			&event.OriginalSourceEventID, &event.CreatedAt,
		); err != nil {
			return nil, err
		}
		if event.QuantityDelta, err = parseOptionalEconomicDecimal(quantity); err != nil {
			return nil, err
		}
		if event.CumulativeFillQuantity, err = parseOptionalEconomicDecimal(cumulative); err != nil {
			return nil, err
		}
		event.Evidence = append(json.RawMessage(nil), evidence...)
		event.SourceAt = event.SourceAt.UTC().Truncate(time.Microsecond)
		event.ReceivedAt = event.ReceivedAt.UTC().Truncate(time.Microsecond)
		event.CreatedAt = event.CreatedAt.UTC().Truncate(time.Microsecond)
		if err := event.Validate(); err != nil {
			return nil, fmt.Errorf("postgres: validate loaded execution event: %w", err)
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func findLifecycleEvent(events []lifecycle.Event, id uuid.UUID) *lifecycle.Event {
	for index := range events {
		if events[index].ID == id {
			return &events[index]
		}
	}
	return nil
}

func nullableExecutionDecimal(value *decimal.Decimal) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func sameExecutionBindingPayload(left, right *lifecycle.OrderBinding) bool {
	return left != nil && right != nil && left.ID == right.ID && left.OrderID == right.OrderID &&
		left.AccountID == right.AccountID && left.Venue == right.Venue &&
		left.ExternalOrderID == right.ExternalOrderID
}

func executionLifecycleWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return fmt.Errorf("postgres: %s: %v: %w", operation, err, repository.ErrIdempotencyConflict)
	}
	return fmt.Errorf("postgres: %s: %w", operation, err)
}
