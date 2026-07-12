package execution_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
)

func expiryPosition(symbol, underlying string, optionType domain.OptionType, strike, entry, quantity float64, side domain.PositionSide, expiry time.Time) domain.Position {
	return domain.Position{ID: uuid.New(), Ticker: symbol, Side: side, Quantity: quantity, AvgEntry: entry, AssetClass: domain.AssetClassOption, UnderlyingTicker: underlying, OptionType: &optionType, Strike: &strike, Expiry: &expiry, ContractMultiplier: 100}
}

func TestSettleExpiredOptionPositionsPersistsExerciseAndWorthlessExpiry(t *testing.T) {
	now := time.Date(2027, 12, 18, 22, 0, 0, 0, time.UTC)
	expiry := time.Date(2027, 12, 17, 0, 0, 0, 0, time.UTC)
	positions := []domain.Position{
		expiryPosition("AAPL271217C00150000", "AAPL", domain.OptionTypeCall, 150, 2, 1, domain.PositionSideLong, expiry),
		expiryPosition("AAPL271217P00140000", "AAPL", domain.OptionTypePut, 140, 1, 2, domain.PositionSideLong, expiry),
	}
	positionRepo, tradeRepo := &mockPositionRepo{}, &mockTradeRepo{}
	summary, err := execution.SettleExpiredOptionPositions(context.Background(), positions, map[string]float64{"AAPL": 155}, now, positionRepo, tradeRepo)
	if err != nil {
		t.Fatalf("SettleExpiredOptionPositions() error = %v", err)
	}
	if summary.CashSettled != 1 || summary.ExpiredWorthless != 1 || len(positionRepo.updates) != 2 || len(tradeRepo.trades) != 2 {
		t.Fatalf("unexpected settlement summary=%+v updates=%d trades=%d", summary, len(positionRepo.updates), len(tradeRepo.trades))
	}
	if math.Abs(positionRepo.updates[0].RealizedPnL-300) > 1e-9 || tradeRepo.trades[0].ExitReason != "exercise_cash_settled" || tradeRepo.trades[0].Premium != 500 {
		t.Fatalf("ITM settlement incorrect: position=%+v trade=%+v", positionRepo.updates[0], tradeRepo.trades[0])
	}
	if math.Abs(positionRepo.updates[1].RealizedPnL-(-200)) > 1e-9 || tradeRepo.trades[1].ExitReason != "expired_worthless" {
		t.Fatalf("OTM settlement incorrect: position=%+v trade=%+v", positionRepo.updates[1], tradeRepo.trades[1])
	}
}

func TestSettleExpiredOptionPositionsValidatesBatchBeforeMutation(t *testing.T) {
	now := time.Date(2027, 12, 18, 22, 0, 0, 0, time.UTC)
	expiry := now.Add(-24 * time.Hour)
	positions := []domain.Position{expiryPosition("AAPL271217C00150000", "AAPL", domain.OptionTypeCall, 150, 2, 1, domain.PositionSideLong, expiry), expiryPosition("MSFT271217C00300000", "MSFT", domain.OptionTypeCall, 300, 2, 1, domain.PositionSideLong, expiry)}
	positionRepo, tradeRepo := &mockPositionRepo{}, &mockTradeRepo{}
	_, err := execution.SettleExpiredOptionPositions(context.Background(), positions, map[string]float64{"AAPL": 155}, now, positionRepo, tradeRepo)
	if err == nil || len(positionRepo.updates) != 0 || len(tradeRepo.trades) != 0 {
		t.Fatalf("invalid batch must fail before mutation: err=%v updates=%d trades=%d", err, len(positionRepo.updates), len(tradeRepo.trades))
	}
}
