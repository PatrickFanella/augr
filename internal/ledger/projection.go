package ledger

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

const (
	PortfolioProjectionType    = "portfolio"
	PortfolioProjectionVersion = "ledger_fifo_v1"
	ProjectionFIFO             = "fifo"
	projectionTimestampLayout  = "2006-01-02T15:04:05.000000Z"
)

// ProjectionRequest defines one bitemporal, source-scoped portfolio rebuild.
type ProjectionRequest struct {
	AccountID     uuid.UUID
	AsOf          time.Time
	MarkSource    string
	MarkNamespace string
	MaxMarkAge    time.Duration
}

// ProjectionInput is the complete immutable evidence visible to one rebuild.
// Candidate rows after AsOf are accepted but never enter the replay or hashes.
type ProjectionInput struct {
	Request      ProjectionRequest
	BaseCurrency string
	Transactions []*Transaction
	Mechanics    []ProjectionMechanics
	Marks        []*MarkObservation
}

// ProjectionMechanics carries immutable schema-68 execution facts that are
// not encoded in balanced postings, chiefly dated valuation multipliers and
// typed execution provenance.
type ProjectionMechanics struct {
	NormalizationID       uuid.UUID
	SourceEventID         uuid.UUID
	TransactionID         uuid.UUID
	EventType             EconomicEventType
	NormalizerVersion     string
	ExecutionOriginType   ExecutionOriginType
	ExecutionOriginID     string
	ReferenceType         string
	ReferenceID           string
	Venue                 string
	InstrumentID          uuid.UUID
	SecondaryInstrumentID uuid.UUID
	VenueContractID       uuid.UUID
	OptionTermsID         uuid.UUID
	CashCurrency          string
	Quantity              *decimal.Decimal
	Price                 *decimal.Decimal
	CostKind              CostKind
	CostCurrency          string
	CostAmount            *decimal.Decimal
	PositionQuantity      *decimal.Decimal
	SettlementPrice       *decimal.Decimal
	OptionContractType    instrument.OptionContractType
	StrikePrice           *decimal.Decimal
	DeliverableQuantity   *decimal.Decimal
	PrimaryMultiplier     decimal.Decimal
	SecondaryMultiplier   decimal.Decimal
}

// ProjectionMark is the exact selected mark embedded in projection evidence.
type ProjectionMark struct {
	ID                  uuid.UUID
	InstrumentID        uuid.UUID
	Price               decimal.Decimal
	PriceCurrency       string
	Source              string
	SourceNamespace     string
	SourceObservationID string
	SourceRevision      string
	EffectiveAt         time.Time
	ObservedAt          time.Time
	Metadata            json.RawMessage
}

// ProjectionLot is one immutable opening segment plus its replayed remainder.
type ProjectionLot struct {
	ID                   uuid.UUID
	InstrumentID         uuid.UUID
	OpeningTransactionID uuid.UUID
	OpeningOriginType    ExecutionOriginType
	OpeningOriginID      string
	OpeningReferenceType string
	OpeningReferenceID   string
	OpenedAt             time.Time
	Quantity             decimal.Decimal
	RemainingQuantity    decimal.Decimal
	OpeningCash          decimal.Decimal
	RemainingOpeningCash decimal.Decimal
	MarkMultiplier       decimal.Decimal
}

// ProjectionMatch records one FIFO consumption or physical basis transfer.
type ProjectionMatch struct {
	ID                   uuid.UUID
	LotID                uuid.UUID
	InstrumentID         uuid.UUID
	ClosingTransactionID uuid.UUID
	OpeningOriginType    ExecutionOriginType
	OpeningOriginID      string
	OpeningReferenceType string
	OpeningReferenceID   string
	ClosingOriginType    ExecutionOriginType
	ClosingOriginID      string
	ClosingReferenceType string
	ClosingReferenceID   string
	ClosedAt             time.Time
	Quantity             decimal.Decimal
	OpeningCash          decimal.Decimal
	ClosingCash          decimal.Decimal
	RealizedPnL          decimal.Decimal
	Disposition          string
}

// ProjectionPosition aggregates all current and historical lot activity for
// one instrument. Closed positions remain in the evidence with Open=false.
type ProjectionPosition struct {
	InstrumentID         uuid.UUID
	Open                 bool
	Quantity             decimal.Decimal
	RemainingOpeningCash decimal.Decimal
	RealizedPnL          decimal.Decimal
	MarketValue          decimal.Decimal
	UnrealizedPnL        decimal.Decimal
	MarkObservationID    uuid.UUID
	OpenLotCount         int
}

// ProjectionTotals expresses the exact portfolio accounting equation.
type ProjectionTotals struct {
	Cash          decimal.Decimal
	NetCapital    decimal.Decimal
	Fees          decimal.Decimal
	Rebates       decimal.Decimal
	RealizedPnL   decimal.Decimal
	UnrealizedPnL decimal.Decimal
	MarketValue   decimal.Decimal
	Equity        decimal.Decimal
	TotalPnL      decimal.Decimal
}

// PortfolioProjection is a rebuild result and immutable checkpoint payload.
type PortfolioProjection struct {
	CheckpointID         uuid.UUID
	ProjectionType       string
	Version              string
	FIFO                 string
	AccountID            uuid.UUID
	BaseCurrency         string
	AsOf                 time.Time
	MarkSource           string
	MarkNamespace        string
	MaxMarkAge           time.Duration
	ThroughTransactionID uuid.UUID
	TransactionCount     int
	InputChecksum        string
	OutputChecksum       string
	Marks                []ProjectionMark
	Lots                 []ProjectionLot
	Matches              []ProjectionMatch
	Positions            []ProjectionPosition
	Totals               ProjectionTotals
	PayloadBytes         []byte
}

// ProjectionCheckpoint is the relational envelope around exact canonical
// projection bytes plus the separately verified replay-worker attestation.
type ProjectionCheckpoint struct {
	ID                   uuid.UUID
	AccountID            uuid.UUID
	ProjectionType       string
	ThroughTransactionID uuid.UUID
	ProjectionVersion    string
	AsOf                 time.Time
	FIFO                 string
	BaseCurrency         string
	MarkSource           string
	MarkNamespace        string
	MaxMarkAge           time.Duration
	TransactionCount     int
	MarkCount            int
	LotCount             int
	MatchCount           int
	PositionCount        int
	InputChecksum        string
	OutputChecksum       string
	PayloadBytes         []byte
	AttestationKeyID     string
	AttestationHMAC      []byte
	CreatedAt            time.Time
}

type projectionState struct {
	lots       []ProjectionLot
	matches    []ProjectionMatch
	activity   map[uuid.UUID]struct{}
	cash       decimal.Decimal
	netCapital decimal.Decimal
	fees       decimal.Decimal
	rebates    decimal.Decimal
	realized   decimal.Decimal
}

// ProjectionMechanicsFromNormalization validates and reduces a complete
// schema-68 aggregate to the replay mechanics that are not already postings.
func ProjectionMechanicsFromNormalization(normalization *EconomicNormalization) (ProjectionMechanics, error) {
	if normalization == nil {
		return ProjectionMechanics{}, fmt.Errorf("projection mechanics require an economic normalization")
	}
	if err := normalization.Validate(); err != nil {
		return ProjectionMechanics{}, fmt.Errorf("projection mechanics normalization: %w", err)
	}
	mechanics := ProjectionMechanics{
		NormalizationID:     normalization.ID,
		SourceEventID:       normalization.SourceEvent.ID,
		TransactionID:       normalization.Transaction.ID,
		EventType:           normalization.EventType,
		NormalizerVersion:   normalization.NormalizerVersion,
		ExecutionOriginType: normalization.ExecutionOriginType,
		ExecutionOriginID:   normalization.ExecutionOriginID,
		ReferenceType:       normalization.ReferenceType,
		ReferenceID:         normalization.ReferenceID,
		Venue:               normalization.Venue,
		CashCurrency:        normalization.CashCurrency,
		Quantity:            cloneEconomicDecimalPointer(normalization.Quantity),
		Price:               cloneEconomicDecimalPointer(normalization.Price),
		CostKind:            normalization.CostKind,
		CostCurrency:        normalization.CostCurrency,
		CostAmount:          cloneEconomicDecimalPointer(normalization.CostAmount),
		PositionQuantity:    cloneEconomicDecimalPointer(normalization.PositionQuantity),
		SettlementPrice:     cloneEconomicDecimalPointer(normalization.SettlementPrice),
	}
	if normalization.Instrument != nil {
		mechanics.InstrumentID = normalization.Instrument.ID
	}
	if normalization.SecondaryInstrument != nil {
		mechanics.SecondaryInstrumentID = normalization.SecondaryInstrument.ID
		mechanics.SecondaryMultiplier = normalization.SecondaryInstrument.Multiplier
	}
	if normalization.VenueContract != nil {
		mechanics.VenueContractID = normalization.VenueContract.ID
		mechanics.PrimaryMultiplier = normalization.VenueContract.Multiplier
	}
	if normalization.OptionTerms != nil {
		mechanics.OptionTermsID = normalization.OptionTerms.ID
		mechanics.OptionContractType = normalization.OptionTerms.ContractType
		mechanics.StrikePrice = cloneEconomicDecimal(normalization.OptionTerms.StrikePrice)
		mechanics.DeliverableQuantity = cloneEconomicDecimal(normalization.OptionTerms.DeliverableQuantity)
	}
	if err := mechanics.Validate(); err != nil {
		return ProjectionMechanics{}, err
	}
	return mechanics, nil
}

// Validate checks a reduced mechanics row independently of its source object.
func (mechanics ProjectionMechanics) Validate() error {
	if mechanics.NormalizationID == uuid.Nil || mechanics.SourceEventID == uuid.Nil || mechanics.TransactionID == uuid.Nil {
		return fmt.Errorf("projection normalization, source-event, and transaction IDs are required")
	}
	if !isNormalizedRequired(mechanics.NormalizerVersion) ||
		mechanics.NormalizationID != economicid.DeterministicUUID(
			economicNormalizationIDDomain, mechanics.SourceEventID.String(), mechanics.NormalizerVersion,
		) || mechanics.TransactionID != economicid.DeterministicUUID(
		"economic-ledger-transaction", mechanics.SourceEventID.String(), mechanics.NormalizerVersion,
	) {
		return fmt.Errorf("projection normalization mechanics have invalid deterministic identity")
	}
	if !validExecutionOriginType(mechanics.ExecutionOriginType) ||
		!isNormalizedRequired(mechanics.ExecutionOriginID) ||
		!isNormalizedRequired(mechanics.ReferenceType) || !isNormalizedRequired(mechanics.ReferenceID) ||
		!isCurrencyUnit(mechanics.CashCurrency) {
		return fmt.Errorf("projection execution origin and reference are invalid")
	}
	if mechanics.Venue != "" && (!isNormalizedRequired(mechanics.Venue) || mechanics.Venue != strings.ToLower(mechanics.Venue)) {
		return fmt.Errorf("projection mechanics venue is not normalized")
	}
	for _, value := range []*decimal.Decimal{
		mechanics.Quantity, mechanics.Price, mechanics.CostAmount, mechanics.PositionQuantity,
		mechanics.SettlementPrice, mechanics.StrikePrice, mechanics.DeliverableQuantity,
	} {
		if value != nil && !validProjectionDecimal(*value) {
			return fmt.Errorf("projection mechanics decimal has invalid precision or magnitude")
		}
	}
	for _, multiplier := range []decimal.Decimal{mechanics.PrimaryMultiplier, mechanics.SecondaryMultiplier} {
		if !multiplier.IsZero() && (!multiplier.IsPositive() || !validProjectionDecimal(multiplier)) {
			return fmt.Errorf("projection multiplier is not a positive exact decimal")
		}
	}
	switch mechanics.EventType {
	case EconomicEventFee, EconomicEventRebate:
		if mechanics.InstrumentID != uuid.Nil || mechanics.SecondaryInstrumentID != uuid.Nil ||
			mechanics.VenueContractID != uuid.Nil || mechanics.OptionTermsID != uuid.Nil || mechanics.Venue != "" ||
			!mechanics.PrimaryMultiplier.IsZero() || !mechanics.SecondaryMultiplier.IsZero() ||
			mechanics.Quantity != nil || mechanics.Price != nil || mechanics.PositionQuantity != nil ||
			mechanics.SettlementPrice != nil || mechanics.CostAmount == nil || !mechanics.CostAmount.IsPositive() ||
			mechanics.CostCurrency != mechanics.CashCurrency || mechanics.OptionContractType != "" ||
			mechanics.StrikePrice != nil || mechanics.DeliverableQuantity != nil ||
			(mechanics.EventType == EconomicEventFee && mechanics.CostKind != CostKindFee) ||
			(mechanics.EventType == EconomicEventRebate && mechanics.CostKind != CostKindRebate) {
			return fmt.Errorf("standalone cost mechanics cannot contain instrument mechanics")
		}
	case EconomicEventFillBuy, EconomicEventFillSell:
		if mechanics.InstrumentID == uuid.Nil || mechanics.SecondaryInstrumentID != uuid.Nil ||
			mechanics.VenueContractID == uuid.Nil || mechanics.OptionTermsID != uuid.Nil || mechanics.Venue == "" ||
			!mechanics.PrimaryMultiplier.IsPositive() || !mechanics.SecondaryMultiplier.IsZero() ||
			mechanics.Quantity == nil || !mechanics.Quantity.IsPositive() || mechanics.Price == nil || mechanics.Price.IsNegative() ||
			mechanics.PositionQuantity != nil || mechanics.SettlementPrice != nil ||
			mechanics.OptionContractType != "" || mechanics.StrikePrice != nil || mechanics.DeliverableQuantity != nil {
			return fmt.Errorf("fill projection mechanics are incomplete or contain irrelevant fields")
		}
		if (mechanics.CostAmount == nil) != (mechanics.CostKind == "" || mechanics.CostCurrency == "") {
			return fmt.Errorf("fill cost projection mechanics must be wholly absent or present")
		}
		if mechanics.CostAmount != nil && (!mechanics.CostAmount.IsPositive() ||
			(mechanics.CostKind != CostKindFee && mechanics.CostKind != CostKindRebate) ||
			mechanics.CostCurrency != mechanics.CashCurrency) {
			return fmt.Errorf("fill cost projection mechanics are invalid")
		}
	case EconomicEventOptionCashSettlement, EconomicEventOptionExpiration, EconomicEventPredictionPayout:
		if mechanics.InstrumentID == uuid.Nil || mechanics.SecondaryInstrumentID != uuid.Nil ||
			mechanics.VenueContractID == uuid.Nil || mechanics.OptionTermsID != uuid.Nil || mechanics.Venue == "" ||
			!mechanics.PrimaryMultiplier.IsPositive() || !mechanics.SecondaryMultiplier.IsZero() ||
			mechanics.Quantity != nil || mechanics.Price != nil || mechanics.CostKind != "" ||
			mechanics.CostCurrency != "" || mechanics.CostAmount != nil || mechanics.PositionQuantity == nil ||
			mechanics.PositionQuantity.IsZero() || mechanics.SettlementPrice == nil || mechanics.SettlementPrice.IsNegative() ||
			mechanics.OptionContractType != "" || mechanics.StrikePrice != nil || mechanics.DeliverableQuantity != nil {
			return fmt.Errorf("cash-settlement projection mechanics are incomplete or contain irrelevant fields")
		}
	case EconomicEventOptionExercise, EconomicEventOptionAssignment:
		if mechanics.InstrumentID == uuid.Nil || mechanics.SecondaryInstrumentID == uuid.Nil ||
			mechanics.VenueContractID == uuid.Nil || mechanics.OptionTermsID == uuid.Nil || mechanics.Venue == "" ||
			!mechanics.PrimaryMultiplier.IsPositive() || !mechanics.SecondaryMultiplier.IsPositive() ||
			mechanics.Quantity != nil || mechanics.Price != nil || mechanics.CostKind != "" ||
			mechanics.CostCurrency != "" || mechanics.CostAmount != nil || mechanics.PositionQuantity == nil ||
			mechanics.PositionQuantity.IsZero() || mechanics.SettlementPrice != nil ||
			(mechanics.OptionContractType != instrument.OptionContractCall && mechanics.OptionContractType != instrument.OptionContractPut) ||
			mechanics.StrikePrice == nil || !mechanics.StrikePrice.IsPositive() ||
			mechanics.DeliverableQuantity == nil || !mechanics.DeliverableQuantity.IsPositive() ||
			!mechanics.PrimaryMultiplier.Equal(*mechanics.DeliverableQuantity) {
			return fmt.Errorf("physical option mechanics require both instruments and positive multipliers")
		}
	default:
		return fmt.Errorf("projection instrument mechanics are invalid for %q", mechanics.EventType)
	}
	return nil
}

// BuildPortfolioProjection performs a complete deterministic replay from zero.
func BuildPortfolioProjection(input ProjectionInput) (*PortfolioProjection, error) {
	request, currency, err := normalizeProjectionBoundary(input.Request, input.BaseCurrency)
	if err != nil {
		return nil, err
	}
	transactions := make([]*Transaction, 0, len(input.Transactions))
	for _, transaction := range input.Transactions {
		if transaction == nil {
			return nil, fmt.Errorf("projection transaction cannot be nil")
		}
		if transaction.EffectiveAt.After(request.AsOf) || transaction.ObservedAt.After(request.AsOf) {
			continue
		}
		if err := transaction.Validate(); err != nil {
			return nil, fmt.Errorf("projection transaction %s: %w", transaction.ID, err)
		}
		if transaction.AccountID != request.AccountID {
			return nil, fmt.Errorf("projection transaction %s belongs to another account", transaction.ID)
		}
		transactions = append(transactions, transaction)
	}
	if len(transactions) == 0 {
		return nil, fmt.Errorf("projection requires at least one eligible ledger transaction")
	}
	sort.Slice(transactions, func(left, right int) bool {
		return projectionTransactionLess(transactions[left], transactions[right])
	})

	mechanicsByTransaction, err := eligibleProjectionMechanics(input.Mechanics, transactions)
	if err != nil {
		return nil, err
	}
	state := projectionState{activity: make(map[uuid.UUID]struct{})}
	for _, transaction := range transactions {
		cash, fees, rebates, err := projectionCashAndCosts(transaction, currency)
		if err != nil {
			return nil, err
		}
		state.cash = state.cash.Add(cash)
		state.fees = state.fees.Add(fees)
		state.rebates = state.rebates.Add(rebates)

		switch transaction.EventType {
		case "capital_flow.deposit", "capital_flow.withdrawal":
			if _, ok := mechanicsByTransaction[transaction.ID]; ok {
				return nil, fmt.Errorf("capital transaction %s unexpectedly has economic mechanics", transaction.ID)
			}
			if projectionHasAssetInventory(transaction) || cash.IsZero() {
				return nil, fmt.Errorf("capital transaction %s has invalid cash or inventory shape", transaction.ID)
			}
			if transaction.EventType == "capital_flow.deposit" && !cash.IsPositive() ||
				transaction.EventType == "capital_flow.withdrawal" && !cash.IsNegative() {
				return nil, fmt.Errorf("capital transaction %s cash direction is invalid", transaction.ID)
			}
			state.netCapital = state.netCapital.Add(cash)
		default:
			mechanics, ok := mechanicsByTransaction[transaction.ID]
			if !ok {
				return nil, fmt.Errorf("economic transaction %s lacks immutable normalization mechanics", transaction.ID)
			}
			if string(mechanics.EventType) != transaction.EventType {
				return nil, fmt.Errorf("transaction %s event type does not match mechanics", transaction.ID)
			}
			if mechanics.CashCurrency != currency {
				return nil, fmt.Errorf("transaction %s mechanics currency does not match account", transaction.ID)
			}
			if err := state.applyEconomicTransaction(transaction, mechanics, cash); err != nil {
				return nil, err
			}
		}
	}

	marks, positions, totals, err := state.finalize(request, currency, input.Marks)
	if err != nil {
		return nil, err
	}
	inputChecksum, err := projectionInputChecksum(request, currency, transactions, mechanicsByTransaction, marks)
	if err != nil {
		return nil, err
	}
	projection := &PortfolioProjection{
		ProjectionType:       PortfolioProjectionType,
		Version:              PortfolioProjectionVersion,
		FIFO:                 ProjectionFIFO,
		AccountID:            request.AccountID,
		BaseCurrency:         currency,
		AsOf:                 request.AsOf,
		MarkSource:           request.MarkSource,
		MarkNamespace:        request.MarkNamespace,
		MaxMarkAge:           request.MaxMarkAge,
		ThroughTransactionID: transactions[len(transactions)-1].ID,
		TransactionCount:     len(transactions),
		InputChecksum:        inputChecksum,
		Marks:                marks,
		Lots:                 append([]ProjectionLot(nil), state.lots...),
		Matches:              append([]ProjectionMatch(nil), state.matches...),
		Positions:            positions,
		Totals:               totals,
	}
	projection.CheckpointID = projectionCheckpointID(projection)
	payloadBytes, err := marshalProjectionPayload(projection)
	if err != nil {
		return nil, err
	}
	projection.PayloadBytes = payloadBytes
	projection.OutputChecksum = sha256Hex(payloadBytes)
	return projection, nil
}

// Checkpoint returns the replay-derived persistence envelope. The PostgreSQL
// repository attaches the external replay-worker attestation before validation
// and persistence.
func (projection *PortfolioProjection) Checkpoint() *ProjectionCheckpoint {
	if projection == nil {
		return nil
	}
	return &ProjectionCheckpoint{
		ID: projection.CheckpointID, AccountID: projection.AccountID,
		ProjectionType: projection.ProjectionType, ThroughTransactionID: projection.ThroughTransactionID,
		ProjectionVersion: projection.Version, AsOf: projection.AsOf, FIFO: projection.FIFO,
		BaseCurrency: projection.BaseCurrency, MarkSource: projection.MarkSource,
		MarkNamespace: projection.MarkNamespace, MaxMarkAge: projection.MaxMarkAge,
		TransactionCount: projection.TransactionCount, MarkCount: len(projection.Marks),
		LotCount: len(projection.Lots), MatchCount: len(projection.Matches), PositionCount: len(projection.Positions),
		InputChecksum: projection.InputChecksum, OutputChecksum: projection.OutputChecksum,
		PayloadBytes: append([]byte(nil), projection.PayloadBytes...),
	}
}

// Validate checks a loaded checkpoint's hashes, deterministic identity,
// canonical header, and collection counts without trusting JSONB reformatting.
func (checkpoint ProjectionCheckpoint) Validate() error {
	if checkpoint.ID == uuid.Nil || checkpoint.AccountID == uuid.Nil || checkpoint.ThroughTransactionID == uuid.Nil {
		return fmt.Errorf("projection checkpoint identities are required")
	}
	if checkpoint.ProjectionType != PortfolioProjectionType || checkpoint.ProjectionVersion != PortfolioProjectionVersion ||
		checkpoint.FIFO != ProjectionFIFO || !isCurrencyUnit(checkpoint.BaseCurrency) ||
		!isNormalizedRequired(checkpoint.MarkSource) || checkpoint.MarkSource != strings.ToLower(checkpoint.MarkSource) ||
		!isNormalizedRequired(checkpoint.MarkNamespace) || checkpoint.MaxMarkAge <= 0 || checkpoint.TransactionCount <= 0 {
		return fmt.Errorf("projection checkpoint contract fields are invalid")
	}
	if checkpoint.MarkCount < 0 || checkpoint.LotCount < 0 || checkpoint.MatchCount < 0 || checkpoint.PositionCount < 0 {
		return fmt.Errorf("projection checkpoint counts cannot be negative")
	}
	if checkpoint.AsOf.IsZero() || checkpoint.AsOf.Location() != time.UTC || !hasPostgresTimestampPrecision(checkpoint.AsOf) {
		return fmt.Errorf("projection checkpoint as-of must use UTC microsecond precision")
	}
	if len(checkpoint.InputChecksum) != 64 || len(checkpoint.OutputChecksum) != 64 ||
		sha256Hex(checkpoint.PayloadBytes) != checkpoint.OutputChecksum {
		return fmt.Errorf("projection checkpoint checksum evidence is invalid")
	}
	if !isProjectionAttestationKeyID(checkpoint.AttestationKeyID) || len(checkpoint.AttestationHMAC) != sha256.Size {
		return fmt.Errorf("projection checkpoint attestation evidence is invalid")
	}
	expectedID := economicid.DeterministicUUID(
		"portfolio-projection-checkpoint", checkpoint.AccountID.String(), checkpoint.ProjectionType,
		checkpoint.ProjectionVersion, projectionTime(checkpoint.AsOf), checkpoint.InputChecksum,
	)
	if checkpoint.ID != expectedID {
		return fmt.Errorf("projection checkpoint ID does not match input identity")
	}
	var payload struct {
		CheckpointID           string            `json:"checkpoint_id"`
		ProjectionType         string            `json:"projection_type"`
		Version                string            `json:"version"`
		FIFO                   string            `json:"fifo"`
		AccountID              string            `json:"account_id"`
		BaseCurrency           string            `json:"base_currency"`
		AsOf                   string            `json:"as_of"`
		MarkSource             string            `json:"mark_source"`
		MarkNamespace          string            `json:"mark_namespace"`
		MaxMarkAgeMicroseconds json.Number       `json:"max_mark_age_microseconds"`
		ThroughTransactionID   string            `json:"through_transaction_id"`
		TransactionCount       int               `json:"transaction_count"`
		InputChecksum          string            `json:"input_checksum"`
		Marks                  []json.RawMessage `json:"marks"`
		Lots                   []json.RawMessage `json:"lots"`
		Matches                []json.RawMessage `json:"matches"`
		Positions              []json.RawMessage `json:"positions"`
	}
	decoder := json.NewDecoder(bytes.NewReader(checkpoint.PayloadBytes))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return fmt.Errorf("decode projection checkpoint payload: %w", err)
	}
	maxAge, err := payload.MaxMarkAgeMicroseconds.Int64()
	if err != nil {
		return fmt.Errorf("decode projection checkpoint mark age: %w", err)
	}
	if payload.CheckpointID != checkpoint.ID.String() || payload.ProjectionType != checkpoint.ProjectionType ||
		payload.Version != checkpoint.ProjectionVersion || payload.FIFO != checkpoint.FIFO ||
		payload.AccountID != checkpoint.AccountID.String() || payload.BaseCurrency != checkpoint.BaseCurrency ||
		payload.AsOf != projectionTime(checkpoint.AsOf) || payload.MarkSource != checkpoint.MarkSource ||
		payload.MarkNamespace != checkpoint.MarkNamespace || maxAge != checkpoint.MaxMarkAge.Microseconds() ||
		payload.ThroughTransactionID != checkpoint.ThroughTransactionID.String() ||
		payload.TransactionCount != checkpoint.TransactionCount || payload.InputChecksum != checkpoint.InputChecksum ||
		len(payload.Marks) != checkpoint.MarkCount || len(payload.Lots) != checkpoint.LotCount ||
		len(payload.Matches) != checkpoint.MatchCount || len(payload.Positions) != checkpoint.PositionCount {
		return fmt.Errorf("projection checkpoint payload does not match relational envelope")
	}
	return nil
}

func isProjectionAttestationKeyID(value string) bool {
	if len(value) == 0 || len(value) > 128 || value != strings.TrimSpace(value) || value != strings.ToLower(value) {
		return false
	}
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		if index > 0 && (character == '.' || character == '_' || character == '-') {
			continue
		}
		return false
	}
	return true
}

func normalizeProjectionBoundary(request ProjectionRequest, baseCurrency string) (ProjectionRequest, string, error) {
	request.AsOf = request.AsOf.UTC().Truncate(time.Microsecond)
	request.MarkSource = strings.ToLower(strings.TrimSpace(request.MarkSource))
	request.MarkNamespace = strings.TrimSpace(request.MarkNamespace)
	currency := strings.ToUpper(strings.TrimSpace(baseCurrency))
	if request.AccountID == uuid.Nil || request.AsOf.IsZero() {
		return request, currency, fmt.Errorf("projection account and as-of time are required")
	}
	if !isNormalizedRequired(request.MarkSource) || request.MarkSource != strings.ToLower(request.MarkSource) ||
		!isNormalizedRequired(request.MarkNamespace) || request.MaxMarkAge <= 0 {
		return request, currency, fmt.Errorf("projection mark source, namespace, and positive maximum age are required")
	}
	request.MaxMarkAge = request.MaxMarkAge.Truncate(time.Microsecond)
	if request.MaxMarkAge <= 0 || !isCurrencyUnit(currency) {
		return request, currency, fmt.Errorf("projection base currency or maximum mark age is invalid")
	}
	return request, currency, nil
}

func eligibleProjectionMechanics(candidates []ProjectionMechanics, transactions []*Transaction) (map[uuid.UUID]ProjectionMechanics, error) {
	eligible := make(map[uuid.UUID]struct{}, len(transactions))
	for _, transaction := range transactions {
		eligible[transaction.ID] = struct{}{}
	}
	result := make(map[uuid.UUID]ProjectionMechanics)
	for _, mechanics := range candidates {
		if _, ok := eligible[mechanics.TransactionID]; !ok {
			continue
		}
		if err := mechanics.Validate(); err != nil {
			return nil, fmt.Errorf("projection mechanics for transaction %s: %w", mechanics.TransactionID, err)
		}
		if _, duplicate := result[mechanics.TransactionID]; duplicate {
			return nil, fmt.Errorf("duplicate projection mechanics for transaction %s", mechanics.TransactionID)
		}
		result[mechanics.TransactionID] = mechanics
	}
	return result, nil
}

func (state *projectionState) applyEconomicTransaction(transaction *Transaction, mechanics ProjectionMechanics, cash decimal.Decimal) error {
	if mechanics.ExecutionOriginType == "" || mechanics.ExecutionOriginID == "" ||
		mechanics.ReferenceType != transaction.ReferenceType || mechanics.ReferenceID != transaction.ReferenceID {
		return fmt.Errorf("transaction %s mechanics provenance does not match ledger reference", transaction.ID)
	}
	switch mechanics.EventType {
	case EconomicEventFillBuy, EconomicEventFillSell:
		movement, err := projectionInstrumentMovement(transaction, "inventory", mechanics.InstrumentID)
		if err != nil {
			return err
		}
		if mechanics.EventType == EconomicEventFillBuy && !movement.IsPositive() ||
			mechanics.EventType == EconomicEventFillSell && !movement.IsNegative() {
			return fmt.Errorf("fill transaction %s inventory direction is invalid", transaction.ID)
		}
		if !movement.Abs().Equal(*mechanics.Quantity) {
			return fmt.Errorf("fill transaction %s inventory does not match normalization quantity", transaction.ID)
		}
		expectedCash := mechanics.Quantity.Mul(*mechanics.Price).Mul(mechanics.PrimaryMultiplier)
		if mechanics.EventType == EconomicEventFillBuy {
			expectedCash = expectedCash.Neg()
		}
		if mechanics.CostAmount != nil {
			if mechanics.CostKind == CostKindFee {
				expectedCash = expectedCash.Sub(*mechanics.CostAmount)
			} else {
				expectedCash = expectedCash.Add(*mechanics.CostAmount)
			}
		}
		if !cash.Equal(expectedCash) {
			return fmt.Errorf("fill transaction %s cash does not match normalization mechanics", transaction.ID)
		}
		return state.applyMovement(transaction, mechanics, mechanics.InstrumentID, movement, cash, mechanics.PrimaryMultiplier, "trade", false)
	case EconomicEventFee, EconomicEventRebate:
		if projectionHasAssetInventory(transaction) {
			return fmt.Errorf("standalone cost transaction %s contains inventory", transaction.ID)
		}
		if mechanics.EventType == EconomicEventFee && !cash.IsNegative() || mechanics.EventType == EconomicEventRebate && !cash.IsPositive() {
			return fmt.Errorf("standalone cost transaction %s cash direction is invalid", transaction.ID)
		}
		expectedCash := *mechanics.CostAmount
		if mechanics.EventType == EconomicEventFee {
			expectedCash = expectedCash.Neg()
		}
		if !cash.Equal(expectedCash) {
			return fmt.Errorf("standalone cost transaction %s cash does not match normalization amount", transaction.ID)
		}
		state.realized = state.realized.Add(cash)
		return nil
	case EconomicEventOptionCashSettlement, EconomicEventOptionExpiration, EconomicEventPredictionPayout:
		movement, err := projectionInstrumentMovement(transaction, "inventory-settlement", mechanics.InstrumentID)
		if err != nil {
			return err
		}
		if !movement.Equal(mechanics.PositionQuantity.Neg()) ||
			!cash.Equal(mechanics.PositionQuantity.Mul(*mechanics.SettlementPrice).Mul(mechanics.PrimaryMultiplier)) {
			return fmt.Errorf("settlement transaction %s postings do not match normalization mechanics", transaction.ID)
		}
		return state.applyMovement(transaction, mechanics, mechanics.InstrumentID, movement, cash, mechanics.PrimaryMultiplier, "settlement", true)
	case EconomicEventOptionExercise, EconomicEventOptionAssignment:
		return state.applyPhysicalOption(transaction, mechanics, cash)
	default:
		return fmt.Errorf("ledger transaction type %q is unsupported by projection %s", transaction.EventType, PortfolioProjectionVersion)
	}
}

func (state *projectionState) applyMovement(
	transaction *Transaction,
	mechanics ProjectionMechanics,
	instrumentID uuid.UUID,
	movement, eventCash, multiplier decimal.Decimal,
	disposition string,
	requireExactClose bool,
) error {
	if movement.IsZero() || instrumentID == uuid.Nil || !multiplier.IsPositive() {
		return fmt.Errorf("transaction %s has invalid inventory movement mechanics", transaction.ID)
	}
	type closeSegment struct {
		lotIndex int
		quantity decimal.Decimal
	}
	segments := make([]closeSegment, 0)
	remainingMovement := movement
	for index := range state.lots {
		lot := &state.lots[index]
		if lot.InstrumentID != instrumentID || lot.RemainingQuantity.IsZero() ||
			lot.RemainingQuantity.Sign() == movement.Sign() {
			continue
		}
		closeQuantity := decimal.Min(lot.RemainingQuantity.Abs(), remainingMovement.Abs())
		segments = append(segments, closeSegment{lotIndex: index, quantity: closeQuantity})
		remainingMovement = remainingMovement.Sub(decimal.NewFromInt(int64(movement.Sign())).Mul(closeQuantity))
		if remainingMovement.IsZero() {
			break
		}
	}
	if requireExactClose && !remainingMovement.IsZero() {
		return fmt.Errorf("settlement transaction %s does not exactly close existing signed inventory", transaction.ID)
	}
	segmentCount := len(segments)
	if !remainingMovement.IsZero() {
		segmentCount++
	}
	if segmentCount == 0 {
		return fmt.Errorf("transaction %s produced no inventory segment", transaction.ID)
	}
	remainingCash := eventCash
	remainingQuantity := movement.Abs()
	matchOrdinal := 0
	for segmentIndex, segment := range segments {
		closingCash := allocateProjectionAmount(eventCash, segment.quantity, movement.Abs(), &remainingCash, &remainingQuantity, segmentIndex == segmentCount-1)
		lot := &state.lots[segment.lotIndex]
		openingCash := lot.RemainingOpeningCash
		if !segment.quantity.Equal(lot.RemainingQuantity.Abs()) {
			openingCash = lot.RemainingOpeningCash.Mul(segment.quantity).Div(lot.RemainingQuantity.Abs()).Round(12)
		}
		movementPart := decimal.NewFromInt(int64(movement.Sign())).Mul(segment.quantity)
		lot.RemainingQuantity = lot.RemainingQuantity.Add(movementPart)
		lot.RemainingOpeningCash = lot.RemainingOpeningCash.Sub(openingCash)
		if lot.RemainingQuantity.IsZero() {
			lot.RemainingOpeningCash = decimal.Zero
		}
		realized := openingCash.Add(closingCash)
		state.realized = state.realized.Add(realized)
		state.matches = append(state.matches, ProjectionMatch{
			ID:                   economicid.DeterministicUUID("portfolio-projection-match", lot.ID.String(), transaction.ID.String(), fmt.Sprintf("%d", matchOrdinal)),
			LotID:                lot.ID,
			InstrumentID:         instrumentID,
			ClosingTransactionID: transaction.ID,
			OpeningOriginType:    lot.OpeningOriginType,
			OpeningOriginID:      lot.OpeningOriginID,
			OpeningReferenceType: lot.OpeningReferenceType,
			OpeningReferenceID:   lot.OpeningReferenceID,
			ClosingOriginType:    mechanics.ExecutionOriginType,
			ClosingOriginID:      mechanics.ExecutionOriginID,
			ClosingReferenceType: mechanics.ReferenceType,
			ClosingReferenceID:   mechanics.ReferenceID,
			ClosedAt:             transaction.EffectiveAt,
			Quantity:             segment.quantity,
			OpeningCash:          openingCash,
			ClosingCash:          closingCash,
			RealizedPnL:          realized,
			Disposition:          disposition,
		})
		matchOrdinal++
	}
	if !remainingMovement.IsZero() {
		openingCash := remainingCash
		state.openLot(transaction, mechanics, instrumentID, remainingMovement, openingCash, multiplier)
	}
	state.activity[instrumentID] = struct{}{}
	return nil
}

func (state *projectionState) openLot(transaction *Transaction, mechanics ProjectionMechanics, instrumentID uuid.UUID, quantity, cash, multiplier decimal.Decimal) {
	segment := 0
	for _, lot := range state.lots {
		if lot.OpeningTransactionID == transaction.ID && lot.InstrumentID == instrumentID {
			segment++
		}
	}
	id := economicid.DeterministicUUID("portfolio-projection-lot", transaction.ID.String(), instrumentID.String(), fmt.Sprintf("%d", segment))
	state.lots = append(state.lots, ProjectionLot{
		ID:                   id,
		InstrumentID:         instrumentID,
		OpeningTransactionID: transaction.ID,
		OpeningOriginType:    mechanics.ExecutionOriginType,
		OpeningOriginID:      mechanics.ExecutionOriginID,
		OpeningReferenceType: mechanics.ReferenceType,
		OpeningReferenceID:   mechanics.ReferenceID,
		OpenedAt:             transaction.EffectiveAt,
		Quantity:             quantity,
		RemainingQuantity:    quantity,
		OpeningCash:          cash,
		RemainingOpeningCash: cash,
		MarkMultiplier:       multiplier,
	})
}

func (state *projectionState) applyPhysicalOption(transaction *Transaction, mechanics ProjectionMechanics, strikeCash decimal.Decimal) error {
	optionMovement, err := projectionInstrumentMovement(transaction, "option-close", mechanics.InstrumentID)
	if err != nil {
		return err
	}
	underlyingMovement, err := projectionInstrumentMovement(transaction, "underlying-delivery", mechanics.SecondaryInstrumentID)
	if err != nil {
		return err
	}
	expectedUnderlyingMovement := mechanics.PositionQuantity.Mul(*mechanics.DeliverableQuantity)
	if mechanics.OptionContractType == instrument.OptionContractPut {
		expectedUnderlyingMovement = expectedUnderlyingMovement.Neg()
	}
	expectedStrikeCash := expectedUnderlyingMovement.Mul(*mechanics.StrikePrice).Neg()
	if !optionMovement.Equal(mechanics.PositionQuantity.Neg()) ||
		!underlyingMovement.Equal(expectedUnderlyingMovement) || !strikeCash.Equal(expectedStrikeCash) {
		return fmt.Errorf("physical option transaction %s postings do not match immutable terms", transaction.ID)
	}
	remaining := optionMovement
	transferredCash := decimal.Zero
	matchOrdinal := 0
	for index := range state.lots {
		lot := &state.lots[index]
		if lot.InstrumentID != mechanics.InstrumentID || lot.RemainingQuantity.IsZero() ||
			lot.RemainingQuantity.Sign() == optionMovement.Sign() {
			continue
		}
		closeQuantity := decimal.Min(lot.RemainingQuantity.Abs(), remaining.Abs())
		openingCash := lot.RemainingOpeningCash
		if !closeQuantity.Equal(lot.RemainingQuantity.Abs()) {
			openingCash = lot.RemainingOpeningCash.Mul(closeQuantity).Div(lot.RemainingQuantity.Abs()).Round(12)
		}
		lot.RemainingQuantity = lot.RemainingQuantity.Add(decimal.NewFromInt(int64(optionMovement.Sign())).Mul(closeQuantity))
		lot.RemainingOpeningCash = lot.RemainingOpeningCash.Sub(openingCash)
		if lot.RemainingQuantity.IsZero() {
			lot.RemainingOpeningCash = decimal.Zero
		}
		transferredCash = transferredCash.Add(openingCash)
		state.matches = append(state.matches, ProjectionMatch{
			ID:                   economicid.DeterministicUUID("portfolio-projection-match", lot.ID.String(), transaction.ID.String(), fmt.Sprintf("%d", matchOrdinal)),
			LotID:                lot.ID,
			InstrumentID:         mechanics.InstrumentID,
			ClosingTransactionID: transaction.ID,
			OpeningOriginType:    lot.OpeningOriginType,
			OpeningOriginID:      lot.OpeningOriginID,
			OpeningReferenceType: lot.OpeningReferenceType,
			OpeningReferenceID:   lot.OpeningReferenceID,
			ClosingOriginType:    mechanics.ExecutionOriginType,
			ClosingOriginID:      mechanics.ExecutionOriginID,
			ClosingReferenceType: mechanics.ReferenceType,
			ClosingReferenceID:   mechanics.ReferenceID,
			ClosedAt:             transaction.EffectiveAt,
			Quantity:             closeQuantity,
			OpeningCash:          openingCash,
			ClosingCash:          openingCash.Neg(),
			RealizedPnL:          decimal.Zero,
			Disposition:          "basis_transfer",
		})
		matchOrdinal++
		remaining = remaining.Sub(decimal.NewFromInt(int64(optionMovement.Sign())).Mul(closeQuantity))
		if remaining.IsZero() {
			break
		}
	}
	if !remaining.IsZero() {
		return fmt.Errorf("physical option transaction %s does not exactly close existing signed option inventory", transaction.ID)
	}
	state.activity[mechanics.InstrumentID] = struct{}{}
	return state.applyMovement(
		transaction,
		mechanics,
		mechanics.SecondaryInstrumentID,
		underlyingMovement,
		strikeCash.Add(transferredCash),
		mechanics.SecondaryMultiplier,
		"physical_delivery",
		false,
	)
}

func allocateProjectionAmount(total, quantity, totalQuantity decimal.Decimal, remainingAmount, remainingQuantity *decimal.Decimal, final bool) decimal.Decimal {
	if final {
		allocated := *remainingAmount
		*remainingAmount = decimal.Zero
		*remainingQuantity = decimal.Zero
		return allocated
	}
	allocated := total.Mul(quantity).Div(totalQuantity).Round(12)
	*remainingAmount = remainingAmount.Sub(allocated)
	*remainingQuantity = remainingQuantity.Sub(quantity)
	return allocated
}

func (state *projectionState) finalize(request ProjectionRequest, currency string, candidates []*MarkObservation) ([]ProjectionMark, []ProjectionPosition, ProjectionTotals, error) {
	selected := make(map[uuid.UUID]*MarkObservation)
	for _, candidate := range candidates {
		if candidate == nil {
			return nil, nil, ProjectionTotals{}, fmt.Errorf("projection mark cannot be nil")
		}
		if err := candidate.Validate(); err != nil {
			return nil, nil, ProjectionTotals{}, fmt.Errorf("projection mark %s: %w", candidate.ID, err)
		}
		if candidate.PriceCurrency != currency || candidate.Source != request.MarkSource ||
			candidate.SourceNamespace != request.MarkNamespace || candidate.EffectiveAt.After(request.AsOf) ||
			candidate.ObservedAt.After(request.AsOf) || request.AsOf.Sub(candidate.EffectiveAt) > request.MaxMarkAge {
			continue
		}
		current := selected[candidate.InstrumentID]
		if current == nil || projectionMarkLater(candidate, current) {
			selected[candidate.InstrumentID] = candidate
		}
	}

	instrumentIDs := make([]uuid.UUID, 0, len(state.activity))
	for instrumentID := range state.activity {
		instrumentIDs = append(instrumentIDs, instrumentID)
	}
	sort.Slice(instrumentIDs, func(left, right int) bool { return instrumentIDs[left].String() < instrumentIDs[right].String() })
	marks := make([]ProjectionMark, 0)
	positions := make([]ProjectionPosition, 0, len(instrumentIDs))
	marketValue := decimal.Zero
	unrealized := decimal.Zero
	for _, instrumentID := range instrumentIDs {
		position := ProjectionPosition{InstrumentID: instrumentID}
		for _, lot := range state.lots {
			if lot.InstrumentID != instrumentID {
				continue
			}
			position.Quantity = position.Quantity.Add(lot.RemainingQuantity)
			position.RemainingOpeningCash = position.RemainingOpeningCash.Add(lot.RemainingOpeningCash)
			if !lot.RemainingQuantity.IsZero() {
				position.OpenLotCount++
			}
		}
		for _, match := range state.matches {
			if match.InstrumentID == instrumentID {
				position.RealizedPnL = position.RealizedPnL.Add(match.RealizedPnL)
			}
		}
		position.Open = !position.Quantity.IsZero()
		if position.Open {
			mark := selected[instrumentID]
			if mark == nil {
				return nil, nil, ProjectionTotals{}, fmt.Errorf("open instrument %s has no fresh canonical mark for %s/%s", instrumentID, request.MarkSource, request.MarkNamespace)
			}
			position.MarkObservationID = mark.ID
			for _, lot := range state.lots {
				if lot.InstrumentID == instrumentID && !lot.RemainingQuantity.IsZero() {
					position.MarketValue = position.MarketValue.Add(lot.RemainingQuantity.Mul(mark.Price).Mul(lot.MarkMultiplier))
				}
			}
			position.UnrealizedPnL = position.MarketValue.Add(position.RemainingOpeningCash)
			marks = append(marks, projectionMarkFromObservation(mark))
			marketValue = marketValue.Add(position.MarketValue)
			unrealized = unrealized.Add(position.UnrealizedPnL)
		}
		positions = append(positions, position)
	}
	sort.Slice(marks, func(left, right int) bool {
		return marks[left].InstrumentID.String() < marks[right].InstrumentID.String()
	})
	totals := ProjectionTotals{
		Cash:          state.cash,
		NetCapital:    state.netCapital,
		Fees:          state.fees,
		Rebates:       state.rebates,
		RealizedPnL:   state.realized,
		UnrealizedPnL: unrealized,
		MarketValue:   marketValue,
	}
	totals.Equity = totals.Cash.Add(totals.MarketValue)
	totals.TotalPnL = totals.Equity.Sub(totals.NetCapital)
	if !totals.RealizedPnL.Add(totals.UnrealizedPnL).Equal(totals.TotalPnL) {
		return nil, nil, ProjectionTotals{}, fmt.Errorf(
			"projection P&L equation failed: realized %s + unrealized %s != total %s",
			totals.RealizedPnL, totals.UnrealizedPnL, totals.TotalPnL,
		)
	}
	return marks, positions, totals, nil
}

func projectionCashAndCosts(transaction *Transaction, currency string) (decimal.Decimal, decimal.Decimal, decimal.Decimal, error) {
	cash := decimal.Zero
	fees := decimal.Zero
	rebates := decimal.Zero
	for _, posting := range transaction.Postings {
		if posting.UnitKind == UnitKindCurrency && posting.Unit != currency {
			return decimal.Zero, decimal.Zero, decimal.Zero, fmt.Errorf("transaction %s contains foreign currency %s", transaction.ID, posting.Unit)
		}
		switch posting.LedgerAccount {
		case "asset:cash":
			if posting.UnitKind != UnitKindCurrency {
				return decimal.Zero, decimal.Zero, decimal.Zero, fmt.Errorf("transaction %s cash posting has a non-currency unit", transaction.ID)
			}
			cash = cash.Add(posting.Amount)
		case "expense:fees":
			if posting.UnitKind != UnitKindCurrency || !posting.Amount.IsPositive() {
				return decimal.Zero, decimal.Zero, decimal.Zero, fmt.Errorf("transaction %s fee posting is invalid", transaction.ID)
			}
			fees = fees.Add(posting.Amount)
		case "income:rebates":
			if posting.UnitKind != UnitKindCurrency || !posting.Amount.IsNegative() {
				return decimal.Zero, decimal.Zero, decimal.Zero, fmt.Errorf("transaction %s rebate posting is invalid", transaction.ID)
			}
			rebates = rebates.Add(posting.Amount.Abs())
		}
	}
	return cash, fees, rebates, nil
}

func projectionInstrumentMovement(transaction *Transaction, key string, instrumentID uuid.UUID) (decimal.Decimal, error) {
	for _, posting := range transaction.Postings {
		if posting.IdempotencyKey != key {
			continue
		}
		if posting.UnitKind != UnitKindInstrument || posting.Unit != instrumentID.String() ||
			!strings.HasPrefix(posting.LedgerAccount, "asset:") {
			return decimal.Zero, fmt.Errorf("transaction %s posting %q does not match canonical instrument mechanics", transaction.ID, key)
		}
		return posting.Amount, nil
	}
	return decimal.Zero, fmt.Errorf("transaction %s is missing posting %q", transaction.ID, key)
}

func projectionHasAssetInventory(transaction *Transaction) bool {
	for _, posting := range transaction.Postings {
		if posting.UnitKind == UnitKindInstrument && strings.HasPrefix(posting.LedgerAccount, "asset:") {
			return true
		}
	}
	return false
}

func projectionTransactionLess(left, right *Transaction) bool {
	if !left.EffectiveAt.Equal(right.EffectiveAt) {
		return left.EffectiveAt.Before(right.EffectiveAt)
	}
	if !left.ObservedAt.Equal(right.ObservedAt) {
		return left.ObservedAt.Before(right.ObservedAt)
	}
	return left.ID.String() < right.ID.String()
}

func projectionMarkLater(left, right *MarkObservation) bool {
	if !left.EffectiveAt.Equal(right.EffectiveAt) {
		return left.EffectiveAt.After(right.EffectiveAt)
	}
	if !left.ObservedAt.Equal(right.ObservedAt) {
		return left.ObservedAt.After(right.ObservedAt)
	}
	return left.ID.String() > right.ID.String()
}

func projectionMarkFromObservation(mark *MarkObservation) ProjectionMark {
	return ProjectionMark{
		ID: mark.ID, InstrumentID: mark.InstrumentID, Price: mark.Price,
		PriceCurrency: mark.PriceCurrency, Source: mark.Source, SourceNamespace: mark.SourceNamespace,
		SourceObservationID: mark.SourceObservationID, SourceRevision: mark.SourceRevision,
		EffectiveAt: mark.EffectiveAt, ObservedAt: mark.ObservedAt, Metadata: append(json.RawMessage(nil), mark.Metadata...),
	}
}

func projectionCheckpointID(projection *PortfolioProjection) uuid.UUID {
	return economicid.DeterministicUUID(
		"portfolio-projection-checkpoint",
		projection.AccountID.String(),
		projection.ProjectionType,
		projection.Version,
		projection.AsOf.Format(projectionTimestampLayout),
		projection.InputChecksum,
	)
}

func projectionInputChecksum(
	request ProjectionRequest,
	currency string,
	transactions []*Transaction,
	mechanics map[uuid.UUID]ProjectionMechanics,
	marks []ProjectionMark,
) (string, error) {
	type canonicalPosting struct {
		ID             string          `json:"id"`
		IdempotencyKey string          `json:"idempotency_key"`
		LedgerAccount  string          `json:"ledger_account"`
		UnitKind       string          `json:"unit_kind"`
		Unit           string          `json:"unit"`
		Amount         string          `json:"amount"`
		Metadata       json.RawMessage `json:"metadata"`
	}
	type canonicalTransaction struct {
		ID             string             `json:"id"`
		AccountID      string             `json:"account_id"`
		EventType      string             `json:"event_type"`
		IdempotencyKey string             `json:"idempotency_key"`
		OriginType     string             `json:"origin_type"`
		OriginID       string             `json:"origin_id"`
		ReferenceType  string             `json:"reference_type"`
		ReferenceID    string             `json:"reference_id"`
		EffectiveAt    string             `json:"effective_at"`
		ObservedAt     string             `json:"observed_at"`
		Metadata       json.RawMessage    `json:"metadata"`
		Postings       []canonicalPosting `json:"postings"`
	}
	type canonicalMechanics struct {
		NormalizationID       string `json:"normalization_id"`
		SourceEventID         string `json:"source_event_id"`
		TransactionID         string `json:"transaction_id"`
		EventType             string `json:"event_type"`
		NormalizerVersion     string `json:"normalizer_version"`
		ExecutionOriginType   string `json:"execution_origin_type"`
		ExecutionOriginID     string `json:"execution_origin_id"`
		ReferenceType         string `json:"reference_type"`
		ReferenceID           string `json:"reference_id"`
		Venue                 string `json:"venue"`
		InstrumentID          string `json:"instrument_id"`
		SecondaryInstrumentID string `json:"secondary_instrument_id"`
		VenueContractID       string `json:"venue_contract_id"`
		OptionTermsID         string `json:"option_terms_id"`
		CashCurrency          string `json:"cash_currency"`
		Quantity              string `json:"quantity"`
		Price                 string `json:"price"`
		CostKind              string `json:"cost_kind"`
		CostCurrency          string `json:"cost_currency"`
		CostAmount            string `json:"cost_amount"`
		PositionQuantity      string `json:"position_quantity"`
		SettlementPrice       string `json:"settlement_price"`
		OptionContractType    string `json:"option_contract_type"`
		StrikePrice           string `json:"strike_price"`
		DeliverableQuantity   string `json:"deliverable_quantity"`
		PrimaryMultiplier     string `json:"primary_multiplier"`
		SecondaryMultiplier   string `json:"secondary_multiplier"`
	}
	canonicalTransactions := make([]canonicalTransaction, 0, len(transactions))
	canonicalMechanicsRows := make([]canonicalMechanics, 0, len(mechanics))
	for _, transaction := range transactions {
		metadata, err := canonicalJSON(transaction.Metadata)
		if err != nil {
			return "", err
		}
		postings := append([]Posting(nil), transaction.Postings...)
		sort.Slice(postings, func(left, right int) bool {
			if postings[left].IdempotencyKey != postings[right].IdempotencyKey {
				return postings[left].IdempotencyKey < postings[right].IdempotencyKey
			}
			return postings[left].ID.String() < postings[right].ID.String()
		})
		canonicalPostings := make([]canonicalPosting, 0, len(postings))
		for _, posting := range postings {
			postingMetadata, err := canonicalJSON(posting.Metadata)
			if err != nil {
				return "", err
			}
			canonicalPostings = append(canonicalPostings, canonicalPosting{
				ID: posting.ID.String(), IdempotencyKey: posting.IdempotencyKey, LedgerAccount: posting.LedgerAccount,
				UnitKind: string(posting.UnitKind), Unit: posting.Unit, Amount: posting.Amount.String(), Metadata: postingMetadata,
			})
		}
		canonicalTransactions = append(canonicalTransactions, canonicalTransaction{
			ID: transaction.ID.String(), AccountID: transaction.AccountID.String(), EventType: transaction.EventType,
			IdempotencyKey: transaction.IdempotencyKey, OriginType: transaction.OriginType, OriginID: transaction.OriginID,
			ReferenceType: transaction.ReferenceType, ReferenceID: transaction.ReferenceID,
			EffectiveAt: projectionTime(transaction.EffectiveAt), ObservedAt: projectionTime(transaction.ObservedAt),
			Metadata: metadata, Postings: canonicalPostings,
		})
		if value, ok := mechanics[transaction.ID]; ok {
			canonicalMechanicsRows = append(canonicalMechanicsRows, canonicalMechanics{
				NormalizationID: value.NormalizationID.String(), SourceEventID: value.SourceEventID.String(),
				TransactionID: value.TransactionID.String(), EventType: string(value.EventType), NormalizerVersion: value.NormalizerVersion,
				ExecutionOriginType: string(value.ExecutionOriginType),
				ExecutionOriginID:   value.ExecutionOriginID, ReferenceType: value.ReferenceType, ReferenceID: value.ReferenceID,
				Venue:        value.Venue,
				InstrumentID: projectionUUID(value.InstrumentID), SecondaryInstrumentID: projectionUUID(value.SecondaryInstrumentID),
				VenueContractID: projectionUUID(value.VenueContractID), OptionTermsID: projectionUUID(value.OptionTermsID),
				CashCurrency: value.CashCurrency, Quantity: projectionDecimalPointer(value.Quantity), Price: projectionDecimalPointer(value.Price),
				CostKind: string(value.CostKind), CostCurrency: value.CostCurrency, CostAmount: projectionDecimalPointer(value.CostAmount),
				PositionQuantity: projectionDecimalPointer(value.PositionQuantity), SettlementPrice: projectionDecimalPointer(value.SettlementPrice),
				OptionContractType: string(value.OptionContractType), StrikePrice: projectionDecimalPointer(value.StrikePrice),
				DeliverableQuantity: projectionDecimalPointer(value.DeliverableQuantity),
				PrimaryMultiplier:   value.PrimaryMultiplier.String(), SecondaryMultiplier: value.SecondaryMultiplier.String(),
			})
		}
	}
	canonicalMarks, err := canonicalProjectionMarks(marks)
	if err != nil {
		return "", err
	}
	payload := struct {
		Version                string                 `json:"version"`
		AccountID              string                 `json:"account_id"`
		BaseCurrency           string                 `json:"base_currency"`
		AsOf                   string                 `json:"as_of"`
		MarkSource             string                 `json:"mark_source"`
		MarkNamespace          string                 `json:"mark_namespace"`
		MaxMarkAgeMicroseconds int64                  `json:"max_mark_age_microseconds"`
		Transactions           []canonicalTransaction `json:"transactions"`
		Mechanics              []canonicalMechanics   `json:"mechanics"`
		Marks                  []canonicalMark        `json:"marks"`
	}{
		Version: PortfolioProjectionVersion, AccountID: request.AccountID.String(), BaseCurrency: currency,
		AsOf: projectionTime(request.AsOf), MarkSource: request.MarkSource, MarkNamespace: request.MarkNamespace,
		MaxMarkAgeMicroseconds: request.MaxMarkAge.Microseconds(), Transactions: canonicalTransactions,
		Mechanics: canonicalMechanicsRows, Marks: canonicalMarks,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal canonical projection input: %w", err)
	}
	return sha256Hex(encoded), nil
}

type canonicalMark struct {
	ID                  string          `json:"id"`
	InstrumentID        string          `json:"instrument_id"`
	Price               string          `json:"price"`
	PriceCurrency       string          `json:"price_currency"`
	Source              string          `json:"source"`
	SourceNamespace     string          `json:"source_namespace"`
	SourceObservationID string          `json:"source_observation_id"`
	SourceRevision      string          `json:"source_revision"`
	EffectiveAt         string          `json:"effective_at"`
	ObservedAt          string          `json:"observed_at"`
	Metadata            json.RawMessage `json:"metadata"`
}

func canonicalProjectionMarks(marks []ProjectionMark) ([]canonicalMark, error) {
	result := make([]canonicalMark, 0, len(marks))
	for _, mark := range marks {
		metadata, err := canonicalJSON(mark.Metadata)
		if err != nil {
			return nil, err
		}
		result = append(result, canonicalMark{
			ID: mark.ID.String(), InstrumentID: mark.InstrumentID.String(), Price: mark.Price.String(),
			PriceCurrency: mark.PriceCurrency, Source: mark.Source, SourceNamespace: mark.SourceNamespace,
			SourceObservationID: mark.SourceObservationID, SourceRevision: mark.SourceRevision,
			EffectiveAt: projectionTime(mark.EffectiveAt), ObservedAt: projectionTime(mark.ObservedAt), Metadata: metadata,
		})
	}
	return result, nil
}

func marshalProjectionPayload(projection *PortfolioProjection) ([]byte, error) {
	canonicalMarks, err := canonicalProjectionMarks(projection.Marks)
	if err != nil {
		return nil, err
	}
	type canonicalLot struct {
		ID                   string `json:"id"`
		InstrumentID         string `json:"instrument_id"`
		OpeningTransactionID string `json:"opening_transaction_id"`
		OpeningOriginType    string `json:"opening_origin_type"`
		OpeningOriginID      string `json:"opening_origin_id"`
		OpeningReferenceType string `json:"opening_reference_type"`
		OpeningReferenceID   string `json:"opening_reference_id"`
		OpenedAt             string `json:"opened_at"`
		Quantity             string `json:"quantity"`
		RemainingQuantity    string `json:"remaining_quantity"`
		OpeningCash          string `json:"opening_cash"`
		RemainingOpeningCash string `json:"remaining_opening_cash"`
		MarkMultiplier       string `json:"mark_multiplier"`
	}
	type canonicalMatch struct {
		ID                   string `json:"id"`
		LotID                string `json:"lot_id"`
		InstrumentID         string `json:"instrument_id"`
		ClosingTransactionID string `json:"closing_transaction_id"`
		OpeningOriginType    string `json:"opening_origin_type"`
		OpeningOriginID      string `json:"opening_origin_id"`
		OpeningReferenceType string `json:"opening_reference_type"`
		OpeningReferenceID   string `json:"opening_reference_id"`
		ClosingOriginType    string `json:"closing_origin_type"`
		ClosingOriginID      string `json:"closing_origin_id"`
		ClosingReferenceType string `json:"closing_reference_type"`
		ClosingReferenceID   string `json:"closing_reference_id"`
		ClosedAt             string `json:"closed_at"`
		Quantity             string `json:"quantity"`
		OpeningCash          string `json:"opening_cash"`
		ClosingCash          string `json:"closing_cash"`
		RealizedPnL          string `json:"realized_pnl"`
		Disposition          string `json:"disposition"`
	}
	type canonicalPosition struct {
		InstrumentID         string `json:"instrument_id"`
		Open                 bool   `json:"open"`
		Quantity             string `json:"quantity"`
		RemainingOpeningCash string `json:"remaining_opening_cash"`
		RealizedPnL          string `json:"realized_pnl"`
		MarketValue          string `json:"market_value"`
		UnrealizedPnL        string `json:"unrealized_pnl"`
		MarkObservationID    string `json:"mark_observation_id"`
		OpenLotCount         int    `json:"open_lot_count"`
	}
	lots := make([]canonicalLot, 0, len(projection.Lots))
	for _, lot := range projection.Lots {
		lots = append(lots, canonicalLot{
			ID: lot.ID.String(), InstrumentID: lot.InstrumentID.String(), OpeningTransactionID: lot.OpeningTransactionID.String(),
			OpeningOriginType: string(lot.OpeningOriginType), OpeningOriginID: lot.OpeningOriginID,
			OpeningReferenceType: lot.OpeningReferenceType, OpeningReferenceID: lot.OpeningReferenceID,
			OpenedAt: projectionTime(lot.OpenedAt), Quantity: lot.Quantity.String(), RemainingQuantity: lot.RemainingQuantity.String(),
			OpeningCash: lot.OpeningCash.String(), RemainingOpeningCash: lot.RemainingOpeningCash.String(), MarkMultiplier: lot.MarkMultiplier.String(),
		})
	}
	matches := make([]canonicalMatch, 0, len(projection.Matches))
	for _, match := range projection.Matches {
		matches = append(matches, canonicalMatch{
			ID: match.ID.String(), LotID: match.LotID.String(), InstrumentID: match.InstrumentID.String(),
			ClosingTransactionID: match.ClosingTransactionID.String(), OpeningOriginType: string(match.OpeningOriginType),
			OpeningOriginID: match.OpeningOriginID, OpeningReferenceType: match.OpeningReferenceType,
			OpeningReferenceID: match.OpeningReferenceID, ClosingOriginType: string(match.ClosingOriginType),
			ClosingOriginID: match.ClosingOriginID, ClosingReferenceType: match.ClosingReferenceType,
			ClosingReferenceID: match.ClosingReferenceID, ClosedAt: projectionTime(match.ClosedAt),
			Quantity: match.Quantity.String(), OpeningCash: match.OpeningCash.String(), ClosingCash: match.ClosingCash.String(),
			RealizedPnL: match.RealizedPnL.String(), Disposition: match.Disposition,
		})
	}
	positions := make([]canonicalPosition, 0, len(projection.Positions))
	for _, position := range projection.Positions {
		positions = append(positions, canonicalPosition{
			InstrumentID: position.InstrumentID.String(), Open: position.Open, Quantity: position.Quantity.String(),
			RemainingOpeningCash: position.RemainingOpeningCash.String(), RealizedPnL: position.RealizedPnL.String(),
			MarketValue: position.MarketValue.String(), UnrealizedPnL: position.UnrealizedPnL.String(),
			MarkObservationID: projectionUUID(position.MarkObservationID), OpenLotCount: position.OpenLotCount,
		})
	}
	payload := struct {
		CheckpointID           string              `json:"checkpoint_id"`
		ProjectionType         string              `json:"projection_type"`
		Version                string              `json:"version"`
		FIFO                   string              `json:"fifo"`
		AccountID              string              `json:"account_id"`
		BaseCurrency           string              `json:"base_currency"`
		AsOf                   string              `json:"as_of"`
		MarkSource             string              `json:"mark_source"`
		MarkNamespace          string              `json:"mark_namespace"`
		MaxMarkAgeMicroseconds int64               `json:"max_mark_age_microseconds"`
		ThroughTransactionID   string              `json:"through_transaction_id"`
		TransactionCount       int                 `json:"transaction_count"`
		InputChecksum          string              `json:"input_checksum"`
		Marks                  []canonicalMark     `json:"marks"`
		Lots                   []canonicalLot      `json:"lots"`
		Matches                []canonicalMatch    `json:"matches"`
		Positions              []canonicalPosition `json:"positions"`
		Totals                 struct {
			Cash          string `json:"cash"`
			NetCapital    string `json:"net_capital"`
			Fees          string `json:"fees"`
			Rebates       string `json:"rebates"`
			RealizedPnL   string `json:"realized_pnl"`
			UnrealizedPnL string `json:"unrealized_pnl"`
			MarketValue   string `json:"market_value"`
			Equity        string `json:"equity"`
			TotalPnL      string `json:"total_pnl"`
		} `json:"totals"`
	}{
		CheckpointID: projection.CheckpointID.String(), ProjectionType: projection.ProjectionType,
		Version: projection.Version, FIFO: projection.FIFO, AccountID: projection.AccountID.String(),
		BaseCurrency: projection.BaseCurrency, AsOf: projectionTime(projection.AsOf), MarkSource: projection.MarkSource,
		MarkNamespace: projection.MarkNamespace, MaxMarkAgeMicroseconds: projection.MaxMarkAge.Microseconds(),
		ThroughTransactionID: projection.ThroughTransactionID.String(), TransactionCount: projection.TransactionCount,
		InputChecksum: projection.InputChecksum, Marks: canonicalMarks, Lots: lots, Matches: matches, Positions: positions,
	}
	payload.Totals.Cash = projection.Totals.Cash.String()
	payload.Totals.NetCapital = projection.Totals.NetCapital.String()
	payload.Totals.Fees = projection.Totals.Fees.String()
	payload.Totals.Rebates = projection.Totals.Rebates.String()
	payload.Totals.RealizedPnL = projection.Totals.RealizedPnL.String()
	payload.Totals.UnrealizedPnL = projection.Totals.UnrealizedPnL.String()
	payload.Totals.MarketValue = projection.Totals.MarketValue.String()
	payload.Totals.Equity = projection.Totals.Equity.String()
	payload.Totals.TotalPnL = projection.Totals.TotalPnL.String()
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical projection payload: %w", err)
	}
	return encoded, nil
}

func canonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode canonical JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("canonical JSON contains trailing values or syntax: %w", err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical JSON: %w", err)
	}
	return encoded, nil
}

func projectionTime(value time.Time) string {
	return value.UTC().Truncate(time.Microsecond).Format(projectionTimestampLayout)
}

func projectionUUID(value uuid.UUID) string {
	if value == uuid.Nil {
		return ""
	}
	return value.String()
}

func projectionDecimalPointer(value *decimal.Decimal) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func cloneEconomicDecimalPointer(value *decimal.Decimal) *decimal.Decimal {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
