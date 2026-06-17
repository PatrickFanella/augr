package kalshi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestBrokerSubmitOrder_IsDisabled(t *testing.T) {
	t.Parallel()

	_, err := NewBroker().SubmitOrder(context.Background(), &domain.Order{})
	if !errors.Is(err, errLiveExecutionDisabled) {
		t.Fatalf("SubmitOrder() error = %v, want disabled error", err)
	}
}

func TestBrokerReadMethods_AreUnsupported(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	if err := broker.CancelOrder(context.Background(), "abc"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("CancelOrder() error = %v, want disabled error", err)
	}
	if _, err := broker.GetOrderStatus(context.Background(), "abc"); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("GetOrderStatus() error = %v, want unsupported error", err)
	}
	if _, err := broker.GetPositions(context.Background()); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("GetPositions() error = %v, want unsupported error", err)
	}
	if _, err := broker.GetAccountBalance(context.Background()); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("GetAccountBalance() error = %v, want unsupported error", err)
	}
}
