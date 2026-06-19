package eventmarkets

import "github.com/PatrickFanella/get-rich-quick/internal/domain"

func IsEventMarket(marketType domain.MarketType) bool {
	switch marketType.Normalize() {
	case domain.MarketTypePolymarket, domain.MarketTypeKalshi:
		return true
	default:
		return false
	}
}

func SupportsOHLCVResweep(marketType domain.MarketType) bool {
	switch marketType.Normalize() {
	case domain.MarketTypeStock, domain.MarketTypeCrypto:
		return true
	default:
		return false
	}
}
