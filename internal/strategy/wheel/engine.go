package wheel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

type (
	optionPosition struct {
		InstrumentID       uuid.UUID
		OptionType, Strike string
		Expiry             time.Time
		Contracts          int
		MarkAsk            string
	}
	engineState struct {
		Cash, Shares, Collateral, UnderlyingMark, OptionLiability, Premiums, Dividends, Fees, CappedUpside decimal.Decimal
		QualityAt                                                                                          time.Time
		QualityEligible                                                                                    bool
		Option                                                                                             *optionPosition
	}
)

type (
	Effect struct {
		Kind           string `json:"kind"`
		InstrumentID   string `json:"instrument_id"`
		Quantity       string `json:"quantity"`
		Amount         string `json:"amount"`
		EvidenceID     string `json:"evidence_id"`
		EvidenceSHA256 string `json:"evidence_sha256"`
	}
	Transition struct {
		Sequence             int       `json:"sequence"`
		EventKind            EventKind `json:"event_kind"`
		OccurredAt           string    `json:"occurred_at"`
		Action               string    `json:"action"`
		Reason               string    `json:"reason"`
		SelectedInstrumentID string    `json:"selected_instrument_id"`
		Cash                 string    `json:"cash"`
		Shares               string    `json:"shares"`
		Collateral           string    `json:"collateral"`
		OptionLiability      string    `json:"option_liability"`
		NetLiquidation       string    `json:"net_liquidation"`
		Effects              []Effect  `json:"effects"`
	}
	reportCanonical struct {
		Schema                string       `json:"schema"`
		State                 string       `json:"state"`
		PolicyID              string       `json:"policy_id"`
		PolicySHA256          string       `json:"policy_sha256"`
		ScenarioID            string       `json:"scenario_id"`
		ScenarioSHA256        string       `json:"scenario_sha256"`
		UnderlyingID          string       `json:"underlying_id"`
		InitialCapital        string       `json:"initial_capital"`
		EvaluationStart       string       `json:"evaluation_start"`
		EvaluationEnd         string       `json:"evaluation_end"`
		Transitions           []Transition `json:"transitions"`
		EndingCash            string       `json:"ending_cash"`
		EndingShares          string       `json:"ending_shares"`
		EndingCollateral      string       `json:"ending_collateral"`
		EndingOptionLiability string       `json:"ending_option_liability"`
		EndingNetLiquidation  string       `json:"ending_net_liquidation"`
		PremiumIncome         string       `json:"premium_income"`
		DividendIncome        string       `json:"dividend_income"`
		TotalFees             string       `json:"total_fees"`
		CappedUpside          string       `json:"capped_upside"`
		AfterCostTotalReturn  string       `json:"after_cost_total_return"`
	}
)

type Report struct {
	canonical reportCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewReport(policy *Policy, scenario *Scenario) (*Report, error) {
	if policy == nil || scenario == nil || scenario.PolicyID() != policy.ID() || scenario.PolicyDigest() != policy.Digest() {
		return nil, fmt.Errorf("wheel report parents do not match")
	}
	state := engineState{Cash: decimal.RequireFromString(scenario.InitialCapital())}
	transitions := make([]Transition, len(scenario.canonical.Events))
	for i, event := range scenario.canonical.Events {
		transition, err := applyEvent(policy, scenario.UnderlyingID(), &state, event)
		if err != nil {
			return nil, fmt.Errorf("wheel transition %d: %w", i, err)
		}
		transitions[i] = transition
	}
	q := func(value decimal.Decimal) string {
		return value.Round(int32(policy.DecimalScale())).StringFixed(int32(policy.DecimalScale()))
	}
	net := netLiquidation(state)
	initial := decimal.RequireFromString(scenario.InitialCapital())
	canonical := reportCanonical{Schema: ReportSchemaV1, State: "completed", PolicyID: policy.ID().String(), PolicySHA256: policy.Digest(), ScenarioID: scenario.ID().String(), ScenarioSHA256: scenario.Digest(), UnderlyingID: scenario.UnderlyingID().String(), InitialCapital: scenario.InitialCapital(), EvaluationStart: formatTime(scenario.EvaluationStart()), EvaluationEnd: formatTime(scenario.EvaluationEnd()), Transitions: transitions, EndingCash: q(state.Cash), EndingShares: q(state.Shares), EndingCollateral: q(state.Collateral), EndingOptionLiability: q(state.OptionLiability), EndingNetLiquidation: q(net), PremiumIncome: q(state.Premiums), DividendIncome: q(state.Dividends), TotalFees: q(state.Fees), CappedUpside: q(state.CappedUpside), AfterCostTotalReturn: q(net.Div(initial).Sub(decimal.NewFromInt(1)))}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Report{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("quality-filtered-wheel-report", ReportSchemaV1+"@sha256:"+digest)}, nil
}

func applyEvent(policy *Policy, underlyingID uuid.UUID, state *engineState, event eventCanonical) (Transition, error) {
	state.UnderlyingMark = decimal.RequireFromString(event.UnderlyingMark)
	action, reason, selected := "noop", "no_state_change", uuid.Nil
	effects := []Effect{}
	effect := func(kind string, instrument uuid.UUID, quantity string, amount decimal.Decimal) {
		effects = append(effects, Effect{Kind: kind, InstrumentID: instrument.String(), Quantity: quantity, Amount: amount.String(), EvidenceID: event.EvidenceID, EvidenceSHA256: event.EvidenceSHA256})
	}
	switch event.Kind {
	case EventAssessQuality:
		quality := event.Quality
		state.QualityAt = parseTime(quality.AvailableAt)
		state.QualityEligible = decimal.RequireFromString(quality.ROIC).GreaterThanOrEqual(decimal.RequireFromString(policy.canonical.MinimumROIC)) && decimal.RequireFromString(quality.DebtToAssets).LessThanOrEqual(decimal.RequireFromString(policy.canonical.MaximumDebtToAssets)) && (!policy.canonical.RequirePositiveFreeCash || decimal.RequireFromString(quality.FreeCashFlow).IsPositive())
		action = "quality_assessed"
		if state.QualityEligible {
			reason = "quality_passed"
		} else {
			reason = "quality_failed"
		}
	case EventOpenPut:
		if state.Option != nil || !state.Shares.IsZero() {
			return Transition{}, fmt.Errorf("cash-secured put requires cash-only state")
		}
		if !state.QualityEligible || parseTime(event.OccurredAt).Sub(state.QualityAt) > time.Duration(policy.canonical.MaximumQualityAgeSeconds)*time.Second {
			action = "rejected"
			reason = "quality_missing_failed_or_stale"
			break
		}
		candidate, ok := selectCandidate(policy, event, "put")
		if !ok {
			action = "rejected"
			reason = "no_eligible_put"
			break
		}
		unitCollateral := decimal.RequireFromString(candidate.Strike).Mul(decimal.RequireFromString(policy.canonical.DeliverableQuantity))
		contracts := int(state.Cash.Div(unitCollateral).Floor().IntPart())
		if contracts > policy.canonical.MaximumContracts {
			contracts = policy.canonical.MaximumContracts
		}
		if contracts < 1 {
			action = "rejected"
			reason = "insufficient_cash_collateral"
			break
		}
		selected = uuid.MustParse(candidate.InstrumentID)
		premium := decimal.RequireFromString(candidate.Bid).Mul(decimal.RequireFromString(policy.canonical.DeliverableQuantity)).Mul(decimal.NewFromInt(int64(contracts)))
		fee := decimal.RequireFromString(policy.canonical.FeePerContract).Mul(decimal.NewFromInt(int64(contracts)))
		state.Cash = state.Cash.Add(premium).Sub(fee)
		state.Collateral = unitCollateral.Mul(decimal.NewFromInt(int64(contracts)))
		state.Premiums = state.Premiums.Add(premium)
		state.Fees = state.Fees.Add(fee)
		state.Option = &optionPosition{InstrumentID: selected, OptionType: "put", Strike: candidate.Strike, Expiry: parseTime(candidate.Expiry), Contracts: contracts, MarkAsk: candidate.Ask}
		state.OptionLiability = decimal.RequireFromString(candidate.Ask).Mul(decimal.RequireFromString(policy.canonical.DeliverableQuantity)).Mul(decimal.NewFromInt(int64(contracts)))
		action = "short_put_opened"
		reason = "selected_by_delta_dte_liquidity"
		effect("premium", selected, fmt.Sprint(contracts), premium)
		effect("fee", selected, fmt.Sprint(contracts), fee.Neg())
		effect("collateral_reserved", selected, fmt.Sprint(contracts), state.Collateral)
	case EventOpenCall:
		if state.Option != nil || !state.Shares.IsPositive() {
			return Transition{}, fmt.Errorf("covered call requires unencumbered shares")
		}
		candidate, ok := selectCandidate(policy, event, "call")
		if !ok {
			action = "rejected"
			reason = "no_eligible_call"
			break
		}
		deliverable := decimal.RequireFromString(policy.canonical.DeliverableQuantity)
		contracts := int(state.Shares.Div(deliverable).Floor().IntPart())
		if contracts > policy.canonical.MaximumContracts {
			contracts = policy.canonical.MaximumContracts
		}
		if contracts < 1 {
			return Transition{}, fmt.Errorf("covered call has no deliverable shares")
		}
		selected = uuid.MustParse(candidate.InstrumentID)
		premium := decimal.RequireFromString(candidate.Bid).Mul(deliverable).Mul(decimal.NewFromInt(int64(contracts)))
		fee := decimal.RequireFromString(policy.canonical.FeePerContract).Mul(decimal.NewFromInt(int64(contracts)))
		state.Cash = state.Cash.Add(premium).Sub(fee)
		state.Premiums = state.Premiums.Add(premium)
		state.Fees = state.Fees.Add(fee)
		state.Option = &optionPosition{InstrumentID: selected, OptionType: "call", Strike: candidate.Strike, Expiry: parseTime(candidate.Expiry), Contracts: contracts, MarkAsk: candidate.Ask}
		state.OptionLiability = decimal.RequireFromString(candidate.Ask).Mul(deliverable).Mul(decimal.NewFromInt(int64(contracts)))
		action = "covered_call_opened"
		reason = "selected_by_delta_dte_liquidity"
		effect("premium", selected, fmt.Sprint(contracts), premium)
		effect("fee", selected, fmt.Sprint(contracts), fee.Neg())
	case EventMark:
		if state.Option == nil {
			return Transition{}, fmt.Errorf("option mark requires open option")
		}
		state.Option.MarkAsk = *event.OptionMarkAsk
		state.OptionLiability = decimal.RequireFromString(*event.OptionMarkAsk).Mul(decimal.RequireFromString(policy.canonical.DeliverableQuantity)).Mul(decimal.NewFromInt(int64(state.Option.Contracts)))
		selected = state.Option.InstrumentID
		action = "option_marked"
		reason = "executable_ask_liability"
		effect("option_liability", selected, fmt.Sprint(state.Option.Contracts), state.OptionLiability.Neg())
	case EventCloseOption:
		if state.Option == nil {
			return Transition{}, fmt.Errorf("option close requires open option")
		}
		selected = state.Option.InstrumentID
		cost := decimal.RequireFromString(*event.OptionMarkAsk).Mul(decimal.RequireFromString(policy.canonical.DeliverableQuantity)).Mul(decimal.NewFromInt(int64(state.Option.Contracts)))
		fee := decimal.RequireFromString(policy.canonical.FeePerContract).Mul(decimal.NewFromInt(int64(state.Option.Contracts)))
		if state.Cash.LessThan(cost.Add(fee)) {
			return Transition{}, fmt.Errorf("option close exceeds cash")
		}
		state.Cash = state.Cash.Sub(cost).Sub(fee)
		state.Fees = state.Fees.Add(fee)
		state.Collateral = decimal.Zero
		effect("buy_to_close", selected, fmt.Sprint(state.Option.Contracts), cost.Neg())
		effect("fee", selected, fmt.Sprint(state.Option.Contracts), fee.Neg())
		state.Option = nil
		state.OptionLiability = decimal.Zero
		action = "short_option_closed"
		reason = "executable_ask_close"
	case EventAssignment:
		if state.Option == nil || state.Option.InstrumentID != uuid.MustParse(event.AssignmentOptionID) {
			return Transition{}, fmt.Errorf("assignment does not match open option")
		}
		selected = state.Option.InstrumentID
		var err error
		action, reason, err = settleOption(policy, underlyingID, state, event, true, &effects)
		if err != nil {
			return Transition{}, err
		}
	case EventExpiry:
		if state.Option == nil || parseTime(event.OccurredAt).Before(state.Option.Expiry) {
			return Transition{}, fmt.Errorf("expiry does not match open expired option")
		}
		selected = state.Option.InstrumentID
		var err error
		action, reason, err = settleOption(policy, underlyingID, state, event, false, &effects)
		if err != nil {
			return Transition{}, err
		}
	case EventDividend:
		if !state.Shares.IsPositive() {
			return Transition{}, fmt.Errorf("dividend has no entitled shares")
		}
		amount := decimal.RequireFromString(*event.DividendPerShare).Mul(state.Shares)
		state.Cash = state.Cash.Add(amount)
		state.Dividends = state.Dividends.Add(amount)
		action = "dividend_credited"
		reason = "shares_held_at_effective_at"
		effect("dividend", underlyingID, state.Shares.String(), amount)
	}
	q := func(value decimal.Decimal) string {
		return value.Round(int32(policy.DecimalScale())).StringFixed(int32(policy.DecimalScale()))
	}
	return Transition{Sequence: event.Sequence, EventKind: event.Kind, OccurredAt: event.OccurredAt, Action: action, Reason: reason, SelectedInstrumentID: selected.String(), Cash: q(state.Cash), Shares: q(state.Shares), Collateral: q(state.Collateral), OptionLiability: q(state.OptionLiability), NetLiquidation: q(netLiquidation(*state)), Effects: effects}, nil
}

func selectCandidate(policy *Policy, event eventCanonical, optionType string) (candidateCanonical, bool) {
	type ranked struct {
		value    candidateCanonical
		distance decimal.Decimal
		dte      int
	}
	values := []ranked{}
	decisionAt := parseTime(event.OccurredAt)
	for _, candidate := range event.Candidates {
		if candidate.OptionType != optionType {
			continue
		}
		dte := int(parseTime(candidate.Expiry).Sub(decisionAt) / (24 * time.Hour))
		if decisionAt.Sub(parseTime(candidate.AvailableAt)) > time.Duration(policy.canonical.MaximumMarketDataAgeSeconds)*time.Second {
			continue
		}
		delta := decimal.RequireFromString(candidate.Delta).Abs()
		minimum, target, maximum := policy.canonical.PutDeltaMinimum, policy.canonical.PutDeltaTarget, policy.canonical.PutDeltaMaximum
		if optionType == "call" {
			minimum, target, maximum = policy.canonical.CallDeltaMinimum, policy.canonical.CallDeltaTarget, policy.canonical.CallDeltaMaximum
		}
		if delta.LessThan(decimal.RequireFromString(minimum)) || delta.GreaterThan(decimal.RequireFromString(maximum)) || dte < policy.canonical.MinimumDTE || dte > policy.canonical.MaximumDTE || decimal.RequireFromString(candidate.OpenInterest).LessThan(decimal.RequireFromString(policy.canonical.MinimumOpenInterest)) || decimal.RequireFromString(candidate.Volume).LessThan(decimal.RequireFromString(policy.canonical.MinimumVolume)) {
			continue
		}
		values = append(values, ranked{candidate, delta.Sub(decimal.RequireFromString(target)).Abs(), dte})
	}
	sort.Slice(values, func(i, j int) bool {
		if !values[i].distance.Equal(values[j].distance) {
			return values[i].distance.LessThan(values[j].distance)
		}
		if values[i].dte != values[j].dte {
			return values[i].dte < values[j].dte
		}
		left, right := decimal.RequireFromString(values[i].value.Strike), decimal.RequireFromString(values[j].value.Strike)
		if !left.Equal(right) {
			return left.LessThan(right)
		}
		return values[i].value.InstrumentID < values[j].value.InstrumentID
	})
	if len(values) == 0 {
		return candidateCanonical{}, false
	}
	return values[0].value, true
}

func settleOption(policy *Policy, underlyingID uuid.UUID, state *engineState, event eventCanonical, sourced bool, effects *[]Effect) (string, string, error) {
	option := state.Option
	deliverable := decimal.RequireFromString(policy.canonical.DeliverableQuantity).Mul(decimal.NewFromInt(int64(option.Contracts)))
	strike := decimal.RequireFromString(option.Strike)
	itm := option.OptionType == "put" && state.UnderlyingMark.LessThan(strike) || option.OptionType == "call" && state.UnderlyingMark.GreaterThan(strike)
	assigned := sourced || itm
	appendEffect := func(kind string, instrument uuid.UUID, quantity string, amount decimal.Decimal) {
		*effects = append(*effects, Effect{Kind: kind, InstrumentID: instrument.String(), Quantity: quantity, Amount: amount.String(), EvidenceID: event.EvidenceID, EvidenceSHA256: event.EvidenceSHA256})
	}
	action, reason := "option_expired", "expired_out_of_money"
	if assigned && option.OptionType == "put" {
		cost := strike.Mul(deliverable)
		fee := decimal.RequireFromString(policy.canonical.FeePerShare).Mul(deliverable)
		if state.Cash.LessThan(cost.Add(fee)) {
			return "", "", fmt.Errorf("put assignment exceeds collateral and available cash")
		}
		state.Cash = state.Cash.Sub(cost).Sub(fee)
		state.Shares = state.Shares.Add(deliverable)
		state.Fees = state.Fees.Add(fee)
		appendEffect("put_assignment_purchase", underlyingID, deliverable.String(), cost.Neg())
		appendEffect("fee", underlyingID, deliverable.String(), fee.Neg())
		action = "put_assigned"
		reason = "sourced_or_automatic_itm_assignment"
	} else if assigned && option.OptionType == "call" {
		proceeds := strike.Mul(deliverable)
		fee := decimal.RequireFromString(policy.canonical.FeePerShare).Mul(deliverable)
		state.Cash = state.Cash.Add(proceeds).Sub(fee)
		state.Shares = state.Shares.Sub(deliverable)
		state.Fees = state.Fees.Add(fee)
		capped := state.UnderlyingMark.Sub(strike)
		if capped.IsPositive() {
			state.CappedUpside = state.CappedUpside.Add(capped.Mul(deliverable))
		}
		appendEffect("call_assignment_sale", underlyingID, deliverable.String(), proceeds)
		appendEffect("fee", underlyingID, deliverable.String(), fee.Neg())
		action = "shares_called_away"
		reason = "sourced_or_automatic_itm_assignment"
	}
	state.Collateral = decimal.Zero
	state.OptionLiability = decimal.Zero
	state.Option = nil
	return action, reason, nil
}

func netLiquidation(state engineState) decimal.Decimal {
	return state.Cash.Add(state.Shares.Mul(state.UnderlyingMark)).Sub(state.OptionLiability)
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

func (r *Report) PolicyID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return uuid.MustParse(r.canonical.PolicyID)
}

func (r *Report) ScenarioID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return uuid.MustParse(r.canonical.ScenarioID)
}

func (r *Report) AfterCostTotalReturn() string {
	if r == nil {
		return ""
	}
	return r.canonical.AfterCostTotalReturn
}

func (r *Report) CappedUpside() string {
	if r == nil {
		return ""
	}
	return r.canonical.CappedUpside
}

func (r *Report) EndingCash() string {
	if r == nil {
		return ""
	}
	return r.canonical.EndingCash
}

func (r *Report) EndingShares() string {
	if r == nil {
		return ""
	}
	return r.canonical.EndingShares
}

func (r *Report) Transitions() []Transition {
	if r == nil {
		return nil
	}
	return append([]Transition(nil), r.canonical.Transitions...)
}

func ReportFromCanonical(id uuid.UUID, digest string, raw []byte, policy *Policy, scenario *Scenario) (*Report, error) {
	var canonical reportCanonical
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil {
		return nil, fmt.Errorf("wheel report envelope is invalid")
	}
	value, err := NewReport(policy, scenario)
	if err != nil || canonical.Schema != ReportSchemaV1 || canonical.State != "completed" || value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("wheel report identity does not reconstruct")
	}
	return value, nil
}
