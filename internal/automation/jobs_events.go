package automation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

var (
	earningsScannerSpec = scheduler.ScheduleSpec{
		Type:         scheduler.ScheduleTypeMarketHours,
		Cron:         "0 10 * * 1-5",
		SkipWeekends: true,
		SkipHolidays: true,
	}
	filingMonitorSpec = scheduler.ScheduleSpec{
		Type:         scheduler.ScheduleTypeCron,
		Cron:         "0 */4 * * 1-5",
		SkipWeekends: true,
		SkipHolidays: true,
	}
)

const filingMonitorMaxTickersPerRun = 20

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

	strategies, err := listAllStrategies(ctx, o.deps.StrategyRepo, repository.StrategyFilter{
		Status: domain.StrategyStatusActive,
	})
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
		if s.MarketType.Normalize() != domain.MarketTypeStock {
			continue
		}
		ticker := strings.ToUpper(strings.TrimSpace(s.Ticker))
		if ticker != "" {
			tickerSet[ticker] = struct{}{}
		}
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
		if _, ok := tickerSet[strings.ToUpper(strings.TrimSpace(ev.Symbol))]; !ok {
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
	o.SetLastSummary("earnings_scanner", map[string]int{"events": len(events), "matched": matched, "active_stock_tickers": len(tickerSet)})
	return nil
}

// filingMonitor checks recent 8-K and 10-Q filings for all active strategy tickers.
func (o *JobOrchestrator) filingMonitor(ctx context.Context) error {
	summary := map[string]int{
		"available":         0,
		"tickers_attempted": 0,
		"tickers_checked":   0,
		"filings_found":     0,
		"rate_limited":      0,
		"request_errors":    0,
		"analysis_errors":   0,
	}
	defer func() { o.SetLastSummary("filing_monitor", summary) }()

	if o.deps.EventsProvider == nil {
		o.logger.Info("filing_monitor: skipped — events provider not configured")
		return nil
	}

	strategies, err := listAllStrategies(ctx, o.deps.StrategyRepo, repository.StrategyFilter{
		Status: domain.StrategyStatusActive,
	})
	if err != nil {
		return fmt.Errorf("filing_monitor: list strategies: %w", err)
	}
	if len(strategies) == 0 {
		o.logger.Info("filing_monitor: no active strategies")
		return nil
	}

	// Build ticker → strategy name map (first match wins for display).
	tickerStrategy := make(map[string]string, len(strategies))
	var tickers []string
	for _, s := range strategies {
		if s.MarketType.Normalize() != domain.MarketTypeStock {
			continue
		}
		ticker := strings.ToUpper(strings.TrimSpace(s.Ticker))
		if ticker == "" {
			continue
		}
		if _, ok := tickerStrategy[ticker]; ok {
			continue
		}
		tickerStrategy[ticker] = s.Name
		tickers = append(tickers, ticker)
	}
	sort.Strings(tickers)
	available := len(tickers)
	summary["available"] = available
	if len(tickers) > filingMonitorMaxTickersPerRun {
		now := time.Now().UTC()
		batchNumber := now.YearDay()*6 + now.Hour()/4
		start := (batchNumber * filingMonitorMaxTickersPerRun) % len(tickers)
		rotated := append(append([]string(nil), tickers[start:]...), tickers[:start]...)
		o.logger.Info("filing_monitor: limiting ticker batch",
			slog.Int("available", len(tickers)),
			slog.Int("checked", filingMonitorMaxTickersPerRun),
			slog.Int("start_offset", start),
		)
		tickers = rotated[:filingMonitorMaxTickersPerRun]
	}

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -1)
	to := now

	for _, ticker := range tickers {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		summary["tickers_attempted"]++
		tickerComplete := true

		for _, formType := range []string{"8-K", "10-Q"} {
			filings, err := o.deps.EventsProvider.GetFilings(ctx, ticker, formType, from, to)
			if err != nil {
				if isFilingProviderRateLimited(err) {
					summary["rate_limited"] = 1
					o.logger.Warn("filing_monitor: provider rate limited; ending run early",
						slog.String("ticker", ticker),
						slog.String("form", formType),
						slog.Any("error", err),
					)
					return fmt.Errorf("filing_monitor: provider rate limited after %d filings: %w", summary["filings_found"], err)
				}
				tickerComplete = false
				summary["request_errors"]++
				o.logger.Warn("filing_monitor: failed to fetch filings",
					slog.String("ticker", ticker),
					slog.String("form", formType),
					slog.Any("error", err),
				)
				continue
			}

			for _, f := range filings {
				summary["filings_found"]++
				o.logger.Info(fmt.Sprintf("filing_monitor: new %s for %s filed %s",
					f.Form, f.Symbol, f.FiledDate.Format("2006-01-02")),
				)

				// Run LLM analysis if provider is available.
				if o.deps.LLMProvider != nil {
					analysis, err := AnalyzeFiling(ctx, o.deps.LLMProvider, "", f, tickerStrategy[ticker], o.logger)
					if err != nil {
						summary["analysis_errors"]++
						o.logger.Warn("filing_monitor: analysis failed",
							slog.String("ticker", ticker),
							slog.Any("error", err),
						)
						continue
					}

					if analysis.Impact == "high" && analysis.Action != "no_change" {
						o.logger.Warn(fmt.Sprintf("filing_monitor: %s %s analyzed — sentiment=%s, impact=%s, action=%s",
							ticker, f.Form, analysis.Sentiment, analysis.Impact, analysis.Action),
						)
					} else {
						o.logger.Info(fmt.Sprintf("filing_monitor: %s %s analyzed — sentiment=%s, impact=%s, action=%s",
							ticker, f.Form, analysis.Sentiment, analysis.Impact, analysis.Action),
						)
					}
				}
			}
		}
		if tickerComplete {
			summary["tickers_checked"]++
		}
	}

	o.logger.Info("filing_monitor: complete",
		slog.Int("tickers_attempted", summary["tickers_attempted"]),
		slog.Int("tickers_checked", summary["tickers_checked"]),
		slog.Int("filings_found", summary["filings_found"]),
		slog.Int("request_errors", summary["request_errors"]),
		slog.Int("analysis_errors", summary["analysis_errors"]),
	)
	if summary["request_errors"] > 0 || summary["analysis_errors"] > 0 {
		return fmt.Errorf("filing_monitor: incomplete run: %d provider requests failed and %d filing analyses failed", summary["request_errors"], summary["analysis_errors"])
	}
	return nil
}

type filingStatusCoder interface{ StatusCode() int }

func isFilingProviderRateLimited(err error) bool {
	var sc filingStatusCoder
	if errors.As(err, &sc) && sc.StatusCode() == http.StatusTooManyRequests {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "api limit") || strings.Contains(text, "rate limit") || strings.Contains(text, "status=429")
}
