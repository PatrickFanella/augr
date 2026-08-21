package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type fakeEconomicBackend struct {
	accounts map[uuid.UUID]*domain.Account
	flows    map[string]*domain.CapitalFlow
}

func newFakeEconomicBackend() *fakeEconomicBackend {
	return &fakeEconomicBackend{accounts: map[uuid.UUID]*domain.Account{}, flows: map[string]*domain.CapitalFlow{}}
}

func (b *fakeEconomicBackend) CreateAccount(_ context.Context, value *domain.Account) error {
	copyValue := *value
	b.accounts[value.ID] = &copyValue
	return nil
}

func (b *fakeEconomicBackend) GetAccount(_ context.Context, id uuid.UUID) (*domain.Account, error) {
	value := b.accounts[id]
	if value == nil {
		return nil, repository.ErrNotFound
	}
	copyValue := *value
	return &copyValue, nil
}

func (b *fakeEconomicBackend) RecordFlow(_ context.Context, value *domain.CapitalFlow) (*domain.CapitalFlow, error) {
	key := value.AccountID.String() + ":" + value.IdempotencyKey
	if existing := b.flows[key]; existing != nil {
		return existing, nil
	}
	copyValue := *value
	b.flows[key] = &copyValue
	return &copyValue, nil
}

func (b *fakeEconomicBackend) CapitalSummary(_ context.Context, id uuid.UUID) (*domain.AccountCapitalSummary, error) {
	account := b.accounts[id]
	if account == nil {
		return nil, repository.ErrNotFound
	}
	net := account.StartingCapital
	for _, flow := range b.flows {
		if flow.AccountID == id && flow.Type == domain.CapitalFlowTypeDeposit {
			net = net.Add(flow.Amount)
		} else if flow.AccountID == id {
			net = net.Sub(flow.Amount)
		}
	}
	return &domain.AccountCapitalSummary{AccountID: id, StartingCapital: account.StartingCapital, NetCapital: net}, nil
}

func (b *fakeEconomicBackend) LedgerByOrigin(_ context.Context, id uuid.UUID, _, origin string) (*ledger.Transaction, error) {
	for _, flow := range b.flows {
		if flow.AccountID == id && flow.ID.String() == origin {
			amount := flow.Amount
			if flow.Type == domain.CapitalFlowTypeWithdrawal {
				amount = amount.Neg()
			}
			return &ledger.Transaction{ID: uuid.New(), AccountID: id, OriginType: "capital_flow", OriginID: origin, Postings: []ledger.Posting{{Amount: amount}, {Amount: amount.Neg()}}}, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (*fakeEconomicBackend) Close() {}

func TestBootstrapScoredTiersIsDeterministicAndIdempotent(t *testing.T) {
	backend := newFakeEconomicBackend()
	input := `{"namespace_prefix":"local/test","created_at":"2026-08-20T12:00:00Z"}`
	for range 2 {
		var output bytes.Buffer
		if err := run(context.Background(), []string{"bootstrap-scored-tiers", "--db-url", "postgres://local"}, strings.NewReader(input), &output, func(context.Context, string) (economicBackend, error) { return backend, nil }); err != nil {
			t.Fatal(err)
		}
		var response struct {
			Accounts []domain.Account `json:"accounts"`
		}
		if err := json.Unmarshal(output.Bytes(), &response); err != nil || len(response.Accounts) != 6 {
			t.Fatalf("response=%s err=%v", output.String(), err)
		}
		if !response.Accounts[0].StartingCapital.Equal(decimal.NewFromInt(500)) || !response.Accounts[5].StartingCapital.Equal(decimal.NewFromInt(5_000_000)) {
			t.Fatalf("reviewed endpoint tiers missing: %+v", response.Accounts)
		}
	}
	if len(backend.accounts) != 6 {
		t.Fatalf("accounts=%d, want idempotent six", len(backend.accounts))
	}
}

func TestCapitalFlowReturnsBalancedLedgerAndPreservesStartingCapital(t *testing.T) {
	backend := newFakeEconomicBackend()
	createdAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	accounts, err := bootstrap(context.Background(), backend, bootstrapInput{"local/flow", createdAt})
	if err != nil {
		t.Fatal(err)
	}
	input := `{"account_id":"` + accounts[0].ID.String() + `","type":"withdrawal","amount":"25","idempotency_key":"withdraw-1","effective_at":"2026-08-20T13:00:00Z","observed_at":"2026-08-20T13:00:01Z"}`
	var output bytes.Buffer
	if err = run(context.Background(), []string{"capital-flow", "--db-url", "postgres://local"}, strings.NewReader(input), &output, func(context.Context, string) (economicBackend, error) { return backend, nil }); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Summary domain.AccountCapitalSummary `json:"capital_summary"`
		Ledger  ledger.Transaction           `json:"ledger_transaction"`
	}
	if err = json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Summary.StartingCapital.Equal(decimal.NewFromInt(500)) || !response.Summary.NetCapital.Equal(decimal.NewFromInt(475)) {
		t.Fatalf("summary=%+v", response.Summary)
	}
	if len(response.Ledger.Postings) != 2 || !response.Ledger.Postings[0].Amount.Add(response.Ledger.Postings[1].Amount).IsZero() {
		t.Fatalf("ledger is not balanced: %+v", response.Ledger)
	}
}

func TestEconomicCommandFailsClosed(t *testing.T) {
	backend := newFakeEconomicBackend()
	var output bytes.Buffer
	err := run(context.Background(), []string{"capital-flow", "--db-url", "postgres://local"}, strings.NewReader(`{"unknown":true}`), &output, func(context.Context, string) (economicBackend, error) { return backend, nil })
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("strict decode error=%v", err)
	}
}
