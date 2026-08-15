package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// InstrumentRepo persists canonical identities and append-only reference
// facts. It deliberately does not modify legacy ticker repositories.
type InstrumentRepo struct{ pool *pgxpool.Pool }

var _ repository.InstrumentRepository = (*InstrumentRepo)(nil)

func NewInstrumentRepo(pool *pgxpool.Pool) *InstrumentRepo {
	return &InstrumentRepo{pool: pool}
}

// CreateInstrument inserts a canonical identity. Replaying the same identity
// and payload returns the original row; changing the payload is a conflict.
func (repo *InstrumentRepo) CreateInstrument(ctx context.Context, value *instrument.Instrument) (*instrument.Instrument, error) {
	if value == nil {
		return nil, fmt.Errorf("postgres: create instrument: instrument is required")
	}
	if err := value.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: create instrument: %w", err)
	}

	var persistedID uuid.UUID
	err := repo.pool.QueryRow(ctx, `INSERT INTO instruments (
		id, identity_key, asset_class, primary_venue, currency, tick_size,
		lot_size, multiplier, expiration, exercise_style, settlement_method,
		underlying_instrument_id, status, metadata, created_at
	) VALUES (
		$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15
	)
	ON CONFLICT (identity_key) DO NOTHING
	RETURNING id`,
		value.ID,
		value.IdentityKey,
		value.AssetClass,
		value.PrimaryVenue,
		nullString(value.Currency),
		nullableInstrumentDecimal(value.TickSize),
		nullableInstrumentDecimal(value.LotSize),
		nullableInstrumentDecimal(value.Multiplier),
		value.Expiration,
		nullString(string(value.ExerciseStyle)),
		nullString(string(value.SettlementMethod)),
		value.UnderlyingID,
		value.Status,
		jsonForStorage(value.Metadata),
		value.CreatedAt,
	).Scan(&persistedID)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := repo.getInstrumentByIdentityKey(ctx, value.IdentityKey)
		if loadErr != nil {
			return nil, fmt.Errorf("postgres: load replayed instrument %q: %w", value.IdentityKey, loadErr)
		}
		if !sameInstrumentPayload(existing, value) {
			return nil, fmt.Errorf("postgres: instrument identity %q reused with mismatched payload: %w", value.IdentityKey, repository.ErrIdempotencyConflict)
		}
		return existing, nil
	}
	if err != nil {
		return nil, instrumentWriteError("create instrument", err)
	}
	return repo.GetInstrumentByID(ctx, persistedID)
}

func (repo *InstrumentRepo) GetInstrumentByID(ctx context.Context, id uuid.UUID) (*instrument.Instrument, error) {
	value, err := scanInstrument(repo.pool.QueryRow(ctx, instrumentSelectSQL+` WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get instrument %s: %w", id, err)
	}
	return value, nil
}

func (repo *InstrumentRepo) getInstrumentByIdentityKey(ctx context.Context, identityKey string) (*instrument.Instrument, error) {
	value, err := scanInstrument(repo.pool.QueryRow(ctx, instrumentSelectSQL+` WHERE identity_key = $1`, identityKey))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return value, nil
}

// AppendAliasEvent records one chronologically appended assignment or
// retirement. PostgreSQL serializes transitions for each alias key.
func (repo *InstrumentRepo) AppendAliasEvent(ctx context.Context, event *instrument.AliasEvent) (*instrument.AliasEvent, error) {
	if event == nil {
		return nil, fmt.Errorf("postgres: append alias event: event is required")
	}
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: append alias event: %w", err)
	}

	var persistedID uuid.UUID
	err := repo.pool.QueryRow(ctx, `INSERT INTO instrument_alias_events (
		id, instrument_id, provider, alias_type, alias_value, action,
		effective_at, source, metadata, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	ON CONFLICT (provider, alias_type, alias_value, effective_at) DO NOTHING
	RETURNING id`,
		event.ID,
		event.InstrumentID,
		event.Provider,
		event.AliasType,
		event.AliasValue,
		event.Action,
		event.EffectiveAt,
		event.Source,
		jsonForStorage(event.Metadata),
		event.CreatedAt,
	).Scan(&persistedID)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := repo.getAliasEventByIdentity(ctx, event.Provider, event.AliasType, event.AliasValue, event.EffectiveAt)
		if loadErr != nil {
			return nil, fmt.Errorf("postgres: load replayed alias event: %w", loadErr)
		}
		if !sameAliasEventPayload(existing, event) {
			return nil, fmt.Errorf("postgres: alias event identity reused with mismatched payload: %w", repository.ErrIdempotencyConflict)
		}
		return existing, nil
	}
	if err != nil {
		return nil, instrumentWriteError("append alias event", err)
	}
	return repo.getAliasEventByID(ctx, persistedID)
}

// ResolveAlias returns the instrument bound by the latest event at or before
// asOf. A retirement is a historical not-found state, not a mutable delete.
func (repo *InstrumentRepo) ResolveAlias(ctx context.Context, provider string, aliasType instrument.AliasType, aliasValue string, asOf time.Time) (*instrument.Instrument, error) {
	normalizedProvider, normalizedValue, err := instrument.NormalizeAlias(provider, aliasType, aliasValue)
	if err != nil {
		return nil, fmt.Errorf("postgres: resolve alias: %w", err)
	}
	if asOf.IsZero() {
		return nil, fmt.Errorf("postgres: resolve alias: as-of time is required")
	}

	var instrumentID uuid.UUID
	var action instrument.AliasAction
	err = repo.pool.QueryRow(ctx, `SELECT instrument_id, action
		FROM instrument_alias_events
		WHERE provider = $1
		  AND alias_type = $2
		  AND alias_value = $3
		  AND effective_at <= $4
		ORDER BY effective_at DESC, created_at DESC, id DESC
		LIMIT 1`,
		normalizedProvider,
		aliasType,
		normalizedValue,
		asOf.UTC().Truncate(time.Microsecond),
	).Scan(&instrumentID, &action)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && action == instrument.AliasRetired) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: resolve alias: %w", err)
	}
	return repo.GetInstrumentByID(ctx, instrumentID)
}

func (repo *InstrumentRepo) RegisterVenueContract(ctx context.Context, contract *instrument.VenueContract) (*instrument.VenueContract, error) {
	if contract == nil {
		return nil, fmt.Errorf("postgres: register venue contract: contract is required")
	}
	if err := contract.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: register venue contract: %w", err)
	}

	var persistedID uuid.UUID
	err := repo.pool.QueryRow(ctx, `INSERT INTO venue_contracts (
		id, instrument_id, venue, contract_id, currency, tick_size, lot_size,
		multiplier, settlement_method, valid_from, valid_to, metadata, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	ON CONFLICT (venue, contract_id, valid_from) DO NOTHING
	RETURNING id`,
		contract.ID,
		contract.InstrumentID,
		contract.Venue,
		contract.ContractID,
		contract.Currency,
		contract.TickSize.StringFixed(12),
		contract.LotSize.StringFixed(12),
		contract.Multiplier.StringFixed(12),
		contract.SettlementMethod,
		contract.ValidFrom,
		contract.ValidTo,
		jsonForStorage(contract.Metadata),
		contract.CreatedAt,
	).Scan(&persistedID)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := repo.getVenueContractByIdentity(ctx, contract.Venue, contract.ContractID, contract.ValidFrom)
		if loadErr != nil {
			return nil, fmt.Errorf("postgres: load replayed venue contract: %w", loadErr)
		}
		if !sameVenueContractPayload(existing, contract) {
			return nil, fmt.Errorf("postgres: venue contract identity reused with mismatched payload: %w", repository.ErrIdempotencyConflict)
		}
		return existing, nil
	}
	if err != nil {
		return nil, instrumentWriteError("register venue contract", err)
	}
	return repo.getVenueContractByID(ctx, persistedID)
}

func (repo *InstrumentRepo) RecordCorporateAction(ctx context.Context, action *instrument.CorporateAction) (*instrument.CorporateAction, error) {
	if action == nil {
		return nil, fmt.Errorf("postgres: record corporate action: action is required")
	}
	if err := action.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: record corporate action: %w", err)
	}

	var persistedID uuid.UUID
	err := repo.pool.QueryRow(ctx, `INSERT INTO corporate_actions (
		id, instrument_id, successor_instrument_id, action_type, effective_at,
		ratio_numerator, ratio_denominator, cash_amount, cash_currency,
		source, source_event_id, metadata, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	ON CONFLICT (source, source_event_id) DO NOTHING
	RETURNING id`,
		action.ID,
		action.InstrumentID,
		action.SuccessorInstrumentID,
		action.ActionType,
		action.EffectiveAt,
		nullableInstrumentDecimal(action.RatioNumerator),
		nullableInstrumentDecimal(action.RatioDenominator),
		nullableDecimalPointer(action.CashAmount),
		nullString(action.CashCurrency),
		action.Source,
		action.SourceEventID,
		jsonForStorage(action.Metadata),
		action.CreatedAt,
	).Scan(&persistedID)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := repo.getCorporateActionBySource(ctx, action.Source, action.SourceEventID)
		if loadErr != nil {
			return nil, fmt.Errorf("postgres: load replayed corporate action: %w", loadErr)
		}
		if !sameCorporateActionPayload(existing, action) {
			return nil, fmt.Errorf("postgres: corporate action source identity reused with mismatched payload: %w", repository.ErrIdempotencyConflict)
		}
		return existing, nil
	}
	if err != nil {
		return nil, instrumentWriteError("record corporate action", err)
	}
	return repo.getCorporateActionByID(ctx, persistedID)
}

const instrumentSelectSQL = `SELECT
	id, identity_key, asset_class, primary_venue, COALESCE(currency, ''),
	COALESCE(tick_size, 0)::TEXT, COALESCE(lot_size, 0)::TEXT,
	COALESCE(multiplier, 0)::TEXT, expiration, COALESCE(exercise_style, ''),
	COALESCE(settlement_method, ''), underlying_instrument_id, status, metadata, created_at
FROM instruments`

func scanInstrument(row accountRow) (*instrument.Instrument, error) {
	var value instrument.Instrument
	var tickSize, lotSize, multiplier string
	var metadata []byte
	if err := row.Scan(
		&value.ID,
		&value.IdentityKey,
		&value.AssetClass,
		&value.PrimaryVenue,
		&value.Currency,
		&tickSize,
		&lotSize,
		&multiplier,
		&value.Expiration,
		&value.ExerciseStyle,
		&value.SettlementMethod,
		&value.UnderlyingID,
		&value.Status,
		&metadata,
		&value.CreatedAt,
	); err != nil {
		return nil, err
	}
	var err error
	if value.TickSize, err = decimal.NewFromString(tickSize); err != nil {
		return nil, fmt.Errorf("parse instrument tick size %q: %w", tickSize, err)
	}
	if value.LotSize, err = decimal.NewFromString(lotSize); err != nil {
		return nil, fmt.Errorf("parse instrument lot size %q: %w", lotSize, err)
	}
	if value.Multiplier, err = decimal.NewFromString(multiplier); err != nil {
		return nil, fmt.Errorf("parse instrument multiplier %q: %w", multiplier, err)
	}
	value.Metadata = append(json.RawMessage(nil), metadata...)
	value.CreatedAt = value.CreatedAt.UTC()
	if value.Expiration != nil {
		expiration := value.Expiration.UTC()
		value.Expiration = &expiration
	}
	if err := value.Validate(); err != nil {
		return nil, fmt.Errorf("validate loaded instrument: %w", err)
	}
	return &value, nil
}

const aliasEventSelectSQL = `SELECT
	id, instrument_id, provider, alias_type, alias_value, action,
	effective_at, source, metadata, created_at
FROM instrument_alias_events`

func (repo *InstrumentRepo) getAliasEventByID(ctx context.Context, id uuid.UUID) (*instrument.AliasEvent, error) {
	return repo.scanAliasEventRow(repo.pool.QueryRow(ctx, aliasEventSelectSQL+` WHERE id = $1`, id))
}

func (repo *InstrumentRepo) getAliasEventByIdentity(ctx context.Context, provider string, aliasType instrument.AliasType, aliasValue string, effectiveAt time.Time) (*instrument.AliasEvent, error) {
	return repo.scanAliasEventRow(repo.pool.QueryRow(ctx, aliasEventSelectSQL+`
		WHERE provider = $1 AND alias_type = $2 AND alias_value = $3 AND effective_at = $4`,
		provider,
		aliasType,
		aliasValue,
		effectiveAt,
	))
}

func (*InstrumentRepo) scanAliasEventRow(row accountRow) (*instrument.AliasEvent, error) {
	var event instrument.AliasEvent
	var metadata []byte
	if err := row.Scan(
		&event.ID,
		&event.InstrumentID,
		&event.Provider,
		&event.AliasType,
		&event.AliasValue,
		&event.Action,
		&event.EffectiveAt,
		&event.Source,
		&metadata,
		&event.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	event.Metadata = append(json.RawMessage(nil), metadata...)
	event.EffectiveAt = event.EffectiveAt.UTC()
	event.CreatedAt = event.CreatedAt.UTC()
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("validate loaded alias event: %w", err)
	}
	return &event, nil
}

const venueContractSelectSQL = `SELECT
	id, instrument_id, venue, contract_id, currency,
	tick_size::TEXT, lot_size::TEXT, multiplier::TEXT,
	settlement_method, valid_from, valid_to, metadata, created_at
FROM venue_contracts`

func (repo *InstrumentRepo) getVenueContractByID(ctx context.Context, id uuid.UUID) (*instrument.VenueContract, error) {
	return scanVenueContract(repo.pool.QueryRow(ctx, venueContractSelectSQL+` WHERE id = $1`, id))
}

func (repo *InstrumentRepo) getVenueContractByIdentity(ctx context.Context, venue, contractID string, validFrom time.Time) (*instrument.VenueContract, error) {
	return scanVenueContract(repo.pool.QueryRow(ctx, venueContractSelectSQL+`
		WHERE venue = $1 AND contract_id = $2 AND valid_from = $3`, venue, contractID, validFrom))
}

func scanVenueContract(row accountRow) (*instrument.VenueContract, error) {
	var contract instrument.VenueContract
	var tickSize, lotSize, multiplier string
	var metadata []byte
	if err := row.Scan(
		&contract.ID,
		&contract.InstrumentID,
		&contract.Venue,
		&contract.ContractID,
		&contract.Currency,
		&tickSize,
		&lotSize,
		&multiplier,
		&contract.SettlementMethod,
		&contract.ValidFrom,
		&contract.ValidTo,
		&metadata,
		&contract.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	var err error
	if contract.TickSize, err = decimal.NewFromString(tickSize); err != nil {
		return nil, fmt.Errorf("parse venue contract tick size %q: %w", tickSize, err)
	}
	if contract.LotSize, err = decimal.NewFromString(lotSize); err != nil {
		return nil, fmt.Errorf("parse venue contract lot size %q: %w", lotSize, err)
	}
	if contract.Multiplier, err = decimal.NewFromString(multiplier); err != nil {
		return nil, fmt.Errorf("parse venue contract multiplier %q: %w", multiplier, err)
	}
	contract.Metadata = append(json.RawMessage(nil), metadata...)
	contract.ValidFrom = contract.ValidFrom.UTC()
	contract.CreatedAt = contract.CreatedAt.UTC()
	if contract.ValidTo != nil {
		validTo := contract.ValidTo.UTC()
		contract.ValidTo = &validTo
	}
	if err := contract.Validate(); err != nil {
		return nil, fmt.Errorf("validate loaded venue contract: %w", err)
	}
	return &contract, nil
}

const corporateActionSelectSQL = `SELECT
	id, instrument_id, successor_instrument_id, action_type, effective_at,
	COALESCE(ratio_numerator, 0)::TEXT, COALESCE(ratio_denominator, 0)::TEXT,
	cash_amount::TEXT, COALESCE(cash_currency, ''), source, source_event_id,
	metadata, created_at
FROM corporate_actions`

func (repo *InstrumentRepo) getCorporateActionByID(ctx context.Context, id uuid.UUID) (*instrument.CorporateAction, error) {
	return scanCorporateAction(repo.pool.QueryRow(ctx, corporateActionSelectSQL+` WHERE id = $1`, id))
}

func (repo *InstrumentRepo) getCorporateActionBySource(ctx context.Context, source, sourceEventID string) (*instrument.CorporateAction, error) {
	return scanCorporateAction(repo.pool.QueryRow(ctx, corporateActionSelectSQL+`
		WHERE source = $1 AND source_event_id = $2`, source, sourceEventID))
}

func scanCorporateAction(row accountRow) (*instrument.CorporateAction, error) {
	var action instrument.CorporateAction
	var ratioNumerator, ratioDenominator string
	var cashAmount *string
	var metadata []byte
	if err := row.Scan(
		&action.ID,
		&action.InstrumentID,
		&action.SuccessorInstrumentID,
		&action.ActionType,
		&action.EffectiveAt,
		&ratioNumerator,
		&ratioDenominator,
		&cashAmount,
		&action.CashCurrency,
		&action.Source,
		&action.SourceEventID,
		&metadata,
		&action.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	var err error
	if action.RatioNumerator, err = decimal.NewFromString(ratioNumerator); err != nil {
		return nil, fmt.Errorf("parse corporate action ratio numerator %q: %w", ratioNumerator, err)
	}
	if action.RatioDenominator, err = decimal.NewFromString(ratioDenominator); err != nil {
		return nil, fmt.Errorf("parse corporate action ratio denominator %q: %w", ratioDenominator, err)
	}
	if cashAmount != nil {
		parsed, err := decimal.NewFromString(*cashAmount)
		if err != nil {
			return nil, fmt.Errorf("parse corporate action cash amount %q: %w", *cashAmount, err)
		}
		action.CashAmount = &parsed
	}
	action.Metadata = append(json.RawMessage(nil), metadata...)
	action.EffectiveAt = action.EffectiveAt.UTC()
	action.CreatedAt = action.CreatedAt.UTC()
	if err := action.Validate(); err != nil {
		return nil, fmt.Errorf("validate loaded corporate action: %w", err)
	}
	return &action, nil
}

func nullableInstrumentDecimal(value decimal.Decimal) any {
	if value.IsZero() {
		return nil
	}
	return value.StringFixed(12)
}

func nullableDecimalPointer(value *decimal.Decimal) any {
	if value == nil {
		return nil
	}
	return value.StringFixed(12)
}

func instrumentWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return fmt.Errorf("postgres: %s: %v: %w", operation, err, repository.ErrIdempotencyConflict)
	}
	return fmt.Errorf("postgres: %s: %w", operation, err)
}

func sameInstrumentPayload(left, right *instrument.Instrument) bool {
	return left != nil && right != nil &&
		left.IdentityKey == right.IdentityKey &&
		left.AssetClass == right.AssetClass &&
		left.PrimaryVenue == right.PrimaryVenue &&
		left.Currency == right.Currency &&
		left.TickSize.Equal(right.TickSize) &&
		left.LotSize.Equal(right.LotSize) &&
		left.Multiplier.Equal(right.Multiplier) &&
		sameTimePointer(left.Expiration, right.Expiration) &&
		left.ExerciseStyle == right.ExerciseStyle &&
		left.SettlementMethod == right.SettlementMethod &&
		sameUUIDPointer(left.UnderlyingID, right.UnderlyingID) &&
		left.Status == right.Status &&
		jsonObjectsEqual(left.Metadata, right.Metadata)
}

func sameAliasEventPayload(left, right *instrument.AliasEvent) bool {
	return left != nil && right != nil &&
		left.InstrumentID == right.InstrumentID &&
		left.Provider == right.Provider &&
		left.AliasType == right.AliasType &&
		left.AliasValue == right.AliasValue &&
		left.Action == right.Action &&
		left.EffectiveAt.Equal(right.EffectiveAt) &&
		left.Source == right.Source &&
		jsonObjectsEqual(left.Metadata, right.Metadata)
}

func sameVenueContractPayload(left, right *instrument.VenueContract) bool {
	return left != nil && right != nil &&
		left.InstrumentID == right.InstrumentID &&
		left.Venue == right.Venue &&
		left.ContractID == right.ContractID &&
		left.Currency == right.Currency &&
		left.TickSize.Equal(right.TickSize) &&
		left.LotSize.Equal(right.LotSize) &&
		left.Multiplier.Equal(right.Multiplier) &&
		left.SettlementMethod == right.SettlementMethod &&
		left.ValidFrom.Equal(right.ValidFrom) &&
		sameTimePointer(left.ValidTo, right.ValidTo) &&
		jsonObjectsEqual(left.Metadata, right.Metadata)
}

func sameCorporateActionPayload(left, right *instrument.CorporateAction) bool {
	return left != nil && right != nil &&
		left.InstrumentID == right.InstrumentID &&
		sameUUIDPointer(left.SuccessorInstrumentID, right.SuccessorInstrumentID) &&
		left.ActionType == right.ActionType &&
		left.EffectiveAt.Equal(right.EffectiveAt) &&
		left.RatioNumerator.Equal(right.RatioNumerator) &&
		left.RatioDenominator.Equal(right.RatioDenominator) &&
		sameDecimalPointer(left.CashAmount, right.CashAmount) &&
		left.CashCurrency == right.CashCurrency &&
		left.Source == right.Source &&
		left.SourceEventID == right.SourceEventID &&
		jsonObjectsEqual(left.Metadata, right.Metadata)
}

func sameTimePointer(left, right *time.Time) bool {
	return (left == nil && right == nil) ||
		(left != nil && right != nil && left.Equal(*right))
}

func sameUUIDPointer(left, right *uuid.UUID) bool {
	return (left == nil && right == nil) ||
		(left != nil && right != nil && *left == *right)
}

func sameDecimalPointer(left, right *decimal.Decimal) bool {
	return (left == nil && right == nil) ||
		(left != nil && right != nil && left.Equal(*right))
}
