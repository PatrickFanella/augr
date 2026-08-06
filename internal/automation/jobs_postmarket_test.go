package automation

import (
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
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

	if err := dailyReviewCompletionError(1); err == nil || !strings.Contains(err.Error(), "strategy run queries failed") {
		t.Fatalf("dailyReviewCompletionError() = %v, want query coverage error", err)
	}
	if err := strategyResweepCompletionError(2); err == nil || !strings.Contains(err.Error(), "strategies failed") {
		t.Fatalf("strategyResweepCompletionError() = %v, want sweep coverage error", err)
	}
	if err := optionsScanCompletionError(1, 2, 3); err == nil || !strings.Contains(err.Error(), "price_fetch_failed=1 chain_fetch_failed=2 persist_failed=3") {
		t.Fatalf("optionsScanCompletionError() = %v, want complete failure counts", err)
	}

	if err := dailyReviewCompletionError(0); err != nil {
		t.Fatalf("dailyReviewCompletionError(0) = %v, want nil", err)
	}
	if err := strategyResweepCompletionError(0); err != nil {
		t.Fatalf("strategyResweepCompletionError(0) = %v, want nil", err)
	}
	if err := optionsScanCompletionError(0, 0, 0); err != nil {
		t.Fatalf("optionsScanCompletionError(0, 0, 0) = %v, want nil", err)
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
