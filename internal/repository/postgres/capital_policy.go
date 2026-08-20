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

	"github.com/PatrickFanella/get-rich-quick/internal/capital"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// CapitalPolicyRepo persists immutable reviewed policy artifacts and explicit
// account bindings. It does not select a current policy or activate admission.
type CapitalPolicyRepo struct{ pool *pgxpool.Pool }

var _ repository.CapitalPolicyRepository = (*CapitalPolicyRepo)(nil)

func NewCapitalPolicyRepo(pool *pgxpool.Pool) *CapitalPolicyRepo {
	return &CapitalPolicyRepo{pool: pool}
}

func (repo *CapitalPolicyRepo) RegisterCapitalPolicy(
	ctx context.Context,
	artifact *capital.PolicyArtifact,
) (*capital.PolicyArtifact, error) {
	if artifact == nil {
		return nil, fmt.Errorf("postgres: register capital policy: artifact is required")
	}
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: register capital policy: repository pool is required")
	}
	if err := artifact.Validate(); err != nil {
		return nil, repo.artifactValidationError(ctx, artifact, err)
	}
	persisted, err := scanCapitalPolicyArtifact(repo.pool.QueryRow(ctx, `INSERT INTO capital_margin_policy_artifacts (
		id, schema_name, policy_version, sha256, canonical_bytes, canonical_json, created_at
	) VALUES ($1,$2,$3,$4,$5,convert_from($5,'UTF8')::JSONB,$6)
	ON CONFLICT (policy_version) DO NOTHING
	RETURNING id, schema_name, policy_version, sha256, canonical_bytes, created_at`,
		artifact.ID, artifact.Schema, artifact.Version, artifact.SHA256, []byte(artifact.CanonicalBytes), artifact.CreatedAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := repo.GetCapitalPolicyByVersion(ctx, artifact.Version)
		if loadErr != nil {
			return nil, fmt.Errorf("postgres: load replayed capital policy %q: %w", artifact.Version, loadErr)
		}
		if !capital.SamePolicyArtifactPayload(existing, artifact) {
			return nil, fmt.Errorf("postgres: capital policy version %q reused with changed payload: %w", artifact.Version, repository.ErrIdempotencyConflict)
		}
		return existing, nil
	}
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return nil, fmt.Errorf("postgres: capital policy identity conflicts: %v: %w", err, repository.ErrIdempotencyConflict)
		}
		return nil, fmt.Errorf("postgres: insert capital policy %q: %w", artifact.Version, err)
	}
	if _, err := capital.PolicyFromArtifact(*persisted); err != nil {
		return nil, fmt.Errorf("postgres: validate inserted capital policy %q: %w", artifact.Version, err)
	}
	return persisted, nil
}

func (repo *CapitalPolicyRepo) GetCapitalPolicyByVersion(
	ctx context.Context,
	version string,
) (*capital.PolicyArtifact, error) {
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: get capital policy: repository pool is required")
	}
	if version == "" || version != strings.TrimSpace(version) || len(version) > 256 {
		return nil, fmt.Errorf("postgres: get capital policy: canonical version is required")
	}
	artifact, err := scanCapitalPolicyArtifact(repo.pool.QueryRow(ctx, `SELECT
		id, schema_name, policy_version, sha256, canonical_bytes, created_at
	FROM capital_margin_policy_artifacts WHERE policy_version = $1`, version))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get capital policy %q: %w", version, err)
	}
	if _, err := capital.PolicyFromArtifact(*artifact); err != nil {
		return nil, fmt.Errorf("postgres: validate loaded capital policy %q: %w", version, err)
	}
	return artifact, nil
}

func (repo *CapitalPolicyRepo) BindCapitalPolicy(
	ctx context.Context,
	binding *capital.Binding,
) (*capital.Binding, error) {
	if binding == nil {
		return nil, fmt.Errorf("postgres: bind capital policy: binding is required")
	}
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: bind capital policy: repository pool is required")
	}
	account, policy, err := repo.bindingContext(ctx, binding.AccountID, binding.PolicyVersion)
	if err != nil {
		return nil, fmt.Errorf("postgres: bind capital policy context: %w", err)
	}
	if err := binding.Validate(*account, policy); err != nil {
		return nil, repo.bindingValidationError(ctx, binding, err)
	}
	persisted, err := scanCapitalBinding(repo.pool.QueryRow(ctx, `INSERT INTO account_capital_policy_bindings (
		id, account_id, policy_artifact_id, policy_version, tier, margin_profile,
		environment, starting_capital, buying_power_multiplier, evidence_class,
		storage_namespace, currency, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	ON CONFLICT (account_id) DO NOTHING
	RETURNING id, account_id, policy_artifact_id, policy_version, tier::TEXT,
		margin_profile, environment, starting_capital::TEXT, buying_power_multiplier::TEXT,
		evidence_class, storage_namespace, currency, created_at`,
		binding.ID, binding.AccountID, binding.PolicyArtifactID, binding.PolicyVersion,
		binding.Tier.StringFixed(8), binding.Profile, binding.Environment,
		binding.StartingCapital.StringFixed(8), binding.BuyingPowerMultiplier.StringFixed(8),
		binding.EvidenceClass, binding.StorageNamespace, binding.Currency, binding.CreatedAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := repo.GetCapitalBinding(ctx, binding.AccountID)
		if loadErr != nil {
			return nil, fmt.Errorf("postgres: load replayed capital binding for account %s: %w", binding.AccountID, loadErr)
		}
		if !capital.SameBindingPayload(existing, binding) {
			return nil, fmt.Errorf("postgres: capital binding for account %s reused with changed payload: %w", binding.AccountID, repository.ErrIdempotencyConflict)
		}
		return existing, nil
	}
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return nil, fmt.Errorf("postgres: capital binding identity conflicts: %v: %w", err, repository.ErrIdempotencyConflict)
		}
		return nil, fmt.Errorf("postgres: insert capital binding for account %s: %w", binding.AccountID, err)
	}
	if err := persisted.Validate(*account, policy); err != nil {
		return nil, fmt.Errorf("postgres: validate inserted capital binding %s: %w", binding.ID, err)
	}
	return persisted, nil
}

func (repo *CapitalPolicyRepo) GetCapitalBinding(
	ctx context.Context,
	accountID uuid.UUID,
) (*capital.Binding, error) {
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: get capital binding: repository pool is required")
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("postgres: get capital binding: account ID is required")
	}
	binding, err := scanCapitalBinding(repo.pool.QueryRow(ctx, `SELECT
		id, account_id, policy_artifact_id, policy_version, tier::TEXT,
		margin_profile, environment, starting_capital::TEXT, buying_power_multiplier::TEXT,
		evidence_class, storage_namespace, currency, created_at
	FROM account_capital_policy_bindings WHERE account_id = $1`, accountID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get capital binding for account %s: %w", accountID, err)
	}
	account, policy, err := repo.bindingContext(ctx, binding.AccountID, binding.PolicyVersion)
	if err != nil {
		return nil, fmt.Errorf("postgres: load capital binding context: %w", err)
	}
	if err := binding.Validate(*account, policy); err != nil {
		return nil, fmt.Errorf("postgres: validate loaded capital binding %s: %w", binding.ID, err)
	}
	return binding, nil
}

func (repo *CapitalPolicyRepo) bindingContext(
	ctx context.Context,
	accountID uuid.UUID,
	version string,
) (*domain.Account, *capital.Policy, error) {
	account, err := NewAccountRepo(repo.pool).GetByID(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	artifact, err := repo.GetCapitalPolicyByVersion(ctx, version)
	if err != nil {
		return nil, nil, err
	}
	policy, err := capital.PolicyFromArtifact(*artifact)
	if err != nil {
		return nil, nil, err
	}
	return account, policy, nil
}

func (repo *CapitalPolicyRepo) artifactValidationError(
	ctx context.Context,
	artifact *capital.PolicyArtifact,
	validationErr error,
) error {
	if artifact.Version != "" {
		existing, err := repo.GetCapitalPolicyByVersion(ctx, artifact.Version)
		if err == nil && !capital.SamePolicyArtifactPayload(existing, artifact) {
			return fmt.Errorf("postgres: capital policy version %q reused with changed payload: %v: %w", artifact.Version, validationErr, repository.ErrIdempotencyConflict)
		}
	}
	return fmt.Errorf("postgres: register capital policy: %w", validationErr)
}

func (repo *CapitalPolicyRepo) bindingValidationError(
	ctx context.Context,
	binding *capital.Binding,
	validationErr error,
) error {
	if binding.AccountID != uuid.Nil {
		existing, err := repo.GetCapitalBinding(ctx, binding.AccountID)
		if err == nil && !capital.SameBindingPayload(existing, binding) {
			return fmt.Errorf("postgres: capital binding for account %s reused with changed payload: %v: %w", binding.AccountID, validationErr, repository.ErrIdempotencyConflict)
		}
	}
	return fmt.Errorf("postgres: bind capital policy: %w", validationErr)
}

func scanCapitalPolicyArtifact(row accountRow) (*capital.PolicyArtifact, error) {
	artifact := new(capital.PolicyArtifact)
	if err := row.Scan(&artifact.ID, &artifact.Schema, &artifact.Version, &artifact.SHA256, &artifact.CanonicalBytes, &artifact.CreatedAt); err != nil {
		return nil, err
	}
	artifact.CreatedAt = artifact.CreatedAt.UTC().Truncate(time.Microsecond)
	return artifact, nil
}

func scanCapitalBinding(row accountRow) (*capital.Binding, error) {
	binding := new(capital.Binding)
	var tierText, startingText, multiplierText string
	if err := row.Scan(
		&binding.ID, &binding.AccountID, &binding.PolicyArtifactID, &binding.PolicyVersion,
		&tierText, &binding.Profile, &binding.Environment, &startingText, &multiplierText,
		&binding.EvidenceClass, &binding.StorageNamespace, &binding.Currency, &binding.CreatedAt,
	); err != nil {
		return nil, err
	}
	var err error
	if binding.Tier, err = decimal.NewFromString(tierText); err != nil {
		return nil, err
	}
	if binding.StartingCapital, err = decimal.NewFromString(startingText); err != nil {
		return nil, err
	}
	if binding.BuyingPowerMultiplier, err = decimal.NewFromString(multiplierText); err != nil {
		return nil, err
	}
	binding.CreatedAt = binding.CreatedAt.UTC().Truncate(time.Microsecond)
	return binding, nil
}
