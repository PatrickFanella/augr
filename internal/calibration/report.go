package calibration

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/edge"
)

const defaultCalibrationBucketCount = 10

// StrategyCalibrationSample is a compact input row for report generation.
// It can be populated from decision/outcome joins or from a pre-aggregated feed.
type StrategyCalibrationSample struct {
	Probability     float64   `json:"probability"`
	Outcome         bool      `json:"outcome"`
	RealizedPnL     float64   `json:"realized_pnl"`
	Accepted        bool      `json:"accepted"`
	RejectionReason string    `json:"rejection_reason,omitempty"`
	Timestamp       time.Time `json:"timestamp,omitempty"`
	Provider        string    `json:"provider,omitempty"`
	Regime          string    `json:"regime,omitempty"`
}

// StrategyCalibrationInput controls report construction.
type StrategyCalibrationInput struct {
	StrategyID       *uuid.UUID                  `json:"strategy_id,omitempty"`
	StrategyKey      string                      `json:"strategy_key,omitempty"`
	MarketType       domain.MarketType           `json:"market_type,omitempty"`
	Samples          []StrategyCalibrationSample `json:"samples"`
	BucketCount      int                         `json:"bucket_count,omitempty"`
	GeneratedAt      time.Time                   `json:"generated_at,omitempty"`
	Provider         string                      `json:"provider,omitempty"`
	Regime           string                      `json:"regime,omitempty"`
	ConfidenceBucket string                      `json:"confidence_bucket,omitempty"`
}

// TimeRange captures the observed time span for samples that provide timestamps.
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// RealizedPnLSummary aggregates finite realized PnL observations.
// Values that would make the running aggregate non-finite are skipped.
type RealizedPnLSummary struct {
	Count int     `json:"count"`
	Sum   float64 `json:"sum"`
	Mean  float64 `json:"mean"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
}

// StrategyCalibrationReport is a deterministic JSON/report artifact shape.
type StrategyCalibrationReport struct {
	StrategyID            *uuid.UUID               `json:"strategy_id,omitempty"`
	StrategyKey           string                   `json:"strategy_key,omitempty"`
	MarketType            domain.MarketType        `json:"market_type,omitempty"`
	SampleCount           int                      `json:"sample_count"`
	BrierScore            float64                  `json:"brier_score"`
	LogLoss               float64                  `json:"log_loss"`
	CalibrationBuckets    []edge.CalibrationBucket `json:"calibration_buckets"`
	RealizedPnLSummary    RealizedPnLSummary       `json:"realized_pnl_summary"`
	AcceptedCount         int                      `json:"accepted_count"`
	RejectedCount         int                      `json:"rejected_count"`
	RejectionReasonCounts map[string]int           `json:"rejection_reason_counts,omitempty"`
	TimeRange             *TimeRange               `json:"time_range,omitempty"`
	GeneratedAt           time.Time                `json:"generated_at"`
	Provider              string                   `json:"provider,omitempty"`
	Regime                string                   `json:"regime,omitempty"`
	ConfidenceBucket      string                   `json:"confidence_bucket,omitempty"`
}

// BuildStrategyCalibrationReport converts compact sample input into a stable report.
func BuildStrategyCalibrationReport(input StrategyCalibrationInput) StrategyCalibrationReport {
	bucketCount := input.BucketCount
	if bucketCount <= 0 {
		bucketCount = defaultCalibrationBucketCount
	}

	report := StrategyCalibrationReport{
		StrategyID:       cloneUUIDPtr(input.StrategyID),
		StrategyKey:      strings.TrimSpace(input.StrategyKey),
		MarketType:       input.MarketType.Normalize(),
		GeneratedAt:      input.GeneratedAt,
		Provider:         strings.TrimSpace(input.Provider),
		Regime:           strings.TrimSpace(input.Regime),
		ConfidenceBucket: strings.TrimSpace(input.ConfidenceBucket),
	}

	probabilityOutcomes := make([]edge.ProbabilityOutcome, 0, len(input.Samples))
	pnlValues := make([]float64, 0, len(input.Samples))
	reasonCounts := make(map[string]int)

	var (
		haveTimeRange bool
		minTimestamp  time.Time
		maxTimestamp  time.Time
	)

	for _, sample := range input.Samples {
		if sample.Accepted {
			report.AcceptedCount++
		} else {
			report.RejectedCount++
			if reason := strings.TrimSpace(sample.RejectionReason); reason != "" {
				reasonCounts[reason]++
			}
		}

		if p, ok := sanitizeProbability(sample.Probability); ok {
			probabilityOutcomes = append(probabilityOutcomes, edge.ProbabilityOutcome{P: p, Outcome: sample.Outcome})
		}

		if isFinite(sample.RealizedPnL) {
			pnlValues = append(pnlValues, sample.RealizedPnL)
		}

		if !sample.Timestamp.IsZero() {
			if !haveTimeRange || sample.Timestamp.Before(minTimestamp) {
				minTimestamp = sample.Timestamp
			}
			if !haveTimeRange || sample.Timestamp.After(maxTimestamp) {
				maxTimestamp = sample.Timestamp
			}
			haveTimeRange = true
		}
	}

	report.SampleCount = len(probabilityOutcomes)
	report.BrierScore = edge.BrierScore(probabilityOutcomes)
	report.LogLoss = edge.LogLoss(probabilityOutcomes)
	report.CalibrationBuckets = edge.BucketCalibration(probabilityOutcomes, bucketCount)
	report.RealizedPnLSummary = summarizePnL(pnlValues)
	if len(reasonCounts) > 0 {
		report.RejectionReasonCounts = reasonCounts
	}
	if haveTimeRange {
		report.TimeRange = &TimeRange{Start: minTimestamp.UTC(), End: maxTimestamp.UTC()}
	}

	return report
}

// BuildSegmentedStrategyCalibrationReports groups one strategy/market sample
// set by provider, regime, and decile so decision quality is measurable across
// every required operating dimension.
func BuildSegmentedStrategyCalibrationReports(input StrategyCalibrationInput) []StrategyCalibrationReport {
	type key struct{ provider, regime, bucket string }
	groups := make(map[key][]StrategyCalibrationSample)
	for _, sample := range input.Samples {
		p, ok := sanitizeProbability(sample.Probability)
		if !ok {
			continue
		}
		lower := int(p*10) * 10
		if lower == 100 {
			lower = 90
		}
		k := key{strings.TrimSpace(sample.Provider), strings.TrimSpace(sample.Regime), fmt.Sprintf("%d-%d%%", lower, lower+10)}
		groups[k] = append(groups[k], sample)
	}
	keys := make([]key, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].provider != keys[j].provider {
			return keys[i].provider < keys[j].provider
		}
		if keys[i].regime != keys[j].regime {
			return keys[i].regime < keys[j].regime
		}
		return keys[i].bucket < keys[j].bucket
	})
	reports := make([]StrategyCalibrationReport, 0, len(keys))
	for _, k := range keys {
		segment := input
		segment.Samples = groups[k]
		segment.Provider, segment.Regime, segment.ConfidenceBucket = k.provider, k.regime, k.bucket
		reports = append(reports, BuildStrategyCalibrationReport(segment))
	}
	return reports
}

func summarizePnL(values []float64) RealizedPnLSummary {
	if len(values) == 0 {
		return RealizedPnLSummary{}
	}

	summary := RealizedPnLSummary{}
	for _, v := range values {
		nextSum := summary.Sum + v
		if !isFinite(nextSum) {
			continue
		}

		if summary.Count == 0 {
			summary.Count = 1
			summary.Sum = v
			summary.Min = v
			summary.Max = v
			continue
		}

		summary.Sum = nextSum
		summary.Count++
		if v < summary.Min {
			summary.Min = v
		}
		if v > summary.Max {
			summary.Max = v
		}
	}
	if summary.Count > 0 {
		summary.Mean = summary.Sum / float64(summary.Count)
	}
	return summary
}

func sanitizeProbability(p float64) (float64, bool) {
	if !isFinite(p) {
		return 0, false
	}
	if p < 0 {
		return 0, true
	}
	if p > 1 {
		return 1, true
	}
	return p, true
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func cloneUUIDPtr(v *uuid.UUID) *uuid.UUID {
	if v == nil {
		return nil
	}
	copied := *v
	return &copied
}
