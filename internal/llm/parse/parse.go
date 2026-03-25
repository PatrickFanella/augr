package parse

import (
	"encoding/json"
	"fmt"
	"strings"
)

// StripCodeFences removes optional markdown code fences (```json ... ``` or ``` ... ```)
// from LLM response content so the JSON can be parsed cleanly. It handles both
// fences with a newline after the opening tag and inline fences where the JSON
// starts on the same line.
func StripCodeFences(s string) string {
	trimmed := strings.TrimSpace(s)

	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}

	// Remove the closing fence if it appears at the end.
	body := trimmed
	if idx := strings.LastIndex(body, "```"); idx > 2 {
		body = body[:idx]
	}

	// Remove the opening fence. When a newline follows the fence line we
	// strip everything up to and including that newline. Otherwise we look
	// for the first '{' or '[' that starts the JSON payload on the same
	// line (inline fence).
	if idx := strings.Index(body, "\n"); idx != -1 {
		body = body[idx+1:]
	} else if idx := strings.IndexAny(body, "{["); idx != -1 {
		body = body[idx:]
	}

	return strings.TrimSpace(body)
}

// Parse strips code fences from content, unmarshals the JSON into T, and
// runs the supplied validation function. It returns the parsed value or a
// descriptive error.
func Parse[T any](content string, validate func(*T) error) (*T, error) {
	cleaned := StripCodeFences(content)

	var result T
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if validate != nil {
		if err := validate(&result); err != nil {
			return nil, err
		}
	}

	return &result, nil
}
