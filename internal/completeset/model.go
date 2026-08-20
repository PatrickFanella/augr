// Package completeset evaluates prediction-market complete sets from immutable
// OVR-505 replay evidence. It cannot reserve runtime capital or route orders.
package completeset

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

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/predictionreplay"
)

const SchemaV1 = "complete-set-arbitrage-v1"

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type LegBinding struct {
	OutcomeID      uuid.UUID
	EntrySequence  int
	UnwindSequence int
}

type Input struct {
	Recorder         *predictionreplay.Recorder
	MarketID         string
	Outcomes         []uuid.UUID
	Legs             []LegBinding
	SetQuantity      string
	PayoutPerSet     string
	AvailableCapital string
	MinimumProfit    string
}

type legCanonical struct {
	Sequence       int    `json:"sequence"`
	OutcomeID      string `json:"outcome_id"`
	EntrySequence  int    `json:"entry_sequence"`
	UnwindSequence int    `json:"unwind_sequence"`
	EntryCost      string `json:"entry_cost"`
	UnwindProceeds string `json:"unwind_proceeds"`
	OrphanLoss     string `json:"orphan_loss"`
}

type bindingCanonical struct {
	OutcomeID      string `json:"outcome_id"`
	EntrySequence  int    `json:"entry_sequence"`
	UnwindSequence int    `json:"unwind_sequence"`
}

type scenarioLegCanonical struct {
	Sequence       int    `json:"sequence"`
	OutcomeID      string `json:"outcome_id"`
	EntryCost      string `json:"entry_cost"`
	UnwindProceeds string `json:"unwind_proceeds"`
	Loss           string `json:"loss"`
}

type scenarioCanonical struct {
	Sequence       int                    `json:"sequence"`
	Key            string                 `json:"key"`
	EntryCost      string                 `json:"entry_cost"`
	UnwindProceeds string                 `json:"unwind_proceeds"`
	Loss           string                 `json:"loss"`
	Legs           []scenarioLegCanonical `json:"legs"`
}

type candidateCanonical struct {
	Schema                 string              `json:"schema"`
	State                  string              `json:"state"`
	Reason                 string              `json:"reason"`
	RecorderID             string              `json:"recorder_id"`
	RecorderSHA256         string              `json:"recorder_sha256"`
	MarketID               string              `json:"market_id"`
	Outcomes               []string            `json:"outcomes"`
	SetQuantity            string              `json:"set_quantity"`
	PayoutPerSet           string              `json:"payout_per_set"`
	AvailableCapital       string              `json:"available_capital"`
	MinimumProfit          string              `json:"minimum_profit"`
	EntryCost              string              `json:"entry_cost"`
	Payout                 string              `json:"payout"`
	AfterCostProfit        string              `json:"after_cost_profit"`
	WorstOrphanKey         string              `json:"worst_orphan_key"`
	WorstOrphanLoss        string              `json:"worst_orphan_loss"`
	ReservedCapital        string              `json:"reserved_capital"`
	ProfitAfterOrphanGuard string              `json:"profit_after_orphan_guard"`
	Bindings               []bindingCanonical  `json:"bindings"`
	Legs                   []legCanonical      `json:"legs"`
	Scenarios              []scenarioCanonical `json:"scenarios"`
}

type Candidate struct {
	canonical candidateCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewCandidate(input Input) (*Candidate, error) {
	if input.Recorder == nil {
		return nil, fmt.Errorf("complete set recorder is required")
	}
	marketID := strings.TrimSpace(input.MarketID)
	quantity, quantityErr := exactDecimal(input.SetQuantity)
	payoutPerSet, payoutErr := exactDecimal(input.PayoutPerSet)
	available, availableErr := exactDecimal(input.AvailableCapital)
	minimum, minimumErr := exactDecimal(input.MinimumProfit)
	if marketID == "" || quantityErr != nil || !quantity.GreaterThan(decimal.Zero) || payoutErr != nil || !payoutPerSet.GreaterThan(decimal.Zero) || availableErr != nil || available.IsNegative() || minimumErr != nil || minimum.IsNegative() {
		return nil, fmt.Errorf("complete set economics are invalid")
	}
	outcomes, err := normalizeOutcomes(input.Outcomes)
	if err != nil {
		return nil, err
	}
	bindings, err := normalizeBindings(input.Legs)
	if err != nil {
		return nil, err
	}
	canonical := candidateCanonical{
		Schema: SchemaV1, State: "rejected", RecorderID: input.Recorder.ID().String(), RecorderSHA256: input.Recorder.Digest(),
		MarketID: marketID, Outcomes: outcomes, SetQuantity: quantity.String(), PayoutPerSet: payoutPerSet.String(),
		AvailableCapital: available.String(), MinimumProfit: minimum.String(), EntryCost: "0", Payout: quantity.Mul(payoutPerSet).String(),
		AfterCostProfit: quantity.Mul(payoutPerSet).String(), WorstOrphanLoss: "0", ReservedCapital: "0",
		ProfitAfterOrphanGuard: quantity.Mul(payoutPerSet).String(), Bindings: []bindingCanonical{}, Legs: []legCanonical{}, Scenarios: []scenarioCanonical{},
	}
	for _, binding := range bindings {
		canonical.Bindings = append(canonical.Bindings, bindingCanonical{binding.OutcomeID.String(), binding.EntrySequence, binding.UnwindSequence})
	}
	if len(bindings) != len(outcomes) || !bindingsCoverOutcomes(bindings, outcomes) {
		canonical.Reason = "incomplete_set"
		return finish(canonical)
	}
	results, err := input.Recorder.ReplayResults()
	if err != nil {
		return nil, err
	}
	bySequence := make(map[int]predictionreplay.ReplayResult, len(results))
	for _, value := range results {
		bySequence[value.Sequence] = value
	}
	legs := make([]legCanonical, 0, len(bindings))
	valid := true
	var commonDecision string
	for sequence, binding := range bindings {
		entry, entryOK := bySequence[binding.EntrySequence]
		unwind, unwindOK := bySequence[binding.UnwindSequence]
		decision := entry.DecisionAt.Format("2006-01-02T15:04:05.000000Z")
		if sequence == 0 {
			commonDecision = decision
		}
		if !entryOK || !unwindOK || entry.MarketID != marketID || unwind.MarketID != marketID || entry.OutcomeID != binding.OutcomeID || unwind.OutcomeID != binding.OutcomeID || !entry.DecisionAt.Equal(unwind.DecisionAt) || decision != commonDecision || entry.Side != predictionreplay.SideBuy || entry.Role != predictionreplay.RoleTaker || unwind.Side != predictionreplay.SideSell || entry.Status != "filled" || unwind.Status != "filled" || entry.Quantity != quantity.String() || unwind.Quantity != quantity.String() || entry.FilledQuantity != quantity.String() || unwind.FilledQuantity != quantity.String() || entry.ResidualQuantity != "0" || unwind.ResidualQuantity != "0" {
			valid = false
			break
		}
		entryCost, entryErr := exactDecimal(entry.NetCash)
		unwindProceeds, unwindErr := exactDecimal(unwind.NetCash)
		if entryErr != nil || unwindErr != nil || entryCost.IsNegative() || unwindProceeds.IsNegative() {
			valid = false
			break
		}
		loss := decimal.Max(entryCost.Sub(unwindProceeds), decimal.Zero)
		legs = append(legs, legCanonical{sequence, binding.OutcomeID.String(), binding.EntrySequence, binding.UnwindSequence, entryCost.String(), unwindProceeds.String(), loss.String()})
	}
	if !valid {
		canonical.Reason = "invalid_replay"
		return finish(canonical)
	}
	canonical.Legs = legs
	entryCost := decimal.Zero
	for _, leg := range legs {
		entryCost = entryCost.Add(decimal.RequireFromString(leg.EntryCost))
	}
	canonical.EntryCost = entryCost.String()
	payout := quantity.Mul(payoutPerSet)
	afterCost := payout.Sub(entryCost)
	canonical.Payout, canonical.AfterCostProfit = payout.String(), afterCost.String()
	scenarios := enumerateScenarios(legs)
	canonical.Scenarios = scenarios
	worstLoss, worstKey := decimal.Zero, ""
	for _, scenario := range scenarios {
		loss := decimal.RequireFromString(scenario.Loss)
		if loss.GreaterThan(worstLoss) || loss.Equal(worstLoss) && (worstKey == "" || scenario.Key < worstKey) {
			worstLoss, worstKey = loss, scenario.Key
		}
	}
	reserved := entryCost.Add(worstLoss)
	guarded := afterCost.Sub(worstLoss)
	canonical.WorstOrphanKey, canonical.WorstOrphanLoss = worstKey, worstLoss.String()
	canonical.ReservedCapital, canonical.ProfitAfterOrphanGuard = reserved.String(), guarded.String()
	switch {
	case available.LessThan(reserved):
		canonical.Reason = "insufficient_capital"
	case !afterCost.GreaterThan(minimum):
		canonical.Reason = "nonpositive_complete_set_profit"
	case !guarded.GreaterThan(minimum):
		canonical.Reason = "orphan_guard_failure"
	default:
		canonical.State, canonical.Reason = "qualified", ""
	}
	return finish(canonical)
}

func normalizeOutcomes(values []uuid.UUID) ([]string, error) {
	if len(values) < 2 || len(values) > 12 {
		return nil, fmt.Errorf("complete set requires between two and twelve outcomes")
	}
	result := make([]string, len(values))
	seen := map[uuid.UUID]bool{}
	for i, value := range values {
		if value == uuid.Nil || seen[value] {
			return nil, fmt.Errorf("complete set outcome identity is invalid")
		}
		seen[value], result[i] = true, value.String()
	}
	sort.Strings(result)
	return result, nil
}

func normalizeBindings(values []LegBinding) ([]LegBinding, error) {
	result := append([]LegBinding(nil), values...)
	sort.Slice(result, func(i, j int) bool { return result[i].OutcomeID.String() < result[j].OutcomeID.String() })
	seenOutcome, seenReplay := map[uuid.UUID]bool{}, map[int]bool{}
	for _, value := range result {
		if value.OutcomeID == uuid.Nil || value.EntrySequence < 0 || value.UnwindSequence < 0 || value.EntrySequence == value.UnwindSequence || seenOutcome[value.OutcomeID] || seenReplay[value.EntrySequence] || seenReplay[value.UnwindSequence] {
			return nil, fmt.Errorf("complete set leg binding is invalid")
		}
		seenOutcome[value.OutcomeID], seenReplay[value.EntrySequence], seenReplay[value.UnwindSequence] = true, true, true
	}
	return result, nil
}

func bindingsCoverOutcomes(bindings []LegBinding, outcomes []string) bool {
	if len(bindings) != len(outcomes) {
		return false
	}
	for i := range bindings {
		if bindings[i].OutcomeID.String() != outcomes[i] {
			return false
		}
	}
	return true
}

func enumerateScenarios(legs []legCanonical) []scenarioCanonical {
	result := make([]scenarioCanonical, 0, (1<<len(legs))-2)
	for mask := 1; mask < (1<<len(legs))-1; mask++ {
		scenario := scenarioCanonical{Sequence: len(result), Legs: []scenarioLegCanonical{}}
		entryCost, unwindProceeds := decimal.Zero, decimal.Zero
		keys := make([]string, 0, len(legs))
		for i, leg := range legs {
			if mask&(1<<i) == 0 {
				continue
			}
			entry := decimal.RequireFromString(leg.EntryCost)
			unwind := decimal.RequireFromString(leg.UnwindProceeds)
			loss := decimal.Max(entry.Sub(unwind), decimal.Zero)
			scenario.Legs = append(scenario.Legs, scenarioLegCanonical{len(scenario.Legs), leg.OutcomeID, entry.String(), unwind.String(), loss.String()})
			entryCost, unwindProceeds = entryCost.Add(entry), unwindProceeds.Add(unwind)
			keys = append(keys, leg.OutcomeID)
		}
		scenario.Key = strings.Join(keys, "+")
		scenario.EntryCost, scenario.UnwindProceeds = entryCost.String(), unwindProceeds.String()
		scenario.Loss = decimal.Max(entryCost.Sub(unwindProceeds), decimal.Zero).String()
		result = append(result, scenario)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	for i := range result {
		result[i].Sequence = i
	}
	return result
}

func finish(canonical candidateCanonical) (*Candidate, error) {
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal complete set: %w", err)
	}
	digest := hash(encoded)
	return &Candidate{canonical, encoded, digest, economicid.DeterministicUUID("complete-set-arbitrage", SchemaV1+"@sha256:"+digest)}, nil
}

func FromCanonical(id uuid.UUID, digest string, raw []byte, recorder *predictionreplay.Recorder) (*Candidate, error) {
	var canonical candidateCanonical
	if id == uuid.Nil || recorder == nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil || canonical.Schema != SchemaV1 || canonical.RecorderID != recorder.ID().String() || canonical.RecorderSHA256 != recorder.Digest() {
		return nil, fmt.Errorf("complete set envelope is invalid")
	}
	input := Input{Recorder: recorder, MarketID: canonical.MarketID, SetQuantity: canonical.SetQuantity, PayoutPerSet: canonical.PayoutPerSet, AvailableCapital: canonical.AvailableCapital, MinimumProfit: canonical.MinimumProfit}
	for _, outcome := range canonical.Outcomes {
		parsed, err := uuid.Parse(outcome)
		if err != nil {
			return nil, fmt.Errorf("complete set canonical outcome is invalid")
		}
		input.Outcomes = append(input.Outcomes, parsed)
	}
	for _, binding := range canonical.Bindings {
		parsed, err := uuid.Parse(binding.OutcomeID)
		if err != nil {
			return nil, fmt.Errorf("complete set canonical leg is invalid")
		}
		input.Legs = append(input.Legs, LegBinding{parsed, binding.EntrySequence, binding.UnwindSequence})
	}
	rebuilt, err := NewCandidate(input)
	if err != nil || rebuilt.ID() != id || rebuilt.Digest() != digest || !bytes.Equal(rebuilt.CanonicalBytes(), raw) {
		return nil, fmt.Errorf("complete set canonical graph does not reconstruct")
	}
	return rebuilt, nil
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

func hash(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
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
func (c *Candidate) Reason() string {
	if c == nil {
		return ""
	}
	return c.canonical.Reason
}

func (c *Candidate) LegCount() int {
	if c == nil {
		return 0
	}
	return len(c.canonical.Legs)
}

func (c *Candidate) ScenarioCount() int {
	if c == nil {
		return 0
	}
	return len(c.canonical.Scenarios)
}
