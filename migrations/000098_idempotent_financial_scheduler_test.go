package migrations_test

import (
	"strings"
	"testing"
)

func TestIdempotentFinancialSchedulerMigration(t *testing.T) {
	up := normalizeSQL(t, readMigrationFile(t, "000098_idempotent_financial_scheduler.up.sql"))
	for _, required := range []string{
		"create table financial_job_definitions", "create table financial_job_occurrences", "create table financial_job_lease_events",
		"create table financial_job_effect_claims", "validate_financial_job_lease_event",
		"validate_financial_job_effect_claim", "economic_deterministic_uuid('financial-job-occurrence'",
		"economic_deterministic_uuid('financial-job-effect'", "financial scheduler evidence is append-only",
	} {
		if !strings.Contains(up, required) {
			t.Fatalf("migration 98 lacks %q", required)
		}
	}
	down := normalizeSQL(t, readMigrationFile(t, "000098_idempotent_financial_scheduler.down.sql"))
	if !strings.Contains(down, "migration 98 rollback refused") {
		t.Fatal("migration 98 rollback is not empty-only")
	}
}
