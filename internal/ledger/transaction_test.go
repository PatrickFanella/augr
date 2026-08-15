package ledger

import (
	"encoding/json"
	"testing"
	"testing/quick"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestNewTransactionCreatesBalancedCurrencyEntry(t *testing.T) {
	accountID := uuid.New()
	capitalFlowID := uuid.New()
	effectiveAt := time.Date(2026, 8, 14, 16, 0, 0, 0, time.UTC)
	observedAt := effectiveAt.Add(time.Second)

	transaction, err := NewTransaction(TransactionInput{
		AccountID:      accountID,
		EventType:      "capital_flow.deposit",
		IdempotencyKey: "capital-flow:" + capitalFlowID.String(),
		OriginType:     "capital_flow",
		OriginID:       capitalFlowID.String(),
		ReferenceType:  "capital_flow",
		ReferenceID:    capitalFlowID.String(),
		EffectiveAt:    effectiveAt,
		ObservedAt:     observedAt,
		Metadata:       json.RawMessage(`{"source":"operator"}`),
		Postings: []PostingInput{
			{
				IdempotencyKey: "cash",
				LedgerAccount:  "asset:cash",
				UnitKind:       UnitKindCurrency,
				Unit:           "USD",
				Amount:         decimal.NewFromInt(500),
			},
			{
				IdempotencyKey: "contributed-capital",
				LedgerAccount:  "equity:contributed_capital",
				UnitKind:       UnitKindCurrency,
				Unit:           "USD",
				Amount:         decimal.NewFromInt(-500),
			},
		},
	})
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	if transaction.ID == uuid.Nil || transaction.AccountID != accountID {
		t.Fatalf("transaction identity = %s/%s, want non-nil/%s", transaction.ID, transaction.AccountID, accountID)
	}
	if transaction.EventType != "capital_flow.deposit" || transaction.IdempotencyKey == "" {
		t.Fatalf("transaction event identity = %q/%q", transaction.EventType, transaction.IdempotencyKey)
	}
	if !transaction.EffectiveAt.Equal(effectiveAt) || !transaction.ObservedAt.Equal(observedAt) {
		t.Fatalf("transaction timestamps = %s/%s", transaction.EffectiveAt, transaction.ObservedAt)
	}
	if len(transaction.Postings) != 2 {
		t.Fatalf("postings = %d, want 2", len(transaction.Postings))
	}
	for _, posting := range transaction.Postings {
		if posting.ID == uuid.Nil || posting.TransactionID != transaction.ID {
			t.Fatalf("posting identity = %s/%s, want transaction %s", posting.ID, posting.TransactionID, transaction.ID)
		}
	}
	if err := transaction.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestNewTransactionNormalizesTimestampsToPostgresPrecision(t *testing.T) {
	effectiveAt := time.Date(2026, 8, 14, 16, 0, 0, 123456789, time.UTC)
	observedAt := time.Date(2026, 8, 14, 16, 0, 1, 987654321, time.UTC)
	transaction, err := NewTransaction(TransactionInput{
		AccountID:      uuid.New(),
		EventType:      "test.timestamp_precision",
		IdempotencyKey: uuid.NewString(),
		OriginType:     "property_test",
		OriginID:       uuid.NewString(),
		EffectiveAt:    effectiveAt,
		ObservedAt:     observedAt,
		Postings: []PostingInput{
			{IdempotencyKey: "debit", LedgerAccount: "asset:cash", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(1)},
			{IdempotencyKey: "credit", LedgerAccount: "equity:test", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-1)},
		},
	})
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	if !transaction.EffectiveAt.Equal(effectiveAt.Truncate(time.Microsecond)) {
		t.Fatalf("effective_at = %s, want PostgreSQL precision %s", transaction.EffectiveAt, effectiveAt.Truncate(time.Microsecond))
	}
	if !transaction.ObservedAt.Equal(observedAt.Truncate(time.Microsecond)) {
		t.Fatalf("observed_at = %s, want PostgreSQL precision %s", transaction.ObservedAt, observedAt.Truncate(time.Microsecond))
	}
	if transaction.CreatedAt.Nanosecond()%1_000 != 0 {
		t.Fatalf("created_at precision = %s, want whole microseconds", transaction.CreatedAt)
	}
	for _, posting := range transaction.Postings {
		if !posting.CreatedAt.Equal(transaction.CreatedAt) {
			t.Fatalf("posting created_at = %s, want transaction created_at %s", posting.CreatedAt, transaction.CreatedAt)
		}
	}
}

func TestNewDeterministicTransactionReproducesAllIDs(t *testing.T) {
	input := TransactionInput{
		AccountID:      uuid.New(),
		EventType:      "fill.buy",
		IdempotencyKey: "economic-source-event:event-1",
		OriginType:     "economic_source_event",
		OriginID:       "22222222-2222-2222-2222-222222222222",
		ReferenceType:  "fill",
		ReferenceID:    "fill-1",
		EffectiveAt:    time.Date(2026, time.August, 15, 15, 0, 0, 0, time.UTC),
		ObservedAt:     time.Date(2026, time.August, 15, 15, 0, 1, 0, time.UTC),
		Postings: []PostingInput{
			{IdempotencyKey: "inventory", LedgerAccount: "asset:security_inventory", UnitKind: UnitKindInstrument, Unit: uuid.NewString(), Amount: decimal.NewFromInt(1)},
			{IdempotencyKey: "clearing-inventory", LedgerAccount: "clearing:execution", UnitKind: UnitKindInstrument, Unit: "temporary", Amount: decimal.NewFromInt(-1)},
		},
	}
	input.Postings[1].Unit = input.Postings[0].Unit
	seed := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	first, err := newDeterministicTransaction(seed, "economic_event_v1", input)
	if err != nil {
		t.Fatalf("newDeterministicTransaction() error = %v", err)
	}
	retry, err := newDeterministicTransaction(seed, "economic_event_v1", input)
	if err != nil {
		t.Fatalf("newDeterministicTransaction(retry) error = %v", err)
	}
	if first.ID != retry.ID {
		t.Fatalf("transaction IDs differ: %s != %s", first.ID, retry.ID)
	}
	for index := range first.Postings {
		if first.Postings[index].ID != retry.Postings[index].ID {
			t.Fatalf("posting %d IDs differ: %s != %s", index, first.Postings[index].ID, retry.Postings[index].ID)
		}
	}

	distinctVersion, err := newDeterministicTransaction(seed, "economic_event_v2", input)
	if err != nil {
		t.Fatal(err)
	}
	if distinctVersion.ID == first.ID {
		t.Fatal("normalizer version did not domain-separate transaction identity")
	}
}

func TestNewDeterministicTransactionRequiresObservedTime(t *testing.T) {
	input := TransactionInput{
		AccountID:      uuid.New(),
		EventType:      "cost.fee",
		IdempotencyKey: "economic-source-event:event-2",
		OriginType:     "economic_source_event",
		OriginID:       uuid.NewString(),
		EffectiveAt:    time.Now().UTC(),
		Postings: []PostingInput{
			{IdempotencyKey: "fee-expense", LedgerAccount: "expense:fees", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(1)},
			{IdempotencyKey: "fee-cash", LedgerAccount: "asset:cash", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-1)},
		},
	}
	if _, err := newDeterministicTransaction(uuid.New(), "economic_event_v1", input); err == nil {
		t.Fatal("newDeterministicTransaction() accepted a missing observed time")
	}
}

func TestNewTransactionLegacyIdentityAndObservedFallbackRemainUnchanged(t *testing.T) {
	input := TransactionInput{
		AccountID:      uuid.New(),
		EventType:      "test.legacy",
		IdempotencyKey: uuid.NewString(),
		OriginType:     "property_test",
		OriginID:       uuid.NewString(),
		EffectiveAt:    time.Now().UTC(),
		Postings: []PostingInput{
			{IdempotencyKey: "debit", LedgerAccount: "asset:cash", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(1)},
			{IdempotencyKey: "credit", LedgerAccount: "equity:test", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-1)},
		},
	}
	first, err := NewTransaction(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewTransaction(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatal("legacy NewTransaction() stopped allocating random IDs")
	}
	if first.ObservedAt.IsZero() || second.ObservedAt.IsZero() {
		t.Fatal("legacy NewTransaction() stopped defaulting observed time")
	}
}

func TestTransactionPropertyRejectsUnbalancedPostings(t *testing.T) {
	property := func(rawAmount uint32, rawDifference uint8) bool {
		amount := decimal.NewFromInt(int64(rawAmount%1_000_000) + 1)
		difference := decimal.NewFromInt(int64(rawDifference%100) + 1)
		_, err := NewTransaction(TransactionInput{
			AccountID:      uuid.New(),
			EventType:      "test.unbalanced",
			IdempotencyKey: uuid.NewString(),
			OriginType:     "property_test",
			OriginID:       uuid.NewString(),
			EffectiveAt:    time.Now().UTC(),
			Postings: []PostingInput{
				{
					IdempotencyKey: "debit",
					LedgerAccount:  "asset:cash",
					UnitKind:       UnitKindCurrency,
					Unit:           "USD",
					Amount:         amount,
				},
				{
					IdempotencyKey: "credit",
					LedgerAccount:  "equity:test",
					UnitKind:       UnitKindCurrency,
					Unit:           "USD",
					Amount:         amount.Neg().Add(difference),
				},
			},
		})
		return err != nil
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestTransactionPropertyRejectsDuplicatePostingKeys(t *testing.T) {
	property := func(rawAmount uint32) bool {
		amount := decimal.NewFromInt(int64(rawAmount%1_000_000) + 1)
		_, err := NewTransaction(TransactionInput{
			AccountID:      uuid.New(),
			EventType:      "test.duplicate",
			IdempotencyKey: uuid.NewString(),
			OriginType:     "property_test",
			OriginID:       uuid.NewString(),
			EffectiveAt:    time.Now().UTC(),
			Postings: []PostingInput{
				{
					IdempotencyKey: "duplicate-line",
					LedgerAccount:  "asset:cash",
					UnitKind:       UnitKindCurrency,
					Unit:           "USD",
					Amount:         amount,
				},
				{
					IdempotencyKey: "duplicate-line",
					LedgerAccount:  "equity:test",
					UnitKind:       UnitKindCurrency,
					Unit:           "USD",
					Amount:         amount.Neg(),
				},
			},
		})
		return err != nil
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestTransactionPropertyRejectsInvalidCurrencyUnits(t *testing.T) {
	invalidCurrencies := []string{"US", "USDX", "U1D", "€UR"}
	property := func(rawAmount uint32, rawCurrency uint8) bool {
		amount := decimal.NewFromInt(int64(rawAmount%1_000_000) + 1)
		currency := invalidCurrencies[int(rawCurrency)%len(invalidCurrencies)]
		_, err := NewTransaction(TransactionInput{
			AccountID:      uuid.New(),
			EventType:      "test.invalid_currency",
			IdempotencyKey: uuid.NewString(),
			OriginType:     "property_test",
			OriginID:       uuid.NewString(),
			EffectiveAt:    time.Now().UTC(),
			Postings: []PostingInput{
				{
					IdempotencyKey: "debit",
					LedgerAccount:  "asset:cash",
					UnitKind:       UnitKindCurrency,
					Unit:           currency,
					Amount:         amount,
				},
				{
					IdempotencyKey: "credit",
					LedgerAccount:  "equity:test",
					UnitKind:       UnitKindCurrency,
					Unit:           currency,
					Amount:         amount.Neg(),
				},
			},
		})
		return err != nil
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestNewTransactionRejectsPostingPrecisionBeyondSchema(t *testing.T) {
	amount := decimal.RequireFromString("1.0000000000001")
	_, err := NewTransaction(TransactionInput{
		AccountID:      uuid.New(),
		EventType:      "test.excess_precision",
		IdempotencyKey: uuid.NewString(),
		OriginType:     "property_test",
		OriginID:       uuid.NewString(),
		EffectiveAt:    time.Now().UTC(),
		Postings: []PostingInput{
			{
				IdempotencyKey: "debit",
				LedgerAccount:  "asset:cash",
				UnitKind:       UnitKindCurrency,
				Unit:           "USD",
				Amount:         amount,
			},
			{
				IdempotencyKey: "credit",
				LedgerAccount:  "equity:test",
				UnitKind:       UnitKindCurrency,
				Unit:           "USD",
				Amount:         amount.Neg(),
			},
		},
	})
	if err == nil {
		t.Fatal("NewTransaction() succeeded with posting precision beyond NUMERIC(38,12)")
	}
}

func TestNewTransactionRejectsPostingMagnitudeBeyondSchema(t *testing.T) {
	amount := decimal.RequireFromString("100000000000000000000000000")
	_, err := NewTransaction(TransactionInput{
		AccountID:      uuid.New(),
		EventType:      "test.excess_magnitude",
		IdempotencyKey: uuid.NewString(),
		OriginType:     "property_test",
		OriginID:       uuid.NewString(),
		EffectiveAt:    time.Now().UTC(),
		Postings: []PostingInput{
			{IdempotencyKey: "debit", LedgerAccount: "asset:cash", UnitKind: UnitKindCurrency, Unit: "USD", Amount: amount},
			{IdempotencyKey: "credit", LedgerAccount: "equity:test", UnitKind: UnitKindCurrency, Unit: "USD", Amount: amount.Neg()},
		},
	})
	if err == nil {
		t.Fatal("NewTransaction() succeeded with posting magnitude beyond NUMERIC(38,12)")
	}
}

func TestNewTransactionRejectsZeroPosting(t *testing.T) {
	_, err := NewTransaction(TransactionInput{
		AccountID:      uuid.New(),
		EventType:      "test.zero_posting",
		IdempotencyKey: uuid.NewString(),
		OriginType:     "property_test",
		OriginID:       uuid.NewString(),
		EffectiveAt:    time.Now().UTC(),
		Postings: []PostingInput{
			{IdempotencyKey: "debit", LedgerAccount: "asset:cash", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(1)},
			{IdempotencyKey: "credit", LedgerAccount: "equity:test", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-1)},
			{IdempotencyKey: "zero", LedgerAccount: "expense:test", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.Zero},
		},
	})
	if err == nil {
		t.Fatal("NewTransaction() succeeded with a zero posting")
	}
}

func TestTransactionValidateRejectsNonObjectMetadata(t *testing.T) {
	transaction, err := NewTransaction(TransactionInput{
		AccountID:      uuid.New(),
		EventType:      "test.metadata",
		IdempotencyKey: uuid.NewString(),
		OriginType:     "property_test",
		OriginID:       uuid.NewString(),
		EffectiveAt:    time.Now().UTC(),
		Postings: []PostingInput{
			{IdempotencyKey: "debit", LedgerAccount: "asset:cash", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(1)},
			{IdempotencyKey: "credit", LedgerAccount: "equity:test", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-1)},
		},
	})
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	transaction.Metadata = json.RawMessage(`[]`)
	if err := transaction.Validate(); err == nil {
		t.Fatal("Validate() accepted non-object transaction metadata")
	}
}

func TestTransactionValidateRejectsMissingDurableTimestamps(t *testing.T) {
	newValidTransaction := func(t *testing.T) *Transaction {
		t.Helper()
		transaction, err := NewTransaction(TransactionInput{
			AccountID:      uuid.New(),
			EventType:      "test.timestamps",
			IdempotencyKey: uuid.NewString(),
			OriginType:     "property_test",
			OriginID:       uuid.NewString(),
			EffectiveAt:    time.Now().UTC(),
			Postings: []PostingInput{
				{IdempotencyKey: "debit", LedgerAccount: "asset:cash", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(1)},
				{IdempotencyKey: "credit", LedgerAccount: "equity:test", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-1)},
			},
		})
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		return transaction
	}

	t.Run("transaction created_at", func(t *testing.T) {
		transaction := newValidTransaction(t)
		transaction.CreatedAt = time.Time{}
		if err := transaction.Validate(); err == nil {
			t.Fatal("Validate() accepted a transaction without created_at")
		}
	})

	t.Run("posting created_at", func(t *testing.T) {
		transaction := newValidTransaction(t)
		transaction.Postings[0].CreatedAt = time.Time{}
		if err := transaction.Validate(); err == nil {
			t.Fatal("Validate() accepted a posting without created_at")
		}
	})
}

func TestTransactionValidateRejectsUnnormalizedManualFields(t *testing.T) {
	newValidTransaction := func(t *testing.T) *Transaction {
		t.Helper()
		transaction, err := NewTransaction(TransactionInput{
			AccountID:      uuid.New(),
			EventType:      "test.normalization",
			IdempotencyKey: uuid.NewString(),
			OriginType:     "property_test",
			OriginID:       uuid.NewString(),
			EffectiveAt:    time.Now().UTC(),
			Postings: []PostingInput{
				{IdempotencyKey: "debit", LedgerAccount: "asset:cash", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(1)},
				{IdempotencyKey: "credit", LedgerAccount: "equity:test", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-1)},
			},
		})
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		return transaction
	}

	tests := []struct {
		name   string
		mutate func(*Transaction)
	}{
		{name: "event type", mutate: func(transaction *Transaction) { transaction.EventType = " test.normalization " }},
		{name: "transaction key", mutate: func(transaction *Transaction) { transaction.IdempotencyKey = " key " }},
		{name: "origin type", mutate: func(transaction *Transaction) { transaction.OriginType = " property_test " }},
		{name: "origin ID", mutate: func(transaction *Transaction) { transaction.OriginID = " origin " }},
		{name: "posting key", mutate: func(transaction *Transaction) { transaction.Postings[0].IdempotencyKey = " debit " }},
		{name: "ledger account", mutate: func(transaction *Transaction) { transaction.Postings[0].LedgerAccount = " asset:cash " }},
		{name: "unit", mutate: func(transaction *Transaction) { transaction.Postings[0].Unit = " USD " }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transaction := newValidTransaction(t)
			test.mutate(transaction)
			if err := transaction.Validate(); err == nil {
				t.Fatal("Validate() accepted an unnormalized manually assembled field")
			}
		})
	}
}

func TestNewTransactionBalancesEachCurrencyAndInstrumentUnitIndependently(t *testing.T) {
	transaction, err := NewTransaction(TransactionInput{
		AccountID:      uuid.New(),
		EventType:      "fill.buy",
		IdempotencyKey: uuid.NewString(),
		OriginType:     "fill",
		OriginID:       uuid.NewString(),
		EffectiveAt:    time.Now().UTC(),
		Postings: []PostingInput{
			{IdempotencyKey: "cash", LedgerAccount: "asset:cash", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-100)},
			{IdempotencyKey: "cash-clearing", LedgerAccount: "clearing:execution", UnitKind: UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(100)},
			{IdempotencyKey: "inventory", LedgerAccount: "asset:security_inventory", UnitKind: UnitKindInstrument, Unit: "AAPL", Amount: decimal.NewFromInt(1)},
			{IdempotencyKey: "inventory-clearing", LedgerAccount: "clearing:execution", UnitKind: UnitKindInstrument, Unit: "AAPL", Amount: decimal.NewFromInt(-1)},
		},
	})
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	if len(transaction.Postings) != 4 {
		t.Fatalf("postings = %d, want 4", len(transaction.Postings))
	}
}

func TestTransactionPropertyRejectsCrossCurrencyOffsets(t *testing.T) {
	property := func(rawAmount uint32) bool {
		amount := decimal.NewFromInt(int64(rawAmount%1_000_000) + 1)
		_, err := NewTransaction(TransactionInput{
			AccountID:      uuid.New(),
			EventType:      "test.wrong_currency",
			IdempotencyKey: uuid.NewString(),
			OriginType:     "property_test",
			OriginID:       uuid.NewString(),
			EffectiveAt:    time.Now().UTC(),
			Postings: []PostingInput{
				{IdempotencyKey: "usd", LedgerAccount: "asset:cash", UnitKind: UnitKindCurrency, Unit: "USD", Amount: amount},
				{IdempotencyKey: "eur", LedgerAccount: "equity:test", UnitKind: UnitKindCurrency, Unit: "EUR", Amount: amount.Neg()},
			},
		})
		return err != nil
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}
