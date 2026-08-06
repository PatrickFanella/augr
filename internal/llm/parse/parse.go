package parse

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// thinkRegexp matches Qwen3-style <think>...</think> reasoning blocks that
// appear before the actual response content.
var (
	thinkRegexp        = regexp.MustCompile(`(?s)<think>.*?</think>`)
	thinkCaptureRegexp = regexp.MustCompile(`(?s)<think>(.*?)</think>`)
)

// StripThinkingTags removes <think>...</think> blocks emitted by models like
// Qwen3 that use an explicit reasoning phase before the response.
func StripThinkingTags(s string) string {
	return strings.TrimSpace(thinkRegexp.ReplaceAllString(s, ""))
}

// ExtractContent strips thinking tags and returns the remaining content.
// If the content outside <think> blocks is empty (the model put its answer
// inside the thinking block), it falls back to extracting the last JSON
// object or array found inside the <think> block. This handles Qwen3's
// behaviour of embedding the final answer in the reasoning trace when
// /no_think is not honoured.
func ExtractContent(s string) string {
	outside := strings.TrimSpace(thinkRegexp.ReplaceAllString(s, ""))
	if outside != "" {
		return outside
	}
	// Try to salvage JSON from inside the think block.
	m := thinkCaptureRegexp.FindStringSubmatch(s)
	if len(m) < 2 {
		return outside
	}
	inner := m[1]
	// Find the last '{' or '[' that starts a JSON value.
	lastBrace := strings.LastIndex(inner, "{")
	lastBracket := strings.LastIndex(inner, "[")
	start := lastBrace
	if lastBracket > start {
		start = lastBracket
	}
	if start == -1 {
		return outside
	}
	return strings.TrimSpace(inner[start:])
}

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

// ExtractJSONObject returns the first complete JSON object in model output.
// It tolerates prose and markdown before or after the object, while correctly
// ignoring braces that appear inside JSON strings. Arrays and incomplete
// objects are rejected because callers use this helper for object schemas.
func ExtractJSONObject(content string) (string, error) {
	cleaned := StripCodeFences(StripThinkingTags(content))
	start := strings.IndexByte(cleaned, '{')
	if start == -1 {
		return "", errors.New("failed to extract JSON object: opening brace not found")
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(cleaned); i++ {
		char := cleaned[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch char {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		switch char {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return cleaned[start : i+1], nil
			}
			if depth < 0 {
				return "", errors.New("failed to extract JSON object: unexpected closing brace")
			}
		}
	}

	return "", errors.New("failed to extract JSON object: incomplete object")
}

// Parse strips code fences from content, unmarshals the JSON into T, and
// runs the supplied validation function. It returns the parsed value or a
// descriptive error.
func Parse[T any](content string, validate func(*T) error) (*T, error) {
	cleaned, err := ExtractJSONObject(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	var result T
	decoder := json.NewDecoder(strings.NewReader(cleaned))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if validate != nil {
		if err := validate(&result); err != nil {
			return nil, err
		}
	}

	return &result, nil
}
