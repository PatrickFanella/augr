package backtest

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
)

func TestCommonSimulationAdapterDelegatesPolicyAndErrors(t *testing.T) {
	policy := commonSimulationTestPolicy(t)
	adapter, err := NewCommonSimulation(policy)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := simulation.NewVenue(policy)
	if err != nil {
		t.Fatal(err)
	}
	if adapter.PolicyVersion() != direct.PolicyVersion() || adapter.PolicyDigest() != direct.PolicyDigest() {
		t.Fatalf("adapter policy = %q/%q, direct = %q/%q",
			adapter.PolicyVersion(), adapter.PolicyDigest(), direct.PolicyVersion(), direct.PolicyDigest())
	}

	request := simulation.EvaluationRequest{}
	adapterResult, adapterErr := adapter.Evaluate(request)
	directResult, directErr := direct.Evaluate(request)
	if adapterResult != nil || directResult != nil || adapterErr == nil || directErr == nil || adapterErr.Error() != directErr.Error() {
		t.Fatalf("adapter/direct invalid request = %#v/%v, %#v/%v", adapterResult, adapterErr, directResult, directErr)
	}
	var adapterVenueError, directVenueError *simulation.VenueError
	if !errors.As(adapterErr, &adapterVenueError) || !errors.As(directErr, &directVenueError) || adapterVenueError.Code != directVenueError.Code {
		t.Fatalf("adapter/direct error types = %#v/%#v", adapterVenueError, directVenueError)
	}
}

func TestCommonSimulationAdapterRejectsMissingPolicy(t *testing.T) {
	if _, err := NewCommonSimulation(nil); err == nil {
		t.Fatal("missing policy unexpectedly constructed a backtest adapter")
	}
}

func commonSimulationTestPolicy(t *testing.T) *simulation.Policy {
	t.Helper()
	base := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	policy, err := simulation.NewPolicy(simulation.PolicyInput{
		Schema: simulation.PolicySchemaV1,
		Assets: []simulation.AssetPolicy{{
			AssetClass:  instrument.AssetClassEquity,
			OrderTypes:  []lifecycle.OrderType{lifecycle.OrderMarket, lifecycle.OrderLimit},
			TimeInForce: []lifecycle.TimeInForce{lifecycle.TimeInForceDay, lifecycle.TimeInForceGTC},
			QuoteRequirements: marketdata.QuoteRequirements{
				RequireSource: true, RequireVenueContract: true, RequireBid: true, RequireAsk: true,
				RequireBidDepth: true, RequireAskDepth: true, RequireMarketStatus: true,
				RequireSessionStatus: true, AllowedMarketStatuses: []string{"open"},
				AllowedSessionStatuses: []string{"regular"}, MaxAge: time.Second,
			},
			MaxDepthParticipation: decimal.NewFromInt(1), FixedLatency: 100 * time.Millisecond,
			Calendar: simulation.CalendarPolicy{
				Kind:     simulation.CalendarExplicitSessions,
				Sessions: []simulation.SessionWindow{{Label: "test", OpenAt: base, CloseAt: base.Add(6 * time.Hour)}},
			},
			Fees: simulation.FeePolicy{Scale: 4},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return policy
}
