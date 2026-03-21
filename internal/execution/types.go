package execution

import "github.com/PatrickFanella/get-rich-quick/internal/domain"

// Balance captures the common account balance fields shared across markets.
type Balance struct {
	Currency    string  `json:"currency"`
	Cash        float64 `json:"cash"`
	BuyingPower float64 `json:"buying_power"`
	Equity      float64 `json:"equity"`
}

// AccountInfo captures shared broker account metadata.
type AccountInfo struct {
	AccountID  string            `json:"account_id"`
	Broker     string            `json:"broker"`
	MarketType domain.MarketType `json:"market_type"`
	IsPaper    bool              `json:"is_paper"`
	Balance    Balance           `json:"balance"`
}
