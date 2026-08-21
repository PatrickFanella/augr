package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
)

// SimulationPolicyRepo persists immutable content-addressed simulation policy
// artifacts. It does not select a current policy or activate an order writer.
type SimulationPolicyRepo struct{ pool *pgxpool.Pool }

var _ repository.SimulationPolicyRepository = (*SimulationPolicyRepo)(nil)

// NewSimulationPolicyRepo returns a simulation-policy repository backed by
// pool.
func NewSimulationPolicyRepo(pool *pgxpool.Pool) *SimulationPolicyRepo {
	return &SimulationPolicyRepo{pool: pool}
}

// RegisterSimulationPolicy inserts one exact artifact or returns the already
// persisted artifact when an identical registration is replayed.
func (repo *SimulationPolicyRepo) RegisterSimulationPolicy(
	ctx context.Context,
	artifact *simulation.PolicyArtifact,
) (*simulation.PolicyArtifact, error) {
	if artifact == nil {
		return nil, fmt.Errorf("postgres: register simulation policy: artifact is required")
	}
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: register simulation policy: repository pool is required")
	}
	if err := artifact.Validate(); err != nil {
		return nil, repo.registrationValidationError(ctx, artifact, err)
	}

	persisted, err := scanSimulationPolicyArtifact(repo.pool.QueryRow(ctx, `INSERT INTO simulation_policy_artifacts (
		id, schema_name, policy_version, sha256, canonical_bytes, canonical_json, created_at
	) VALUES ($1,$2,$3,$4,$5,convert_from($5,'UTF8')::JSONB,$6)
	ON CONFLICT (policy_version) DO NOTHING
	RETURNING id, schema_name, policy_version, sha256, canonical_bytes, created_at`,
		artifact.ID,
		artifact.Schema,
		artifact.Version,
		artifact.SHA256,
		[]byte(artifact.CanonicalBytes),
		artifact.CreatedAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := repo.GetSimulationPolicyByVersion(ctx, artifact.Version)
		if loadErr != nil {
			return nil, fmt.Errorf("postgres: load replayed simulation policy %q: %w", artifact.Version, loadErr)
		}
		if !simulation.SamePolicyArtifactPayload(existing, artifact) {
			return nil, fmt.Errorf(
				"postgres: simulation policy version %q reused with changed payload: %w",
				artifact.Version,
				repository.ErrIdempotencyConflict,
			)
		}
		return existing, nil
	}
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return nil, fmt.Errorf(
				"postgres: simulation policy identity conflicts with an existing artifact: %v: %w",
				err,
				repository.ErrIdempotencyConflict,
			)
		}
		return nil, fmt.Errorf("postgres: insert simulation policy %q: %w", artifact.Version, err)
	}
	if err := persisted.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: validate inserted simulation policy %q: %w", artifact.Version, err)
	}
	return persisted, nil
}

// GetSimulationPolicyByVersion reloads the exact content-addressed bytes used
// by a routed order.
func (repo *SimulationPolicyRepo) GetSimulationPolicyByVersion(
	ctx context.Context,
	version string,
) (*simulation.PolicyArtifact, error) {
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: get simulation policy: repository pool is required")
	}
	if version == "" || version != strings.TrimSpace(version) || len(version) > 256 {
		return nil, fmt.Errorf("postgres: get simulation policy: canonical version is required")
	}
	artifact, err := scanSimulationPolicyArtifact(repo.pool.QueryRow(ctx, `SELECT
		id, schema_name, policy_version, sha256, canonical_bytes, created_at
	FROM simulation_policy_artifacts
	WHERE policy_version = $1`, version))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get simulation policy %q: %w", version, err)
	}
	if err := artifact.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: validate loaded simulation policy %q: %w", version, err)
	}
	return artifact, nil
}

func (repo *SimulationPolicyRepo) registrationValidationError(
	ctx context.Context,
	artifact *simulation.PolicyArtifact,
	validationErr error,
) error {
	if artifact.Version != "" {
		existing, err := repo.GetSimulationPolicyByVersion(ctx, artifact.Version)
		if err == nil && !simulation.SamePolicyArtifactPayload(existing, artifact) {
			return fmt.Errorf(
				"postgres: simulation policy version %q reused with changed payload: %v: %w",
				artifact.Version,
				validationErr,
				repository.ErrIdempotencyConflict,
			)
		}
	}
	return fmt.Errorf("postgres: register simulation policy: %w", validationErr)
}

func scanSimulationPolicyArtifact(row accountRow) (*simulation.PolicyArtifact, error) {
	artifact := new(simulation.PolicyArtifact)
	if err := row.Scan(
		&artifact.ID,
		&artifact.Schema,
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
