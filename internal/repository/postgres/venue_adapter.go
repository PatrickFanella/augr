package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// VenueAdapterRepo persists reviewed policy artifacts and exact raw provider
// observations. It does not select a current policy or call a provider.
type VenueAdapterRepo struct{ pool *pgxpool.Pool }

var (
	_ repository.VenuePolicyRepository      = (*VenueAdapterRepo)(nil)
	_ repository.VenueObservationRepository = (*VenueAdapterRepo)(nil)
)

// NewVenueAdapterRepo returns a venue-adapter repository backed by pool.
func NewVenueAdapterRepo(pool *pgxpool.Pool) *VenueAdapterRepo {
	return &VenueAdapterRepo{pool: pool}
}

// RegisterVenuePolicy inserts one exact reviewed artifact or returns the
// already-persisted artifact when an identical registration is replayed.
func (repo *VenueAdapterRepo) RegisterVenuePolicy(
	ctx context.Context,
	artifact *venue.PolicyArtifact,
) (*venue.PolicyArtifact, error) {
	if artifact == nil {
		return nil, fmt.Errorf("postgres: register venue policy: artifact is required")
	}
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: register venue policy: repository pool is required")
	}
	if _, err := venue.PolicyFromArtifact(*artifact); err != nil {
		return nil, repo.venuePolicyValidationError(ctx, artifact, err)
	}

	persisted, err := scanVenuePolicyArtifact(repo.pool.QueryRow(ctx, `INSERT INTO venue_adapter_policy_artifacts (
		id, schema_name, provider, venue, policy_version, sha256,
		canonical_bytes, canonical_json, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,convert_from($7,'UTF8')::JSONB,$8)
	ON CONFLICT (policy_version) DO NOTHING
	RETURNING id, schema_name, provider, venue, policy_version, sha256, canonical_bytes, created_at`,
		artifact.ID,
		artifact.Schema,
		artifact.Provider,
		artifact.Venue,
		artifact.Version,
		artifact.SHA256,
		[]byte(artifact.CanonicalBytes),
		artifact.CreatedAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := repo.GetVenuePolicyByVersion(ctx, artifact.Version)
		if loadErr != nil {
			return nil, fmt.Errorf("postgres: load replayed venue policy %q: %w", artifact.Version, loadErr)
		}
		if !venue.SamePolicyArtifactPayload(existing, artifact) {
			return nil, fmt.Errorf(
				"postgres: venue policy version %q reused with changed payload: %w",
				artifact.Version,
				repository.ErrIdempotencyConflict,
			)
		}
		return existing, nil
	}
	if err != nil {
		return nil, venueAdapterWriteError("insert venue policy", err)
	}
	if _, err := venue.PolicyFromArtifact(*persisted); err != nil {
		return nil, fmt.Errorf("postgres: validate inserted venue policy %q: %w", artifact.Version, err)
	}
	return persisted, nil
}

// GetVenuePolicyByVersion reloads the exact reviewed artifact pinned on an
// order. There is intentionally no mutable current-policy lookup.
func (repo *VenueAdapterRepo) GetVenuePolicyByVersion(
	ctx context.Context,
	version string,
) (*venue.PolicyArtifact, error) {
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: get venue policy: repository pool is required")
	}
	if version == "" || version != strings.TrimSpace(version) || len(version) > 256 {
		return nil, fmt.Errorf("postgres: get venue policy: canonical version is required")
	}
	artifact, err := scanVenuePolicyArtifact(repo.pool.QueryRow(ctx, `SELECT
		id, schema_name, provider, venue, policy_version, sha256, canonical_bytes, created_at
	FROM venue_adapter_policy_artifacts
	WHERE policy_version = $1`, version))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get venue policy %q: %w", version, err)
	}
	if _, err := venue.PolicyFromArtifact(*artifact); err != nil {
		return nil, fmt.Errorf("postgres: validate loaded venue policy %q: %w", version, err)
	}
	return artifact, nil
}

// RecordVenueObservation journals exact provider evidence before any
// lifecycle or economic interpretation. Exact retries return the original
// row; changed reuse of the deterministic source identity is a conflict.
func (repo *VenueAdapterRepo) RecordVenueObservation(
	ctx context.Context,
	observation *venue.Observation,
) (*venue.Observation, error) {
	if observation == nil {
		return nil, fmt.Errorf("postgres: record venue observation: observation is required")
	}
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: record venue observation: repository pool is required")
	}
	if err := observation.Validate(); err != nil {
		return nil, repo.venueObservationValidationError(ctx, observation, err)
	}

	persisted, err := scanVenueObservation(repo.pool.QueryRow(ctx, venueObservationInsertSQL,
		observation.ID,
		observation.AccountID,
		observation.IntentID,
		observation.OrderID,
		observation.BindingID,
		observation.VenueContractID,
		observation.Provider,
		observation.Venue,
		observation.PolicyVersion,
		observation.Kind,
		observation.ProviderState,
		observation.MappedOutcome,
		observation.ExternalOrderID,
		observation.ClientOrderID,
		observation.ProviderContractID,
		observation.CanonicalOutcome,
		observation.ProviderBookSide,
		observation.ProviderAction,
		nullableVenueDecimal(observation.ProviderPrice),
		observation.IdentityKind,
		observation.SourceNamespace,
		observation.SourceEventID,
		observation.SourceRevision,
		observation.SourceAt,
		observation.ReceivedAt,
		[]byte(observation.RawPayload),
		observation.PayloadSHA256,
		observation.CreatedAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := repo.GetVenueObservationByID(ctx, observation.ID)
		if loadErr != nil {
			return nil, fmt.Errorf("postgres: load replayed venue observation %s: %w", observation.ID, loadErr)
		}
		if !venue.SameObservationPayload(existing, observation) {
			return nil, fmt.Errorf(
				"postgres: venue observation identity %s reused with changed evidence: %w",
				observation.ID,
				repository.ErrIdempotencyConflict,
			)
		}
		return existing, nil
	}
	if err != nil {
		return nil, venueAdapterWriteError("insert venue observation", err)
	}
	if !venue.SameObservationPayload(persisted, observation) {
		return nil, fmt.Errorf("postgres: inserted venue observation %s differs from requested evidence", observation.ID)
	}
	return persisted, nil
}

// GetVenueObservationByID reloads one immutable raw provider fact.
func (repo *VenueAdapterRepo) GetVenueObservationByID(
	ctx context.Context,
	id uuid.UUID,
) (*venue.Observation, error) {
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: get venue observation: repository pool is required")
	}
	if id == uuid.Nil {
		return nil, fmt.Errorf("postgres: get venue observation: ID is required")
	}
	observation, err := scanVenueObservation(repo.pool.QueryRow(ctx, venueObservationSelectSQL+" WHERE id=$1", id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get venue observation %s: %w", id, err)
	}
	return observation, nil
}

func (repo *VenueAdapterRepo) venuePolicyValidationError(
	ctx context.Context,
	artifact *venue.PolicyArtifact,
	validationErr error,
) error {
	if artifact.Version != "" {
		existing, err := repo.GetVenuePolicyByVersion(ctx, artifact.Version)
		if err == nil && !venue.SamePolicyArtifactPayload(existing, artifact) {
			return fmt.Errorf(
				"postgres: venue policy version %q reused with changed payload: %v: %w",
				artifact.Version,
				validationErr,
				repository.ErrIdempotencyConflict,
			)
		}
	}
	return fmt.Errorf("postgres: register venue policy: %w", validationErr)
}

func (repo *VenueAdapterRepo) venueObservationValidationError(
	ctx context.Context,
	observation *venue.Observation,
	validationErr error,
) error {
	if observation.ID != uuid.Nil {
		existing, err := repo.GetVenueObservationByID(ctx, observation.ID)
		if err == nil && !venue.SameObservationPayload(existing, observation) {
			return fmt.Errorf(
				"postgres: venue observation identity %s reused with changed evidence: %v: %w",
				observation.ID,
				validationErr,
				repository.ErrIdempotencyConflict,
			)
		}
	}
	return fmt.Errorf("postgres: record venue observation: %w", validationErr)
}

func scanVenuePolicyArtifact(row accountRow) (*venue.PolicyArtifact, error) {
	artifact := new(venue.PolicyArtifact)
	if err := row.Scan(
		&artifact.ID,
		&artifact.Schema,
		&artifact.Provider,
		&artifact.Venue,
		&artifact.Version,
		&artifact.SHA256,
		&artifact.CanonicalBytes,
		&artifact.CreatedAt,
	); err != nil {
		return nil, err
	}
	artifact.CreatedAt = artifact.CreatedAt.UTC().Truncate(time.Microsecond)
	return artifact, nil
}

const venueObservationColumns = `
	id, account_id, intent_id, order_id, binding_id, venue_contract_id,
	provider, venue, policy_version, kind, provider_state, mapped_outcome,
	external_order_id, client_order_id, provider_contract_id, canonical_outcome,
	provider_book_side, provider_action, provider_price::TEXT, identity_kind,
	source_namespace, source_event_id, source_revision, source_at, received_at,
	raw_bytes, raw_sha256, created_at`

const venueObservationInsertSQL = `INSERT INTO venue_observations (
	id, account_id, intent_id, order_id, binding_id, venue_contract_id,
	provider, venue, policy_version, kind, provider_state, mapped_outcome,
	external_order_id, client_order_id, provider_contract_id, canonical_outcome,
	provider_book_side, provider_action, provider_price, identity_kind,
	source_namespace, source_event_id, source_revision, source_at, received_at,
	raw_bytes, raw_sha256, raw_json, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,
	$19,$20,$21,$22,$23,$24,$25,$26,$27,convert_from($26,'UTF8')::JSONB,$28)
ON CONFLICT DO NOTHING
RETURNING ` + venueObservationColumns

const venueObservationSelectSQL = `SELECT ` + venueObservationColumns + ` FROM venue_observations`

func scanVenueObservation(row accountRow) (*venue.Observation, error) {
	var (
		storedID         uuid.UUID
		bindingID        *uuid.UUID
		provider         string
		kind             string
		mappedOutcome    string
		providerPriceRaw *string
		identityKind     string
		rawPayload       []byte
		storedDigest     string
		input            venue.ObservationInput
	)
	if err := row.Scan(
		&storedID,
		&input.AccountID,
		&input.IntentID,
		&input.OrderID,
		&bindingID,
		&input.VenueContractID,
		&provider,
		&input.Venue,
		&input.PolicyVersion,
		&kind,
		&input.ProviderState,
		&mappedOutcome,
		&input.ExternalOrderID,
		&input.ClientOrderID,
		&input.ProviderContractID,
		&input.CanonicalOutcome,
		&input.ProviderBookSide,
		&input.ProviderAction,
		&providerPriceRaw,
		&identityKind,
		&input.SourceNamespace,
		&input.SourceEventID,
		&input.SourceRevision,
		&input.SourceAt,
		&input.ReceivedAt,
		&rawPayload,
		&storedDigest,
		&input.CreatedAt,
	); err != nil {
		return nil, err
	}
	input.BindingID = bindingID
	input.Provider = venue.Provider(provider)
	input.Kind = venue.ObservationKind(kind)
	input.MappedOutcome = venue.MappedOutcome(mappedOutcome)
	input.IdentityKind = venue.SourceIdentityKind(identityKind)
	input.RawPayload = append([]byte(nil), rawPayload...)
	if providerPriceRaw != nil {
		parsed, err := decimal.NewFromString(*providerPriceRaw)
		if err != nil {
			return nil, fmt.Errorf("parse venue observation provider price %q: %w", *providerPriceRaw, err)
		}
		input.ProviderPrice = &parsed
	}
	observation, err := venue.NewObservation(input)
	if err != nil {
		return nil, fmt.Errorf("validate loaded venue observation: %w", err)
	}
	if observation.ID != storedID || observation.PayloadSHA256 != storedDigest {
		return nil, fmt.Errorf("loaded venue observation identity or digest differs from stored fact")
	}
	return observation, nil
}

func nullableVenueDecimal(value *decimal.Decimal) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func venueAdapterWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return fmt.Errorf("postgres: %s: %v: %w", operation, err, repository.ErrIdempotencyConflict)
	}
	return fmt.Errorf("postgres: %s: %w", operation, err)
}
