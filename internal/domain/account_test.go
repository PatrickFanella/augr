package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestNewAccountBuildsScoredEconomicBoundary(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 8, 14, 20, 0, 0, 123456789, time.UTC)
	account, err := NewAccount(AccountInput{
		Name:                  "Primary scored paper",
		Environment:           AccountEnvironmentPaperScored,
		Venue:                 "internal",
		BaseCurrency:          "usd",
		StorageNamespace:      "paper_scored/primary",
		StartingCapital:       decimal.RequireFromString("100000.00000000"),
		BuyingPowerMultiplier: decimal.RequireFromString("2"),
		MarginProfile:         MarginProfileRegT,
		CreatedBy:             "phase-1-test",
		CreationMetadata:      json.RawMessage(`{"purpose":"promotion baseline"}`),
		CreatedAt:             createdAt,
	})
	if err != nil {
		t.Fatalf("NewAccount() error = %v", err)
	}

	if account.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Fatal("NewAccount() ID is nil")
	}
	if account.BaseCurrency != "USD" {
		t.Fatalf("BaseCurrency = %q, want USD", account.BaseCurrency)
	}
	if account.EvidenceClass != PaperEvidenceClassPromotion || !account.PromotionEligible() {
		t.Fatalf("scored account evidence = %q, promotion eligible = %v", account.EvidenceClass, account.PromotionEligible())
	}
	if !account.StartingCapital.Equal(decimal.NewFromInt(100_000)) {
		t.Fatalf("StartingCapital = %s, want 100000", account.StartingCapital)
	}
	if !account.CreatedAt.Equal(createdAt.Truncate(time.Microsecond)) {
		t.Fatalf("CreatedAt = %s, want PostgreSQL precision %s", account.CreatedAt, createdAt.Truncate(time.Microsecond))
	}
	if string(account.CreationMetadata) != `{"purpose":"promotion baseline"}` {
		t.Fatalf("CreationMetadata = %s", account.CreationMetadata)
	}
}

func TestScoredAndStressAccountsCannotShareStorage(t *testing.T) {
	t.Parallel()

	scored, err := NewAccount(AccountInput{
		Name:                  "Scored",
		Environment:           AccountEnvironmentPaperScored,
		Venue:                 "internal",
		BaseCurrency:          "USD",
		StorageNamespace:      "paper_scored/shared",
		StartingCapital:       decimal.NewFromInt(100_000),
		BuyingPowerMultiplier: decimal.NewFromInt(2),
		MarginProfile:         MarginProfileRegT,
		CreatedBy:             "phase-1-test",
	})
	if err != nil {
		t.Fatalf("NewAccount(scored) error = %v", err)
	}
	stress, err := NewAccount(AccountInput{
		Name:                  "Stress",
		Environment:           AccountEnvironmentPaperStress,
		Venue:                 "internal",
		BaseCurrency:          "USD",
		StorageNamespace:      "paper_stress/shared",
		StartingCapital:       decimal.NewFromInt(100_000),
		BuyingPowerMultiplier: decimal.Zero,
		MarginProfile:         MarginProfileStressUnlimited,
		CreatedBy:             "phase-1-test",
	})
	if err != nil {
		t.Fatalf("NewAccount(stress) error = %v", err)
	}

	if stress.PromotionEligible() {
		t.Fatal("stress account is promotion eligible")
	}
	if scored.CanShareStorageWith(*stress) {
		t.Fatal("scored and stress accounts can share storage")
	}
}

func TestNewCapitalFlowCreatesAppendOnlyDeposit(t *testing.T) {
	t.Parallel()

	accountID := mustAccountUUID(t, "11111111-1111-4111-8111-111111111111")
	effectiveAt := time.Date(2026, 8, 15, 14, 30, 0, 123456789, time.UTC)
	flow, err := NewCapitalFlow(CapitalFlowInput{
		AccountID:         accountID,
		Type:              CapitalFlowTypeDeposit,
		Amount:            decimal.RequireFromString("4500.12500000"),
		Currency:          "usd",
		IdempotencyKey:    "operator-deposit-2026-08-15",
		Source:            CapitalFlowSourceOperator,
		ExternalReference: "transfer-42",
		Metadata:          json.RawMessage(`{"capital_tier":"5000"}`),
		EffectiveAt:       effectiveAt,
	})
	if err != nil {
		t.Fatalf("NewCapitalFlow() error = %v", err)
	}

	if flow.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Fatal("NewCapitalFlow() ID is nil")
	}
	if flow.AccountID != accountID || flow.Currency != "USD" {
		t.Fatalf("flow identity = account:%s currency:%q", flow.AccountID, flow.Currency)
	}
	if !flow.Amount.Equal(decimal.RequireFromString("4500.125")) {
		t.Fatalf("Amount = %s, want 4500.125", flow.Amount)
	}
	if !flow.EffectiveAt.Equal(effectiveAt.Truncate(time.Microsecond)) || flow.ObservedAt.IsZero() || flow.ObservedAt.Nanosecond()%1_000 != 0 {
		t.Fatalf("timestamps = effective:%s observed:%s", flow.EffectiveAt, flow.ObservedAt)
	}
}

func TestNewCapitalFlowRejectsPrecisionBeyondSchema(t *testing.T) {
	t.Parallel()

	_, err := NewCapitalFlow(CapitalFlowInput{
		AccountID:      mustAccountUUID(t, "22222222-2222-4222-8222-222222222222"),
		Type:           CapitalFlowTypeDeposit,
		Amount:         decimal.RequireFromString("0.000000001"),
		Currency:       "USD",
		IdempotencyKey: "too-precise",
		Source:         CapitalFlowSourceOperator,
		EffectiveAt:    time.Now(),
	})
	if err == nil {
		t.Fatal("NewCapitalFlow() error = nil, want precision error")
	}
}

func mustAccountUUID(t *testing.T, value string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(value)
	if err != nil {
		t.Fatalf("uuid.Parse(%q) error = %v", value, err)
	}
	return id
}
