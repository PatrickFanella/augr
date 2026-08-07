package execution

import (
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type OptionsReconciliation struct {
	OptionOrders    int
	OptionPositions int
	OptionTrades    int
	LegGroups       int
	Findings        []string
}

func (r OptionsReconciliation) Healthy() bool { return len(r.Findings) == 0 }

// ReconcileOptionsLifecycle checks the durable order-position-trade graph. It
// reports inconsistencies but does not invent repairs or broker state.
func ReconcileOptionsLifecycle(orders []domain.Order, positions []domain.Position, trades []domain.Trade) OptionsReconciliation {
	result := OptionsReconciliation{}
	tradesByOrder, openByPosition, closeByPosition := map[uuid.UUID]int{}, map[uuid.UUID]int{}, map[uuid.UUID]int{}
	for _, trade := range trades {
		if trade.AssetClass != domain.AssetClassOption {
			continue
		}
		result.OptionTrades++
		if trade.OrderID != nil {
			tradesByOrder[*trade.OrderID]++
		}
		if trade.PositionID != nil {
			if trade.OpenClose == "open" {
				openByPosition[*trade.PositionID]++
			}
			if trade.OpenClose == "close" {
				closeByPosition[*trade.PositionID]++
			}
		}
	}
	groups := map[uuid.UUID][]domain.Position{}
	for _, order := range orders {
		marketOption := order.MarketType.Normalize() == domain.MarketTypeOptions
		assetOption := order.AssetClass == domain.AssetClassOption
		if !marketOption && !assetOption {
			continue
		}
		result.OptionOrders++
		if marketOption != assetOption {
			result.Findings = append(result.Findings, fmt.Sprintf("option order %s has inconsistent market and asset classification", order.ID))
			continue
		}
		if order.Status == domain.OrderStatusFilled && tradesByOrder[order.ID] == 0 {
			result.Findings = append(result.Findings, fmt.Sprintf("filled option order %s has no trade", order.ID))
		}
	}
	for _, position := range positions {
		if position.AssetClass != domain.AssetClassOption {
			continue
		}
		result.OptionPositions++
		if openByPosition[position.ID] == 0 {
			result.Findings = append(result.Findings, fmt.Sprintf("option position %s has no opening trade", position.ID))
		}
		if position.ClosedAt != nil && closeByPosition[position.ID] == 0 {
			result.Findings = append(result.Findings, fmt.Sprintf("closed option position %s has no closing trade", position.ID))
		}
		if position.ClosedAt == nil && closeByPosition[position.ID] > 0 {
			result.Findings = append(result.Findings, fmt.Sprintf("open option position %s already has a closing trade", position.ID))
		}
		if position.LegGroupID != nil {
			groups[*position.LegGroupID] = append(groups[*position.LegGroupID], position)
		}
	}
	result.LegGroups = len(groups)
	for groupID, legs := range groups {
		if len(legs) < 2 {
			result.Findings = append(result.Findings, fmt.Sprintf("option leg group %s has only %d persisted leg", groupID, len(legs)))
			continue
		}
		closed := legs[0].ClosedAt != nil
		for _, leg := range legs[1:] {
			if (leg.ClosedAt != nil) != closed {
				result.Findings = append(result.Findings, fmt.Sprintf("option leg group %s has mixed open and closed legs", groupID))
				break
			}
		}
	}
	sort.Strings(result.Findings)
	return result
}
