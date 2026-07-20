package migrations_test

import (
	"strings"
	"testing"
)

func TestFinancialLifecycleAtomicityMigrationIncludesExpectedConstraints(t *testing.T) {
	t.Parallel()

	upSQL := normalizeSQL(t, readMigrationFile(t, "000055_financial_lifecycle_atomicity.up.sql"))
	for _, fragment := range []string{
		"create table if not exists financial_fill_idempotency",
		"create table if not exists prediction_settlement_idempotency",
		"decision_id uuid not null unique references trade_decisions(id) on delete cascade",
		"payout numeric(20,8) not null",
		"resolved_at timestamptz not null",
		"raise exception 'migration 000055 aborted: duplicate non-null external_id values exist in trades'",
		"create unique index if not exists idx_trades_external_id_unique",
		"where external_id is not null",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected up migration to contain %q, got:\n%s", fragment, upSQL)
		}
	}
}

func TestFinancialLifecycleAtomicityMigrationDownDropsInReverseOrder(t *testing.T) {
	t.Parallel()

	downSQL := normalizeSQL(t, readMigrationFile(t, "000055_financial_lifecycle_atomicity.down.sql"))
	for _, fragment := range []string{
		"drop index if exists idx_trades_external_id_unique",
		"drop table if exists prediction_settlement_idempotency",
		"drop table if exists financial_fill_idempotency",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("expected down migration to contain %q, got:\n%s", fragment, downSQL)
		}
	}
}
