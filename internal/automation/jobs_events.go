package automation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

var (
	earningsScannerSpec = scheduler.ScheduleSpec{
		Type:         scheduler.ScheduleTypePreMarket,
		Cron:         "0 10 * * 1-5",
		SkipWeekends: true,
		SkipHolidays: true,
	}
	filingMonitorSpec = scheduler.ScheduleSpec{
		Type:         scheduler.ScheduleTypeCron,
		Cron:         "0 */2 * * 1-5",
		SkipWeekends: true,
		SkipHolidays: true,
	}
)

func (o *JobOrchestrator) registerEventJobs() {
	o.Register("earnings_scanner", "Scan upcoming earnings for watched tickers", earningsScannerSpec, o.earningsScanner)
	o.Register("filing_monitor", "Monitor recent 8-K filings for active strategies", filingMonitorSpec, o.filingMonitor)
}

// earningsScanner checks this week's earnings and cross-references with active strategy tickers.
func (o *JobOrchestrator) earningsScanner(ctx context.Context) error {
	if o.deps.EventsProvider == nil {
		o.logger.Info("earnings_scanner: skipped — events provider not configured")
		return nil
	}

	strategies, err := o.deps.StrategyRepo.List(ctx, repository.StrategyFilter{
		Status: domain.StrategyStatusActive,
	}, 0, 0)
	if err != nil {
		return fmt.Errorf("earnings_scanner: list strategies: %w", err)
	}
	if len(strategies) == 0 {
		o.logger.Info("earnings_scanner: no active strategies")
		return nil
	}

	// Build ticker set from active strategies.
	tickerSet := make(map[string]struct{}, len(strategies))
	for _, s := range strategies {
		tickerSet[s.Ticker] = struct{}{}
	}

	now := time.Now().UTC()
	from := now
	to := now.AddDate(0, 0, 7)

	events, err := o.deps.EventsProvider.GetEarningsCalendar(ctx, from, to)
	if err != nil {
		return fmt.Errorf("earnings_scanner: get earnings calendar: %w", err)
	}

	var matched int
	for _, ev := range events {
		if _, ok := tickerSet[ev.Symbol]; !ok {
			continue
		}
		matched++
		daysAway := int(ev.Date.Sub(now).Hours() / 24)
		o.logger.Info(fmt.Sprintf("earnings_scanner: %s earnings on %s (%s), %d days away",
			ev.Symbol, ev.Date.Format("2006-01-02"), ev.Hour, daysAway),
		)
	}

	o.logger.Info("earnings_scanner: complete",
		slog.Int("total_events", len(events)),
		slog.Int("matched", matched),
		slog.Int("active_tickers", len(tickerSet)),
	)
	return nil
}

// filingMonitor checks recent 8-K filings for all active strategy tickers.
func (o *JobOrchestrator) filingMonitor(ctx context.Context) error {
	if o.deps.EventsProvider == nil {
		o.logger.Info("filing_monitor: skipped — events provider not configured")
		return nil
	}

	strategies, err := o.deps.StrategyRepo.List(ctx, repository.StrategyFilter{
		Status: domain.StrategyStatusActive,
	}, 0, 0)
	if err != nil {
		return fmt.Errorf("filing_monitor: list strategies: %w", err)
	}
	if len(strategies) == 0 {
		o.logger.Info("filing_monitor: no active strategies")
		return nil
	}

	// Deduplicate tickers.
	seen := make(map[string]struct{}, len(strategies))
	var tickers []string
	for _, s := range strategies {
		if _, ok := seen[s.Ticker]; ok {
			continue
		}
		seen[s.Ticker] = struct{}{}
		tickers = append(tickers, s.Ticker)
	}

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -1)
	to := now

	var totalFilings int
	for _, ticker := range tickers {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		filings, err := o.deps.EventsProvider.GetFilings(ctx, ticker, "8-K", from, to)
		if err != nil {
			o.logger.Warn("filing_monitor: failed to fetch filings",
				slog.String("ticker", ticker),
				slog.Any("error", err),
			)
			continue
		}

		for _, f := range filings {
			totalFilings++
			o.logger.Info(fmt.Sprintf("filing_monitor: new 8-K for %s filed %s",
				f.Symbol, f.FiledDate.Format("2006-01-02")),
			)
		}
	}

	o.logger.Info("filing_monitor: complete",
		slog.Int("tickers_checked", len(tickers)),
		slog.Int("filings_found", totalFilings),
	)
	return nil
}
