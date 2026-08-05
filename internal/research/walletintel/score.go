package walletintel

import (
	"math"
	"sort"
	"strings"
	"time"
)

const (
	defaultROIWeight                   = 0.30
	defaultTradeCountWeight            = 0.20
	defaultCalibrationWeight           = 0.25
	defaultRecencyWeight               = 0.15
	defaultCategoryWeight              = 0.10
	defaultROIClamp                    = 2.0
	defaultTradeCountCap               = 50
	defaultRecencyHorizon              = 30 * 24 * time.Hour
	defaultHighConcentrationThreshold  = 0.70
	realizedROICategoryKey             = "realized_roi"
	tradeCountCategoryKey              = "trade_count"
	calibrationCategoryKey             = "calibration"
	recencyCategoryKey                 = "recency"
	categoryDiversificationCategoryKey = "category_diversification"
	categoryConcentrationCategoryKey   = "category_concentration"
)

// WalletSample is a compact research-only wallet profile input.
type WalletSample struct {
	WalletID         string             `json:"wallet_id"`
	RealizedROI      float64            `json:"realized_roi"`
	TradeCount       int                `json:"trade_count"`
	CalibrationScore float64            `json:"calibration_score"`
	LastTradeAt      time.Time          `json:"last_trade_at,omitempty"`
	CategoryExposure map[string]float64 `json:"category_exposure,omitempty"`
}

// ScoreConfig controls deterministic wallet scoring.
type ScoreConfig struct {
	Now                        time.Time     `json:"now,omitempty"`
	ROIWeight                  float64       `json:"roi_weight,omitempty"`
	TradeCountWeight           float64       `json:"trade_count_weight,omitempty"`
	CalibrationWeight          float64       `json:"calibration_weight,omitempty"`
	RecencyWeight              float64       `json:"recency_weight,omitempty"`
	CategoryWeight             float64       `json:"category_weight,omitempty"`
	ROIClamp                   float64       `json:"roi_clamp,omitempty"`
	TradeCountCap              int           `json:"trade_count_cap,omitempty"`
	RecencyHorizon             time.Duration `json:"recency_horizon,omitempty"`
	HighConcentrationThreshold float64       `json:"high_concentration_threshold,omitempty"`
}

// WalletScore is the JSON-safe research output.
type WalletScore struct {
	WalletID              string             `json:"wallet_id"`
	Score                 float64            `json:"score"`
	Components            map[string]float64 `json:"components"`
	CategoryConcentration float64            `json:"category_concentration"`
	Reasons               []string           `json:"reasons,omitempty"`
	GeneratedAt           time.Time          `json:"generated_at"`
}

// ScoreWallet assigns a deterministic research score to one wallet sample.
func ScoreWallet(sample WalletSample, cfg ScoreConfig) WalletScore {
	nc := normalizeScoreConfig(cfg)
	reasons := make([]string, 0, 4)
	walletID := strings.TrimSpace(sample.WalletID)
	if walletID == "" {
		reasons = append(reasons, "missing_wallet_id")
	}

	roiScore, roiReason := scoreRealizedROI(sample.RealizedROI, nc.roiClamp)
	if roiReason != "" {
		reasons = append(reasons, roiReason)
	}

	tradeCountScore, tradeReason := scoreTradeCount(sample.TradeCount, nc.tradeCountCap)
	if tradeReason != "" {
		reasons = append(reasons, tradeReason)
	}

	calibrationScore, calibrationReason := scoreCalibration(sample.CalibrationScore)
	if calibrationReason != "" {
		reasons = append(reasons, calibrationReason)
	}

	recencyScore, recencyReason := scoreRecency(sample.LastTradeAt, nc.now, nc.recencyHorizon)
	if recencyReason != "" {
		reasons = append(reasons, recencyReason)
	}

	categoryConcentration, categoryDiversification, categoryReason := scoreCategoryConcentration(sample.CategoryExposure)
	if categoryReason != "" {
		reasons = append(reasons, categoryReason)
	}
	if categoryConcentration >= nc.highConcentrationThreshold && categoryConcentration > 0 {
		reasons = append(reasons, "high_category_concentration")
	}

	components := map[string]float64{
		realizedROICategoryKey:             roiScore,
		tradeCountCategoryKey:              tradeCountScore,
		calibrationCategoryKey:             calibrationScore,
		recencyCategoryKey:                 recencyScore,
		categoryDiversificationCategoryKey: categoryDiversification,
		categoryConcentrationCategoryKey:   categoryConcentration,
	}

	weights := []float64{
		nc.roiWeight,
		nc.tradeCountWeight,
		nc.calibrationWeight,
		nc.recencyWeight,
		nc.categoryWeight,
	}
	values := []float64{roiScore, tradeCountScore, calibrationScore, recencyScore, categoryDiversification}
	score := weightedAverage(values, weights)

	return WalletScore{
		WalletID:              walletID,
		Score:                 clamp01(score),
		Components:            components,
		CategoryConcentration: categoryConcentration,
		Reasons:               uniqueStrings(reasons),
		GeneratedAt:           generatedAt(nc.now),
	}
}

// ScoreWallets scores and sorts wallets deterministically.
func ScoreWallets(samples []WalletSample, cfg ScoreConfig) []WalletScore {
	out := make([]WalletScore, 0, len(samples))
	for _, sample := range samples {
		out = append(out, ScoreWallet(sample, cfg))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			if out[i].WalletID == out[j].WalletID {
				return out[i].GeneratedAt.Before(out[j].GeneratedAt)
			}
			return out[i].WalletID < out[j].WalletID
		}
		return out[i].Score > out[j].Score
	})
	return out
}

type normalizedScoreConfig struct {
	now                        time.Time
	roiWeight                  float64
	tradeCountWeight           float64
	calibrationWeight          float64
	recencyWeight              float64
	categoryWeight             float64
	roiClamp                   float64
	tradeCountCap              float64
	recencyHorizon             time.Duration
	highConcentrationThreshold float64
}

func normalizeScoreConfig(cfg ScoreConfig) normalizedScoreConfig {
	nc := normalizedScoreConfig{
		now:                        cfg.Now,
		roiWeight:                  finitePositiveOrDefault(cfg.ROIWeight, defaultROIWeight),
		tradeCountWeight:           finitePositiveOrDefault(cfg.TradeCountWeight, defaultTradeCountWeight),
		calibrationWeight:          finitePositiveOrDefault(cfg.CalibrationWeight, defaultCalibrationWeight),
		recencyWeight:              finitePositiveOrDefault(cfg.RecencyWeight, defaultRecencyWeight),
		categoryWeight:             finitePositiveOrDefault(cfg.CategoryWeight, defaultCategoryWeight),
		roiClamp:                   finitePositiveOrDefault(cfg.ROIClamp, defaultROIClamp),
		tradeCountCap:              float64(cfg.TradeCountCap),
		recencyHorizon:             cfg.RecencyHorizon,
		highConcentrationThreshold: finiteUnitIntervalOrDefault(cfg.HighConcentrationThreshold, defaultHighConcentrationThreshold),
	}
	if nc.tradeCountCap <= 0 || !isFiniteFloat(nc.tradeCountCap) {
		nc.tradeCountCap = float64(defaultTradeCountCap)
	}
	if nc.recencyHorizon <= 0 {
		nc.recencyHorizon = defaultRecencyHorizon
	}
	if !isFiniteFloat(nc.roiWeight+nc.tradeCountWeight+nc.calibrationWeight+nc.recencyWeight+nc.categoryWeight) ||
		nc.roiWeight+nc.tradeCountWeight+nc.calibrationWeight+nc.recencyWeight+nc.categoryWeight <= 0 {
		nc.roiWeight = 1
		nc.tradeCountWeight = 1
		nc.calibrationWeight = 1
		nc.recencyWeight = 1
		nc.categoryWeight = 1
	}
	return nc
}

func scoreRealizedROI(v, clampWidth float64) (float64, string) {
	if !isFiniteFloat(v) {
		return 0, "invalid_realized_roi"
	}
	if clampWidth <= 0 || !isFiniteFloat(clampWidth) {
		clampWidth = defaultROIClamp
	}
	clamped := clamp(v, -clampWidth, clampWidth)
	return clamp01((clamped + clampWidth) / (2 * clampWidth)), ""
}

func scoreTradeCount(v int, capValue float64) (float64, string) {
	if v < 0 {
		return 0, "invalid_trade_count"
	}
	if capValue <= 0 || !isFiniteFloat(capValue) {
		capValue = float64(defaultTradeCountCap)
	}
	return clamp01(1 - math.Exp(-float64(v)/capValue)), ""
}

func scoreCalibration(v float64) (float64, string) {
	if !isFiniteFloat(v) {
		return 0, "invalid_calibration_score"
	}
	return clamp01(v), ""
}

func scoreRecency(lastTradeAt, now time.Time, horizon time.Duration) (float64, string) {
	if lastTradeAt.IsZero() {
		return 0, "missing_last_trade_at"
	}
	if now.IsZero() {
		return 0, "missing_now_reference"
	}
	if horizon <= 0 {
		horizon = defaultRecencyHorizon
	}
	age := now.Sub(lastTradeAt)
	if age <= 0 {
		return 1, ""
	}
	if age >= horizon {
		return 0, ""
	}
	return clamp01(1 - age.Seconds()/horizon.Seconds()), ""
}

func scoreCategoryConcentration(exposures map[string]float64) (float64, float64, string) {
	if len(exposures) == 0 {
		return 0, 0, "missing_category_exposure"
	}
	weights := make([]float64, 0, len(exposures))
	var total float64
	invalid := false
	for category, exposure := range exposures {
		if strings.TrimSpace(category) == "" || !isFiniteFloat(exposure) {
			invalid = true
			continue
		}
		value := math.Abs(exposure)
		if value <= 0 {
			continue
		}
		weights = append(weights, value)
		total += value
	}
	if len(weights) == 0 || total <= 0 {
		if invalid {
			return 0, 0, "invalid_category_exposure"
		}
		return 0, 0, "missing_category_exposure"
	}
	var concentration float64
	for _, weight := range weights {
		share := weight / total
		concentration += share * share
	}
	concentration = clamp01(concentration)
	if invalid {
		return concentration, clamp01(1 - concentration), "invalid_category_exposure_ignored"
	}
	return concentration, clamp01(1 - concentration), ""
}

func weightedAverage(values, weights []float64) float64 {
	if len(values) == 0 || len(values) != len(weights) {
		return 0
	}
	var totalWeight, total float64
	for i := range values {
		value := values[i]
		weight := weights[i]
		if !isFiniteFloat(value) || !isFiniteFloat(weight) || weight <= 0 {
			continue
		}
		totalWeight += weight
		total += value * weight
	}
	if totalWeight <= 0 {
		return 0
	}
	return total / totalWeight
}

func generatedAt(now time.Time) time.Time {
	if now.IsZero() {
		return time.Time{}
	}
	return now.UTC()
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func clamp(v, minimum, maximum float64) float64 {
	if v < minimum {
		return minimum
	}
	if v > maximum {
		return maximum
	}
	return v
}

func clamp01(v float64) float64 { return clamp(v, 0, 1) }

func finitePositiveOrDefault(v, fallback float64) float64 {
	if !isFiniteFloat(v) || v <= 0 {
		return fallback
	}
	return v
}

func finiteUnitIntervalOrDefault(v, fallback float64) float64 {
	if !isFiniteFloat(v) {
		return fallback
	}
	return clamp01(v)
}

func isFiniteFloat(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
