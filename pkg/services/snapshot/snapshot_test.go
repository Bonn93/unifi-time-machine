package snapshot

import (
	"bytes"
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

// fakeJPEGBody returns a byte slice of at least minSnapshotBytes that looks like
// camera data. Real content doesn't matter for unit tests — only the size does.
func fakeJPEGBody() []byte {
	return bytes.Repeat([]byte("X"), int(minSnapshotBytes)+100)
}

var mockServer *httptest.Server

func setupMockServer() {
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "snapshot") {
			w.WriteHeader(http.StatusOK)
			w.Write(fakeJPEGBody())
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
	ok := TakeSnapshot()
	assert.True(t, ok, "TakeSnapshot should return true when NVR returns a valid-sized body")

	now := time.Now()
	snapshotDir := filepath.Join(config.AppConfig.SnapshotsDir, now.Format("2006-01"), now.Format("02"), now.Format("15"))
	assert.DirExists(t, snapshotDir)

	galleryFileName := now.Format("2006-01-02-15") + ".jpg"
	galleryPath := filepath.Join(config.AppConfig.GalleryDir, galleryFileName)
	assert.FileExists(t, galleryPath)

	latestPath := filepath.Join(config.AppConfig.DataDir, "latest_snapshot.jpg")
	assert.FileExists(t, latestPath)
}

func setupSnapshotDirs(t *testing.T) {
	t.Helper()
	tempDir := t.TempDir()
	config.AppConfig.DataDir = tempDir
	config.AppConfig.SnapshotsDir = filepath.Join(tempDir, "snapshots")
	config.AppConfig.GalleryDir = filepath.Join(tempDir, "gallery")
	os.MkdirAll(config.AppConfig.SnapshotsDir, 0755)
	os.MkdirAll(config.AppConfig.GalleryDir, 0755)
}

func TestTakeSnapshot_EmptyBodyDiscarded(t *testing.T) {
	// NVR returns HTTP 200 with an empty body (camera offline but NVR is up).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// write nothing
	}))
	defer srv.Close()

	setupSnapshotDirs(t)
	config.AppConfig.UFPHost = srv.URL
	config.AppConfig.UFPAPIKey = "key"
	config.AppConfig.TargetCameraID = "cam"

	ok := TakeSnapshot()
	assert.False(t, ok, "empty response body should be rejected")

	// No snapshot file should remain on disk.
	snaps, _ := filepath.Glob(filepath.Join(config.AppConfig.SnapshotsDir, "*/*/*/*.jpg"))
	assert.Empty(t, snaps, "no snapshot file should be left after an empty-body rejection")
}

func TestTakeSnapshot_TinyBodyDiscarded(t *testing.T) {
	// NVR returns HTTP 200 with a body smaller than minSnapshotBytes.
	tinyBody := bytes.Repeat([]byte("x"), int(minSnapshotBytes)-1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(tinyBody)
	}))
	defer srv.Close()

	setupSnapshotDirs(t)
	config.AppConfig.UFPHost = srv.URL
	config.AppConfig.UFPAPIKey = "key"
	config.AppConfig.TargetCameraID = "cam"

	ok := TakeSnapshot()
	assert.False(t, ok, "below-threshold body should be rejected")

	snaps, _ := filepath.Glob(filepath.Join(config.AppConfig.SnapshotsDir, "*/*/*/*.jpg"))
	assert.Empty(t, snaps, "no snapshot file should be left after a size rejection")
}

func TestTakeSnapshot_NonOKStatusRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("service unavailable"))
	}))
	defer srv.Close()

	setupSnapshotDirs(t)
	config.AppConfig.UFPHost = srv.URL
	config.AppConfig.UFPAPIKey = "key"
	config.AppConfig.TargetCameraID = "cam"

	ok := TakeSnapshot()
	assert.False(t, ok, "non-200 status should be rejected")

	snaps, _ := filepath.Glob(filepath.Join(config.AppConfig.SnapshotsDir, "*/*/*/*.jpg"))
	assert.Empty(t, snaps, "no snapshot file should be created for a non-200 response")
}

func TestTakeSnapshot_ValidBodyAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(fakeJPEGBody())
	}))
	defer srv.Close()

	setupSnapshotDirs(t)
	database.InitDB()
	settings.Init()
	config.AppConfig.UFPHost = srv.URL
	config.AppConfig.UFPAPIKey = "key"
	config.AppConfig.TargetCameraID = "cam"

	ok := TakeSnapshot()
	assert.True(t, ok, "valid-sized body should be accepted")

	snaps, _ := filepath.Glob(filepath.Join(config.AppConfig.SnapshotsDir, "*/*/*/*.jpg"))
	assert.Len(t, snaps, 1, "exactly one snapshot file should be saved")

	info, err := os.Stat(snaps[0])
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, info.Size(), minSnapshotBytes, "saved snapshot must meet minimum size")
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
