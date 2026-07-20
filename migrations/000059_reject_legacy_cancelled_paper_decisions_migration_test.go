package migrations_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRejectLegacyCancelledPaperDecisionsUpMigrationDefinesExpectedContract(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000059_reject_legacy_cancelled_paper_decisions.up.sql"))
	for _, fragment := range []string{
		"do $$ declare",
		"migration 000059 aborted: duplicate candidate decisions per order are not allowed",
		"migration 000059 aborted: audited target orders must be fill-free and trade-free",
		"migration 000059 aborted: audited target count, candidate count, and distinct paper_order_id count must match",
		"o.market_type = 'kalshi'",
		"o.broker = 'paper'",
		"o.order_type = 'limit'",
		"reason_added",
		"legacy_paper_decision_rejected",
		"migration:000059",
		"al.details ->> 'reason' = 'missing_historical_reference_price'",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected up migration to contain %q, got:\n%s", fragment, upSQL)
		}
	}
}

func TestRejectLegacyCancelledPaperDecisionsDownMigrationRevertsMigrationOwnedRows(t *testing.T) {
	downSQL := normalizeSQL(t, readMigrationFile(t, "000059_reject_legacy_cancelled_paper_decisions.down.sql"))
	for _, fragment := range []string{
		"al.event_type = 'legacy_paper_decision_rejected'",
		"al.actor = 'migration:000059'",
		"reason_added",
		"case when rd.reason_added then array_remove(td.risk_reasons, 'legacy_paper_order_cancelled') else td.risk_reasons end",
		"delete from audit_log al",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("expected down migration to contain %q, got:\n%s", fragment, downSQL)
		}
	}
}

func TestRejectLegacyCancelledPaperDecisionsMigrationAppliesAgainstCurrentSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping migration integration test in short mode")
	}
	databaseURL := os.Getenv("DB_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("skipping migration integration test: DB_URL or DATABASE_URL is not set")
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("failed to create admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)
	if _, err := adminPool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		t.Fatalf("failed to ensure pgcrypto extension: %v", err)
	}

	runCase := func(t *testing.T, seed func(context.Context, *pgxpool.Pool), expectUpErr bool, verify func(context.Context, *pgxpool.Pool)) {
		t.Helper()
		schemaName := "migr_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		sanitizedSchemaName := pgx.Identifier{schemaName}.Sanitize()
		if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+sanitizedSchemaName); err != nil {
			t.Fatalf("failed to create schema: %v", err)
		}
		t.Cleanup(func() { _, _ = adminPool.Exec(ctx, `DROP SCHEMA IF EXISTS `+sanitizedSchemaName+` CASCADE`) })
		config, err := pgxpool.ParseConfig(databaseURL)
		if err != nil {
			t.Fatalf("failed to parse db config: %v", err)
		}
		config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"
		config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatalf("failed to create schema pool: %v", err)
		}
		t.Cleanup(pool.Close)
		for _, filename := range sortedUpMigrationsThrough(t, "000058_position_provenance.up.sql") {
			if _, err := pool.Exec(ctx, readMigrationFile(t, filename)); err != nil {
				t.Fatalf("failed to apply %s: %v", filename, err)
			}
		}
		seed(ctx, pool)
		_, err = pool.Exec(ctx, readMigrationFile(t, "000059_reject_legacy_cancelled_paper_decisions.up.sql"))
		if expectUpErr {
			if err == nil {
				t.Fatal("expected up migration to fail")
			}
			return
		}
		if err != nil {
			t.Fatalf("failed to apply up migration: %v", err)
		}
		verify(ctx, pool)
	}

	assertNoAudit := func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, decisionID uuid.UUID) {
		t.Helper()
		var count int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_log WHERE entity_id = $1 AND event_type = 'legacy_paper_decision_rejected'`, decisionID).Scan(&count); err != nil {
			t.Fatalf("failed to query migration audit rows: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected no migration audit rows, got %d", count)
		}
	}

	t.Run("valid strict candidate", func(t *testing.T) {
		runCase(t, func(ctx context.Context, pool *pgxpool.Pool) {
			execMust(t, ctx, pool, `INSERT INTO orders (id, market_type, broker, ticker, side, order_type, quantity, filled_quantity, filled_avg_price, filled_at, status) VALUES ($1,'kalshi','paper',$2,'buy','limit',10,0,NULL,NULL,'cancelled')`, orderID(1), "T1")
			execMust(t, ctx, pool, `INSERT INTO trade_decisions (id, market_type, instrument_key, side, risk_status, risk_reasons, paper_order_id, status) VALUES ($1,'kalshi',$2,'buy','approved',ARRAY[]::text[],$3,'paper_ordered')`, decisionID(1), "k1", orderID(1))
			execMust(t, ctx, pool, `INSERT INTO audit_log (event_type, entity_type, entity_id, actor, details) VALUES ('legacy_paper_order_cancelled','order',$1,'operator:p0_backfill','{"reason":"missing_historical_reference_price"}')`, orderID(1))
		}, false, func(ctx context.Context, pool *pgxpool.Pool) {
			assertDecisionState(t, ctx, pool, decisionID(1), "rejected", []string{"legacy_paper_order_cancelled"})
			assertAuditDetails(t, ctx, pool, decisionID(1), true)
			if _, err := pool.Exec(ctx, readMigrationFile(t, "000059_reject_legacy_cancelled_paper_decisions.down.sql")); err != nil {
				t.Fatalf("failed to apply down migration: %v", err)
			}
			assertDecisionState(t, ctx, pool, decisionID(1), "paper_ordered", []string{})
		})
	})

	t.Run("preexisting reason candidate", func(t *testing.T) {
		runCase(t, func(ctx context.Context, pool *pgxpool.Pool) {
			execMust(t, ctx, pool, `INSERT INTO orders (id, market_type, broker, ticker, side, order_type, quantity, filled_quantity, filled_avg_price, filled_at, status) VALUES ($1,'kalshi','paper',$2,'buy','limit',10,0,NULL,NULL,'cancelled')`, orderID(2), "T2")
			execMust(t, ctx, pool, `INSERT INTO trade_decisions (id, market_type, instrument_key, side, risk_status, risk_reasons, paper_order_id, status) VALUES ($1,'kalshi',$2,'buy','approved',ARRAY['legacy_paper_order_cancelled']::text[],$3,'paper_ordered')`, decisionID(2), "k2", orderID(2))
			execMust(t, ctx, pool, `INSERT INTO audit_log (event_type, entity_type, entity_id, actor, details) VALUES ('legacy_paper_order_cancelled','order',$1,'operator:p0_backfill','{"reason":"missing_historical_reference_price"}')`, orderID(2))
		}, false, func(ctx context.Context, pool *pgxpool.Pool) {
			assertDecisionState(t, ctx, pool, decisionID(2), "rejected", []string{"legacy_paper_order_cancelled"})
			assertAuditDetails(t, ctx, pool, decisionID(2), false)
			if _, err := pool.Exec(ctx, readMigrationFile(t, "000059_reject_legacy_cancelled_paper_decisions.down.sql")); err != nil {
				t.Fatalf("failed to apply down migration: %v", err)
			}
			assertDecisionState(t, ctx, pool, decisionID(2), "paper_ordered", []string{"legacy_paper_order_cancelled"})
		})
	})

	t.Run("filled holdings remain untouched", func(t *testing.T) {
		runCase(t, func(ctx context.Context, pool *pgxpool.Pool) {
			for i := 3; i <= 5; i++ {
				execMust(t, ctx, pool, `INSERT INTO orders (id, market_type, broker, order_type, ticker, side, quantity, filled_quantity, filled_avg_price, filled_at, status) VALUES ($1,'kalshi','paper','limit',$2,'buy',10,10,1,NOW(),'filled')`, orderID(i), fmt.Sprintf("T%d", i))
				execMust(t, ctx, pool, `INSERT INTO trade_decisions (id, market_type, instrument_key, side, risk_status, risk_reasons, paper_order_id, status) VALUES ($1,'kalshi',$2,'buy','approved',ARRAY[]::text[],$3,'paper_ordered')`, decisionID(i), fmt.Sprintf("k%d", i), orderID(i))
			}
		}, false, func(ctx context.Context, pool *pgxpool.Pool) {
			for i := 3; i <= 5; i++ {
				assertDecisionState(t, ctx, pool, decisionID(i), "paper_ordered", []string{})
				assertNoAudit(t, ctx, pool, decisionID(i))
			}
		})
	})

	t.Run("unaudited canceled untouched", func(t *testing.T) {
		runCase(t, func(ctx context.Context, pool *pgxpool.Pool) {
			execMust(t, ctx, pool, `INSERT INTO orders (id, market_type, broker, order_type, ticker, side, quantity, filled_quantity, filled_avg_price, filled_at, status) VALUES ($1,'kalshi','paper','limit',$2,'buy',10,0,NULL,NULL,'cancelled')`, orderID(6), "T6")
			execMust(t, ctx, pool, `INSERT INTO trade_decisions (id, market_type, instrument_key, side, risk_status, risk_reasons, paper_order_id, status) VALUES ($1,'kalshi',$2,'buy','approved',ARRAY[]::text[],$3,'paper_ordered')`, decisionID(6), "k6", orderID(6))
		}, false, func(ctx context.Context, pool *pgxpool.Pool) {
			assertDecisionState(t, ctx, pool, decisionID(6), "paper_ordered", []string{})
			assertNoAudit(t, ctx, pool, decisionID(6))
		})
	})

	t.Run("nonmatching targets untouched", func(t *testing.T) {
		runCase(t, func(ctx context.Context, pool *pgxpool.Pool) {
			execMust(t, ctx, pool, `INSERT INTO orders (id, market_type, broker, order_type, ticker, side, quantity, filled_quantity, filled_avg_price, filled_at, status) VALUES ($1,'polymarket','paper','limit',$2,'buy',10,0,NULL,NULL,'cancelled')`, orderID(7), "T7")
			execMust(t, ctx, pool, `INSERT INTO trade_decisions (id, market_type, instrument_key, side, risk_status, risk_reasons, paper_order_id, status) VALUES ($1,'kalshi',$2,'buy','approved',ARRAY[]::text[],$3,'paper_ordered')`, decisionID(7), "k7", orderID(7))

			execMust(t, ctx, pool, `INSERT INTO orders (id, market_type, broker, order_type, ticker, side, quantity, filled_quantity, filled_avg_price, filled_at, status) VALUES ($1,'kalshi','alpaca','limit',$2,'buy',10,0,NULL,NULL,'cancelled')`, orderID(8), "T8")
			execMust(t, ctx, pool, `INSERT INTO trade_decisions (id, market_type, instrument_key, side, risk_status, risk_reasons, paper_order_id, status) VALUES ($1,'kalshi',$2,'buy','approved',ARRAY[]::text[],$3,'paper_ordered')`, decisionID(8), "k8", orderID(8))

			execMust(t, ctx, pool, `INSERT INTO orders (id, market_type, broker, order_type, ticker, side, quantity, filled_quantity, filled_avg_price, filled_at, status) VALUES ($1,'kalshi','paper','market',$2,'buy',10,0,NULL,NULL,'cancelled')`, orderID(9), "T9")
			execMust(t, ctx, pool, `INSERT INTO trade_decisions (id, market_type, instrument_key, side, risk_status, risk_reasons, paper_order_id, status) VALUES ($1,'kalshi',$2,'buy','approved',ARRAY[]::text[],$3,'paper_ordered')`, decisionID(9), "k9", orderID(9))

		}, false, func(ctx context.Context, pool *pgxpool.Pool) {
			for i := 7; i <= 9; i++ {
				assertDecisionState(t, ctx, pool, decisionID(i), "paper_ordered", []string{})
				assertNoAudit(t, ctx, pool, decisionID(i))
			}
		})
	})

	t.Run("duplicate decision order aborts up", func(t *testing.T) {
		runCase(t, func(ctx context.Context, pool *pgxpool.Pool) {
			execMust(t, ctx, pool, `INSERT INTO orders (id, market_type, broker, order_type, ticker, side, quantity, filled_quantity, filled_avg_price, filled_at, status) VALUES ($1,'kalshi','paper','limit',$2,'buy',10,0,NULL,NULL,'cancelled')`, orderID(10), "T10")
			execMust(t, ctx, pool, `INSERT INTO trade_decisions (id, market_type, instrument_key, side, risk_status, risk_reasons, paper_order_id, status) VALUES ($1,'kalshi',$2,'buy','approved',ARRAY[]::text[],$3,'paper_ordered')`, decisionID(10), "k10a", orderID(10))
			execMust(t, ctx, pool, `INSERT INTO trade_decisions (id, market_type, instrument_key, side, risk_status, risk_reasons, paper_order_id, status) VALUES ($1,'kalshi',$2,'buy','approved',ARRAY[]::text[],$3,'paper_ordered')`, decisionID(11), "k10b", orderID(10))
			execMust(t, ctx, pool, `INSERT INTO audit_log (event_type, entity_type, entity_id, actor, details) VALUES ('legacy_paper_order_cancelled','order',$1,'operator:p0_backfill','{"reason":"missing_historical_reference_price"}')`, orderID(10))
		}, true, nil)
	})
}

func execMust(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("failed to exec fixture sql %q: %v", sql, err)
	}
}

func orderID(n int) uuid.UUID { return uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", n)) }

func decisionID(n int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", 100+n))
}

func assertDecisionState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, status string, expectedReasons []string) {
	t.Helper()
	var gotStatus string
	var gotReasons []string
	if err := pool.QueryRow(ctx, `SELECT status, risk_reasons FROM trade_decisions WHERE id = $1`, id).Scan(&gotStatus, &gotReasons); err != nil {
		t.Fatalf("failed to query decision %s: %v", id, err)
	}
	if gotStatus != status {
		t.Fatalf("expected status %s, got %s", status, gotStatus)
	}
	sort.Strings(gotReasons)
	sort.Strings(expectedReasons)
	if strings.Join(gotReasons, ",") != strings.Join(expectedReasons, ",") {
		t.Fatalf("expected reasons %v, got %v", expectedReasons, gotReasons)
	}
}

func assertAuditDetails(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, expectAdded bool) {
	t.Helper()
	var got bool
	if err := pool.QueryRow(ctx, `SELECT (details ->> 'reason_added')::boolean FROM audit_log WHERE entity_id = $1 AND event_type = 'legacy_paper_decision_rejected'`, id).Scan(&got); err != nil {
		t.Fatalf("failed to query audit details: %v", err)
	}
	if got != expectAdded {
		t.Fatalf("expected reason_added=%v, got %v", expectAdded, got)
	}
}
