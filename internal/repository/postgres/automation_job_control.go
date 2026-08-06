package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// AutomationJobControlRepo persists explicit operator enable/disable choices.
type AutomationJobControlRepo struct {
	pool *pgxpool.Pool
}

var _ repository.AutomationJobControlRepository = (*AutomationJobControlRepo)(nil)

func NewAutomationJobControlRepo(pool *pgxpool.Pool) *AutomationJobControlRepo {
	return &AutomationJobControlRepo{pool: pool}
}

func (r *AutomationJobControlRepo) List(ctx context.Context) ([]domain.AutomationJobControl, error) {
	rows, err := r.pool.Query(ctx, `SELECT job_name, enabled, updated_by, updated_at FROM automation_job_controls ORDER BY job_name`)
	if err != nil {
		return nil, fmt.Errorf("postgres: list automation job controls: %w", err)
	}
	defer rows.Close()

	var controls []domain.AutomationJobControl
	for rows.Next() {
		var control domain.AutomationJobControl
		if err := rows.Scan(&control.JobName, &control.Enabled, &control.UpdatedBy, &control.UpdatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan automation job control: %w", err)
		}
		controls = append(controls, control)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate automation job controls: %w", err)
	}
	return controls, nil
}

func (r *AutomationJobControlRepo) SetEnabled(ctx context.Context, name string, enabled bool, actor string) error {
	name = strings.TrimSpace(name)
	actor = strings.TrimSpace(actor)
	if name == "" {
		return fmt.Errorf("postgres: set automation job control: job name is required")
	}
	if actor == "" {
		actor = "unknown"
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO automation_job_controls (job_name, enabled, updated_by, updated_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (job_name) DO UPDATE
		 SET enabled = EXCLUDED.enabled, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		name, enabled, actor,
	)
	if err != nil {
		return fmt.Errorf("postgres: set automation job control: %w", err)
	}
	return nil
}
