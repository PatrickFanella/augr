package automation

import (
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/data/stocktwits"
)

func TestNewsScanSpec_ReducesCadenceToThirtyMinutesWhenTwentyMinuteScheduleStillOverlaps(t *testing.T) {
	t.Parallel()

	const want = "7-59/30 * * * 1-5"
	if got := newsScanSpec.Cron; got != want {
		t.Fatalf("newsScanSpec.Cron = %q, want %q", got, want)
	}
}

func TestNormalizeStocktwitsSentimentUsesSignedScoreAndRatios(t *testing.T) {
	t.Parallel()

	score, bullish, bearish := normalizeStocktwitsSentiment(&stocktwits.SymbolSentiment{Bullish: 14, Bearish: 1, Total: 15})
	if score != 13.0/15.0 || bullish != 14.0/15.0 || bearish != 1.0/15.0 {
		t.Fatalf("normalized sentiment = (%f, %f, %f), want signed score and ratios", score, bullish, bearish)
	}
}
