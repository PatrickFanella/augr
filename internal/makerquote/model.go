// Package makerquote evaluates deterministic research-only passive quotes over
// immutable prediction-market books. It cannot create intents, reserve capital,
// schedule work, route orders, or promote strategies.
package makerquote

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

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/predictionreplay"
)

const (
	SchemaV1   = "maker-quote-evaluation-v1"
	timeLayout = "2006-01-02T15:04:05.000000Z"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type ScenarioInput struct {
	Key          string
	Weight       string
	HorizonAt    time.Time
	QueueOutflow string
}

type Input struct {
	Recorder                *predictionreplay.Recorder
	CandidateKey            string
	MarketID                string
	OutcomeID               uuid.UUID
	Side                    predictionreplay.Side
	DecisionAt              time.Time
	QuotePrice              string
	QuoteQuantity           string
	PriorQueue              string
	StartingInventory       string
	InventoryLimit          string
	HourlyInventoryCostRate string
	MinimumExpectedNet      string
	Scenarios               []ScenarioInput
}

type scenarioCanonical struct {
	Sequence          int    `json:"sequence"`
	Key               string `json:"key"`
	Weight            string `json:"weight"`
	HorizonAt         string `json:"horizon_at"`
	QueueOutflow      string `json:"queue_outflow"`
	MarkBookSourceKey string `json:"mark_book_source_key"`
	MarkPrice         string `json:"mark_price"`
	FilledQuantity    string `json:"filled_quantity"`
	ResidualQuantity  string `json:"residual_quantity"`
	PostFillInventory string `json:"post_fill_inventory"`
	GrossCapture      string `json:"gross_capture"`
	MakerFee          string `json:"maker_fee"`
	InventoryCost     string `json:"inventory_cost"`
	NetCapture        string `json:"net_capture"`
}

type candidateCanonical struct {
	Schema                  string              `json:"schema"`
	State                   string              `json:"state"`
	Reason                  string              `json:"reason"`
	RecorderID              string              `json:"recorder_id"`
	RecorderSHA256          string              `json:"recorder_sha256"`
	CandidateKey            string              `json:"candidate_key"`
	MarketID                string              `json:"market_id"`
	OutcomeID               string              `json:"outcome_id"`
	Side                    string              `json:"side"`
	DecisionAt              string              `json:"decision_at"`
	QuoteBookSourceKey      string              `json:"quote_book_source_key"`
	Venue                   string              `json:"venue"`
	QuotePrice              string              `json:"quote_price"`
	QuoteQuantity           string              `json:"quote_quantity"`
	DisplayedQueue          string              `json:"displayed_queue"`
	PriorQueue              string              `json:"prior_queue"`
	QueueAhead              string              `json:"queue_ahead"`
	StartingInventory       string              `json:"starting_inventory"`
	InventoryLimit          string              `json:"inventory_limit"`
	HourlyInventoryCostRate string              `json:"hourly_inventory_cost_rate"`
	MinimumExpectedNet      string              `json:"minimum_expected_net"`
	FilledScenarioCount     int                 `json:"filled_scenario_count"`
	ExpectedGrossCapture    string              `json:"expected_gross_capture"`
	ExpectedMakerFee        string              `json:"expected_maker_fee"`
	ExpectedInventoryCost   string              `json:"expected_inventory_cost"`
	ExpectedNetCapture      string              `json:"expected_net_capture"`
	Scenarios               []scenarioCanonical `json:"scenarios"`
}

type Candidate struct {
	canonical candidateCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewCandidate(input Input) (*Candidate, error) {
	if input.Recorder == nil || input.OutcomeID == uuid.Nil || strings.TrimSpace(input.CandidateKey) == "" || strings.TrimSpace(input.MarketID) == "" || !canonicalTime(input.DecisionAt) || input.Side != predictionreplay.SideBuy && input.Side != predictionreplay.SideSell || len(input.Scenarios) == 0 {
		return nil, fmt.Errorf("maker quote input is invalid")
	}
	quotePrice, priceErr := exactDecimal(input.QuotePrice)
	quantity, quantityErr := exactDecimal(input.QuoteQuantity)
	priorQueue, priorErr := exactDecimal(input.PriorQueue)
	startingInventory, startingErr := exactDecimal(input.StartingInventory)
	inventoryLimit, limitErr := exactDecimal(input.InventoryLimit)
	hourlyRate, rateErr := exactDecimal(input.HourlyInventoryCostRate)
	minimum, minimumErr := exactDecimal(input.MinimumExpectedNet)
	if priceErr != nil || quantityErr != nil || priorErr != nil || startingErr != nil || limitErr != nil || rateErr != nil || minimumErr != nil || !quotePrice.GreaterThan(decimal.Zero) || !quotePrice.LessThan(decimal.NewFromInt(1)) || !quantity.GreaterThan(decimal.Zero) || priorQueue.IsNegative() || !inventoryLimit.GreaterThan(decimal.Zero) || hourlyRate.IsNegative() || minimum.IsNegative() {
		return nil, fmt.Errorf("maker quote decimal is invalid")
	}
	canonical := candidateCanonical{Schema: SchemaV1, State: "rejected", Reason: "invalid_quote", RecorderID: input.Recorder.ID().String(), RecorderSHA256: input.Recorder.Digest(), CandidateKey: strings.TrimSpace(input.CandidateKey), MarketID: strings.TrimSpace(input.MarketID), OutcomeID: input.OutcomeID.String(), Side: string(input.Side), DecisionAt: formatTime(input.DecisionAt), QuotePrice: quotePrice.String(), QuoteQuantity: quantity.String(), DisplayedQueue: "0", PriorQueue: priorQueue.String(), QueueAhead: priorQueue.String(), StartingInventory: startingInventory.String(), InventoryLimit: inventoryLimit.String(), HourlyInventoryCostRate: hourlyRate.String(), MinimumExpectedNet: minimum.String(), ExpectedGrossCapture: "0", ExpectedMakerFee: "0", ExpectedInventoryCost: "0", ExpectedNetCapture: "0", Scenarios: []scenarioCanonical{}}
	book, bookErr := input.Recorder.BookAt(input.DecisionAt, canonical.MarketID, input.OutcomeID)
	if bookErr != nil || len(book.Bids) == 0 || len(book.Asks) == 0 {
		return finalize(canonical)
	}
	inside := book.Bids[0]
	if input.Side == predictionreplay.SideSell {
		inside = book.Asks[0]
	}
	if inside.Price != quotePrice.String() {
		return finalize(canonical)
	}
	displayed := decimal.RequireFromString(inside.Size)
	queueAhead := displayed.Add(priorQueue)
	canonical.QuoteBookSourceKey, canonical.Venue = book.SourceKey, book.Venue
	canonical.DisplayedQueue, canonical.QueueAhead = displayed.String(), queueAhead.String()

	type parsedScenario struct {
		input  ScenarioInput
		weight decimal.Decimal
		flow   decimal.Decimal
	}
	parsed := make([]parsedScenario, 0, len(input.Scenarios))
	seen, weightSum := map[string]bool{}, decimal.Zero
	invalidScenarios := false
	for _, value := range input.Scenarios {
		value.Key = strings.TrimSpace(value.Key)
		weight, weightErr := exactDecimal(value.Weight)
		flow, flowErr := exactDecimal(value.QueueOutflow)
		if value.Key == "" || seen[value.Key] || weightErr != nil || flowErr != nil || !weight.GreaterThan(decimal.Zero) || flow.IsNegative() || !canonicalTime(value.HorizonAt) || !value.HorizonAt.After(input.DecisionAt) {
			invalidScenarios = true
			break
		}
		seen[value.Key], weightSum = true, weightSum.Add(weight)
		parsed = append(parsed, parsedScenario{value, weight, flow})
	}
	sort.Slice(parsed, func(i, j int) bool { return parsed[i].input.Key < parsed[j].input.Key })
	if invalidScenarios || !weightSum.Equal(decimal.NewFromInt(1)) {
		canonical.Reason = "invalid_scenarios"
		return finalize(canonical)
	}

	expectedGross, expectedFee := decimal.Zero, decimal.Zero
	expectedInventoryCost, expectedNet := decimal.Zero, decimal.Zero
	inventoryExceeded := false
	for sequence, value := range parsed {
		markBook, markErr := input.Recorder.BookAt(value.input.HorizonAt, canonical.MarketID, input.OutcomeID)
		if markErr != nil || len(markBook.Bids) == 0 || len(markBook.Asks) == 0 {
			canonical.Reason = "invalid_scenarios"
			return finalize(canonical)
		}
		mark := decimal.RequireFromString(markBook.Bids[0].Price).Add(decimal.RequireFromString(markBook.Asks[0].Price)).Div(decimal.NewFromInt(2))
		fill := decimal.Min(decimal.Max(value.flow.Sub(queueAhead), decimal.Zero), quantity)
		residual := quantity.Sub(fill)
		postInventory := startingInventory
		gross, fee := decimal.Zero, decimal.Zero
		if !fill.IsZero() {
			canonical.FilledScenarioCount++
			if input.Side == predictionreplay.SideBuy {
				postInventory = postInventory.Add(fill)
				gross = mark.Sub(quotePrice).Mul(fill)
			} else {
				postInventory = postInventory.Sub(fill)
				gross = quotePrice.Sub(mark).Mul(fill)
			}
			feeResult, feeErr := input.Recorder.MakerFeeAt(input.DecisionAt, input.OutcomeID, book.Venue, quotePrice.String(), fill.String())
			if feeErr != nil {
				return finalize(canonical)
			}
			fee = decimal.RequireFromString(feeResult.Amount)
		}
		if postInventory.Abs().GreaterThan(inventoryLimit) {
			inventoryExceeded = true
		}
		hours := decimal.NewFromInt(value.input.HorizonAt.Sub(input.DecisionAt).Microseconds()).Div(decimal.NewFromInt(3_600_000_000))
		inventoryCost := postInventory.Abs().Mul(quotePrice).Mul(hourlyRate).Mul(hours)
		net := gross.Sub(fee).Sub(inventoryCost)
		canonical.Scenarios = append(canonical.Scenarios, scenarioCanonical{sequence, value.input.Key, value.weight.String(), formatTime(value.input.HorizonAt), value.flow.String(), markBook.SourceKey, mark.String(), fill.String(), residual.String(), postInventory.String(), gross.String(), fee.String(), inventoryCost.String(), net.String()})
		expectedGross = expectedGross.Add(value.weight.Mul(gross))
		expectedFee = expectedFee.Add(value.weight.Mul(fee))
		expectedInventoryCost = expectedInventoryCost.Add(value.weight.Mul(inventoryCost))
		expectedNet = expectedNet.Add(value.weight.Mul(net))
	}
	canonical.ExpectedGrossCapture, canonical.ExpectedMakerFee = expectedGross.String(), expectedFee.String()
	canonical.ExpectedInventoryCost, canonical.ExpectedNetCapture = expectedInventoryCost.String(), expectedNet.String()
	switch {
	case canonical.FilledScenarioCount == 0:
		canonical.Reason = "no_fill"
	case inventoryExceeded:
		canonical.Reason = "inventory_limit"
	case !expectedNet.GreaterThan(decimal.Zero) || !expectedNet.GreaterThan(minimum):
		canonical.Reason = "nonpositive_net_capture"
	default:
		canonical.State, canonical.Reason = "qualified", ""
	}
	return finalize(canonical)
}

func finalize(canonical candidateCanonical) (*Candidate, error) {
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal maker quote: %w", err)
	}
	digest := hash(encoded)
	return &Candidate{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("maker-quote-evaluation", SchemaV1+"@sha256:"+digest)}, nil
}

func FromCanonical(id uuid.UUID, digest string, raw []byte, recorder *predictionreplay.Recorder) (*Candidate, error) {
	var canonical candidateCanonical
	if id == uuid.Nil || recorder == nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil || canonical.Schema != SchemaV1 || canonical.RecorderID != recorder.ID().String() || canonical.RecorderSHA256 != recorder.Digest() {
		return nil, fmt.Errorf("maker quote canonical evidence is invalid")
	}
	input, err := canonicalInput(canonical, recorder)
	if err != nil {
		return nil, err
	}
	rebuilt, err := NewCandidate(input)
	if err != nil || rebuilt.id != id || rebuilt.digest != digest || !bytes.Equal(rebuilt.bytes, raw) {
		return nil, fmt.Errorf("maker quote canonical graph does not reconstruct")
	}
	return rebuilt, nil
}

func canonicalInput(value candidateCanonical, recorder *predictionreplay.Recorder) (Input, error) {
	outcomeID, idErr := uuid.Parse(value.OutcomeID)
	decisionAt, timeErr := time.Parse(timeLayout, value.DecisionAt)
	if idErr != nil || timeErr != nil {
		return Input{}, fmt.Errorf("maker quote canonical identity is invalid")
	}
	input := Input{Recorder: recorder, CandidateKey: value.CandidateKey, MarketID: value.MarketID, OutcomeID: outcomeID, Side: predictionreplay.Side(value.Side), DecisionAt: decisionAt, QuotePrice: value.QuotePrice, QuoteQuantity: value.QuoteQuantity, PriorQueue: value.PriorQueue, StartingInventory: value.StartingInventory, InventoryLimit: value.InventoryLimit, HourlyInventoryCostRate: value.HourlyInventoryCostRate, MinimumExpectedNet: value.MinimumExpectedNet}
	for _, row := range value.Scenarios {
		horizonAt, err := time.Parse(timeLayout, row.HorizonAt)
		if err != nil {
			return Input{}, fmt.Errorf("maker quote canonical scenario is invalid")
		}
		input.Scenarios = append(input.Scenarios, ScenarioInput{row.Key, row.Weight, horizonAt, row.QueueOutflow})
	}
	return input, nil
}

func exactDecimal(value string) (decimal.Decimal, error) {
	parsed, err := decimal.NewFromString(value)
	if err != nil || parsed.String() != value || parsed.Exponent() < -18 {
		return decimal.Zero, fmt.Errorf("decimal is not exact canonical")
	}
	return parsed, nil
}

func canonicalTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Nanosecond()%1000 == 0
}
func formatTime(value time.Time) string { return value.UTC().Format(timeLayout) }
func hash(value []byte) string          { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }

func decodeExact(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("canonical JSON contains extra data")
	}
	return nil
}

func (c *Candidate) ID() uuid.UUID {
	if c == nil {
		return uuid.Nil
	}
	return c.id
}

func (c *Candidate) Digest() string {
	if c == nil {
		return ""
	}
	return c.digest
}

func (c *Candidate) CanonicalBytes() json.RawMessage {
	if c == nil {
		return nil
	}
	return append(json.RawMessage(nil), c.bytes...)
}

func (c *Candidate) RecorderID() uuid.UUID {
	if c == nil {
		return uuid.Nil
	}
	return uuid.MustParse(c.canonical.RecorderID)
}

func (c *Candidate) Qualified() bool { return c != nil && c.canonical.State == "qualified" }

func (c *Candidate) State() string {
	if c == nil {
		return ""
	}
	return c.canonical.State
}

func (c *Candidate) Reason() string {
	if c == nil {
		return ""
	}
	return c.canonical.Reason
}

func (c *Candidate) ExpectedNetCapture() string {
	if c == nil {
		return ""
	}
	return c.canonical.ExpectedNetCapture
}

func (c *Candidate) ScenarioCount() int {
	if c == nil {
		return 0
	}
	return len(c.canonical.Scenarios)
}
