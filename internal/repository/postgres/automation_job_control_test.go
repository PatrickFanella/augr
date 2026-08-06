package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAutomationJobControlRepoRoundTrip(t *testing.T) {
	connString := os.Getenv("DB_URL")
	if connString == "" {
		connString = os.Getenv("DATABASE_URL")
	}
	if connString == "" {
		t.Skip("skipping integration test: DB_URL or DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	t.Cleanup(pool.Close)
	repo := NewAutomationJobControlRepo(pool)
	name := "test-control-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM automation_job_controls WHERE job_name = $1`, name)
	})

	if err := repo.SetEnabled(ctx, name, false, "audit-test"); err != nil {
		t.Fatalf("SetEnabled(false): %v", err)
	}
	controls, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	found := false
	for _, control := range controls {
		if control.JobName != name {
			continue
		}
		found = true
		if control.Enabled || control.UpdatedBy != "audit-test" || control.UpdatedAt.IsZero() {
			t.Fatalf("control = %+v", control)
		}
	}
	if !found {
		t.Fatalf("control %q not listed", name)
	}

	if err := repo.SetEnabled(ctx, name, true, "audit-test-update"); err != nil {
		t.Fatalf("SetEnabled(true): %v", err)
	}
	var enabled bool
	var actor string
	if err := pool.QueryRow(ctx, `SELECT enabled, updated_by FROM automation_job_controls WHERE job_name = $1`, name).Scan(&enabled, &actor); err != nil {
		t.Fatalf("read updated control: %v", err)
	}
	if !enabled || actor != "audit-test-update" {
		t.Fatalf("updated control enabled/actor = %t/%q", enabled, actor)
	}
}
