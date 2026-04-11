package main

import (
	"os"
	"testing"
	"time"
)

func TestLoadStaleRunTTL(t *testing.T) {
	t.Parallel()

	old := os.Getenv("STALE_RUN_TTL")
	defer func() {
		if old == "" {
			_ = os.Unsetenv("STALE_RUN_TTL")
		} else {
			_ = os.Setenv("STALE_RUN_TTL", old)
		}
	}()

	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "default when unset", value: "", want: 30 * time.Minute},
		{name: "custom duration", value: "45m", want: 45 * time.Minute},
		{name: "invalid falls back", value: "oops", want: 30 * time.Minute},
		{name: "non-positive falls back", value: "0s", want: 30 * time.Minute},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.value == "" {
				_ = os.Unsetenv("STALE_RUN_TTL")
			} else {
				_ = os.Setenv("STALE_RUN_TTL", tc.value)
			}
			if got := loadStaleRunTTL(slogDiscardLogger()); got != tc.want {
				t.Fatalf("loadStaleRunTTL() = %s, want %s", got, tc.want)
			}
		})
	}
}
