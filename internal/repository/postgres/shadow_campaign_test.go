package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/evidenceprogram"
)

type shadowCampaignFixture struct {
	ctx        context.Context
	base       benchmarkFixture
	repo       *ShadowCampaignRepo
	campaign   *evidenceprogram.ShadowCampaign
	candidates []evidenceprogram.ShadowCandidate
}

func newShadowCampaignFixture(t *testing.T) shadowCampaignFixture {
	t.Helper()
	ctx := context.Background()
	base := newBenchmarkFixture(t)
	if _, err := base.repo.RegisterDeclaration(ctx, base.declaration); err != nil {
		t.Fatal(err)
	}
	if _, err := base.repo.RecordReport(ctx, base.report); err != nil {
		t.Fatal(err)
	}
	strategy := base.evaluation.experiment.strategy
	second := strategyCatalogVersion(t, strategy.family.ID(), json.RawMessage(`{"lookback_sessions":60,"rebalance":"daily"}`), dataset.KindBars, dataset.KindQuotes)
	if _, err := strategy.repo.RegisterStrategyVersion(ctx, second); err != nil {
		t.Fatal(err)
	}
	if _, err := strategy.pool.Exec(ctx, repositoryMigrationSQL(t, "000102_shadow_campaign_evidence.up.sql")); err != nil {
		t.Fatal(err)
	}
	candidates := []evidenceprogram.ShadowCandidate{
		{Key: "alpha", VersionID: strategy.version.ID(), SHA256: strategy.version.Digest()},
		{Key: "beta", VersionID: second.ID(), SHA256: second.Digest()},
	}
	campaign, err := evidenceprogram.NewShadowCampaign(evidenceprogram.ShadowCampaignInput{
		Key:        "local-shadow-qualification",
		StartedAt:  time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC),
		Benchmark:  evidenceprogram.EvidenceRef{Kind: "benchmark_opportunity_cost_report", ID: base.report.ID(), SHA256: base.report.Digest()},
		Candidates: candidates,
	})
	if err != nil {
		t.Fatal(err)
	}
	return shadowCampaignFixture{ctx: ctx, base: base, repo: NewShadowCampaignRepo(strategy.pool), campaign: campaign, candidates: candidates}
}

func (fixture shadowCampaignFixture) day(t *testing.T, sequence int) *evidenceprogram.ShadowDay {
	t.Helper()
	day, err := evidenceprogram.NewShadowDay(evidenceprogram.ShadowDayInput{
		Campaign:   fixture.campaign,
		Sequence:   sequence,
		ObservedAt: fixture.campaign.StartedAt().Add(time.Duration(sequence) * 24 * time.Hour),
		Candidates: []evidenceprogram.ShadowCandidateDayInput{
			{Key: "alpha", ExecutableSamples: 4, SimulatedFills: 3, SlippageKnown: true, SlippageDivergence: "0.001"},
			{Key: "beta", ExecutableSamples: 5, SimulatedFills: 2, SlippageKnown: true, SlippageDivergence: "-0.001"},
		},
		Source: evidenceprogram.EvidenceRef{Kind: "local_shadow_observation", ID: uuid.NewSHA1(uuid.NameSpaceURL, []byte{byte(102), byte(sequence)}), SHA256: strings.Repeat("a", 64)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return day
}

func TestShadowCampaignRepositoryRollbackRoundTripAndEightWriters(t *testing.T) {
	fixture := newShadowCampaignFixture(t)
	for _, failedStage := range []string{"campaign", "candidates"} {
		fixture.repo.afterStage = func(stage string) error {
			if stage == failedStage {
				return errors.New("injected")
			}
			return nil
		}
		if err := fixture.repo.RegisterCampaign(fixture.ctx, fixture.campaign); err == nil {
			t.Fatalf("%s interruption accepted", failedStage)
		}
		var count int
		if err := fixture.base.evaluation.experiment.strategy.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM shadow_campaigns`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s partial rows=%d err=%v", failedStage, count, err)
		}
	}
	fixture.repo.afterStage = nil
	var wait sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errs <- fixture.repo.RegisterCampaign(fixture.ctx, fixture.campaign)
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}

	first := fixture.day(t, 0)
	for _, failedStage := range []string{"day", "day_candidates"} {
		fixture.repo.afterStage = func(stage string) error {
			if stage == failedStage {
				return errors.New("injected")
			}
			return nil
		}
		if err := fixture.repo.RegisterDay(fixture.ctx, first); err == nil {
			t.Fatalf("%s interruption accepted", failedStage)
		}
		var count int
		if err := fixture.base.evaluation.experiment.strategy.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM shadow_campaign_days`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s partial rows=%d err=%v", failedStage, count, err)
		}
	}
	fixture.repo.afterStage = nil
	days := make([]*evidenceprogram.ShadowDay, evidenceprogram.ShadowTargetDays)
	for sequence := range days {
		days[sequence] = fixture.day(t, sequence)
		if err := fixture.repo.RegisterDay(fixture.ctx, days[sequence]); err != nil {
			t.Fatal(err)
		}
	}

	restarted := NewShadowCampaignRepo(fixture.base.evaluation.experiment.strategy.pool)
	loaded, err := restarted.GetCampaign(fixture.ctx, fixture.campaign.ID())
	if err != nil || loaded.Digest() != fixture.campaign.Digest() || !bytes.Equal(loaded.CanonicalBytes(), fixture.campaign.CanonicalBytes()) {
		t.Fatalf("campaign reload=%v err=%v", loaded, err)
	}
	loadedDays, err := restarted.ListDays(fixture.ctx, loaded)
	if err != nil || len(loadedDays) != evidenceprogram.ShadowTargetDays {
		t.Fatalf("day reload count=%d err=%v", len(loadedDays), err)
	}
	for sequence := range loadedDays {
		if loadedDays[sequence].Digest() != days[sequence].Digest() || !bytes.Equal(loadedDays[sequence].CanonicalBytes(), days[sequence].CanonicalBytes()) {
			t.Fatalf("day %d changed across restart", sequence)
		}
	}
	assessment, err := evidenceprogram.BuildShadowAssessment(loaded, loadedDays)
	if err != nil || assessment.Outcome() != evidenceprogram.OutcomeQualified {
		t.Fatalf("assessment=%v err=%v", assessment, err)
	}
}

func TestShadowCampaignRepositoryConflictsAppendOnlyAndRollbackRefusal(t *testing.T) {
	fixture := newShadowCampaignFixture(t)
	if err := fixture.repo.RegisterCampaign(fixture.ctx, fixture.campaign); err != nil {
		t.Fatal(err)
	}
	day := fixture.day(t, 0)
	if err := fixture.repo.RegisterDay(fixture.ctx, day); err != nil {
		t.Fatal(err)
	}
	changedCampaign, err := evidenceprogram.NewShadowCampaign(evidenceprogram.ShadowCampaignInput{
		Key:        "local-shadow-qualification",
		StartedAt:  fixture.campaign.StartedAt().Add(24 * time.Hour),
		Benchmark:  fixture.campaign.Record().Benchmark,
		Candidates: fixture.candidates,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = fixture.repo.RegisterCampaign(fixture.ctx, changedCampaign); !errors.Is(err, ErrShadowCampaignConflict) {
		t.Fatalf("campaign conflict=%v", err)
	}
	changedDay, err := evidenceprogram.NewShadowDay(evidenceprogram.ShadowDayInput{
		Campaign: fixture.campaign, Sequence: 0, ObservedAt: fixture.campaign.StartedAt(),
		Candidates: []evidenceprogram.ShadowCandidateDayInput{
			{Key: "alpha", CriticalDefects: 1, ExecutableSamples: 4, SimulatedFills: 3, SlippageKnown: true, SlippageDivergence: "0.001"},
			{Key: "beta", ExecutableSamples: 5, SimulatedFills: 2, SlippageKnown: true, SlippageDivergence: "-0.001"},
		},
		Source: evidenceprogram.EvidenceRef{Kind: "local_shadow_observation", ID: uuid.New(), SHA256: strings.Repeat("b", 64)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = fixture.repo.RegisterDay(fixture.ctx, changedDay); !errors.Is(err, ErrShadowCampaignConflict) {
		t.Fatalf("day conflict=%v", err)
	}
	pool := fixture.base.evaluation.experiment.strategy.pool
	if _, err = pool.Exec(fixture.ctx, `UPDATE shadow_campaign_day_candidates SET critical_defects=1 WHERE day_id=$1`, day.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("mutation error=%v", err)
	}
	if _, err = pool.Exec(fixture.ctx, repositoryMigrationSQL(t, "000102_shadow_campaign_evidence.down.sql")); err == nil || !strings.Contains(err.Error(), "rollback refused") {
		t.Fatalf("nonempty rollback=%v", err)
	}
}
