package parse

import (
	"fmt"
	"strings"
	"testing"
)

func TestStripThinkingTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no thinking tags",
			input: `{"key": "value"}`,
			want:  `{"key": "value"}`,
		},
		{
			name:  "thinking block before JSON",
			input: "<think>\nLet me analyze this...\nThe user wants JSON.\n</think>\n{\"key\": \"value\"}",
			want:  `{"key": "value"}`,
		},
		{
			name:  "thinking block before code-fenced JSON",
			input: "<think>\nReasoning here\n</think>\n```json\n{\"key\": \"value\"}\n```",
			want:  "```json\n{\"key\": \"value\"}\n```",
		},
		{
			name:  "empty thinking block",
			input: "<think></think>{\"key\": \"value\"}",
			want:  `{"key": "value"}`,
		},
		{
			name:  "thinking block with free text response",
			input: "<think>\nI should analyze the market trends.\n</think>\nThe market shows bullish momentum.",
			want:  "The market shows bullish momentum.",
		},
		{
			name:  "no content after thinking block",
			input: "<think>\nJust thinking\n</think>",
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := StripThinkingTags(tc.input)
			if got != tc.want {
				t.Fatalf("StripThinkingTags(%q) =\n  %q\nwant:\n  %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestStripCodeFences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no fences plain JSON",
			input: `{"key": "value"}`,
			want:  `{"key": "value"}`,
		},
		{
			name:  "json fences with newline",
			input: "```json\n{\"key\": \"value\"}\n```",
			want:  `{"key": "value"}`,
		},
		{
			name:  "plain fences without json tag",
			input: "```\n{\"key\": \"value\"}\n```",
			want:  `{"key": "value"}`,
		},
		{
			name:  "inline fence json starts on same line",
			input: "```json{\"key\": \"value\"}```",
			want:  `{"key": "value"}`,
		},
		{
			name:  "inline fence with space before JSON",
			input: "```json {\"key\": \"value\"}```",
			want:  `{"key": "value"}`,
		},
		{
			name:  "closing fence only no opening",
			input: "{\"key\": \"value\"}\n```",
			want:  "{\"key\": \"value\"}\n```",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace only input",
			input: "   \n\t  ",
			want:  "",
		},
		{
			name:  "fences with leading and trailing whitespace",
			input: "  \n```json\n{\"a\":1}\n```  \n",
			want:  `{"a":1}`,
		},
		{
			name:  "fences with array JSON",
			input: "```json\n[1, 2, 3]\n```",
			want:  "[1, 2, 3]",
		},
		{
			name:  "inline fence with array",
			input: "```json[1,2,3]```",
			want:  "[1,2,3]",
		},
		{
			name:  "nested backticks in content after opening fence",
			input: "```json\n{\"code\": \"use ` for ticks\"}\n```",
			want:  "{\"code\": \"use ` for ticks\"}",
		},
		{
			name:  "multiline JSON inside fences",
			input: "```json\n{\n  \"a\": 1,\n  \"b\": 2\n}\n```",
			want:  "{\n  \"a\": 1,\n  \"b\": 2\n}",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := StripCodeFences(tc.input)
			if got != tc.want {
				t.Fatalf("StripCodeFences(%q) =\n  %q\nwant:\n  %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestExtractJSONObject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "plain object", input: `{"ok":true}`, want: `{"ok":true}`},
		{name: "prose prefixed production shape", input: "**Focusing on JSON**\nI will return it now.\n{\"version\":1,\"name\":\"safe\"}", want: `{"version":1,"name":"safe"}`},
		{name: "fenced with trailing prose", input: "```json\n{\"nested\":{\"value\":\"} inside string\"}}\n```\nDone", want: `{"nested":{"value":"} inside string"}}`},
		{name: "escaped quote and brace", input: `prefix {"value":"escaped \" { brace"} suffix`, want: `{"value":"escaped \" { brace"}`},
		{name: "missing object", input: "no structured response", wantErr: "opening brace not found"},
		{name: "incomplete object", input: `reasoning {"ok":true`, wantErr: "incomplete object"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ExtractJSONObject(tt.input)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ExtractJSONObject() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExtractJSONObject() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ExtractJSONObject() = %q, want %q", got, tt.want)
			}
		})
	}
}

// testPayload is a simple struct used by Parse tests.
type testPayload struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestParseNoFences(t *testing.T) {
	input := `{"name":"alpha","value":42}`
	result, err := Parse[testPayload](input, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if result.Name != "alpha" || result.Value != 42 {
		t.Fatalf("Parse() = %+v, want {Name:alpha Value:42}", result)
	}
}

func TestParseWithCodeFences(t *testing.T) {
	input := "```json\n{\"name\":\"beta\",\"value\":7}\n```"
	result, err := Parse[testPayload](input, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if result.Name != "beta" || result.Value != 7 {
		t.Fatalf("Parse() = %+v, want {Name:beta Value:7}", result)
	}
}

func TestParseInvalidJSON(t *testing.T) {
	input := "this is not json"
	_, err := Parse[testPayload](input, nil)
	if err == nil {
		t.Fatal("Parse() error = nil, want non-nil for invalid JSON")
	}
	if got := err.Error(); !strings.Contains(got, "failed to parse JSON") {
		t.Fatalf("error = %q, want it to contain %q", got, "failed to parse JSON")
	}
}

func TestParseValidationFailure(t *testing.T) {
	input := `{"name":"","value":0}`
	validator := func(p *testPayload) error {
		if p.Name == "" {
			return fmt.Errorf("name is required")
		}
		return nil
	}
	_, err := Parse[testPayload](input, validator)
	if err == nil {
		t.Fatal("Parse() error = nil, want non-nil for validation failure")
	}
	if got := err.Error(); got != "name is required" {
		t.Fatalf("error = %q, want %q", got, "name is required")
	}
}

func TestParseValidationSuccess(t *testing.T) {
	input := `{"name":"gamma","value":99}`
	validator := func(p *testPayload) error {
		if p.Value <= 0 {
			return fmt.Errorf("value must be positive")
		}
		return nil
	}
	result, err := Parse[testPayload](input, validator)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if result.Name != "gamma" || result.Value != 99 {
		t.Fatalf("Parse() = %+v, want {Name:gamma Value:99}", result)
	}
}

func TestParseNilValidator(t *testing.T) {
	input := `{"name":"delta","value":1}`
	result, err := Parse[testPayload](input, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if result.Name != "delta" {
		t.Fatalf("Parse().Name = %q, want %q", result.Name, "delta")
	}
}

func TestParseEmptyInput(t *testing.T) {
	_, err := Parse[testPayload]("", nil)
	if err == nil {
		t.Fatal("Parse() error = nil, want non-nil for empty input")
	}
}

func TestParseWhitespaceInput(t *testing.T) {
	_, err := Parse[testPayload]("   \n\t  ", nil)
	if err == nil {
		t.Fatal("Parse() error = nil, want non-nil for whitespace input")
	}
}

func TestParseInlineFence(t *testing.T) {
	input := "```json{\"name\":\"inline\",\"value\":5}```"
	result, err := Parse[testPayload](input, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if result.Name != "inline" || result.Value != 5 {
		t.Fatalf("Parse() = %+v, want {Name:inline Value:5}", result)
	}
}

func TestParseWithThinkingTags(t *testing.T) {
	input := "<think>\nThe user wants name=qwen and value=14.\n</think>\n{\"name\":\"qwen\",\"value\":14}"
	result, err := Parse[testPayload](input, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if result.Name != "qwen" || result.Value != 14 {
		t.Fatalf("Parse() = %+v, want {Name:qwen Value:14}", result)
	}
}

func TestParseWithThinkingTagsAndCodeFences(t *testing.T) {
	input := "<think>\nLet me generate valid JSON.\n</think>\n```json\n{\"name\":\"combo\",\"value\":99}\n```"
	result, err := Parse[testPayload](input, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if result.Name != "combo" || result.Value != 99 {
		t.Fatalf("Parse() = %+v, want {Name:combo Value:99}", result)
	}
}

func TestParseNestedBackticks(t *testing.T) {
	input := "```json\n{\"name\":\"has ` tick\",\"value\":3}\n```"
	result, err := Parse[testPayload](input, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if result.Name != "has ` tick" {
		t.Fatalf("Parse().Name = %q, want %q", result.Name, "has ` tick")
	}
}
