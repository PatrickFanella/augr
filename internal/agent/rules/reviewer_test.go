package rules

import (
	"context"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

type mockLLMProvider struct {
	response string
	err      error
}

func (m *mockLLMProvider) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &llm.CompletionResponse{Content: m.response}, nil
}

func TestSignalReviewer_Confirm(t *testing.T) {
	t.Parallel()
	provider := &mockLLMProvider{
		response: `{"verdict":"confirm","confidence":0.85,"adjusted_position_size":0,"adjusted_stop_loss":0,"reasoning":"Signal looks solid."}`,
	}
	reviewer := NewSignalReviewer(provider, "test-model", nil)
	plan := &agent.TradingPlan{
		Action: domain.PipelineSignalBuy, Ticker: "AAPL", EntryPrice: 150,
		PositionSize: 10, StopLoss: 145, TakeProfit: 160,
	}
	bar := domain.OHLCV{Close: 150, Open: 148, High: 152, Low: 147, Volume: 100000}

	ok := reviewer.Review(context.Background(), plan, nil, bar)
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
		response: `{"verdict":"veto","confidence":0.3,"adjusted_position_size":0,"adjusted_stop_loss":0,"reasoning":"Overbought conditions."}`,
	}
	reviewer := NewSignalReviewer(provider, "test-model", nil)
	plan := &agent.TradingPlan{
		Action: domain.PipelineSignalBuy, Ticker: "AAPL", EntryPrice: 150,
		PositionSize: 10, StopLoss: 145, TakeProfit: 160,
	}
	bar := domain.OHLCV{Close: 150}

	ok := reviewer.Review(context.Background(), plan, nil, bar)
	if ok {
		t.Fatal("expected veto to return false")
	}
}

func TestSignalReviewer_Modify(t *testing.T) {
	t.Parallel()
	provider := &mockLLMProvider{
		response: `{"verdict":"modify","confidence":0.7,"adjusted_position_size":5,"adjusted_stop_loss":143,"reasoning":"Reduce size."}`,
	}
	reviewer := NewSignalReviewer(provider, "test-model", nil)
	plan := &agent.TradingPlan{
		Action: domain.PipelineSignalBuy, Ticker: "AAPL", EntryPrice: 150,
		PositionSize: 10, StopLoss: 145, TakeProfit: 160, Rationale: "Rules signal",
	}
	bar := domain.OHLCV{Close: 150}

	ok := reviewer.Review(context.Background(), plan, nil, bar)
	if !ok {
		t.Fatal("expected modify to return true")
	}
	if plan.PositionSize != 5 {
		t.Errorf("position size = %v, want 5", plan.PositionSize)
	}
	if plan.StopLoss != 143 {
		t.Errorf("stop loss = %v, want 143", plan.StopLoss)
	}
}

func TestSignalReviewer_LLMErrorConfirmsByDefault(t *testing.T) {
	t.Parallel()
	provider := &mockLLMProvider{err: context.DeadlineExceeded}
	reviewer := NewSignalReviewer(provider, "test-model", nil)
	plan := &agent.TradingPlan{
		Action: domain.PipelineSignalBuy, Ticker: "AAPL", EntryPrice: 150,
		PositionSize: 10,
	}
	bar := domain.OHLCV{Close: 150}

	ok := reviewer.Review(context.Background(), plan, nil, bar)
	if !ok {
		t.Fatal("expected LLM error to confirm by default")
	}
}
