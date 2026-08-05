package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestBuildTradeDecisionListQuery_NoFilters(t *testing.T) {
	query, args := buildTradeDecisionListQuery(repository.TradeDecisionFilter{}, 10, 0)

	if len(args) != 2 {
		t.Fatalf("expected 2 args (limit, offset), got %d", len(args))
	}
	if args[0] != 10 || args[1] != 0 {
		t.Fatalf("unexpected args: %#v", args)
	}
	assertContains(t, query, "FROM trade_decisions")
	assertContains(t, query, "ORDER BY created_at DESC, id DESC")
	assertContains(t, query, "LIMIT $1 OFFSET $2")
	assertNotContains(t, query, "WHERE")
}

func TestBuildTradeDecisionListQuery_AllFilters(t *testing.T) {
	strategyID := uuid.New()
	instrumentKey := "KX-ABC"
	after := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	before := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)

	query, args := buildTradeDecisionListQuery(repository.TradeDecisionFilter{
		StrategyID:    &strategyID,
		InstrumentKey: instrumentKey,
		MarketType:    domain.MarketTypeStock,
		Status:        domain.TradeDecisionStatusLive,
		CreatedAfter:  &after,
		CreatedBefore: &before,
	}, 25, 50)

	if len(args) != 8 {
		t.Fatalf("expected 7 args, got %d: %#v", len(args), args)
	}
	assertContains(t, query, "strategy_id = $1")
	assertContains(t, query, "instrument_key = $2")
	assertContains(t, query, "market_type = $3")
	assertContains(t, query, "status = $4")
	assertContains(t, query, "created_at >= $5")
	assertContains(t, query, "created_at <= $6")
	assertContains(t, query, "LIMIT $7 OFFSET $8")
	if args[0] != strategyID || args[1] != instrumentKey || args[2] != domain.MarketTypeStock || args[3] != domain.TradeDecisionStatusLive {
		t.Fatalf("unexpected filter args: %#v", args[:4])
	}
}

func TestBuildTradeDecisionCountQuery(t *testing.T) {
	strategyID := uuid.New()
	query, args := buildTradeDecisionCountQuery(repository.TradeDecisionFilter{StrategyID: &strategyID, Status: domain.TradeDecisionStatusPaper})

	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}
	assertContains(t, query, "SELECT COUNT(*) FROM trade_decisions")
	assertContains(t, query, "strategy_id = $1")
	assertContains(t, query, "status = $2")
	assertNotContains(t, query, "LIMIT")
}

func TestBuildTradeDecisionAttachQuery(t *testing.T) {
	decisionID := uuid.New()
	orderID := uuid.New()
	query, args := buildTradeDecisionAttachQuery("paper_order_id", decisionID, orderID, domain.TradeDecisionStatusPaper)

	assertContains(t, query, "UPDATE trade_decisions SET paper_order_id = $2")
	assertContains(t, query, "status = $3")
	assertContains(t, query, "RETURNING id")
	if len(args) != 3 || args[0] != decisionID || args[1] != orderID || args[2] != domain.TradeDecisionStatusPaper {
		t.Fatalf("unexpected attach args: %#v", args)
	}
}

func TestMarshalTradeDecisionJSON(t *testing.T) {
	got, err := marshalTradeDecisionJSON(json.RawMessage(`{"a":1}`))
	if err != nil {
		t.Fatalf("marshalTradeDecisionJSON(valid) error = %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Fatalf("marshalTradeDecisionJSON(valid) = %s", got)
	}

	got, err = marshalTradeDecisionJSON(nil)
	if err != nil {
		t.Fatalf("marshalTradeDecisionJSON(nil) error = %v", err)
	}
	if string(got) != `{}` {
		t.Fatalf("marshalTradeDecisionJSON(nil) = %s", got)
	}

	if _, err := marshalTradeDecisionJSON(json.RawMessage(`{not valid`)); err == nil {
		t.Fatal("marshalTradeDecisionJSON(invalid) error = nil, want error")
	}
}

func TestScanTradeDecision_RoundTrip(t *testing.T) {
	strategyID := uuid.New()
	runID := uuid.New()
	paperOrderID := uuid.New()
	liveOrderID := uuid.New()
	externalMarketID := "mkt-123"
	outcome := "yes"
	createdAt := time.Date(2026, 6, 8, 14, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(5 * time.Minute)
	evidence := json.RawMessage(`{"signals":[1,2,3]}`)
	features := json.RawMessage(`{"price":1.23}`)
	promptText := "system: trade carefully\nuser: evaluate AAPL"
	llmProvider := "openai"
	llmModel := "gpt-4.1"
	promptTokens := 123
	completionTokens := 45
	latencyMS := 678
	costUSD := 0.0123

	got, err := scanTradeDecision(fakeTradeDecisionScanner{values: []any{
		uuid.New(),
		&strategyID,
		&runID,
		domain.MarketTypeStock,
		"AAPL-2026-06-08-C150",
		&externalMarketID,
		domain.OrderSideBuy,
		&outcome,
		1.25,
		1.20,
		0.05,
		10.0,
		5.0,
		4.5,
		0.12,
		100.0,
		95.0,
		domain.RiskDecisionApproved,
		[]string{"liquidity", "spread"},
		[]byte(evidence),
		[]byte(features),
		[]string{"momentum", "high-volume"},
		&promptText,
		&llmProvider,
		&llmModel,
		&promptTokens,
		&completionTokens,
		&latencyMS,
		&costUSD,
		&paperOrderID,
		&liveOrderID,
		domain.TradeDecisionStatusLive,
		createdAt,
		updatedAt,
	}})
	if err != nil {
		t.Fatalf("scanTradeDecision() error = %v", err)
	}

	if got.StrategyID == nil || *got.StrategyID != strategyID {
		t.Fatalf("StrategyID = %v, want %v", got.StrategyID, strategyID)
	}
	if got.PipelineRunID == nil || *got.PipelineRunID != runID {
		t.Fatalf("PipelineRunID = %v, want %v", got.PipelineRunID, runID)
	}
	if got.ExternalMarketID != externalMarketID || got.Outcome != outcome {
		t.Fatalf("unexpected string roundtrip: %+v", got)
	}
	if got.PaperOrderID == nil || *got.PaperOrderID != paperOrderID || got.LiveOrderID == nil || *got.LiveOrderID != liveOrderID {
		t.Fatalf("unexpected order attachment roundtrip: %+v", got)
	}
	if !jsonBytesEqual(got.Evidence, evidence) || !jsonBytesEqual(got.Features, features) {
		t.Fatalf("unexpected JSON roundtrip: evidence=%s features=%s", got.Evidence, got.Features)
	}
	if !reflect.DeepEqual(got.RiskReasons, []string{"liquidity", "spread"}) || !reflect.DeepEqual(got.RegimeTags, []string{"momentum", "high-volume"}) {
		t.Fatalf("unexpected array roundtrip: %+v", got)
	}
	if got.PromptText != promptText || got.LLMProvider != llmProvider || got.LLMModel != llmModel {
		t.Fatalf("unexpected LLM string metadata: %+v", got)
	}
	if got.PromptTokens == nil || *got.PromptTokens != promptTokens {
		t.Fatalf("PromptTokens = %v, want %d", got.PromptTokens, promptTokens)
	}
	if got.CompletionTokens == nil || *got.CompletionTokens != completionTokens {
		t.Fatalf("CompletionTokens = %v, want %d", got.CompletionTokens, completionTokens)
	}
	if got.LatencyMS == nil || *got.LatencyMS != latencyMS {
		t.Fatalf("LatencyMS = %v, want %d", got.LatencyMS, latencyMS)
	}
	if got.CostUSD == nil || *got.CostUSD != costUSD {
		t.Fatalf("CostUSD = %v, want %f", got.CostUSD, costUSD)
	}
	if got.Status != domain.TradeDecisionStatusLive || got.RiskStatus != domain.RiskDecisionApproved {
		t.Fatalf("unexpected enum roundtrip: %+v", got)
	}
	if !got.CreatedAt.Equal(createdAt) || !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected timestamps: got=%v/%v want=%v/%v", got.CreatedAt, got.UpdatedAt, createdAt, updatedAt)
	}
}

func TestTradeDecisionJournalRepo_CountByNoActionReason_ParsesFilterAndCoalescesZero(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	pool, cleanup := newTradeDecisionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewTradeDecisionJournalRepo(pool)
	strategyID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO strategies (id, market_type) VALUES ($1, $2)`, strategyID, domain.MarketTypeStock); err != nil {
		t.Fatalf("insert strategy: %v", err)
	}
	decisionTime := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	rows := []struct {
		status   domain.TradeDecisionStatus
		reasons  []string
		evidence json.RawMessage
	}{
		{status: domain.TradeDecisionStatusClosed, reasons: []string{"hold_signal"}},
		{status: domain.TradeDecisionStatusRejected, reasons: []string{"risk_rejected"}, evidence: json.RawMessage(`{"note":"risk flagged"}`)},
		{status: domain.TradeDecisionStatusRejected, reasons: []string{"sizing_zero"}, evidence: json.RawMessage(`{"note":"size 0"}`)},
		{status: domain.TradeDecisionStatusRejected},
	}
	for _, row := range rows {
		if _, err := pool.Exec(ctx, `INSERT INTO trade_decisions (id, strategy_id, instrument_key, market_type, side, status, risk_reasons, evidence, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, uuid.New(), &strategyID, "AAPL", domain.MarketTypeStock, domain.OrderSideBuy, row.status, row.reasons, row.evidence, decisionTime); err != nil {
			t.Fatalf("insert trade decision: %v", err)
		}
	}
	filtered, err := repo.CountByNoActionReason(ctx, repository.TradeDecisionFilter{StrategyID: &strategyID, MarketType: domain.MarketTypeStock})
	if err != nil {
		t.Fatalf("CountByNoActionReason() error = %v", err)
	}
	if filtered["hold_signal"] != 1 || filtered["risk_rejected"] != 1 || filtered["sizing_zero"] != 1 || filtered["unknown"] != 1 {
		t.Fatalf("unexpected reason counts: %#v", filtered)
	}
	empty, err := repo.CountByNoActionReason(ctx, repository.TradeDecisionFilter{InstrumentKey: "NOPE"})
	if err != nil {
		t.Fatalf("CountByNoActionReason() empty error = %v", err)
	}
	for _, key := range []string{"hold_signal", "risk_rejected", "sizing_zero", "unknown"} {
		if empty[key] != 0 {
			t.Fatalf("expected zero for %s, got %d", key, empty[key])
		}
	}
}

func TestTradeDecisionJournalRepo_CountByNoActionReason_EmptyTableEmptyFilterReturnsZeros(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	pool, cleanup := newTradeDecisionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewTradeDecisionJournalRepo(pool)
	counts, err := repo.CountByNoActionReason(ctx, repository.TradeDecisionFilter{})
	if err != nil {
		t.Fatalf("CountByNoActionReason() error = %v", err)
	}
	for _, key := range []string{"hold_signal", "risk_rejected", "sizing_zero", "sell_without_position", "kill_switch", "live_gate_denied", "missing_data", "unknown"} {
		if counts[key] != 0 {
			t.Fatalf("expected zero for %s, got %d", key, counts[key])
		}
	}
}

func newTradeDecisionIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	connString := os.Getenv("DB_URL")
	if connString == "" {
		connString = os.Getenv("DATABASE_URL")
	}
	if connString == "" {
		t.Skip("skipping integration test: DB_URL or DATABASE_URL is not set")
	}
	adminPool, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("failed to create admin pool: %v", err)
	}
	if _, err := adminPool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		adminPool.Close()
		t.Fatalf("failed to ensure pgcrypto extension: %v", err)
	}
	schemaName := "integration_trade_decision_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+"\""+schemaName+"\""); err != nil {
		adminPool.Close()
		t.Fatalf("failed to create test schema: %v", err)
	}
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+"\""+schemaName+"\""+` CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to parse pool config: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+"\""+schemaName+"\""+` CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to create test pool: %v", err)
	}
	ddl := []string{
		`CREATE TYPE trade_decision_status AS ENUM ('candidate','paper','live','closed','rejected','hold')`,
		`CREATE TYPE market_type AS ENUM ('stock','crypto','kalshi','polymarket')`,
		`CREATE TYPE order_side AS ENUM ('buy','sell')`,
		`CREATE TABLE strategies (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), market_type market_type NOT NULL)`,
		`CREATE TABLE trade_decisions (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), strategy_id UUID REFERENCES strategies(id), instrument_key TEXT NOT NULL, market_type market_type NOT NULL, side order_side NOT NULL, status trade_decision_status NOT NULL, risk_reasons TEXT[], evidence JSONB, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
	}
	for _, stmt := range ddl {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			pool.Close()
			_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+"\""+schemaName+"\""+` CASCADE`)
			adminPool.Close()
			t.Fatalf("failed to apply test schema DDL: %v", err)
		}
	}
	cleanup := func() {
		pool.Close()
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+"\""+schemaName+"\""+` CASCADE`)
		adminPool.Close()
	}
	return pool, cleanup
}

type fakeTradeDecisionScanner struct {
	values []any
}

func (s fakeTradeDecisionScanner) Scan(dest ...any) error {
	if len(dest) != len(s.values) {
		return fmt.Errorf("scan arity mismatch: got %d dests, want %d values", len(dest), len(s.values))
	}
	for i := range dest {
		if err := assignScanValue(dest[i], s.values[i]); err != nil {
			return fmt.Errorf("scan %d: %w", i, err)
		}
	}
	return nil
}

func assignScanValue(dst, src any) error {
	dv := reflect.ValueOf(dst)
	if dv.Kind() != reflect.Pointer || dv.IsNil() {
		return fmt.Errorf("destination must be a non-nil pointer")
	}
	dv = dv.Elem()
	if src == nil {
		dv.Set(reflect.Zero(dv.Type()))
		return nil
	}
	sv := reflect.ValueOf(src)
	if sv.Type().AssignableTo(dv.Type()) {
		dv.Set(sv)
		return nil
	}
	if sv.Type().ConvertibleTo(dv.Type()) {
		dv.Set(sv.Convert(dv.Type()))
		return nil
	}
	return fmt.Errorf("cannot assign %T to %s", src, dv.Type())
}
