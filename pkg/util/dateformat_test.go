package util

import (
	"testing"
	"time"

	"time-machine/pkg/config"

	"github.com/stretchr/testify/assert"
)

func TestFormatDate(t *testing.T) {
	// Save original config
	originalDateFormat := config.AppConfig.DateFormat
	defer func() { config.AppConfig.DateFormat = originalDateFormat }()

	timestamp := time.Date(2023, 10, 27, 15, 30, 0, 0, time.UTC)

	// Test DD/MM/YYYY format
	config.AppConfig.DateFormat = "DD/MM/YYYY"
	assert.Equal(t, "27/10/2023", FormatDate(timestamp))

	// Test MM/DD/YYYY format
	config.AppConfig.DateFormat = "MM/DD/YYYY"
	assert.Equal(t, "10/27/2023", FormatDate(timestamp))

	// Test YYYY-MM-DD format
	config.AppConfig.DateFormat = "YYYY-MM-DD"
	assert.Equal(t, "2023-10-27", FormatDate(timestamp))

	// Test unknown format (should default to DD/MM/YYYY)
	config.AppConfig.DateFormat = "unknown"
	assert.Equal(t, "27/10/2023", FormatDate(timestamp))
}

func TestFormatTime(t *testing.T) {
	// Save original config
	originalTimeFormat := config.AppConfig.TimeFormat
	defer func() { config.AppConfig.TimeFormat = originalTimeFormat }()

	timestamp := time.Date(2023, 10, 27, 15, 30, 0, 0, time.UTC)

	// Test 24h format
	config.AppConfig.TimeFormat = "24h"
	assert.Equal(t, "15:30", FormatTime(timestamp))

	// Test 12h format
	config.AppConfig.TimeFormat = "12h"
	assert.Equal(t, "03:30 PM", FormatTime(timestamp))

	// Test unknown format (should default to 12h)
	config.AppConfig.TimeFormat = "unknown"
	assert.Equal(t, "03:30 PM", FormatTime(timestamp))
}

func TestFormatDateTime(t *testing.T) {
	originalDateFormat := config.AppConfig.DateFormat
	originalTimeFormat := config.AppConfig.TimeFormat
	defer func() {
		config.AppConfig.DateFormat = originalDateFormat
		config.AppConfig.TimeFormat = originalTimeFormat
	}()

	timestamp := time.Date(2023, 10, 27, 15, 30, 0, 0, time.UTC)

	config.AppConfig.DateFormat = "DD/MM/YYYY"
	config.AppConfig.TimeFormat = "24h"
	assert.Equal(t, "27/10/2023 15:30", FormatDateTime(timestamp))

	config.AppConfig.DateFormat = "YYYY-MM-DD"
	config.AppConfig.TimeFormat = "12h"
	assert.Equal(t, "2023-10-27 03:30 PM", FormatDateTime(timestamp))
}

func TestParseDate(t *testing.T) {
	originalDateFormat := config.AppConfig.DateFormat
	defer func() { config.AppConfig.DateFormat = originalDateFormat }()

	config.AppConfig.DateFormat = "DD/MM/YYYY"
	result, err := ParseDate("27/10/2023")
	assert.NoError(t, err)
	assert.Equal(t, time.Date(2023, 10, 27, 0, 0, 0, 0, time.UTC), result)

	config.AppConfig.DateFormat = "YYYY-MM-DD"
	result, err = ParseDate("2023-10-27")
	assert.NoError(t, err)
	assert.Equal(t, time.Date(2023, 10, 27, 0, 0, 0, 0, time.UTC), result)

	// Test invalid date format
	_, err = ParseDate("invalid-date")
	assert.Error(t, err)
}

func TestFormatDateForDisplay(t *testing.T) {
	originalDateFormat := config.AppConfig.DateFormat
	defer func() { config.AppConfig.DateFormat = originalDateFormat }()

	config.AppConfig.DateFormat = "DD/MM/YYYY"
	assert.Equal(t, "27/10/2023", FormatDateForDisplay("2023-10-27"))

	config.AppConfig.DateFormat = "MM/DD/YYYY"
	assert.Equal(t, "10/27/2023", FormatDateForDisplay("2023-10-27"))

	assert.Equal(t, "invalid-date", FormatDateForDisplay("invalid-date"))
}

func TestFormatRFC3339ForDisplay(t *testing.T) {
	originalDateFormat := config.AppConfig.DateFormat
	originalTimeFormat := config.AppConfig.TimeFormat
	defer func() {
		config.AppConfig.DateFormat = originalDateFormat
		config.AppConfig.TimeFormat = originalTimeFormat
	}()

	config.AppConfig.DateFormat = "DD/MM/YYYY"
	config.AppConfig.TimeFormat = "24h"

	// Valid RFC3339
	rfc3339Str := "2023-10-27T15:30:00Z"
	assert.Equal(t, "27/10/2023 15:30", FormatRFC3339ForDisplay(rfc3339Str))

	// Invalid fallback
	assert.Equal(t, "not-a-date", FormatRFC3339ForDisplay("not-a-date"))
}
