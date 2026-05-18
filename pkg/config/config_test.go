package config

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadConfig(t *testing.T) {
	os.Clearenv()
	os.Setenv("UFP_API_KEY", "test_api_key")
	os.Setenv("TARGET_CAMERA_ID", "test_camera_id")
	os.Setenv("DATA_DIR", "/test/data")
	os.Setenv("APP_KEY", base64.StdEncoding.EncodeToString([]byte("test_app_key")))
	os.Setenv("ADMIN_PASSWORD", "test_admin_password")
	os.Setenv("SNAPSHOTS_DIR", "test_snapshots")
	os.Setenv("GALLERY_DIR", "test_gallery")
	os.Setenv("UFP_HOST", "testhost")

	LoadConfig()

	assert.Equal(t, "test_api_key", AppConfig.UFPAPIKey)
	assert.Equal(t, "test_camera_id", AppConfig.TargetCameraID)
	assert.Equal(t, "/test/data", AppConfig.DataDir)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("test_app_key")), AppConfig.AppKey)
	assert.Equal(t, "test_admin_password", AppConfig.AdminPassword)
	assert.True(t, strings.HasSuffix(AppConfig.SnapshotsDir, "test_snapshots"))
	assert.True(t, strings.HasSuffix(AppConfig.GalleryDir, "test_gallery"))
	assert.Equal(t, "https://testhost", AppConfig.UFPHost)

	// Test defaults
	os.Clearenv()
	os.Setenv("APP_KEY", base64.StdEncoding.EncodeToString([]byte("test_app_key")))
	LoadConfig()
	assert.Equal(t, "", AppConfig.UFPAPIKey)
	assert.Equal(t, "", AppConfig.TargetCameraID)
	assert.True(t, strings.HasSuffix(AppConfig.DataDir, "data"))
	assert.True(t, strings.HasSuffix(AppConfig.SnapshotsDir, "snapshots"))
	assert.True(t, strings.HasSuffix(AppConfig.GalleryDir, "gallery"))
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
