package automation

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/universe"
)

func TestEasternDayStartUTCUsesTradingDayAcrossUTCMidnight(t *testing.T) {
	got := easternDayStartUTC(time.Date(2026, time.August, 6, 0, 30, 0, 0, time.UTC))
	want := time.Date(2026, time.August, 5, 4, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("easternDayStartUTC() = %s, want %s", got, want)
	}
}

func TestPostMarketCompletionErrorsExposePartialCoverage(t *testing.T) {
	t.Parallel()

	if err := dailyReviewCompletionError(map[string]int{"query_errors": 1}); err == nil || !strings.Contains(err.Error(), "query_errors=1") {
		t.Fatalf("dailyReviewCompletionError() = %v, want query coverage error", err)
	}
	if err := strategyResweepCompletionError(2); err == nil || !strings.Contains(err.Error(), "strategies failed") {
		t.Fatalf("strategyResweepCompletionError() = %v, want sweep coverage error", err)
	}
	if err := optionsScanCompletionError(map[string]int{"price_fetch_failed": 1, "fetch_failed": 2, "persist_failed": 3}); err == nil || !strings.Contains(err.Error(), "price_fetch_failed=1") || !strings.Contains(err.Error(), "chain_fetch_failed=2") || !strings.Contains(err.Error(), "persist_failed=3") {
		t.Fatalf("optionsScanCompletionError() = %v, want complete failure counts", err)
	}

	if err := dailyReviewCompletionError(map[string]int{}); err != nil {
		t.Fatalf("dailyReviewCompletionError(empty) = %v, want nil", err)
	}
	if err := dailyReviewCompletionError(map[string]int{"failed": 2, "completed_without_signal": 1}); err == nil || !strings.Contains(err.Error(), "failed=2") {
		t.Fatalf("dailyReviewCompletionError(findings) = %v", err)
	}
	if err := strategyResweepCompletionError(0); err != nil {
		t.Fatalf("strategyResweepCompletionError(0) = %v, want nil", err)
	}
	if err := optionsScanCompletionError(map[string]int{}); err != nil {
		t.Fatalf("optionsScanCompletionError(empty) = %v, want nil", err)
	}
	if err := optionsScanCompletionError(map[string]int{"optionable": 10, "chain_insufficient": 10}); err == nil || !strings.Contains(err.Error(), "no_usable_chains=1") {
		t.Fatalf("optionsScanCompletionError(no chains) = %v", err)
	}
}

func TestOptionsScanTickersNormalizesDeduplicatesAndCapsWatchlist(t *testing.T) {
	t.Parallel()

	watchlist := []universe.TrackedTicker{{Ticker: " aapl "}, {Ticker: "AAPL"}, {Ticker: ""}}
	for i := 0; i < optionsScanWatchlistLimit+5; i++ {
		watchlist = append(watchlist, universe.TrackedTicker{Ticker: fmt.Sprintf("t%03d", i)})
	}

	got := optionsScanTickers(watchlist)
	if len(got) != optionsScanWatchlistLimit {
		t.Fatalf("ticker count = %d, want %d", len(got), optionsScanWatchlistLimit)
	}
	if got[0] != "AAPL" || got[1] != "T000" || got[len(got)-1] != "T098" {
		t.Fatalf("normalized capped tickers = %#v", got)
	}
}

func TestSummarizePipelineRunsSeparatesStatusFromDecision(t *testing.T) {
	t.Parallel()

	got := summarizePipelineRuns([]domain.PipelineRun{
		{Status: domain.PipelineStatusCompleted, Signal: domain.PipelineSignalBuy},
		{Status: domain.PipelineStatusCompleted, Signal: domain.PipelineSignalHold},
		{Status: domain.PipelineStatusCompleted},
		{Status: domain.PipelineStatusFailed, Signal: domain.PipelineSignalHold},
		{Status: domain.PipelineStatusRunning},
	})

	want := map[string]int{
		"runs": 5, "completed": 3, "failed": 1, "running": 1,
		"buy": 1, "hold": 1, "completed_without_signal": 1,
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("summary[%q] = %d, want %d (summary=%v)", key, got[key], value, got)
		}
	}
	if got["sell"] != 0 {
		t.Fatalf("summary[sell] = %d, want 0", got["sell"])
	}
}
