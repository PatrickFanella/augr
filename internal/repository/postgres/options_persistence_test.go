package postgres

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type optionPersistenceScanner []any

func (s optionPersistenceScanner) Scan(dest ...any) error {
	for index, value := range s {
		if value == nil {
			continue
		}
		reflect.ValueOf(dest[index]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}

func TestOptionsPersistenceSelectsContractFields(t *testing.T) {
	for name, query := range map[string]string{"orders": orderSelectSQL, "positions": positionSelectSQL, "trades": tradeSelectSQL} {
		for _, column := range []string{"asset_class", "contract_multiplier"} {
			if !strings.Contains(query, column) {
				t.Fatalf("%s select omits %s", name, column)
			}
		}
	}
	for _, nullablePredictionColumn := range []string{
		"COALESCE(prediction_side, '')",
		"COALESCE(polymarket_intent, '')",
	} {
		if !strings.Contains(orderSelectSQL, nullablePredictionColumn) {
			t.Fatalf("orders select does not normalize legacy NULL: %s", nullablePredictionColumn)
		}
	}
}

func TestScanOrderRestoresOptionContract(t *testing.T) {
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	expiry := time.Date(2027, 12, 17, 0, 0, 0, 0, time.UTC)
	optionType, strike, intent, group := domain.OptionTypeCall, 150.0, domain.PositionIntentBuyToOpen, uuid.New()
	order, err := scanOrder(optionPersistenceScanner{uuid.New(), (*uuid.UUID)(nil), (*uuid.UUID)(nil), (*string)(nil), "AAPL271217C00150000", stringPtr("options"), domain.OrderSideBuy, domain.OrderTypeLimit, 1.0, (*float64)(nil), (*float64)(nil), 1.0, (*float64)(nil), domain.OrderStatusFilled, (*string)(nil), (*time.Time)(nil), (*time.Time)(nil), now, domain.AssetClassOption, stringPtr("AAPL"), &optionType, &strike, &expiry, 100.0, &intent, &group})
	if err != nil {
		t.Fatalf("scanOrder() error = %v", err)
	}
	if order.UnderlyingTicker != "AAPL" || order.OptionType == nil || *order.OptionType != optionType || order.LegGroupID == nil || *order.LegGroupID != group {
		t.Fatalf("option order metadata lost: %+v", order)
	}
}

func TestScanPositionAndTradeRestoreOptionLifecycle(t *testing.T) {
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	expiry := time.Date(2027, 12, 17, 0, 0, 0, 0, time.UTC)
	optionType, strike, group, delta := domain.OptionTypeCall, 150.0, uuid.New(), 0.4
	position, err := scanPosition(optionPersistenceScanner{uuid.New(), (*uuid.UUID)(nil), stringPtr("options"), "AAPL271217C00150000", domain.PositionSideLong, 1.0, 2.5, (*float64)(nil), (*float64)(nil), 0.0, (*float64)(nil), (*float64)(nil), now, (*time.Time)(nil), domain.AssetClassOption, stringPtr("AAPL"), &optionType, &strike, &expiry, 100.0, &group, &delta, (*float64)(nil), (*float64)(nil), (*float64)(nil)})
	if err != nil || position.UnderlyingTicker != "AAPL" || position.Delta == nil || *position.Delta != delta {
		t.Fatalf("option position metadata lost: position=%+v err=%v", position, err)
	}
	trade, err := scanTrade(optionPersistenceScanner{uuid.New(), (*string)(nil), (*uuid.UUID)(nil), (*uuid.UUID)(nil), "AAPL271217C00150000", domain.OrderSideBuy, 1.0, 2.5, 0.65, now, now, domain.AssetClassOption, stringPtr("open"), 100.0, 250.0})
	if err != nil || trade.OpenClose != "open" || trade.Premium != 250 || trade.ContractMultiplier != 100 {
		t.Fatalf("option trade metadata lost: trade=%+v err=%v", trade, err)
	}
}
