package migrations_test

import (
	"strings"
	"testing"
)

func TestAutomationJobControlsMigrationDefinesDurableOverrideContract(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000061_automation_job_controls.up.sql"))
	for _, fragment := range []string{
		"create table if not exists automation_job_controls",
		"job_name text primary key",
		"enabled boolean not null",
		"updated_by text not null",
		"updated_at timestamptz not null default now()",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected up migration to contain %q, got:\n%s", fragment, upSQL)
		}
	}

	downSQL := normalizeSQL(t, readMigrationFile(t, "000061_automation_job_controls.down.sql"))
	if !strings.Contains(downSQL, "drop table if exists automation_job_controls") {
		t.Fatalf("unexpected down migration:\n%s", downSQL)
	}
}
