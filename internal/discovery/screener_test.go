package discovery

import "testing"

func TestNormalizeScreenerConfigAppliesDocumentedDefaults(t *testing.T) {
	t.Parallel()

	cfg := normalizeScreenerConfig(ScreenerConfig{})
	if cfg.MinADV != defaultScreenerMinADV {
		t.Fatalf("MinADV = %v, want %v", cfg.MinADV, defaultScreenerMinADV)
	}
	if cfg.MinATR != defaultScreenerMinATR {
		t.Fatalf("MinATR = %v, want %v", cfg.MinATR, defaultScreenerMinATR)
	}
}

func TestNormalizeScreenerConfigPreservesExplicitThresholds(t *testing.T) {
	t.Parallel()

	cfg := normalizeScreenerConfig(ScreenerConfig{MinADV: 250_000, MinATR: 1.25})
	if cfg.MinADV != 250_000 || cfg.MinATR != 1.25 {
		t.Fatalf("thresholds changed: MinADV=%v MinATR=%v", cfg.MinADV, cfg.MinATR)
	}
}
