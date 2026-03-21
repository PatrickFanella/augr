package llm

import "context"

// Provider defines the minimal interface required to execute an LLM completion.
type Provider interface {
	Complete(ctx context.Context, request CompletionRequest) (*CompletionResponse, error)
}
