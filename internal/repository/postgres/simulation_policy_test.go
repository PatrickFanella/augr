package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
)

func TestSimulationPolicyRepoRegistersLoadsAndReplaysExactArtifact(t *testing.T) {
	ctx, pool := newSimulationPolicyIntegrationPool(t)
	repo := NewSimulationPolicyRepo(pool)
	artifact := newSimulationPolicyArtifact(t, "1.25")

	persisted, err := repo.RegisterSimulationPolicy(ctx, artifact)
	if err != nil {
		t.Fatalf("RegisterSimulationPolicy() error = %v", err)
	}
	replayed, err := repo.RegisterSimulationPolicy(ctx, artifact)
	if err != nil {
		t.Fatalf("RegisterSimulationPolicy(retry) error = %v", err)
	}
	loaded, err := repo.GetSimulationPolicyByVersion(ctx, artifact.Version)
	if err != nil {
		t.Fatalf("GetSimulationPolicyByVersion() error = %v", err)
	}
	for name, candidate := range map[string]*simulation.PolicyArtifact{
		"persisted": persisted,
		"replayed":  replayed,
		"loaded":    loaded,
	} {
		if !simulation.SamePolicyArtifactPayload(candidate, artifact) || candidate.CreatedAt != artifact.CreatedAt {
			t.Fatalf("%s artifact = %#v, want exact payload and creation evidence", name, candidate)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM simulation_policy_artifacts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("artifact count = %d, want 1", count)
	}
}

func TestSimulationPolicyRepoRejectsChangedBytesForSameIdentity(t *testing.T) {
	ctx, pool := newSimulationPolicyIntegrationPool(t)
	repo := NewSimulationPolicyRepo(pool)
	artifact := newSimulationPolicyArtifact(t, "1.25")
	if _, err := repo.RegisterSimulationPolicy(ctx, artifact); err != nil {
		t.Fatal(err)
	}
	changed := *artifact
	changed.CanonicalBytes = append(append([]byte(nil), artifact.CanonicalBytes...), ' ')
	if _, err := repo.RegisterSimulationPolicy(ctx, &changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed artifact error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestSimulationPolicyRepoConcurrentRegistrationConverges(t *testing.T) {
	ctx, pool := newSimulationPolicyIntegrationPool(t)
	repo := NewSimulationPolicyRepo(pool)
	artifact := newSimulationPolicyArtifact(t, "1.25")
	const writers = 8
	var ready sync.WaitGroup
	var wait sync.WaitGroup
	ready.Add(writers)
	start := make(chan struct{})
	errorsFound := make(chan error, writers)
	for range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ready.Done()
			<-start
			result, err := repo.RegisterSimulationPolicy(ctx, artifact)
			if err != nil {
				errorsFound <- err
				return
			}
			if !simulation.SamePolicyArtifactPayload(result, artifact) {
				errorsFound <- errors.New("concurrent registration returned a different artifact")
			}
		}()
	}
	ready.Wait()
	close(start)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent RegisterSimulationPolicy() error = %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM simulation_policy_artifacts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("concurrent artifact count = %d, want 1", count)
	}
}

func TestSimulationPolicyRepoRecoversRoutedVersionAfterCurrentPolicyChanges(t *testing.T) {
	ctx, pool := newSimulationPolicyIntegrationPool(t)
	repo := NewSimulationPolicyRepo(pool)
	original := newSimulationPolicyArtifact(t, "1.25")
	current := newSimulationPolicyArtifact(t, "2.50")
	for _, artifact := range []*simulation.PolicyArtifact{original, current} {
		if _, err := repo.RegisterSimulationPolicy(ctx, artifact); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := repo.GetSimulationPolicyByVersion(ctx, original.Version)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := simulation.PolicyFromArtifact(*loaded)
	if err != nil {
		t.Fatalf("PolicyFromArtifact() error = %v", err)
	}
	if recovered.Version() != original.Version || recovered.Version() == current.Version {
		t.Fatalf("recovered version = %q, original/current = %q/%q", recovered.Version(), original.Version, current.Version)
	}
	if _, err := repo.GetSimulationPolicyByVersion(ctx, "simulation-policy-v1@sha256:"+strings.Repeat("0", 64)); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing version error = %v, want ErrNotFound", err)
	}
}

func newSimulationPolicyIntegrationPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool := newExecutionLifecycleIntegrationPool(t, ctx)
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000072_simulation_policy_artifacts.up.sql")); err != nil {
		t.Fatalf("apply migration 72: %v", err)
	}
	return ctx, pool
}

func newSimulationPolicyArtifact(t *testing.T, perOrder string) *simulation.PolicyArtifact {
	t.Helper()
	base := time.Date(2026, 8, 17, 12, 0, 0, 123456000, time.UTC)
	policy, err := simulation.NewPolicy(simulation.PolicyInput{
		Schema: simulation.PolicySchemaV1,
		Assets: []simulation.AssetPolicy{{
			AssetClass: instrument.AssetClassEquity,
			OrderTypes: []lifecycle.OrderType{lifecycle.OrderMarket, lifecycle.OrderLimit},
			TimeInForce: []lifecycle.TimeInForce{
				lifecycle.TimeInForceDay,
				lifecycle.TimeInForceGTC,
				lifecycle.TimeInForceIOC,
				lifecycle.TimeInForceFOK,
			},
			QuoteRequirements: marketdata.QuoteRequirements{
				RequireSource: true, RequireVenueContract: true, RequireBid: true, RequireAsk: true,
				RequireBidDepth: true, RequireAskDepth: true, RequireMarketStatus: true,
				RequireSessionStatus: true, AllowedMarketStatuses: []string{"open"},
				AllowedSessionStatuses: []string{"regular"}, MaxAge: 2 * time.Second,
			},
			MaxDepthParticipation: decimal.RequireFromString("0.25"),
			FixedLatency:          40 * time.Millisecond,
			Calendar: simulation.CalendarPolicy{
				Kind: simulation.CalendarExplicitSessions,
				Sessions: []simulation.SessionWindow{{
					Label: "regular-2026-08-17", OpenAt: base, CloseAt: base.Add(6 * time.Hour),
				}},
			},
			Fees: simulation.FeePolicy{
				PerOrder: decimal.RequireFromString(perOrder), PerUnit: decimal.RequireFromString("0.01"),
				NotionalBPS: decimal.RequireFromString("2"), Scale: 4,
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := policy.NewArtifact(base.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}
