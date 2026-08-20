package capital

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
)

// StateFromCanonical restores a sealed state only when its exact bytes,
// content identity, and independently loaded account/binding/policy agree.
func StateFromCanonical(id uuid.UUID, digest string, raw []byte, account domain.Account, binding Binding, policy *Policy) (*State, error) {
	if id == uuid.Nil || len(digest) != 64 || len(raw) == 0 {
		return nil, fmt.Errorf("restore capital state: envelope is invalid")
	}
	var canonical canonicalState
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&canonical); err != nil {
		return nil, fmt.Errorf("restore capital state: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("restore capital state: canonical JSON has trailing content")
	}
	accountID, err := uuid.Parse(canonical.AccountID)
	if err != nil {
		return nil, fmt.Errorf("restore capital state account: %w", err)
	}
	bindingID, err := uuid.Parse(canonical.BindingID)
	if err != nil {
		return nil, fmt.Errorf("restore capital state binding: %w", err)
	}
	checkpointID, err := uuid.Parse(canonical.ProjectionCheckpointID)
	if err != nil {
		return nil, fmt.Errorf("restore capital state checkpoint: %w", err)
	}
	asOf, err := time.Parse("2006-01-02T15:04:05.000000Z", canonical.AsOf)
	if err != nil {
		return nil, fmt.Errorf("restore capital state as-of: %w", err)
	}
	values := make([]decimal.Decimal, 6)
	for index, text := range []string{canonical.Cash, canonical.Equity, canonical.LongExposure, canonical.ShortExposure, canonical.GrossExposure, canonical.MaintenanceRequirement} {
		values[index], err = decimal.NewFromString(text)
		if err != nil || values[index].String() != text {
			return nil, fmt.Errorf("restore capital state: amount %d is not canonical", index)
		}
	}
	state := &State{
		accountID: accountID, bindingID: bindingID, policyVersion: canonical.PolicyVersion,
		projectionCheckpointID: checkpointID, projectionChecksum: canonical.ProjectionChecksum,
		asOf: asOf, currency: canonical.Currency, cash: values[0], equity: values[1], longExposure: values[2],
		shortExposure: values[3], grossExposure: values[4], maintenanceRequirement: values[5],
	}
	if canonical.Schema != capitalStateSchema {
		return nil, fmt.Errorf("restore capital state: schema is invalid")
	}
	if err := state.seal(); err != nil {
		return nil, fmt.Errorf("restore capital state: %w", err)
	}
	if state.id != id || state.hash != digest || !bytes.Equal(state.canonicalBytes, raw) {
		return nil, fmt.Errorf("restore capital state: identity does not reconstruct")
	}
	if err := state.validate(account, binding, policy); err != nil {
		return nil, fmt.Errorf("restore capital state: %w", err)
	}
	return state, nil
}

const (
	capitalStateSchema = "capital-state-v1"
	capitalStateDomain = "capital-state"
)

// State is a canonical point-in-time capital view derived only from an
// OVR-104 projection and complete OVR-201 instrument identity. Its fields are
// private so callers cannot author exposure or maintenance totals.
type State struct {
	id                     uuid.UUID
	accountID              uuid.UUID
	bindingID              uuid.UUID
	policyVersion          string
	projectionCheckpointID uuid.UUID
	projectionChecksum     string
	asOf                   time.Time
	currency               string
	cash                   decimal.Decimal
	equity                 decimal.Decimal
	longExposure           decimal.Decimal
	shortExposure          decimal.Decimal
	grossExposure          decimal.Decimal
	maintenanceRequirement decimal.Decimal
	canonicalBytes         json.RawMessage
	hash                   string
}

type canonicalState struct {
	Schema                 string `json:"schema"`
	AccountID              string `json:"account_id"`
	BindingID              string `json:"binding_id"`
	PolicyVersion          string `json:"policy_version"`
	ProjectionCheckpointID string `json:"projection_checkpoint_id"`
	ProjectionChecksum     string `json:"projection_checksum"`
	AsOf                   string `json:"as_of"`
	Currency               string `json:"currency"`
	Cash                   string `json:"cash"`
	Equity                 string `json:"equity"`
	LongExposure           string `json:"long_exposure"`
	ShortExposure          string `json:"short_exposure"`
	GrossExposure          string `json:"gross_exposure"`
	MaintenanceRequirement string `json:"maintenance_requirement"`
}

type projectionCapitalPayload struct {
	CheckpointID string `json:"checkpoint_id"`
	AccountID    string `json:"account_id"`
	BaseCurrency string `json:"base_currency"`
	AsOf         string `json:"as_of"`
	Positions    []struct {
		InstrumentID string `json:"instrument_id"`
		Open         bool   `json:"open"`
		Quantity     string `json:"quantity"`
		MarketValue  string `json:"market_value"`
	} `json:"positions"`
	Totals struct {
		Cash        string `json:"cash"`
		MarketValue string `json:"market_value"`
		Equity      string `json:"equity"`
	} `json:"totals"`
}

// StateFromProjection is the sole v1 state constructor. It fails closed when
// an open position lacks one matching active equity/ETF instrument.
func StateFromProjection(
	account domain.Account,
	binding Binding,
	policy *Policy,
	projection *ledger.PortfolioProjection,
	instruments []instrument.Instrument,
) (*State, error) {
	if err := binding.Validate(account, policy); err != nil {
		return nil, fmt.Errorf("derive capital state binding: %w", err)
	}
	if projection == nil {
		return nil, fmt.Errorf("derive capital state: portfolio projection is required")
	}
	if projection.CheckpointID == uuid.Nil || projection.AccountID != account.ID ||
		projection.ProjectionType != ledger.PortfolioProjectionType || projection.Version != ledger.PortfolioProjectionVersion ||
		projection.FIFO != ledger.ProjectionFIFO || projection.BaseCurrency != account.BaseCurrency ||
		projection.AsOf.IsZero() || projection.AsOf.Location() != time.UTC ||
		!projection.AsOf.Equal(projection.AsOf.Truncate(time.Microsecond)) ||
		projection.ThroughTransactionID == uuid.Nil || projection.TransactionCount <= 0 ||
		len(projection.InputChecksum) != 64 || len(projection.OutputChecksum) != 64 || len(projection.PayloadBytes) == 0 {
		return nil, fmt.Errorf("derive capital state: projection envelope is invalid")
	}
	digestBytes := sha256.Sum256(projection.PayloadBytes)
	if hex.EncodeToString(digestBytes[:]) != projection.OutputChecksum {
		return nil, fmt.Errorf("derive capital state: projection output checksum does not match payload")
	}
	if err := validateCapitalProjectionTotals(projection.Totals); err != nil {
		return nil, fmt.Errorf("derive capital state: %w", err)
	}

	payload, err := decodeProjectionCapitalPayload(projection.PayloadBytes)
	if err != nil {
		return nil, fmt.Errorf("derive capital state: %w", err)
	}
	if payload.CheckpointID != projection.CheckpointID.String() || payload.AccountID != projection.AccountID.String() ||
		payload.BaseCurrency != projection.BaseCurrency || payload.AsOf != formatCapitalTime(projection.AsOf) ||
		payload.Totals.Cash != projection.Totals.Cash.String() ||
		payload.Totals.MarketValue != projection.Totals.MarketValue.String() ||
		payload.Totals.Equity != projection.Totals.Equity.String() || len(payload.Positions) != len(projection.Positions) {
		return nil, fmt.Errorf("derive capital state: projection payload does not match in-memory envelope")
	}

	instrumentByID := make(map[uuid.UUID]instrument.Instrument, len(instruments))
	for _, reference := range instruments {
		if err := reference.Validate(); err != nil {
			return nil, fmt.Errorf("derive capital state instrument %s: %w", reference.ID, err)
		}
		if reference.Currency != account.BaseCurrency ||
			(reference.AssetClass != instrument.AssetClassEquity && reference.AssetClass != instrument.AssetClassETF) {
			return nil, fmt.Errorf("derive capital state: instrument %s has unsupported asset class or currency", reference.ID)
		}
		if _, duplicate := instrumentByID[reference.ID]; duplicate {
			return nil, fmt.Errorf("derive capital state: instrument %s is duplicated", reference.ID)
		}
		instrumentByID[reference.ID] = reference
	}

	seenPositions := make(map[uuid.UUID]struct{}, len(projection.Positions))
	longExposure := decimal.Zero
	shortExposure := decimal.Zero
	marketValue := decimal.Zero
	openCount := 0
	for index, position := range projection.Positions {
		if position.InstrumentID == uuid.Nil {
			return nil, fmt.Errorf("derive capital state: projection position %d lacks instrument identity", index)
		}
		if _, duplicate := seenPositions[position.InstrumentID]; duplicate {
			return nil, fmt.Errorf("derive capital state: projection position %s is duplicated", position.InstrumentID)
		}
		seenPositions[position.InstrumentID] = struct{}{}
		if !validCapitalAmount(position.Quantity) || !validCapitalAmount(position.MarketValue) ||
			position.Open != !position.Quantity.IsZero() {
			return nil, fmt.Errorf("derive capital state: projection position %s is inconsistent", position.InstrumentID)
		}
		payloadPosition := payload.Positions[index]
		if payloadPosition.InstrumentID != position.InstrumentID.String() || payloadPosition.Open != position.Open ||
			payloadPosition.Quantity != position.Quantity.String() || payloadPosition.MarketValue != position.MarketValue.String() {
			return nil, fmt.Errorf("derive capital state: projection position %s differs from payload", position.InstrumentID)
		}
		marketValue = marketValue.Add(position.MarketValue)
		if !position.Open {
			if !position.MarketValue.IsZero() {
				return nil, fmt.Errorf("derive capital state: closed position %s has market exposure", position.InstrumentID)
			}
			continue
		}
		openCount++
		reference, ok := instrumentByID[position.InstrumentID]
		if !ok {
			return nil, fmt.Errorf("derive capital state: open position %s lacks canonical instrument", position.InstrumentID)
		}
		if position.MarkObservationID == uuid.Nil || position.OpenLotCount <= 0 ||
			(!position.MarketValue.IsZero() && position.Quantity.Sign() != position.MarketValue.Sign()) {
			return nil, fmt.Errorf("derive capital state: open position %s has inconsistent mark or sign", position.InstrumentID)
		}
		_ = reference
		if position.MarketValue.IsPositive() {
			longExposure = longExposure.Add(position.MarketValue)
		} else if position.MarketValue.IsNegative() {
			shortExposure = shortExposure.Add(position.MarketValue.Abs())
		}
	}
	if len(instrumentByID) != openCount {
		return nil, fmt.Errorf("derive capital state: canonical instrument set does not exactly cover open positions")
	}
	if !marketValue.Equal(projection.Totals.MarketValue) ||
		!projection.Totals.Equity.Equal(projection.Totals.Cash.Add(projection.Totals.MarketValue)) {
		return nil, fmt.Errorf("derive capital state: projection accounting totals are inconsistent")
	}

	profile, ok := policy.Profile(binding.Profile)
	if !ok {
		return nil, fmt.Errorf("derive capital state: binding profile is not in policy")
	}
	gross := longExposure.Add(shortExposure)
	maintenance := decimal.Zero
	if !profile.Unlimited {
		maintenance = roundCapitalUp(
			longExposure.Mul(profile.MaintenanceLong).Add(shortExposure.Mul(profile.MaintenanceShort)),
			policy.Scale(),
		)
	}
	state := &State{
		accountID: account.ID, bindingID: binding.ID, policyVersion: policy.Version(),
		projectionCheckpointID: projection.CheckpointID, projectionChecksum: projection.OutputChecksum,
		asOf: projection.AsOf, currency: projection.BaseCurrency, cash: projection.Totals.Cash,
		equity: projection.Totals.Equity, longExposure: longExposure, shortExposure: shortExposure,
		grossExposure: gross, maintenanceRequirement: maintenance,
	}
	if err := state.seal(); err != nil {
		return nil, fmt.Errorf("derive capital state: %w", err)
	}
	if err := state.validate(account, binding, policy); err != nil {
		return nil, fmt.Errorf("derive capital state: %w", err)
	}
	return state, nil
}

func decodeProjectionCapitalPayload(raw []byte) (projectionCapitalPayload, error) {
	var payload projectionCapitalPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&payload); err != nil {
		return payload, fmt.Errorf("decode projection payload: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return payload, fmt.Errorf("decode projection payload: %w", err)
	}
	return payload, nil
}

func validateCapitalProjectionTotals(totals ledger.ProjectionTotals) error {
	values := []decimal.Decimal{
		totals.Cash, totals.NetCapital, totals.Fees, totals.Rebates, totals.RealizedPnL,
		totals.UnrealizedPnL, totals.MarketValue, totals.Equity, totals.TotalPnL,
	}
	for _, value := range values {
		if !validCapitalAmount(value) {
			return fmt.Errorf("projection total exceeds capital precision")
		}
	}
	if totals.Equity.IsNegative() {
		return fmt.Errorf("projection equity cannot be negative for admission")
	}
	return nil
}

func (state *State) seal() error {
	canonical, err := state.canonical()
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return fmt.Errorf("marshal capital state: %w", err)
	}
	digestBytes := sha256.Sum256(encoded)
	state.canonicalBytes = encoded
	state.hash = hex.EncodeToString(digestBytes[:])
	state.id = economicid.DeterministicUUID(capitalStateDomain, state.hash)
	return nil
}

func (state *State) validate(account domain.Account, binding Binding, policy *Policy) error {
	if state == nil || state.id == uuid.Nil || len(state.canonicalBytes) == 0 || len(state.hash) != 64 {
		return fmt.Errorf("capital state identity is invalid")
	}
	if state.accountID != account.ID || state.bindingID != binding.ID || state.policyVersion != policy.Version() ||
		state.currency != account.BaseCurrency || state.projectionCheckpointID == uuid.Nil ||
		len(state.projectionChecksum) != 64 || state.asOf.IsZero() || state.asOf.Location() != time.UTC ||
		!state.asOf.Equal(state.asOf.Truncate(time.Microsecond)) {
		return fmt.Errorf("capital state context is invalid")
	}
	for _, value := range []decimal.Decimal{
		state.cash, state.equity, state.longExposure, state.shortExposure,
		state.grossExposure, state.maintenanceRequirement,
	} {
		if !validCapitalAmount(value) {
			return fmt.Errorf("capital state amount exceeds precision")
		}
	}
	if state.equity.IsNegative() || state.longExposure.IsNegative() || state.shortExposure.IsNegative() ||
		state.grossExposure.IsNegative() || state.maintenanceRequirement.IsNegative() ||
		!state.grossExposure.Equal(state.longExposure.Add(state.shortExposure)) {
		return fmt.Errorf("capital state exposure totals are inconsistent")
	}
	canonical, err := state.canonical()
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return err
	}
	digestBytes := sha256.Sum256(encoded)
	digest := hex.EncodeToString(digestBytes[:])
	if !bytes.Equal(encoded, state.canonicalBytes) || digest != state.hash ||
		state.id != economicid.DeterministicUUID(capitalStateDomain, digest) {
		return fmt.Errorf("capital state canonical evidence does not match fields")
	}
	return nil
}

func (state *State) canonical() (canonicalState, error) {
	if state == nil {
		return canonicalState{}, fmt.Errorf("capital state is required")
	}
	return canonicalState{
		Schema: capitalStateSchema, AccountID: state.accountID.String(), BindingID: state.bindingID.String(),
		PolicyVersion: state.policyVersion, ProjectionCheckpointID: state.projectionCheckpointID.String(),
		ProjectionChecksum: state.projectionChecksum, AsOf: formatCapitalTime(state.asOf), Currency: state.currency,
		Cash: state.cash.String(), Equity: state.equity.String(), LongExposure: state.longExposure.String(),
		ShortExposure: state.shortExposure.String(), GrossExposure: state.grossExposure.String(),
		MaintenanceRequirement: state.maintenanceRequirement.String(),
	}, nil
}

func validCapitalAmount(value decimal.Decimal) bool {
	if !value.Equal(value.Round(12)) {
		return false
	}
	integer := value.Abs().Truncate(0).String()
	return len(strings.TrimPrefix(integer, "-")) <= 26
}

func roundCapitalUp(value decimal.Decimal, scale int32) decimal.Decimal {
	return value.RoundCeil(scale)
}

func formatCapitalTime(value time.Time) string {
	return value.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z")
}

func (state *State) ID() uuid.UUID {
	if state == nil {
		return uuid.Nil
	}
	return state.id
}

// ProjectionCheckpointID returns the immutable accounting checkpoint from
// which this sealed capital state was derived.
func (state *State) ProjectionCheckpointID() uuid.UUID {
	if state == nil {
		return uuid.Nil
	}
	return state.projectionCheckpointID
}

func (state *State) Cash() decimal.Decimal {
	if state == nil {
		return decimal.Zero
	}
	return state.cash
}

func (state *State) Equity() decimal.Decimal {
	if state == nil {
		return decimal.Zero
	}
	return state.equity
}

func (state *State) LongExposure() decimal.Decimal {
	if state == nil {
		return decimal.Zero
	}
	return state.longExposure
}

func (state *State) ShortExposure() decimal.Decimal {
	if state == nil {
		return decimal.Zero
	}
	return state.shortExposure
}

func (state *State) GrossExposure() decimal.Decimal {
	if state == nil {
		return decimal.Zero
	}
	return state.grossExposure
}

func (state *State) MaintenanceRequirement() decimal.Decimal {
	if state == nil {
		return decimal.Zero
	}
	return state.maintenanceRequirement
}

func (state *State) CanonicalBytes() json.RawMessage {
	if state == nil {
		return nil
	}
	return append(json.RawMessage(nil), state.canonicalBytes...)
}

func (state *State) Hash() string {
	if state == nil {
		return ""
	}
	return state.hash
}
