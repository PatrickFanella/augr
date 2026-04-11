package postgres

import "testing"

func TestNormalizePolymarketTradeSide(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "yes lowercase", input: "yes", want: "YES"},
		{name: "no spaced", input: " no ", want: "NO"},
		{name: "up mixed", input: "uP", want: "Up"},
		{name: "down", input: "DOWN", want: "Down"},
		{name: "over", input: "over", want: "Over"},
		{name: "under", input: "Under", want: "Under"},
		{name: "invalid", input: "sideways", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizePolymarketTradeSide(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizePolymarketTradeSide(%q) error = nil, want error", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizePolymarketTradeSide(%q) error = %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("normalizePolymarketTradeSide(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
