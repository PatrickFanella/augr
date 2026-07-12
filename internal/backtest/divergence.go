package backtest

import (
	"errors"
	"math"
)

// Divergence compares persisted simulation evidence with observed paper/live
// outcomes. Repository aggregation and API exposure live outside the fill
// engine because the two samples are produced by different runtimes.
type DivergenceStatus string

const (
	DivergenceWithinTolerance  DivergenceStatus = "within_tolerance"
	DivergenceExceedsTolerance DivergenceStatus = "exceeds_tolerance"
)

const DefaultDivergenceTolerance = 0.03

type SidedMetrics struct {
	FillRate float64
	WinRate  float64
	Samples  int
}

type Divergence struct {
	StrategyID string
	Backtest   SidedMetrics
	Live       SidedMetrics
	Tolerance  float64
}

var ErrDivergenceNotFound = errors.New("divergence: not found")

func (d Divergence) effectiveTolerance() float64 {
	if d.Tolerance > 0 {
		return d.Tolerance
	}
	return DefaultDivergenceTolerance
}

func (d Divergence) Status() DivergenceStatus {
	if d.Backtest.Samples == 0 || d.Live.Samples == 0 {
		return DivergenceWithinTolerance
	}
	if math.Abs(d.Live.FillRate-d.Backtest.FillRate) <= d.effectiveTolerance() && math.Abs(d.Live.WinRate-d.Backtest.WinRate) <= d.effectiveTolerance() {
		return DivergenceWithinTolerance
	}
	return DivergenceExceedsTolerance
}

func (d Divergence) MaxAbsDelta() float64 {
	fill := math.Abs(d.Live.FillRate - d.Backtest.FillRate)
	win := math.Abs(d.Live.WinRate - d.Backtest.WinRate)
	if fill > win {
		return fill
	}
	return win
}
