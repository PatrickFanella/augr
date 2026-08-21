package copytrading

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const CalculationVersion = 2

type PriceSnapshot struct {
	Ticker          string     `json:"ticker"`
	QuoteSnapshotID uuid.UUID  `json:"quote_snapshot_id"`
	Bid             string     `json:"bid"`
	Ask             string     `json:"ask"`
	AvgDollarVolume float64    `json:"avg_dollar_volume"`
	AvailableAt     *time.Time `json:"available_at"`
	MarketStatus    string     `json:"market_status"`
	SessionStatus   string     `json:"session_status"`
}

type PreviewSummary struct {
	TotalDisclosedValue float64  `json:"total_disclosed_value"`
	MappedWeight        float64  `json:"mapped_weight"`
	UnmappedWeight      float64  `json:"unmapped_weight"`
	ExcludedWeight      float64  `json:"excluded_weight"`
	TargetInvestedValue float64  `json:"target_invested_value"`
	TargetCashValue     float64  `json:"target_cash_value"`
	DesiredTurnover     float64  `json:"desired_turnover"`
	ApprovedTurnover    float64  `json:"approved_turnover"`
	TurnoverScale       float64  `json:"turnover_scale"`
	Warnings            []string `json:"warnings"`
}

type Preview struct {
	Observation domain.CopySourceObservation `json:"observation"`
	Snapshot    domain.CopyPortfolioSnapshot `json:"snapshot"`
	Intents     []domain.CopyTradeIntent     `json:"intents"`
	Summary     PreviewSummary               `json:"summary"`
}

type TargetInput struct {
	Subscription domain.CopySubscription
	Observation  domain.CopySourceObservation
	Snapshot     domain.CopyPortfolioSnapshot
	Mappings     []domain.CopyInstrumentMapping
	Prices       map[string]PriceSnapshot
	Positions    []domain.Position
	DecisionAt   time.Time
}

type targetHolding struct {
	holding domain.CopyPortfolioHolding
	mapping domain.CopyInstrumentMapping
	weight  float64
	value   float64
}

// Build13FTarget deterministically converts one disclosed 13F snapshot into
// capped follower intents. Unsupported, derivative, blocked, and unmapped
// holdings remain cash rather than being redistributed.
func Build13FTarget(input TargetInput) Preview {
	sub := input.Subscription
	result := Preview{Observation: input.Observation, Snapshot: input.Snapshot, Intents: make([]domain.CopyTradeIntent, 0)}
	result.Snapshot.Holdings = nil // avoid repeating the full source table in every preview response
	result.Summary.TotalDisclosedValue = input.Snapshot.TotalDisclosedValue
	if input.Snapshot.TotalDisclosedValue <= 0 {
		result.Summary.Warnings = append(result.Summary.Warnings, "snapshot_total_value_is_zero")
		result.Summary.TargetCashValue = sub.CapitalBudget
		return result
	}

	mappingByCUSIP := make(map[string]domain.CopyInstrumentMapping, len(input.Mappings))
	for _, mapping := range input.Mappings {
		if mapping.Confidence == "manual_verified" || mapping.Confidence == "provider_verified" {
			mappingByCUSIP[strings.ToUpper(strings.TrimSpace(mapping.IdentifierValue))] = mapping
		}
	}
	allow := stringSet(sub.StockAllowlist)
	block := stringSet(sub.StockBlocklist)
	candidates := make([]targetHolding, 0, len(input.Snapshot.Holdings))

	for _, holding := range input.Snapshot.Holdings {
		weight := holding.DisclosedValue / input.Snapshot.TotalDisclosedValue
		if weight <= 0 {
			continue
		}
		if strings.TrimSpace(holding.PutCall) != "" {
			result.Summary.ExcludedWeight += weight
			continue
		}
		mapping, ok := mappingByCUSIP[strings.ToUpper(strings.TrimSpace(holding.CUSIP))]
		if !ok {
			result.Summary.UnmappedWeight += weight
			continue
		}
		ticker := strings.ToUpper(strings.TrimSpace(mapping.Ticker))
		if len(allow) > 0 && !allow[ticker] || block[ticker] || weight < sub.MinSourceWeight {
			result.Summary.ExcludedWeight += weight
			continue
		}
		result.Summary.MappedWeight += weight
		investableBudget := sub.CapitalBudget * (1 - sub.CashBufferPct)
		targetValue := math.Min(weight*investableBudget, sub.CapitalBudget*sub.MaxPositionWeight)
		candidates = append(candidates, targetHolding{holding: holding, mapping: mapping, weight: weight, value: targetValue})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].weight != candidates[j].weight {
			return candidates[i].weight > candidates[j].weight
		}
		return candidates[i].mapping.Ticker < candidates[j].mapping.Ticker
	})
	if sub.TopN > 0 && len(candidates) > sub.TopN {
		for _, excluded := range candidates[sub.TopN:] {
			result.Summary.ExcludedWeight += excluded.weight
			result.Summary.MappedWeight -= excluded.weight
		}
		candidates = candidates[:sub.TopN]
	}

	currentValue := make(map[string]float64)
	for _, position := range input.Positions {
		if position.ClosedAt != nil || position.Quantity <= 0 {
			continue
		}
		ticker := strings.ToUpper(strings.TrimSpace(position.Ticker))
		price := position.AvgEntry
		if snapshot, ok := input.Prices[ticker]; ok {
			if mark, markErr := quoteMidpoint(snapshot); markErr == nil {
				price, _ = mark.Float64()
			}
		} else if position.CurrentPrice != nil && *position.CurrentPrice > 0 {
			price = *position.CurrentPrice
		}
		currentValue[ticker] += position.Quantity * price
	}

	targets := make(map[string]targetHolding, len(candidates))
	for _, candidate := range candidates {
		ticker := strings.ToUpper(candidate.mapping.Ticker)
		targets[ticker] = candidate
		result.Summary.TargetInvestedValue += candidate.value
	}
	result.Summary.TargetCashValue = math.Max(0, sub.CapitalBudget-result.Summary.TargetInvestedValue)

	allTickers := make([]string, 0, len(targets)+len(currentValue))
	seen := map[string]bool{}
	for ticker := range targets {
		seen[ticker] = true
		allTickers = append(allTickers, ticker)
	}
	for ticker := range currentValue {
		if !seen[ticker] {
			allTickers = append(allTickers, ticker)
		}
	}
	sort.Strings(allTickers)

	for _, ticker := range allTickers {
		target := targets[ticker]
		targetValue := target.value
		current := currentValue[ticker]
		delta := targetValue - current
		if math.Abs(delta) < 0.01 {
			continue
		}
		side := domain.OrderSideBuy
		if delta < 0 {
			side = domain.OrderSideSell
		}
		intent := domain.CopyTradeIntent{SubscriptionID: sub.ID, OriginType: "copy_subscription", OriginID: sub.ID, SourceObservationID: input.Observation.ID, InstrumentKey: ticker, Ticker: ticker, Side: side, TargetWeight: target.weight, TargetValue: roundMoney(targetValue), AttributedCurrentValue: roundMoney(current), RequestedNotional: roundMoney(math.Abs(delta)), CalculationVersion: CalculationVersion, QuoteGateVersion: 1, PolicyStatus: "approved", RiskStatus: "pending", Status: "received"}
		intent.ID = economicid.DeterministicUUID("copy-trade-intent", sub.ID.String(), input.Observation.ID.String(), ticker, fmt.Sprintf("%d", CalculationVersion))
		price, ok := input.Prices[ticker]
		reasons := make([]string, 0, 3)
		if !ok || price.QuoteSnapshotID == uuid.Nil {
			reasons = append(reasons, "missing_executable_quote")
		} else {
			bid, bidErr := decimal.NewFromString(price.Bid)
			ask, askErr := decimal.NewFromString(price.Ask)
			switch {
			case bidErr != nil || askErr != nil:
				reasons = append(reasons, "missing_two_sided_quote")
			case !bid.IsPositive() || !ask.IsPositive():
				reasons = append(reasons, "missing_two_sided_quote")
			case ask.LessThan(bid):
				reasons = append(reasons, "crossed_quote")
			default:
				midpoint := bid.Add(ask).Div(decimal.NewFromInt(2))
				spread := ask.Sub(bid).Div(midpoint).Mul(decimal.NewFromInt(10000)).Round(12)
				executable := ask
				if side == domain.OrderSideSell {
					executable = bid
				}
				executableFloat, _ := executable.Float64()
				intent.ExecutablePrice = &executableFloat
				intent.DecisionQuoteSnapshotID = &price.QuoteSnapshotID
				intent.DecisionBid, intent.DecisionAsk, intent.DecisionSpreadBPS = bid.String(), ask.String(), spread.String()
				intent.DecisionAvailableAt, intent.DecisionAt = price.AvailableAt, timePointer(input.DecisionAt)
				intent.DecisionMarketStatus, intent.DecisionSessionStatus = price.MarketStatus, price.SessionStatus
				if executableFloat < sub.MinPrice {
					reasons = append(reasons, "below_min_price")
				}
				if sub.MaxSpreadBPS > 0 && spread.GreaterThan(decimal.NewFromInt(int64(sub.MaxSpreadBPS))) {
					reasons = append(reasons, "above_max_spread")
				}
			}
			if price.AvailableAt == nil {
				reasons = append(reasons, "missing_quote_availability")
			} else {
				age := input.DecisionAt.Sub(*price.AvailableAt)
				if age < 0 {
					reasons = append(reasons, "quote_not_yet_available")
				} else if age > time.Duration(sub.MaxQuoteAgeSeconds)*time.Second {
					reasons = append(reasons, "stale_quote")
				}
			}
			if price.MarketStatus != "open" {
				reasons = append(reasons, "market_not_open")
			}
			if !sessionAllowed(sub.AllowedSessions, price.SessionStatus) {
				reasons = append(reasons, "session_not_allowed")
			}
			if price.AvgDollarVolume < sub.MinAvgDollarVolume {
				reasons = append(reasons, "below_min_avg_dollar_volume")
			}
		}
		if len(reasons) > 0 {
			intent.PolicyStatus = "skipped"
			intent.PolicyReasons = reasons
			intent.Status = "skipped"
		}
		intent.Calculation = mustJSON(map[string]any{"source_weight": target.weight, "target_value": targetValue, "current_value": current, "unscaled_delta": math.Abs(delta), "cash_buffer_pct": sub.CashBufferPct, "max_position_weight": sub.MaxPositionWeight})
		result.Intents = append(result.Intents, intent)
		if intent.PolicyStatus == "approved" {
			result.Summary.DesiredTurnover += intent.RequestedNotional
		}
	}

	turnoverCap := sub.CapitalBudget * sub.MaxTurnoverPct
	result.Summary.TurnoverScale = 1
	if result.Summary.DesiredTurnover > turnoverCap && result.Summary.DesiredTurnover > 0 {
		result.Summary.TurnoverScale = turnoverCap / result.Summary.DesiredTurnover
		result.Summary.Warnings = append(result.Summary.Warnings, "turnover_cap_applied")
	}
	for i := range result.Intents {
		if result.Intents[i].PolicyStatus != "approved" {
			continue
		}
		result.Intents[i].RequestedNotional = roundMoney(result.Intents[i].RequestedNotional * result.Summary.TurnoverScale)
		result.Summary.ApprovedTurnover += result.Intents[i].RequestedNotional
	}
	result.Summary.DesiredTurnover = roundMoney(result.Summary.DesiredTurnover)
	result.Summary.ApprovedTurnover = roundMoney(result.Summary.ApprovedTurnover)
	result.Summary.TargetInvestedValue = roundMoney(result.Summary.TargetInvestedValue)
	result.Summary.TargetCashValue = roundMoney(result.Summary.TargetCashValue)
	return result
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		if value = strings.ToUpper(strings.TrimSpace(value)); value != "" {
			out[value] = true
		}
	}
	return out
}

func roundMoney(value float64) float64 { return math.Round(value*100) / 100 }

func quoteMidpoint(value PriceSnapshot) (decimal.Decimal, error) {
	bid, bidErr := decimal.NewFromString(value.Bid)
	ask, askErr := decimal.NewFromString(value.Ask)
	if bidErr != nil || askErr != nil || !bid.IsPositive() || !ask.IsPositive() || ask.LessThan(bid) {
		return decimal.Zero, fmt.Errorf("invalid quote")
	}
	return bid.Add(ask).Div(decimal.NewFromInt(2)), nil
}

func sessionAllowed(allowed []string, value string) bool {
	for _, candidate := range allowed {
		if candidate == value {
			return true
		}
	}
	return false
}

func timePointer(value time.Time) *time.Time {
	normalized := value.UTC().Truncate(time.Microsecond)
	return &normalized
}

func mustJSON(value any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}
