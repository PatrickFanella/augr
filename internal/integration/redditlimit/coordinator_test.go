package redditlimit

import (
	"testing"
	"time"
)

func TestCoordinatorSharesCooldownAndFreshness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
	c := &Coordinator{}
	effective := c.Start(now, 2*time.Minute)
	if effective < 2*time.Minute || effective >= 132*time.Second {
		t.Fatalf("effective cooldown = %s, want [2m, 2m12s)", effective)
	}
	if remaining := c.Remaining(now.Add(time.Minute)); remaining < time.Minute {
		t.Fatalf("remaining = %s, want at least 1m", remaining)
	}
	c.MarkSuccess(now.Add(30 * time.Second))
	if got := c.LastSuccess(); !got.Equal(now.Add(30 * time.Second)) {
		t.Fatalf("LastSuccess() = %s", got)
	}
	if remaining := c.Remaining(now.Add(3 * time.Minute)); remaining != 0 {
		t.Fatalf("expired remaining = %s, want 0", remaining)
	}
}
