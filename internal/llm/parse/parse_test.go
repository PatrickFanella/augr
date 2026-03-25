package parse

import (
	"fmt"
	"strings"
	"testing"
)

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
