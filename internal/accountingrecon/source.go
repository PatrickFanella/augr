package accountingrecon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
)

type SourceRequest struct {
	AccountID         uuid.UUID
	AsOf              time.Time
	ProjectionVersion string
	MarkSource        string
	MarkNamespace     string
	MaxMarkAge        time.Duration
}

// LegacyBalance is the compatibility balance envelope captured from the
// mutable paper broker. Binary-float provenance is preserved intentionally.
type LegacyBalance struct {
	Currency    string
	Cash        float64
	BuyingPower float64
	Equity      float64
}

// LegacyCapture is one lock-consistent read of the mutable paper broker. It
// remains compatibility evidence, never immutable economic truth.
type LegacyCapture struct {
	Balance    LegacyBalance
	Positions  []domain.Position
	CapturedAt time.Time
}

type LegacyAccountingReader interface {
	CaptureLegacyAccounting(context.Context) (LegacyCapture, error)
}

type LegacyPositionResolver interface {
	ResolveLegacyPosition(context.Context, domain.Position, time.Time) (uuid.UUID, error)
}

type LegacyPaperSource struct {
	reader    LegacyAccountingReader
	resolver  LegacyPositionResolver
	synthetic bool
}

func NewLegacyPaperSource(reader LegacyAccountingReader, resolver LegacyPositionResolver, synthetic bool) *LegacyPaperSource {
	return &LegacyPaperSource{reader: reader, resolver: resolver, synthetic: synthetic}
}

func (source *LegacyPaperSource) Capture(ctx context.Context, request SourceRequest, lease CaptureLease) (*Snapshot, error) {
	if source == nil || source.reader == nil || source.resolver == nil {
		return nil, fmt.Errorf("legacy paper accounting source dependencies are required")
	}
	if err := validateSourceLease(request, lease); err != nil {
		return nil, err
	}
	capture, err := source.reader.CaptureLegacyAccounting(ctx)
	if err != nil {
		return nil, fmt.Errorf("capture legacy paper accounting: %w", err)
	}
	if capture.CapturedAt.IsZero() {
		return nil, fmt.Errorf("legacy paper accounting capture time is required")
	}
	capture.CapturedAt = capture.CapturedAt.UTC().Truncate(time.Microsecond)
	if capture.CapturedAt.Before(request.AsOf) {
		return nil, fmt.Errorf("legacy paper accounting capture precedes as_of")
	}

	cash, err := exactLegacyFloat(capture.Balance.Cash)
	if err != nil {
		return nil, fmt.Errorf("legacy paper cash: %w", err)
	}
	buyingPower, err := exactLegacyFloat(capture.Balance.BuyingPower)
	if err != nil {
		return nil, fmt.Errorf("legacy paper buying power: %w", err)
	}
	equity, err := exactLegacyFloat(capture.Balance.Equity)
	if err != nil {
		return nil, fmt.Errorf("legacy paper equity: %w", err)
	}

	positionTotals := make(map[uuid.UUID]decimal.Decimal)
	positionCoverageComplete := true
	missing := []MissingFactInput{
		{FactKey: MetricFactKey(MetricFees), ReasonCode: MissingSourceUnavailable, EvidenceRef: "legacy-paper:fees-not-exposed"},
		{FactKey: MetricFactKey(MetricRealizedPnL), ReasonCode: MissingSourceUnavailable, EvidenceRef: "legacy-paper:realized-pnl-not-exposed"},
		{FactKey: MetricFactKey(MetricUnrealizedPnL), ReasonCode: MissingSourceUnavailable, EvidenceRef: "legacy-paper:unrealized-pnl-not-exposed"},
	}
	for _, position := range capture.Positions {
		quantity, conversionErr := exactLegacyFloat(position.Quantity)
		if conversionErr != nil {
			return nil, fmt.Errorf("legacy paper position %q quantity: %w", position.Ticker, conversionErr)
		}
		switch position.Side {
		case domain.PositionSideLong:
		case domain.PositionSideShort:
			quantity = quantity.Neg()
		default:
			return nil, fmt.Errorf("legacy paper position %q side %q is invalid", position.Ticker, position.Side)
		}
		instrumentID, resolveErr := source.resolver.ResolveLegacyPosition(ctx, position, request.AsOf)
		if resolveErr != nil || instrumentID == uuid.Nil {
			positionCoverageComplete = false
			identity := sha256.Sum256([]byte(legacyPositionIdentity(position)))
			missing = append(missing, MissingFactInput{
				FactKey:     "legacy_position:" + hex.EncodeToString(identity[:]) + ":identity",
				ReasonCode:  MissingInstrumentIdentity,
				EvidenceRef: "legacy-paper-position:" + strings.TrimSpace(position.Ticker),
			})
			continue
		}
		positionTotals[instrumentID] = positionTotals[instrumentID].Add(quantity)
	}
	positionInputs := make([]PositionInput, 0, len(positionTotals))
	for instrumentID, quantity := range positionTotals {
		positionInputs = append(positionInputs, PositionInput{InstrumentID: instrumentID, Quantity: quantity, Provenance: ProvenanceBinaryFloat})
	}

	evidenceBytes, err := canonicalLegacyCapture(capture)
	if err != nil {
		return nil, err
	}
	evidenceHash := sha256.Sum256(evidenceBytes)
	return NewSnapshot(SnapshotInput{
		Source: SourceLegacy, AccountID: request.AccountID, AsOf: request.AsOf,
		ObservedAt: capture.CapturedAt, Currency: strings.ToUpper(strings.TrimSpace(capture.Balance.Currency)),
		ProjectionVersion: request.ProjectionVersion, MarkSource: request.MarkSource,
		MarkNamespace: request.MarkNamespace, MaxMarkAge: request.MaxMarkAge,
		CaptureFenceID: lease.FenceID(), CaptureEpoch: lease.Epoch(),
		EvidenceID:       fmt.Sprintf("legacy-paper:%s:%d", lease.FenceID(), lease.Epoch()),
		EvidenceChecksum: hex.EncodeToString(evidenceHash[:]), Synthetic: source.synthetic,
		PositionCoverageComplete: positionCoverageComplete,
		Metrics: []MetricInput{
			{Kind: MetricCash, Value: cash, Provenance: ProvenanceBinaryFloat},
			{Kind: MetricBuyingPower, Value: buyingPower, Provenance: ProvenanceBinaryFloat},
			{Kind: MetricMarketValue, Value: equity.Sub(cash), Provenance: ProvenanceBinaryFloat},
			{Kind: MetricEquity, Value: equity, Provenance: ProvenanceBinaryFloat},
		},
		Positions: positionInputs, Missing: missing,
	})
}

type ProjectionSourceRepository interface {
	RebuildPortfolioProjection(context.Context, ledger.ProjectionRequest) (*ledger.PortfolioProjection, error)
	GetProjectionCheckpointByID(context.Context, uuid.UUID) (*ledger.ProjectionCheckpoint, error)
}

type LedgerProjectionSource struct {
	repository ProjectionSourceRepository
	synthetic  bool
}

func NewLedgerProjectionSource(repository ProjectionSourceRepository, synthetic bool) *LedgerProjectionSource {
	return &LedgerProjectionSource{repository: repository, synthetic: synthetic}
}

func (source *LedgerProjectionSource) Capture(ctx context.Context, request SourceRequest, lease CaptureLease) (*Snapshot, error) {
	if source == nil || source.repository == nil {
		return nil, fmt.Errorf("ledger projection source repository is required")
	}
	if err := validateSourceLease(request, lease); err != nil {
		return nil, err
	}
	projection, err := source.repository.RebuildPortfolioProjection(ctx, ledger.ProjectionRequest{
		AccountID: request.AccountID, AsOf: request.AsOf, MarkSource: request.MarkSource,
		MarkNamespace: request.MarkNamespace, MaxMarkAge: request.MaxMarkAge,
	})
	if err != nil {
		return nil, fmt.Errorf("rebuild ledger accounting projection: %w", err)
	}
	if projection == nil || projection.CheckpointID == uuid.Nil {
		return nil, fmt.Errorf("ledger accounting projection is invalid")
	}
	checkpoint, err := source.repository.GetProjectionCheckpointByID(ctx, projection.CheckpointID)
	if err != nil {
		return nil, fmt.Errorf("load ledger accounting checkpoint: %w", err)
	}
	if checkpoint == nil {
		return nil, fmt.Errorf("ledger accounting checkpoint is required")
	}
	if err := checkpoint.Validate(); err != nil {
		return nil, fmt.Errorf("validate ledger accounting checkpoint: %w", err)
	}
	if projection.Version != request.ProjectionVersion || projection.AccountID != request.AccountID || !projection.AsOf.Equal(request.AsOf) ||
		projection.MarkSource != request.MarkSource || projection.MarkNamespace != request.MarkNamespace || projection.MaxMarkAge != request.MaxMarkAge ||
		projection.CheckpointID != checkpoint.ID || projection.OutputChecksum != checkpoint.OutputChecksum || !bytes.Equal(projection.PayloadBytes, checkpoint.PayloadBytes) {
		return nil, fmt.Errorf("ledger accounting projection and checkpoint boundary differ")
	}
	if checkpoint.CreatedAt.Before(request.AsOf) {
		return nil, fmt.Errorf("ledger accounting checkpoint predates as_of")
	}
	positions := make([]PositionInput, 0, len(projection.Positions))
	for _, position := range projection.Positions {
		if position.Quantity.IsZero() {
			continue
		}
		positions = append(positions, PositionInput{InstrumentID: position.InstrumentID, Quantity: position.Quantity, Provenance: ProvenanceExactDecimal})
	}
	return NewSnapshot(SnapshotInput{
		Source: SourceLedger, AccountID: projection.AccountID, AsOf: projection.AsOf,
		ObservedAt: checkpoint.CreatedAt.UTC().Truncate(time.Microsecond), Currency: projection.BaseCurrency,
		ProjectionVersion: projection.Version, MarkSource: projection.MarkSource,
		MarkNamespace: projection.MarkNamespace, MaxMarkAge: projection.MaxMarkAge,
		CaptureFenceID: lease.FenceID(), CaptureEpoch: lease.Epoch(),
		EvidenceID: checkpoint.ID.String(), EvidenceChecksum: checkpoint.OutputChecksum,
		Synthetic: source.synthetic, PositionCoverageComplete: true,
		Metrics: []MetricInput{
			{Kind: MetricCash, Value: projection.Totals.Cash, Provenance: ProvenanceExactDecimal},
			{Kind: MetricFees, Value: projection.Totals.Fees, Provenance: ProvenanceExactDecimal},
			{Kind: MetricRealizedPnL, Value: projection.Totals.RealizedPnL, Provenance: ProvenanceExactDecimal},
			{Kind: MetricUnrealizedPnL, Value: projection.Totals.UnrealizedPnL, Provenance: ProvenanceExactDecimal},
			{Kind: MetricMarketValue, Value: projection.Totals.MarketValue, Provenance: ProvenanceExactDecimal},
			{Kind: MetricEquity, Value: projection.Totals.Equity, Provenance: ProvenanceExactDecimal},
		},
		Positions: positions,
		Missing:   []MissingFactInput{{FactKey: MetricFactKey(MetricBuyingPower), ReasonCode: MissingSourceUnavailable, EvidenceRef: "ledger-fifo-v1:buying-power-not-modeled"}},
	})
}

func validateSourceLease(request SourceRequest, lease CaptureLease) error {
	if request.AccountID == uuid.Nil || request.AsOf.IsZero() {
		return fmt.Errorf("accounting source request and capture lease are required")
	}
	if err := validateCaptureLease(request.AccountID, request.AsOf, lease); err != nil {
		return fmt.Errorf("accounting source capture lease: %w", err)
	}
	return nil
}

func exactLegacyFloat(value float64) (decimal.Decimal, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return decimal.Zero, fmt.Errorf("value is not finite")
	}
	parsed, err := decimal.NewFromString(strconv.FormatFloat(value, 'f', -1, 64))
	if err != nil || !validExactDecimal(parsed) {
		return decimal.Zero, fmt.Errorf("value cannot be represented by the reconciliation decimal contract")
	}
	return parsed, nil
}

func legacyPositionIdentity(position domain.Position) string {
	return strings.Join([]string{strings.TrimSpace(position.Ticker), string(position.MarketType), string(position.AssetClass), string(position.Side), strconv.FormatFloat(position.Quantity, 'f', -1, 64)}, "\x1f")
}

func canonicalLegacyCapture(capture LegacyCapture) ([]byte, error) {
	type positionPayload struct {
		ID                 string  `json:"id"`
		Ticker             string  `json:"ticker"`
		MarketType         string  `json:"market_type"`
		AssetClass         string  `json:"asset_class"`
		Side               string  `json:"side"`
		Quantity           string  `json:"quantity"`
		AverageEntry       string  `json:"average_entry"`
		CurrentPrice       *string `json:"current_price"`
		ContractMultiplier string  `json:"contract_multiplier"`
		UnderlyingTicker   string  `json:"underlying_ticker"`
		OptionType         string  `json:"option_type"`
	}
	payload := struct {
		Currency    string            `json:"currency"`
		Cash        string            `json:"cash"`
		BuyingPower string            `json:"buying_power"`
		Equity      string            `json:"equity"`
		CapturedAt  string            `json:"captured_at"`
		Positions   []positionPayload `json:"positions"`
	}{
		Currency: capture.Balance.Currency, Cash: strconv.FormatFloat(capture.Balance.Cash, 'f', -1, 64),
		BuyingPower: strconv.FormatFloat(capture.Balance.BuyingPower, 'f', -1, 64), Equity: strconv.FormatFloat(capture.Balance.Equity, 'f', -1, 64),
		CapturedAt: capture.CapturedAt.UTC().Truncate(time.Microsecond).Format(timestampLayout), Positions: make([]positionPayload, 0, len(capture.Positions)),
	}
	for _, position := range capture.Positions {
		var currentPrice *string
		if position.CurrentPrice != nil {
			value := strconv.FormatFloat(*position.CurrentPrice, 'f', -1, 64)
			currentPrice = &value
		}
		optionType := ""
		if position.OptionType != nil {
			optionType = string(*position.OptionType)
		}
		payload.Positions = append(payload.Positions, positionPayload{
			ID: position.ID.String(), Ticker: position.Ticker, MarketType: string(position.MarketType), AssetClass: string(position.AssetClass),
			Side: string(position.Side), Quantity: strconv.FormatFloat(position.Quantity, 'f', -1, 64), AverageEntry: strconv.FormatFloat(position.AvgEntry, 'f', -1, 64),
			CurrentPrice: currentPrice, ContractMultiplier: strconv.FormatFloat(position.ContractMultiplier, 'f', -1, 64),
			UnderlyingTicker: position.UnderlyingTicker, OptionType: optionType,
		})
	}
	sort.Slice(payload.Positions, func(i, j int) bool {
		left, right := payload.Positions[i], payload.Positions[j]
		return left.ID+"\x1f"+left.Ticker+"\x1f"+left.Side < right.ID+"\x1f"+right.Ticker+"\x1f"+right.Side
	})
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal legacy paper accounting evidence: %w", err)
	}
	return encoded, nil
}
