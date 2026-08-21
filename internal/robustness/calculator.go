package robustness

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
)

type candidateCalculation struct {
	rawProbability decimal.Decimal
	baselineMean   decimal.Decimal
}

func calculate(policy *Policy, candidates []CandidateEvidence, sources reportSources) error {
	calculations := make([]candidateCalculation, len(candidates))
	for index := range candidates {
		calculation, statistics, gates, err := calculateCandidate(policy, candidates[index], sources)
		if err != nil {
			return err
		}
		calculations[index], candidates[index].Statistics, candidates[index].Gates = calculation, statistics, gates
	}
	applyHolm(policy, candidates, calculations)
	return nil
}

func calculateCandidate(policy *Policy, candidate CandidateEvidence, sources reportSources) (candidateCalculation, []Statistic, []Gate, error) {
	baselineReturns := make([]float64, 0)
	perturbationReturns := map[string][]float64{}
	for _, kind := range policy.RequiredPerturbations() {
		perturbationReturns[kind] = nil
	}
	for _, fold := range candidate.Folds {
		baseline := sources[uuid.MustParse(fold.Baseline.ReportID)]
		values, err := periodReturns(baseline)
		if err != nil {
			return candidateCalculation{}, nil, nil, err
		}
		baselineReturns = append(baselineReturns, values...)
		for _, scenario := range fold.Perturbations {
			values, err := periodReturns(sources[uuid.MustParse(scenario.ReportID)])
			if err != nil {
				return candidateCalculation{}, nil, nil, err
			}
			perturbationReturns[scenario.Kind] = append(perturbationReturns[scenario.Kind], values...)
		}
	}
	if len(baselineReturns) < 2 {
		return candidateCalculation{}, nil, nil, fmt.Errorf("candidate requires at least two out-of-sample returns")
	}
	for _, value := range baselineReturns {
		if !finite(value) {
			return candidateCalculation{}, nil, nil, fmt.Errorf("candidate return numeric range exceeded")
		}
	}
	scale := int32(policy.DecimalScale())
	meanValue := mean(baselineReturns)
	lower, upper, raw := bootstrap(policy, uuid.MustParse(candidate.VersionID), baselineReturns)
	largest, topDecile, concentrationAvailable := concentration(baselineReturns)
	statistics := []Statistic{
		availableStatistic("fold_count", strconv.Itoa(len(candidate.Folds)), "count", "ordered_purged_walk_forward_test_folds"),
		availableStatistic("out_of_sample_return_count", strconv.Itoa(len(baselineReturns)), "count", "combined_baseline_out_of_sample_period_returns"),
		availableStatistic("baseline_mean_return", number(meanValue, scale), "ratio_per_period", "mean_combined_out_of_sample_baseline_return"),
		availableStatistic("bootstrap_lower_confidence_bound", number(lower, scale), "ratio_per_period", "deterministic_percentile_bootstrap_lower_bound"),
		availableStatistic("bootstrap_upper_confidence_bound", number(upper, scale), "ratio_per_period", "deterministic_percentile_bootstrap_upper_bound"),
		availableStatistic("raw_nonpositive_mean_probability", number(raw, scale), "probability", "empirical_bootstrap_probability_mean_is_nonpositive_with_plus_one_correction"),
	}
	if concentrationAvailable {
		statistics = append(statistics,
			availableStatistic("largest_positive_period_share", number(largest, scale), "ratio", "largest_positive_period_return_over_total_positive_period_returns"),
			availableStatistic("top_decile_positive_period_share", number(topDecile, scale), "ratio", "top_ceil_ten_percent_positive_period_returns_over_total_positive_period_returns"))
	} else {
		statistics = append(statistics,
			unavailableStatistic("largest_positive_period_share", "ratio", "no_positive_out_of_sample_periods", "largest_positive_period_return_over_total_positive_period_returns"),
			unavailableStatistic("top_decile_positive_period_share", "ratio", "no_positive_out_of_sample_periods", "top_ceil_ten_percent_positive_period_returns_over_total_positive_period_returns"))
	}
	bootstrapState := GateFail
	bootstrapReason := "lower_confidence_bound_not_positive"
	if lower > 0 {
		bootstrapState, bootstrapReason = GatePass, ""
	}
	gates := []Gate{{
		Name: "bootstrap_positive_mean", State: bootstrapState, Threshold: "0", Observed: number(lower, scale), Reason: bootstrapReason,
		Description: "lower_confidence_bound_must_be_strictly_positive",
	}}
	concentrationState, concentrationReason := GateFail, "no_positive_out_of_sample_periods"
	if concentrationAvailable && largest <= decimal.RequireFromString(policy.canonical.MaxLargestPositiveShare).InexactFloat64() &&
		topDecile <= decimal.RequireFromString(policy.canonical.MaxTopDecilePositiveShare).InexactFloat64() {
		concentrationState, concentrationReason = GatePass, ""
	} else if concentrationAvailable {
		concentrationReason = "positive_return_concentration_exceeds_policy"
	}
	gates = append(gates, Gate{
		Name: "return_concentration", State: concentrationState,
		Threshold: policy.canonical.MaxLargestPositiveShare + "/" + policy.canonical.MaxTopDecilePositiveShare,
		Observed:  number(largest, scale) + "/" + number(topDecile, scale), Reason: concentrationReason,
		Description: "largest_and_top_decile_positive_return_shares_within_policy",
	})
	perturbationPass := true
	worstDegradation := math.Inf(-1)
	for _, kind := range policy.RequiredPerturbations() {
		values := perturbationReturns[kind]
		if len(values) != len(baselineReturns) {
			return candidateCalculation{}, nil, nil, fmt.Errorf("perturbation %s sample count diverges", kind)
		}
		perturbedMean := mean(values)
		degradation := meanValue - perturbedMean
		worstDegradation = max(worstDegradation, degradation)
		statistics = append(statistics, availableStatistic("perturbation_"+kind+"_mean_return", number(perturbedMean, scale), "ratio_per_period", "combined_out_of_sample_perturbation_mean_return"))
		if !finite(perturbedMean) || perturbedMean <= 0 || degradation > decimal.RequireFromString(policy.canonical.MaxPerturbationDegradation).InexactFloat64() {
			perturbationPass = false
		}
	}
	perturbationState, perturbationReason := GatePass, ""
	if !perturbationPass {
		perturbationState, perturbationReason = GateFail, "perturbation_nonpositive_or_degradation_exceeds_policy"
	}
	gates = append(gates, Gate{
		Name: "perturbation_stability", State: perturbationState, Threshold: policy.canonical.MaxPerturbationDegradation,
		Observed: number(worstDegradation, scale), Reason: perturbationReason, Description: "all_required_perturbations_positive_and_within_maximum_mean_degradation",
	})
	return candidateCalculation{rawProbability: decimal.NewFromFloat(raw), baselineMean: decimal.NewFromFloat(meanValue)}, statistics, gates, nil
}

func applyHolm(policy *Policy, candidates []CandidateEvidence, calculations []candidateCalculation) {
	type ranked struct {
		index int
		id    string
		raw   decimal.Decimal
	}
	values := make([]ranked, len(candidates))
	for index := range candidates {
		values[index] = ranked{index: index, id: candidates[index].VersionID, raw: calculations[index].rawProbability}
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i].raw.LessThan(values[j].raw) || values[i].raw.Equal(values[j].raw) && values[i].id < values[j].id
	})
	running := decimal.Zero
	adjusted := make([]decimal.Decimal, len(values))
	for rank, value := range values {
		candidate := value.raw.Mul(decimal.NewFromInt(int64(len(values) - rank)))
		if candidate.GreaterThan(decimal.NewFromInt(1)) {
			candidate = decimal.NewFromInt(1)
		}
		if candidate.LessThan(running) {
			candidate = running
		} else {
			running = candidate
		}
		adjusted[value.index] = candidate
	}
	alpha := decimal.RequireFromString(policy.canonical.FamilyWiseAlpha)
	scale := int32(policy.DecimalScale())
	for index := range candidates {
		candidates[index].Statistics = append(candidates[index].Statistics,
			availableStatistic("holm_adjusted_nonpositive_mean_probability", adjusted[index].Round(scale).String(), "probability", "family_wide_holm_bonferroni_adjusted_probability"))
		state, reason := GatePass, ""
		if adjusted[index].GreaterThan(alpha) {
			state, reason = GateFail, "adjusted_probability_exceeds_family_wise_alpha"
		}
		candidates[index].Gates = append(candidates[index].Gates, Gate{
			Name: "multiple_testing_adjustment", State: state,
			Threshold: policy.canonical.FamilyWiseAlpha, Observed: adjusted[index].Round(scale).String(), Reason: reason,
			Description: "holm_bonferroni_adjusted_probability_within_family_wise_alpha",
		})
		overall := GatePass
		overallReason := ""
		for _, gate := range candidates[index].Gates {
			if gate.State != GatePass {
				overall, overallReason = GateFail, "one_or_more_required_robustness_gates_failed"
				break
			}
		}
		candidates[index].Gates = append(candidates[index].Gates, Gate{
			Name: "overall_robustness", State: overall,
			Threshold: "all_required_gates_pass", Observed: overall, Reason: overallReason,
			Description: "evidence_only_not_promotion_or_deployment_authority",
		})
	}
}

func periodReturns(report *evaluation.Report) ([]float64, error) {
	if report == nil {
		return nil, fmt.Errorf("evaluation report is required")
	}
	observations := report.Observations()
	result := make([]float64, len(observations)-1)
	for index := 1; index < len(observations); index++ {
		prior, _ := decimal.NewFromString(observations[index-1].Equity)
		current, _ := decimal.NewFromString(observations[index].Equity)
		result[index-1] = current.Div(prior).Sub(decimal.NewFromInt(1)).InexactFloat64()
		if !finite(result[index-1]) {
			return nil, fmt.Errorf("evaluation period return numeric range exceeded")
		}
	}
	return result, nil
}

func bootstrap(policy *Policy, candidateID uuid.UUID, samples []float64) (float64, float64, float64) {
	seedBytes := sha256.Sum256(append(candidateID[:], byte(policy.canonical.BootstrapSeed), byte(policy.canonical.BootstrapSeed>>8),
		byte(policy.canonical.BootstrapSeed>>16), byte(policy.canonical.BootstrapSeed>>24), byte(policy.canonical.BootstrapSeed>>32),
		byte(policy.canonical.BootstrapSeed>>40), byte(policy.canonical.BootstrapSeed>>48), byte(policy.canonical.BootstrapSeed>>56)))
	rng := xorshift64star{state: binary.LittleEndian.Uint64(seedBytes[:8])}
	if rng.state == 0 {
		rng.state = 0x9e3779b97f4a7c15
	}
	means := make([]float64, policy.canonical.BootstrapIterations)
	nonpositive := 0
	for iteration := range means {
		total := 0.0
		for range samples {
			total += samples[rng.next()%uint64(len(samples))]
		}
		means[iteration] = total / float64(len(samples))
		if means[iteration] <= 0 {
			nonpositive++
		}
	}
	sort.Float64s(means)
	confidence := decimal.RequireFromString(policy.canonical.ConfidenceLevel).InexactFloat64()
	tail := (1 - confidence) / 2
	lowerIndex := int(math.Floor(tail * float64(len(means)-1)))
	upperIndex := int(math.Ceil((1 - tail) * float64(len(means)-1)))
	return means[lowerIndex], means[upperIndex], float64(nonpositive+1) / float64(len(means)+1)
}

type xorshift64star struct{ state uint64 }

func (rng *xorshift64star) next() uint64 {
	rng.state ^= rng.state >> 12
	rng.state ^= rng.state << 25
	rng.state ^= rng.state >> 27
	return rng.state * 2685821657736338717
}

func concentration(samples []float64) (float64, float64, bool) {
	positive := make([]float64, 0)
	total := 0.0
	for _, value := range samples {
		if value > 0 {
			positive = append(positive, value)
			total += value
		}
	}
	if len(positive) == 0 || total <= 0 {
		return 0, 0, false
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(positive)))
	count := int(math.Ceil(float64(len(positive)) * 0.1))
	top := 0.0
	for _, value := range positive[:count] {
		top += value
	}
	return positive[0] / total, top / total, true
}

func availableStatistic(name, value, unit, description string) Statistic {
	return Statistic{Name: name, State: "available", Value: value, Unit: unit, Description: description}
}

func unavailableStatistic(name, unit, reason, description string) Statistic {
	return Statistic{Name: name, State: "unavailable", Unit: unit, Reason: reason, Description: description}
}

func number(value float64, scale int32) string {
	return decimal.NewFromFloat(value).Round(scale).String()
}
func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
func mean(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}
