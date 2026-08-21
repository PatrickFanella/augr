// Package marketdata owns canonical, point-in-time market observations.
// Provider-specific feed types remain in subpackages until explicit adapters
// preserve their provenance in this contract.
package marketdata

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

// MaxDepthLevelsPerSide is the hard generic ingestion ceiling. Provider
// adapters may enforce lower configured limits.
const MaxDepthLevelsPerSide = 1000

// DepthSide identifies one side of an ordered book.
type DepthSide string

const (
	DepthSideBid DepthSide = "bid"
	DepthSideAsk DepthSide = "ask"
)

// DepthLevel is one exact price/size point in a canonical ordered book.
type DepthLevel struct {
	Side  DepthSide       `json:"side"`
	Level int             `json:"level"`
	Price decimal.Decimal `json:"price"`
	Size  decimal.Decimal `json:"size"`
}

// DepthLevelInput is a caller-supplied level. The constructor assigns its
// zero-based level index and side from its position in Bids or Asks.
type DepthLevelInput struct {
	Price decimal.Decimal
	Size  decimal.Decimal
}

// QuoteSnapshot is one immutable, attributable market observation. Optional
// pointers preserve the distinction between a missing value and a present zero.
type QuoteSnapshot struct {
	ID                   uuid.UUID        `json:"id"`
	IngestSequence       int64            `json:"ingest_sequence"`
	InstrumentID         uuid.UUID        `json:"instrument_id"`
	VenueContractID      *uuid.UUID       `json:"venue_contract_id,omitempty"`
	Provider             string           `json:"provider"`
	Venue                string           `json:"venue"`
	Source               string           `json:"source,omitempty"`
	ObservationNamespace string           `json:"observation_namespace"`
	ObservationID        string           `json:"observation_id"`
	SourceRevision       string           `json:"source_revision,omitempty"`
	SourceSequence       *int64           `json:"source_sequence,omitempty"`
	ExchangeAt           *time.Time       `json:"exchange_at,omitempty"`
	ReceivedAt           time.Time        `json:"received_at"`
	AvailableAt          *time.Time       `json:"available_at,omitempty"`
	Bid                  *decimal.Decimal `json:"bid,omitempty"`
	BidSize              *decimal.Decimal `json:"bid_size,omitempty"`
	Ask                  *decimal.Decimal `json:"ask,omitempty"`
	AskSize              *decimal.Decimal `json:"ask_size,omitempty"`
	Last                 *decimal.Decimal `json:"last,omitempty"`
	Mark                 *decimal.Decimal `json:"mark,omitempty"`
	MarketStatus         string           `json:"market_status,omitempty"`
	SessionStatus        string           `json:"session_status,omitempty"`
	Depth                []DepthLevel     `json:"depth,omitempty"`
	Metadata             json.RawMessage  `json:"metadata"`
	CreatedAt            time.Time        `json:"created_at"`
}

// QuoteSnapshotInput contains provider facts for one new observation.
type QuoteSnapshotInput struct {
	InstrumentID         uuid.UUID
	VenueContractID      *uuid.UUID
	Provider             string
	Venue                string
	Source               string
	ObservationNamespace string
	ObservationID        string
	SourceRevision       string
	SourceSequence       *int64
	ExchangeAt           *time.Time
	ReceivedAt           time.Time
	AvailableAt          *time.Time
	Bid                  *decimal.Decimal
	BidSize              *decimal.Decimal
	Ask                  *decimal.Decimal
	AskSize              *decimal.Decimal
	Last                 *decimal.Decimal
	Mark                 *decimal.Decimal
	MarketStatus         string
	SessionStatus        string
	Bids                 []DepthLevelInput
	Asks                 []DepthLevelInput
	Metadata             json.RawMessage
	CreatedAt            time.Time
}

// QuoteSelector identifies one no-lookahead point-in-time lookup.
type QuoteSelector struct {
	InstrumentID         uuid.UUID
	Provider             string
	Venue                string
	ObservationNamespace string
	AsOf                 time.Time
}

// AssessmentCode is a stable fail-closed reason recorded at an execution
// boundary independently of provider-specific error text.
type AssessmentCode string

const (
	AssessmentMissingSource           AssessmentCode = "missing_source"
	AssessmentMissingVenueContract    AssessmentCode = "missing_venue_contract"
	AssessmentMissingAvailability     AssessmentCode = "missing_availability_time"
	AssessmentMissingBid              AssessmentCode = "missing_bid"
	AssessmentMissingAsk              AssessmentCode = "missing_ask"
	AssessmentMissingExchangeTime     AssessmentCode = "missing_exchange_time"
	AssessmentFutureObservation       AssessmentCode = "future_observation"
	AssessmentStaleQuote              AssessmentCode = "stale_quote"
	AssessmentMissingBidDepth         AssessmentCode = "missing_bid_depth"
	AssessmentMissingAskDepth         AssessmentCode = "missing_ask_depth"
	AssessmentMissingMarketStatus     AssessmentCode = "missing_market_status"
	AssessmentMissingSessionStatus    AssessmentCode = "missing_session_status"
	AssessmentMarketNotExecutable     AssessmentCode = "market_not_executable"
	AssessmentSessionNotExecutable    AssessmentCode = "session_not_executable"
	AssessmentInstrumentMismatch      AssessmentCode = "instrument_mismatch"
	AssessmentInstrumentNotExecutable AssessmentCode = "instrument_not_executable"
	AssessmentVenueContractMismatch   AssessmentCode = "venue_contract_mismatch"
	AssessmentInvalidPriceTick        AssessmentCode = "invalid_price_tick"
	AssessmentInvalidLotSize          AssessmentCode = "invalid_lot_size"
)

// QuoteRequirements declares which optional observation facts an execution
// boundary needs. A non-empty status allowlist implicitly requires that status.
type QuoteRequirements struct {
	RequireSource          bool
	RequireVenueContract   bool
	RequireBid             bool
	RequireAsk             bool
	RequireBidDepth        bool
	RequireAskDepth        bool
	RequireMarketStatus    bool
	RequireSessionStatus   bool
	AllowedMarketStatuses  []string
	AllowedSessionStatuses []string
	MaxAge                 time.Duration
}

// AssessmentError reports one stable reason that an observation cannot satisfy
// the requested execution contract.
type AssessmentError struct {
	Code   AssessmentCode
	Detail string
}

func (err *AssessmentError) Error() string {
	if err == nil {
		return ""
	}
	if err.Detail == "" {
		return string(err.Code)
	}
	return string(err.Code) + ": " + err.Detail
}

// QuoteAssessment contains values derived at one intent or route time. Optional
// pointers retain the absence of exchange time or spread.
type QuoteAssessment struct {
	SnapshotID        uuid.UUID        `json:"snapshot_id"`
	VenueContractID   *uuid.UUID       `json:"venue_contract_id,omitempty"`
	EvaluatedAt       time.Time        `json:"evaluated_at"`
	ExchangeAge       *time.Duration   `json:"exchange_age,omitempty"`
	ReceiveAge        time.Duration    `json:"receive_age"`
	AvailabilityAge   time.Duration    `json:"availability_age"`
	TransportLatency  *time.Duration   `json:"transport_latency,omitempty"`
	ValidationLatency *time.Duration   `json:"validation_latency,omitempty"`
	Spread            *decimal.Decimal `json:"spread,omitempty"`
}

// NewQuoteSnapshot normalizes one provider observation and validates its
// durable identity, timestamps, and independently present top-of-book values.
func NewQuoteSnapshot(input QuoteSnapshotInput) (*QuoteSnapshot, error) {
	metadata, err := normalizeJSONObject(input.Metadata, "quote snapshot metadata")
	if err != nil {
		return nil, err
	}
	createdAt := input.CreatedAt.UTC().Truncate(time.Microsecond)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	snapshot := &QuoteSnapshot{
		ID:                   uuid.New(),
		InstrumentID:         input.InstrumentID,
		VenueContractID:      cloneUUID(input.VenueContractID),
		Provider:             normalizeLower(input.Provider),
		Venue:                normalizeLower(input.Venue),
		Source:               strings.TrimSpace(input.Source),
		ObservationNamespace: strings.TrimSpace(input.ObservationNamespace),
		ObservationID:        strings.TrimSpace(input.ObservationID),
		SourceRevision:       strings.TrimSpace(input.SourceRevision),
		SourceSequence:       cloneInt64(input.SourceSequence),
		ExchangeAt:           normalizeOptionalTime(input.ExchangeAt),
		ReceivedAt:           input.ReceivedAt.UTC().Truncate(time.Microsecond),
		AvailableAt:          normalizeOptionalTime(input.AvailableAt),
		Bid:                  cloneDecimal(input.Bid),
		BidSize:              cloneDecimal(input.BidSize),
		Ask:                  cloneDecimal(input.Ask),
		AskSize:              cloneDecimal(input.AskSize),
		Last:                 cloneDecimal(input.Last),
		Mark:                 cloneDecimal(input.Mark),
		MarketStatus:         normalizeLower(input.MarketStatus),
		SessionStatus:        normalizeLower(input.SessionStatus),
		Depth:                makeDepth(input.Bids, input.Asks),
		Metadata:             metadata,
		CreatedAt:            createdAt,
	}
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// NewQuoteSelector normalizes one point-in-time repository lookup.
func NewQuoteSelector(instrumentID uuid.UUID, provider, venue, observationNamespace string, asOf time.Time) (QuoteSelector, error) {
	selector := QuoteSelector{
		InstrumentID:         instrumentID,
		Provider:             normalizeLower(provider),
		Venue:                normalizeLower(venue),
		ObservationNamespace: strings.TrimSpace(observationNamespace),
		AsOf:                 asOf.UTC().Truncate(time.Microsecond),
	}
	if err := selector.Validate(); err != nil {
		return QuoteSelector{}, err
	}
	return selector, nil
}

// Validate checks a point-in-time lookup without mutating its normalization.
func (selector QuoteSelector) Validate() error {
	if selector.InstrumentID == uuid.Nil {
		return fmt.Errorf("quote selector instrument ID is required")
	}
	if !isNormalizedLowerRequired(selector.Provider) || !isNormalizedLowerRequired(selector.Venue) {
		return fmt.Errorf("quote selector provider and venue must be non-empty normalized lowercase values")
	}
	if !isNormalizedRequired(selector.ObservationNamespace) {
		return fmt.Errorf("quote selector observation namespace must be non-empty and normalized")
	}
	return validateRequiredTimestamp(selector.AsOf, "selector as-of")
}

// Assess evaluates observation sufficiency at an intent or route time.
// Availability always fails closed; other optional facts are required by the
// supplied policy. This method deliberately does not establish instrument or
// venue-mechanics eligibility; execution callers use AssessForExecution.
func (snapshot QuoteSnapshot) Assess(asOf time.Time, requirements QuoteRequirements) (*QuoteAssessment, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("assess quote snapshot: %w", err)
	}
	evaluatedAt := asOf.UTC().Truncate(time.Microsecond)
	if err := validateRequiredTimestamp(evaluatedAt, "assessment"); err != nil {
		return nil, err
	}
	if requirements.MaxAge < 0 {
		return nil, fmt.Errorf("quote assessment max age cannot be negative")
	}
	allowedMarket, err := normalizeAllowedStatuses(requirements.AllowedMarketStatuses, "market")
	if err != nil {
		return nil, err
	}
	allowedSession, err := normalizeAllowedStatuses(requirements.AllowedSessionStatuses, "session")
	if err != nil {
		return nil, err
	}
	if snapshot.AvailableAt == nil {
		return nil, assessmentFailure(AssessmentMissingAvailability, "observation has no decision-availability time")
	}
	if snapshot.AvailableAt.After(evaluatedAt) {
		return nil, assessmentFailure(AssessmentFutureObservation, "observation was not available at the requested time")
	}
	if requirements.RequireSource && snapshot.Source == "" {
		return nil, assessmentFailure(AssessmentMissingSource, "source is required")
	}
	if requirements.RequireVenueContract && snapshot.VenueContractID == nil {
		return nil, assessmentFailure(AssessmentMissingVenueContract, "dated venue mechanics are required")
	}
	if requirements.RequireBid && snapshot.Bid == nil {
		return nil, assessmentFailure(AssessmentMissingBid, "bid is required")
	}
	if requirements.RequireAsk && snapshot.Ask == nil {
		return nil, assessmentFailure(AssessmentMissingAsk, "ask is required")
	}
	if requirements.RequireBidDepth && !snapshot.hasDepthSide(DepthSideBid) {
		return nil, assessmentFailure(AssessmentMissingBidDepth, "bid depth is required")
	}
	if requirements.RequireAskDepth && !snapshot.hasDepthSide(DepthSideAsk) {
		return nil, assessmentFailure(AssessmentMissingAskDepth, "ask depth is required")
	}
	marketRequired := requirements.RequireMarketStatus || len(allowedMarket) > 0
	if marketRequired && snapshot.MarketStatus == "" {
		return nil, assessmentFailure(AssessmentMissingMarketStatus, "market status is required")
	}
	if len(allowedMarket) > 0 && !statusAllowed(snapshot.MarketStatus, allowedMarket) {
		return nil, assessmentFailure(AssessmentMarketNotExecutable, "market status is not allowed")
	}
	sessionRequired := requirements.RequireSessionStatus || len(allowedSession) > 0
	if sessionRequired && snapshot.SessionStatus == "" {
		return nil, assessmentFailure(AssessmentMissingSessionStatus, "session status is required")
	}
	if len(allowedSession) > 0 && !statusAllowed(snapshot.SessionStatus, allowedSession) {
		return nil, assessmentFailure(AssessmentSessionNotExecutable, "session status is not allowed")
	}

	assessment := &QuoteAssessment{
		SnapshotID:      snapshot.ID,
		VenueContractID: cloneUUID(snapshot.VenueContractID),
		EvaluatedAt:     evaluatedAt,
		ReceiveAge:      evaluatedAt.Sub(snapshot.ReceivedAt),
		AvailabilityAge: evaluatedAt.Sub(*snapshot.AvailableAt),
	}
	validationLatency := snapshot.AvailableAt.Sub(snapshot.ReceivedAt)
	assessment.ValidationLatency = &validationLatency
	if snapshot.ExchangeAt != nil {
		exchangeAge := evaluatedAt.Sub(*snapshot.ExchangeAt)
		transportLatency := snapshot.ReceivedAt.Sub(*snapshot.ExchangeAt)
		assessment.ExchangeAge = &exchangeAge
		assessment.TransportLatency = &transportLatency
	}
	if requirements.MaxAge > 0 {
		if assessment.ExchangeAge == nil {
			return nil, assessmentFailure(AssessmentMissingExchangeTime, "exchange time is required to calculate quote age")
		}
		if *assessment.ExchangeAge > requirements.MaxAge {
			return nil, assessmentFailure(AssessmentStaleQuote, fmt.Sprintf("exchange age %s exceeds %s", *assessment.ExchangeAge, requirements.MaxAge))
		}
	}
	if snapshot.Bid != nil && snapshot.Ask != nil {
		spread := snapshot.Ask.Sub(*snapshot.Bid)
		assessment.Spread = &spread
	}
	return assessment, nil
}

// AssessForExecution joins point-in-time observation sufficiency to immutable
// reference eligibility. It requires an active, unexpired matching instrument
// and exact tick/lot compliance with the snapshot's dated venue contract.
func (snapshot QuoteSnapshot) AssessForExecution(
	asOf time.Time,
	requirements QuoteRequirements,
	reference instrument.Instrument,
	contract instrument.VenueContract,
) (*QuoteAssessment, error) {
	requirements.RequireVenueContract = true
	assessment, err := snapshot.Assess(asOf, requirements)
	if err != nil {
		return nil, err
	}
	if err := snapshot.ValidateInstrumentEligibility(reference, assessment.EvaluatedAt); err != nil {
		return nil, err
	}
	if err := snapshot.ValidateAgainstVenueContract(contract); err != nil {
		return nil, err
	}
	if !venueContractEffectiveAt(contract, assessment.EvaluatedAt) {
		return nil, assessmentFailure(AssessmentVenueContractMismatch, "venue contract is not effective at the execution evaluation time")
	}
	return assessment, nil
}

// ValidateInstrumentEligibility rejects execution against malformed,
// mismatched, inactive, quarantined, expired, or time-expired reference data.
// Quote retention remains independent of this execution boundary.
func (snapshot QuoteSnapshot) ValidateInstrumentEligibility(reference instrument.Instrument, asOf time.Time) error {
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("validate quote snapshot instrument eligibility: %w", err)
	}
	if err := reference.Validate(); err != nil {
		return assessmentFailure(AssessmentInstrumentNotExecutable, fmt.Sprintf("instrument reference is invalid: %v", err))
	}
	if reference.ID != snapshot.InstrumentID {
		return assessmentFailure(AssessmentInstrumentMismatch, "instrument reference does not match the observation")
	}
	if reference.Status != instrument.StatusActive {
		return assessmentFailure(AssessmentInstrumentNotExecutable, fmt.Sprintf("instrument status %q is not active", reference.Status))
	}
	evaluatedAt := asOf.UTC().Truncate(time.Microsecond)
	if err := validateRequiredTimestamp(evaluatedAt, "instrument eligibility"); err != nil {
		return err
	}
	if reference.Expiration != nil && !evaluatedAt.Before(*reference.Expiration) {
		return assessmentFailure(AssessmentInstrumentNotExecutable, "instrument is expired at the requested time")
	}
	return nil
}

// ValidateAgainstVenueContract checks executable quote fields against the
// exact dated venue mechanics bound to the retained observation. Bid/ask and
// depth prices use TickSize; displayed top/depth sizes use LotSize. Last and
// mark remain evidence rather than executable prices because their event time
// or theoretical-price semantics may not match the current quote tick regime.
func (snapshot QuoteSnapshot) ValidateAgainstVenueContract(contract instrument.VenueContract) error {
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("validate quote snapshot venue contract: %w", err)
	}
	if snapshot.VenueContractID == nil {
		return assessmentFailure(AssessmentMissingVenueContract, "dated venue mechanics are required")
	}
	if err := contract.Validate(); err != nil {
		return assessmentFailure(AssessmentVenueContractMismatch, fmt.Sprintf("venue contract reference is invalid: %v", err))
	}
	if contract.ID != *snapshot.VenueContractID || contract.InstrumentID != snapshot.InstrumentID || contract.Venue != snapshot.Venue {
		return assessmentFailure(AssessmentVenueContractMismatch, "venue contract identity does not match the observation")
	}
	observedAt := snapshot.ReceivedAt
	if snapshot.ExchangeAt != nil {
		observedAt = *snapshot.ExchangeAt
	}
	if !venueContractEffectiveAt(contract, observedAt) {
		return assessmentFailure(AssessmentVenueContractMismatch, "venue contract is not effective at the observation time")
	}

	for _, candidate := range []struct {
		label string
		value *decimal.Decimal
	}{
		{label: "bid", value: snapshot.Bid},
		{label: "ask", value: snapshot.Ask},
	} {
		if candidate.value != nil && !isExactMultiple(*candidate.value, contract.TickSize) {
			return assessmentFailure(AssessmentInvalidPriceTick, fmt.Sprintf("%s %s is not an exact multiple of tick size %s", candidate.label, candidate.value, contract.TickSize))
		}
	}
	for _, candidate := range []struct {
		label string
		value *decimal.Decimal
	}{
		{label: "bid size", value: snapshot.BidSize},
		{label: "ask size", value: snapshot.AskSize},
	} {
		if candidate.value != nil && !isExactMultiple(*candidate.value, contract.LotSize) {
			return assessmentFailure(AssessmentInvalidLotSize, fmt.Sprintf("%s %s is not an exact multiple of lot size %s", candidate.label, candidate.value, contract.LotSize))
		}
	}
	for _, level := range snapshot.Depth {
		if !isExactMultiple(level.Price, contract.TickSize) {
			return assessmentFailure(AssessmentInvalidPriceTick, fmt.Sprintf("%s depth level %d price %s is not an exact multiple of tick size %s", level.Side, level.Level, level.Price, contract.TickSize))
		}
		if !isExactMultiple(level.Size, contract.LotSize) {
			return assessmentFailure(AssessmentInvalidLotSize, fmt.Sprintf("%s depth level %d size %s is not an exact multiple of lot size %s", level.Side, level.Level, level.Size, contract.LotSize))
		}
	}
	return nil
}

// Validate checks the durable observation identity and top-of-book shape.
func (snapshot QuoteSnapshot) Validate() error {
	if snapshot.ID == uuid.Nil || snapshot.InstrumentID == uuid.Nil {
		return fmt.Errorf("quote snapshot and instrument IDs are required")
	}
	if snapshot.IngestSequence < 0 {
		return fmt.Errorf("quote snapshot ingest sequence cannot be negative")
	}
	if snapshot.VenueContractID != nil && *snapshot.VenueContractID == uuid.Nil {
		return fmt.Errorf("quote snapshot venue contract ID cannot be nil")
	}
	if !isNormalizedLowerRequired(snapshot.Provider) || !isNormalizedLowerRequired(snapshot.Venue) {
		return fmt.Errorf("quote snapshot provider and venue must be non-empty normalized lowercase values")
	}
	if snapshot.Source != strings.TrimSpace(snapshot.Source) {
		return fmt.Errorf("quote snapshot source must be normalized")
	}
	if !isNormalizedRequired(snapshot.ObservationNamespace) || !isNormalizedRequired(snapshot.ObservationID) {
		return fmt.Errorf("quote snapshot observation namespace and ID must be non-empty normalized values")
	}
	if snapshot.SourceRevision != strings.TrimSpace(snapshot.SourceRevision) {
		return fmt.Errorf("quote snapshot source revision must be normalized")
	}
	if snapshot.SourceSequence != nil && *snapshot.SourceSequence < 0 {
		return fmt.Errorf("quote snapshot source sequence cannot be negative")
	}
	if err := validateRequiredTimestamp(snapshot.ReceivedAt, "receive"); err != nil {
		return err
	}
	if err := validateRequiredTimestamp(snapshot.CreatedAt, "creation"); err != nil {
		return err
	}
	if err := validateOptionalTimestamp(snapshot.ExchangeAt, "exchange"); err != nil {
		return err
	}
	if err := validateOptionalTimestamp(snapshot.AvailableAt, "availability"); err != nil {
		return err
	}
	if snapshot.ExchangeAt != nil && snapshot.ExchangeAt.After(snapshot.ReceivedAt) {
		return fmt.Errorf("quote snapshot exchange time cannot be after receive time")
	}
	if snapshot.AvailableAt != nil && snapshot.AvailableAt.Before(snapshot.ReceivedAt) {
		return fmt.Errorf("quote snapshot availability time cannot be before receive time")
	}
	for name, value := range map[string]*decimal.Decimal{
		"bid": snapshot.Bid, "bid size": snapshot.BidSize,
		"ask": snapshot.Ask, "ask size": snapshot.AskSize,
		"last": snapshot.Last, "mark": snapshot.Mark,
	} {
		if err := validateOptionalDecimal(name, value); err != nil {
			return err
		}
	}
	if snapshot.BidSize != nil && snapshot.Bid == nil {
		return fmt.Errorf("quote snapshot bid size requires bid")
	}
	if snapshot.AskSize != nil && snapshot.Ask == nil {
		return fmt.Errorf("quote snapshot ask size requires ask")
	}
	if snapshot.Bid != nil && snapshot.Ask != nil && snapshot.Bid.GreaterThan(*snapshot.Ask) {
		return fmt.Errorf("quote snapshot bid cannot exceed ask")
	}
	if !isNormalizedLowerOptional(snapshot.MarketStatus) || !isNormalizedLowerOptional(snapshot.SessionStatus) {
		return fmt.Errorf("quote snapshot market and session statuses must be normalized lowercase values")
	}
	if err := snapshot.validateDepth(); err != nil {
		return err
	}
	if _, err := normalizeJSONObject(snapshot.Metadata, "quote snapshot metadata"); err != nil {
		return err
	}
	return nil
}

func (snapshot QuoteSnapshot) validateDepth() error {
	bids := make([]DepthLevel, 0)
	asks := make([]DepthLevel, 0)
	seenAsk := false
	for _, level := range snapshot.Depth {
		if level.Side != DepthSideBid && level.Side != DepthSideAsk {
			return fmt.Errorf("quote snapshot depth side %q is invalid", level.Side)
		}
		if level.Side == DepthSideAsk {
			seenAsk = true
		} else if seenAsk {
			return fmt.Errorf("quote snapshot bid depth cannot follow ask depth")
		}
		if err := validateOptionalDecimal("depth price", &level.Price); err != nil {
			return err
		}
		if err := validateOptionalDecimal("depth size", &level.Size); err != nil {
			return err
		}
		sideLevels := &bids
		if level.Side == DepthSideAsk {
			sideLevels = &asks
		}
		if level.Level != len(*sideLevels) {
			return fmt.Errorf("quote snapshot %s depth indexes must be contiguous from zero", level.Side)
		}
		*sideLevels = append(*sideLevels, level)
	}
	if len(bids) > MaxDepthLevelsPerSide || len(asks) > MaxDepthLevelsPerSide {
		return fmt.Errorf("quote snapshot depth exceeds %d levels per side", MaxDepthLevelsPerSide)
	}
	for index := 1; index < len(bids); index++ {
		if !bids[index-1].Price.GreaterThan(bids[index].Price) {
			return fmt.Errorf("quote snapshot bid depth must strictly descend")
		}
	}
	for index := 1; index < len(asks); index++ {
		if !asks[index-1].Price.LessThan(asks[index].Price) {
			return fmt.Errorf("quote snapshot ask depth must strictly ascend")
		}
	}
	if len(bids) > 0 && len(asks) > 0 && bids[0].Price.GreaterThan(asks[0].Price) {
		return fmt.Errorf("quote snapshot depth bid cannot exceed depth ask")
	}
	if len(bids) > 0 {
		if snapshot.Bid != nil && !bids[0].Price.Equal(*snapshot.Bid) {
			return fmt.Errorf("quote snapshot bid depth top must equal bid")
		}
		if snapshot.BidSize != nil && !bids[0].Size.Equal(*snapshot.BidSize) {
			return fmt.Errorf("quote snapshot bid depth top size must equal bid size")
		}
	}
	if len(asks) > 0 {
		if snapshot.Ask != nil && !asks[0].Price.Equal(*snapshot.Ask) {
			return fmt.Errorf("quote snapshot ask depth top must equal ask")
		}
		if snapshot.AskSize != nil && !asks[0].Size.Equal(*snapshot.AskSize) {
			return fmt.Errorf("quote snapshot ask depth top size must equal ask size")
		}
	}
	return nil
}

func (snapshot QuoteSnapshot) hasDepthSide(side DepthSide) bool {
	for _, level := range snapshot.Depth {
		if level.Side == side {
			return true
		}
	}
	return false
}

func assessmentFailure(code AssessmentCode, detail string) *AssessmentError {
	return &AssessmentError{Code: code, Detail: detail}
}

func normalizeAllowedStatuses(values []string, label string) (map[string]struct{}, error) {
	allowed := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizeLower(value)
		if normalized == "" {
			return nil, fmt.Errorf("quote assessment allowed %s statuses cannot contain an empty value", label)
		}
		allowed[normalized] = struct{}{}
	}
	return allowed, nil
}

func statusAllowed(value string, allowed map[string]struct{}) bool {
	_, ok := allowed[value]
	return ok
}

func isExactMultiple(value, increment decimal.Decimal) bool {
	return value.Mod(increment).IsZero()
}

func venueContractEffectiveAt(contract instrument.VenueContract, at time.Time) bool {
	return !at.Before(contract.ValidFrom) && (contract.ValidTo == nil || at.Before(*contract.ValidTo))
}

func makeDepth(bids, asks []DepthLevelInput) []DepthLevel {
	depth := make([]DepthLevel, 0, len(bids)+len(asks))
	for level, value := range bids {
		depth = append(depth, DepthLevel{Side: DepthSideBid, Level: level, Price: value.Price, Size: value.Size})
	}
	for level, value := range asks {
		depth = append(depth, DepthLevel{Side: DepthSideAsk, Level: level, Price: value.Price, Size: value.Size})
	}
	return depth
}

func normalizeLower(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isNormalizedLowerRequired(value string) bool {
	return value != "" && value == normalizeLower(value)
}

func isNormalizedLowerOptional(value string) bool {
	return value == "" || value == normalizeLower(value)
}

func isNormalizedRequired(value string) bool {
	return value != "" && value == strings.TrimSpace(value)
}

func normalizeOptionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC().Truncate(time.Microsecond)
	return &normalized
}

func validateRequiredTimestamp(value time.Time, label string) error {
	if value.IsZero() || value.Location() != time.UTC || !value.Equal(value.Truncate(time.Microsecond)) {
		return fmt.Errorf("quote snapshot %s time must use normalized UTC microsecond precision", label)
	}
	return nil
}

func validateOptionalTimestamp(value *time.Time, label string) error {
	if value == nil {
		return nil
	}
	return validateRequiredTimestamp(*value, label)
}

func validateOptionalDecimal(label string, value *decimal.Decimal) error {
	if value == nil {
		return nil
	}
	if value.IsNegative() {
		return fmt.Errorf("quote snapshot %s cannot be negative", label)
	}
	if !value.Equal(value.Round(12)) {
		return fmt.Errorf("quote snapshot %s supports at most 12 decimal places", label)
	}
	if value.NumDigits()+int(value.Exponent()) > 26 {
		return fmt.Errorf("quote snapshot %s exceeds exact decimal magnitude", label)
	}
	return nil
}

func normalizeJSONObject(value json.RawMessage, label string) (json.RawMessage, error) {
	normalized := append(json.RawMessage(nil), value...)
	if len(normalized) == 0 {
		normalized = json.RawMessage(`{}`)
	}
	var object map[string]any
	if err := json.Unmarshal(normalized, &object); err != nil || object == nil {
		return nil, fmt.Errorf("%s must be a JSON object", label)
	}
	return normalized, nil
}

func cloneDecimal(value *decimal.Decimal) *decimal.Decimal {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneUUID(value *uuid.UUID) *uuid.UUID {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
