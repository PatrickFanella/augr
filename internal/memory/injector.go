package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// Injector retrieves relevant memories and injects them into LLM messages.
type Injector struct {
	memoryRepo repository.MemoryRepository
	logger     *slog.Logger
}

// NewInjector creates an Injector with the given dependencies.
// A nil logger is replaced with slog.Default().
func NewInjector(memoryRepo repository.MemoryRepository, logger *slog.Logger) *Injector {
	if logger == nil {
		logger = slog.Default()
	}

	return &Injector{
		memoryRepo: memoryRepo,
		logger:     logger,
	}
}

// GetMemoryContext retrieves relevant memories for the given role and ticker and
// formats them for inclusion in an agent prompt.
func (i *Injector) GetMemoryContext(ctx context.Context, role domain.AgentRole, ticker string, limit int) (string, error) {
	ticker = strings.TrimSpace(ticker)
	if ticker == "" || limit <= 0 {
		return "", nil
	}

	memories, err := i.memoryRepo.Search(ctx, ticker, repository.MemorySearchFilter{
		AgentRole: role,
	}, limit, 0)
	if err != nil {
		return "", fmt.Errorf("search memories for role %s ticker %q limit %d: %w", role, ticker, limit, err)
	}

	if len(memories) == 0 {
		return "", nil
	}

	var builder strings.Builder
	builder.WriteString("## Relevant Past Experience")
	for _, memory := range memories {
		builder.WriteString("\n- Situation: ")
		builder.WriteString(memory.Situation)
		builder.WriteString(", Lesson: ")
		builder.WriteString(memory.Recommendation)
	}

	return builder.String(), nil
}

// InjectIntoMessages appends memory context to the first system message when
// memory context is available.
func (i *Injector) InjectIntoMessages(messages []llm.Message, memoryContext string) []llm.Message {
	if memoryContext == "" {
		return messages
	}

	systemIdx := -1
	for idx := range messages {
		if messages[idx].Role != "system" {
			continue
		}
		systemIdx = idx
		break
	}

	if systemIdx == -1 {
		i.logger.Warn("memory injector: no system message found; skipping memory injection")
		return messages
	}

	out := append([]llm.Message(nil), messages...)
	if out[systemIdx].Content == "" {
		out[systemIdx].Content = memoryContext
	} else {
		out[systemIdx].Content += "\n\n" + memoryContext
	}

	return out
}
