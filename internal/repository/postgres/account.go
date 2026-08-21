package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// AccountRepo persists explicit economic accounts and their append-only
// capital-flow history.
type AccountRepo struct{ pool *pgxpool.Pool }

var _ repository.AccountRepository = (*AccountRepo)(nil)

func NewAccountRepo(pool *pgxpool.Pool) *AccountRepo {
	return &AccountRepo{pool: pool}
}

// Create atomically persists an account and a matching opening deposit. No
// account can be created through this boundary without its initial capital
// history.
func (r *AccountRepo) Create(ctx context.Context, account *domain.Account) error {
	if account == nil {
		return fmt.Errorf("postgres: create account: account is required")
	}
	if err := account.Validate(); err != nil {
		return fmt.Errorf("postgres: create account: %w", err)
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("postgres: begin create account: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(
		ctx, `INSERT INTO accounts (
		id, name, environment, venue, external_account_id, base_currency,
		storage_namespace, evidence_class, starting_capital,
		buying_power_multiplier, margin_profile, status, created_by,
		creation_metadata, created_at
	) VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		account.ID,
		account.Name,
		account.Environment,
		account.Venue,
		account.ExternalAccountID,
		account.BaseCurrency,
		account.StorageNamespace,
		account.EvidenceClass,
		account.StartingCapital.StringFixed(8),
		account.BuyingPowerMultiplier.StringFixed(8),
		account.MarginProfile,
		account.Status,
		account.CreatedBy,
		account.CreationMetadata,
		account.CreatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("postgres: create account %s: %w", account.ID, err)
	}

	if _, err := tx.Exec(
		ctx, `INSERT INTO capital_flows (
		id, account_id, flow_type, amount, currency, idempotency_key,
		source, metadata, effective_at, observed_at, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9,$9)`,
		uuid.New(),
		account.ID,
		domain.CapitalFlowTypeDeposit,
		account.StartingCapital.StringFixed(8),
		account.BaseCurrency,
		"account-opening:"+account.ID.String(),
		domain.CapitalFlowSourceAccountOpening,
		json.RawMessage(`{"reason":"opening_capital"}`),
		account.CreatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("postgres: create opening capital flow for account %s: %w", account.ID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit create account %s: %w", account.ID, err)
	}
	return nil
}

func (r *AccountRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Account, error) {
	row := r.pool.QueryRow(ctx, `SELECT
		id, name, environment, venue, COALESCE(external_account_id, ''),
		base_currency, storage_namespace, evidence_class,
		starting_capital::TEXT, buying_power_multiplier::TEXT,
		margin_profile, status, created_by, creation_metadata, created_at
	FROM accounts WHERE id = $1`, id)
	account, err := scanEconomicAccount(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get account %s: %w", id, err)
	}
	return account, nil
}

// List returns economic accounts in stable creation order for read-only
// operator inspection.
func (r *AccountRepo) List(ctx context.Context, limit, offset int) ([]domain.Account, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := r.pool.Query(ctx, `SELECT
		id, name, environment, venue, COALESCE(external_account_id, ''),
		base_currency, storage_namespace, evidence_class,
		starting_capital::TEXT, buying_power_multiplier::TEXT,
		margin_profile, status, created_by, creation_metadata, created_at
	FROM accounts ORDER BY created_at ASC, id ASC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("postgres: list accounts: %w", err)
	}
	defer rows.Close()
	accounts := make([]domain.Account, 0)
	for rows.Next() {
		account, scanErr := scanEconomicAccount(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("postgres: scan account: %w", scanErr)
		}
		accounts = append(accounts, *account)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list accounts: %w", err)
	}
	return accounts, nil
}

// RecordCapitalFlow appends one economic flow. An identical retry returns the
// original row; reuse of the key for a different economic payload fails
// closed.
func (r *AccountRepo) RecordCapitalFlow(ctx context.Context, flow *domain.CapitalFlow) (*domain.CapitalFlow, error) {
	if flow == nil {
		return nil, fmt.Errorf("postgres: record capital flow: flow is required")
	}
	if err := flow.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: record capital flow: %w", err)
	}

	row := r.pool.QueryRow(
		ctx, `INSERT INTO capital_flows (
		id, account_id, flow_type, amount, currency, idempotency_key,
		source, external_reference, metadata, effective_at, observed_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10,$11)
	ON CONFLICT (account_id, idempotency_key) DO NOTHING
	RETURNING id, account_id, flow_type, amount::TEXT, currency,
		idempotency_key, source, COALESCE(external_reference, ''), metadata,
		effective_at, observed_at, created_at`,
		flow.ID,
		flow.AccountID,
		flow.Type,
		flow.Amount.StringFixed(8),
		flow.Currency,
		flow.IdempotencyKey,
		flow.Source,
		flow.ExternalReference,
		flow.Metadata,
		flow.EffectiveAt.UTC(),
		flow.ObservedAt.UTC(),
	)
	created, err := scanCapitalFlow(row)
	if err == nil {
		return created, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("postgres: record capital flow for account %s: %w", flow.AccountID, err)
	}

	existing, err := r.getCapitalFlowByIdempotencyKey(ctx, flow.AccountID, flow.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("postgres: replay capital flow for account %s: %w", flow.AccountID, err)
	}
	if !sameCapitalFlowPayload(existing, flow) {
		return nil, fmt.Errorf("postgres: capital-flow idempotency key %q reused with mismatched payload: %w", flow.IdempotencyKey, repository.ErrIdempotencyConflict)
	}
	return existing, nil
}

func (r *AccountRepo) ListCapitalFlows(ctx context.Context, accountID uuid.UUID, limit, offset int) ([]domain.CapitalFlow, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := r.pool.Query(ctx, `SELECT
		id, account_id, flow_type, amount::TEXT, currency,
		idempotency_key, source, COALESCE(external_reference, ''), metadata,
		effective_at, observed_at, created_at
	FROM capital_flows
	WHERE account_id = $1
	ORDER BY effective_at ASC, id ASC
	LIMIT $2 OFFSET $3`, accountID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("postgres: list capital flows for account %s: %w", accountID, err)
	}
	defer rows.Close()

	flows := make([]domain.CapitalFlow, 0)
	for rows.Next() {
		flow, err := scanCapitalFlow(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan capital flow for account %s: %w", accountID, err)
		}
		flows = append(flows, *flow)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list capital flows for account %s: %w", accountID, err)
	}
	return flows, nil
}

// GetCapitalSummary reconciles the account's immutable starting-capital field
// against its complete append-only flow history.
func (r *AccountRepo) GetCapitalSummary(ctx context.Context, accountID uuid.UUID) (*domain.AccountCapitalSummary, error) {
	var (
		summary         domain.AccountCapitalSummary
		startingText    string
		depositsText    string
		withdrawalsText string
		netText         string
	)
	err := r.pool.QueryRow(ctx, `SELECT
		a.id,
		a.base_currency,
		a.starting_capital::TEXT,
		COALESCE(SUM(CASE WHEN f.flow_type = 'deposit' THEN f.amount ELSE 0 END), 0)::TEXT,
		COALESCE(SUM(CASE WHEN f.flow_type = 'withdrawal' THEN f.amount ELSE 0 END), 0)::TEXT,
		COALESCE(SUM(CASE WHEN f.flow_type = 'deposit' THEN f.amount ELSE -f.amount END), 0)::TEXT,
		COUNT(f.id)
	FROM accounts a
	LEFT JOIN capital_flows f ON f.account_id = a.id
	WHERE a.id = $1
	GROUP BY a.id, a.base_currency, a.starting_capital`, accountID).Scan(
		&summary.AccountID,
		&summary.Currency,
		&startingText,
		&depositsText,
		&withdrawalsText,
		&netText,
		&summary.FlowCount,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: summarize capital for account %s: %w", accountID, err)
	}

	var parseErr error
	if summary.StartingCapital, parseErr = decimal.NewFromString(startingText); parseErr != nil {
		return nil, fmt.Errorf("postgres: parse starting capital %q: %w", startingText, parseErr)
	}
	if summary.Deposits, parseErr = decimal.NewFromString(depositsText); parseErr != nil {
		return nil, fmt.Errorf("postgres: parse deposits %q: %w", depositsText, parseErr)
	}
	if summary.Withdrawals, parseErr = decimal.NewFromString(withdrawalsText); parseErr != nil {
		return nil, fmt.Errorf("postgres: parse withdrawals %q: %w", withdrawalsText, parseErr)
	}
	if summary.NetCapital, parseErr = decimal.NewFromString(netText); parseErr != nil {
		return nil, fmt.Errorf("postgres: parse net capital %q: %w", netText, parseErr)
	}
	return &summary, nil
}

func (r *AccountRepo) getCapitalFlowByIdempotencyKey(ctx context.Context, accountID uuid.UUID, key string) (*domain.CapitalFlow, error) {
	row := r.pool.QueryRow(ctx, `SELECT
		id, account_id, flow_type, amount::TEXT, currency,
		idempotency_key, source, COALESCE(external_reference, ''), metadata,
		effective_at, observed_at, created_at
	FROM capital_flows
	WHERE account_id = $1 AND idempotency_key = $2`, accountID, key)
	flow, err := scanCapitalFlow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	return flow, nil
}

func sameCapitalFlowPayload(left, right *domain.CapitalFlow) bool {
	if left == nil || right == nil {
		return false
	}
	return left.AccountID == right.AccountID &&
		left.Type == right.Type &&
		left.Amount.Equal(right.Amount) &&
		left.Currency == right.Currency &&
		left.IdempotencyKey == right.IdempotencyKey &&
		left.Source == right.Source &&
		left.ExternalReference == right.ExternalReference &&
		left.EffectiveAt.Equal(right.EffectiveAt) &&
		jsonObjectsEqual(left.Metadata, right.Metadata)
}

func jsonObjectsEqual(left, right json.RawMessage) bool {
	leftValue, ok := decodeComparableJSONObject(left)
	if !ok {
		return false
	}
	rightValue, ok := decodeComparableJSONObject(right)
	if !ok {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

type comparableJSONNumber string

func decodeComparableJSONObject(value json.RawMessage) (map[string]any, bool) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, false
	}
	normalized, ok := normalizeComparableJSON(object)
	if !ok {
		return nil, false
	}
	return normalized.(map[string]any), true
}

func normalizeComparableJSON(value any) (any, bool) {
	switch typed := value.(type) {
	case json.Number:
		number, err := decimal.NewFromString(typed.String())
		if err != nil {
			return nil, false
		}
		return comparableJSONNumber(number.String()), true
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, child := range typed {
			normalizedChild, ok := normalizeComparableJSON(child)
			if !ok {
				return nil, false
			}
			normalized[key] = normalizedChild
		}
		return normalized, true
	case []any:
		normalized := make([]any, len(typed))
		for index, child := range typed {
			normalizedChild, ok := normalizeComparableJSON(child)
			if !ok {
				return nil, false
			}
			normalized[index] = normalizedChild
		}
		return normalized, true
	default:
		return value, true
	}
}

type accountRow interface {
	Scan(dest ...any) error
}

func scanEconomicAccount(row accountRow) (*domain.Account, error) {
	var (
		account         domain.Account
		startingCapital string
		multiplier      string
		metadata        []byte
	)
	if err := row.Scan(
		&account.ID,
		&account.Name,
		&account.Environment,
		&account.Venue,
		&account.ExternalAccountID,
		&account.BaseCurrency,
		&account.StorageNamespace,
		&account.EvidenceClass,
		&startingCapital,
		&multiplier,
		&account.MarginProfile,
		&account.Status,
		&account.CreatedBy,
		&metadata,
		&account.CreatedAt,
	); err != nil {
		return nil, err
	}
	var err error
	account.StartingCapital, err = decimal.NewFromString(startingCapital)
	if err != nil {
		return nil, fmt.Errorf("parse starting capital %q: %w", startingCapital, err)
	}
	account.BuyingPowerMultiplier, err = decimal.NewFromString(multiplier)
	if err != nil {
		return nil, fmt.Errorf("parse buying-power multiplier %q: %w", multiplier, err)
	}
	account.CreationMetadata = append(json.RawMessage(nil), metadata...)
	return &account, nil
}

func scanCapitalFlow(row accountRow) (*domain.CapitalFlow, error) {
	var (
		flow     domain.CapitalFlow
		amount   string
		metadata []byte
	)
	if err := row.Scan(
		&flow.ID,
		&flow.AccountID,
		&flow.Type,
		&amount,
		&flow.Currency,
		&flow.IdempotencyKey,
		&flow.Source,
		&flow.ExternalReference,
		&metadata,
		&flow.EffectiveAt,
		&flow.ObservedAt,
		&flow.CreatedAt,
	); err != nil {
		return nil, err
	}
	parsedAmount, err := decimal.NewFromString(amount)
	if err != nil {
		return nil, fmt.Errorf("parse capital-flow amount %q: %w", amount, err)
	}
	flow.Amount = parsedAmount
	flow.Metadata = append(json.RawMessage(nil), metadata...)
	return &flow, nil
}
