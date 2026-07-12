package operations

import (
	"context"
	"sort"
	"time"
)

type Capability struct {
	Name     string   `json:"name"`
	Mode     string   `json:"mode"`
	Ready    bool     `json:"ready"`
	Required bool     `json:"required"`
	Blockers []string `json:"blockers,omitempty"`
}

type ReadinessReport struct {
	ReleaseReady       bool         `json:"release_ready"`
	LiveTradingEnabled bool         `json:"live_trading_enabled"`
	Capabilities       []Capability `json:"capabilities"`
	GeneratedAt        time.Time    `json:"generated_at"`
}

type Source interface {
	Readiness(context.Context) (ReadinessReport, error)
}
type SourceFunc func(context.Context) (ReadinessReport, error)

func (f SourceFunc) Readiness(ctx context.Context) (ReadinessReport, error) { return f(ctx) }

type BuildInput struct {
	Database, Schema, DecisionJournal    bool
	Scheduler, OptionsData               bool
	PolymarketData, PolymarketSettlement bool
	KalshiData, KalshiSettlement         bool
	LiveTradingEnabled                   bool
	RecoveryDrillsPassed                 bool
	GeneratedAt                          time.Time
}

func BuildReadiness(in BuildInput) ReadinessReport {
	capability := func(name string, checks map[string]bool) Capability {
		c := Capability{Name: name, Mode: "paper", Required: true, Ready: true}
		for label, ok := range checks {
			if !ok {
				c.Ready = false
				c.Blockers = append(c.Blockers, label)
			}
		}
		sort.Strings(c.Blockers)
		return c
	}
	base := map[string]bool{"database unavailable": in.Database, "schema mismatch": in.Schema, "decision journal unavailable": in.DecisionJournal}
	capabilities := []Capability{
		capability("stocks", merge(base, map[string]bool{"scheduler unavailable": in.Scheduler})),
		capability("options", merge(base, map[string]bool{"scheduler unavailable": in.Scheduler, "options data unavailable": in.OptionsData})),
		capability("polymarket", merge(base, map[string]bool{"polymarket data unavailable": in.PolymarketData, "settlement job unavailable": in.PolymarketSettlement})),
		capability("kalshi", merge(base, map[string]bool{"kalshi data unavailable": in.KalshiData, "settlement job unavailable": in.KalshiSettlement})),
		capability("recovery_drills", map[string]bool{"required recovery drills not verified": in.RecoveryDrillsPassed}),
		{Name: "live_execution", Mode: "live", Ready: false, Required: false, Blockers: []string{"incremental operator activation required"}},
	}
	ready := true
	for _, c := range capabilities {
		if c.Required && !c.Ready {
			ready = false
		}
	}
	generated := in.GeneratedAt
	if generated.IsZero() {
		generated = time.Now().UTC()
	}
	return ReadinessReport{ReleaseReady: ready, LiveTradingEnabled: in.LiveTradingEnabled, Capabilities: capabilities, GeneratedAt: generated}
}

func merge(left, right map[string]bool) map[string]bool {
	out := make(map[string]bool, len(left)+len(right))
	for k, v := range left {
		out[k] = v
	}
	for k, v := range right {
		out[k] = v
	}
	return out
}
