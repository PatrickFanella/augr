package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type fakeEventsProvider struct {
	economicErr error
}

func (f fakeEventsProvider) GetEarningsCalendar(context.Context, time.Time, time.Time) ([]domain.EarningsEvent, error) {
	return []domain.EarningsEvent{}, nil
}

func (f fakeEventsProvider) GetNextEarnings(context.Context, string) (*domain.EarningsEvent, error) {
	return nil, nil
}

func (f fakeEventsProvider) GetFilings(context.Context, string, string, time.Time, time.Time) ([]domain.SECFiling, error) {
	return []domain.SECFiling{}, nil
}

func (f fakeEventsProvider) GetEconomicCalendar(context.Context) ([]domain.EconomicEvent, error) {
	if f.economicErr != nil {
		return nil, f.economicErr
	}
	return []domain.EconomicEvent{}, nil
}

func (f fakeEventsProvider) GetIPOCalendar(context.Context, time.Time, time.Time) ([]domain.IPOEvent, error) {
	return []domain.IPOEvent{}, nil
}

type fakeStatusError struct {
	status int
}

func (e fakeStatusError) Error() string   { return fmt.Sprintf("status=%d", e.status) }
func (e fakeStatusError) StatusCode() int { return e.status }

func TestGetEconomicCalendarReturnsEmptyListWhenProviderForbidden(t *testing.T) {
	deps := testDeps()
	deps.EventsProvider = fakeEventsProvider{economicErr: fakeStatusError{status: http.StatusForbidden}}
	srv := newTestServerWithDeps(t, deps)

	rr := doUnauthenticatedRequest(t, srv, http.MethodGet, "/api/v1/calendar/economic", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if body := rr.Body.String(); body != "[]\n" {
		t.Fatalf("body = %q, want empty JSON array", body)
	}
}

func TestGetFilingsWithoutTickerReturnsEmptyList(t *testing.T) {
	deps := testDeps()
	deps.EventsProvider = fakeEventsProvider{}
	srv := newTestServerWithDeps(t, deps)

	rr := doUnauthenticatedRequest(t, srv, http.MethodGet, "/api/v1/calendar/filings?from=2026-06-01&to=2026-06-30", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if body := rr.Body.String(); body != "[]\n" {
		t.Fatalf("body = %q, want empty JSON array", body)
	}
}
