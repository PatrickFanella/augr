package copyorigin

import (
	"bytes"
	"testing"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestRunIsOrderIndependentExactAndFailsCrossSubscription(t *testing.T) {
	t.Parallel()
	subscriptionID, sourceID := uuid.New(), uuid.New()
	subscription := domain.DefaultCopySubscription()
	subscription.ID, subscription.OriginType, subscription.OriginID = subscriptionID, "copy_subscription", subscriptionID
	intent := func(key string) domain.CopyTradeIntent {
		return domain.CopyTradeIntent{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(key)), SubscriptionID: subscriptionID, OriginType: "copy_subscription", OriginID: subscriptionID, SourceObservationID: sourceID, InstrumentKey: key, CalculationVersion: 1}
	}
	a, b := intent("AAPL"), intent("MSFT")
	first, err := NewRun(subscription, []domain.CopyTradeIntent{b, a})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRun(subscription, []domain.CopyTradeIntent{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID() != second.ID() || first.Digest() != second.Digest() || !bytes.Equal(first.CanonicalBytes(), second.CanonicalBytes()) {
		t.Fatal("input order changed run")
	}
	if _, err = FromCanonical(first.ID(), first.Digest(), first.CanonicalBytes()); err != nil {
		t.Fatal(err)
	}
	b.OriginID = uuid.New()
	if _, err = NewRun(subscription, []domain.CopyTradeIntent{a, b}); err == nil {
		t.Fatal("accepted cross-subscription intent")
	}
}
