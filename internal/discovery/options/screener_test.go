package options

import "testing"

func TestOptionsScreenCompletionError(t *testing.T) {
	for _, tc := range []struct {
		name     string
		attempts int64
		errors   int64
		wantErr  bool
	}{
		{name: "no eligible tickers", attempts: 0, errors: 0},
		{name: "valid empty screen", attempts: 3, errors: 0},
		{name: "partial provider failure", attempts: 3, errors: 2},
		{name: "all provider lookups failed", attempts: 3, errors: 3, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := optionsScreenCompletionError(tc.attempts, tc.errors)
			if (err != nil) != tc.wantErr {
				t.Fatalf("optionsScreenCompletionError(%d, %d) error = %v, wantErr %v", tc.attempts, tc.errors, err, tc.wantErr)
			}
		})
	}
}
