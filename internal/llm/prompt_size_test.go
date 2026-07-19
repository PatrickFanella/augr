package llm

import "testing"

func TestValidatePromptBytes(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
		limit    int
		wantErr  bool
	}{
		{name: "at limit", messages: []Message{{Role: "user", Content: "1234"}}, limit: 4},
		{name: "over limit", messages: []Message{{Role: "user", Content: "12345"}}, limit: 4, wantErr: true},
		{name: "utf8 counts bytes", messages: []Message{{Role: "user", Content: "é"}}, limit: 1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePromptBytes(tt.messages, tt.limit)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
