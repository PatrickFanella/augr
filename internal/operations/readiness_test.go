package operations

import "testing"

func TestBuildReadinessIsCapabilityScopedAndLiveFailClosed(t *testing.T) {
	report := BuildReadiness(BuildInput{Database: true, Schema: true, DecisionJournal: true, Scheduler: true, OptionsData: true, PolymarketData: true, PolymarketSettlement: true, KalshiData: false, KalshiSettlement: true, RecoveryDrillsPassed: true})
	if report.ReleaseReady {
		t.Fatal("release ready despite required Kalshi blocker")
	}
	if len(report.Capabilities) != 6 {
		t.Fatalf("capabilities = %d", len(report.Capabilities))
	}
	if report.Capabilities[3].Ready || len(report.Capabilities[3].Blockers) != 1 {
		t.Fatalf("kalshi = %+v", report.Capabilities[3])
	}
	if report.Capabilities[5].Ready || report.Capabilities[5].Required {
		t.Fatalf("live capability = %+v", report.Capabilities[5])
	}
}

func TestBuildReadinessDoesNotRequireRetiredPolymarketCapability(t *testing.T) {
	report := BuildReadiness(BuildInput{
		Database:             true,
		Schema:               true,
		DecisionJournal:      true,
		Scheduler:            true,
		OptionsData:          true,
		KalshiData:           true,
		KalshiSettlement:     true,
		RecoveryDrillsPassed: true,
		PolymarketData:       false,
		PolymarketSettlement: false,
	})
	if !report.ReleaseReady {
		t.Fatalf("release not ready with only optional Polymarket blockers: %+v", report)
	}
	polymarket := report.Capabilities[2]
	if polymarket.Required || polymarket.Ready || len(polymarket.Blockers) != 2 {
		t.Fatalf("polymarket = %+v, want optional and visibly blocked", polymarket)
	}
}
