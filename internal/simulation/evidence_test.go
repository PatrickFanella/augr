package simulation

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

func TestSimulationFillEvidenceIsByteStable(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	snapshot := fixture.snapshot("stable-evidence", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	first, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Fills) != 1 || len(second.Fills) != 1 || !bytes.Equal(first.Fills[0].Evidence, second.Fills[0].Evidence) {
		t.Fatalf("repeated evidence differs:\n%s\n%s", first.Fills[0].Evidence, second.Fills[0].Evidence)
	}
	var decoded simulationFillEvidence
	if err := json.Unmarshal(first.Fills[0].Evidence, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Schema != fillEvidenceSchema || decoded.PolicyVersion != fixture.policy.Version() ||
		decoded.Environment != string(fixture.account.Environment) || decoded.EvidenceClass != fixture.account.EvidenceClass ||
		decoded.StorageNamespace != fixture.account.StorageNamespace || decoded.DepthLevel != 0 ||
		decoded.Multiplier != fixture.contract.Multiplier.String() || decoded.FeeAmount == "" {
		t.Fatalf("decoded evidence = %#v", decoded)
	}
	if !bytes.Equal(first.Transitions[0].Event.Evidence, first.Transitions[0].Normalization.SourceEvent.RawPayload) {
		t.Fatal("lifecycle and raw economic evidence bytes differ")
	}
}

func TestSimulationObservationEvidenceIsByteStable(t *testing.T) {
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) {
		config.orderType = "limit"
		config.limitPrice = decimalTestPointer("10.20")
	})
	snapshot := fixture.snapshot("stable-working", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.22"), Size: decimal.NewFromInt(10)}},
	)
	first, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Transitions) != 1 || len(second.Transitions) != 1 ||
		!bytes.Equal(first.Transitions[0].Event.Evidence, second.Transitions[0].Event.Evidence) ||
		first.Transitions[0].Event.SourceEventID != second.Transitions[0].Event.SourceEventID {
		t.Fatal("repeated working observation is not byte stable")
	}
}
