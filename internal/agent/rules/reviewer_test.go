package rules

import (
	"context"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

type mockLLMProvider struct {
	response    string
	err         error
	nilResponse bool
}

func (m *mockLLMProvider) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.nilResponse {
		return nil, nil
	}
	return &llm.CompletionResponse{Content: m.response}, nil
}

func testState() *agent.PipelineState {
	return &agent.PipelineState{
		Ticker: "AAPL",
		Market: &agent.MarketData{
			Bars: []domain.OHLCV{
				{Close: 148, Open: 147, High: 149, Low: 146, Volume: 90000},
				{Close: 150, Open: 148, High: 152, Low: 147, Volume: 100000},
			},
			Indicators: []domain.Indicator{
				{Name: "rsi_14", Value: 28},
				{Name: "sma_200", Value: 145},
				{Name: "atr_14", Value: 3.5},
			},
		},
	}
}

func TestSignalReviewer_Confirm(t *testing.T) {
	t.Parallel()
	provider := &mockLLMProvider{
		response: `{"verdict":"confirm","confidence":0.85,"adjusted_position_size":0,"adjusted_stop_loss":0,"adjusted_take_profit":0,"holding_strategy":"Exit if the thesis breaks.","reasoning":"Signal looks solid given oversold RSI and price above SMA-200."}`,
	}
	reviewer := NewSignalReviewer(provider, "test-model", nil)
	plan := &agent.TradingPlan{
		Action: domain.PipelineSignalBuy, Ticker: "AAPL", EntryPrice: 150,
		PositionSize: 10, StopLoss: 145, TakeProfit: 160,
	}
	bar := domain.OHLCV{Close: 150, Open: 148, High: 152, Low: 147, Volume: 100000}

	ok, _ := reviewer.Review(context.Background(), plan, testState(), bar, 50000)
	if !ok {
		t.Fatal("expected confirm to return true")
	}
	if plan.Confidence != 0.85 {
		t.Errorf("confidence = %v, want 0.85", plan.Confidence)
	}
}

func TestSignalReviewer_Veto(t *testing.T) {
	t.Parallel()
	provider := &mockLLMProvider{
		response: `{"verdict":"veto","confidence":0.3,"adjusted_position_size":0,"adjusted_stop_loss":0,"adjusted_take_profit":0,"reasoning":"Price is at resistance with declining volume."}`,
	}
	reviewer := NewSignalReviewer(provider, "test-model", nil)
	plan := &agent.TradingPlan{
		Action: domain.PipelineSignalBuy, Ticker: "AAPL", EntryPrice: 150,
		PositionSize: 10, StopLoss: 145, TakeProfit: 160,
	}
	bar := domain.OHLCV{Close: 150}

	ok, _ := reviewer.Review(context.Background(), plan, testState(), bar, 50000)
	if ok {
		t.Fatal("expected veto to return false")
	}
}

func TestSignalReviewer_Modify(t *testing.T) {
	t.Parallel()
	provider := &mockLLMProvider{
		response: `{"verdict":"modify","confidence":0.7,"adjusted_position_size":5,"adjusted_stop_loss":143,"adjusted_take_profit":162,"holding_strategy":"Exit below support.","reasoning":"Reduce size, tighten stop to recent support at 143."}`,
	}
	reviewer := NewSignalReviewer(provider, "test-model", nil)
	plan := &agent.TradingPlan{
		Action: domain.PipelineSignalBuy, Ticker: "AAPL", EntryPrice: 150,
		PositionSize: 10, StopLoss: 145, TakeProfit: 160, Rationale: "Rules signal",
	}
	bar := domain.OHLCV{Close: 150}

	ok, _ := reviewer.Review(context.Background(), plan, testState(), bar, 50000)
	if !ok {
		t.Fatal("expected modify to return true")
	}
	if plan.PositionSize != 5 {
		t.Errorf("position size = %v, want 5", plan.PositionSize)
	}
	if plan.StopLoss != 143 {
		t.Errorf("stop loss = %v, want 143", plan.StopLoss)
	}
	if plan.TakeProfit != 162 {
		t.Errorf("take profit = %v, want 162", plan.TakeProfit)
	}
}

func TestSignalReviewer_IncompleteReviewVetoesEntry(t *testing.T) {
	t.Parallel()
	for name, provider := range map[string]llm.Provider{
		"provider error":   &mockLLMProvider{err: context.DeadlineExceeded},
		"malformed":        &mockLLMProvider{response: `not-json`},
		"unknown verdict":  &mockLLMProvider{response: `{"verdict":"maybe","confidence":0.8,"reasoning":"uncertain","holding_strategy":"hold"}`},
		"missing strategy": &mockLLMProvider{response: `{"verdict":"confirm","confidence":0.8,"reasoning":"looks good"}`},
		"unknown field":    &mockLLMProvider{response: `{"verdict":"confirm","confidence":0.8,"holding_strategy":"hold","reasoning":"looks good","override":true}`},
	} {
		t.Run(name, func(t *testing.T) {
			reviewer := NewSignalReviewer(provider, "test-model", nil)
			plan := &agent.TradingPlan{Action: domain.PipelineSignalBuy, Ticker: "AAPL", EntryPrice: 150, PositionSize: 10}
			if ok, _ := reviewer.Review(context.Background(), plan, testState(), domain.OHLCV{Close: 150}, 50000); ok {
				t.Fatal("incomplete review must veto entry")
			}
		})
	}
}

func TestSignalReviewer_IncompleteExitReviewConfirmsExit(t *testing.T) {
	t.Parallel()

	tests := map[string]llm.Provider{
		"nil reviewer provider": nil,
		"nil response":          &mockLLMProvider{nilResponse: true},
		"unknown field":         &mockLLMProvider{response: `{"verdict":"veto","confidence":0.8,"reasoning":"hold","override":true}`},
		"invalid confidence":    &mockLLMProvider{response: `{"verdict":"veto","confidence":2,"reasoning":"hold"}`},
		"missing reasoning":     &mockLLMProvider{response: `{"verdict":"veto","confidence":0.8}`},
	}
	for name, provider := range tests {
		name, provider := name, provider
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reviewer := NewSignalReviewer(provider, "test-model", nil)
			if closePosition, _ := reviewer.ReviewExit(context.Background(), &OpenPosition{Ticker: "AAPL"}, testState(), domain.OHLCV{Close: 150}, 50000); !closePosition {
				t.Fatal("incomplete exit review must confirm exposure reduction")
			}
		})
	}
}
