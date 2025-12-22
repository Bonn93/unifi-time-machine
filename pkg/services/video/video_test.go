package video

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"time-machine/pkg/config"
	"time-machine/pkg/models"
	"time-machine/pkg/util"
)

func setupTest(t *testing.T) (string, func()) {
	tempDir, err := os.MkdirTemp("", "video-test")
	assert.NoError(t, err)

	config.AppConfig.DataDir = tempDir
	config.AppConfig.SnapshotsDir = filepath.Join(tempDir, "snapshots")
	os.MkdirAll(config.AppConfig.SnapshotsDir, 0755)

	// Create some dummy snapshot files
	for i := 0; i < 5; i++ {
		now := time.Now().Add(-time.Duration(i) * time.Hour)
		snapshotDir := filepath.Join(config.AppConfig.SnapshotsDir, now.Format("2006-01"), now.Format("02"), now.Format("15"))
		os.MkdirAll(snapshotDir, 0755)
		dummyFile := filepath.Join(snapshotDir, now.Format("2006-01-02-15-04-05")+".jpg")
		os.WriteFile(dummyFile, []byte("dummy"), 0644)
	}

	return tempDir, func() {
		os.RemoveAll(tempDir)
	}
}

func TestReadWriteLastAppendedSnapshot(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	timelapseName := "test_timelapse"
	snapshotPath := "/path/to/snapshot.jpg"

	err := writeLastAppendedSnapshot(timelapseName, snapshotPath)
	assert.NoError(t, err)

	readPath, err := readLastAppendedSnapshot(timelapseName)
	assert.NoError(t, err)
	assert.Equal(t, snapshotPath, readPath)
}

func TestFilterSnapshots(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "video-filter-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Set config for this test only, and restore it afterward
	originalSnapshotsDir := config.AppConfig.SnapshotsDir
	config.AppConfig.SnapshotsDir = filepath.Join(tempDir, "snapshots")
	defer func() { config.AppConfig.SnapshotsDir = originalSnapshotsDir }()
	os.MkdirAll(config.AppConfig.SnapshotsDir, 0755)

	// Use a fixed time to make the test deterministic
	testTime := time.Date(2025, 12, 22, 12, 0, 0, 0, time.UTC)

	// Create files on disk
	for i := 0; i < 48; i++ {
		tm := testTime.Add(-time.Duration(i) * time.Hour)
		snapshotDir := filepath.Join(config.AppConfig.SnapshotsDir, tm.Format("2006-01"), tm.Format("02"), tm.Format("15"))
		os.MkdirAll(snapshotDir, 0755)
		dummyFile := filepath.Join(snapshotDir, tm.Format("2006-01-02-15-04-05")+".jpg")
		os.WriteFile(dummyFile, []byte("dummy"), 0644)
	}

	allFiles := util.GetSnapshotFiles()
	assert.Len(t, allFiles, 48) // Sanity check

	// --- Test "all" pattern ---
	cfg24h := models.TimelapseConfig{Duration: 24 * time.Hour, FramePattern: "all"}
	filtered24h := filterSnapshots(allFiles, cfg24h, testTime)
	assert.Len(t, filtered24h, 25, "should have 25 snapshots for 'all' in last 24h")

	// --- Test "hourly" pattern ---
	cfg1w := models.TimelapseConfig{Duration: 7 * 24 * time.Hour, FramePattern: "hourly"}
	filtered1w := filterSnapshots(allFiles, cfg1w, testTime)
	assert.Len(t, filtered1w, 48, "should have 48 snapshots for 'hourly' in last 48h")

	// --- Test "daily" pattern ---
	// With a fixed time, we know the 48 hour period spans 3 days
	cfg1m := models.TimelapseConfig{Duration: 30 * 24 * time.Hour, FramePattern: "daily"}
	filtered1m := filterSnapshots(allFiles, cfg1m, testTime)
	assert.Len(t, filtered1m, 3, "daily count should be 3 for the fixed time period")
}

func TestCleanupSnapshots(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	// Create an old file
	oldTime := time.Now().Add(-370 * 24 * time.Hour)
	oldDir := filepath.Join(config.AppConfig.SnapshotsDir, oldTime.Format("2006-01"), oldTime.Format("02"), oldTime.Format("15"))
	os.MkdirAll(oldDir, 0755)
	oldFile := filepath.Join(oldDir, oldTime.Format("2006-01-02-15-04-05")+".jpg")
	os.WriteFile(oldFile, []byte("old"), 0644)

	CleanupSnapshots()

	_, err := os.Stat(oldFile)
	assert.True(t, os.IsNotExist(err))
}

func TestCleanOldVideos(t *testing.T) {
	tempDir, cleanup := setupTest(t)
	defer cleanup()
	config.AppConfig.VideoArchivesToKeep = 1

	for i := 0; i < 3; i++ {
		filename := fmt.Sprintf("timelapse_24_hour_%d.webm", i)
		os.WriteFile(filepath.Join(tempDir, filename), []byte("dummy"), 0644)
	}

	CleanOldVideos()

	files, _ := filepath.Glob(filepath.Join(tempDir, "timelapse_24_hour_*.webm"))
	assert.Len(t, files, 1)
}

func TestCleanupLogFiles(t *testing.T) {
	tempDir, cleanup := setupTest(t)
	defer cleanup()

	// Create old log file
	oldLog := filepath.Join(tempDir, "ffmpeg_log_2020-01-01.txt")
	os.WriteFile(oldLog, []byte("old log"), 0644)

	// Create recent log file
	recentLog := filepath.Join(tempDir, "ffmpeg_log_"+time.Now().Format("2006-01-02")+".txt")
	os.WriteFile(recentLog, []byte("recent log"), 0644)

	CleanupLogFiles()

	_, err := os.Stat(oldLog)
	assert.True(t, os.IsNotExist(err), "Old log file should be deleted")
	_, err = os.Stat(recentLog)
	assert.False(t, os.IsNotExist(err), "Recent log file should not be deleted")
}
