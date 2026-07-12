package main

import (
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func runtimeOptionSnapshot(symbol string, optionType domain.OptionType, delta, bid, ask float64, expiry time.Time) domain.OptionSnapshot {
	contract, err := domain.ParseOCC(symbol)
	if err != nil {
		panic(err)
	}
	contract.OptionType = optionType
	contract.Expiry = expiry
	return domain.OptionSnapshot{Contract: *contract, Greeks: domain.OptionGreeks{Delta: delta, IV: 0.25}, Bid: bid, Ask: ask, OpenInterest: 100, Volume: 20}
}

func TestBuildPaperSingleLegPlanSelectsExecutableContract(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	expiry := time.Date(2026, 12, 18, 0, 0, 0, 0, time.UTC)
	cfg := &rules.OptionsRulesConfig{LegSelection: map[string]rules.LegSelector{"long_call": {OptionType: domain.OptionTypeCall, DeltaTarget: 0.4, DTEMin: 30, DTEMax: 60, Side: domain.OrderSideBuy, Intent: domain.PositionIntentBuyToOpen, Ratio: 1}}, PositionSizing: rules.OptionsSizingConfig{Method: "premium_budget", PremiumBudget: 1000}}
	chain := []domain.OptionSnapshot{
		runtimeOptionSnapshot("AAPL261218C00150000", domain.OptionTypeCall, 0.7, 4.8, 5.0, expiry),
		runtimeOptionSnapshot("AAPL261218C00160000", domain.OptionTypeCall, 0.41, 2.4, 2.5, expiry),
	}
	plan, err := buildPaperSingleLegPlan(cfg, chain, now)
	if err != nil {
		t.Fatalf("buildPaperSingleLegPlan() error = %v", err)
	}
	if plan.Ticker != "AAPL261218C00160000" || plan.EntryPrice != 2.5 || plan.PositionSize != 4 || plan.EntryType != "limit" {
		t.Fatalf("unexpected options plan: %+v", plan)
	}
}

func TestBuildPaperSingleLegPlanFailsClosed(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	expiry := time.Date(2026, 12, 18, 0, 0, 0, 0, time.UTC)
	snapshot := runtimeOptionSnapshot("AAPL261218C00160000", domain.OptionTypeCall, 0.4, 2.4, 2.5, expiry)
	tests := []struct {
		name string
		cfg  *rules.OptionsRulesConfig
		want string
	}{
		{"spread", &rules.OptionsRulesConfig{LegSelection: map[string]rules.LegSelector{"one": {}, "two": {}}}, "single-leg"},
		{"uncovered short", &rules.OptionsRulesConfig{LegSelection: map[string]rules.LegSelector{"short": {OptionType: domain.OptionTypeCall, DeltaTarget: 0.4, DTEMin: 30, DTEMax: 60, Side: domain.OrderSideSell, Intent: domain.PositionIntentSellToOpen}}, PositionSizing: rules.OptionsSizingConfig{Method: "fixed_contracts", FixedContracts: 1}}, "short options"},
		{"insufficient budget", &rules.OptionsRulesConfig{LegSelection: map[string]rules.LegSelector{"long": {OptionType: domain.OptionTypeCall, DeltaTarget: 0.4, DTEMin: 30, DTEMax: 60, Side: domain.OrderSideBuy, Intent: domain.PositionIntentBuyToOpen}}, PositionSizing: rules.OptionsSizingConfig{Method: "premium_budget", PremiumBudget: 100}}, "one contract"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := buildPaperSingleLegPlan(test.cfg, []domain.OptionSnapshot{snapshot}, now)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestExecutableOptionClosePriceUsesBidForLongAndAskForShort(t *testing.T) {
	expiry := time.Date(2026, 12, 18, 0, 0, 0, 0, time.UTC)
	chain := []domain.OptionSnapshot{runtimeOptionSnapshot("AAPL261218C00160000", domain.OptionTypeCall, 0.4, 3.1, 3.3, expiry)}
	position := &domain.Position{Ticker: "AAPL261218C00160000", Side: domain.PositionSideLong}
	price, err := executableOptionClosePrice(position, chain)
	if err != nil || price != 3.1 {
		t.Fatalf("long close price = %v, err=%v", price, err)
	}
	position.Side = domain.PositionSideShort
	price, err = executableOptionClosePrice(position, chain)
	if err != nil || price != 3.3 {
		t.Fatalf("short close price = %v, err=%v", price, err)
	}
}

func TestBuildPaperDebitSpreadPlanUsesExecutableSides(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	expiry := time.Date(2026, 12, 18, 0, 0, 0, 0, time.UTC)
	cfg := &rules.OptionsRulesConfig{StrategyType: domain.StrategyBullCallSpread, Underlying: "AAPL", LegSelection: map[string]rules.LegSelector{
		"long":  {OptionType: domain.OptionTypeCall, DeltaTarget: 0.6, DTEMin: 30, DTEMax: 60, Side: domain.OrderSideBuy, Intent: domain.PositionIntentBuyToOpen, Ratio: 1},
		"short": {OptionType: domain.OptionTypeCall, DeltaTarget: 0.3, DTEMin: 30, DTEMax: 60, Side: domain.OrderSideSell, Intent: domain.PositionIntentSellToOpen, Ratio: 1},
	}, PositionSizing: rules.OptionsSizingConfig{Method: "max_risk", MaxRiskUSD: 1000}}
	chain := []domain.OptionSnapshot{runtimeOptionSnapshot("AAPL261218C00150000", domain.OptionTypeCall, 0.61, 4.8, 5, expiry), runtimeOptionSnapshot("AAPL261218C00155000", domain.OptionTypeCall, 0.31, 2, 2.2, expiry)}
	spread, quantity, err := buildPaperDebitSpreadPlan(cfg, chain, now)
	if err != nil {
		t.Fatalf("buildPaperDebitSpreadPlan() error = %v", err)
	}
	if quantity != 3 || spread.MaxRisk != 300 || spread.MaxReward != 200 {
		t.Fatalf("unexpected spread sizing: quantity=%v spread=%+v", quantity, spread)
	}
}
