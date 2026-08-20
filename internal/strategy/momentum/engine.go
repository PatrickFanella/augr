package momentum

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
	Rank struct {
		InstrumentID   string `json:"instrument_id"`
		Eligible       bool   `json:"eligible"`
		Reason         string `json:"reason"`
		Momentum       string `json:"momentum"`
		Volatility     string `json:"volatility"`
		Rank           int    `json:"rank"`
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
		Regime               string    `json:"regime"`
		DesiredTurnover      string    `json:"desired_turnover"`
		AppliedTurnover      string    `json:"applied_turnover"`
		TurnoverScale        string    `json:"turnover_scale"`
		RemainingTargetDrift string    `json:"remaining_target_drift"`
		Cost                 string    `json:"cost"`
		Cash                 string    `json:"cash"`
		Equity               string    `json:"equity"`
		Ranks                []Rank    `json:"ranks"`
		Trades               []Trade   `json:"trades"`
		Holdings             []Holding `json:"holdings"`
	}
	regimeResult struct {
		Regime           string `json:"regime"`
		ObservationCount int    `json:"observation_count"`
		StartEquity      string `json:"start_equity"`
		EndEquity        string `json:"end_equity"`
		Return           string `json:"return"`
	}
	reportCanonical struct {
		Schema               string         `json:"schema"`
		State                string         `json:"state"`
		PolicyID             string         `json:"policy_id"`
		PolicySHA256         string         `json:"policy_sha256"`
		ScenarioID           string         `json:"scenario_id"`
		ScenarioSHA256       string         `json:"scenario_sha256"`
		InitialCapital       string         `json:"initial_capital"`
		EvaluationStart      string         `json:"evaluation_start"`
		EvaluationEnd        string         `json:"evaluation_end"`
		Rebalances           []Rebalance    `json:"rebalances"`
		Regimes              []regimeResult `json:"regimes"`
		EndingCash           string         `json:"ending_cash"`
		EndingEquity         string         `json:"ending_equity"`
		CumulativeTurnover   string         `json:"cumulative_turnover"`
		TotalCost            string         `json:"total_cost"`
		AfterCostTotalReturn string         `json:"after_cost_total_return"`
	}
	Report struct {
		canonical reportCanonical
		bytes     json.RawMessage
		digest    string
		id        uuid.UUID
	}
)

type (
	engineState struct {
		cash                          decimal.Decimal
		quantities                    map[string]decimal.Decimal
		cumulativeTurnover, totalCost decimal.Decimal
	}
	rankedMember struct {
		member               memberCanonical
		momentum, volatility decimal.Decimal
		eligible             bool
		reason               string
		rank                 int
		target               decimal.Decimal
	}
)

func NewReport(policy *Policy, scenario *Scenario) (*Report, error) {
	if policy == nil || scenario == nil || scenario.PolicyID() != policy.ID() || scenario.PolicyDigest() != policy.Digest() {
		return nil, fmt.Errorf("momentum report parents do not match")
	}
	state := engineState{cash: decimal.RequireFromString(scenario.canonical.InitialCapital), quantities: map[string]decimal.Decimal{}}
	rebalances := make([]Rebalance, len(scenario.canonical.Rebalances))
	for i, event := range scenario.canonical.Rebalances {
		value, err := applyRebalance(policy, &state, event)
		if err != nil {
			return nil, fmt.Errorf("momentum rebalance %d: %w", i, err)
		}
		rebalances[i] = value
	}
	q := func(v decimal.Decimal) string {
		return v.Round(int32(policy.DecimalScale())).StringFixed(int32(policy.DecimalScale()))
	}
	ending := decimal.RequireFromString(rebalances[len(rebalances)-1].Equity)
	initial := decimal.RequireFromString(scenario.canonical.InitialCapital)
	canonical := reportCanonical{Schema: ReportSchemaV1, State: "completed", PolicyID: policy.ID().String(), PolicySHA256: policy.Digest(), ScenarioID: scenario.ID().String(), ScenarioSHA256: scenario.Digest(), InitialCapital: scenario.canonical.InitialCapital, EvaluationStart: scenario.canonical.EvaluationStart, EvaluationEnd: scenario.canonical.EvaluationEnd, Rebalances: rebalances, Regimes: regimeResults(policy, rebalances), EndingCash: q(state.cash), EndingEquity: q(ending), CumulativeTurnover: q(state.cumulativeTurnover), TotalCost: q(state.totalCost), AfterCostTotalReturn: q(ending.Div(initial).Sub(decimal.NewFromInt(1)))}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Report{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("momentum-quality-report", ReportSchemaV1+"@sha256:"+digest)}, nil
}

func applyRebalance(policy *Policy, state *engineState, event rebalanceCanonical) (Rebalance, error) {
	q := func(v decimal.Decimal) string {
		return v.Round(int32(policy.DecimalScale())).StringFixed(int32(policy.DecimalScale()))
	}
	ranked := make([]rankedMember, len(event.Members))
	marks := map[string]decimal.Decimal{}
	members := map[string]memberCanonical{}
	equity := state.cash
	for i, m := range event.Members {
		lookback, skip := decimal.RequireFromString(m.LookbackPrice), decimal.RequireFromString(m.SkipPrice)
		momentum := skip.Div(lookback).Sub(decimal.NewFromInt(1))
		vol := decimal.RequireFromString(m.Volatility)
		eligible, reason := true, "eligible"
		switch {
		case m.HistoryDays < policy.canonical.MinimumHistoryDays:
			eligible, reason = false, "insufficient_history"
		case decimal.RequireFromString(m.ROIC).LessThan(decimal.RequireFromString(policy.canonical.MinimumROIC)) || decimal.RequireFromString(m.DebtToAssets).GreaterThan(decimal.RequireFromString(policy.canonical.MaximumDebtToAssets)) || policy.canonical.RequirePositiveFreeCash && !decimal.RequireFromString(m.FreeCashFlow).IsPositive():
			eligible, reason = false, "quality_failed"
		case vol.GreaterThan(decimal.RequireFromString(policy.canonical.MaximumVolatility)):
			eligible, reason = false, "volatility_failed"
		case parseTime(event.OccurredAt).Sub(parseTime(m.AvailableAt)).Seconds() > float64(policy.canonical.MaximumEvidenceAgeSeconds) || parseTime(event.OccurredAt).Sub(parseTime(m.MembershipAvailableAt)).Seconds() > float64(policy.canonical.MaximumEvidenceAgeSeconds):
			eligible, reason = false, "evidence_stale"
		}
		ranked[i] = rankedMember{member: m, momentum: momentum, volatility: vol, eligible: eligible, reason: reason}
		marks[m.InstrumentID] = decimal.RequireFromString(m.Bid)
		members[m.InstrumentID] = m
		if quantity := state.quantities[m.InstrumentID]; quantity.IsPositive() {
			equity = equity.Add(quantity.Mul(marks[m.InstrumentID]))
		}
	}
	for instrumentID, quantity := range state.quantities {
		if _, present := members[instrumentID]; !present && quantity.IsPositive() {
			return Rebalance{}, fmt.Errorf("held instrument is absent from point-in-time universe")
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].eligible != ranked[j].eligible {
			return ranked[i].eligible
		}
		if !ranked[i].momentum.Equal(ranked[j].momentum) {
			return ranked[i].momentum.GreaterThan(ranked[j].momentum)
		}
		if !ranked[i].volatility.Equal(ranked[j].volatility) {
			return ranked[i].volatility.LessThan(ranked[j].volatility)
		}
		return ranked[i].member.InstrumentID < ranked[j].member.InstrumentID
	})
	selected := 0
	for i := range ranked {
		if ranked[i].eligible && selected < policy.canonical.PortfolioSize {
			selected++
			ranked[i].rank = selected
		}
	}
	if selected > 0 {
		weight := decimal.NewFromInt(1).Div(decimal.NewFromInt(int64(selected)))
		for i := range ranked {
			if ranked[i].rank > 0 {
				ranked[i].target = weight
			}
		}
	}
	desiredNotional := decimal.Zero
	currentValues := map[string]decimal.Decimal{}
	targets := map[string]decimal.Decimal{}
	for _, item := range ranked {
		current := state.quantities[item.member.InstrumentID].Mul(decimal.RequireFromString(item.member.Bid))
		target := equity.Mul(item.target)
		currentValues[item.member.InstrumentID] = current
		targets[item.member.InstrumentID] = target
		desiredNotional = desiredNotional.Add(target.Sub(current).Abs())
	}
	desiredTurnover := desiredNotional.Div(equity).Div(decimal.NewFromInt(2))
	scale := decimal.NewFromInt(1)
	turnoverCap := decimal.RequireFromString(policy.canonical.MaximumRebalanceTurnover)
	if desiredTurnover.GreaterThan(turnoverCap) {
		scale = turnoverCap.Div(desiredTurnover)
	}
	costRate := decimal.RequireFromString(policy.canonical.CostBPS).Div(decimal.NewFromInt(10000))
	trades := []Trade{}
	actualNotional := decimal.Zero
	tradeSide := func(side string) error {
		for _, item := range ranked {
			m := item.member
			delta := targets[m.InstrumentID].Sub(currentValues[m.InstrumentID]).Mul(scale)
			if side == "sell" && !delta.IsNegative() || side == "buy" && !delta.IsPositive() {
				continue
			}
			price := decimal.RequireFromString(m.Ask)
			if side == "sell" {
				price = decimal.RequireFromString(m.Bid)
			}
			notional := delta.Abs()
			quantity := notional.Div(price)
			lotSize := decimal.RequireFromString(m.LotSize)
			quantity = quantity.Div(lotSize).Floor().Mul(lotSize)
			notional = quantity.Mul(price)
			cost := notional.Mul(costRate)
			if !quantity.IsPositive() {
				continue
			}
			if side == "sell" {
				if quantity.GreaterThan(state.quantities[m.InstrumentID]) {
					quantity = state.quantities[m.InstrumentID]
					notional = quantity.Mul(price)
					cost = notional.Mul(costRate)
				}
				state.quantities[m.InstrumentID] = state.quantities[m.InstrumentID].Sub(quantity)
				state.cash = state.cash.Add(notional).Sub(cost)
			} else {
				required := notional.Add(cost)
				if required.GreaterThan(state.cash) {
					notional = state.cash.Div(decimal.NewFromInt(1).Add(costRate))
					quantity = notional.Div(price)
					quantity = quantity.Div(lotSize).Floor().Mul(lotSize)
					notional = quantity.Mul(price)
					cost = notional.Mul(costRate)
				}
				if !quantity.IsPositive() {
					continue
				}
				state.quantities[m.InstrumentID] = state.quantities[m.InstrumentID].Add(quantity)
				state.cash = state.cash.Sub(notional).Sub(cost)
			}
			actualNotional = actualNotional.Add(notional)
			state.totalCost = state.totalCost.Add(cost)
			trades = append(trades, Trade{InstrumentID: m.InstrumentID, VenueContractID: m.VenueContractID, Side: side, Quantity: q(quantity), Price: q(price), Notional: q(notional), Cost: q(cost), EvidenceSHA256: m.EvidenceSHA256})
		}
		return nil
	}
	_ = tradeSide("sell")
	_ = tradeSide("buy")
	applied := actualNotional.Div(equity).Div(decimal.NewFromInt(2))
	state.cumulativeTurnover = state.cumulativeTurnover.Add(applied)
	endingEquity := state.cash
	holdings := []Holding{}
	for _, item := range ranked {
		quantity := state.quantities[item.member.InstrumentID]
		if !quantity.IsPositive() {
			continue
		}
		mark := decimal.RequireFromString(item.member.Bid)
		value := quantity.Mul(mark)
		endingEquity = endingEquity.Add(value)
		holdings = append(holdings, Holding{InstrumentID: item.member.InstrumentID, Quantity: q(quantity), Mark: q(mark), MarketValue: q(value)})
	}
	for i := range holdings {
		holdings[i].Weight = q(decimal.RequireFromString(holdings[i].MarketValue).Div(endingEquity))
	}
	ranks := make([]Rank, len(ranked))
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].member.InstrumentID < ranked[j].member.InstrumentID })
	for i, item := range ranked {
		ranks[i] = Rank{InstrumentID: item.member.InstrumentID, Eligible: item.eligible, Reason: item.reason, Momentum: q(item.momentum), Volatility: q(item.volatility), Rank: item.rank, TargetWeight: q(item.target), EvidenceSHA256: item.member.EvidenceSHA256}
	}
	sort.Slice(trades, func(i, j int) bool {
		if trades[i].Side != trades[j].Side {
			return trades[i].Side == "sell"
		}
		return trades[i].InstrumentID < trades[j].InstrumentID
	})
	return Rebalance{Sequence: event.Sequence, OccurredAt: event.OccurredAt, Regime: regime(policy, event), DesiredTurnover: q(desiredTurnover), AppliedTurnover: q(applied), TurnoverScale: q(scale), RemainingTargetDrift: q(desiredTurnover.Sub(applied).Mul(decimal.NewFromInt(2))), Cost: q(sumTradeCost(trades)), Cash: q(state.cash), Equity: q(endingEquity), Ranks: ranks, Trades: trades, Holdings: holdings}, nil
}

func regime(policy *Policy, event rebalanceCanonical) string {
	trend := decimal.RequireFromString(event.BenchmarkTrend)
	threshold := decimal.RequireFromString(policy.canonical.BullBearTrendThreshold)
	vol := decimal.RequireFromString(event.BenchmarkVolatility)
	if trend.GreaterThanOrEqual(threshold) && vol.LessThanOrEqual(decimal.RequireFromString(policy.canonical.MaximumBullVolatility)) {
		return "bull"
	}
	if trend.LessThanOrEqual(threshold.Neg()) {
		return "bear"
	}
	return "sideways"
}

func sumTradeCost(values []Trade) decimal.Decimal {
	total := decimal.Zero
	for _, v := range values {
		total = total.Add(decimal.RequireFromString(v.Cost))
	}
	return total
}

func regimeResults(policy *Policy, rebalances []Rebalance) []regimeResult {
	q := func(v decimal.Decimal) string {
		return v.Round(int32(policy.DecimalScale())).StringFixed(int32(policy.DecimalScale()))
	}
	type agg struct {
		count      int
		start, end decimal.Decimal
	}
	values := map[string]agg{}
	for _, r := range rebalances {
		equity := decimal.RequireFromString(r.Equity)
		a := values[r.Regime]
		if a.count == 0 {
			a.start = equity
		}
		a.count++
		a.end = equity
		values[r.Regime] = a
	}
	result := []regimeResult{}
	for _, name := range []string{"bull", "bear", "sideways"} {
		if a := values[name]; a.count > 0 {
			result = append(result, regimeResult{Regime: name, ObservationCount: a.count, StartEquity: q(a.start), EndEquity: q(a.end), Return: q(a.end.Div(a.start).Sub(decimal.NewFromInt(1)))})
		}
	}
	return result
}

func ReportFromCanonical(id uuid.UUID, digest string, raw []byte, policy *Policy, scenario *Scenario) (*Report, error) {
	var canonical reportCanonical
	if id == uuid.Nil || policy == nil || scenario == nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil {
		return nil, fmt.Errorf("momentum report envelope is invalid")
	}
	value, err := NewReport(policy, scenario)
	if err != nil || canonical.Schema != ReportSchemaV1 || canonical.State != "completed" || value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("momentum report identity does not reconstruct")
	}
	return value, nil
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

func (r *Report) CumulativeTurnover() string {
	if r == nil {
		return ""
	}
	return r.canonical.CumulativeTurnover
}
