package economicid

import (
	"testing"

	"github.com/google/uuid"
)

func TestDeterministicUUIDUsesDomainAndLengthPrefixedComponents(t *testing.T) {
	first := DeterministicUUID("economic-source-event", "account", "feed", "event-1")
	retry := DeterministicUUID("economic-source-event", "account", "feed", "event-1")
	if first == uuid.Nil || first != retry {
		t.Fatalf("DeterministicUUID() = %s/%s, want identical non-nil UUIDs", first, retry)
	}
	if first == DeterministicUUID("economic-normalization", "account", "feed", "event-1") {
		t.Fatal("DeterministicUUID() ignored its domain")
	}
	if DeterministicUUID("economic-source-event", "ab", "c") ==
		DeterministicUUID("economic-source-event", "a", "bc") {
		t.Fatal("DeterministicUUID() did not separate variable-length components")
	}
}
