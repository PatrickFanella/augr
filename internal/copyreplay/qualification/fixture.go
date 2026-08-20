// Package qualification provides one deterministic, synthetic OVR-504 replay
// fixture. It is local/golden evidence, not a historical data provider.
package qualification

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/copyreplay"
	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
)

func Build() (copyreplay.Input, error) {
	managerA := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	managerB := uuid.MustParse("20000000-0000-4000-8000-000000000002")
	managerLate := uuid.MustParse("30000000-0000-4000-8000-000000000003")
	at := func(month time.Month, day int) time.Time { return time.Date(2026, month, day, 14, 0, 0, 0, time.UTC) }
	date := func(month time.Month, day int) time.Time { return time.Date(2026, month, day, 0, 0, 0, 0, time.UTC) }
	managers := []copyreplay.ManagerEvidence{
		{ManagerID: managerA, SourceKey: "manager-a-score", ContentSHA256: strings.Repeat("1", 64), AvailableAt: at(time.January, 5), Eligible: true, Score: "10"},
		{ManagerID: managerB, SourceKey: "manager-b-score", ContentSHA256: strings.Repeat("2", 64), AvailableAt: at(time.January, 6), Eligible: true, Score: "9"},
		{ManagerID: managerLate, SourceKey: "manager-late-score", ContentSHA256: strings.Repeat("3", 64), AvailableAt: at(time.January, 11), Eligible: true, Score: "100"},
	}
	filings := []copyreplay.FilingEvidence{
		{ManagerID: managerA, SourceKey: "a-q1", ContentSHA256: strings.Repeat("a", 64), ReportPeriod: date(time.March, 31), PublishedAt: at(time.May, 15), AvailableAt: at(time.May, 15)},
		{ManagerID: managerA, SourceKey: "a-q1-amendment", ContentSHA256: strings.Repeat("b", 64), ReportPeriod: date(time.March, 31), PublishedAt: at(time.June, 1), AvailableAt: at(time.June, 1), AmendmentNumber: 1, SupersedesKey: "a-q1"},
		{ManagerID: managerA, SourceKey: "a-q2", ContentSHA256: strings.Repeat("c", 64), ReportPeriod: date(time.June, 30), PublishedAt: at(time.August, 14), AvailableAt: at(time.August, 14)},
		{ManagerID: managerB, SourceKey: "b-q1", ContentSHA256: strings.Repeat("d", 64), ReportPeriod: date(time.March, 31), PublishedAt: at(time.May, 10), AvailableAt: at(time.May, 10)},
	}
	observations := make([]dataset.ObservationInput, 0, len(managers)+len(filings))
	for _, value := range managers {
		published := value.AvailableAt
		observations = append(observations, dataset.ObservationInput{SourceKey: value.SourceKey, EffectiveAt: value.AvailableAt, PublishedAt: &published, ObservedAt: value.AvailableAt, AvailableAt: value.AvailableAt, Revision: "r1", ContentSHA256: value.ContentSHA256})
	}
	for _, value := range filings {
		published := value.PublishedAt
		observations = append(observations, dataset.ObservationInput{SourceKey: value.SourceKey, EffectiveAt: value.ReportPeriod, PublishedAt: &published, ObservedAt: value.AvailableAt, AvailableAt: value.AvailableAt, Revision: "r1", CorrectionOf: value.SupersedesKey, ContentSHA256: value.ContentSHA256})
	}
	manifest, err := dataset.NewManifest(dataset.ManifestInput{DecisionCutoff: at(time.August, 20), Partitions: []dataset.PartitionInput{{Kind: dataset.KindFilings, Provider: "sec", Source: "edgar", Namespace: "sec/13f/qualification", RequestSHA256: strings.Repeat("f", 64), MediaType: "application/json", SymbologyVersion: "sec-cik-v1", AdjustmentPolicy: "point_in_time", Timezone: "UTC", Calendar: "sec", Revision: "r1", License: "public-sec", RetentionPolicy: "retain-qualification", Observations: observations}}})
	if err != nil {
		return copyreplay.Input{}, err
	}
	return copyreplay.Input{Manifest: manifest, Policy: copyreplay.Policy{SelectionCutoff: at(time.January, 10), TopN: 2}, Managers: managers, Filings: filings, DecisionTimes: []time.Time{at(time.May, 1), at(time.May, 15), at(time.May, 20), at(time.June, 1), at(time.August, 14)}}, nil
}
