package scheduler

import (
	"fmt"
	"strconv"
	"strings"
)

// DescribeCron converts a 5-field cron expression to human-readable English.
// It handles common patterns and falls back to the raw expression if unparseable.
func DescribeCron(expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "Manual only"
	}

	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return expr
	}

	minute, hour, _, month, dow := fields[0], fields[1], fields[2], fields[3], fields[4]

	var parts []string

	// Build the time/frequency part.
	switch {
	case strings.HasPrefix(minute, "*/"):
		n, err := strconv.Atoi(minute[2:])
		if err != nil {
			return expr
		}
		parts = append(parts, fmt.Sprintf("Every %d minutes", n))

	case strings.HasPrefix(hour, "*/"):
		n, err := strconv.Atoi(hour[2:])
		if err != nil {
			return expr
		}
		parts = append(parts, fmt.Sprintf("Every %d hours", n))

	case isNumeric(minute) && isNumeric(hour):
		h, _ := strconv.Atoi(hour)
		m, _ := strconv.Atoi(minute)
		parts = append(parts, fmt.Sprintf("Daily at %s UTC", formatTime12(h, m)))

	default:
		return expr
	}

	// Day-of-week suffix.
	if dowStr := describeDOW(dow); dowStr != "" {
		parts = append(parts, dowStr)
	}

	// Month suffix.
	if monthStr := describeMonth(month); monthStr != "" {
		parts = append(parts, monthStr)
	}

	result := strings.Join(parts, ", ")

	// Upgrade "Daily at ... , Sun" → "Weekly on Sun at ..."
	if isNumeric(minute) && isNumeric(hour) && isSingleDOW(dow) {
		h, _ := strconv.Atoi(hour)
		m, _ := strconv.Atoi(minute)
		dayName := describeDOW(dow)
		result = fmt.Sprintf("Weekly on %s at %s UTC", dayName, formatTime12(h, m))
		if monthStr := describeMonth(month); monthStr != "" {
			result += ", " + monthStr
		}
	}

	return result
}

func isNumeric(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func isSingleDOW(dow string) bool {
	if dow == "*" {
		return false
	}
	_, err := strconv.Atoi(dow)
	return err == nil
}

var dayNames = [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

func describeDOW(dow string) string {
	if dow == "*" {
		return ""
	}
	switch dow {
	case "1-5":
		return "Mon\u2013Fri"
	case "0,6":
		return "Weekends"
	}

	// Comma-separated list or single value.
	parts := strings.Split(dow, ",")
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 || n > 6 {
			return dow
		}
		names = append(names, dayNames[n])
	}
	return strings.Join(names, ", ")
}

func describeMonth(month string) string {
	if month == "*" {
		return ""
	}
	n, err := strconv.Atoi(month)
	if err != nil || n < 1 || n > 12 {
		return month
	}
	monthNames := [12]string{
		"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December",
	}
	return monthNames[n-1]
}

func formatTime12(hour, minute int) string {
	period := "AM"
	h := hour
	switch {
	case h == 0:
		h = 12
	case h == 12:
		period = "PM"
	case h > 12:
		h -= 12
		period = "PM"
	}
	return fmt.Sprintf("%d:%02d %s", h, minute, period)
}
