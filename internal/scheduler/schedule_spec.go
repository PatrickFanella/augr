package scheduler

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// ScheduleType classifies when a schedule should fire relative to market sessions.
type ScheduleType string

const (
	ScheduleTypeCron        ScheduleType = "cron"
	ScheduleTypeMarketHours ScheduleType = "market_hours"
	ScheduleTypePreMarket   ScheduleType = "pre_market"
	ScheduleTypeMarketClose ScheduleType = "market_close"
	ScheduleTypeAfterHours  ScheduleType = "after_hours"
)

// ScheduleSpec describes when and under what market conditions a job should run.
type ScheduleSpec struct {
	Type         ScheduleType `json:"type"`
	Cron         string       `json:"cron,omitempty"`
	MarketType   string       `json:"market_type,omitempty"`
	SkipWeekends bool         `json:"skip_weekends,omitempty"`
	SkipHolidays bool         `json:"skip_holidays,omitempty"`
}

// Predefined schedule templates for common use cases.
var (
	ScheduleMarketHoursEvery4h = ScheduleSpec{
		Type:         ScheduleTypeMarketHours,
		Cron:         "0 */4 * * 1-5",
		MarketType:   string(domain.MarketTypeStock),
		SkipWeekends: true,
		SkipHolidays: true,
	}
	SchedulePreMarketDaily = ScheduleSpec{
		Type:         ScheduleTypePreMarket,
		Cron:         "0 9 * * 1-5",
		MarketType:   string(domain.MarketTypeStock),
		SkipWeekends: true,
		SkipHolidays: true,
	}
	ScheduleMarketCloseDaily = ScheduleSpec{
		Type:         ScheduleTypeMarketClose,
		Cron:         "30 15 * * 1-5",
		MarketType:   string(domain.MarketTypeStock),
		SkipWeekends: true,
		SkipHolidays: true,
	}
)

// ParseScheduleSpec parses a schedule string. If it starts with "{", it is
// treated as JSON. Otherwise it is a raw cron expression; stock market types
// default to market_hours with weekend/holiday skipping, while crypto and
// other always-open markets default to a plain cron type.
func ParseScheduleSpec(raw string, marketType domain.MarketType) ScheduleSpec {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ScheduleSpec{Type: ScheduleTypeCron}
	}

	if strings.HasPrefix(raw, "{") {
		var spec ScheduleSpec
		if err := json.Unmarshal([]byte(raw), &spec); err != nil {
			return ScheduleSpec{Type: ScheduleTypeCron, Cron: raw}
		}
		return spec
	}

	mt := marketType.Normalize()
	switch mt {
	case domain.MarketTypeCrypto, domain.MarketTypePolymarket:
		return ScheduleSpec{
			Type:       ScheduleTypeCron,
			Cron:       raw,
			MarketType: string(mt),
		}
	default:
		return ScheduleSpec{
			Type:         ScheduleTypeMarketHours,
			Cron:         raw,
			MarketType:   string(mt),
			SkipWeekends: true,
			SkipHolidays: true,
		}
	}
}

// ShouldFire returns true if the schedule should execute at the given time,
// taking into account weekend/holiday skipping and market session windows.
func (s ScheduleSpec) ShouldFire(now time.Time) bool {
	if s.SkipWeekends {
		wd := now.In(newYorkLocation).Weekday()
		if wd == time.Saturday || wd == time.Sunday {
			return false
		}
	}

	if s.SkipHolidays {
		et := now.In(newYorkLocation)
		if isNYSEHoliday(et) {
			return false
		}
	}

	mt := domain.MarketType(s.MarketType).Normalize()
	isAlwaysOpen := mt == domain.MarketTypeCrypto || mt == domain.MarketTypePolymarket

	switch s.Type {
	case ScheduleTypeCron:
		return true

	case ScheduleTypeMarketHours:
		if isAlwaysOpen {
			return true
		}
		return IsMarketOpen(now, mt)

	case ScheduleTypePreMarket:
		if isAlwaysOpen {
			return true
		}
		return IsPreMarket(now)

	case ScheduleTypeMarketClose:
		if isAlwaysOpen {
			return true
		}
		return IsNearMarketClose(now)

	case ScheduleTypeAfterHours:
		if isAlwaysOpen {
			return true
		}
		return IsAfterHours(now)

	default:
		return true
	}
}

// Describe returns a human-readable description including market constraints.
func (s ScheduleSpec) Describe() string {
	desc := DescribeCron(s.Cron)

	switch s.Type {
	case ScheduleTypeMarketHours:
		desc += " (market hours only)"
	case ScheduleTypePreMarket:
		desc += " (pre-market)"
	case ScheduleTypeMarketClose:
		desc += " (at market close)"
	case ScheduleTypeAfterHours:
		desc += " (after hours)"
	}

	if s.SkipHolidays {
		desc += ", skip holidays"
	}

	return desc
}
