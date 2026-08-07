package execution_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type recordingOptionSettlementRepo struct {
	inputs []repository.OptionPositionSettlementInput
	err    error
}

func (r *recordingOptionSettlementRepo) SettleOptionPosition(_ context.Context, input repository.OptionPositionSettlementInput) (repository.OptionPositionSettlementResult, error) {
	if r.err != nil {
		return repository.OptionPositionSettlementResult{}, r.err
	}
	r.inputs = append(r.inputs, input)
	return repository.OptionPositionSettlementResult{PositionID: input.PositionID, TradeID: uuid.New()}, nil
}

func expiryPosition(symbol, underlying string, optionType domain.OptionType, strike, entry, quantity float64, side domain.PositionSide, expiry time.Time) domain.Position {
	return domain.Position{ID: uuid.New(), Ticker: symbol, Side: side, Quantity: quantity, AvgEntry: entry, AssetClass: domain.AssetClassOption, UnderlyingTicker: underlying, OptionType: &optionType, Strike: &strike, Expiry: &expiry, ContractMultiplier: 100}
}

func TestSettleExpiredOptionPositionsPersistsExerciseAndWorthlessExpiryWithoutFabricatingAssignment(t *testing.T) {
	now := time.Date(2027, 12, 18, 22, 0, 0, 0, time.UTC)
	expiry := time.Date(2027, 12, 17, 0, 0, 0, 0, time.UTC)
	positions := []domain.Position{
		expiryPosition("AAPL271217C00150000", "AAPL", domain.OptionTypeCall, 150, 2, 1, domain.PositionSideLong, expiry),
		expiryPosition("AAPL271217P00140000", "AAPL", domain.OptionTypePut, 140, 1, 2, domain.PositionSideLong, expiry),
	}
	settlementRepo := &recordingOptionSettlementRepo{}
	summary, err := execution.SettleExpiredOptionPositions(context.Background(), positions, map[string]float64{"AAPL": 155}, now, settlementRepo)
	if err != nil {
		t.Fatalf("SettleExpiredOptionPositions() error = %v", err)
	}
	if summary.CashSettled != 1 || summary.ExpiredWorthless != 1 || len(settlementRepo.inputs) != 2 {
		t.Fatalf("unexpected settlement summary=%+v atomic_calls=%d", summary, len(settlementRepo.inputs))
	}
	if math.Abs(settlementRepo.inputs[0].SettlementPrice-5) > 1e-9 || settlementRepo.inputs[0].ExitReason != "exercise_cash_settled" {
		t.Fatalf("ITM settlement incorrect: %+v", settlementRepo.inputs[0])
	}
	if settlementRepo.inputs[1].SettlementPrice != 0 || settlementRepo.inputs[1].ExitReason != "expired_worthless" {
		t.Fatalf("OTM settlement incorrect: %+v", settlementRepo.inputs[1])
	}
}

func TestSettleExpiredOptionPositionsValidatesBatchBeforeMutation(t *testing.T) {
	now := time.Date(2027, 12, 18, 22, 0, 0, 0, time.UTC)
	expiry := now.Add(-24 * time.Hour)
	positions := []domain.Position{expiryPosition("AAPL271217C00150000", "AAPL", domain.OptionTypeCall, 150, 2, 1, domain.PositionSideLong, expiry), expiryPosition("MSFT271217C00300000", "MSFT", domain.OptionTypeCall, 300, 2, 1, domain.PositionSideLong, expiry)}
	settlementRepo := &recordingOptionSettlementRepo{}
	_, err := execution.SettleExpiredOptionPositions(context.Background(), positions, map[string]float64{"AAPL": 155}, now, settlementRepo)
	if err == nil || len(settlementRepo.inputs) != 0 {
		t.Fatalf("invalid batch must fail before mutation: err=%v atomic_calls=%d", err, len(settlementRepo.inputs))
	}
}
