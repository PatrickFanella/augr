package rules

import (
	"math"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// Snapshot holds resolved field values for the current bar. Values maps
// indicator names and OHLCV fields to their float64 values.
type Snapshot struct {
	Values map[string]float64
}

// NewSnapshotFromBar builds a Snapshot from indicator scalars and the current bar.
func NewSnapshotFromBar(indicators []domain.Indicator, bar domain.OHLCV) Snapshot {
	values := make(map[string]float64, len(indicators)+5)
	for _, ind := range indicators {
		values[ind.Name] = ind.Value
	}
	values["close"] = bar.Close
	values["open"] = bar.Open
	values["high"] = bar.High
	values["low"] = bar.Low
	values["volume"] = bar.Volume
	return Snapshot{Values: values}
}

// EvaluateGroup evaluates an AND/OR condition group. For cross_above/cross_below
// conditions, prev must be non-nil (the previous bar's snapshot).
func EvaluateGroup(group ConditionGroup, snap Snapshot, prev *Snapshot) bool {
	if len(group.Conditions) == 0 {
		return false
	}
	isAND := strings.ToUpper(group.Operator) == "AND"
	for _, cond := range group.Conditions {
		result := EvaluateCondition(cond, snap, prev)
		if isAND && !result {
			return false
		}
		if !isAND && result {
			return true
		}
	}
	return isAND
}

// EvaluateCondition evaluates a single condition. Returns false if fields are
// missing (insufficient data = don't trade).
func EvaluateCondition(cond Condition, snap Snapshot, prev *Snapshot) bool {
	fieldVal, ok := snap.Values[cond.Field]
	if !ok || math.IsNaN(fieldVal) {
		return false
	}

	var threshold float64
	if cond.Ref != "" {
		refVal, ok := snap.Values[cond.Ref]
		if !ok || math.IsNaN(refVal) {
			return false
		}
		threshold = refVal
	} else if cond.Value != nil {
		threshold = *cond.Value
	} else {
		return false
	}

	switch cond.Op {
	case "gt":
		return fieldVal > threshold
	case "gte":
		return fieldVal >= threshold
	case "lt":
		return fieldVal < threshold
	case "lte":
		return fieldVal <= threshold
	case "eq":
		return fieldVal == threshold
	case "cross_above":
		return crossAbove(cond, fieldVal, threshold, prev)
	case "cross_below":
		return crossBelow(cond, fieldVal, threshold, prev)
	default:
		return false
	}
}

func crossAbove(cond Condition, currentField, currentThreshold float64, prev *Snapshot) bool {
	if prev == nil {
		return false
	}
	prevField, ok := prev.Values[cond.Field]
	if !ok {
		return false
	}
	var prevThreshold float64
	if cond.Ref != "" {
		pt, ok := prev.Values[cond.Ref]
		if !ok {
			return false
		}
		prevThreshold = pt
	} else if cond.Value != nil {
		prevThreshold = *cond.Value
	}
	return currentField > currentThreshold && prevField <= prevThreshold
}

func crossBelow(cond Condition, currentField, currentThreshold float64, prev *Snapshot) bool {
	if prev == nil {
		return false
	}
	prevField, ok := prev.Values[cond.Field]
	if !ok {
		return false
	}
	var prevThreshold float64
	if cond.Ref != "" {
		pt, ok := prev.Values[cond.Ref]
		if !ok {
			return false
		}
		prevThreshold = pt
	} else if cond.Value != nil {
		prevThreshold = *cond.Value
	}
	return currentField < currentThreshold && prevField >= prevThreshold
}

// PassesFilters checks volume and ATR minimums.
func PassesFilters(filters *FilterConfig, snap Snapshot) bool {
	if filters == nil {
		return true
	}
	if filters.MinVolume > 0 {
		vol, ok := snap.Values["volume"]
		if !ok || vol < filters.MinVolume {
			return false
		}
	}
	if filters.MinATR > 0 {
		atr, ok := snap.Values["atr_14"]
		if !ok || atr < filters.MinATR {
			return false
		}
	}
	return true
}
