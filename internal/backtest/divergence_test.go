package backtest

import (
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/google/uuid"
)

func TestDivergence_WithinTolerance_Default(t *testing.T) {
	d := Divergence{Backtest: SidedMetrics{FillRate: 0.50, WinRate: 0.55, Samples: 120}, Live: SidedMetrics{FillRate: 0.52, WinRate: 0.54, Samples: 80}}
	if got := d.Status(); got != DivergenceWithinTolerance {
		t.Fatalf("Status() = %q, want %q", got, DivergenceWithinTolerance)
	}
}

func TestDivergence_ExceedsOnFillRate(t *testing.T) {
	d := Divergence{Backtest: SidedMetrics{FillRate: 0.50, WinRate: 0.55, Samples: 120}, Live: SidedMetrics{FillRate: 0.55, WinRate: 0.55, Samples: 80}}
	if got := d.Status(); got != DivergenceExceedsTolerance {
		t.Fatalf("Status() = %q, want %q", got, DivergenceExceedsTolerance)
	}
}

func TestDivergence_ExceedsOnWinRate(t *testing.T) {
	d := Divergence{Backtest: SidedMetrics{FillRate: 0.50, WinRate: 0.55, Samples: 120}, Live: SidedMetrics{FillRate: 0.50, WinRate: 0.61, Samples: 80}}
	if got := d.Status(); got != DivergenceExceedsTolerance {
		t.Fatalf("Status() = %q, want %q", got, DivergenceExceedsTolerance)
	}
}

func TestDivergence_EmptyLive_TreatsWithinTolerance(t *testing.T) {
	d := Divergence{Backtest: SidedMetrics{FillRate: 0.50, WinRate: 0.55, Samples: 120}, Live: SidedMetrics{Samples: 0}}
	if got := d.Status(); got != DivergenceWithinTolerance {
		t.Fatalf("Status() = %q, want %q", got, DivergenceWithinTolerance)
	}
}

func TestDivergence_CustomTolerance(t *testing.T) {
	d := Divergence{Backtest: SidedMetrics{FillRate: 0.50, WinRate: 0.55, Samples: 120}, Live: SidedMetrics{FillRate: 0.57, WinRate: 0.55, Samples: 80}, Tolerance: 0.10}
	if got := d.Status(); got != DivergenceWithinTolerance {
		t.Fatalf("Status() = %q, want %q", got, DivergenceWithinTolerance)
	}
}

func TestDivergence_MaxAbsDelta(t *testing.T) {
	d := Divergence{Backtest: SidedMetrics{FillRate: 0.50, WinRate: 0.55, Samples: 120}, Live: SidedMetrics{FillRate: 0.57, WinRate: 0.61, Samples: 80}}
	if got := d.MaxAbsDelta(); got < 0.069999 || got > 0.070001 {
		t.Fatalf("MaxAbsDelta() = %v, want ~%v", got, 0.07)
	}
}

func TestBuildRepositoryDivergenceUsesPersistedFillsAndClosedOutcomes(t *testing.T) {
	orderID := uuid.New()
	closed := time.Now()
	d := buildRepositoryDivergence(uuid.New(), Metrics{FillRate: .75, WinRate: .6, OrderAttempts: 20}, []domain.TradeDecision{
		{PaperOrderID: &orderID}, {}, {LiveOrderID: &orderID},
	}, []domain.Position{{ClosedAt: &closed, RealizedPnL: 10}, {ClosedAt: &closed, RealizedPnL: -2}, {}})
	if d.Backtest.FillRate != .75 || d.Backtest.Samples != 20 {
		t.Fatalf("backtest metrics = %+v", d.Backtest)
	}
	if d.Live.FillRate != 2.0/3.0 || d.Live.WinRate != .5 || d.Live.Samples != 3 {
		t.Fatalf("live metrics = %+v", d.Live)
	}
}
