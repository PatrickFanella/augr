package automation

import (
	"slices"
	"strings"
	"testing"
)

func TestOvernightScheduleRefreshesHistoryBeforeConsumers(t *testing.T) {
	t.Parallel()

	if historyRefreshSpec.Cron != "0 0 * * 2-6" {
		t.Fatalf("history refresh cron = %q, want midnight before overnight consumers", historyRefreshSpec.Cron)
	}
	if overnightBacktestSpec.Cron != "*/30 1-5 * * 2-6" {
		t.Fatalf("overnight backtest cron = %q", overnightBacktestSpec.Cron)
	}
	if overnightSweepSpec.Cron != "30 0 * * 2-6" || overnightGenerateSpec.Cron != "0 6 * * 2-6" {
		t.Fatalf("overnight consumer crons = sweep %q generate %q", overnightSweepSpec.Cron, overnightGenerateSpec.Cron)
	}
	if optionsDiscoverySpec.Cron != "30 6 * * 2-6" {
		t.Fatalf("options discovery cron = %q, want after overnight generation", optionsDiscoverySpec.Cron)
	}
	// Ten backtest slots are available. With a 20-candidate cap, the default
	// chunk size needs at most one screen + seven generate + one sweep slots.
	neededSlots := 1 + (overnightBacktestWatchlistLimit+overnightBacktestGeneratePerChunk-1)/overnightBacktestGeneratePerChunk + 1
	if neededSlots > 10 {
		t.Fatalf("overnight backtest needs %d slots, only 10 are scheduled", neededSlots)
	}
}

func TestOvernightCompletionErrorsExposePartialCoverage(t *testing.T) {
	t.Parallel()

	if err := overnightSweepCompletionError(2); err == nil || !strings.Contains(err.Error(), "2 strategies failed") {
		t.Fatalf("overnightSweepCompletionError() = %v, want sweep coverage error", err)
	}
	if err := overnightGenerateCompletionError(1); err == nil || !strings.Contains(err.Error(), "1 index groups failed") {
		t.Fatalf("overnightGenerateCompletionError() = %v, want generation coverage error", err)
	}
	if err := overnightSweepCompletionError(0); err != nil {
		t.Fatalf("overnightSweepCompletionError(0) = %v, want nil", err)
	}
	if err := overnightGenerateCompletionError(0); err != nil {
		t.Fatalf("overnightGenerateCompletionError(0) = %v, want nil", err)
	}
}

func TestOvernightGenerationCoversEveryUniverseIndexGroup(t *testing.T) {
	for _, group := range []string{"nasdaq", "nyse", "other"} {
		if !slices.Contains(overnightIndexGroups, group) {
			t.Fatalf("overnight index groups %v omit %q", overnightIndexGroups, group)
		}
	}
}
