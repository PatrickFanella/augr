// Package predictionreplay owns immutable point-in-time prediction-market book
// and fee evidence. It computes research fills but cannot create intents, reserve
// capital, route orders, or promote strategies.
package predictionreplay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const SchemaV1 = "prediction-book-fee-recorder-v1"

const timeLayout = "2006-01-02T15:04:05.000000Z"

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type (
	Side          string
	LiquidityRole string
	FeeFormula    string
	RoundingMode  string
)

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"

	RoleMaker LiquidityRole = "maker"
	RoleTaker LiquidityRole = "taker"

	FeeNotionalBPS   FeeFormula = "notional_bps"
	FeeContractCurve FeeFormula = "contract_curve"

	RoundHalfUp  RoundingMode = "half_up"
	RoundCeiling RoundingMode = "ceiling"
)

type LevelInput struct {
	Price string
	Size  string
}

type BookInput struct {
	MarketID      string
	OutcomeID     uuid.UUID
	Venue         string
	SourceKey     string
	ContentSHA256 string
	ExchangeAt    time.Time
	AvailableAt   time.Time
	Revision      int
	CorrectionOf  string
	Bids          []LevelInput
	Asks          []LevelInput
}

type FeePolicyInput struct {
	InstrumentID  uuid.UUID
	Venue         string
	Role          LiquidityRole
	SourceKey     string
	ContentSHA256 string
	AvailableAt   time.Time
	EffectiveFrom time.Time
	EffectiveTo   *time.Time
	Formula       FeeFormula
	Rate          string
	Scale         int32
	Rounding      RoundingMode
}

type ReplayInput struct {
	DecisionAt time.Time
	MarketID   string
	OutcomeID  uuid.UUID
	Side       Side
	Role       LiquidityRole
	Quantity   string
	LimitPrice string
}

type Input struct {
	Manifest *dataset.Manifest
	Books    []BookInput
	Fees     []FeePolicyInput
	Replays  []ReplayInput
}

type evidenceCanonical struct {
	PartitionContentSHA256 string `json:"partition_content_sha256"`
	SourceKey              string `json:"source_key"`
	ContentSHA256          string `json:"content_sha256"`
	AvailableAt            string `json:"available_at"`
}

type levelCanonical struct {
	Side     string `json:"side"`
	Sequence int    `json:"sequence"`
	Price    string `json:"price"`
	Size     string `json:"size"`
}

type bookCanonical struct {
	evidenceCanonical
	MarketID     string           `json:"market_id"`
	OutcomeID    string           `json:"outcome_id"`
	Venue        string           `json:"venue"`
	ExchangeAt   string           `json:"exchange_at"`
	Revision     int              `json:"revision"`
	CorrectionOf string           `json:"correction_of"`
	Levels       []levelCanonical `json:"levels"`
}

type feeCanonical struct {
	evidenceCanonical
	InstrumentID  string `json:"instrument_id"`
	Venue         string `json:"venue"`
	Role          string `json:"role"`
	EffectiveFrom string `json:"effective_from"`
	EffectiveTo   string `json:"effective_to"`
	Formula       string `json:"formula"`
	Rate          string `json:"rate"`
	Scale         int32  `json:"scale"`
	Rounding      string `json:"rounding"`
}

type fillCanonical struct {
	Sequence int    `json:"sequence"`
	Level    int    `json:"level"`
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
	Gross    string `json:"gross"`
}

type replayCanonical struct {
	Sequence         int             `json:"sequence"`
	DecisionAt       string          `json:"decision_at"`
	MarketID         string          `json:"market_id"`
	OutcomeID        string          `json:"outcome_id"`
	Side             string          `json:"side"`
	Role             string          `json:"role"`
	Quantity         string          `json:"quantity"`
	LimitPrice       string          `json:"limit_price"`
	Status           string          `json:"status"`
	BookSourceKey    string          `json:"book_source_key"`
	FeeSourceKey     string          `json:"fee_source_key"`
	FilledQuantity   string          `json:"filled_quantity"`
	ResidualQuantity string          `json:"residual_quantity"`
	WeightedPrice    string          `json:"weighted_price"`
	GrossCash        string          `json:"gross_cash"`
	Fee              string          `json:"fee"`
	NetCash          string          `json:"net_cash"`
	Fills            []fillCanonical `json:"fills"`
}

type recorderCanonical struct {
	Schema         string            `json:"schema"`
	State          string            `json:"state"`
	ManifestID     string            `json:"manifest_id"`
	ManifestSHA256 string            `json:"manifest_sha256"`
	ManifestCutoff string            `json:"manifest_cutoff"`
	Books          []bookCanonical   `json:"books"`
	Fees           []feeCanonical    `json:"fees"`
	Replays        []replayCanonical `json:"replays"`
}

type Recorder struct {
	canonical recorderCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

// ReplayResult is an immutable copy of one canonical research replay outcome.
// Numeric values remain exact canonical decimal strings.
type ReplayResult struct {
	Sequence         int
	DecisionAt       time.Time
	MarketID         string
	OutcomeID        uuid.UUID
	Side             Side
	Role             LiquidityRole
	Status           string
	Quantity         string
	FilledQuantity   string
	ResidualQuantity string
	GrossCash        string
	Fee              string
	NetCash          string
}

// BookLevelResult is a detached exact level from a point-in-time book view.
type BookLevelResult struct {
	Price string
	Size  string
}

// BookResult is the latest recorder book whose exchange and availability
// timestamps are both eligible at At. Slices and values are detached copies.
type BookResult struct {
	MarketID    string
	OutcomeID   uuid.UUID
	Venue       string
	SourceKey   string
	ExchangeAt  time.Time
	AvailableAt time.Time
	Revision    int
	Bids        []BookLevelResult
	Asks        []BookLevelResult
}

// FeeResult identifies the exact maker policy and rounded fee used by a
// downstream research simulation.
type FeeResult struct {
	SourceKey string
	Amount    string
}

type manifestObservation struct {
	kind       dataset.Kind
	partition  string
	instrument string
	available  time.Time
}

type parsedLevel struct {
	price decimal.Decimal
	size  decimal.Decimal
	row   levelCanonical
}

type parsedBook struct {
	input BookInput
	row   bookCanonical
	bids  []parsedLevel
	asks  []parsedLevel
}

type parsedFee struct {
	input FeePolicyInput
	row   feeCanonical
	rate  decimal.Decimal
}

func NewRecorder(input Input) (*Recorder, error) {
	if input.Manifest == nil {
		return nil, fmt.Errorf("prediction replay manifest is required")
	}
	index, err := manifestIndex(input.Manifest)
	if err != nil {
		return nil, err
	}
	books, err := normalizeBooks(input.Books, index)
	if err != nil {
		return nil, err
	}
	fees, err := normalizeFees(input.Fees, index)
	if err != nil {
		return nil, err
	}
	replays, err := buildReplays(input.Replays, books, fees, input.Manifest.DecisionCutoff())
	if err != nil {
		return nil, err
	}
	canonical := recorderCanonical{Schema: SchemaV1, State: "completed", ManifestID: input.Manifest.ID().String(), ManifestSHA256: input.Manifest.Digest(), ManifestCutoff: formatTime(input.Manifest.DecisionCutoff())}
	for _, value := range books {
		canonical.Books = append(canonical.Books, value.row)
	}
	for _, value := range fees {
		canonical.Fees = append(canonical.Fees, value.row)
	}
	canonical.Replays = replays
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal prediction replay: %w", err)
	}
	digest := hash(encoded)
	return &Recorder{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("prediction-book-fee-recorder", SchemaV1+"@sha256:"+digest)}, nil
}

func manifestIndex(manifest *dataset.Manifest) (map[string]manifestObservation, error) {
	result := map[string]manifestObservation{}
	for _, partition := range manifest.Partitions() {
		if partition.Kind != dataset.KindPredictionBooks && partition.Kind != dataset.KindPredictionFees {
			continue
		}
		for _, observation := range partition.Observations {
			key := evidenceKey(partition.Kind, observation.SourceKey, observation.ContentSHA256)
			if _, exists := result[key]; exists {
				return nil, fmt.Errorf("prediction replay manifest evidence is duplicated")
			}
			available, parseErr := time.Parse(timeLayout, observation.AvailableAt)
			if parseErr != nil {
				return nil, fmt.Errorf("prediction replay manifest time is invalid")
			}
			result[key] = manifestObservation{partition.Kind, partition.ContentSHA256, observation.InstrumentID, available}
		}
	}
	return result, nil
}

func normalizeBooks(values []BookInput, index map[string]manifestObservation) ([]parsedBook, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("prediction replay requires books")
	}
	result := make([]parsedBook, 0, len(values))
	bySource := map[string]BookInput{}
	identity := map[string]bool{}
	for _, value := range values {
		value.MarketID, value.Venue, value.SourceKey = strings.TrimSpace(value.MarketID), normalizeToken(value.Venue), strings.TrimSpace(value.SourceKey)
		obs, exists := index[evidenceKey(dataset.KindPredictionBooks, value.SourceKey, value.ContentSHA256)]
		identityKey := value.OutcomeID.String() + "\x00" + formatTime(value.ExchangeAt) + fmt.Sprintf("\x00%d", value.Revision)
		if value.MarketID == "" || value.OutcomeID == uuid.Nil || value.Venue == "" || value.SourceKey == "" || !digestPattern.MatchString(value.ContentSHA256) || !canonicalTime(value.ExchangeAt) || !canonicalTime(value.AvailableAt) || value.ExchangeAt.After(value.AvailableAt) || value.Revision < 0 || !exists || obs.kind != dataset.KindPredictionBooks || obs.instrument != value.OutcomeID.String() || !obs.available.Equal(value.AvailableAt) || identity[identityKey] {
			return nil, fmt.Errorf("prediction replay book evidence is invalid")
		}
		identity[identityKey] = true
		bids, bidRows, err := normalizeLevels("bid", value.Bids, true)
		if err != nil {
			return nil, err
		}
		asks, askRows, err := normalizeLevels("ask", value.Asks, false)
		if err != nil {
			return nil, err
		}
		if len(bids) == 0 || len(asks) == 0 || !bids[0].price.LessThan(asks[0].price) {
			return nil, fmt.Errorf("prediction replay book is empty or crossed")
		}
		levels := make([]levelCanonical, 0, len(bidRows)+len(askRows))
		levels = append(levels, bidRows...)
		levels = append(levels, askRows...)
		result = append(result, parsedBook{input: value, bids: bids, asks: asks, row: bookCanonical{evidenceCanonical{obs.partition, value.SourceKey, value.ContentSHA256, formatTime(value.AvailableAt)}, value.MarketID, value.OutcomeID.String(), value.Venue, formatTime(value.ExchangeAt), value.Revision, value.CorrectionOf, levels}})
		if _, duplicate := bySource[value.SourceKey]; duplicate {
			return nil, fmt.Errorf("prediction replay book source key is duplicated")
		}
		bySource[value.SourceKey] = value
	}
	for _, value := range values {
		if value.Revision == 0 {
			if value.CorrectionOf != "" {
				return nil, fmt.Errorf("prediction replay original book names a correction")
			}
			continue
		}
		prior, ok := bySource[value.CorrectionOf]
		if !ok || prior.OutcomeID != value.OutcomeID || prior.MarketID != value.MarketID || !prior.ExchangeAt.Equal(value.ExchangeAt) || prior.Revision+1 != value.Revision || !prior.AvailableAt.Before(value.AvailableAt) {
			return nil, fmt.Errorf("prediction replay book correction is invalid")
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i].input, result[j].input
		if left.MarketID != right.MarketID {
			return left.MarketID < right.MarketID
		}
		if left.OutcomeID != right.OutcomeID {
			return left.OutcomeID.String() < right.OutcomeID.String()
		}
		if !left.ExchangeAt.Equal(right.ExchangeAt) {
			return left.ExchangeAt.Before(right.ExchangeAt)
		}
		if left.Revision != right.Revision {
			return left.Revision < right.Revision
		}
		return left.SourceKey < right.SourceKey
	})
	return result, nil
}

func normalizeLevels(side string, values []LevelInput, descending bool) ([]parsedLevel, []levelCanonical, error) {
	parsed := make([]parsedLevel, 0, len(values))
	rows := make([]levelCanonical, 0, len(values))
	for i, value := range values {
		price, priceErr := exactDecimal(value.Price)
		size, sizeErr := exactDecimal(value.Size)
		if priceErr != nil || sizeErr != nil || !price.GreaterThan(decimal.Zero) || !price.LessThan(decimal.NewFromInt(1)) || !size.GreaterThan(decimal.Zero) {
			return nil, nil, fmt.Errorf("prediction replay %s level is invalid", side)
		}
		if i > 0 && (descending && !parsed[i-1].price.GreaterThan(price) || !descending && !parsed[i-1].price.LessThan(price)) {
			return nil, nil, fmt.Errorf("prediction replay %s levels are not strictly ordered", side)
		}
		row := levelCanonical{side, i, price.String(), size.String()}
		parsed = append(parsed, parsedLevel{price, size, row})
		rows = append(rows, row)
	}
	return parsed, rows, nil
}

func normalizeFees(values []FeePolicyInput, index map[string]manifestObservation) ([]parsedFee, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("prediction replay requires fee policies")
	}
	result := make([]parsedFee, 0, len(values))
	identity := map[string]bool{}
	for _, value := range values {
		value.Venue, value.SourceKey = normalizeToken(value.Venue), strings.TrimSpace(value.SourceKey)
		rate, rateErr := exactDecimal(value.Rate)
		obs, exists := index[evidenceKey(dataset.KindPredictionFees, value.SourceKey, value.ContentSHA256)]
		end := ""
		if value.EffectiveTo != nil {
			end = formatTime(*value.EffectiveTo)
		}
		key := value.InstrumentID.String() + "\x00" + value.Venue + "\x00" + string(value.Role) + "\x00" + formatTime(value.EffectiveFrom) + "\x00" + end
		validFormula := value.Formula == FeeNotionalBPS || value.Formula == FeeContractCurve
		validRole := value.Role == RoleMaker || value.Role == RoleTaker
		validRound := value.Rounding == RoundHalfUp || value.Rounding == RoundCeiling
		if value.InstrumentID == uuid.Nil || value.Venue == "" || !validRole || value.SourceKey == "" || !digestPattern.MatchString(value.ContentSHA256) || !canonicalTime(value.AvailableAt) || !canonicalTime(value.EffectiveFrom) || value.EffectiveTo != nil && (!canonicalTime(*value.EffectiveTo) || !value.EffectiveFrom.Before(*value.EffectiveTo)) || !validFormula || rateErr != nil || rate.IsNegative() || value.Scale < 0 || value.Scale > 12 || !validRound || !exists || obs.instrument != value.InstrumentID.String() || !obs.available.Equal(value.AvailableAt) || identity[key] {
			return nil, fmt.Errorf("prediction replay fee policy is invalid")
		}
		identity[key] = true
		result = append(result, parsedFee{input: value, rate: rate, row: feeCanonical{evidenceCanonical{obs.partition, value.SourceKey, value.ContentSHA256, formatTime(value.AvailableAt)}, value.InstrumentID.String(), value.Venue, string(value.Role), formatTime(value.EffectiveFrom), end, string(value.Formula), rate.String(), value.Scale, string(value.Rounding)}})
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i].input, result[j].input
		if left.InstrumentID != right.InstrumentID {
			return left.InstrumentID.String() < right.InstrumentID.String()
		}
		if left.Venue != right.Venue {
			return left.Venue < right.Venue
		}
		if left.Role != right.Role {
			return left.Role < right.Role
		}
		if !left.EffectiveFrom.Equal(right.EffectiveFrom) {
			return left.EffectiveFrom.Before(right.EffectiveFrom)
		}
		return left.SourceKey < right.SourceKey
	})
	for i := 1; i < len(result); i++ {
		prior, current := result[i-1].input, result[i].input
		if prior.InstrumentID == current.InstrumentID && prior.Venue == current.Venue && prior.Role == current.Role &&
			(prior.EffectiveTo == nil || current.EffectiveFrom.Before(*prior.EffectiveTo)) {
			return nil, fmt.Errorf("prediction replay fee policy windows overlap")
		}
	}
	return result, nil
}

func buildReplays(values []ReplayInput, books []parsedBook, fees []parsedFee, cutoff time.Time) ([]replayCanonical, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("prediction replay requires replay requests")
	}
	requests := append([]ReplayInput(nil), values...)
	sort.Slice(requests, func(i, j int) bool { return replayInputKey(requests[i]) < replayInputKey(requests[j]) })
	result := make([]replayCanonical, 0, len(requests))
	priorKey := ""
	for sequence, value := range requests {
		value.MarketID = strings.TrimSpace(value.MarketID)
		quantity, quantityErr := exactDecimal(value.Quantity)
		limit, limitErr := exactDecimal(value.LimitPrice)
		key := replayInputKey(value)
		if key == priorKey || value.MarketID == "" || value.OutcomeID == uuid.Nil || !canonicalTime(value.DecisionAt) || value.DecisionAt.After(cutoff) || value.Side != SideBuy && value.Side != SideSell || value.Role != RoleMaker && value.Role != RoleTaker || quantityErr != nil || !quantity.GreaterThan(decimal.Zero) || limitErr != nil || !limit.GreaterThan(decimal.Zero) || !limit.LessThan(decimal.NewFromInt(1)) {
			return nil, fmt.Errorf("prediction replay request is invalid")
		}
		priorKey = key
		row := replayCanonical{Sequence: sequence, DecisionAt: formatTime(value.DecisionAt), MarketID: value.MarketID, OutcomeID: value.OutcomeID.String(), Side: string(value.Side), Role: string(value.Role), Quantity: quantity.String(), LimitPrice: limit.String(), Status: "no_book", FilledQuantity: "0", ResidualQuantity: quantity.String(), WeightedPrice: "0", GrossCash: "0", Fee: "0", NetCash: "0", Fills: []fillCanonical{}}
		book := selectBook(books, value)
		if book == nil {
			result = append(result, row)
			continue
		}
		row.BookSourceKey = book.input.SourceKey
		fee := selectFee(fees, value, book.input.Venue)
		if fee == nil {
			row.Status = "no_fee_policy"
			result = append(result, row)
			continue
		}
		row.FeeSourceKey = fee.input.SourceKey
		levels := book.asks
		if value.Side == SideSell {
			levels = book.bids
		}
		remaining, gross := quantity, decimal.Zero
		for _, level := range levels {
			if value.Side == SideBuy && level.price.GreaterThan(limit) || value.Side == SideSell && level.price.LessThan(limit) {
				break
			}
			fill := decimal.Min(remaining, level.size)
			if fill.IsZero() {
				continue
			}
			levelGross := fill.Mul(level.price)
			row.Fills = append(row.Fills, fillCanonical{len(row.Fills), level.row.Sequence, level.price.String(), fill.String(), levelGross.String()})
			gross, remaining = gross.Add(levelGross), remaining.Sub(fill)
			if remaining.IsZero() {
				break
			}
		}
		filled := quantity.Sub(remaining)
		if filled.IsZero() {
			row.Status = "limit_blocked"
			result = append(result, row)
			continue
		}
		row.Status = "partial"
		if remaining.IsZero() {
			row.Status = "filled"
		}
		feeValue := calculateFee(*fee, row.Fills)
		net := gross.Add(feeValue)
		if value.Side == SideSell {
			net = gross.Sub(feeValue)
		}
		row.FilledQuantity, row.ResidualQuantity, row.WeightedPrice = filled.String(), remaining.String(), gross.Div(filled).String()
		row.GrossCash, row.Fee, row.NetCash = gross.String(), feeValue.String(), net.String()
		result = append(result, row)
	}
	return result, nil
}

func selectBook(values []parsedBook, request ReplayInput) *parsedBook {
	var selected *parsedBook
	for i := range values {
		value := &values[i]
		if value.input.MarketID != request.MarketID || value.input.OutcomeID != request.OutcomeID || value.input.ExchangeAt.After(request.DecisionAt) || value.input.AvailableAt.After(request.DecisionAt) {
			continue
		}
		if selected == nil || value.input.ExchangeAt.After(selected.input.ExchangeAt) || value.input.ExchangeAt.Equal(selected.input.ExchangeAt) && (value.input.Revision > selected.input.Revision || value.input.Revision == selected.input.Revision && value.input.SourceKey > selected.input.SourceKey) {
			selected = value
		}
	}
	return selected
}

func selectFee(values []parsedFee, request ReplayInput, venue string) *parsedFee {
	var selected *parsedFee
	for i := range values {
		value := &values[i]
		if value.input.InstrumentID != request.OutcomeID || value.input.Venue != venue || value.input.Role != request.Role || value.input.AvailableAt.After(request.DecisionAt) || value.input.EffectiveFrom.After(request.DecisionAt) || value.input.EffectiveTo != nil && !request.DecisionAt.Before(*value.input.EffectiveTo) {
			continue
		}
		if selected == nil || value.input.EffectiveFrom.After(selected.input.EffectiveFrom) || value.input.EffectiveFrom.Equal(selected.input.EffectiveFrom) && (value.input.AvailableAt.After(selected.input.AvailableAt) || value.input.AvailableAt.Equal(selected.input.AvailableAt) && value.input.SourceKey > selected.input.SourceKey) {
			selected = value
		}
	}
	return selected
}

func calculateFee(policy parsedFee, fills []fillCanonical) decimal.Decimal {
	value := decimal.Zero
	for _, fill := range fills {
		price := decimal.RequireFromString(fill.Price)
		quantity := decimal.RequireFromString(fill.Quantity)
		component := price.Mul(quantity)
		if policy.input.Formula == FeeNotionalBPS {
			component = component.Mul(policy.rate).Div(decimal.NewFromInt(10000))
		} else {
			component = component.Mul(decimal.NewFromInt(1).Sub(price)).Mul(policy.rate)
		}
		value = value.Add(component)
	}
	if policy.input.Rounding == RoundCeiling {
		return value.Shift(policy.input.Scale).Ceil().Shift(-policy.input.Scale)
	}
	return value.Round(policy.input.Scale)
}

func FromCanonical(id uuid.UUID, digest string, raw []byte, manifest *dataset.Manifest) (*Recorder, error) {
	var canonical recorderCanonical
	if id == uuid.Nil || manifest == nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil || canonical.Schema != SchemaV1 || canonical.State != "completed" || canonical.ManifestID != manifest.ID().String() || canonical.ManifestSHA256 != manifest.Digest() || canonical.ManifestCutoff != formatTime(manifest.DecisionCutoff()) {
		return nil, fmt.Errorf("prediction replay envelope is invalid")
	}
	input, err := canonicalInput(canonical, manifest)
	if err != nil {
		return nil, err
	}
	rebuilt, err := NewRecorder(input)
	if err != nil || rebuilt.Digest() != digest || !bytes.Equal(rebuilt.CanonicalBytes(), raw) || rebuilt.ID() != id {
		return nil, fmt.Errorf("prediction replay canonical graph does not reconstruct")
	}
	return rebuilt, nil
}

func canonicalInput(value recorderCanonical, manifest *dataset.Manifest) (Input, error) {
	input := Input{Manifest: manifest}
	for _, row := range value.Books {
		outcome, idErr := uuid.Parse(row.OutcomeID)
		exchange, exErr := time.Parse(timeLayout, row.ExchangeAt)
		available, avErr := time.Parse(timeLayout, row.AvailableAt)
		if idErr != nil || exErr != nil || avErr != nil {
			return Input{}, fmt.Errorf("prediction replay canonical book is invalid")
		}
		book := BookInput{row.MarketID, outcome, row.Venue, row.SourceKey, row.ContentSHA256, exchange, available, row.Revision, row.CorrectionOf, nil, nil}
		for _, level := range row.Levels {
			switch level.Side {
			case "bid":
				book.Bids = append(book.Bids, LevelInput{level.Price, level.Size})
			case "ask":
				book.Asks = append(book.Asks, LevelInput{level.Price, level.Size})
			default:
				return Input{}, fmt.Errorf("prediction replay canonical level is invalid")
			}
		}
		input.Books = append(input.Books, book)
	}
	for _, row := range value.Fees {
		instrumentID, idErr := uuid.Parse(row.InstrumentID)
		available, avErr := time.Parse(timeLayout, row.AvailableAt)
		from, fromErr := time.Parse(timeLayout, row.EffectiveFrom)
		if idErr != nil || avErr != nil || fromErr != nil {
			return Input{}, fmt.Errorf("prediction replay canonical fee is invalid")
		}
		var to *time.Time
		if row.EffectiveTo != "" {
			parsed, parseErr := time.Parse(timeLayout, row.EffectiveTo)
			if parseErr != nil {
				return Input{}, fmt.Errorf("prediction replay canonical fee end is invalid")
			}
			to = &parsed
		}
		input.Fees = append(input.Fees, FeePolicyInput{instrumentID, row.Venue, LiquidityRole(row.Role), row.SourceKey, row.ContentSHA256, available, from, to, FeeFormula(row.Formula), row.Rate, row.Scale, RoundingMode(row.Rounding)})
	}
	for _, row := range value.Replays {
		decision, timeErr := time.Parse(timeLayout, row.DecisionAt)
		outcome, idErr := uuid.Parse(row.OutcomeID)
		if timeErr != nil || idErr != nil {
			return Input{}, fmt.Errorf("prediction replay canonical request is invalid")
		}
		input.Replays = append(input.Replays, ReplayInput{decision, row.MarketID, outcome, Side(row.Side), LiquidityRole(row.Role), row.Quantity, row.LimitPrice})
	}
	return input, nil
}

func exactDecimal(value string) (decimal.Decimal, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "eE+") {
		return decimal.Zero, fmt.Errorf("decimal is not canonical")
	}
	parsed, err := decimal.NewFromString(value)
	if err != nil || parsed.String() != value || parsed.Exponent() < -12 {
		return decimal.Zero, fmt.Errorf("decimal is not exact canonical scale")
	}
	return parsed, nil
}

func replayInputKey(value ReplayInput) string {
	return formatTime(value.DecisionAt) + "\x00" + value.MarketID + "\x00" + value.OutcomeID.String() + "\x00" + string(value.Side) + "\x00" + string(value.Role) + "\x00" + value.Quantity + "\x00" + value.LimitPrice
}

func evidenceKey(kind dataset.Kind, source, digest string) string {
	return string(kind) + "\x00" + source + "\x00" + digest
}
func normalizeToken(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
func canonicalTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Nanosecond()%1000 == 0
}
func formatTime(value time.Time) string { return value.Format(timeLayout) }
func hash(value []byte) string          { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }

func decodeExact(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("canonical JSON contains extra data")
	}
	return nil
}

func (r *Recorder) ID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return r.id
}

func (r *Recorder) Digest() string {
	if r == nil {
		return ""
	}
	return r.digest
}

func (r *Recorder) CanonicalBytes() json.RawMessage {
	if r == nil {
		return nil
	}
	return append(json.RawMessage(nil), r.bytes...)
}

func (r *Recorder) ManifestID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return uuid.MustParse(r.canonical.ManifestID)
}

func (r *Recorder) BookCount() int {
	if r == nil {
		return 0
	}
	return len(r.canonical.Books)
}

func (r *Recorder) FeeCount() int {
	if r == nil {
		return 0
	}
	return len(r.canonical.Fees)
}

func (r *Recorder) ReplayCount() int {
	if r == nil {
		return 0
	}
	return len(r.canonical.Replays)
}

// ReplayResults returns detached typed summaries for downstream research
// boundaries. Callers cannot mutate recorder identity through the returned data.
func (r *Recorder) ReplayResults() ([]ReplayResult, error) {
	if r == nil {
		return nil, fmt.Errorf("prediction recorder is required")
	}
	result := make([]ReplayResult, 0, len(r.canonical.Replays))
	for _, row := range r.canonical.Replays {
		decisionAt, timeErr := time.Parse(timeLayout, row.DecisionAt)
		outcomeID, idErr := uuid.Parse(row.OutcomeID)
		if timeErr != nil || idErr != nil {
			return nil, fmt.Errorf("prediction recorder replay result is invalid")
		}
		result = append(result, ReplayResult{
			Sequence: row.Sequence, DecisionAt: decisionAt, MarketID: row.MarketID,
			OutcomeID: outcomeID, Side: Side(row.Side), Role: LiquidityRole(row.Role), Status: row.Status,
			Quantity: row.Quantity, FilledQuantity: row.FilledQuantity, ResidualQuantity: row.ResidualQuantity,
			GrossCash: row.GrossCash, Fee: row.Fee, NetCash: row.NetCash,
		})
	}
	return result, nil
}

// BookAt returns the latest point-in-time book for one market and outcome.
func (r *Recorder) BookAt(at time.Time, marketID string, outcomeID uuid.UUID) (BookResult, error) {
	if r == nil || !canonicalTime(at) || marketID == "" || outcomeID == uuid.Nil {
		return BookResult{}, fmt.Errorf("prediction recorder book query is invalid")
	}
	cutoff, err := time.Parse(timeLayout, r.canonical.ManifestCutoff)
	if err != nil || at.After(cutoff) {
		return BookResult{}, fmt.Errorf("prediction recorder book query exceeds cutoff")
	}
	var selected *bookCanonical
	for i := range r.canonical.Books {
		row := &r.canonical.Books[i]
		exchangeAt, exchangeErr := time.Parse(timeLayout, row.ExchangeAt)
		availableAt, availableErr := time.Parse(timeLayout, row.AvailableAt)
		if exchangeErr != nil || availableErr != nil || row.MarketID != marketID || row.OutcomeID != outcomeID.String() || exchangeAt.After(at) || availableAt.After(at) {
			continue
		}
		if selected == nil {
			selected = row
			continue
		}
		selectedExchange, _ := time.Parse(timeLayout, selected.ExchangeAt)
		if exchangeAt.After(selectedExchange) || exchangeAt.Equal(selectedExchange) && (row.Revision > selected.Revision || row.Revision == selected.Revision && row.SourceKey > selected.SourceKey) {
			selected = row
		}
	}
	if selected == nil {
		return BookResult{}, fmt.Errorf("prediction recorder has no eligible book")
	}
	result := BookResult{MarketID: selected.MarketID, OutcomeID: outcomeID, Venue: selected.Venue, SourceKey: selected.SourceKey, Revision: selected.Revision}
	result.ExchangeAt, _ = time.Parse(timeLayout, selected.ExchangeAt)
	result.AvailableAt, _ = time.Parse(timeLayout, selected.AvailableAt)
	for _, level := range selected.Levels {
		value := BookLevelResult{Price: level.Price, Size: level.Size}
		if level.Side == "bid" {
			result.Bids = append(result.Bids, value)
		} else {
			result.Asks = append(result.Asks, value)
		}
	}
	return result, nil
}

// MakerFeeAt applies the exact point-in-time OVR-505 maker fee formula and
// rounding to one simulated single-price fill.
func (r *Recorder) MakerFeeAt(at time.Time, outcomeID uuid.UUID, venue, priceValue, quantityValue string) (FeeResult, error) {
	if r == nil || !canonicalTime(at) || outcomeID == uuid.Nil || venue == "" {
		return FeeResult{}, fmt.Errorf("prediction recorder maker fee query is invalid")
	}
	cutoff, cutoffErr := time.Parse(timeLayout, r.canonical.ManifestCutoff)
	price, priceErr := exactDecimal(priceValue)
	quantity, quantityErr := exactDecimal(quantityValue)
	if cutoffErr != nil || at.After(cutoff) || priceErr != nil || quantityErr != nil || !price.GreaterThan(decimal.Zero) || !price.LessThan(decimal.NewFromInt(1)) || !quantity.GreaterThan(decimal.Zero) {
		return FeeResult{}, fmt.Errorf("prediction recorder maker fee values are invalid")
	}
	var selected *feeCanonical
	for i := range r.canonical.Fees {
		row := &r.canonical.Fees[i]
		availableAt, availableErr := time.Parse(timeLayout, row.AvailableAt)
		effectiveFrom, fromErr := time.Parse(timeLayout, row.EffectiveFrom)
		var effectiveTo time.Time
		if row.EffectiveTo != "" {
			effectiveTo, _ = time.Parse(timeLayout, row.EffectiveTo)
		}
		if availableErr != nil || fromErr != nil || row.InstrumentID != outcomeID.String() || row.Venue != normalizeToken(venue) || row.Role != string(RoleMaker) || availableAt.After(at) || effectiveFrom.After(at) || row.EffectiveTo != "" && !at.Before(effectiveTo) {
			continue
		}
		if selected == nil {
			selected = row
			continue
		}
		selectedFrom, _ := time.Parse(timeLayout, selected.EffectiveFrom)
		selectedAvailable, _ := time.Parse(timeLayout, selected.AvailableAt)
		if effectiveFrom.After(selectedFrom) || effectiveFrom.Equal(selectedFrom) && (availableAt.After(selectedAvailable) || availableAt.Equal(selectedAvailable) && row.SourceKey > selected.SourceKey) {
			selected = row
		}
	}
	if selected == nil {
		return FeeResult{}, fmt.Errorf("prediction recorder has no eligible maker fee")
	}
	rate := decimal.RequireFromString(selected.Rate)
	fee := price.Mul(quantity)
	if selected.Formula == string(FeeNotionalBPS) {
		fee = fee.Mul(rate).Div(decimal.NewFromInt(10000))
	} else {
		fee = fee.Mul(decimal.NewFromInt(1).Sub(price)).Mul(rate)
	}
	if selected.Rounding == string(RoundCeiling) {
		fee = fee.Shift(selected.Scale).Ceil().Shift(-selected.Scale)
	} else {
		fee = fee.Round(selected.Scale)
	}
	return FeeResult{SourceKey: selected.SourceKey, Amount: fee.String()}, nil
}
