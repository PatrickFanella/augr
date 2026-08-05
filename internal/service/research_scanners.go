package service

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/optionsresearch"
	"github.com/PatrickFanella/get-rich-quick/internal/polymarketresearch"
)

const researchDefaultLimit = 5

// PolymarketMarketDataFetcher fetches market data for slug-based research scans.
type PolymarketMarketDataFetcher interface {
	GetMarketData(ctx context.Context, slug string) (*agent.PredictionMarketData, error)
}

// ResearchScannerService exposes read-only research opportunity scans.
type ResearchScannerService interface {
	ScanOptions(ctx context.Context, req OptionsOpportunityRequest) ([]ResearchOpportunity, error)
	ScanPolymarket(ctx context.Context, req PolymarketOpportunityRequest) ([]ResearchOpportunity, error)
}

// OptionsOpportunityRequest configures an options research scan.
type OptionsOpportunityRequest struct {
	Underlying string
	StrategyID *uuid.UUID
	Limit      int
	Expiry     *time.Time
	OptionType domain.OptionType
}

// PolymarketOpportunityRequest configures a Polymarket research scan.
type PolymarketOpportunityRequest struct {
	Slug        string
	TokenID     string
	Outcome     string
	StrategyID  *uuid.UUID
	Limit       int
	Probability *float64
	BestBid     *float64
	BestAsk     *float64
	AskDepthUSD *float64
	AskSize     *float64
}

// ResearchOpportunity wraps a journal-ready trade decision from a scan.
type ResearchOpportunity struct {
	Decision domain.TradeDecision `json:"decision"`
}

type optionCandidate struct {
	spread domain.OptionSpread
	chain  []domain.OptionSnapshot
}

// ResearchScanner performs safe read-only scans over provider-backed data.
type ResearchScanner struct {
	optionsProvider   data.OptionsDataProvider
	polymarketFetcher PolymarketMarketDataFetcher
	optionsScanner    optionsresearch.Scanner
	polymarketConfig  polymarketresearch.ScanConfig
	logger            *slog.Logger
	nowFunc           func() time.Time
}

// NewResearchScannerService constructs the read-only research service boundary.
func NewResearchScannerService(optionsProvider data.OptionsDataProvider, polymarketFetcher PolymarketMarketDataFetcher, logger *slog.Logger) ResearchScannerService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ResearchScanner{
		optionsProvider:   optionsProvider,
		polymarketFetcher: polymarketFetcher,
		optionsScanner: optionsresearch.NewScanner(optionsresearch.Config{
			MinOpenInterest:       100,
			MinVolume:             50,
			MaxBidAskSpread:       1.00,
			MinNetEdge:            0.01,
			MaxThetaExposure:      50_000,
			MaxUnderlyingExposure: 50_000,
			Bankroll:              10_000,
			FractionalKellyCap:    0.20,
			RiskFreeRate:          0.02,
			DividendYield:         0,
			PeriodsPerYear:        252,
		}),
		polymarketConfig: polymarketresearch.DefaultScanConfig(),
		logger:           logger,
		nowFunc:          time.Now,
	}
}

func (s *ResearchScanner) ScanOptions(ctx context.Context, req OptionsOpportunityRequest) ([]ResearchOpportunity, error) {
	if s == nil || s.optionsProvider == nil {
		return []ResearchOpportunity{}, nil
	}

	underlying := strings.ToUpper(strings.TrimSpace(req.Underlying))
	if underlying == "" {
		return []ResearchOpportunity{}, nil
	}

	chain, err := s.optionsProvider.GetOptionsChain(ctx, underlying, timeFromPtr(req.Expiry), req.OptionType)
	if err != nil {
		return nil, err
	}
	if len(chain) == 0 {
		return []ResearchOpportunity{}, nil
	}

	now := s.now().UTC()
	_, filtered := selectOptionsExpiry(chain, req.Expiry, now)
	if len(filtered) == 0 {
		return []ResearchOpportunity{}, nil
	}

	underlyingPrice := estimateUnderlyingPrice(filtered)
	if !isFinitePositive(underlyingPrice) {
		return []ResearchOpportunity{}, nil
	}

	accepted := make([]ResearchOpportunity, 0, 4)
	for _, candidate := range buildOptionsCandidates(filtered, underlyingPrice, req.OptionType) {
		result := s.optionsScanner.Scan(optionsresearch.Input{
			Now:              now,
			UnderlyingPrice:  underlyingPrice,
			UnderlyingPrices: syntheticUnderlyingPrices(underlyingPrice),
			Spread:           candidate.spread,
			Chain:            candidate.chain,
			StrategyID:       req.StrategyID,
		})
		if result.Accepted {
			accepted = append(accepted, ResearchOpportunity{Decision: result.Decision})
		}
	}

	sort.SliceStable(accepted, func(i, j int) bool {
		return accepted[i].Decision.InstrumentKey < accepted[j].Decision.InstrumentKey
	})
	return limitResearchOpportunities(accepted, req.Limit), nil
}

func (s *ResearchScanner) ScanPolymarket(ctx context.Context, req PolymarketOpportunityRequest) ([]ResearchOpportunity, error) {
	if s == nil {
		return []ResearchOpportunity{}, nil
	}

	now := s.now().UTC()
	snapshot, estimate, ok, err := s.buildPolymarketInput(ctx, req, now)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []ResearchOpportunity{}, nil
	}

	opp := polymarketresearch.ScanBinaryOpportunity(now, snapshot, estimate, s.polymarketConfig)
	if opp.State != polymarketresearch.OpportunityStateAccepted {
		return []ResearchOpportunity{}, nil
	}
	return limitResearchOpportunities([]ResearchOpportunity{{Decision: opp.Decision}}, req.Limit), nil
}

func (s *ResearchScanner) buildPolymarketInput(ctx context.Context, req PolymarketOpportunityRequest, now time.Time) (domain.PolymarketBookSnapshot, polymarketresearch.BinaryProbabilityEstimate, bool, error) {
	if snapshot, est, ok := req.querySnapshot(now); ok {
		return snapshot, est, true, nil
	}
	if req.Outcome != "" && !strings.EqualFold(strings.TrimSpace(req.Outcome), "yes") {
		return domain.PolymarketBookSnapshot{}, polymarketresearch.BinaryProbabilityEstimate{}, false, nil
	}

	if req.Slug == "" || s.polymarketFetcher == nil {
		return domain.PolymarketBookSnapshot{}, polymarketresearch.BinaryProbabilityEstimate{}, false, nil
	}

	market, err := s.polymarketFetcher.GetMarketData(ctx, strings.TrimSpace(req.Slug))
	if err != nil {
		return domain.PolymarketBookSnapshot{}, polymarketresearch.BinaryProbabilityEstimate{}, false, err
	}
	if market == nil {
		return domain.PolymarketBookSnapshot{}, polymarketresearch.BinaryProbabilityEstimate{}, false, nil
	}

	snapshot := marketSnapshotFromData(market, req, now)
	est := estimateFromRequestOrMarket(req, market)
	if !isFiniteProbability(est.Probability) || !hasExecutableBook(snapshot) {
		return domain.PolymarketBookSnapshot{}, polymarketresearch.BinaryProbabilityEstimate{}, false, nil
	}
	return snapshot, est, true, nil
}

func (req PolymarketOpportunityRequest) querySnapshot(now time.Time) (domain.PolymarketBookSnapshot, polymarketresearch.BinaryProbabilityEstimate, bool) {
	if req.Probability == nil || req.BestBid == nil || req.BestAsk == nil || (req.AskDepthUSD == nil && req.AskSize == nil) {
		return domain.PolymarketBookSnapshot{}, polymarketresearch.BinaryProbabilityEstimate{}, false
	}
	if !isFiniteProbability(*req.Probability) || !isFinitePrice(*req.BestBid) || !isFinitePrice(*req.BestAsk) || *req.BestAsk <= *req.BestBid {
		return domain.PolymarketBookSnapshot{}, polymarketresearch.BinaryProbabilityEstimate{}, false
	}
	if req.Outcome != "" && !strings.EqualFold(strings.TrimSpace(req.Outcome), "yes") {
		return domain.PolymarketBookSnapshot{}, polymarketresearch.BinaryProbabilityEstimate{}, false
	}
	if req.TokenID == "" && req.Slug == "" {
		return domain.PolymarketBookSnapshot{}, polymarketresearch.BinaryProbabilityEstimate{}, false
	}
	book := domain.PolymarketBookSnapshot{
		Slug:       strings.TrimSpace(req.Slug),
		TokenID:    strings.TrimSpace(req.TokenID),
		Outcome:    normalizePolymarketOutcome(req.Outcome),
		BestBid:    *req.BestBid,
		BestAsk:    *req.BestAsk,
		ReceivedAt: now,
	}
	if req.AskDepthUSD != nil && isFinitePositive(*req.AskDepthUSD) {
		book.AskDepthUSD = *req.AskDepthUSD
		book.DepthUSD = *req.AskDepthUSD
	}
	if req.AskSize != nil && isFinitePositive(*req.AskSize) {
		book.Asks = []domain.PolymarketBookLevel{{Price: *req.BestAsk, Size: *req.AskSize}}
		if book.AskDepthUSD == 0 {
			book.AskDepthUSD = *req.BestAsk * *req.AskSize
			book.DepthUSD = book.AskDepthUSD
		}
	}
	if !hasExecutableBook(book) {
		return domain.PolymarketBookSnapshot{}, polymarketresearch.BinaryProbabilityEstimate{}, false
	}
	return book, polymarketresearch.BinaryProbabilityEstimate{Probability: *req.Probability}, true
}

func buildOptionsCandidates(chain []domain.OptionSnapshot, underlyingPrice float64, optionType domain.OptionType) []optionCandidate {
	filtered := dedupeAndSortOptions(chain)
	var out []optionCandidate

	if optionType == "" || optionType == domain.OptionTypeCall {
		callSnaps := filterSnapshotsByType(filtered, domain.OptionTypeCall)
		if len(callSnaps) > 0 {
			if snap := nearestSnapshot(callSnaps, underlyingPrice); snap != nil {
				out = append(out, optionCandidate{spread: spreadForSingle(*snap, domain.StrategyLongCall), chain: callSnaps})
			}
			if vert := adjacentVertical(callSnaps, underlyingPrice, true); vert != nil {
				out = append(out, *vert)
			}
		}
	}
	if optionType == "" || optionType == domain.OptionTypePut {
		putSnaps := filterSnapshotsByType(filtered, domain.OptionTypePut)
		if len(putSnaps) > 0 {
			if snap := nearestSnapshot(putSnaps, underlyingPrice); snap != nil {
				out = append(out, optionCandidate{spread: spreadForSingle(*snap, domain.StrategyLongPut), chain: putSnaps})
			}
			if vert := adjacentVertical(putSnaps, underlyingPrice, false); vert != nil {
				out = append(out, *vert)
			}
		}
	}
	return out
}

func spreadForSingle(snap domain.OptionSnapshot, strategy domain.OptionStrategyType) domain.OptionSpread {
	return domain.OptionSpread{
		StrategyType: strategy,
		Underlying:   snap.Contract.Underlying,
		Legs: []domain.SpreadLeg{{
			Contract:       snap.Contract,
			Side:           domain.OrderSideBuy,
			PositionIntent: domain.PositionIntentBuyToOpen,
			Ratio:          1,
			Quantity:       1,
		}},
	}
}

func adjacentVertical(snaps []domain.OptionSnapshot, underlyingPrice float64, bullish bool) *optionCandidate {
	if len(snaps) < 2 {
		return nil
	}
	ordered := append([]domain.OptionSnapshot(nil), snaps...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Contract.Strike == ordered[j].Contract.Strike {
			return ordered[i].Contract.OCCSymbol < ordered[j].Contract.OCCSymbol
		}
		return ordered[i].Contract.Strike < ordered[j].Contract.Strike
	})

	upperIdx := sort.Search(len(ordered), func(i int) bool { return ordered[i].Contract.Strike > underlyingPrice })
	if upperIdx == 0 {
		upperIdx = 1
	}
	if upperIdx >= len(ordered) {
		upperIdx = len(ordered) - 1
	}
	lowerIdx := upperIdx - 1
	if lowerIdx < 0 || lowerIdx >= len(ordered) || almostEqual(ordered[lowerIdx].Contract.Strike, ordered[upperIdx].Contract.Strike, 1e-9) {
		return nil
	}

	longLeg := domain.SpreadLeg{Quantity: 1, Ratio: 1, PositionIntent: domain.PositionIntentBuyToOpen, Side: domain.OrderSideBuy}
	shortLeg := domain.SpreadLeg{Quantity: 1, Ratio: 1, PositionIntent: domain.PositionIntentSellToOpen, Side: domain.OrderSideSell}
	if bullish {
		if ordered[lowerIdx].Contract.OptionType != domain.OptionTypeCall || ordered[upperIdx].Contract.OptionType != domain.OptionTypeCall {
			return nil
		}
		longLeg.Contract = ordered[lowerIdx].Contract
		shortLeg.Contract = ordered[upperIdx].Contract
		return &optionCandidate{spread: domain.OptionSpread{StrategyType: domain.StrategyBullCallSpread, Underlying: ordered[lowerIdx].Contract.Underlying, Legs: []domain.SpreadLeg{longLeg, shortLeg}}, chain: ordered}
	}
	if ordered[lowerIdx].Contract.OptionType != domain.OptionTypePut || ordered[upperIdx].Contract.OptionType != domain.OptionTypePut {
		return nil
	}
	longLeg.Contract = ordered[upperIdx].Contract
	shortLeg.Contract = ordered[lowerIdx].Contract
	return &optionCandidate{spread: domain.OptionSpread{StrategyType: domain.StrategyBearPutSpread, Underlying: ordered[upperIdx].Contract.Underlying, Legs: []domain.SpreadLeg{longLeg, shortLeg}}, chain: ordered}
}

func nearestSnapshot(snaps []domain.OptionSnapshot, underlyingPrice float64) *domain.OptionSnapshot {
	if len(snaps) == 0 {
		return nil
	}
	bestIdx := 0
	bestDist := math.MaxFloat64
	for i := range snaps {
		dist := math.Abs(snaps[i].Contract.Strike - underlyingPrice)
		if dist < bestDist || (almostEqual(dist, bestDist, 1e-9) && snaps[i].Contract.Strike < snaps[bestIdx].Contract.Strike) {
			bestIdx = i
			bestDist = dist
		}
	}
	return &snaps[bestIdx]
}

func dedupeAndSortOptions(chain []domain.OptionSnapshot) []domain.OptionSnapshot {
	if len(chain) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(chain))
	out := make([]domain.OptionSnapshot, 0, len(chain))
	for _, snap := range chain {
		key := snap.Contract.OCCSymbol
		if key == "" {
			key = domain.FormatOCC(snap.Contract.Underlying, snap.Contract.OptionType, snap.Contract.Strike, snap.Contract.Expiry)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, snap)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Contract.Expiry.Equal(out[j].Contract.Expiry) {
			return out[i].Contract.Expiry.Before(out[j].Contract.Expiry)
		}
		if out[i].Contract.OptionType != out[j].Contract.OptionType {
			return out[i].Contract.OptionType < out[j].Contract.OptionType
		}
		if out[i].Contract.Strike != out[j].Contract.Strike {
			return out[i].Contract.Strike < out[j].Contract.Strike
		}
		return out[i].Contract.OCCSymbol < out[j].Contract.OCCSymbol
	})
	return out
}

func filterSnapshotsByType(chain []domain.OptionSnapshot, optionType domain.OptionType) []domain.OptionSnapshot {
	if optionType == "" {
		return append([]domain.OptionSnapshot(nil), chain...)
	}
	out := make([]domain.OptionSnapshot, 0, len(chain))
	for _, snap := range chain {
		if snap.Contract.OptionType == optionType {
			out = append(out, snap)
		}
	}
	return out
}

func selectOptionsExpiry(chain []domain.OptionSnapshot, requested *time.Time, now time.Time) (time.Time, []domain.OptionSnapshot) {
	if len(chain) == 0 {
		return time.Time{}, nil
	}
	filtered := make([]domain.OptionSnapshot, 0, len(chain))
	if requested != nil && !requested.IsZero() {
		req := requested.UTC()
		for _, snap := range chain {
			if snap.Contract.Expiry.Equal(req) {
				filtered = append(filtered, snap)
			}
		}
		return req, filtered
	}

	var target time.Time
	for _, snap := range chain {
		expiry := snap.Contract.Expiry.UTC()
		if expiry.IsZero() || !expiry.After(now) {
			continue
		}
		if target.IsZero() || expiry.Before(target) {
			target = expiry
		}
	}
	if target.IsZero() {
		return time.Time{}, nil
	}
	for _, snap := range chain {
		if snap.Contract.Expiry.Equal(target) {
			filtered = append(filtered, snap)
		}
	}
	return target, filtered
}

func estimateUnderlyingPrice(chain []domain.OptionSnapshot) float64 {
	if len(chain) == 0 {
		return 0
	}
	byStrike := make(map[int64]map[domain.OptionType]domain.OptionSnapshot, len(chain))
	strikes := make([]float64, 0, len(chain))
	for _, snap := range chain {
		if !isFinitePositive(snap.Contract.Strike) {
			continue
		}
		key := strikeKey(snap.Contract.Strike)
		if _, ok := byStrike[key]; !ok {
			byStrike[key] = make(map[domain.OptionType]domain.OptionSnapshot, 2)
			strikes = append(strikes, snap.Contract.Strike)
		}
		byStrike[key][snap.Contract.OptionType] = snap
	}
	parityEstimates := make([]float64, 0, len(byStrike))
	for strikeKeyVal, pair := range byStrike {
		call, hasCall := pair[domain.OptionTypeCall]
		put, hasPut := pair[domain.OptionTypePut]
		if !hasCall || !hasPut {
			continue
		}
		callMid := quoteMid(call)
		putMid := quoteMid(put)
		if !isFinitePositive(callMid) || !isFinitePositive(putMid) {
			continue
		}
		est := float64(strikeKeyVal)/1000.0 + callMid - putMid
		if isFinitePositive(est) {
			parityEstimates = append(parityEstimates, est)
		}
	}
	if len(parityEstimates) > 0 {
		return median(parityEstimates)
	}
	if len(strikes) > 0 {
		return median(strikes)
	}
	return 0
}

func quoteMid(snap domain.OptionSnapshot) float64 {
	if isFinitePositive(snap.Mid) {
		return snap.Mid
	}
	if isFinitePositive(snap.Bid) && isFinitePositive(snap.Ask) && snap.Ask > snap.Bid {
		return (snap.Bid + snap.Ask) / 2
	}
	return 0
}

func strikeKey(strike float64) int64 { return int64(math.Round(strike * 1000)) }

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

func syntheticUnderlyingPrices(spot float64) []float64 {
	if !isFinitePositive(spot) {
		return nil
	}
	return []float64{spot * 0.99, spot, spot * 1.01, spot * 1.005}
}

func timeFromPtr(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return v.UTC()
}

func normalizePolymarketOutcome(outcome string) string {
	trimmed := strings.ToUpper(strings.TrimSpace(outcome))
	if trimmed == "" {
		return "YES"
	}
	return trimmed
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func estimateFromRequestOrMarket(req PolymarketOpportunityRequest, market *agent.PredictionMarketData) polymarketresearch.BinaryProbabilityEstimate {
	if req.Probability != nil && isFiniteProbability(*req.Probability) {
		return polymarketresearch.BinaryProbabilityEstimate{Probability: *req.Probability}
	}
	if market == nil || !isFiniteProbability(market.YesPrice) {
		return polymarketresearch.BinaryProbabilityEstimate{}
	}
	return polymarketresearch.BinaryProbabilityEstimate{Probability: market.YesPrice, Source: "market_yes_price"}
}

func marketSnapshotFromData(market *agent.PredictionMarketData, req PolymarketOpportunityRequest, now time.Time) domain.PolymarketBookSnapshot {
	snapshot := domain.PolymarketBookSnapshot{
		Slug:       strings.TrimSpace(firstNonEmpty(req.Slug, market.Slug)),
		TokenID:    strings.TrimSpace(firstNonEmpty(req.TokenID, market.YesTokenID)),
		Outcome:    normalizePolymarketOutcome(firstNonEmpty(req.Outcome, "YES")),
		BestBid:    market.BestBidYes,
		BestAsk:    market.BestAskYes,
		ReceivedAt: now,
	}
	if isFinitePositive(market.Liquidity) {
		snapshot.AskDepthUSD = market.Liquidity
		snapshot.DepthUSD = market.Liquidity
	}
	if isFinitePositive(market.BestAskYes) && isFinitePositive(market.Liquidity) {
		snapshot.Asks = []domain.PolymarketBookLevel{{Price: market.BestAskYes, Size: market.Liquidity / market.BestAskYes}}
	}
	return snapshot
}

func (s *ResearchScanner) now() time.Time {
	if s != nil && s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now().UTC()
}

func limitResearchOpportunities(items []ResearchOpportunity, limit int) []ResearchOpportunity {
	if limit <= 0 {
		limit = researchDefaultLimit
	}
	if limit >= len(items) {
		if items == nil {
			return []ResearchOpportunity{}
		}
		return items
	}
	return append([]ResearchOpportunity(nil), items[:limit]...)
}

func hasExecutableBook(snapshot domain.PolymarketBookSnapshot) bool {
	if !isFinitePrice(snapshot.BestBid) || !isFinitePrice(snapshot.BestAsk) || snapshot.BestAsk <= snapshot.BestBid {
		return false
	}
	if isFinitePositive(snapshot.AskDepthUSD) {
		return true
	}
	return len(snapshot.Asks) > 0 && isFinitePrice(snapshot.Asks[0].Price) && isFinitePositive(snapshot.Asks[0].Size)
}

func isFinite(v float64) bool            { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func isFinitePositive(v float64) bool    { return isFinite(v) && v > 0 }
func isFinitePrice(v float64) bool       { return isFinite(v) && v > 0 && v < 1 }
func isFiniteProbability(v float64) bool { return isFinite(v) && v > 0 && v < 1 }
func almostEqual(a, b, tol float64) bool {
	if a > b {
		return a-b <= tol
	}
	return b-a <= tol
}
