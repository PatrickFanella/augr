package definedrisk

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

type Fill struct {
	Sequence       int    `json:"sequence"`
	InstrumentID   string `json:"instrument_id"`
	Position       string `json:"position"`
	Action         string `json:"action"`
	Quantity       int    `json:"quantity"`
	Price          string `json:"price"`
	Fee            string `json:"fee"`
	EvidenceID     string `json:"evidence_id"`
	EvidenceSHA256 string `json:"evidence_sha256"`
}

type reportCanonical struct {
	Schema                   string        `json:"schema"`
	State                    string        `json:"state"`
	PolicyID                 string        `json:"policy_id"`
	PolicySHA256             string        `json:"policy_sha256"`
	ScenarioID               string        `json:"scenario_id"`
	ScenarioSHA256           string        `json:"scenario_sha256"`
	Strategy                 Strategy      `json:"strategy"`
	ExecutionMode            ExecutionMode `json:"execution_mode"`
	Outcome                  string        `json:"outcome"`
	Reason                   string        `json:"reason"`
	Contracts                int           `json:"contracts"`
	Width                    string        `json:"width"`
	NetPremiumPerContract    string        `json:"net_premium_per_contract"`
	MaximumLossPerContract   string        `json:"maximum_loss_per_contract"`
	MaximumRewardPerContract string        `json:"maximum_reward_per_contract"`
	OrphanReservePerContract string        `json:"orphan_reserve_per_contract"`
	ReservedCapital          string        `json:"reserved_capital"`
	EntryFees                string        `json:"entry_fees"`
	UnwindFees               string        `json:"unwind_fees"`
	OrphanLoss               string        `json:"orphan_loss"`
	ExpirationPayoff         string        `json:"expiration_payoff"`
	EndingCash               string        `json:"ending_cash"`
	AfterCostTotalReturn     string        `json:"after_cost_total_return"`
	Fills                    []Fill        `json:"fills"`
}

type Report struct {
	canonical reportCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewReport(policy *Policy, scenario *Scenario) (*Report, error) {
	if policy == nil || scenario == nil || scenario.canonical.PolicyID != policy.ID().String() || scenario.canonical.PolicySHA256 != policy.Digest() {
		return nil, fmt.Errorf("defined-risk report parents do not match")
	}
	low, high := scenario.canonical.Legs[0], scenario.canonical.Legs[1]
	protective, short := low, high
	if low.Position == "short" {
		protective, short = high, low
	}
	multiplier := decimal.RequireFromString(low.Multiplier)
	fee := decimal.RequireFromString(policy.canonical.FeePerContractPerLeg)
	width := decimal.RequireFromString(high.Strike).Sub(decimal.RequireFromString(low.Strike))
	longAsk := decimal.RequireFromString(protective.Entry.Ask)
	shortBid := decimal.RequireFromString(short.Entry.Bid)
	netPremium := longAsk.Sub(shortBid)
	entryFeesPerContract := fee.Mul(decimal.NewFromInt(2))
	maxLoss := netPremium.Mul(multiplier).Add(entryFeesPerContract)
	maxReward := width.Sub(netPremium).Mul(multiplier).Sub(entryFeesPerContract)
	if netPremium.IsNegative() {
		maxLoss = width.Add(netPremium).Mul(multiplier).Add(entryFeesPerContract)
		maxReward = netPremium.Neg().Mul(multiplier).Sub(entryFeesPerContract)
	}
	if !maxLoss.IsPositive() || maxReward.IsNegative() {
		return nil, fmt.Errorf("defined-risk executable economics are invalid")
	}
	orphanReserve := decimal.Zero
	if policy.canonical.ExecutionMode == ExecutionSequential {
		if protective.Unwind == nil {
			return nil, fmt.Errorf("defined-risk sequential execution lacks protective-leg unwind evidence")
		}
		orphanReserve = longAsk.Sub(decimal.RequireFromString(protective.Unwind.Bid)).Mul(multiplier).Add(entryFeesPerContract)
		if orphanReserve.IsNegative() {
			orphanReserve = decimal.Zero
		}
	}
	reservationPerContract := maxLoss.Add(orphanReserve)
	capitalLimit := decimal.Min(decimal.RequireFromString(scenario.canonical.InitialCapital), decimal.RequireFromString(policy.canonical.MaximumPositionCapital))
	capitalContracts := int(capitalLimit.Div(reservationPerContract).Floor().IntPart())
	contracts := minimumInt(scenario.canonical.RequestedContracts, policy.canonical.MaximumContracts, capitalContracts)
	q := func(v decimal.Decimal) string {
		return v.Round(int32(policy.DecimalScale())).StringFixed(int32(policy.DecimalScale()))
	}
	zero := q(decimal.Zero)
	report := reportCanonical{Schema: ReportSchemaV1, State: "completed", PolicyID: policy.ID().String(), PolicySHA256: policy.Digest(), ScenarioID: scenario.ID().String(), ScenarioSHA256: scenario.Digest(), Strategy: scenario.canonical.Strategy, ExecutionMode: policy.canonical.ExecutionMode, Outcome: "rejected", Reason: "insufficient_capital", Width: q(width), NetPremiumPerContract: q(netPremium.Mul(multiplier)), MaximumLossPerContract: q(maxLoss), MaximumRewardPerContract: q(maxReward), OrphanReservePerContract: q(orphanReserve), ReservedCapital: zero, EntryFees: zero, UnwindFees: zero, OrphanLoss: zero, ExpirationPayoff: zero, EndingCash: q(decimal.RequireFromString(scenario.canonical.InitialCapital)), AfterCostTotalReturn: zero, Fills: []Fill{}}
	if contracts < 1 {
		return finishReport(report)
	}
	if available(protective.Entry.AskSize) < contracts {
		report.Reason = "insufficient_protective_depth"
		return finishReport(report)
	}
	if policy.canonical.ExecutionMode == ExecutionAtomic && available(short.Entry.BidSize) < contracts {
		report.Reason = "atomic_package_depth_unavailable"
		return finishReport(report)
	}
	report.Contracts = contracts
	report.ReservedCapital = q(reservationPerContract.Mul(decimal.NewFromInt(int64(contracts))))
	entryFees := entryFeesPerContract.Mul(decimal.NewFromInt(int64(contracts)))
	report.EntryFees = q(entryFees)
	report.Fills = append(report.Fills, fill(0, protective, "open", contracts, protective.Entry.Ask, fee, protective.Entry))
	if policy.canonical.ExecutionMode == ExecutionSequential && available(short.Entry.BidSize) < contracts {
		unwind := *protective.Unwind
		if available(unwind.BidSize) < contracts {
			return nil, fmt.Errorf("defined-risk orphan unwind depth is insufficient")
		}
		report.Fills = append(report.Fills, fill(1, protective, "unwind", contracts, unwind.Bid, fee, unwind))
		unwindFees := fee.Mul(decimal.NewFromInt(int64(contracts)))
		report.EntryFees = q(unwindFees)
		actualLoss := longAsk.Sub(decimal.RequireFromString(unwind.Bid)).Mul(multiplier).Mul(decimal.NewFromInt(int64(contracts))).Add(entryFeesPerContract.Mul(decimal.NewFromInt(int64(contracts))))
		ending := decimal.RequireFromString(scenario.canonical.InitialCapital).Sub(actualLoss)
		report.Outcome, report.Reason = "orphan_unwound", "second_leg_depth_unavailable"
		report.UnwindFees, report.OrphanLoss, report.ExpirationPayoff, report.EndingCash = q(unwindFees), q(actualLoss), q(decimal.Zero), q(ending)
		report.AfterCostTotalReturn = q(ending.Div(decimal.RequireFromString(scenario.canonical.InitialCapital)).Sub(decimal.NewFromInt(1)))
		return finishReport(report)
	}
	report.Fills = append(report.Fills, fill(1, short, "open", contracts, short.Entry.Bid, fee, short.Entry))
	spot := decimal.RequireFromString(scenario.canonical.TerminalUnderlying)
	payoffPerContract := signedIntrinsic(low, spot).Add(signedIntrinsic(high, spot)).Mul(multiplier)
	payoff := payoffPerContract.Mul(decimal.NewFromInt(int64(contracts)))
	openingCash := netPremium.Mul(multiplier).Mul(decimal.NewFromInt(int64(contracts))).Add(entryFees)
	ending := decimal.RequireFromString(scenario.canonical.InitialCapital).Sub(openingCash).Add(payoff)
	report.Outcome, report.Reason = "settled", "expiry_intrinsic_cash_settlement"
	report.OrphanLoss, report.UnwindFees, report.ExpirationPayoff, report.EndingCash = q(decimal.Zero), q(decimal.Zero), q(payoff), q(ending)
	report.AfterCostTotalReturn = q(ending.Div(decimal.RequireFromString(scenario.canonical.InitialCapital)).Sub(decimal.NewFromInt(1)))
	return finishReport(report)
}

func signedIntrinsic(leg legCanonical, spot decimal.Decimal) decimal.Decimal {
	strike := decimal.RequireFromString(leg.Strike)
	intrinsic := decimal.Zero
	if leg.OptionType == "call" && spot.GreaterThan(strike) {
		intrinsic = spot.Sub(strike)
	}
	if leg.OptionType == "put" && strike.GreaterThan(spot) {
		intrinsic = strike.Sub(spot)
	}
	if leg.Position == "short" {
		return intrinsic.Neg()
	}
	return intrinsic
}

func fill(sequence int, leg legCanonical, action string, contracts int, price string, fee decimal.Decimal, evidence quoteCanonical) Fill {
	return Fill{sequence, leg.InstrumentID, leg.Position, action, contracts, price, fee.Mul(decimal.NewFromInt(int64(contracts))).String(), evidence.EvidenceID, evidence.EvidenceSHA256}
}
func available(value string) int { return int(decimal.RequireFromString(value).Floor().IntPart()) }
func minimumInt(values ...int) int {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

func finishReport(c reportCanonical) (*Report, error) {
	encoded, _ := json.Marshal(c)
	digest := hash(encoded)
	return &Report{canonical: c, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("defined-risk-options-report", ReportSchemaV1+"@sha256:"+digest)}, nil
}

func ReportFromCanonical(id uuid.UUID, digest string, raw []byte, policy *Policy, scenario *Scenario) (*Report, error) {
	var c reportCanonical
	if id == uuid.Nil || policy == nil || scenario == nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &c) != nil {
		return nil, fmt.Errorf("defined-risk report envelope is invalid")
	}
	value, err := NewReport(policy, scenario)
	if err != nil || c.Schema != ReportSchemaV1 || c.State != "completed" || value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("defined-risk report identity does not reconstruct")
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

func (r *Report) Outcome() string {
	if r == nil {
		return ""
	}
	return r.canonical.Outcome
}

func (r *Report) Contracts() int {
	if r == nil {
		return 0
	}
	return r.canonical.Contracts
}

func (r *Report) ReservedCapital() string {
	if r == nil {
		return ""
	}
	return r.canonical.ReservedCapital
}

func (r *Report) OrphanLoss() string {
	if r == nil {
		return ""
	}
	return r.canonical.OrphanLoss
}

func (r *Report) EndingCash() string {
	if r == nil {
		return ""
	}
	return r.canonical.EndingCash
}

func (r *Report) Fills() []Fill {
	if r == nil {
		return nil
	}
	return append([]Fill(nil), r.canonical.Fills...)
}
