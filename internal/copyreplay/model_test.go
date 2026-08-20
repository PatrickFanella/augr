package copyreplay

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
)

func replayFixture(t *testing.T) Input {
	t.Helper()
	managerA := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	managerB := uuid.MustParse("20000000-0000-4000-8000-000000000002")
	managerLate := uuid.MustParse("30000000-0000-4000-8000-000000000003")
	at := func(month time.Month, day int) time.Time { return time.Date(2026, month, day, 14, 0, 0, 0, time.UTC) }
	date := func(month time.Month, day int) time.Time { return time.Date(2026, month, day, 0, 0, 0, 0, time.UTC) }
	managerEvidence := []ManagerEvidence{
		{managerA, "manager-a-score", strings.Repeat("1", 64), at(time.January, 5), true, "10"},
		{managerB, "manager-b-score", strings.Repeat("2", 64), at(time.January, 6), true, "9"},
		{managerLate, "manager-late-score", strings.Repeat("3", 64), at(time.January, 11), true, "100"},
	}
	filings := []FilingEvidence{
		{managerA, "a-q1", strings.Repeat("a", 64), date(time.March, 31), at(time.May, 15), at(time.May, 15), 0, ""},
		{managerA, "a-q1-amendment", strings.Repeat("b", 64), date(time.March, 31), at(time.June, 1), at(time.June, 1), 1, "a-q1"},
		{managerA, "a-q2", strings.Repeat("c", 64), date(time.June, 30), at(time.August, 14), at(time.August, 14), 0, ""},
		{managerB, "b-q1", strings.Repeat("d", 64), date(time.March, 31), at(time.May, 10), at(time.May, 10), 0, ""},
	}
	observations := make([]dataset.ObservationInput, 0, len(managerEvidence)+len(filings))
	for _, value := range managerEvidence {
		published := value.AvailableAt
		observations = append(observations, dataset.ObservationInput{SourceKey: value.SourceKey, EffectiveAt: value.AvailableAt, PublishedAt: &published, ObservedAt: value.AvailableAt, AvailableAt: value.AvailableAt, Revision: "r1", ContentSHA256: value.ContentSHA256})
	}
	for _, value := range filings {
		published := value.PublishedAt
		observations = append(observations, dataset.ObservationInput{SourceKey: value.SourceKey, EffectiveAt: value.ReportPeriod, PublishedAt: &published, ObservedAt: value.AvailableAt, AvailableAt: value.AvailableAt, Revision: "r1", CorrectionOf: value.SupersedesKey, ContentSHA256: value.ContentSHA256})
	}
	manifest, err := dataset.NewManifest(dataset.ManifestInput{DecisionCutoff: at(time.August, 20), Partitions: []dataset.PartitionInput{{Kind: dataset.KindFilings, Provider: "sec", Source: "edgar", Namespace: "sec/13f/qualification", RequestSHA256: strings.Repeat("f", 64), MediaType: "application/json", SymbologyVersion: "sec-cik-v1", AdjustmentPolicy: "point_in_time", Timezone: "UTC", Calendar: "sec", Revision: "r1", License: "public-sec", RetentionPolicy: "retain-qualification", Observations: observations}}})
	if err != nil {
		t.Fatal(err)
	}
	return Input{Manifest: manifest, Policy: Policy{SelectionCutoff: at(time.January, 10), TopN: 2}, Managers: managerEvidence, Filings: filings, DecisionTimes: []time.Time{at(time.May, 1), at(time.May, 15), at(time.May, 20), at(time.June, 1), at(time.August, 14)}}
}

func TestReplayUsesOnlyPointInTimeAvailableEvidence(t *testing.T) {
	input := replayFixture(t)
	replay, err := NewReplay(input)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Managers() != 2 || replay.Decisions() != 10 || len(replay.PlanSteps()) != 4 {
		t.Fatalf("counts managers/decisions/steps=%d/%d/%d", replay.Managers(), replay.Decisions(), len(replay.PlanSteps()))
	}
	var canonical struct {
		Managers []struct {
			ManagerID string `json:"manager_id"`
		} `json:"managers"`
		Decisions []struct {
			DecisionAt      string `json:"decision_at"`
			ManagerID       string `json:"manager_id"`
			Status          string `json:"status"`
			FilingSourceKey string `json:"filing_source_key"`
		} `json:"decisions"`
	}
	if json.Unmarshal(replay.CanonicalBytes(), &canonical) != nil {
		t.Fatal("decode")
	}
	for _, manager := range canonical.Managers {
		if manager.ManagerID == "30000000-0000-4000-8000-000000000003" {
			t.Fatal("late manager was selected")
		}
	}
	wantA := []string{"", "a-q1", "a-q1", "a-q1-amendment", "a-q2"}
	wantStatus := []string{"no_filing", "selected", "unchanged", "selected", "selected"}
	index := 0
	for _, decision := range canonical.Decisions {
		if decision.ManagerID != "10000000-0000-4000-8000-000000000001" {
			continue
		}
		if decision.FilingSourceKey != wantA[index] || decision.Status != wantStatus[index] {
			t.Fatalf("A decision %d=%+v", index, decision)
		}
		index++
	}
	for _, step := range replay.PlanSteps() {
		if step.Action != experimentrun.ActionNoop || step.Intent != nil || step.RejectionCode != "" {
			t.Fatalf("step=%+v", step)
		}
	}
	reloaded, err := FromCanonical(replay.ID(), replay.Digest(), replay.CanonicalBytes(), input.Manifest)
	if err != nil || reloaded.Digest() != replay.Digest() {
		t.Fatalf("reload=%v/%v", reloaded, err)
	}
}

func TestReplayInputPermutationIsCanonical(t *testing.T) {
	input := replayFixture(t)
	first, err := NewReplay(input)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(input.Managers)
	slices.Reverse(input.Filings)
	second, err := NewReplay(input)
	if err != nil || first.Digest() != second.Digest() {
		t.Fatalf("digests=%s/%s err=%v", first.Digest(), second.Digest(), err)
	}
}

func TestReplayRejectsUnmanifestedAndInvalidAmendments(t *testing.T) {
	input := replayFixture(t)
	input.Managers[0].ContentSHA256 = strings.Repeat("0", 64)
	if _, err := NewReplay(input); err == nil {
		t.Fatal("unmanifested manager accepted")
	}
	input = replayFixture(t)
	input.Filings[1].ManagerID = input.Managers[1].ManagerID
	if _, err := NewReplay(input); err == nil {
		t.Fatal("cross-manager amendment accepted")
	}
}
