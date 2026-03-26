package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestBuildBacktestConfigListQuery_NoFilters(t *testing.T) {
	query, args := buildBacktestConfigListQuery(repository.BacktestConfigFilter{}, 10, 0)

	if len(args) != 2 {
		t.Fatalf("expected 2 args (limit, offset), got %d", len(args))
	}

	if args[0] != 10 {
		t.Errorf("expected limit=10, got %v", args[0])
	}
	if args[1] != 0 {
		t.Errorf("expected offset=0, got %v", args[1])
	}

	assertContains(t, query, "FROM backtest_configs")
	assertContains(t, query, "ORDER BY created_at DESC, id DESC")
	assertContains(t, query, "LIMIT $1 OFFSET $2")
	assertNotContains(t, query, "WHERE")
}

func TestBuildBacktestConfigListQuery_AllFilters(t *testing.T) {
	strategyID := uuid.New()
	createdAfter := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)
	createdBefore := time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)

	query, args := buildBacktestConfigListQuery(repository.BacktestConfigFilter{
		StrategyID:    &strategyID,
		CreatedAfter:  &createdAfter,
		CreatedBefore: &createdBefore,
	}, 25, 50)

	if len(args) != 5 {
		t.Fatalf("expected 5 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "strategy_id = $1")
	assertContains(t, query, "created_at >= $2")
	assertContains(t, query, "created_at <= $3")
	assertContains(t, query, "LIMIT $4 OFFSET $5")
}

func TestMarshalBacktestSimulation_ValidJSON(t *testing.T) {
	data, err := marshalBacktestSimulation(domain.BacktestSimulationParameters{
		InitialCapital:   100000,
		SlippageModel:    json.RawMessage(`{"type":"proportional","basis_points":10}`),
		TransactionCosts: json.RawMessage(`{"commission_per_order":1.25}`),
		SpreadModel:      json.RawMessage(`{"type":"fixed","spread_bps":20}`),
		MaxVolumePct:     0.2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got domain.BacktestSimulationParameters
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("failed to unmarshal marshaled simulation params: %v", err)
	}

	if got.InitialCapital != 100000 {
		t.Errorf("expected initial capital 100000, got %v", got.InitialCapital)
	}
	if got.MaxVolumePct != 0.2 {
		t.Errorf("expected max volume pct 0.2, got %v", got.MaxVolumePct)
	}
	assertJSONEqual(t, got.SlippageModel, json.RawMessage(`{"type":"proportional","basis_points":10}`))
	assertJSONEqual(t, got.TransactionCosts, json.RawMessage(`{"commission_per_order":1.25}`))
	assertJSONEqual(t, got.SpreadModel, json.RawMessage(`{"type":"fixed","spread_bps":20}`))
}

func TestMarshalBacktestSimulation_InvalidJSON(t *testing.T) {
	_, err := marshalBacktestSimulation(domain.BacktestSimulationParameters{
		InitialCapital: 100000,
		SlippageModel:  json.RawMessage(`{"type":"proportional"`),
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestBacktestConfigRepoCreate_ValidateError(t *testing.T) {
	repo := NewBacktestConfigRepo(nil)

	err := repo.Create(context.Background(), &domain.BacktestConfig{
		StrategyID: uuid.New(),
		Name:       "invalid",
		StartDate:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:    time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "validate backtest config") {
		t.Fatalf("expected wrapped validation error, got %v", err)
	}
}

func TestBacktestConfigRepoUpdate_ValidateError(t *testing.T) {
	repo := NewBacktestConfigRepo(nil)

	err := repo.Update(context.Background(), &domain.BacktestConfig{
		ID:         uuid.New(),
		StrategyID: uuid.New(),
		Name:       "invalid",
		StartDate:  time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
		EndDate:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Simulation: domain.BacktestSimulationParameters{
			InitialCapital: 100000,
		},
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "validate backtest config") {
		t.Fatalf("expected wrapped validation error, got %v", err)
	}
}

func TestBacktestConfigRepoIntegration_CRUD(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newPositionIntegrationPool(t, ctx)
	defer cleanup()

	ensureBacktestConfigTable(t, ctx, pool)

	repo := NewBacktestConfigRepo(pool)
	strategyID := createTestPositionStrategy(t, ctx, pool)

	config := &domain.BacktestConfig{
		StrategyID:  strategyID,
		Name:        "Momentum 2024 baseline",
		Description: "Reusable baseline backtest",
		StartDate:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:     time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
		Simulation: domain.BacktestSimulationParameters{
			InitialCapital:   100000,
			SlippageModel:    json.RawMessage(`{"type":"proportional","basis_points":12}`),
			TransactionCosts: json.RawMessage(`{"commission_per_order":1.25,"exchange_fee_pct":0.001}`),
			SpreadModel:      json.RawMessage(`{"type":"fixed","spread_bps":15}`),
			MaxVolumePct:     0.25,
		},
	}

	if err := repo.Create(ctx, config); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if config.ID == uuid.Nil {
		t.Fatal("expected Create() to populate ID")
	}
	if config.CreatedAt.IsZero() || config.UpdatedAt.IsZero() {
		t.Fatal("expected Create() to populate timestamps")
	}

	got, err := repo.Get(ctx, config.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	assertBacktestConfigEqual(t, got, config)

	listed, err := repo.List(ctx, repository.BacktestConfigFilter{StrategyID: &strategyID}, 10, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 listed config, got %d", len(listed))
	}
	assertBacktestConfigEqual(t, &listed[0], config)

	originalUpdatedAt := config.UpdatedAt
	time.Sleep(time.Millisecond)
	config.Description = "Updated reusable baseline backtest"
	config.EndDate = time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)
	config.Simulation.InitialCapital = 250000
	config.Simulation.TransactionCosts = json.RawMessage(`{"commission_per_order":0.75,"exchange_fee_pct":0.0005}`)
	config.Simulation.MaxVolumePct = 0.5

	if err := repo.Update(ctx, config); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !config.UpdatedAt.After(originalUpdatedAt) {
		t.Fatalf("expected UpdatedAt to advance, got before=%v after=%v", originalUpdatedAt, config.UpdatedAt)
	}

	updated, err := repo.Get(ctx, config.ID)
	if err != nil {
		t.Fatalf("Get() after update error = %v", err)
	}
	assertBacktestConfigEqual(t, updated, config)

	if err := repo.Delete(ctx, config.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err = repo.Get(ctx, config.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func ensureBacktestConfigTable(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(ctx, `CREATE TABLE backtest_configs (
		id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
		strategy_id       UUID        NOT NULL REFERENCES strategies (id),
		name              TEXT        NOT NULL,
		description       TEXT        NOT NULL DEFAULT '',
		start_date        DATE        NOT NULL,
		end_date          DATE        NOT NULL,
		simulation_params JSONB       NOT NULL DEFAULT '{}',
		created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		t.Fatalf("failed to create backtest_configs table: %v", err)
	}
}

func assertBacktestConfigEqual(t *testing.T, got, want *domain.BacktestConfig) {
	t.Helper()

	if got.ID != want.ID {
		t.Fatalf("expected ID %s, got %s", want.ID, got.ID)
	}
	if got.StrategyID != want.StrategyID {
		t.Fatalf("expected StrategyID %s, got %s", want.StrategyID, got.StrategyID)
	}
	if got.Name != want.Name {
		t.Fatalf("expected Name %q, got %q", want.Name, got.Name)
	}
	if got.Description != want.Description {
		t.Fatalf("expected Description %q, got %q", want.Description, got.Description)
	}
	if !got.StartDate.Equal(want.StartDate) {
		t.Fatalf("expected StartDate %s, got %s", want.StartDate, got.StartDate)
	}
	if !got.EndDate.Equal(want.EndDate) {
		t.Fatalf("expected EndDate %s, got %s", want.EndDate, got.EndDate)
	}
	if got.Simulation.InitialCapital != want.Simulation.InitialCapital {
		t.Fatalf("expected InitialCapital %v, got %v", want.Simulation.InitialCapital, got.Simulation.InitialCapital)
	}
	if got.Simulation.MaxVolumePct != want.Simulation.MaxVolumePct {
		t.Fatalf("expected MaxVolumePct %v, got %v", want.Simulation.MaxVolumePct, got.Simulation.MaxVolumePct)
	}
	assertJSONEqual(t, got.Simulation.SlippageModel, want.Simulation.SlippageModel)
	assertJSONEqual(t, got.Simulation.TransactionCosts, want.Simulation.TransactionCosts)
	assertJSONEqual(t, got.Simulation.SpreadModel, want.Simulation.SpreadModel)
}

func assertJSONEqual(t *testing.T, got, want json.RawMessage) {
	t.Helper()

	var gotValue any
	if len(got) != 0 {
		if err := json.Unmarshal(got, &gotValue); err != nil {
			t.Fatalf("failed to unmarshal actual JSON %s: %v", got, err)
		}
	}

	var wantValue any
	if len(want) != 0 {
		if err := json.Unmarshal(want, &wantValue); err != nil {
			t.Fatalf("failed to unmarshal expected JSON %s: %v", want, err)
		}
	}

	gotBytes, err := json.Marshal(gotValue)
	if err != nil {
		t.Fatalf("failed to marshal actual JSON value: %v", err)
	}
	wantBytes, err := json.Marshal(wantValue)
	if err != nil {
		t.Fatalf("failed to marshal expected JSON value: %v", err)
	}

	if string(gotBytes) != string(wantBytes) {
		t.Fatalf("expected JSON %s, got %s", wantBytes, gotBytes)
	}
}
