package main

import "testing"

func TestNormalizePolymarketStrategySide(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "yes", input: "yes", want: "YES"},
		{name: "no", input: "NO", want: "NO"},
		{name: "up", input: "up", want: "Up"},
		{name: "down", input: "Down", want: "Down"},
		{name: "over", input: "OVER", want: "Over"},
		{name: "under", input: "under", want: "Under"},
		{name: "blank", input: "", wantErr: true},
		{name: "invalid", input: "sideways", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizePolymarketStrategySide(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizePolymarketStrategySide(%q) error = nil, want error", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizePolymarketStrategySide(%q) error = %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("normalizePolymarketStrategySide(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
