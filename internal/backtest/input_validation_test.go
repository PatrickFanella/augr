package backtest

import (
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestValidateChronologicalBarsRejectsDuplicateAndUnsortedTimestamps(t *testing.T) {
	now := time.Now().UTC()
	valid := []domain.OHLCV{{Timestamp: now}, {Timestamp: now.Add(time.Minute)}}
	if err := ValidateChronologicalBars(valid); err != nil {
		t.Fatalf("valid bars rejected: %v", err)
	}
	for _, bars := range [][]domain.OHLCV{{{Timestamp: now}, {Timestamp: now}}, {{Timestamp: now}, {Timestamp: now.Add(-time.Minute)}}} {
		if err := ValidateChronologicalBars(bars); err == nil {
			t.Fatalf("ambiguous bars accepted: %+v", bars)
		}
	}
}
