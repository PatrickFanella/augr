package optionsresearch

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/edge"
)

// Config controls conservative paper-only options screening.
// MaxBidAskSpread is an absolute per-contract spread in option premium units.
type Config struct {
	MinOpenInterest       float64
	MinVolume             float64
	MaxBidAskSpread       float64
	MinNetEdge            float64
	MaxThetaExposure      float64
	MaxUnderlyingExposure float64
	Bankroll              float64
	FractionalKellyCap    float64
	RiskFreeRate          float64
	DividendYield         float64
	PeriodsPerYear        float64
}

// Input supplies the spread candidate and market context.
type Input struct {
	Now              time.Time
	UnderlyingPrice  float64
	UnderlyingPrices []float64
	Spread           domain.OptionSpread
	Chain            []domain.OptionSnapshot
	StrategyID       *uuid.UUID
	PipelineRunID    *uuid.UUID
}

// Result contains the journal-ready decision and scan metadata.
type Result struct {
	Accepted           bool
	Reasons            []string
	Decision           domain.TradeDecision
	ModelPrice         float64
	ExecutablePrice    float64
	GrossEdge          float64
	NetEdge            float64
	KellyFraction      float64
	ProposedSize       float64
	ApprovedSize       float64
	ThetaExposure      float64
	UnderlyingExposure float64
}

// Scanner performs conservative options screening.
type Scanner struct {
	cfg Config
}

// NewScanner creates a new conservative scanner.
func NewScanner(cfg Config) Scanner {
	return Scanner{cfg: cfg}
}

// Scan evaluates a single options spread candidate.
func (s Scanner) Scan(in Input) Result {
	reasons := make([]string, 0, 4)
	if !isFinitePositive(in.UnderlyingPrice) {
		reasons = append(reasons, "invalid_underlying_price")
	}

	spreadKind := in.Spread.StrategyType
	legs, structuralReason := classifySpread(in.Spread)
	if structuralReason != "" {
		reasons = append(reasons, structuralReason)
	}

	if len(legs) == 0 {
		return s.rejectedDecision(in, reasons, 0, 0, 0, 0, 0, 0, 0)
	}

	resolvedLegs, legReasons := s.resolveLegs(in, legs)
	reasons = append(reasons, legReasons...)
	if len(legReasons) > 0 {
		return s.rejectedDecision(in, reasons, 0, 0, 0, 0, 0, 0, 0)
	}

	for _, rl := range resolvedLegs {
		if !isFinitePositive(rl.snapshot.Bid) || !isFinitePositive(rl.snapshot.Ask) || rl.snapshot.Ask <= rl.snapshot.Bid {
			reasons = append(reasons, "invalid_executable_price")
			return s.rejectedDecision(in, reasons, 0, 0, 0, 0, 0, 0, 0)
		}
	}

	if unsupported := unsupportedPhaseOneReason(spreadKind, resolvedLegs); unsupported != "" {
		reasons = append(reasons, unsupported)
		return s.rejectedDecision(in, reasons, 0, 0, 0, 0, 0, 0, 0)
	}

	modelPrice, execPrice, delta, theta, openInterest, volume, spreadWidth, modelOK := s.priceSpread(in, resolvedLegs)
	if !modelOK {
		reasons = append(reasons, "invalid_pricing_inputs")
		return s.rejectedDecision(in, reasons, modelPrice, execPrice, 0, 0, 0, 0, 0)
	}

	if spreadWidth > 0 && s.cfg.MaxBidAskSpread > 0 && spreadWidth > s.cfg.MaxBidAskSpread {
		reasons = append(reasons, "wide_spread")
	}
	if openInterest < s.cfg.MinOpenInterest {
		reasons = append(reasons, "insufficient_open_interest")
	}
	if volume < s.cfg.MinVolume {
		reasons = append(reasons, "insufficient_volume")
	}
	if modelPrice <= 0 || execPrice <= 0 || !isFinite(modelPrice) || !isFinite(execPrice) {
		reasons = append(reasons, "invalid_executable_price")
	}
	if len(reasons) > 0 {
		return s.rejectedDecision(in, reasons, modelPrice, execPrice, 0, 0, 0, 0, 0)
	}

	grossEdge := modelPrice - execPrice
	netEdge := grossEdge
	if isFinitePositive(execPrice) && grossEdge >= s.cfg.MinNetEdge {
		kelly := grossEdge / execPrice
		if s.cfg.FractionalKellyCap > 0 && kelly > s.cfg.FractionalKellyCap {
			kelly = s.cfg.FractionalKellyCap
		}
		if !isFinite(kelly) || kelly <= 0 {
			reasons = append(reasons, "insufficient_edge")
			return s.rejectedDecision(in, reasons, modelPrice, execPrice, grossEdge, netEdge, 0, 0, 0)
		}

		proposedSize := s.cfg.Bankroll * kelly
		if !isFinitePositive(s.cfg.Bankroll) || !isFinitePositive(s.cfg.FractionalKellyCap) {
			reasons = append(reasons, "invalid_scanner_config")
			return s.rejectedDecision(in, reasons, modelPrice, execPrice, grossEdge, netEdge, 0, 0, 0)
		}
		if proposedSize <= 0 {
			reasons = append(reasons, "insufficient_edge")
			return s.rejectedDecision(in, reasons, modelPrice, execPrice, grossEdge, netEdge, 0, 0, 0)
		}

		contracts := proposedSize / (execPrice * resolvedLegs[0].snapshot.Contract.Multiplier)
		if !isFinitePositive(contracts) {
			reasons = append(reasons, "invalid_contract_multiplier")
			return s.rejectedDecision(in, reasons, modelPrice, execPrice, grossEdge, netEdge, 0, 0, 0)
		}

		thetaExposure := math.Abs(theta) * proposedSize / execPrice
		underlyingExposure := math.Abs(delta) * in.UnderlyingPrice * proposedSize / execPrice
		if s.cfg.MaxThetaExposure > 0 && thetaExposure > s.cfg.MaxThetaExposure {
			reasons = append(reasons, "theta_limit_exceeded")
			return s.rejectedDecision(in, reasons, modelPrice, execPrice, grossEdge, netEdge, thetaExposure, underlyingExposure, proposedSize)
		}
		if s.cfg.MaxUnderlyingExposure > 0 && underlyingExposure > s.cfg.MaxUnderlyingExposure {
			reasons = append(reasons, "allocation_limit_exceeded")
			return s.rejectedDecision(in, reasons, modelPrice, execPrice, grossEdge, netEdge, thetaExposure, underlyingExposure, proposedSize)
		}

		decision := s.acceptedDecision(in, resolvedLegs, modelPrice, execPrice, grossEdge, netEdge, kelly, proposedSize, proposedSize, openInterest, volume, spreadWidth)
		return Result{
			Accepted:           true,
			Decision:           decision,
			ModelPrice:         modelPrice,
			ExecutablePrice:    execPrice,
			GrossEdge:          grossEdge,
			NetEdge:            netEdge,
			KellyFraction:      kelly,
			ProposedSize:       proposedSize,
			ApprovedSize:       proposedSize,
			ThetaExposure:      thetaExposure,
			UnderlyingExposure: underlyingExposure,
		}
	}

	reasons = append(reasons, "insufficient_edge")
	return s.rejectedDecision(in, reasons, modelPrice, execPrice, grossEdge, netEdge, 0, 0, 0)
}

type resolvedLeg struct {
	leg      domain.SpreadLeg
	snapshot domain.OptionSnapshot
}

func (s Scanner) resolveLegs(in Input, legs []domain.SpreadLeg) ([]resolvedLeg, []string) {
	resolved := make([]resolvedLeg, 0, len(legs))
	var reasons []string
	for _, leg := range legs {
		snap, ok := matchSnapshot(leg.Contract, in.Chain)
		if !ok {
			reasons = append(reasons, "invalid_pricing_inputs")
			continue
		}
		if snap.Contract.Expiry.IsZero() || !in.Now.Before(snap.Contract.Expiry) {
			reasons = append(reasons, "stale_contract")
			continue
		}
		resolved = append(resolved, resolvedLeg{leg: leg, snapshot: snap})
	}
	return resolved, reasons
}

func (s Scanner) priceSpread(in Input, legs []resolvedLeg) (modelPrice, execPrice, delta, theta, openInterest, volume, spreadWidth float64, ok bool) {
	openInterest = math.Inf(1)
	volume = math.Inf(1)
	for _, rl := range legs {
		qty := rl.leg.Quantity
		if !isFinitePositive(qty) || rl.leg.Ratio <= 0 {
			return 0, 0, 0, 0, 0, 0, 0, false
		}
		multiplier := rl.snapshot.Contract.Multiplier
		if !isFinitePositive(multiplier) {
			return 0, 0, 0, 0, 0, 0, 0, false
		}

		vol := rl.snapshot.Greeks.IV
		if !isFinitePositive(vol) {
			rv := edge.RealizedVolatility(in.UnderlyingPrices, s.cfg.PeriodsPerYear)
			if !rv.OK {
				return 0, 0, 0, 0, 0, 0, 0, false
			}
			vol = rv.Annualized
		}

		pricingIn := edge.BlackScholesInput{
			Spot:              in.UnderlyingPrice,
			Strike:            rl.snapshot.Contract.Strike,
			Rate:              s.cfg.RiskFreeRate,
			DividendYield:     s.cfg.DividendYield,
			Volatility:        vol,
			TimeToExpiryYears: yearsToExpiry(in.Now, rl.snapshot.Contract.Expiry),
		}
		var priced edge.BlackScholesResult
		switch rl.snapshot.Contract.OptionType {
		case domain.OptionTypeCall:
			priced = edge.BlackScholesCall(pricingIn)
		case domain.OptionTypePut:
			priced = edge.BlackScholesPut(pricingIn)
		default:
			return 0, 0, 0, 0, 0, 0, 0, false
		}
		if !priced.OK {
			return 0, 0, 0, 0, 0, 0, 0, false
		}

		sign := 1.0
		if rl.leg.PositionIntent == domain.PositionIntentSellToOpen {
			sign = -1
		}
		weight := qty * float64(rl.leg.Ratio)
		modelPrice += sign * priced.Price * weight
		execQuote := rl.snapshot.Ask
		if sign < 0 {
			execQuote = rl.snapshot.Bid
		}
		if !isFinitePositive(execQuote) || !isFinitePositive(rl.snapshot.Bid) || !isFinitePositive(rl.snapshot.Ask) || rl.snapshot.Ask <= rl.snapshot.Bid {
			return 0, 0, 0, 0, 0, 0, 0, false
		}
		execPrice += sign * execQuote * weight
		delta += sign * priced.Greeks.Delta * weight
		theta += sign * priced.Greeks.Theta * weight
		if rl.snapshot.OpenInterest < openInterest {
			openInterest = rl.snapshot.OpenInterest
		}
		if rl.snapshot.Volume < volume {
			volume = rl.snapshot.Volume
		}
		spreadWidth += (rl.snapshot.Ask - rl.snapshot.Bid) * weight
	}

	if !isFinite(openInterest) || !isFinite(volume) {
		return 0, 0, 0, 0, 0, 0, 0, false
	}

	return modelPrice, execPrice, delta, theta, openInterest, volume, spreadWidth, true
}

func (s Scanner) acceptedDecision(in Input, legs []resolvedLeg, modelPrice, execPrice, grossEdge, netEdge, kelly, proposedSize, approvedSize, openInterest, volume, spreadWidth float64) domain.TradeDecision {
	return domain.TradeDecision{
		ID:              uuid.New(),
		StrategyID:      in.StrategyID,
		PipelineRunID:   in.PipelineRunID,
		MarketType:      domain.MarketTypeOptions,
		InstrumentKey:   spreadKey(in.Spread, legs),
		Side:            domain.OrderSideBuy,
		FairValue:       finiteOrZero(modelPrice),
		ExecutablePrice: finiteOrZero(execPrice),
		Spread:          finiteOrZero(spreadWidth),
		Depth:           finiteOrZero(openInterest + volume),
		GrossEV:         finiteOrZero(grossEdge),
		NetEV:           finiteOrZero(netEdge),
		KellyFraction:   finiteOrZero(kelly),
		ProposedSize:    finiteOrZero(proposedSize),
		ApprovedSize:    finiteOrZero(approvedSize),
		RiskStatus:      domain.RiskDecisionApproved,
		Status:          domain.TradeDecisionStatusCandidate,
		CreatedAt:       in.Now,
		UpdatedAt:       in.Now,
	}
}

func (s Scanner) rejectedDecision(in Input, reasons []string, modelPrice, execPrice, grossEdge, netEdge, thetaExposure, underlyingExposure, proposedSize float64) Result {
	decision := domain.TradeDecision{
		ID:              uuid.New(),
		StrategyID:      in.StrategyID,
		PipelineRunID:   in.PipelineRunID,
		MarketType:      domain.MarketTypeOptions,
		InstrumentKey:   spreadKey(in.Spread, nil),
		Side:            domain.OrderSideBuy,
		FairValue:       finiteOrZero(modelPrice),
		ExecutablePrice: finiteOrZero(execPrice),
		Spread:          0,
		Depth:           0,
		GrossEV:         finiteOrZero(grossEdge),
		NetEV:           finiteOrZero(netEdge),
		KellyFraction:   0,
		ProposedSize:    finiteOrZero(proposedSize),
		ApprovedSize:    0,
		RiskStatus:      domain.RiskDecisionRejected,
		RiskReasons:     append([]string(nil), reasons...),
		Status:          domain.TradeDecisionStatusRejected,
		CreatedAt:       in.Now,
		UpdatedAt:       in.Now,
	}
	return Result{
		Accepted:           false,
		Reasons:            append([]string(nil), reasons...),
		Decision:           decision,
		ModelPrice:         finiteOrZero(modelPrice),
		ExecutablePrice:    finiteOrZero(execPrice),
		GrossEdge:          finiteOrZero(grossEdge),
		NetEdge:            finiteOrZero(netEdge),
		ProposedSize:       finiteOrZero(proposedSize),
		ApprovedSize:       0,
		ThetaExposure:      finiteOrZero(thetaExposure),
		UnderlyingExposure: finiteOrZero(underlyingExposure),
	}
}

func classifySpread(spread domain.OptionSpread) ([]domain.SpreadLeg, string) {
	switch spread.StrategyType {
	case domain.StrategyLongCall:
		return spread.Legs, validateSingleLeg(spread, domain.OptionTypeCall)
	case domain.StrategyLongPut:
		return spread.Legs, validateSingleLeg(spread, domain.OptionTypePut)
	case domain.StrategyBullCallSpread:
		return spread.Legs, validateDebitVertical(spread, domain.OptionTypeCall, true)
	case domain.StrategyBearPutSpread:
		return spread.Legs, validateDebitVertical(spread, domain.OptionTypePut, false)
	default:
		for _, leg := range spread.Legs {
			if leg.PositionIntent == domain.PositionIntentSellToOpen {
				return nil, "undefined_risk_rejected"
			}
		}
		return nil, "unsupported_strategy"
	}
}

func validateSingleLeg(spread domain.OptionSpread, expectedType domain.OptionType) string {
	if len(spread.Legs) != 1 {
		if hasSellToOpenLeg(spread.Legs) {
			return "undefined_risk_rejected"
		}
		return "unsupported_strategy"
	}
	leg := spread.Legs[0]
	if leg.PositionIntent != domain.PositionIntentBuyToOpen || leg.Side != domain.OrderSideBuy || leg.Contract.OptionType != expectedType {
		if leg.PositionIntent == domain.PositionIntentSellToOpen {
			return "undefined_risk_rejected"
		}
		return "unsupported_strategy"
	}
	return ""
}

func validateDebitVertical(spread domain.OptionSpread, expectedType domain.OptionType, bullish bool) string {
	rejectReason := "unsupported_strategy"
	if hasSellToOpenLeg(spread.Legs) {
		rejectReason = "undefined_risk_rejected"
	}
	if len(spread.Legs) != 2 {
		return rejectReason
	}
	var longLeg, shortLeg *domain.SpreadLeg
	for i := range spread.Legs {
		leg := &spread.Legs[i]
		switch leg.PositionIntent {
		case domain.PositionIntentBuyToOpen:
			longLeg = leg
		case domain.PositionIntentSellToOpen:
			shortLeg = leg
		default:
			return rejectReason
		}
	}
	if longLeg == nil || shortLeg == nil {
		return rejectReason
	}
	if longLeg.Contract.OptionType != expectedType || shortLeg.Contract.OptionType != expectedType {
		return rejectReason
	}
	longWeight := longLeg.Quantity * float64(longLeg.Ratio)
	shortWeight := shortLeg.Quantity * float64(shortLeg.Ratio)
	if !isFinitePositive(longWeight) || !isFinitePositive(shortWeight) || !almostEqual(longWeight, shortWeight, 1e-9) {
		return rejectReason
	}
	if bullish && !(longLeg.Contract.Strike < shortLeg.Contract.Strike) {
		return rejectReason
	}
	if !bullish && !(longLeg.Contract.Strike > shortLeg.Contract.Strike) {
		return rejectReason
	}
	if longLeg.Side != domain.OrderSideBuy || shortLeg.Side != domain.OrderSideSell {
		return rejectReason
	}
	if longLeg.Contract.Underlying != shortLeg.Contract.Underlying || !longLeg.Contract.Expiry.Equal(shortLeg.Contract.Expiry) {
		return rejectReason
	}
	return ""
}

func hasSellToOpenLeg(legs []domain.SpreadLeg) bool {
	for _, leg := range legs {
		if leg.PositionIntent == domain.PositionIntentSellToOpen {
			return true
		}
	}
	return false
}

func unsupportedPhaseOneReason(strategy domain.OptionStrategyType, legs []resolvedLeg) string {
	if strategy == domain.StrategyLongCall || strategy == domain.StrategyLongPut || strategy == domain.StrategyBullCallSpread || strategy == domain.StrategyBearPutSpread {
		return ""
	}
	if len(legs) == 1 && legs[0].leg.PositionIntent == domain.PositionIntentSellToOpen {
		return "undefined_risk_rejected"
	}
	return "unsupported_strategy"
}

func matchSnapshot(contract domain.OptionContract, chain []domain.OptionSnapshot) (domain.OptionSnapshot, bool) {
	for _, snap := range chain {
		if contract.OCCSymbol != "" && snap.Contract.OCCSymbol == contract.OCCSymbol {
			return snap, true
		}
		if contract.OCCSymbol == "" && sameContract(contract, snap.Contract) {
			return snap, true
		}
	}
	return domain.OptionSnapshot{}, false
}

func sameContract(a, b domain.OptionContract) bool {
	return strings.EqualFold(strings.TrimSpace(a.Underlying), strings.TrimSpace(b.Underlying)) && a.OptionType == b.OptionType && almostEqual(a.Strike, b.Strike, 1e-9) && a.Expiry.Equal(b.Expiry)
}

func spreadKey(spread domain.OptionSpread, legs []resolvedLeg) string {
	if len(legs) == 0 {
		return strings.TrimSpace(string(spread.StrategyType))
	}
	if len(legs) == 1 {
		return resolvedLegKey(legs[0])
	}
	ordered := make([]resolvedLeg, len(legs))
	copy(ordered, legs)
	if len(ordered) == 2 && ordered[0].leg.PositionIntent == domain.PositionIntentSellToOpen && ordered[1].leg.PositionIntent == domain.PositionIntentBuyToOpen {
		ordered[0], ordered[1] = ordered[1], ordered[0]
	}
	parts := make([]string, 0, len(ordered))
	for _, rl := range ordered {
		parts = append(parts, resolvedLegKey(rl))
	}
	return fmt.Sprintf("%s:%s", spread.StrategyType, strings.Join(parts, "+"))
}

func resolvedLegKey(rl resolvedLeg) string {
	contract := rl.snapshot.Contract
	if contract.OCCSymbol != "" {
		return contract.OCCSymbol
	}
	if rl.leg.Contract.OCCSymbol != "" {
		return rl.leg.Contract.OCCSymbol
	}
	if contract.Underlying == "" {
		contract.Underlying = rl.leg.Contract.Underlying
	}
	if contract.OptionType == "" {
		contract.OptionType = rl.leg.Contract.OptionType
	}
	if contract.Strike == 0 {
		contract.Strike = rl.leg.Contract.Strike
	}
	if contract.Expiry.IsZero() {
		contract.Expiry = rl.leg.Contract.Expiry
	}
	return domain.FormatOCC(contract.Underlying, contract.OptionType, contract.Strike, contract.Expiry)
}

func yearsToExpiry(now, expiry time.Time) float64 {
	if expiry.IsZero() || !now.Before(expiry) {
		return 0
	}
	return expiry.Sub(now).Hours() / 24 / 365.0
}

func finiteOrZero(v float64) float64 {
	if !isFinite(v) {
		return 0
	}
	return v
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func isFinitePositive(v float64) bool {
	return isFinite(v) && v > 0
}

func almostEqual(a, b, tol float64) bool {
	if a > b {
		return a-b <= tol
	}
	return b-a <= tol
}
