package evaluation

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

func calculate(policy *Policy, observations []observationCanonical, trades []tradeCanonical, openLots int, execution ExecutionInput, start, end time.Time) ([]Metric, ExecutionInput, error) {
	attemptedOrders, err := canonicalCount(execution.AttemptedOrders)
	if err != nil {
		return nil, ExecutionInput{}, fmt.Errorf("attempted orders: %w", err)
	}
	filledOrders, err := canonicalCount(execution.FilledOrders)
	if err != nil || filledOrders.GreaterThan(attemptedOrders) {
		return nil, ExecutionInput{}, fmt.Errorf("filled orders are invalid")
	}
	attemptedQuantity, err := canonicalNonnegative(execution.AttemptedQuantity)
	if err != nil {
		return nil, ExecutionInput{}, err
	}
	filledQuantity, err := canonicalNonnegative(execution.FilledQuantity)
	if err != nil || filledQuantity.GreaterThan(attemptedQuantity) {
		return nil, ExecutionInput{}, fmt.Errorf("filled quantity is invalid")
	}
	execution = ExecutionInput{AttemptedOrders: attemptedOrders.String(), FilledOrders: filledOrders.String(), AttemptedQuantity: attemptedQuantity.String(), FilledQuantity: filledQuantity.String()}
	scale := int32(policy.DecimalScale())
	metrics := make([]Metric, 0, 32)
	add := func(section, name, value, unit, description string) {
		metrics = append(metrics, Metric{Section: section, Name: name, State: MetricAvailable, Value: value, Unit: unit, Description: description})
	}
	unavailable := func(section, name, unit, reason, description string) {
		metrics = append(metrics, Metric{Section: section, Name: name, State: MetricUnavailable, Unit: unit, Reason: reason, Description: description})
	}
	infinity := func(section, name, unit, description string) {
		metrics = append(metrics, Metric{Section: section, Name: name, State: MetricPositiveInfinity, Unit: unit, Description: description})
	}

	equity := decimalSeries(observations, func(v observationCanonical) string { return v.Equity })
	benchmark := decimalSeries(observations, func(v observationCanonical) string { return v.BenchmarkValue })
	strategyReturns := returns(equity)
	benchmarkReturns := returns(benchmark)
	cashReturns := decimalFloats(decimalSeries(observations[1:], func(v observationCanonical) string { return v.CashReturn }))
	totalReturn := equity[len(equity)-1].Div(equity[0]).Sub(decimal.NewFromInt(1))
	benchmarkTotal := benchmark[len(benchmark)-1].Div(benchmark[0]).Sub(decimal.NewFromInt(1))
	add("portfolio", "after_cost_total_return", quantize(totalReturn, scale), "ratio", "equity_return_after_declared_ownership_costs")
	add("portfolio", "benchmark_total_return", quantize(benchmarkTotal, scale), "ratio", "declared_benchmark_total_return")
	add("portfolio", "benchmark_excess_return", quantize(totalReturn.Sub(benchmarkTotal), scale), "ratio", "after_cost_return_minus_benchmark_return")
	years := end.Sub(start).Hours() / (24 * 365.25)
	if years > 0 {
		annualized := math.Pow(equity[len(equity)-1].Div(equity[0]).InexactFloat64(), 1/years) - 1
		if math.IsNaN(annualized) || math.IsInf(annualized, 0) {
			unavailable("portfolio", "after_cost_annualized_return", "ratio_per_year", "numeric_range_exceeded", "calendar_time_annualized_after_cost_return")
		} else {
			add("portfolio", "after_cost_annualized_return", number(annualized, scale), "ratio_per_year", "calendar_time_annualized_after_cost_return")
		}
	}
	active := subtract(strategyReturns, benchmarkReturns)
	if len(active) >= 2 {
		tracking := sampleStd(active)
		annualTracking := tracking * math.Sqrt(float64(policy.PeriodsPerYear()))
		if finite(annualTracking) {
			add("portfolio", "tracking_error", number(annualTracking, scale), "ratio_per_year", "annualized_sample_standard_deviation_of_active_returns")
		} else {
			unavailable("portfolio", "tracking_error", "ratio_per_year", "numeric_range_exceeded", "annualized_sample_standard_deviation_of_active_returns")
		}
		information := mean(active) / tracking * math.Sqrt(float64(policy.PeriodsPerYear()))
		switch {
		case tracking <= 0:
			unavailable("portfolio", "information_ratio", "ratio", "zero_tracking_error", "annualized_active_return_information_ratio")
		case !finite(information):
			unavailable("portfolio", "information_ratio", "ratio", "numeric_range_exceeded", "annualized_active_return_information_ratio")
		default:
			add("portfolio", "information_ratio", number(information, scale), "ratio", "annualized_active_return_information_ratio")
		}
	} else {
		unavailable("portfolio", "tracking_error", "ratio_per_year", "insufficient_return_samples", "annualized_sample_standard_deviation_of_active_returns")
		unavailable("portfolio", "information_ratio", "ratio", "insufficient_return_samples", "annualized_active_return_information_ratio")
	}
	excessCash := subtract(strategyReturns, cashReturns)
	if len(excessCash) >= 2 {
		vol := sampleStd(excessCash)
		sharpe := mean(excessCash) / vol * math.Sqrt(float64(policy.PeriodsPerYear()))
		switch {
		case vol <= 0:
			unavailable("portfolio", "sharpe_ratio", "ratio", "zero_volatility", "annualized_excess_cash_return_over_sample_volatility")
		case !finite(sharpe):
			unavailable("portfolio", "sharpe_ratio", "ratio", "numeric_range_exceeded", "annualized_excess_cash_return_over_sample_volatility")
		default:
			add("portfolio", "sharpe_ratio", number(sharpe, scale), "ratio", "annualized_excess_cash_return_over_sample_volatility")
		}
		down := downsideDeviation(excessCash)
		sortino := mean(excessCash) / down * math.Sqrt(float64(policy.PeriodsPerYear()))
		switch {
		case down <= 0:
			unavailable("portfolio", "sortino_ratio", "ratio", "zero_downside_deviation", "annualized_excess_cash_return_over_downside_deviation")
		case !finite(sortino):
			unavailable("portfolio", "sortino_ratio", "ratio", "numeric_range_exceeded", "annualized_excess_cash_return_over_downside_deviation")
		default:
			add("portfolio", "sortino_ratio", number(sortino, scale), "ratio", "annualized_excess_cash_return_over_downside_deviation")
		}
	} else {
		unavailable("portfolio", "sharpe_ratio", "ratio", "insufficient_return_samples", "annualized_excess_cash_return_over_sample_volatility")
		unavailable("portfolio", "sortino_ratio", "ratio", "insufficient_return_samples", "annualized_excess_cash_return_over_downside_deviation")
	}
	maxDD, recovery := drawdown(equity)
	add("portfolio", "maximum_drawdown", quantize(maxDD, scale), "ratio", "maximum_peak_to_trough_equity_drawdown")
	if recovery >= 0 {
		add("portfolio", "maximum_drawdown_recovery_periods", strconv.Itoa(recovery), "periods", "first_equity_at_or_above_prior_peak")
	} else {
		unavailable("portfolio", "maximum_drawdown_recovery_periods", "periods", "drawdown_unrecovered", "first_equity_at_or_above_prior_peak")
	}
	if annual, available := metricValue(metrics, "portfolio", "after_cost_annualized_return"); maxDD.IsPositive() && available {
		add("portfolio", "calmar_ratio", quantize(decimal.RequireFromString(annual).Div(maxDD), scale), "ratio", "annualized_after_cost_return_over_maximum_drawdown")
	} else if maxDD.IsPositive() {
		unavailable("portfolio", "calmar_ratio", "ratio", "annualized_return_unavailable", "annualized_after_cost_return_over_maximum_drawdown")
	} else {
		unavailable("portfolio", "calmar_ratio", "ratio", "zero_maximum_drawdown", "annualized_after_cost_return_over_maximum_drawdown")
	}

	wins, losses, breakeven := 0, 0, 0
	grossProfit, grossLoss, pnlTotal := decimal.Zero, decimal.Zero, decimal.Zero
	holdTotal := time.Duration(0)
	for _, trade := range trades {
		pnl := decimal.RequireFromString(trade.AfterCostPnL)
		pnlTotal = pnlTotal.Add(pnl)
		holdTotal += parseTime(trade.ExitAt).Sub(parseTime(trade.EntryAt))
		switch {
		case pnl.IsPositive():
			wins++
			grossProfit = grossProfit.Add(pnl)
		case pnl.IsNegative():
			losses++
			grossLoss = grossLoss.Add(pnl.Abs())
		default:
			breakeven++
		}
	}
	add("trade", "closed_trade_count", strconv.Itoa(len(trades)), "count", "fifo_closed_trade_round_trips")
	add("trade", "open_lot_count", strconv.Itoa(openLots), "count", "unclosed_fifo_lots_excluded_from_trade_outcomes")
	add("trade", "winning_trade_count", strconv.Itoa(wins), "count", "closed_trades_with_positive_after_cost_pnl")
	add("trade", "losing_trade_count", strconv.Itoa(losses), "count", "closed_trades_with_negative_after_cost_pnl")
	add("trade", "breakeven_trade_count", strconv.Itoa(breakeven), "count", "closed_trades_with_zero_after_cost_pnl")
	if len(trades) > 0 {
		add("trade", "win_rate", quantize(decimal.NewFromInt(int64(wins)).Div(decimal.NewFromInt(int64(len(trades)))), scale), "ratio", "closed_trade_after_cost_win_rate_not_bar_return_rate")
		add("trade", "expectancy", quantize(pnlTotal.Div(decimal.NewFromInt(int64(len(trades)))), scale), "currency_per_trade", "mean_closed_trade_after_cost_pnl")
		add("trade", "mean_holding_seconds", quantize(decimal.NewFromInt(int64(holdTotal/time.Microsecond)).Div(decimal.NewFromInt(1_000_000)).Div(decimal.NewFromInt(int64(len(trades)))), scale), "seconds", "mean_fifo_closed_trade_holding_time")
	} else {
		unavailable("trade", "win_rate", "ratio", "no_closed_trades", "closed_trade_after_cost_win_rate_not_bar_return_rate")
		unavailable("trade", "expectancy", "currency_per_trade", "no_closed_trades", "mean_closed_trade_after_cost_pnl")
		unavailable("trade", "mean_holding_seconds", "seconds", "no_closed_trades", "mean_fifo_closed_trade_holding_time")
	}
	switch {
	case losses > 0:
		add("trade", "profit_factor", quantize(grossProfit.Div(grossLoss), scale), "ratio", "gross_positive_after_cost_pnl_over_gross_negative_after_cost_pnl")
	case wins > 0:
		infinity("trade", "profit_factor", "ratio", "gross_positive_after_cost_pnl_with_no_losing_trade")
	default:
		unavailable("trade", "profit_factor", "ratio", "no_winning_and_losing_closed_trades", "gross_positive_after_cost_pnl_over_gross_negative_after_cost_pnl")
	}

	positivePeriods := 0
	for _, value := range strategyReturns {
		if value > 0 {
			positivePeriods++
		}
	}
	add("curve_diagnostics", "bar_positive_return_rate", quantize(decimal.NewFromInt(int64(positivePeriods)).Div(decimal.NewFromInt(int64(len(strategyReturns)))), scale), "ratio", "descriptor_only_not_trade_win_rate")
	add("sample", "portfolio_observation_count", strconv.Itoa(len(observations)), "count", "ordered_point_in_time_portfolio_observations")
	add("sample", "return_sample_count", strconv.Itoa(len(strategyReturns)), "count", "simple_return_periods")
	if attemptedOrders.IsPositive() {
		add("execution", "order_fill_ratio", quantize(filledOrders.Div(attemptedOrders), scale), "ratio", "filled_orders_over_attempted_orders")
	} else {
		unavailable("execution", "order_fill_ratio", "ratio", "no_attempted_orders", "filled_orders_over_attempted_orders")
	}
	if attemptedQuantity.IsPositive() {
		add("execution", "quantity_fill_ratio", quantize(filledQuantity.Div(attemptedQuantity), scale), "ratio", "filled_quantity_over_attempted_quantity")
	} else {
		unavailable("execution", "quantity_fill_ratio", "ratio", "no_attempted_quantity", "filled_quantity_over_attempted_quantity")
	}
	last := observations[len(observations)-1]
	add("cost", "total_ownership_cost", last.CumulativeOwnershipCost, "currency", "cumulative_declared_fees_spread_impact_financing_and_other_cost")
	add("cost", "turnover", last.CumulativeTurnover, "ratio", "cumulative_traded_notional_over_average_equity")
	add("cost", "modeled_slippage", last.CumulativeModeledSlippage, "currency", "cumulative_modeled_execution_slippage")
	if last.CumulativeObservedSlippage != nil {
		add("cost", "observed_slippage", *last.CumulativeObservedSlippage, "currency", "cumulative_observed_execution_slippage")
	} else {
		unavailable("cost", "observed_slippage", "currency", "observed_slippage_not_available", "cumulative_observed_execution_slippage")
	}
	gross := decimalSeries(observations, func(v observationCanonical) string { return v.GrossExposure })
	net := decimalSeries(observations, func(v observationCanonical) string { return v.NetExposure })
	concentration := decimalSeries(observations, func(v observationCanonical) string { return v.LargestPositionWeight })
	add("exposure", "mean_gross_exposure", quantize(decimalMean(gross), scale), "currency", "mean_gross_exposure_across_observations")
	for i := range net {
		net[i] = net[i].Abs()
	}
	add("exposure", "mean_absolute_net_exposure", quantize(decimalMean(net), scale), "currency", "mean_absolute_net_exposure_across_observations")
	add("exposure", "maximum_largest_position_weight", quantize(decimalMax(concentration), scale), "ratio", "maximum_largest_position_weight_across_observations")
	return metrics, execution, nil
}

func canonicalCount(value string) (decimal.Decimal, error) {
	d, err := canonicalNonnegative(value)
	if err != nil || !d.Equal(d.Truncate(0)) {
		return decimal.Zero, fmt.Errorf("canonical integer count is required")
	}
	return d, nil
}

func canonicalNonnegative(value string) (decimal.Decimal, error) {
	d, err := decimal.NewFromString(value)
	if err != nil || !validDecimal(value) || d.IsNegative() {
		return decimal.Zero, fmt.Errorf("canonical nonnegative decimal is required")
	}
	return d, nil
}

func decimalSeries(values []observationCanonical, selectValue func(observationCanonical) string) []decimal.Decimal {
	result := make([]decimal.Decimal, len(values))
	for i, value := range values {
		result[i] = decimal.RequireFromString(selectValue(value))
	}
	return result
}

func returns(values []decimal.Decimal) []float64 {
	result := make([]float64, len(values)-1)
	for i := 1; i < len(values); i++ {
		result[i-1] = values[i].Div(values[i-1]).Sub(decimal.NewFromInt(1)).InexactFloat64()
	}
	return result
}

func decimalFloats(values []decimal.Decimal) []float64 {
	result := make([]float64, len(values))
	for i := range values {
		result[i] = values[i].InexactFloat64()
	}
	return result
}

func subtract(left, right []float64) []float64 {
	n := min(len(left), len(right))
	result := make([]float64, n)
	for i := range n {
		result[i] = left[i] - right[i]
	}
	return result
}

func mean(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func sampleStd(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	avg := mean(values)
	total := 0.0
	for _, v := range values {
		d := v - avg
		total += d * d
	}
	return math.Sqrt(total / float64(len(values)-1))
}

func downsideDeviation(values []float64) float64 {
	total := 0.0
	count := 0
	for _, v := range values {
		if v < 0 {
			total += v * v
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return math.Sqrt(total / float64(count))
}

func drawdown(values []decimal.Decimal) (decimal.Decimal, int) {
	peak := values[0]
	maxDD := decimal.Zero
	troughIndex := -1
	peakAtMax := decimal.Zero
	for i, v := range values {
		if v.GreaterThan(peak) {
			peak = v
		}
		dd := peak.Sub(v).Div(peak)
		if dd.GreaterThan(maxDD) {
			maxDD = dd
			troughIndex = i
			peakAtMax = peak
		}
	}
	if troughIndex < 0 {
		return decimal.Zero, 0
	}
	for i := troughIndex + 1; i < len(values); i++ {
		if values[i].GreaterThanOrEqual(peakAtMax) {
			return maxDD, i - troughIndex
		}
	}
	return maxDD, -1
}
func quantize(value decimal.Decimal, scale int32) string { return value.Round(scale).String() }
func number(value float64, scale int32) string {
	return decimal.NewFromFloat(value).Round(scale).String()
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func decimalMean(values []decimal.Decimal) decimal.Decimal {
	total := decimal.Zero
	for _, v := range values {
		total = total.Add(v)
	}
	return total.Div(decimal.NewFromInt(int64(len(values))))
}

func decimalMax(values []decimal.Decimal) decimal.Decimal {
	result := values[0]
	for _, v := range values[1:] {
		if v.GreaterThan(result) {
			result = v
		}
	}
	return result
}

func metricValue(values []Metric, section, name string) (string, bool) {
	for _, v := range values {
		if v.Section == section && v.Name == name && v.State == MetricAvailable {
			return v.Value, true
		}
	}
	return "", false
}
