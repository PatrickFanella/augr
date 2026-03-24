package risk

import (
	"context"
	"sync/atomic"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// mockProvider is a test double for llm.Provider shared across risk agent tests.
type mockProvider struct {
	response *llm.CompletionResponse
	err      error
	calls    atomic.Int32
	lastReq  llm.CompletionRequest
}

func (m *mockProvider) Complete(_ context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.calls.Add(1)
	m.lastReq = req
	return m.response, m.err
}
