package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/evidenceprogram"
)

type milestoneEvidenceFixture struct {
	ctx        context.Context
	shadowBase shadowCampaignFixture
	repo       *MilestoneEvidenceRepo
	chain      []*evidenceprogram.Assessment
}

func newMilestoneEvidenceFixture(t *testing.T) milestoneEvidenceFixture {
	t.Helper()
	ctx := context.Background()
	base := newShadowCampaignFixture(t)
	if _, err := base.base.evaluation.experiment.strategy.pool.Exec(ctx, repositoryMigrationSQL(t, "000103_milestone_evidence_assessments.up.sql")); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	shadow, err := evidenceprogram.AssessShadow(evidenceprogram.ShadowInput{
		StartedAt: start, EndedAt: start.Add(30 * 24 * time.Hour), DailyComplete: true,
		Parents: []evidenceprogram.EvidenceRef{{Kind: "shadow_campaign", ID: base.campaign.ID(), SHA256: base.campaign.Digest()}},
		Candidates: []evidenceprogram.CandidateShadow{
			{Key: "alpha", ObservedDays: 30, ExecutableSamples: 120, SimulatedFills: 90, SlippageKnown: true, SlippageDivergence: "0.001"},
			{Key: "beta", ObservedDays: 30, ExecutableSamples: 110, SimulatedFills: 80, SlippageKnown: true, SlippageDivergence: "-0.001"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	paper, err := evidenceprogram.AssessPaper(evidenceprogram.PaperInput{
		Shadow: shadow, StartedAt: start, EndedAt: start.Add(60 * 24 * time.Hour),
		Parents: []evidenceprogram.EvidenceRef{milestoneRef("cost_attribution_report", 1)},
		Candidates: []evidenceprogram.CandidatePaper{
			{Key: "alpha", Observations: 120, AfterCostExpectancy: "0.004", CostsComplete: true, StatisticallyHonest: true, MarginBounded: true},
			{Key: "beta", Observations: 110, AfterCostExpectancy: "-0.002", CostsComplete: true, StatisticallyHonest: true, MarginBounded: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	portfolio, err := evidenceprogram.AssessPortfolio(evidenceprogram.PortfolioInput{
		Paper: paper, StartedAt: start, EndedAt: start.Add(60 * 24 * time.Hour),
		CombinedRiskAdjusted: "1.05", BestSingleRiskAdjusted: "1", SameInterval: true, SameCostBasis: true,
		Parents: []evidenceprogram.EvidenceRef{milestoneRef("allocation_report", 2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	capabilityNames := []string{"accept_deposits", "resize_safely", "run_unattended", "brake", "restart", "reconcile", "daily_explanation"}
	capabilities := make([]evidenceprogram.Capability, len(capabilityNames))
	for index, name := range capabilityNames {
		capabilities[index] = evidenceprogram.Capability{Name: name, Passed: true, Evidence: milestoneRef("qualification", byte(index+3))}
	}
	readiness, err := evidenceprogram.AssessReadiness(evidenceprogram.ReadinessInput{Portfolio: portfolio, Capabilities: capabilities})
	if err != nil {
		t.Fatal(err)
	}
	return milestoneEvidenceFixture{ctx: ctx, shadowBase: base, repo: NewMilestoneEvidenceRepo(base.base.evaluation.experiment.strategy.pool), chain: []*evidenceprogram.Assessment{shadow, paper, portfolio, readiness}}
}

func milestoneRef(kind string, value byte) evidenceprogram.EvidenceRef {
	return evidenceprogram.EvidenceRef{Kind: kind, ID: uuid.MustParse(fmt.Sprintf("70300000-0000-4000-8000-%012x", value)), SHA256: fmt.Sprintf("%064x", value)}
}

func TestMilestoneEvidenceRepositoryRollbackConcurrencyAndRestart(t *testing.T) {
	fixture := newMilestoneEvidenceFixture(t)
	shadow := fixture.chain[0]
	for _, failedStage := range []string{"assessment", "blockers", "parents"} {
		fixture.repo.afterStage = func(stage string) error {
			if stage == failedStage {
				return errors.New("injected")
			}
			return nil
		}
		if err := fixture.repo.RecordAssessment(fixture.ctx, shadow); err == nil {
			t.Fatalf("%s interruption accepted", failedStage)
		}
		var count int
		if err := fixture.shadowBase.base.evaluation.experiment.strategy.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM milestone_evidence_assessments`).Scan(&count); err != nil || count != 0 {
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
			errs <- fixture.repo.RecordAssessment(fixture.ctx, shadow)
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	for _, value := range fixture.chain[1:] {
		if err := fixture.repo.RecordAssessment(fixture.ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	restarted := NewMilestoneEvidenceRepo(fixture.shadowBase.base.evaluation.experiment.strategy.pool)
	for _, want := range fixture.chain {
		got, err := restarted.GetAssessment(fixture.ctx, want.ID())
		if err != nil || got.Digest() != want.Digest() || !bytes.Equal(got.CanonicalBytes(), want.CanonicalBytes()) {
			t.Fatalf("reload %s=%v err=%v", want.Campaign(), got, err)
		}
	}
}

func TestMilestoneEvidenceRepositoryForgeryAppendOnlyAndRollbackRefusal(t *testing.T) {
	fixture := newMilestoneEvidenceFixture(t)
	for _, value := range fixture.chain {
		if err := fixture.repo.RecordAssessment(fixture.ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	pool := fixture.shadowBase.base.evaluation.experiment.strategy.pool
	if _, err := pool.Exec(fixture.ctx, `UPDATE milestone_evidence_assessments SET outcome='held' WHERE id=$1`, fixture.chain[0].ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("mutation error=%v", err)
	}
	if _, err := pool.Exec(fixture.ctx, `ALTER TABLE milestone_evidence_parents DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(fixture.ctx, `UPDATE milestone_evidence_parents SET evidence_sha256=$1 WHERE assessment_id=$2 AND sequence=0`, strings.Repeat("f", 64), fixture.chain[3].ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(fixture.ctx, `ALTER TABLE milestone_evidence_parents ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.GetAssessment(fixture.ctx, fixture.chain[3].ID()); err == nil || !strings.Contains(err.Error(), "parent does not reconstruct") {
		t.Fatalf("normalized forgery reload=%v", err)
	}
	if _, err := pool.Exec(fixture.ctx, repositoryMigrationSQL(t, "000103_milestone_evidence_assessments.down.sql")); err == nil || !strings.Contains(err.Error(), "rollback refused") {
		t.Fatalf("nonempty rollback=%v", err)
	}
}
