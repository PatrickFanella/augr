package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/financialscheduler"
)

var (
	ErrFinancialLeaseNotCurrent    = errors.New("postgres: financial scheduler lease is not current")
	ErrFinancialOccurrenceConflict = errors.New("postgres: financial scheduler occurrence conflict")
	ErrFinancialEffectConflict     = errors.New("postgres: financial scheduler effect conflict")
)

const (
	minimumFinancialLease = time.Second
	maximumFinancialLease = 24 * time.Hour
)

type FinancialSchedulerRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

func NewFinancialSchedulerRepo(pool *pgxpool.Pool) *FinancialSchedulerRepo {
	return &FinancialSchedulerRepo{pool: pool}
}

func (r *FinancialSchedulerRepo) RegisterCatalog(ctx context.Context, catalog map[string]financialscheduler.JobDefinition) error {
	if r == nil || r.pool == nil {
		return fmt.Errorf("postgres: financial scheduler pool is required")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("postgres: begin financial scheduler catalog: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	keys := make([]string, 0, len(catalog))
	for key := range catalog {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		definition := catalog[key]
		classes := make([]string, len(definition.Mutations))
		for index, class := range definition.Mutations {
			classes[index] = string(class)
		}
		_, err := tx.Exec(ctx, `INSERT INTO financial_job_definitions(id,schema_name,job_key,mutation_classes,sha256,canonical_bytes,canonical_json) VALUES($1,'financial-job-definition-v1',$2,$3,$4,$5,$6) ON CONFLICT(job_key) DO NOTHING`, definition.ID, definition.Key, classes, definition.SHA256, []byte(definition.CanonicalJSON), definition.CanonicalJSON)
		if err != nil {
			return fmt.Errorf("postgres: persist financial job definition %q: %w", key, err)
		}
		var id uuid.UUID
		var sha string
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT id,sha256,canonical_bytes FROM financial_job_definitions WHERE job_key=$1`, key).Scan(&id, &sha, &raw); err != nil {
			return fmt.Errorf("postgres: read financial job definition %q: %w", key, err)
		}
		if id != definition.ID || sha != definition.SHA256 || !bytes.Equal(raw, definition.CanonicalJSON) {
			return ErrFinancialOccurrenceConflict
		}
	}
	if err := r.stage("definitions"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type leaseEventRow struct {
	Sequence   int64
	Kind       string
	OwnerID    uuid.UUID
	FenceToken int64
	OccurredAt time.Time
	ExpiresAt  *time.Time
}

func (r *FinancialSchedulerRepo) Acquire(ctx context.Context, occurrence *financialscheduler.Occurrence, ownerID uuid.UUID, ttl time.Duration) (financialscheduler.Acquisition, error) {
	if r == nil || r.pool == nil {
		return financialscheduler.Acquisition{}, fmt.Errorf("postgres: financial scheduler pool is required")
	}
	if occurrence == nil || ownerID == uuid.Nil {
		return financialscheduler.Acquisition{}, fmt.Errorf("postgres: financial scheduler occurrence and owner are required")
	}
	if err := occurrence.Validate(); err != nil {
		return financialscheduler.Acquisition{}, err
	}
	if ttl < minimumFinancialLease || ttl > maximumFinancialLease {
		return financialscheduler.Acquisition{}, fmt.Errorf("postgres: financial scheduler lease duration is out of bounds")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return financialscheduler.Acquisition{}, fmt.Errorf("postgres: begin financial scheduler acquisition: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := persistOccurrence(ctx, tx, occurrence); err != nil {
		return financialscheduler.Acquisition{}, err
	}
	if err := r.stage("occurrence"); err != nil {
		return financialscheduler.Acquisition{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM financial_job_occurrences WHERE id=$1 FOR UPDATE`, occurrence.ID); err != nil {
		return financialscheduler.Acquisition{}, fmt.Errorf("postgres: lock financial occurrence: %w", err)
	}
	previous, found, err := latestLeaseEvent(ctx, tx, occurrence.ID)
	if err != nil {
		return financialscheduler.Acquisition{}, err
	}
	var dbNow time.Time
	if err := tx.QueryRow(ctx, `SELECT date_trunc('microseconds',clock_timestamp())`).Scan(&dbNow); err != nil {
		return financialscheduler.Acquisition{}, fmt.Errorf("postgres: read financial scheduler clock: %w", err)
	}
	if found && (previous.Kind == "succeeded" || previous.Kind == "failed") {
		if err := tx.Commit(ctx); err != nil {
			return financialscheduler.Acquisition{}, err
		}
		return financialscheduler.Acquisition{Terminal: true}, nil
	}
	if found && previous.ExpiresAt != nil && previous.ExpiresAt.After(dbNow) {
		if err := tx.Commit(ctx); err != nil {
			return financialscheduler.Acquisition{}, err
		}
		return financialscheduler.Acquisition{Lease: eventLease(occurrence.ID, previous), Acquired: false}, nil
	}
	sequence, fence := int64(1), int64(1)
	if found {
		sequence, fence = previous.Sequence+1, previous.FenceToken+1
	}
	eventID := leaseEventID(occurrence.ID, sequence, "acquired", ownerID, fence)
	var acquiredAt, expiresAt time.Time
	if err := tx.QueryRow(ctx, `INSERT INTO financial_job_lease_events(id,occurrence_id,sequence,event_kind,owner_id,fence_token,occurred_at,lease_expires_at) VALUES($1,$2,$3,'acquired',$4,$5,date_trunc('microseconds',clock_timestamp()),date_trunc('microseconds',clock_timestamp())+($6*interval '1 microsecond')) RETURNING occurred_at,lease_expires_at`, eventID, occurrence.ID, sequence, ownerID, fence, ttl.Microseconds()).Scan(&acquiredAt, &expiresAt); err != nil {
		return financialscheduler.Acquisition{}, fmt.Errorf("postgres: acquire financial scheduler lease: %w", err)
	}
	if err := r.stage("lease_event"); err != nil {
		return financialscheduler.Acquisition{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return financialscheduler.Acquisition{}, fmt.Errorf("postgres: commit financial scheduler acquisition: %w", err)
	}
	return financialscheduler.Acquisition{Lease: financialscheduler.Lease{OccurrenceID: occurrence.ID, OwnerID: ownerID, FenceToken: fence, Sequence: sequence, AcquiredAt: acquiredAt, ExpiresAt: expiresAt}, Acquired: true}, nil
}

func (r *FinancialSchedulerRepo) Renew(ctx context.Context, lease financialscheduler.Lease, ttl time.Duration) (financialscheduler.Lease, error) {
	if ttl < minimumFinancialLease || ttl > maximumFinancialLease {
		return financialscheduler.Lease{}, fmt.Errorf("postgres: financial scheduler lease duration is out of bounds")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return financialscheduler.Lease{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM financial_job_occurrences WHERE id=$1 FOR UPDATE`, lease.OccurrenceID); err != nil {
		return financialscheduler.Lease{}, err
	}
	previous, found, err := latestLeaseEvent(ctx, tx, lease.OccurrenceID)
	if err != nil {
		return financialscheduler.Lease{}, err
	}
	if !found || previous.OwnerID != lease.OwnerID || previous.FenceToken != lease.FenceToken || previous.Sequence != lease.Sequence || (previous.Kind != "acquired" && previous.Kind != "renewed") {
		return financialscheduler.Lease{}, ErrFinancialLeaseNotCurrent
	}
	sequence := previous.Sequence + 1
	eventID := leaseEventID(lease.OccurrenceID, sequence, "renewed", lease.OwnerID, lease.FenceToken)
	var occurredAt, expiresAt time.Time
	err = tx.QueryRow(ctx, `INSERT INTO financial_job_lease_events(id,occurrence_id,sequence,event_kind,owner_id,fence_token,occurred_at,lease_expires_at) VALUES($1,$2,$3,'renewed',$4,$5,date_trunc('microseconds',clock_timestamp()),date_trunc('microseconds',clock_timestamp())+($6*interval '1 microsecond')) RETURNING occurred_at,lease_expires_at`, eventID, lease.OccurrenceID, sequence, lease.OwnerID, lease.FenceToken, ttl.Microseconds()).Scan(&occurredAt, &expiresAt)
	if err != nil {
		return financialscheduler.Lease{}, fmt.Errorf("postgres: renew financial scheduler lease: %w", err)
	}
	if err := r.stage("lease_event"); err != nil {
		return financialscheduler.Lease{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return financialscheduler.Lease{}, err
	}
	return financialscheduler.Lease{OccurrenceID: lease.OccurrenceID, OwnerID: lease.OwnerID, FenceToken: lease.FenceToken, Sequence: sequence, AcquiredAt: occurredAt, ExpiresAt: expiresAt}, nil
}

func (r *FinancialSchedulerRepo) ClaimEffect(ctx context.Context, lease financialscheduler.Lease, effect *financialscheduler.Effect) (*financialscheduler.Effect, error) {
	if effect == nil || effect.OccurrenceID != lease.OccurrenceID {
		return nil, fmt.Errorf("postgres: financial scheduler effect scope mismatch")
	}
	if err := effect.Validate(); err != nil {
		return nil, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM financial_job_occurrences WHERE id=$1 FOR UPDATE`, lease.OccurrenceID); err != nil {
		return nil, err
	}
	current, found, err := latestLeaseEvent(ctx, tx, lease.OccurrenceID)
	if err != nil {
		return nil, err
	}
	var dbNow time.Time
	if err := tx.QueryRow(ctx, `SELECT date_trunc('microseconds',clock_timestamp())`).Scan(&dbNow); err != nil {
		return nil, err
	}
	if !found || current.OwnerID != lease.OwnerID || current.FenceToken != lease.FenceToken || current.Sequence != lease.Sequence || current.ExpiresAt == nil || !current.ExpiresAt.After(dbNow) || (current.Kind != "acquired" && current.Kind != "renewed") {
		return nil, ErrFinancialLeaseNotCurrent
	}
	var existingSHA, existingPayload string
	var existingBytes []byte
	err = tx.QueryRow(ctx, `SELECT sha256,payload_sha256,canonical_bytes FROM financial_job_effect_claims WHERE id=$1`, effect.ID).Scan(&existingSHA, &existingPayload, &existingBytes)
	if err == nil {
		if existingSHA != effect.SHA256 || existingPayload != effect.PayloadSHA256 || !bytes.Equal(existingBytes, effect.CanonicalJSON) {
			return nil, ErrFinancialEffectConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		copy := *effect
		return &copy, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("postgres: read financial scheduler effect: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO financial_job_effect_claims(id,schema_name,occurrence_id,effect_kind,business_key,payload_sha256,owner_id,fence_token,sha256,canonical_bytes,canonical_json,claimed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,date_trunc('microseconds',clock_timestamp()))`, effect.ID, financialscheduler.EffectSchemaV1, effect.OccurrenceID, effect.Kind, effect.BusinessKey, effect.PayloadSHA256, lease.OwnerID, lease.FenceToken, effect.SHA256, []byte(effect.CanonicalJSON), effect.CanonicalJSON)
	if err != nil {
		return nil, fmt.Errorf("postgres: claim financial scheduler effect: %w", err)
	}
	if err := r.stage("effect_claim"); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	copy := *effect
	return &copy, nil
}

func (r *FinancialSchedulerRepo) Complete(ctx context.Context, lease financialscheduler.Lease, succeeded bool, outcomeSHA256 string) error {
	if len(outcomeSHA256) != 64 {
		return fmt.Errorf("postgres: financial scheduler outcome sha256 is invalid")
	}
	kind := "failed"
	if succeeded {
		kind = "succeeded"
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM financial_job_occurrences WHERE id=$1 FOR UPDATE`, lease.OccurrenceID); err != nil {
		return err
	}
	current, found, err := latestLeaseEvent(ctx, tx, lease.OccurrenceID)
	if err != nil {
		return err
	}
	if !found || current.OwnerID != lease.OwnerID || current.FenceToken != lease.FenceToken || current.Sequence != lease.Sequence || (current.Kind != "acquired" && current.Kind != "renewed") {
		return ErrFinancialLeaseNotCurrent
	}
	sequence := current.Sequence + 1
	id := leaseEventID(lease.OccurrenceID, sequence, kind, lease.OwnerID, lease.FenceToken)
	if _, err := tx.Exec(ctx, `INSERT INTO financial_job_lease_events(id,occurrence_id,sequence,event_kind,owner_id,fence_token,occurred_at,outcome_sha256) VALUES($1,$2,$3,$4,$5,$6,date_trunc('microseconds',clock_timestamp()),$7)`, id, lease.OccurrenceID, sequence, kind, lease.OwnerID, lease.FenceToken, outcomeSHA256); err != nil {
		return fmt.Errorf("postgres: complete financial scheduler occurrence: %w", err)
	}
	if err := r.stage("terminal_event"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *FinancialSchedulerRepo) stage(name string) error {
	if r.afterStage == nil {
		return nil
	}
	return r.afterStage(name)
}

func persistOccurrence(ctx context.Context, tx pgx.Tx, occurrence *financialscheduler.Occurrence) error {
	_, err := tx.Exec(ctx, `INSERT INTO financial_job_occurrences(id,schema_name,job_key,schedule_revision,trigger_kind,due_at,manual_request_id,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO NOTHING`, occurrence.ID, financialscheduler.OccurrenceSchemaV1, occurrence.JobKey, occurrence.ScheduleRevision, occurrence.Trigger, occurrence.DueAt, financialNullableUUID(occurrence.ManualRequestID), occurrence.SHA256, []byte(occurrence.CanonicalJSON), occurrence.CanonicalJSON)
	if err != nil {
		return fmt.Errorf("postgres: persist financial occurrence: %w", err)
	}
	var sha string
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM financial_job_occurrences WHERE id=$1`, occurrence.ID).Scan(&sha, &raw); err != nil {
		return err
	}
	if sha != occurrence.SHA256 || !bytes.Equal(raw, occurrence.CanonicalJSON) {
		return ErrFinancialOccurrenceConflict
	}
	return nil
}

func latestLeaseEvent(ctx context.Context, tx pgx.Tx, occurrenceID uuid.UUID) (leaseEventRow, bool, error) {
	var row leaseEventRow
	err := tx.QueryRow(ctx, `SELECT sequence,event_kind,owner_id,fence_token,occurred_at,lease_expires_at FROM financial_job_lease_events WHERE occurrence_id=$1 ORDER BY sequence DESC LIMIT 1`, occurrenceID).Scan(&row.Sequence, &row.Kind, &row.OwnerID, &row.FenceToken, &row.OccurredAt, &row.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return leaseEventRow{}, false, nil
	}
	if err != nil {
		return leaseEventRow{}, false, fmt.Errorf("postgres: read financial scheduler lease: %w", err)
	}
	return row, true, nil
}

func leaseEventID(occurrenceID uuid.UUID, sequence int64, kind string, ownerID uuid.UUID, fence int64) uuid.UUID {
	return economicid.DeterministicUUID("financial-job-lease-event", occurrenceID.String(), fmt.Sprint(sequence), kind, ownerID.String(), fmt.Sprint(fence))
}
func eventLease(occurrenceID uuid.UUID, event leaseEventRow) financialscheduler.Lease {
	lease := financialscheduler.Lease{OccurrenceID: occurrenceID, OwnerID: event.OwnerID, FenceToken: event.FenceToken, Sequence: event.Sequence, AcquiredAt: event.OccurredAt}
	if event.ExpiresAt != nil {
		lease.ExpiresAt = *event.ExpiresAt
	}
	return lease
}
func financialNullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}
