package agent

import (
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// PromptTextFromMessages concatenates completion message content in request
// order so the full prompt sent to the LLM can be persisted for observability.
func PromptTextFromMessages(messages []llm.Message) string {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		if strings.TrimSpace(message.Content) == "" {
			continue
		}
		parts = append(parts, message.Content)
	}

	return strings.Join(parts, "\n\n")
}
