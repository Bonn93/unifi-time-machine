package util

import (
	"testing"
	"time"

	"time-machine/pkg/config"
	"time-machine/pkg/database"
	"time-machine/pkg/services/settings"

	"github.com/stretchr/testify/assert"
)

func setupDateFormatTest(t *testing.T) {
	t.Helper()
	config.AppConfig.DataDir = t.TempDir()
	database.InitDB()
	settings.Init()
}

func TestFormatDate(t *testing.T) {
	setupDateFormatTest(t)

	timestamp := time.Date(2023, 10, 27, 15, 30, 0, 0, time.UTC)

	settings.Set("ui.date_format", "DD/MM/YYYY")
	settings.Invalidate()
	assert.Equal(t, "27/10/2023", FormatDate(timestamp))

	settings.Set("ui.date_format", "MM/DD/YYYY")
	settings.Invalidate()
	assert.Equal(t, "10/27/2023", FormatDate(timestamp))

	settings.Set("ui.date_format", "YYYY-MM-DD")
	settings.Invalidate()
	assert.Equal(t, "2023-10-27", FormatDate(timestamp))

	// Unknown format falls back to DD/MM/YYYY
	settings.Set("ui.date_format", "unknown")
	settings.Invalidate()
	assert.Equal(t, "27/10/2023", FormatDate(timestamp))
}

func TestFormatTime(t *testing.T) {
	setupDateFormatTest(t)

	timestamp := time.Date(2023, 10, 27, 15, 30, 0, 0, time.UTC)

	settings.Set("ui.time_format", "24h")
	settings.Invalidate()
	assert.Equal(t, "15:30", FormatTime(timestamp))

	settings.Set("ui.time_format", "12h")
	settings.Invalidate()
	assert.Equal(t, "03:30 PM", FormatTime(timestamp))

	// Unknown format falls back to 12h
	settings.Set("ui.time_format", "unknown")
	settings.Invalidate()
	assert.Equal(t, "03:30 PM", FormatTime(timestamp))
}

func TestFormatDateTime(t *testing.T) {
	setupDateFormatTest(t)

	timestamp := time.Date(2023, 10, 27, 15, 30, 0, 0, time.UTC)

	settings.Set("ui.date_format", "DD/MM/YYYY")
	settings.Set("ui.time_format", "24h")
	settings.Invalidate()
	assert.Equal(t, "27/10/2023 15:30", FormatDateTime(timestamp))

	settings.Set("ui.date_format", "YYYY-MM-DD")
	settings.Set("ui.time_format", "12h")
	settings.Invalidate()
	assert.Equal(t, "2023-10-27 03:30 PM", FormatDateTime(timestamp))
}

func TestParseDate(t *testing.T) {
	setupDateFormatTest(t)

	settings.Set("ui.date_format", "DD/MM/YYYY")
	settings.Invalidate()
	result, err := ParseDate("27/10/2023")
	assert.NoError(t, err)
	assert.Equal(t, time.Date(2023, 10, 27, 0, 0, 0, 0, time.UTC), result)

	settings.Set("ui.date_format", "YYYY-MM-DD")
	settings.Invalidate()
	result, err = ParseDate("2023-10-27")
	assert.NoError(t, err)
	assert.Equal(t, time.Date(2023, 10, 27, 0, 0, 0, 0, time.UTC), result)

	// Invalid date format
	_, err = ParseDate("invalid-date")
	assert.Error(t, err)
}

func TestFormatDateForDisplay(t *testing.T) {
	setupDateFormatTest(t)

	settings.Set("ui.date_format", "DD/MM/YYYY")
	settings.Invalidate()
	assert.Equal(t, "27/10/2023", FormatDateForDisplay("2023-10-27"))

	settings.Set("ui.date_format", "MM/DD/YYYY")
	settings.Invalidate()
	assert.Equal(t, "10/27/2023", FormatDateForDisplay("2023-10-27"))

	assert.Equal(t, "invalid-date", FormatDateForDisplay("invalid-date"))
}

func TestFormatRFC3339ForDisplay(t *testing.T) {
	setupDateFormatTest(t)

	settings.Set("ui.date_format", "DD/MM/YYYY")
	settings.Set("ui.time_format", "24h")
	settings.Invalidate()

	rfc3339Str := "2023-10-27T15:30:00Z"
	assert.Equal(t, "27/10/2023 15:30", FormatRFC3339ForDisplay(rfc3339Str))

	assert.Equal(t, "not-a-date", FormatRFC3339ForDisplay("not-a-date"))
}
