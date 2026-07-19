package llm

import "fmt"

// PromptTooLargeError reports a prompt body that exceeds a byte ceiling.
type PromptTooLargeError struct {
	ActualBytes int
	MaxBytes    int
}

func (e *PromptTooLargeError) Error() string {
	return fmt.Sprintf("llm: prompt is %d bytes; maximum is %d", e.ActualBytes, e.MaxBytes)
}

// PromptBytes returns the UTF-8 byte size of all message contents.
func PromptBytes(messages []Message) int {
	total := 0
	for _, message := range messages {
		total += len(message.Content)
	}
	return total
}

// ValidatePromptBytes rejects prompts that exceed maxBytes.
func ValidatePromptBytes(messages []Message, maxBytes int) error {
	actual := PromptBytes(messages)
	if maxBytes > 0 && actual > maxBytes {
		return &PromptTooLargeError{ActualBytes: actual, MaxBytes: maxBytes}
	}
	return nil
}
