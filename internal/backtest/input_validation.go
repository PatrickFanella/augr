package backtest

import (
	"fmt"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// ValidateChronologicalBars rejects ambiguous data rather than allowing the
// replay iterator and strategy pipeline to observe different bar orders.
func ValidateChronologicalBars(bars []domain.OHLCV) error {
	for i := range bars {
		if bars[i].Timestamp.IsZero() {
			return fmt.Errorf("backtest: bar %d has zero timestamp", i)
		}
		if i > 0 && !bars[i].Timestamp.After(bars[i-1].Timestamp) {
			return fmt.Errorf("backtest: bars must be strictly chronological; bar %d timestamp %s is not after %s", i, bars[i].Timestamp, bars[i-1].Timestamp)
		}
	}
	return nil
}
