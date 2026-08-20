package qualification

import (
	"github.com/PatrickFanella/get-rich-quick/internal/makerquote"
	"github.com/PatrickFanella/get-rich-quick/internal/predictionreplay"
	predictionqualification "github.com/PatrickFanella/get-rich-quick/internal/predictionreplay/qualification"
)

type Fixture struct {
	RecorderInput predictionreplay.Input
}

func Build() (Fixture, error) {
	input, err := predictionqualification.Build()
	return Fixture{RecorderInput: input}, err
}

func (Fixture) CandidateInput(recorder *predictionreplay.Recorder, key, minimum, rate string) makerquote.Input {
	return makerquote.Input{Recorder: recorder, CandidateKey: key, MarketID: "market-1", OutcomeID: predictionqualification.OutcomeNo, Side: predictionreplay.SideSell, DecisionAt: predictionqualification.At(3, 14), QuotePrice: "0.58", QuoteQuantity: "5", PriorQueue: "0", StartingInventory: "0", InventoryLimit: "10", HourlyInventoryCostRate: rate, MinimumExpectedNet: minimum, Scenarios: []makerquote.ScenarioInput{
		{Key: "no-fill", Weight: "0.25", HorizonAt: predictionqualification.At(3, 15), QueueOutflow: "10"},
		{Key: "full-fill", Weight: "0.75", HorizonAt: predictionqualification.At(3, 15), QueueOutflow: "15"},
	}}
}
