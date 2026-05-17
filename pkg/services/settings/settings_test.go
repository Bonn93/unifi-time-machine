package settings

import (
	"os"
	"testing"

	"time-machine/pkg/config"
	"time-machine/pkg/database"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	config.AppConfig.DataDir = t.TempDir()
	database.InitDB()
}

func TestGetSet(t *testing.T) {
	setupTestDB(t)
	Init()

	// Default value returned when key is absent
	assert.Equal(t, "fallback", Get("nonexistent.key", "fallback"))

	// Set then Get round-trip
	require.NoError(t, Set("test.key", "hello"))
	assert.Equal(t, "hello", Get("test.key", "default"))

	// GetInt conversion
	require.NoError(t, Set("test.int", "42"))
	assert.Equal(t, 42, GetInt("test.int", 0))
	assert.Equal(t, 99, GetInt("nonexistent.int", 99))
}

func TestInitSeeding_EnvVar(t *testing.T) {
	setupTestDB(t)
	t.Setenv("TIMELAPSE_INTERVAL", "7200")
	Init()

	val := Get("snapshot.interval_sec", "")
	assert.Equal(t, "7200", val, "env var should be seeded into DB on Init")
}

func TestInitSeeding_Default(t *testing.T) {
	setupTestDB(t)
	os.Unsetenv("TIMELAPSE_INTERVAL")
	Init()

	val := Get("snapshot.interval_sec", "")
	assert.Equal(t, "3600", val, "default value should be seeded when env var is absent")
}

func TestInitSeeding_DoesNotOverwrite(t *testing.T) {
	setupTestDB(t)
	Init()

	// Manually change the value
	require.NoError(t, Set("snapshot.interval_sec", "999"))
	Invalidate()

	// Re-run Init — should NOT overwrite the existing value
	Init()
	assert.Equal(t, "999", Get("snapshot.interval_sec", ""), "Init must not overwrite existing settings")
}

func TestCacheInvalidation(t *testing.T) {
	setupTestDB(t)
	Init()

	require.NoError(t, Set("video.quality", "high"))
	// Set calls Invalidate internally; next Get reloads
	assert.Equal(t, "high", Get("video.quality", ""))

	// Direct invalidate forces reload on next Get
	require.NoError(t, Set("video.quality", "ultra"))
	Invalidate()
	assert.Equal(t, "ultra", Get("video.quality", ""))
}

func TestGetCRFForQuality(t *testing.T) {
	assert.Equal(t, "35", GetCRFForQuality("low"))
	assert.Equal(t, "28", GetCRFForQuality("medium"))
	assert.Equal(t, "20", GetCRFForQuality("high"))
	assert.Equal(t, "15", GetCRFForQuality("ultra"))
	assert.Equal(t, "28", GetCRFForQuality("unknown"))
}

func TestGetAll(t *testing.T) {
	setupTestDB(t)
	Init()

	all, err := GetAll()
	require.NoError(t, err)
	assert.Contains(t, all, "snapshot.interval_sec")
	assert.Contains(t, all, "video.format")
}
