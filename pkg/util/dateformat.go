package util

import (
	"strings"
	"time"

	"time-machine/pkg/services/settings"
)

func getDateFormatString() string {
	switch strings.ToUpper(settings.Get("ui.date_format", "DD/MM/YYYY")) {
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

func getTimeFormatString() string {
	switch strings.ToLower(settings.Get("ui.time_format", "12h")) {
	case "24h":
		return "15:04"
	case "12h":
		fallthrough
	default:
		return "03:04 PM"
	}
}

func FormatDate(t time.Time) string {
	return t.Format(getDateFormatString())
}

func FormatTime(t time.Time) string {
	return t.Format(getTimeFormatString())
}

func FormatDateTime(t time.Time) string {
	format := getDateFormatString() + " " + getTimeFormatString()
	return t.Format(format)
}

func ParseDate(dateStr string) (time.Time, error) {
	return time.Parse(getDateFormatString(), dateStr)
}

func FormatDateForDisplay(dateStr string) string {
	if len(dateStr) == 10 && strings.Count(dateStr, "-") == 2 {
		if t, err := time.Parse("2006-01-02", dateStr); err == nil {
			return FormatDate(t)
		}
	}
	return dateStr
}

func FormatRFC3339ForDisplay(timeStr string) string {
	if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
		return FormatDateTime(t)
	}
	return timeStr
}
