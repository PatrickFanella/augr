package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestKalshiSnapshotHypertableMigration(t *testing.T) {
	up, err := os.ReadFile("000063_kalshi_snapshot_hypertable.up.sql")
	if err != nil {
		t.Fatalf("ReadFile(up): %v", err)
	}
	sql := strings.ToLower(string(up))

	for _, required := range []string{
		"create table kalshi_market_snapshots_partitioned",
		"primary key (id, captured_at)",
		"create_hypertable(",
		"by_range('captured_at', interval '1 day')",
		"ticker, captured_at desc",
		"environment in ('demo', 'live', 'unknown')",
		"timescaledb.compress_segmentby = 'provider,environment,ticker'",
		"timescaledb.compress_orderby = 'captured_at desc, id'",
		"compress_after => interval '7 days'",
		"drop_after => interval '30 days'",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("up migration missing %q", required)
		}
	}

	for _, forbidden := range []string{
		"drop table kalshi_market_snapshots;",
		"delete from kalshi_market_snapshots",
	} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("up migration contains unsafe cutover operation %q", forbidden)
		}
	}
}

func TestKalshiSnapshotHypertableDownFailsSafeAfterCutover(t *testing.T) {
	down, err := os.ReadFile("000063_kalshi_snapshot_hypertable.down.sql")
	if err != nil {
		t.Fatalf("ReadFile(down): %v", err)
	}
	sql := strings.ToLower(string(down))
	if !strings.Contains(sql, "cannot be reversed automatically after cutover") {
		t.Fatal("down migration must fail safely after the staging table has been renamed")
	}
	if !strings.Contains(sql, "drop table kalshi_market_snapshots_partitioned") {
		t.Fatal("down migration must remove the uncut staging hypertable")
	}
}

func TestKalshiSnapshotArchiveAndRestoreScriptsFailClosed(t *testing.T) {
	archiveBytes, err := os.ReadFile("../scripts/archive-kalshi-snapshots.sh")
	if err != nil {
		t.Fatalf("ReadFile(archive): %v", err)
	}
	archive := strings.ToLower(string(archiveBytes))
	for _, required := range []string{
		"format csv",
		"sha256sum",
		"manifest.csv.tmp",
		"dd of=",
		"captured_at >=",
		"captured_at <",
	} {
		if !strings.Contains(archive, required) {
			t.Errorf("archive script missing %q", required)
		}
	}
	if strings.Contains(archive, "order by captured_at") {
		t.Fatal("archive script must not sort the large source table")
	}

	restoreBytes, err := os.ReadFile("../scripts/restore-kalshi-snapshots.sh")
	if err != nil {
		t.Fatalf("ReadFile(restore): %v", err)
	}
	restore := strings.ToLower(string(restoreBytes))
	for _, required := range []string{
		"refusing restore while",
		"sha256sum",
		"target day",
		"row parity failed",
		"from stdin with (format csv)",
		"compress_chunk",
		"timescaledb_information.hypertables",
	} {
		if !strings.Contains(restore, required) {
			t.Errorf("restore script missing %q", required)
		}
	}
}
