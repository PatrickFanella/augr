package scheduler

import (
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestShouldFire(t *testing.T) {
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	tests := []struct {
		name string
		spec ScheduleSpec
		now  time.Time
		want bool
	}{
		{
			name: "market_hours Monday 10 AM ET",
			spec: ScheduleSpec{
				Type:         ScheduleTypeMarketHours,
				MarketType:   "stock",
				SkipWeekends: true,
				SkipHolidays: true,
			},
			now:  time.Date(2024, time.January, 8, 10, 0, 0, 0, et), // Monday
			want: true,
		},
		{
			name: "market_hours Saturday",
			spec: ScheduleSpec{
				Type:         ScheduleTypeMarketHours,
				MarketType:   "stock",
				SkipWeekends: true,
				SkipHolidays: true,
			},
			now:  time.Date(2024, time.January, 6, 10, 0, 0, 0, et), // Saturday
			want: false,
		},
		{
			name: "market_hours NYSE holiday",
			spec: ScheduleSpec{
				Type:         ScheduleTypeMarketHours,
				MarketType:   "stock",
				SkipWeekends: true,
				SkipHolidays: true,
			},
			now:  time.Date(2024, time.December, 25, 10, 0, 0, 0, et), // Christmas
			want: false,
		},
		{
			name: "pre_market Monday 9:15 AM ET",
			spec: ScheduleSpec{
				Type:         ScheduleTypePreMarket,
				MarketType:   "stock",
				SkipWeekends: true,
				SkipHolidays: true,
			},
			now:  time.Date(2024, time.January, 8, 9, 15, 0, 0, et),
			want: true,
		},
		{
			name: "pre_market Monday 10:00 AM ET",
			spec: ScheduleSpec{
				Type:         ScheduleTypePreMarket,
				MarketType:   "stock",
				SkipWeekends: true,
				SkipHolidays: true,
			},
			now:  time.Date(2024, time.January, 8, 10, 0, 0, 0, et),
			want: false,
		},
		{
			name: "market_close Monday 15:45 ET",
			spec: ScheduleSpec{
				Type:         ScheduleTypeMarketClose,
				MarketType:   "stock",
				SkipWeekends: true,
				SkipHolidays: true,
			},
			now:  time.Date(2024, time.January, 8, 15, 45, 0, 0, et),
			want: true,
		},
		{
			name: "market_close Monday 14:00 ET too early",
			spec: ScheduleSpec{
				Type:         ScheduleTypeMarketClose,
				MarketType:   "stock",
				SkipWeekends: true,
				SkipHolidays: true,
			},
			now:  time.Date(2024, time.January, 8, 14, 0, 0, 0, et),
			want: false,
		},
		{
			name: "after_hours Monday 17:00 ET",
			spec: ScheduleSpec{
				Type:         ScheduleTypeAfterHours,
				MarketType:   "stock",
				SkipWeekends: true,
				SkipHolidays: true,
			},
			now:  time.Date(2024, time.January, 8, 17, 0, 0, 0, et),
			want: true,
		},
		{
			name: "after_hours Monday 21:00 ET too late",
			spec: ScheduleSpec{
				Type:         ScheduleTypeAfterHours,
				MarketType:   "stock",
				SkipWeekends: true,
				SkipHolidays: true,
			},
			now:  time.Date(2024, time.January, 8, 21, 0, 0, 0, et),
			want: false,
		},
		{
			name: "crypto market_hours any time",
			spec: ScheduleSpec{
				Type:       ScheduleTypeMarketHours,
				MarketType: "crypto",
			},
			now:  time.Date(2024, time.January, 6, 3, 0, 0, 0, et), // Saturday 3 AM
			want: true,
		},
		{
			name: "crypto pre_market any time",
			spec: ScheduleSpec{
				Type:       ScheduleTypePreMarket,
				MarketType: "crypto",
			},
			now:  time.Date(2024, time.January, 6, 3, 0, 0, 0, et),
			want: true,
		},
		{
			name: "cron type always fires",
			spec: ScheduleSpec{
				Type: ScheduleTypeCron,
				Cron: "*/5 * * * *",
			},
			now:  time.Date(2024, time.January, 6, 3, 0, 0, 0, et),
			want: true,
		},
		{
			name: "polymarket after_hours any time",
			spec: ScheduleSpec{
				Type:       ScheduleTypeAfterHours,
				MarketType: "polymarket",
			},
			now:  time.Date(2024, time.January, 6, 22, 0, 0, 0, et),
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.spec.ShouldFire(tc.now)
			if got != tc.want {
				t.Errorf("ShouldFire(%v) = %v, want %v", tc.now.Format(time.RFC3339), got, tc.want)
			}
		})
	}
}

func TestParseScheduleSpec(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		marketType domain.MarketType
		wantType   ScheduleType
		wantCron   string
		wantSkipWE bool
		wantSkipH  bool
	}{
		{
			name:       "raw cron for stock",
			raw:        "0 */4 * * 1-5",
			marketType: domain.MarketTypeStock,
			wantType:   ScheduleTypeMarketHours,
			wantCron:   "0 */4 * * 1-5",
			wantSkipWE: true,
			wantSkipH:  true,
		},
		{
			name:       "raw cron for crypto",
			raw:        "*/5 * * * *",
			marketType: domain.MarketTypeCrypto,
			wantType:   ScheduleTypeCron,
			wantCron:   "*/5 * * * *",
			wantSkipWE: false,
			wantSkipH:  false,
		},
		{
			name:       "JSON input",
			raw:        `{"type":"pre_market","cron":"0 9 * * 1-5","market_type":"stock","skip_weekends":true,"skip_holidays":true}`,
			marketType: domain.MarketTypeStock,
			wantType:   ScheduleTypePreMarket,
			wantCron:   "0 9 * * 1-5",
			wantSkipWE: true,
			wantSkipH:  true,
		},
		{
			name:       "empty string",
			raw:        "",
			marketType: domain.MarketTypeStock,
			wantType:   ScheduleTypeCron,
			wantCron:   "",
			wantSkipWE: false,
			wantSkipH:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseScheduleSpec(tc.raw, tc.marketType)
			if got.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tc.wantType)
			}
			if got.Cron != tc.wantCron {
				t.Errorf("Cron = %q, want %q", got.Cron, tc.wantCron)
			}
			if got.SkipWeekends != tc.wantSkipWE {
				t.Errorf("SkipWeekends = %v, want %v", got.SkipWeekends, tc.wantSkipWE)
			}
			if got.SkipHolidays != tc.wantSkipH {
				t.Errorf("SkipHolidays = %v, want %v", got.SkipHolidays, tc.wantSkipH)
			}
		})
	}
}

func TestDescribe(t *testing.T) {
	tests := []struct {
		name string
		spec ScheduleSpec
		want string
	}{
		{
			name: "market hours every 4h",
			spec: ScheduleSpec{
				Type:         ScheduleTypeMarketHours,
				Cron:         "0 */4 * * 1-5",
				SkipHolidays: true,
			},
			want: "Every 4 hours, Mon\u2013Fri (market hours only), skip holidays",
		},
		{
			name: "pre_market daily",
			spec: ScheduleSpec{
				Type:         ScheduleTypePreMarket,
				Cron:         "0 9 * * 1-5",
				SkipHolidays: true,
			},
			want: "Daily at 9:00 AM UTC, Mon\u2013Fri (pre-market), skip holidays",
		},
		{
			name: "at market close",
			spec: ScheduleSpec{
				Type: ScheduleTypeMarketClose,
				Cron: "30 15 * * 1-5",
			},
			want: "Daily at 3:30 PM UTC, Mon\u2013Fri (at market close)",
		},
		{
			name: "after hours",
			spec: ScheduleSpec{
				Type: ScheduleTypeAfterHours,
				Cron: "0 16 * * 1-5",
			},
			want: "Daily at 4:00 PM UTC, Mon\u2013Fri (after hours)",
		},
		{
			name: "plain cron no suffix",
			spec: ScheduleSpec{
				Type: ScheduleTypeCron,
				Cron: "*/5 * * * *",
			},
			want: "Every 5 minutes",
		},
		{
			name: "empty cron manual",
			spec: ScheduleSpec{
				Type: ScheduleTypeCron,
			},
			want: "Manual only",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.spec.Describe()
			if got != tc.want {
				t.Errorf("Describe() = %q, want %q", got, tc.want)
			}
		})
	}
}
