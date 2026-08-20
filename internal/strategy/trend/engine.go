package trend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

type (
	Signal struct {
		InstrumentID   string `json:"instrument_id"`
		HorizonSigns   []int  `json:"horizon_signs"`
		Score          string `json:"score"`
		Long           bool   `json:"long"`
		RawWeight      string `json:"raw_weight"`
		TargetWeight   string `json:"target_weight"`
		EvidenceSHA256 string `json:"evidence_sha256"`
	}
	Trade struct {
		InstrumentID    string `json:"instrument_id"`
		VenueContractID string `json:"venue_contract_id"`
		Side            string `json:"side"`
		Quantity        string `json:"quantity"`
		Price           string `json:"price"`
		Notional        string `json:"notional"`
		Cost            string `json:"cost"`
		EvidenceSHA256  string `json:"evidence_sha256"`
	}
	Holding struct {
		InstrumentID string `json:"instrument_id"`
		Quantity     string `json:"quantity"`
		Mark         string `json:"mark"`
		MarketValue  string `json:"market_value"`
		Weight       string `json:"weight"`
	}
	Rebalance struct {
		Sequence             int       `json:"sequence"`
		OccurredAt           string    `json:"occurred_at"`
		DesiredTurnover      string    `json:"desired_turnover"`
		AppliedTurnover      string    `json:"applied_turnover"`
		TurnoverScale        string    `json:"turnover_scale"`
		RemainingTargetDrift string    `json:"remaining_target_drift"`
		Cost                 string    `json:"cost"`
		Cash                 string    `json:"cash"`
		Equity               string    `json:"equity"`
		GrossTargetWeight    string    `json:"gross_target_weight"`
		Signals              []Signal  `json:"signals"`
		Trades               []Trade   `json:"trades"`
		Holdings             []Holding `json:"holdings"`
	}
	reportCanonical struct {
		Schema               string      `json:"schema"`
		State                string      `json:"state"`
		PolicyID             string      `json:"policy_id"`
		PolicySHA256         string      `json:"policy_sha256"`
		ScenarioID           string      `json:"scenario_id"`
		ScenarioSHA256       string      `json:"scenario_sha256"`
		InitialCapital       string      `json:"initial_capital"`
		EvaluationStart      string      `json:"evaluation_start"`
		EvaluationEnd        string      `json:"evaluation_end"`
		Rebalances           []Rebalance `json:"rebalances"`
		EndingCash           string      `json:"ending_cash"`
		EndingEquity         string      `json:"ending_equity"`
		CumulativeTurnover   string      `json:"cumulative_turnover"`
		TotalCost            string      `json:"total_cost"`
		AfterCostTotalReturn string      `json:"after_cost_total_return"`
	}
	Report struct {
		canonical reportCanonical
		bytes     json.RawMessage
		digest    string
		id        uuid.UUID
	}
	engineState struct {
		cash                          decimal.Decimal
		quantities                    map[string]decimal.Decimal
		cumulativeTurnover, totalCost decimal.Decimal
	}
	calculated struct {
		member             memberCanonical
		signs              []int
		score, raw, target decimal.Decimal
		long               bool
	}
)

func NewReport(policy *Policy, scenario *Scenario) (*Report, error) {
	if policy == nil || scenario == nil || scenario.PolicyID() != policy.ID() || scenario.PolicyDigest() != policy.Digest() {
		return nil, fmt.Errorf("trend report parents do not match")
	}
	state := engineState{decimal.RequireFromString(scenario.canonical.InitialCapital), map[string]decimal.Decimal{}, decimal.Zero, decimal.Zero}
	rebalances := make([]Rebalance, len(scenario.canonical.Rebalances))
	for i, event := range scenario.canonical.Rebalances {
		value, err := applyRebalance(policy, &state, event)
		if err != nil {
			return nil, fmt.Errorf("trend rebalance %d: %w", i, err)
		}
		rebalances[i] = value
	}
	q := quantizer(policy)
	ending := decimal.RequireFromString(rebalances[len(rebalances)-1].Equity)
	initial := decimal.RequireFromString(scenario.canonical.InitialCapital)
	c := reportCanonical{ReportSchemaV1, "completed", policy.ID().String(), policy.Digest(), scenario.ID().String(), scenario.Digest(), scenario.canonical.InitialCapital, scenario.canonical.EvaluationStart, scenario.canonical.EvaluationEnd, rebalances, q(state.cash), q(ending), q(state.cumulativeTurnover), q(state.totalCost), q(ending.Div(initial).Sub(decimal.NewFromInt(1)))}
	encoded, _ := json.Marshal(c)
	digest := hash(encoded)
	return &Report{c, encoded, digest, economicid.DeterministicUUID("etf-time-series-trend-report", ReportSchemaV1+"@sha256:"+digest)}, nil
}

func applyRebalance(policy *Policy, state *engineState, event rebalanceCanonical) (Rebalance, error) {
	q := quantizer(policy)
	items := make([]calculated, len(event.Members))
	equity := state.cash
	members := map[string]memberCanonical{}
	for i, m := range event.Members {
		members[m.InstrumentID] = m
		if qty := state.quantities[m.InstrumentID]; qty.IsPositive() {
			equity = equity.Add(qty.Mul(decimal.RequireFromString(m.Bid)))
		}
		score := decimal.Zero
		signs := make([]int, len(m.HorizonPrices))
		for j, anchor := range m.HorizonPrices {
			returnValue := decimal.RequireFromString(m.CurrentPrice).Div(decimal.RequireFromString(anchor)).Sub(decimal.NewFromInt(1))
			sign := 0
			if returnValue.IsPositive() {
				sign = 1
			} else if returnValue.IsNegative() {
				sign = -1
			}
			signs[j] = sign
			score = score.Add(decimal.NewFromInt(int64(sign)).Mul(decimal.RequireFromString(policy.canonical.Horizons[j].Weight)))
		}
		long := score.GreaterThan(decimal.RequireFromString(policy.canonical.SignalThreshold))
		raw, target := decimal.Zero, decimal.Zero
		if long {
			raw = decimal.RequireFromString(policy.canonical.TargetVolatility).Div(decimal.RequireFromString(m.RealizedVolatility))
			target = decimal.Min(raw, decimal.RequireFromString(policy.canonical.MaximumInstrumentWeight))
		}
		items[i] = calculated{m, signs, score, raw, target, long}
	}
	for id, qty := range state.quantities {
		if _, ok := members[id]; !ok && qty.IsPositive() {
			return Rebalance{}, fmt.Errorf("held ETF is absent from point-in-time universe")
		}
	}
	gross := decimal.Zero
	for _, item := range items {
		gross = gross.Add(item.target)
	}
	grossCap := decimal.RequireFromString(policy.canonical.MaximumGrossWeight)
	if gross.GreaterThan(grossCap) {
		scale := grossCap.Div(gross)
		for i := range items {
			items[i].target = items[i].target.Mul(scale)
		}
		gross = grossCap
	}
	current, targets := map[string]decimal.Decimal{}, map[string]decimal.Decimal{}
	desiredNotional := decimal.Zero
	for _, item := range items {
		value := state.quantities[item.member.InstrumentID].Mul(decimal.RequireFromString(item.member.Bid))
		target := equity.Mul(item.target)
		current[item.member.InstrumentID] = value
		targets[item.member.InstrumentID] = target
		desiredNotional = desiredNotional.Add(target.Sub(value).Abs())
	}
	desired := desiredNotional.Div(equity).Div(decimal.NewFromInt(2))
	turnoverScale := decimal.NewFromInt(1)
	turnoverCap := decimal.RequireFromString(policy.canonical.MaximumRebalanceTurnover)
	if desired.GreaterThan(turnoverCap) {
		turnoverScale = turnoverCap.Div(desired)
	}
	costRate := decimal.RequireFromString(policy.canonical.CostBPS).Div(decimal.NewFromInt(10000))
	trades := []Trade{}
	actual := decimal.Zero
	for _, side := range []string{"sell", "buy"} {
		for _, item := range items {
			m := item.member
			delta := targets[m.InstrumentID].Sub(current[m.InstrumentID]).Mul(turnoverScale)
			if side == "sell" && !delta.IsNegative() || side == "buy" && !delta.IsPositive() {
				continue
			}
			price := decimal.RequireFromString(m.Ask)
			if side == "sell" {
				price = decimal.RequireFromString(m.Bid)
			}
			lot := decimal.RequireFromString(m.LotSize)
			quantity := delta.Abs().Div(price).Div(lot).Floor().Mul(lot)
			if !quantity.IsPositive() {
				continue
			}
			notional := quantity.Mul(price)
			cost := notional.Mul(costRate)
			if side == "sell" {
				if quantity.GreaterThan(state.quantities[m.InstrumentID]) {
					quantity = state.quantities[m.InstrumentID]
					notional = quantity.Mul(price)
					cost = notional.Mul(costRate)
				}
				state.quantities[m.InstrumentID] = state.quantities[m.InstrumentID].Sub(quantity)
				state.cash = state.cash.Add(notional).Sub(cost)
			} else {
				if notional.Add(cost).GreaterThan(state.cash) {
					quantity = state.cash.Div(decimal.NewFromInt(1).Add(costRate)).Div(price).Div(lot).Floor().Mul(lot)
					notional = quantity.Mul(price)
					cost = notional.Mul(costRate)
				}
				if !quantity.IsPositive() {
					continue
				}
				state.quantities[m.InstrumentID] = state.quantities[m.InstrumentID].Add(quantity)
				state.cash = state.cash.Sub(notional).Sub(cost)
			}
			actual = actual.Add(notional)
			state.totalCost = state.totalCost.Add(cost)
			trades = append(trades, Trade{m.InstrumentID, m.VenueContractID, side, q(quantity), q(price), q(notional), q(cost), m.EvidenceSHA256})
		}
	}
	applied := actual.Div(equity).Div(decimal.NewFromInt(2))
	state.cumulativeTurnover = state.cumulativeTurnover.Add(applied)
	ending := state.cash
	holdings := []Holding{}
	for _, item := range items {
		qty := state.quantities[item.member.InstrumentID]
		if !qty.IsPositive() {
			continue
		}
		mark := decimal.RequireFromString(item.member.Bid)
		value := qty.Mul(mark)
		ending = ending.Add(value)
		holdings = append(holdings, Holding{item.member.InstrumentID, q(qty), q(mark), q(value), ""})
	}
	for i := range holdings {
		holdings[i].Weight = q(decimal.RequireFromString(holdings[i].MarketValue).Div(ending))
	}
	signals := make([]Signal, len(items))
	sort.Slice(items, func(i, j int) bool { return items[i].member.InstrumentID < items[j].member.InstrumentID })
	for i, item := range items {
		signals[i] = Signal{item.member.InstrumentID, item.signs, q(item.score), item.long, q(item.raw), q(item.target), item.member.EvidenceSHA256}
	}
	sort.Slice(trades, func(i, j int) bool {
		if trades[i].Side != trades[j].Side {
			return trades[i].Side == "sell"
		}
		return trades[i].InstrumentID < trades[j].InstrumentID
	})
	return Rebalance{event.Sequence, event.OccurredAt, q(desired), q(applied), q(turnoverScale), q(desired.Sub(applied).Mul(decimal.NewFromInt(2))), q(sumCosts(trades)), q(state.cash), q(ending), q(gross), signals, trades, holdings}, nil
}

func quantizer(policy *Policy) func(decimal.Decimal) string {
	return func(v decimal.Decimal) string {
		return v.Round(int32(policy.DecimalScale())).StringFixed(int32(policy.DecimalScale()))
	}
}

func sumCosts(values []Trade) decimal.Decimal {
	v := decimal.Zero
	for _, trade := range values {
		v = v.Add(decimal.RequireFromString(trade.Cost))
	}
	return v
}

func ReportFromCanonical(id uuid.UUID, digest string, raw []byte, policy *Policy, scenario *Scenario) (*Report, error) {
	var c reportCanonical
	if id == uuid.Nil || policy == nil || scenario == nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &c) != nil {
		return nil, fmt.Errorf("trend report envelope is invalid")
	}
	v, err := NewReport(policy, scenario)
	if err != nil || c.Schema != ReportSchemaV1 || c.State != "completed" || v.ID() != id || v.Digest() != digest || !bytes.Equal(v.bytes, raw) {
		return nil, fmt.Errorf("trend report identity does not reconstruct")
	}
	return v, nil
}

func (r *Report) ID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return r.id
}

func (r *Report) Digest() string {
	if r == nil {
		return ""
	}
	return r.digest
}

func (r *Report) CanonicalBytes() json.RawMessage {
	if r == nil {
		return nil
	}
	return append(json.RawMessage(nil), r.bytes...)
}

func (r *Report) Rebalances() []Rebalance {
	if r == nil {
		return nil
	}
	return append([]Rebalance(nil), r.canonical.Rebalances...)
}

func (r *Report) AfterCostTotalReturn() string {
	if r == nil {
		return ""
	}
	return r.canonical.AfterCostTotalReturn
}
