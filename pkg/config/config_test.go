package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetFFmpegLogPath(t *testing.T) {
	AppConfig.DataDir = "/tmp"
	expectedLogPath := filepath.Join("/tmp", "ffmpeg_log_"+time.Now().Format("2006-01-02")+".txt")
	assert.Equal(t, expectedLogPath, GetFFmpegLogPath())
}

func TestGetCRFValue(t *testing.T) {
	c := &Config{}

	c.VideoQuality = "low"
	assert.Equal(t, "35", c.GetCRFValue())

	c.VideoQuality = "medium"
	assert.Equal(t, "28", c.GetCRFValue())

	c.VideoQuality = "high"
	assert.Equal(t, "20", c.GetCRFValue())

	c.VideoQuality = "ultra"
	assert.Equal(t, "15", c.GetCRFValue())

	c.VideoQuality = "unknown"
	assert.Equal(t, "28", c.GetCRFValue())
}

func TestLoadConfig(t *testing.T) {
	// Unset all env vars
	os.Clearenv()

	// Set environment variables for testing
	os.Setenv("UFP_API_KEY", "test_api_key")
	os.Setenv("TARGET_CAMERA_ID", "test_camera_id")
	os.Setenv("DATA_DIR", "/test/data")
	os.Setenv("TIMELAPSE_INTERVAL", "1800")
	os.Setenv("VIDEO_CRON_INTERVAL", "600")
	os.Setenv("VIDEO_ARCHIVES_TO_KEEP", "5")
	os.Setenv("APP_KEY", base64.StdEncoding.EncodeToString([]byte("test_app_key")))
	os.Setenv("ADMIN_PASSWORD", "test_admin_password")
	os.Setenv("VIDEO_QUALITY", "high")
	os.Setenv("SNAPSHOTS_DIR", "test_snapshots")
	os.Setenv("GALLERY_DIR", "test_gallery")
	os.Setenv("HQSNAP", "high_quality")
	os.Setenv("UFP_HOST", "testhost")

	LoadConfig()

	assert.Equal(t, "test_api_key", AppConfig.UFPAPIKey)
	assert.Equal(t, "test_camera_id", AppConfig.TargetCameraID)
	assert.Equal(t, "/test/data", AppConfig.DataDir)
	assert.Equal(t, 1800, AppConfig.SnapshotIntervalSec)
	assert.Equal(t, 600, AppConfig.VideoCronIntervalSec)
	assert.Equal(t, 5, AppConfig.VideoArchivesToKeep)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("test_app_key")), AppConfig.AppKey)
	assert.Equal(t, "test_admin_password", AppConfig.AdminPassword)
	assert.Equal(t, "high", AppConfig.VideoQuality)
	assert.True(t, strings.HasSuffix(AppConfig.SnapshotsDir, "test_snapshots"))
	assert.True(t, strings.HasSuffix(AppConfig.GalleryDir, "test_gallery"))
	assert.Equal(t, "https://testhost", AppConfig.UFPHost)
	assert.Equal(t, "high_quality", AppConfig.HQSnapParams)

	// Test default values
	os.Clearenv()
	os.Setenv("APP_KEY", base64.StdEncoding.EncodeToString([]byte("test_app_key")))
	LoadConfig()
	assert.Equal(t, "", AppConfig.UFPAPIKey)
	assert.Equal(t, "", AppConfig.TargetCameraID)
	assert.Equal(t, "data", AppConfig.DataDir)
	assert.Equal(t, 3600, AppConfig.SnapshotIntervalSec)
	assert.Equal(t, 300, AppConfig.VideoCronIntervalSec)
	assert.Equal(t, 3, AppConfig.VideoArchivesToKeep)
	assert.Equal(t, "medium", AppConfig.VideoQuality)
	assert.True(t, strings.HasSuffix(AppConfig.SnapshotsDir, "snapshots"))
	assert.True(t, strings.HasSuffix(AppConfig.GalleryDir, "gallery"))
	assert.Equal(t, "auto", AppConfig.HQSnapParams)
}

func TestGetEnvAsInt(t *testing.T) {
	os.Setenv("TEST_INT", "123")
	val := getEnvAsInt("TEST_INT", 456)
	assert.Equal(t, 123, val)

	os.Unsetenv("TEST_INT")
	val = getEnvAsInt("TEST_INT", 456)
	assert.Equal(t, 456, val)

	os.Setenv("TEST_INT", "abc")
	val = getEnvAsInt("TEST_INT", 456)
	assert.Equal(t, 456, val)
}
