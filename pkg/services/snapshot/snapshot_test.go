package snapshot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"time-machine/pkg/config"
	"time-machine/pkg/database"
	"time-machine/pkg/services/settings"
)

var mockServer *httptest.Server

func setupMockServer() {
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "snapshot") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("jpeg_image_data"))
		} else if strings.Contains(r.URL.Path, "cameras") {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"featureFlags": map[string]interface{}{
					"supportFullHdSnapshot": true,
				},
				"state":    "CONNECTED",
				"upSince":  float64(time.Now().UnixNano() / int64(time.Millisecond)),
				"modelKey": "UVC G5 Dome",
				"name":     "Test Camera",
			})
		}
	}))
}

func teardownMockServer() {
	mockServer.Close()
}

func setupTestDB(t *testing.T) {
	t.Helper()
	config.AppConfig.DataDir = t.TempDir()
	database.InitDB()
	settings.Init()
}

func TestInitSnapshotSettings(t *testing.T) {
	setupMockServer()
	defer teardownMockServer()
	setupTestDB(t)

	config.AppConfig.UFPHost = mockServer.URL
	config.AppConfig.UFPAPIKey = "test-key"
	config.AppConfig.TargetCameraID = "test-cam"

	// "auto" — mock server returns supportFullHdSnapshot=true, so HQ should be detected
	settings.Set("snapshot.hq_params", "auto")
	InitSnapshotSettings()
	assert.True(t, GetHQCapable(), "auto mode should detect HQ capability from mock camera")
	assert.True(t, isHighQualityEnabled())

	// Persist check: camera.hq_capable should be stored in the DB
	stored := settings.Get("camera.hq_capable", "")
	assert.Equal(t, "true", stored, "detected HQ capability should be persisted in DB")

	// "true" — forced on regardless of camera
	settings.Set("snapshot.hq_params", "true")
	InitSnapshotSettings()
	assert.True(t, isHighQualityEnabled())

	// "false" — forced off regardless of camera
	settings.Set("snapshot.hq_params", "false")
	InitSnapshotSettings()
	assert.False(t, isHighQualityEnabled())
}

func TestTakeSnapshot(t *testing.T) {
	setupMockServer()
	defer teardownMockServer()

	tempDir := t.TempDir()
	config.AppConfig.DataDir = tempDir
	config.AppConfig.SnapshotsDir = filepath.Join(tempDir, "snapshots")
	config.AppConfig.GalleryDir = filepath.Join(tempDir, "gallery")
	os.MkdirAll(config.AppConfig.SnapshotsDir, 0755)
	os.MkdirAll(config.AppConfig.GalleryDir, 0755)
	config.AppConfig.UFPHost = mockServer.URL
	config.AppConfig.UFPAPIKey = "test-key"
	config.AppConfig.TargetCameraID = "test-cam"

	// Force HQ on so the snapshot URL uses ?highQuality=true
	settings.Set("snapshot.hq_params", "true")
	hqCapable = true
	TakeSnapshot()

	now := time.Now()
	snapshotDir := filepath.Join(config.AppConfig.SnapshotsDir, now.Format("2006-01"), now.Format("02"), now.Format("15"))
	assert.DirExists(t, snapshotDir)

	galleryFileName := now.Format("2006-01-02-15") + ".jpg"
	galleryPath := filepath.Join(config.AppConfig.GalleryDir, galleryFileName)
	assert.FileExists(t, galleryPath)

	latestPath := filepath.Join(config.AppConfig.DataDir, "latest_snapshot.jpg")
	assert.FileExists(t, latestPath)
}

func TestGetCameraStatus(t *testing.T) {
	setupMockServer()
	defer teardownMockServer()

	config.AppConfig.UFPHost = mockServer.URL
	config.AppConfig.UFPAPIKey = "test-key"
	config.AppConfig.TargetCameraID = "test-cam"

	status := GetCameraStatus()
	assert.NotNil(t, status)
	assert.NotContains(t, status, "error")
	assert.Equal(t, "CONNECTED", status["state"])
}

func TestGetFormattedCameraStatus(t *testing.T) {
	setupMockServer()
	defer teardownMockServer()
	setupTestDB(t)

	config.AppConfig.UFPHost = mockServer.URL
	config.AppConfig.UFPAPIKey = "test-key"
	config.AppConfig.TargetCameraID = "test-cam"

	// Seed HQ setting and simulate an HQ-capable camera detected at startup
	settings.Set("snapshot.hq_params", "auto")
	hqCapable = true

	formattedStatus := GetFormattedCameraStatus()
	assert.NotNil(t, formattedStatus)
	assert.Equal(t, "CONNECTED", formattedStatus["Status"])
	assert.Equal(t, "Test Camera", formattedStatus["Name"])
	assert.Equal(t, "G5 Dome", formattedStatus["Model"])
	assert.Equal(t, "true", formattedStatus["Connected"])

	// HQ fields
	assert.Equal(t, "true", formattedStatus["HQCapable"])
	assert.Equal(t, "true", formattedStatus["HQEnabled"])
	assert.Equal(t, "auto", formattedStatus["HQSetting"])
	assert.Contains(t, formattedStatus["SnapshotQuality"], "High Quality")
}

func TestIsHighQualityEnabled(t *testing.T) {
	setupTestDB(t)

	hqCapable = true

	settings.Set("snapshot.hq_params", "true")
	assert.True(t, isHighQualityEnabled(), "forced true should enable HQ")

	settings.Set("snapshot.hq_params", "false")
	assert.False(t, isHighQualityEnabled(), "forced false should disable HQ")

	settings.Set("snapshot.hq_params", "auto")
	assert.True(t, isHighQualityEnabled(), "auto with hqCapable=true should enable HQ")

	hqCapable = false
	assert.False(t, isHighQualityEnabled(), "auto with hqCapable=false should disable HQ")
}

func TestGetEffectiveSnapshotQuality(t *testing.T) {
	setupTestDB(t)

	hqCapable = true

	settings.Set("snapshot.hq_params", "true")
	q := GetEffectiveSnapshotQuality()
	assert.Contains(t, q, "High Quality")
	assert.Contains(t, q, "forced")

	settings.Set("snapshot.hq_params", "false")
	q = GetEffectiveSnapshotQuality()
	assert.Contains(t, q, "Standard")
	assert.Contains(t, q, "forced")

	settings.Set("snapshot.hq_params", "auto")
	q = GetEffectiveSnapshotQuality()
	assert.Contains(t, q, "High Quality")
	assert.Contains(t, q, "auto")

	hqCapable = false
	q = GetEffectiveSnapshotQuality()
	assert.Contains(t, q, "Standard")
	assert.Contains(t, q, "auto")
}

func TestDetectAndPersistHQCapability_FallbackOnError(t *testing.T) {
	setupTestDB(t)

	// Pre-seed a stored capability value
	settings.Set("camera.hq_capable", "true")

	// Point to an unreachable host to force an error
	config.AppConfig.UFPHost = "http://127.0.0.1:1"
	config.AppConfig.UFPAPIKey = "test-key"
	config.AppConfig.TargetCameraID = "test-cam"

	hqCapable = false // reset
	detectAndPersistHQCapability()

	// Should fall back to the stored value
	assert.True(t, hqCapable, "should use last-known stored value when camera probe fails")
}
