package backtest

import "time"

// EquityCurvePoint is a visualization-ready equity curve sample with drawdown
// overlay data derived from the underlying portfolio equity snapshot.
type EquityCurvePoint struct {
	Timestamp        time.Time     `json:"timestamp"`
	Cash             float64       `json:"cash"`
	MarketValue      float64       `json:"market_value"`
	PortfolioValue   float64       `json:"portfolio_value"`
	RealizedPnL      float64       `json:"realized_pnl"`
	UnrealizedPnL    float64       `json:"unrealized_pnl"`
	TotalPnL         float64       `json:"total_pnl"`
	PeakEquity       float64       `json:"peak_equity"`
	DrawdownValue    float64       `json:"drawdown_value"`
	DrawdownPct      float64       `json:"drawdown_pct"`
	DrawdownDuration time.Duration `json:"drawdown_duration"`
}

// DrawdownPeriod identifies a drawdown interval between a peak, trough, and
// optional recovery timestamp.
type DrawdownPeriod struct {
	StartTimestamp    time.Time     `json:"start_timestamp"`
	TroughTimestamp   time.Time     `json:"trough_timestamp"`
	RecoveryTimestamp *time.Time    `json:"recovery_timestamp,omitempty"`
	PeakEquity        float64       `json:"peak_equity"`
	TroughEquity      float64       `json:"trough_equity"`
	RecoveryEquity    *float64      `json:"recovery_equity,omitempty"`
	DepthValue        float64       `json:"depth_value"`
	DepthPct          float64       `json:"depth_pct"`
	Duration          time.Duration `json:"duration"`
}

// EquityCurveReport contains timestamped equity samples plus drawdown periods
// suitable for charting and overlay visualization.
type EquityCurveReport struct {
	Points          []EquityCurvePoint `json:"points"`
	DrawdownPeriods []DrawdownPeriod   `json:"drawdown_periods"`
}

// GenerateEquityCurveReport derives visualization-ready equity and drawdown
// data from the raw backtest equity curve.
func GenerateEquityCurveReport(curve []EquityPoint) EquityCurveReport {
	if len(curve) == 0 {
		return EquityCurveReport{}
	}

	points := make([]EquityCurvePoint, 0, len(curve))
	periods := make([]DrawdownPeriod, 0)

	peakEquity := curve[0].Equity
	peakTimestamp := curve[0].Timestamp
	var active *DrawdownPeriod

	for _, sample := range curve {
		point := EquityCurvePoint{
			Timestamp:      sample.Timestamp,
			Cash:           sample.Cash,
			MarketValue:    sample.MarketValue,
			PortfolioValue: sample.Equity,
			RealizedPnL:    sample.RealizedPnL,
			UnrealizedPnL:  sample.UnrealizedPnL,
			TotalPnL:       sample.TotalPnL,
			PeakEquity:     peakEquity,
		}

		if sample.Equity >= peakEquity {
			if active != nil {
				recoveryTimestamp := sample.Timestamp
				recoveryEquity := sample.Equity
				active.RecoveryTimestamp = &recoveryTimestamp
				active.RecoveryEquity = &recoveryEquity
				active.Duration = recoveryTimestamp.Sub(active.StartTimestamp)
				periods = append(periods, *active)
				active = nil
			}

			peakEquity = sample.Equity
			peakTimestamp = sample.Timestamp
			point.PeakEquity = peakEquity
			points = append(points, point)
			continue
		}

		point.DrawdownValue = peakEquity - sample.Equity
		point.DrawdownPct = drawdownPct(peakEquity, sample.Equity)

		if active == nil {
			active = &DrawdownPeriod{
				StartTimestamp:  peakTimestamp,
				TroughTimestamp: sample.Timestamp,
				PeakEquity:      peakEquity,
				TroughEquity:    sample.Equity,
				DepthValue:      point.DrawdownValue,
				DepthPct:        point.DrawdownPct,
			}
		} else if sample.Equity < active.TroughEquity {
			active.TroughTimestamp = sample.Timestamp
			active.TroughEquity = sample.Equity
			active.DepthValue = point.DrawdownValue
			active.DepthPct = point.DrawdownPct
		}

		point.DrawdownDuration = sample.Timestamp.Sub(active.StartTimestamp)
		points = append(points, point)
	}

	if active != nil {
		active.Duration = curve[len(curve)-1].Timestamp.Sub(active.StartTimestamp)
		periods = append(periods, *active)
	}

	return EquityCurveReport{
		Points:          points,
		DrawdownPeriods: periods,
	}
}

func drawdownPct(peakEquity float64, currentEquity float64) float64 {
	if peakEquity <= 0 {
		return 0
	}

	return (peakEquity - currentEquity) / peakEquity
}
