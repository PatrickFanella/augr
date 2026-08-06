package automation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type filingStrategyRepo struct{ *kalshiStrategyRepoStub }

func (s *filingStrategyRepo) Count(context.Context, repository.StrategyFilter) (int, error) {
	return len(s.strategies), nil
}

type filingEventsProviderStub struct {
	calls  int
	failAt int
	err    error
}

func (s *filingEventsProviderStub) GetEarningsCalendar(context.Context, time.Time, time.Time) ([]domain.EarningsEvent, error) {
	return nil, nil
}

func (s *filingEventsProviderStub) GetNextEarnings(context.Context, string) (*domain.EarningsEvent, error) {
	return nil, nil
}

func (s *filingEventsProviderStub) GetFilings(context.Context, string, string, time.Time, time.Time) ([]domain.SECFiling, error) {
	s.calls++
	failAt := s.failAt
	if failAt == 0 {
		failAt = 3
	}
	if s.calls == failAt {
		if s.err != nil {
			return nil, s.err
		}
		return nil, filingRateLimitError{}
	}
	return nil, nil
}

func TestFilingMonitorFailsPartialNonRateLimitedCoverage(t *testing.T) {
	provider := &filingEventsProviderStub{failAt: 2, err: errors.New("provider unavailable")}
	orch := NewJobOrchestrator(OrchestratorDeps{
		EventsProvider: provider,
		StrategyRepo: &filingStrategyRepo{&kalshiStrategyRepoStub{strategies: []domain.Strategy{
			{Ticker: "AAPL", MarketType: domain.MarketTypeStock, Status: domain.StrategyStatusActive},
			{Ticker: "MSFT", MarketType: domain.MarketTypeStock, Status: domain.StrategyStatusActive},
		}}},
	})
	orch.Register("filing_monitor", "test", schedulerSpecEveryMinute(), orch.filingMonitor)

	err := orch.filingMonitor(context.Background())
	if err == nil || !strings.Contains(err.Error(), "1 provider requests failed") {
		t.Fatalf("filingMonitor() error = %v, want partial-coverage error", err)
	}

	got := orch.jobs["filing_monitor"].LastSummary
	want := map[string]int{
		"available": 2, "tickers_attempted": 2, "tickers_checked": 1,
		"filings_found": 0, "rate_limited": 0, "request_errors": 1,
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("summary[%q] = %d, want %d (summary=%v)", key, got[key], value, got)
		}
	}
}

func (s *filingEventsProviderStub) GetEconomicCalendar(context.Context) ([]domain.EconomicEvent, error) {
	return nil, nil
}

func (s *filingEventsProviderStub) GetIPOCalendar(context.Context, time.Time, time.Time) ([]domain.IPOEvent, error) {
	return nil, nil
}

type filingRateLimitError struct{}

func (filingRateLimitError) Error() string   { return "provider quota exhausted" }
func (filingRateLimitError) StatusCode() int { return 429 }

func TestFilingMonitorDistinguishesAttemptedAndCompletedTickers(t *testing.T) {
	provider := &filingEventsProviderStub{}
	orch := NewJobOrchestrator(OrchestratorDeps{
		EventsProvider: provider,
		StrategyRepo: &filingStrategyRepo{&kalshiStrategyRepoStub{strategies: []domain.Strategy{
			{Ticker: "AAPL", MarketType: domain.MarketTypeStock, Status: domain.StrategyStatusActive},
			{Ticker: "MSFT", MarketType: domain.MarketTypeStock, Status: domain.StrategyStatusActive},
		}}},
	})
	orch.Register("filing_monitor", "test", schedulerSpecEveryMinute(), orch.filingMonitor)

	err := orch.filingMonitor(context.Background())
	if err == nil || !isFilingProviderRateLimited(err) {
		t.Fatalf("filingMonitor() error = %v, want rate-limit error", err)
	}
	if !errors.As(err, new(filingStatusCoder)) {
		// The job wraps the provider error; this guards typed-error preservation.
		t.Fatalf("filingMonitor() error = %v, want wrapped status coder", err)
	}

	got := orch.jobs["filing_monitor"].LastSummary
	want := map[string]int{
		"available": 2, "tickers_attempted": 2, "tickers_checked": 1,
		"filings_found": 0, "rate_limited": 1,
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("summary[%q] = %d, want %d (summary=%v)", key, got[key], value, got)
		}
	}
}
