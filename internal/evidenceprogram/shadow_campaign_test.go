package evidenceprogram

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func shadowCampaignFixture(t *testing.T) *ShadowCampaign {
	t.Helper()
	campaign, err := NewShadowCampaign(ShadowCampaignInput{
		Key:       "ovr702-local-shadow",
		StartedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		Benchmark: ref("benchmark_opportunity_cost_report", 8),
		Candidates: []ShadowCandidate{
			{Key: "momentum_quality", VersionID: uuid.MustParse("70200000-0000-4000-8000-000000000001"), SHA256: fmt.Sprintf("%064x", 11)},
			{Key: "etf_trend", VersionID: uuid.MustParse("70200000-0000-4000-8000-000000000002"), SHA256: fmt.Sprintf("%064x", 12)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return campaign
}

func shadowDays(t *testing.T, campaign *ShadowCampaign, count int) []*ShadowDay {
	t.Helper()
	days := make([]*ShadowDay, 0, count)
	for sequence := range count {
		day, err := NewShadowDay(ShadowDayInput{
			Campaign:   campaign,
			Sequence:   sequence,
			ObservedAt: campaign.StartedAt().Add(time.Duration(sequence) * 24 * time.Hour),
			Source:     ref("daily_execution_capture", byte(sequence%8+1)),
			Candidates: []ShadowCandidateDayInput{
				{Key: "etf_trend", ExecutableSamples: 4, SimulatedFills: 2, SlippageKnown: true, SlippageDivergence: "0.2"},
				{Key: "momentum_quality", ExecutableSamples: 5, SimulatedFills: 3, SlippageKnown: true, SlippageDivergence: "0.1"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		days = append(days, day)
	}
	return days
}

func TestShadowCampaignAndDaysAreDeterministic(t *testing.T) {
	first := shadowCampaignFixture(t)
	second := shadowCampaignFixture(t)
	if first.ID() != second.ID() || first.Digest() != second.Digest() || !bytes.Equal(first.CanonicalBytes(), second.CanonicalBytes()) {
		t.Fatal("campaign identity diverged")
	}
	firstDay := shadowDays(t, first, 1)[0]
	secondDay := shadowDays(t, second, 1)[0]
	if firstDay.ID() != secondDay.ID() || firstDay.Digest() != secondDay.Digest() {
		t.Fatal("day identity diverged")
	}
	reloaded, err := ShadowCampaignFromCanonical(first.ID(), first.Digest(), first.CanonicalBytes())
	if err != nil || reloaded.ID() != first.ID() {
		t.Fatalf("campaign reload=%v/%v", reloaded, err)
	}
	reloadedDay, err := ShadowDayFromCanonical(firstDay.ID(), firstDay.Digest(), firstDay.CanonicalBytes(), reloaded)
	if err != nil || reloadedDay.ID() != firstDay.ID() {
		t.Fatalf("day reload=%v/%v", reloadedDay, err)
	}
}

func TestShadowCampaignRejectsCandidateAndCalendarDrift(t *testing.T) {
	campaign := shadowCampaignFixture(t)
	input := ShadowDayInput{Campaign: campaign, Sequence: 0, ObservedAt: campaign.StartedAt().Add(time.Hour), Source: ref("daily_execution_capture", 1), Candidates: []ShadowCandidateDayInput{{Key: "etf_trend"}, {Key: "momentum_quality"}}}
	if _, err := NewShadowDay(input); err == nil {
		t.Fatal("accepted calendar drift")
	}
	input.ObservedAt = campaign.StartedAt()
	input.Candidates[0].Key = "unknown"
	if _, err := NewShadowDay(input); err == nil {
		t.Fatal("accepted candidate drift")
	}
}

func TestBuildShadowAssessmentRequiresThirtyCompleteDays(t *testing.T) {
	campaign := shadowCampaignFixture(t)
	held, err := BuildShadowAssessment(campaign, shadowDays(t, campaign, 29))
	if err != nil || held.Outcome() != OutcomeHeld {
		t.Fatalf("held=%v err=%v", held, err)
	}
	qualified, err := BuildShadowAssessment(campaign, shadowDays(t, campaign, 30))
	if err != nil || qualified.Outcome() != OutcomeQualified {
		t.Fatalf("qualified=%v err=%v", qualified, err)
	}
}

func TestBuildShadowAssessmentRetainsDefectsAndUnknownSlippage(t *testing.T) {
	campaign := shadowCampaignFixture(t)
	days := shadowDays(t, campaign, 30)
	broken, err := NewShadowDay(ShadowDayInput{Campaign: campaign, Sequence: 7, ObservedAt: campaign.StartedAt().Add(7 * 24 * time.Hour), Source: ref("daily_execution_capture", 7), Candidates: []ShadowCandidateDayInput{{Key: "etf_trend", CriticalDefects: 1, ExecutableSamples: 4, SimulatedFills: 2, SlippageKnown: true, SlippageDivergence: "0.2"}, {Key: "momentum_quality", ExecutableSamples: 5, SimulatedFills: 3, SlippageKnown: false}}})
	if err != nil {
		t.Fatal(err)
	}
	days[7] = broken
	held, err := BuildShadowAssessment(campaign, days)
	if err != nil || held.Outcome() != OutcomeHeld || len(held.Blockers()) < 2 {
		t.Fatalf("held=%+v err=%v", held.Record(), err)
	}
}
