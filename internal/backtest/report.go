package backtest

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// BacktestReport combines the full machine-readable output of a backtest run:
// strategy inputs, date range, computed analytics, trade log, and equity-curve
// data.
type BacktestReport struct {
	StrategyConfiguration ReportStrategyConfiguration `json:"strategy_configuration"`
	DateRange             ReportDateRange             `json:"date_range"`
	PerformanceMetrics    Metrics                     `json:"performance_metrics"`
	TradeAnalytics        TradeAnalytics              `json:"trade_analytics"`
	BenchmarkComparison   BenchmarkComparison         `json:"benchmark_comparison"`
	EquityCurve           EquityCurveReport           `json:"equity_curve"`
	TradeLog              []domain.Trade              `json:"trade_log"`
}

// ReportDateRange captures the time window used for a backtest run.
type ReportDateRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// ReportStrategyConfiguration captures the strategy and execution inputs used
// to produce the report.
type ReportStrategyConfiguration struct {
	StrategyID        uuid.UUID               `json:"strategy_id"`
	Ticker            string                  `json:"ticker"`
	InitialCash       float64                 `json:"initial_cash"`
	FillConfiguration ReportFillConfiguration `json:"fill_configuration"`
	PromptVersion     string                  `json:"prompt_version,omitempty"`
	PromptVersionHash string                  `json:"prompt_version_hash,omitempty"`
}

// ReportFillConfiguration is a JSON-friendly snapshot of fill-engine settings.
type ReportFillConfiguration struct {
	Slippage         ReportModelConfiguration  `json:"slippage"`
	Spread           *ReportModelConfiguration `json:"spread,omitempty"`
	TransactionCosts ReportTransactionCosts    `json:"transaction_costs"`
	MaxVolumePct     float64                   `json:"max_volume_pct"`
}

// ReportModelConfiguration describes a configured fill sub-model.
type ReportModelConfiguration struct {
	Type        string   `json:"type"`
	Amount      *float64 `json:"amount,omitempty"`
	BasisPoints *float64 `json:"basis_points,omitempty"`
	ScaleFactor *float64 `json:"scale_factor,omitempty"`
	SpreadBps   *float64 `json:"spread_bps,omitempty"`
}

// ReportTransactionCosts captures transaction-cost inputs in JSON-friendly form.
type ReportTransactionCosts struct {
	CommissionPerOrder float64 `json:"commission_per_order"`
	CommissionPerUnit  float64 `json:"commission_per_unit"`
	ExchangeFeePct     float64 `json:"exchange_fee_pct"`
}

// BenchmarkComparison isolates the benchmark-relative metrics in the report.
type BenchmarkComparison struct {
	BuyAndHoldReturn float64
	Alpha            float64
	Beta             float64
	InformationRatio float64
}

// MarshalJSON keeps benchmark-relative metrics valid JSON even when future
// computations yield non-finite values.
func (b BenchmarkComparison) MarshalJSON() ([]byte, error) {
	type benchmarkComparisonJSON struct {
		BuyAndHoldReturn any `json:"buy_and_hold_return"`
		Alpha            any `json:"alpha"`
		Beta             any `json:"beta"`
		InformationRatio any `json:"information_ratio"`
	}

	return json.Marshal(benchmarkComparisonJSON{
		BuyAndHoldReturn: jsonFloatValue(b.BuyAndHoldReturn),
		Alpha:            jsonFloatValue(b.Alpha),
		Beta:             jsonFloatValue(b.Beta),
		InformationRatio: jsonFloatValue(b.InformationRatio),
	})
}

// GenerateBacktestReport builds a single structured summary from a completed
// orchestrated backtest run.
func GenerateBacktestReport(cfg OrchestratorConfig, result *OrchestratorResult) (*BacktestReport, error) {
	if result == nil {
		return nil, fmt.Errorf("backtest: result is required")
	}

	equityCurve := result.EquityCurveReport
	if len(equityCurve.Points) == 0 && len(equityCurve.DrawdownPeriods) == 0 {
		equityCurve = GenerateEquityCurveReport(result.EquityCurve)
	}

	report := &BacktestReport{
		StrategyConfiguration: reportStrategyConfiguration(cfg, result),
		DateRange: ReportDateRange{
			Start: cfg.StartDate,
			End:   cfg.EndDate,
		},
		PerformanceMetrics: result.Metrics,
		TradeAnalytics:     result.TradeAnalytics,
		BenchmarkComparison: BenchmarkComparison{
			BuyAndHoldReturn: result.Metrics.BuyAndHoldReturn,
			Alpha:            result.Metrics.Alpha,
			Beta:             result.Metrics.Beta,
			InformationRatio: result.Metrics.InformationRatio,
		},
		EquityCurve: normalizeEquityCurveReport(equityCurve),
		TradeLog:    append([]domain.Trade(nil), result.Trades...),
	}

	if report.TradeLog == nil {
		report.TradeLog = []domain.Trade{}
	}

	return report, nil
}

func reportStrategyConfiguration(cfg OrchestratorConfig, result *OrchestratorResult) ReportStrategyConfiguration {
	promptVersion := cfg.PromptVersion
	if result != nil && result.PromptVersion != "" {
		promptVersion = result.PromptVersion
	}

	promptVersionHash := cfg.PromptVersionHash
	if result != nil && result.PromptVersionHash != "" {
		promptVersionHash = result.PromptVersionHash
	}

	return ReportStrategyConfiguration{
		StrategyID:        cfg.StrategyID,
		Ticker:            cfg.Ticker,
		InitialCash:       cfg.InitialCash,
		FillConfiguration: reportFillConfiguration(cfg.FillConfig),
		PromptVersion:     promptVersion,
		PromptVersionHash: promptVersionHash,
	}
}

func reportFillConfiguration(cfg FillConfig) ReportFillConfiguration {
	return ReportFillConfiguration{
		Slippage: reportModelConfiguration(cfg.Slippage),
		Spread:   reportSpreadConfiguration(cfg.Spread),
		TransactionCosts: ReportTransactionCosts{
			CommissionPerOrder: cfg.Costs.CommissionPerOrder,
			CommissionPerUnit:  cfg.Costs.CommissionPerUnit,
			ExchangeFeePct:     cfg.Costs.ExchangeFeePct,
		},
		MaxVolumePct: cfg.MaxVolumePct,
	}
}

func reportModelConfiguration(model SlippageModel) ReportModelConfiguration {
	switch v := model.(type) {
	case FixedSlippage:
		return ReportModelConfiguration{Type: "fixed", Amount: ptrFloat64(v.Amount)}
	case *FixedSlippage:
		if v == nil {
			return ReportModelConfiguration{}
		}
		return ReportModelConfiguration{Type: "fixed", Amount: ptrFloat64(v.Amount)}
	case ProportionalSlippage:
		return ReportModelConfiguration{Type: "proportional", BasisPoints: ptrFloat64(v.BasisPoints)}
	case *ProportionalSlippage:
		if v == nil {
			return ReportModelConfiguration{}
		}
		return ReportModelConfiguration{Type: "proportional", BasisPoints: ptrFloat64(v.BasisPoints)}
	case VolatilityScaledSlippage:
		return ReportModelConfiguration{Type: "volatility_scaled", ScaleFactor: ptrFloat64(v.ScaleFactor)}
	case *VolatilityScaledSlippage:
		if v == nil {
			return ReportModelConfiguration{}
		}
		return ReportModelConfiguration{Type: "volatility_scaled", ScaleFactor: ptrFloat64(v.ScaleFactor)}
	case nil:
		return ReportModelConfiguration{}
	default:
		return ReportModelConfiguration{Type: fmt.Sprintf("%T", model)}
	}
}

func reportSpreadConfiguration(model SpreadModel) *ReportModelConfiguration {
	switch v := model.(type) {
	case FixedSpread:
		return &ReportModelConfiguration{Type: "fixed", SpreadBps: ptrFloat64(v.SpreadBps)}
	case *FixedSpread:
		if v == nil {
			return nil
		}
		return &ReportModelConfiguration{Type: "fixed", SpreadBps: ptrFloat64(v.SpreadBps)}
	case nil:
		return nil
	default:
		return &ReportModelConfiguration{Type: fmt.Sprintf("%T", model)}
	}
}

func normalizeEquityCurveReport(report EquityCurveReport) EquityCurveReport {
	if report.Points == nil {
		report.Points = []EquityCurvePoint{}
	}
	if report.DrawdownPeriods == nil {
		report.DrawdownPeriods = []DrawdownPeriod{}
	}
	return report
}

func ptrFloat64(value float64) *float64 {
	return &value
}

func jsonFloatValue(value float64) any {
	switch {
	case math.IsNaN(value):
		return "NaN"
	case math.IsInf(value, 1):
		return "Infinity"
	case math.IsInf(value, -1):
		return "-Infinity"
	default:
		return value
	}
}
