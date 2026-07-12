package risk

import (
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestCheckOptionsExposureEnforcesDeltaAndOffsetsSpreadLegs(t *testing.T) {
	limits := DefaultOptionsLimits()
	highDelta := []OptionLegExposure{{Greeks: domain.OptionGreeks{Delta: 0.9}, Side: domain.OrderSideBuy, Quantity: 3, Multiplier: 100}}
	_, allowed, reason := CheckOptionsExposure(limits, 100000, 100, nil, highDelta)
	if allowed || !strings.Contains(reason, "delta") {
		t.Fatalf("high delta allowed=%v reason=%q", allowed, reason)
	}
	offset := []OptionLegExposure{{Greeks: domain.OptionGreeks{Delta: 0.6, Gamma: .01, Theta: -.1, Vega: .2}, Side: domain.OrderSideBuy, Quantity: 1, Multiplier: 100}, {Greeks: domain.OptionGreeks{Delta: 0.4, Gamma: .008, Theta: -.08, Vega: .15}, Side: domain.OrderSideSell, Quantity: 1, Multiplier: 100}}
	exposure, allowed, reason := CheckOptionsExposure(limits, 100000, 100, nil, offset)
	if !allowed || reason != "" || exposure.DeltaPctEquity >= limits.MaxAbsDeltaPctEquity {
		t.Fatalf("defined-risk offset rejected: exposure=%+v reason=%q", exposure, reason)
	}
}

func TestCheckOptionsExposureIncludesPersistedGreeks(t *testing.T) {
	delta, gamma, theta, vega := 0.8, 0.01, -0.1, 0.2
	existing := []domain.Position{{AssetClass: domain.AssetClassOption, Side: domain.PositionSideLong, Quantity: 3, ContractMultiplier: 100, Delta: &delta, Gamma: &gamma, Theta: &theta, Vega: &vega}}
	_, allowed, reason := CheckOptionsExposure(DefaultOptionsLimits(), 100000, 100, existing, nil)
	if allowed || !strings.Contains(reason, "delta") {
		t.Fatalf("persisted Greek exposure allowed=%v reason=%q", allowed, reason)
	}
}
