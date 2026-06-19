package eventmarkets

import (
	"encoding/json"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type BuildPaperStrategyInput struct {
	Provider     domain.MarketType
	Name         string
	Ticker       string
	ScheduleCron string
	ConfigJSON   json.RawMessage
}

func ReuseByTickerOnly(marketType domain.MarketType) bool {
	return IsEventMarket(marketType)
}

func BuildPaperStrategy(input BuildPaperStrategyInput) domain.Strategy {
	return domain.Strategy{
		Name:         input.Name,
		Ticker:       input.Ticker,
		MarketType:   input.Provider.Normalize(),
		IsPaper:      true,
		Status:       domain.StrategyStatusActive,
		ScheduleCron: input.ScheduleCron,
		Config:       append(json.RawMessage(nil), input.ConfigJSON...),
	}
}
