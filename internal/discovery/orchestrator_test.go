package discovery

import (
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestCheckpointCandidateRoundTrip(t *testing.T) {
	screened := []ScreenResult{{
		Ticker: "AAPL",
		Bars: []domain.OHLCV{{
			Timestamp: time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC),
			Open:      1,
			High:      2,
			Low:       1,
			Close:     2,
			Volume:    100,
		}},
		Indicators: []domain.Indicator{{Name: "rsi_14", Value: 55}},
		Close:      2,
		ADV:        100,
		ATR:        1,
	}}

	checkpoint := CheckpointCandidatesFromScreenResults(screened)
	got := ScreenResultsFromCheckpointCandidates(checkpoint)
	if len(got) != 1 || got[0].Ticker != "AAPL" || got[0].Indicators[0].Name != "rsi_14" {
		t.Fatalf("round trip failed: %#v", got)
	}
}

func TestDiscoveryCanDeployOnlyCompleteResults(t *testing.T) {
	t.Parallel()
	if discoveryCanDeploy(nil) {
		t.Fatal("discoveryCanDeploy(nil) = true")
	}
	if discoveryCanDeploy(&DiscoveryResult{Errors: []string{"generation failed"}}) {
		t.Fatal("discoveryCanDeploy(partial result) = true")
	}
	if !discoveryCanDeploy(&DiscoveryResult{}) {
		t.Fatal("discoveryCanDeploy(complete result) = false")
	}
}
