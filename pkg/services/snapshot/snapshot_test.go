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

	// Test "auto" — mock server returns supportFullHdSnapshot=true
	settings.Set("snapshot.hq_params", "auto")
	InitSnapshotSettings()
	assert.True(t, useHighQuality)

	// Test "true"
	settings.Set("snapshot.hq_params", "true")
	InitSnapshotSettings()
	assert.True(t, useHighQuality)

	// Test "false"
	settings.Set("snapshot.hq_params", "false")
	InitSnapshotSettings()
	assert.False(t, useHighQuality)
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

	useHighQuality = true
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

	config.AppConfig.UFPHost = mockServer.URL
	config.AppConfig.UFPAPIKey = "test-key"
	config.AppConfig.TargetCameraID = "test-cam"

	formattedStatus := GetFormattedCameraStatus()
	assert.NotNil(t, formattedStatus)
	assert.Equal(t, "CONNECTED", formattedStatus["Status"])
	assert.Equal(t, "Test Camera", formattedStatus["Name"])
	assert.Equal(t, "G5 Dome", formattedStatus["Model"])
	assert.Equal(t, "true", formattedStatus["Connected"])
}
