// Command augr-economic performs explicitly invoked local economic setup.
// It is not reachable from HTTP, is never scheduled, and has no broker or
// deployment authority.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/capital"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	postgresrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
)

type bootstrapInput struct {
	NamespacePrefix string    `json:"namespace_prefix"`
	CreatedAt       time.Time `json:"created_at"`
}

type flowInput struct {
	AccountID         uuid.UUID              `json:"account_id"`
	Type              domain.CapitalFlowType `json:"type"`
	Amount            string                 `json:"amount"`
	IdempotencyKey    string                 `json:"idempotency_key"`
	ExternalReference string                 `json:"external_reference"`
	Metadata          json.RawMessage        `json:"metadata"`
	EffectiveAt       time.Time              `json:"effective_at"`
	ObservedAt        time.Time              `json:"observed_at"`
}

type economicBackend interface {
	CreateAccount(context.Context, *domain.Account) error
	GetAccount(context.Context, uuid.UUID) (*domain.Account, error)
	RecordFlow(context.Context, *domain.CapitalFlow) (*domain.CapitalFlow, error)
	CapitalSummary(context.Context, uuid.UUID) (*domain.AccountCapitalSummary, error)
	LedgerByOrigin(context.Context, uuid.UUID, string, string) (*ledger.Transaction, error)
	Close()
}

type postgresEconomicBackend struct {
	db       *postgresrepo.DB
	accounts *postgresrepo.AccountRepo
	ledger   *postgresrepo.LedgerRepo
}

func openEconomicBackend(ctx context.Context, url string) (economicBackend, error) {
	db, err := postgresrepo.NewDB(ctx, url)
	if err != nil {
		return nil, err
	}
	version, err := postgresrepo.CurrentSchemaVersion(ctx, db.Pool)
	if err != nil || version != postgresrepo.RequiredSchemaVersion {
		db.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("augr-economic: schema version %d does not match required version %d", version, postgresrepo.RequiredSchemaVersion)
	}
	return &postgresEconomicBackend{db, postgresrepo.NewAccountRepo(db.Pool), postgresrepo.NewLedgerRepo(db.Pool)}, nil
}

func (b *postgresEconomicBackend) CreateAccount(ctx context.Context, value *domain.Account) error {
	return b.accounts.Create(ctx, value)
}

func (b *postgresEconomicBackend) GetAccount(ctx context.Context, id uuid.UUID) (*domain.Account, error) {
	return b.accounts.GetByID(ctx, id)
}

func (b *postgresEconomicBackend) RecordFlow(ctx context.Context, value *domain.CapitalFlow) (*domain.CapitalFlow, error) {
	return b.accounts.RecordCapitalFlow(ctx, value)
}

func (b *postgresEconomicBackend) CapitalSummary(ctx context.Context, id uuid.UUID) (*domain.AccountCapitalSummary, error) {
	return b.accounts.GetCapitalSummary(ctx, id)
}

func (b *postgresEconomicBackend) LedgerByOrigin(ctx context.Context, id uuid.UUID, kind, origin string) (*ledger.Transaction, error) {
	return b.ledger.GetByOrigin(ctx, id, kind, origin)
}
func (b *postgresEconomicBackend) Close() { b.db.Close() }

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, openEconomicBackend); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, open func(context.Context, string) (economicBackend, error)) error {
	if len(args) == 0 {
		return errors.New("usage: augr-economic <bootstrap-scored-tiers|capital-flow> [flags]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	databaseURL := flags.String("db-url", firstSet(os.Getenv("DB_URL"), os.Getenv("DATABASE_URL")), "schema-103 PostgreSQL connection URL")
	inputPath := flags.String("input", "-", "JSON input path, or - for stdin")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return fmt.Errorf("augr-economic: invalid flags")
	}
	if *databaseURL == "" {
		return errors.New("augr-economic: --db-url, DB_URL, or DATABASE_URL is required")
	}
	backend, err := open(ctx, *databaseURL)
	if err != nil {
		return err
	}
	defer backend.Close()
	encoder := json.NewEncoder(stdout)
	switch args[0] {
	case "bootstrap-scored-tiers":
		var input bootstrapInput
		if err = decodeInput(*inputPath, stdin, &input); err != nil {
			return err
		}
		accounts, bootstrapErr := bootstrap(ctx, backend, input)
		if bootstrapErr != nil {
			return bootstrapErr
		}
		return encoder.Encode(map[string]any{"accounts": accounts})
	case "capital-flow":
		var input flowInput
		if err = decodeInput(*inputPath, stdin, &input); err != nil {
			return err
		}
		flow, summary, transaction, flowErr := recordFlow(ctx, backend, input)
		if flowErr != nil {
			return flowErr
		}
		return encoder.Encode(map[string]any{"flow": flow, "capital_summary": summary, "ledger_transaction": transaction})
	default:
		return fmt.Errorf("augr-economic: unknown command %q", args[0])
	}
}

func bootstrap(ctx context.Context, backend economicBackend, input bootstrapInput) ([]domain.Account, error) {
	prefix := strings.Trim(strings.TrimSpace(input.NamespacePrefix), "/")
	if prefix == "" || input.CreatedAt.IsZero() {
		return nil, errors.New("augr-economic: namespace_prefix and created_at are required")
	}
	accounts := make([]domain.Account, 0, 6)
	for _, tier := range capital.ReviewedPolicyV1Input().Tiers {
		tierKey := tier.String()
		account, err := domain.NewAccount(domain.AccountInput{Name: "scored-" + tierKey, Environment: domain.AccountEnvironmentPaperScored, Venue: "internal", BaseCurrency: "USD", StorageNamespace: string(domain.AccountEnvironmentPaperScored) + "/" + prefix + "/" + tierKey, StartingCapital: tier, BuyingPowerMultiplier: capitalMultiplierOne(), MarginProfile: domain.MarginProfileCash, CreatedBy: "augr-economic", CreationMetadata: json.RawMessage(`{"policy":"capital-margin-policy-v1"}`), CreatedAt: input.CreatedAt})
		if err != nil {
			return nil, err
		}
		account.ID = economicid.DeterministicUUID("augr-economic-scored-account", account.StorageNamespace)
		existing, getErr := backend.GetAccount(ctx, account.ID)
		switch {
		case errors.Is(getErr, repository.ErrNotFound):
			if err = backend.CreateAccount(ctx, account); err != nil {
				return nil, err
			}
			existing = account
		case getErr != nil:
			return nil, getErr
		case !sameAccount(existing, account):
			return nil, fmt.Errorf("augr-economic: account %s conflicts with requested tier", account.ID)
		}
		accounts = append(accounts, *existing)
	}
	return accounts, nil
}

func recordFlow(ctx context.Context, backend economicBackend, input flowInput) (*domain.CapitalFlow, *domain.AccountCapitalSummary, *ledger.Transaction, error) {
	account, err := backend.GetAccount(ctx, input.AccountID)
	if err != nil {
		return nil, nil, nil, err
	}
	amount, err := decimalFromString(input.Amount)
	if err != nil {
		return nil, nil, nil, err
	}
	flow, err := domain.NewCapitalFlow(domain.CapitalFlowInput{AccountID: input.AccountID, Type: input.Type, Amount: amount, Currency: account.BaseCurrency, IdempotencyKey: input.IdempotencyKey, Source: domain.CapitalFlowSourceOperator, ExternalReference: input.ExternalReference, Metadata: input.Metadata, EffectiveAt: input.EffectiveAt, ObservedAt: input.ObservedAt})
	if err != nil {
		return nil, nil, nil, err
	}
	flow, err = backend.RecordFlow(ctx, flow)
	if err != nil {
		return nil, nil, nil, err
	}
	summary, err := backend.CapitalSummary(ctx, input.AccountID)
	if err != nil {
		return nil, nil, nil, err
	}
	transaction, err := backend.LedgerByOrigin(ctx, input.AccountID, "capital_flow", flow.ID.String())
	return flow, summary, transaction, err
}

func decodeInput(path string, stdin io.Reader, target any) error {
	reader := stdin
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		reader = file
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("augr-economic: decode input: %w", err)
	}
	return nil
}

func sameAccount(left, right *domain.Account) bool {
	if left == nil || right == nil {
		return false
	}
	leftCopy, rightCopy := *left, *right
	leftCopy.CreationMetadata, rightCopy.CreationMetadata = normalizeJSON(left.CreationMetadata), normalizeJSON(right.CreationMetadata)
	return reflect.DeepEqual(leftCopy, rightCopy)
}

func capitalMultiplierOne() decimal.Decimal { return decimal.NewFromInt(1) }

func decimalFromString(value string) (decimal.Decimal, error) {
	amount, err := decimal.NewFromString(strings.TrimSpace(value))
	if err != nil {
		return decimal.Zero, fmt.Errorf("augr-economic: invalid amount: %w", err)
	}
	return amount, nil
}

func normalizeJSON(raw json.RawMessage) json.RawMessage {
	var value any
	_ = json.Unmarshal(raw, &value)
	normalized, _ := json.Marshal(value)
	return normalized
}

func firstSet(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
