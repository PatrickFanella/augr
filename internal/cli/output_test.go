package cli

import "testing"

func TestTruncatePreservesUTF8Runes(t *testing.T) {
	t.Parallel()

	got := truncate("héllo世界", 6)
	want := "héllo…"
	if got != want {
		t.Fatalf("truncate() = %q, want %q", got, want)
	}
}
