package migrations_test

import (
	"strings"
	"testing"
)

func TestPredictionNativeSnapshotTypesMigrationAllowsRuntimeSnapshotTypes(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000105_prediction_native_snapshot_types.up.sql"))
	for _, fragment := range []string{
		"drop constraint if exists pipeline_run_snapshots_data_type_check",
		"add constraint pipeline_run_snapshots_data_type_check",
		"'polymarket_native_snapshot'",
		"'kalshi_native_snapshot'",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected migration to contain %q, got:\n%s", fragment, upSQL)
		}
	}
}

func TestPredictionNativeSnapshotTypesDownMigrationRestoresLegacyConstraint(t *testing.T) {
	downSQL := normalizeSQL(t, readMigrationFile(t, "000105_prediction_native_snapshot_types.down.sql"))
	if !strings.Contains(downSQL, "check (data_type in ('market', 'news', 'fundamentals', 'social'))") {
		t.Fatalf("expected down migration to restore legacy constraint, got:\n%s", downSQL)
	}
}
