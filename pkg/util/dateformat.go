package util

import (
	"strings"
	"time"

	"time-machine/pkg/config"
)

// getDateFormatString returns the Go format string based on configuration
func getDateFormatString() string {
	switch strings.ToUpper(config.AppConfig.DateFormat) {
	case "MM/DD/YYYY":
		return "01/02/2006"
	case "YYYY-MM-DD":
		return "2006-01-02"
	case "DD/MM/YYYY":
		fallthrough
	default:
		return "02/01/2006"
	}
}

// getTimeFormatString returns the Go time format string based on configuration
func getTimeFormatString() string {
	switch strings.ToLower(config.AppConfig.TimeFormat) {
	case "24h":
		return "15:04"
	case "12h":
		fallthrough
	default:
		return "03:04 PM"
	}
}

// FormatDate formats a time.Time according to the application's configured date format
func FormatDate(t time.Time) string {
	return t.Format(getDateFormatString())
}

// FormatTime formats a time.Time according to the application's configured time format
func FormatTime(t time.Time) string {
	return t.Format(getTimeFormatString())
}

// FormatDateTime formats a time.Time according to the application's configured date and time format
func FormatDateTime(t time.Time) string {
	format := getDateFormatString() + " " + getTimeFormatString()
	return t.Format(format)
}

// ParseDate attempts to parse a date string according to the configured format
func ParseDate(dateStr string) (time.Time, error) {
	return time.Parse(getDateFormatString(), dateStr)
}

// FormatDateForDisplay formats a date string (YYYY-MM-DD) for display in the UI
func FormatDateForDisplay(dateStr string) string {
	// If we have a date string in YYYY-MM-DD format, convert it to the configured format
	if len(dateStr) == 10 && strings.Count(dateStr, "-") == 2 {
		if t, err := time.Parse("2006-01-02", dateStr); err == nil {
			return FormatDate(t)
		}
	}
	return dateStr
}

// FormatRFC3339ForDisplay takes an RFC3339 string and returns a formatted date/time string
func FormatRFC3339ForDisplay(timeStr string) string {
	if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
		return FormatDateTime(t)
	}
	return timeStr
}
