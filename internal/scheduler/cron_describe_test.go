package scheduler

import "testing"

func TestDescribeCron(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		// Specified examples.
		{"0 */4 * * 1-5", "Every 4 hours, Mon\u2013Fri"},
		{"30 10 * * 1-5", "Daily at 10:30 AM UTC, Mon\u2013Fri"},
		{"0 9 * * *", "Daily at 9:00 AM UTC"},
		{"*/5 * * * *", "Every 5 minutes"},
		{"0 0 * * 0", "Weekly on Sun at 12:00 AM UTC"},
		{"0 */4 * * *", "Every 4 hours"},
		{"", "Manual only"},

		// Edge cases.
		{"0 12 * * *", "Daily at 12:00 PM UTC"},
		{"0 13 * * *", "Daily at 1:00 PM UTC"},
		{"0 0 * * *", "Daily at 12:00 AM UTC"},
		{"*/1 * * * *", "Every 1 minutes"},
		{"0 */1 * * *", "Every 1 hours"},
		{"0 0 * * 0,6", "Daily at 12:00 AM UTC, Weekends"},
		{"0 9 * * 1,3,5", "Daily at 9:00 AM UTC, Mon, Wed, Fri"},
		{"0 9 * 3 *", "Daily at 9:00 AM UTC, March"},
		{"0 9 * * 6", "Weekly on Sat at 9:00 AM UTC"},

		// Unparseable: wrong field count.
		{"0 9 * *", "0 9 * *"},
		{"invalid", "invalid"},
		{"0 9 * * * *", "0 9 * * * *"},

		// Whitespace handling.
		{"  0 9 * * *  ", "Daily at 9:00 AM UTC"},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			got := DescribeCron(tc.expr)
			if got != tc.want {
				t.Errorf("DescribeCron(%q) = %q, want %q", tc.expr, got, tc.want)
			}
		})
	}
}
