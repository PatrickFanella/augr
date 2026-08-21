package paper

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
)

func TestCommonSimulationAdapterExposesExactPolicy(t *testing.T) {
	policy := paperCommonSimulationTestPolicy(t)
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
}

func TestCommonSimulationAdapterAcceptsOnlyMatchingPaperModes(t *testing.T) {
	adapter, err := NewCommonSimulation(paperCommonSimulationTestPolicy(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, environment := range []domain.AccountEnvironment{
		domain.AccountEnvironmentPaperScored,
		domain.AccountEnvironmentPaperStress,
	} {
		t.Run(string(environment), func(t *testing.T) {
			account := paperCommonSimulationTestAccount(t, environment)
			request := simulation.EvaluationRequest{
				Account: account,
				Aggregate: &lifecycle.Aggregate{
					Intent: lifecycle.Intent{AccountID: account.ID, Environment: account.Environment},
					Order:  &lifecycle.Order{AccountID: account.ID},
				},
			}
			_, evaluateErr := adapter.Evaluate(request)
			if evaluateErr == nil || errors.Is(evaluateErr, ErrCommonSimulationPaperBoundary) {
				t.Fatalf("matching %s request did not reach venue validation: %v", environment, evaluateErr)
			}
			var venueError *simulation.VenueError
			if !errors.As(evaluateErr, &venueError) || venueError.Code != simulation.VenueErrorInvalidRequest {
				t.Fatalf("matching %s request error = %T %v", environment, evaluateErr, evaluateErr)
			}
			if environment == domain.AccountEnvironmentPaperStress && account.PromotionEligible() {
				t.Fatal("stress account became promotion eligible")
			}
		})
	}

	for _, environment := range []domain.AccountEnvironment{
		domain.AccountEnvironmentShadow,
		domain.AccountEnvironmentLive,
	} {
		t.Run(string(environment), func(t *testing.T) {
			account := paperCommonSimulationTestAccount(t, environment)
			request := simulation.EvaluationRequest{
				Account: account,
				Aggregate: &lifecycle.Aggregate{
					Intent: lifecycle.Intent{AccountID: account.ID, Environment: account.Environment},
					Order:  &lifecycle.Order{AccountID: account.ID},
				},
			}
			if _, evaluateErr := adapter.Evaluate(request); !errors.Is(evaluateErr, ErrCommonSimulationPaperBoundary) {
				t.Fatalf("%s request error = %v", environment, evaluateErr)
			}
		})
	}
}

func TestCommonSimulationAdapterRejectsMismatchedPaperAccountAggregate(t *testing.T) {
	adapter, err := NewCommonSimulation(paperCommonSimulationTestPolicy(t))
	if err != nil {
		t.Fatal(err)
	}
	account := paperCommonSimulationTestAccount(t, domain.AccountEnvironmentPaperScored)
	request := simulation.EvaluationRequest{
		Account: account,
		Aggregate: &lifecycle.Aggregate{
			Intent: lifecycle.Intent{AccountID: uuid.New(), Environment: account.Environment},
			Order:  &lifecycle.Order{AccountID: account.ID},
		},
	}
	if _, evaluateErr := adapter.Evaluate(request); !errors.Is(evaluateErr, ErrCommonSimulationPaperBoundary) {
		t.Fatalf("mismatched request error = %v", evaluateErr)
	}
}

func TestCommonSimulationAdapterRejectsMissingPolicy(t *testing.T) {
	if _, err := NewCommonSimulation(nil); err == nil {
		t.Fatal("missing policy unexpectedly constructed a paper adapter")
	}
}

func paperCommonSimulationTestAccount(t *testing.T, environment domain.AccountEnvironment) domain.Account {
	t.Helper()
	namespace := string(environment) + "/common-simulation"
	account, err := domain.NewAccount(domain.AccountInput{
		Name: "paper common simulation", Environment: environment, Venue: "internal",
		BaseCurrency: "USD", StorageNamespace: namespace, StartingCapital: decimal.NewFromInt(100000),
		BuyingPowerMultiplier: decimal.NewFromInt(1), MarginProfile: domain.MarginProfileCash,
		CreatedBy: "paper-common-simulation-test", CreationMetadata: json.RawMessage(`{"fixture":"paper-common-simulation"}`),
		CreatedAt: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return *account
}

func paperCommonSimulationTestPolicy(t *testing.T) *simulation.Policy {
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
