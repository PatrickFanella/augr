package financialscheduler

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOccurrenceIdentityConvergesAcrossTimezoneAndPrecision(t *testing.T) {
	due := time.Date(2026, 11, 1, 5, 30, 0, 999, time.UTC)
	first, err := NewOccurrence(OccurrenceInput{"kalshi_settlement", "cron-v1@sha256:" + strings.Repeat("a", 64), TriggerScheduled, due, uuid.Nil})
	if err != nil {
		t.Fatal(err)
	}
	local := due.In(time.FixedZone("prior-offset", -4*60*60))
	retry, err := NewOccurrence(OccurrenceInput{"kalshi_settlement", first.ScheduleRevision, TriggerScheduled, local, uuid.Nil})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != retry.ID || first.SHA256 != retry.SHA256 {
		t.Fatalf("timezone retry diverged: %s/%s", first.ID, retry.ID)
	}
	changed, _ := NewOccurrence(OccurrenceInput{"kalshi_settlement", first.ScheduleRevision, TriggerScheduled, due.Add(time.Microsecond), uuid.Nil})
	if changed.ID == first.ID {
		t.Fatal("changed due slot reused occurrence identity")
	}
	if err := first.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestManualOccurrenceRequiresStableRequestIdentity(t *testing.T) {
	due := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	req := uuid.MustParse("60400000-0000-4000-8000-000000000001")
	a, err := NewOccurrence(OccurrenceInput{"portfolio_allocator", "manual-v1", TriggerManual, due, req})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewOccurrence(OccurrenceInput{"portfolio_allocator", "manual-v1", TriggerManual, due, req})
	if a.ID != b.ID {
		t.Fatal("manual request retry diverged")
	}
	c, _ := NewOccurrence(OccurrenceInput{"portfolio_allocator", "manual-v1", TriggerManual, due, uuid.MustParse("60400000-0000-4000-8000-000000000002")})
	if a.ID == c.ID {
		t.Fatal("distinct manual requests collapsed")
	}
	if _, err := NewOccurrence(OccurrenceInput{"portfolio_allocator", "manual-v1", TriggerManual, due, uuid.Nil}); err == nil {
		t.Fatal("missing manual request accepted")
	}
}

func TestEffectIdentityAndLifecycleBridges(t *testing.T) {
	occurrence := uuid.MustParse("60400000-0000-4000-8000-000000000010")
	payload := strings.Repeat("b", 64)
	a, err := NewEffect(EffectInput{occurrence, EffectIntent, "account/strategy/slot", payload})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewEffect(EffectInput{occurrence, EffectIntent, "account/strategy/slot", payload})
	if a.ID != b.ID || a.IntentIdempotencyKey() != b.IntentIdempotencyKey() {
		t.Fatal("effect retry diverged")
	}
	changed, _ := NewEffect(EffectInput{occurrence, EffectIntent, "account/strategy/slot", strings.Repeat("c", 64)})
	if changed.ID != a.ID || changed.SHA256 == a.SHA256 {
		t.Fatal("business identity or semantic conflict contract changed")
	}
	if a.IntentIdempotencyKey() == a.OrderIdempotencyKey() || a.OrderIdempotencyKey() == a.SettlementIdempotencyKey() {
		t.Fatal("effect bridges are not domain separated")
	}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestJobDefinitionIsClosedAndCanonical(t *testing.T) {
	definition, err := NewJobDefinition("options_expiry_settlement", MutationSettlement, MutationEvidence, MutationSettlement)
	if err != nil {
		t.Fatal(err)
	}
	if got := definition.Mutations; len(got) != 2 || got[0] != MutationEvidence || got[1] != MutationSettlement {
		t.Fatalf("mutations = %v", got)
	}
	if _, err := NewJobDefinition("Bad Job", MutationEvidence); err == nil {
		t.Fatal("invalid job key accepted")
	}
	if _, err := NewJobDefinition("job", MutationClass("unknown")); err == nil {
		t.Fatal("unknown mutation accepted")
	}
}
