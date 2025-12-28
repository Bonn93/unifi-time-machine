package video

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"time-machine/pkg/config"
	"time-machine/pkg/jobs"
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

	originalSnapshotsDir := config.AppConfig.SnapshotsDir
	config.AppConfig.SnapshotsDir = filepath.Join(tempDir, "snapshots")
	defer func() { config.AppConfig.SnapshotsDir = originalSnapshotsDir }()
	os.MkdirAll(config.AppConfig.SnapshotsDir, 0755)

	// Use a fixed time to make the test deterministic
	testTime := time.Date(2025, 12, 22, 12, 0, 0, 0, time.UTC) // Noon on Dec 22nd, 2025

	// Create snapshots for a few days around testTime
	// Create snapshots for Dec 21st, 22nd, and 23rd, one per hour
	for dayOffset := -1; dayOffset <= 1; dayOffset++ { // -1 = Dec 21, 0 = Dec 22, 1 = Dec 23
		currentDay := testTime.AddDate(0, 0, dayOffset)
		for hour := 0; hour < 24; hour++ {
			tm := time.Date(currentDay.Year(), currentDay.Month(), currentDay.Day(), hour, 0, 0, 0, time.UTC)
			snapshotDir := filepath.Join(config.AppConfig.SnapshotsDir, tm.Format("2006-01"), tm.Format("02"), tm.Format("15"))
			os.MkdirAll(snapshotDir, 0755)
			dummyFile := filepath.Join(snapshotDir, tm.Format("2006-01-02-15-04-05")+".jpg")
			os.WriteFile(dummyFile, []byte("dummy"), 0644)
		}
	}

	allFiles := util.GetSnapshotFiles()
	assert.Len(t, allFiles, 3*24, "should have 72 snapshots for 3 days") // 24 hours * 3 days

	// --- Test "all" pattern for a specific day (Dec 22nd) ---
	// This simulates a dynamically generated 24-hour timelapse for Dec 22nd
	cfgDaily22 := models.TimelapseConfig{Name: "24_hour_2025-12-22", Duration: 24 * time.Hour, FramePattern: "all"}
	// targetTime for filterSnapshots in this case is still testTime (Dec 22nd 12:00),
	// but the logic inside filterSnapshots should truncate it to Dec 22nd 00:00 for windowStart.
	filteredDaily22 := filterSnapshots(allFiles, cfgDaily22, testTime)
	assert.Len(t, filteredDaily22, 24, "should have 24 snapshots for Dec 22nd (00:00-23:00)")

	// --- Test "all" pattern for last 24h (old behavior, 24 hours back from testTime) ---
	cfg24hOld := models.TimelapseConfig{Duration: 24 * time.Hour, FramePattern: "all"}
	// testTime is Dec 22nd 12:00. Cutoff is Dec 21st 12:00.
	// Snapshots from Dec 21st 12:00:00 to Dec 22nd 11:00:00 should be included.
	// (24 - 12) hours from Dec 21st + 12 hours from Dec 22nd = 12 + 12 = 24.
	// But because of how `filterSnapshots` uses `!fileTime.Before(cutoff) && fileTime.Before(windowEnd)`,
	// if a snapshot is at `cutoff` (12:00:00), it's included. If `testTime` is 12:00:00,
	// `testTime.Add(-24*time.Hour)` is 12:00:00 the previous day. So it's 24 hours plus the start hour.
	// In my setup, I generate snapshots for 00:00, 01:00, ..., 23:00.
	// So from 12:00 Dec 21 to 12:00 Dec 22:
	// Dec 21: 12:00, 13:00, ..., 23:00 (12 snapshots)
	// Dec 22: 00:00, 01:00, ..., 11:00 (12 snapshots)
	// Total = 24.
	// The original test said 25. Let's trace it carefully.
	// The `filterSnapshots` function's `windowEnd` is `targetTime` itself for non-daily.
	// So `!fileTime.Before(cutoff) && fileTime.Before(targetTime)`.
	// cutoff = Dec 21 12:00:00. targetTime = Dec 22 12:00:00.
	// So it should include [Dec 21 12:00:00, Dec 22 12:00:00).
	// This means Dec 21 12:00, ..., 23:00 (12 snapshots) AND Dec 22 00:00, ..., 11:00 (12 snapshots). Total 24.
	// The original test was probably off by one or had different snapshot generation.
	filtered24hOld := filterSnapshots(allFiles, cfg24hOld, testTime)
	assert.Len(t, filtered24hOld, 24, "should have 24 snapshots for 'all' in last 24h (12:00-12:00)")

	// --- Test "hourly" pattern ---
	cfg1w := models.TimelapseConfig{Duration: 7 * 24 * time.Hour, FramePattern: "hourly"}
	// testTime is Dec 22nd 12:00. Cutoff is Dec 15th 12:00.
	// Snapshots exist from Dec 21 00:00 to Dec 23 23:00.
	// So it should pick one hourly snapshot from the window [Dec 21 00:00, Dec 22 12:00).
	// This is 24 hours from Dec 21st + 12 hours from Dec 22nd = 36 unique hourly snapshots.
	filtered1w := filterSnapshots(allFiles, cfg1w, testTime)
	assert.Len(t, filtered1w, 36, "should have 36 snapshots for 'hourly' from Dec 21 00:00 to Dec 22 12:00")

	// --- Test "daily" pattern ---
	cfg1m := models.TimelapseConfig{Duration: 30 * 24 * time.Hour, FramePattern: "daily"}
	// testTime is Dec 22nd 12:00. Cutoff is Nov 22nd 12:00.
	// Snapshots exist for Dec 21, Dec 22, Dec 23.
	// It should pick the first snapshot of each day within the window [Nov 22 12:00, Dec 22 12:00).
	// This means it should pick one for Dec 21, and one for Dec 22.
	filtered1m := filterSnapshots(allFiles, cfg1m, testTime)
	assert.Len(t, filtered1m, 2, "daily count should be 2 for the fixed time period (Dec 21, Dec 22)")
}

func TestCleanupSnapshots(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	// Set a specific retention period for this test
	originalRetentionDays := config.AppConfig.SnapshotRetentionDays
	config.AppConfig.SnapshotRetentionDays = 10
	defer func() { config.AppConfig.SnapshotRetentionDays = originalRetentionDays }()

	// Create an old file that should be deleted
	oldTime := time.Now().Add(-11 * 24 * time.Hour)
	oldDir := filepath.Join(config.AppConfig.SnapshotsDir, oldTime.Format("2006-01"), oldTime.Format("02"), oldTime.Format("15"))
	os.MkdirAll(oldDir, 0755)
	oldFile := filepath.Join(oldDir, oldTime.Format("2006-01-02-15-04-05")+".jpg")
	os.WriteFile(oldFile, []byte("old"), 0644)

	// Create a newer file that should be kept
	newTime := time.Now().Add(-5 * 24 * time.Hour)
	newDir := filepath.Join(config.AppConfig.SnapshotsDir, newTime.Format("2006-01"), newTime.Format("02"), newTime.Format("15"))
	os.MkdirAll(newDir, 0755)
	newFile := filepath.Join(newDir, newTime.Format("2006-01-02-15-04-05")+".jpg")
	os.WriteFile(newFile, []byte("new"), 0644)

	// Create a malformed file that should be skipped and kept
	malformedFile := filepath.Join(config.AppConfig.SnapshotsDir, "malformed-file.jpg")
	os.WriteFile(malformedFile, []byte("malformed"), 0644)

	CleanupSnapshots()

	// Assert old file is deleted
	_, err := os.Stat(oldFile)
	assert.True(t, os.IsNotExist(err), "Old snapshot file should be deleted")

	// Assert new file still exists
	_, err = os.Stat(newFile)
	assert.False(t, os.IsNotExist(err), "New snapshot file should not be deleted")

	// Assert malformed file still exists
	_, err = os.Stat(malformedFile)
	assert.False(t, os.IsNotExist(err), "Malformed snapshot file should not be deleted")
}

func TestCleanOldVideos(t *testing.T) {
	tempDir, cleanup := setupTest(t)
	defer cleanup()

	// --- Test Daily 24-Hour Timelapses Cleanup ---
	originalDaysOf24HourSnapshots := config.AppConfig.DaysOf24HourSnapshots
	config.AppConfig.DaysOf24HourSnapshots = 2 // Keep last 2 full days of daily timelapses (e.g., today and yesterday)
	defer func() { config.AppConfig.DaysOf24HourSnapshots = originalDaysOf24HourSnapshots }()

	today := time.Now().Truncate(24 * time.Hour)
	yesterday := today.AddDate(0, 0, -1)
	twoDaysAgo := today.AddDate(0, 0, -2)
	threeDaysAgo := today.AddDate(0, 0, -3)

	// Create daily video files
	createDailyVideo := func(date time.Time) {
		filename := fmt.Sprintf("timelapse_24_hour_%s.webm", date.Format("2006-01-02"))
		os.WriteFile(filepath.Join(tempDir, filename), []byte("dummy"), 0644)
	}

	createDailyVideo(today)        // Should be kept
	createDailyVideo(yesterday)    // Should be kept
	createDailyVideo(twoDaysAgo)   // Should be kept (as cutoffDate is exclusive 'before')
	createDailyVideo(threeDaysAgo) // Should be deleted

	CleanOldVideos()

	// Check remaining daily videos
	files, _ := filepath.Glob(filepath.Join(tempDir, "timelapse_24_hour_*.webm"))
	sort.Strings(files) // Ensure consistent order for assertions
	expectedFiles := []string{
		filepath.Join(tempDir, fmt.Sprintf("timelapse_24_hour_%s.webm", twoDaysAgo.Format("2006-01-02"))),
		filepath.Join(tempDir, fmt.Sprintf("timelapse_24_hour_%s.webm", yesterday.Format("2006-01-02"))),
		filepath.Join(tempDir, fmt.Sprintf("timelapse_24_hour_%s.webm", today.Format("2006-01-02"))),
	}
	assert.ElementsMatch(t, expectedFiles, files, "should keep today, yesterday, and two days ago's daily videos")
	assert.Len(t, files, 3)

	// --- Test Other Timelapse Cleanup (e.g., 1_week, 1_month, etc.) ---
	originalVideoArchivesToKeep := config.AppConfig.VideoArchivesToKeep
	config.AppConfig.VideoArchivesToKeep = 1
	defer func() { config.AppConfig.VideoArchivesToKeep = originalVideoArchivesToKeep }()

	originalTimelapseConfigsData := models.TimelapseConfigsData
	models.TimelapseConfigsData = []models.TimelapseConfig{
		{Name: "1_week"},
	}
	defer func() { models.TimelapseConfigsData = originalTimelapseConfigsData }()


	for i := 0; i < 3; i++ {
		filename := fmt.Sprintf("timelapse_1_week_%d.webm", i)
		os.WriteFile(filepath.Join(tempDir, filename), []byte("dummy"), 0644)
	}

	CleanOldVideos()

	filesWeek, _ := filepath.Glob(filepath.Join(tempDir, "timelapse_1_week_*.webm"))
	assert.Len(t, filesWeek, 1, "should keep only 1 archive for 1_week timelapse")
	assert.Contains(t, filesWeek, filepath.Join(tempDir, "timelapse_1_week_2.webm")) // Expecting the newest one
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

func TestCleanupGallery(t *testing.T) {
	tempDir, cleanup := setupTest(t)
	defer cleanup()

	// Set a specific gallery dir for this test
	originalGalleryDir := config.AppConfig.GalleryDir
	config.AppConfig.GalleryDir = filepath.Join(tempDir, "gallery")
	os.MkdirAll(config.AppConfig.GalleryDir, 0755)
	defer func() { config.AppConfig.GalleryDir = originalGalleryDir }()

	// Set a specific retention period for this test
	originalRetentionDays := config.AppConfig.GalleryRetentionDays
	config.AppConfig.GalleryRetentionDays = 1
	defer func() { config.AppConfig.GalleryRetentionDays = originalRetentionDays }()

	// Create an old file that should be deleted
	oldTime := time.Now().Add(-2 * 24 * time.Hour)
	oldFile := filepath.Join(config.AppConfig.GalleryDir, oldTime.Format("2006-01-02-15")+".jpg")
	os.WriteFile(oldFile, []byte("old"), 0644)

	// Create a newer file that should be kept
	newTime := time.Now()
	newFile := filepath.Join(config.AppConfig.GalleryDir, newTime.Format("2006-01-02-15")+".jpg")
	os.WriteFile(newFile, []byte("new"), 0644)

	CleanupGallery()

	// Assert old file is deleted
	_, err := os.Stat(oldFile)
	assert.True(t, os.IsNotExist(err), "Old gallery file should be deleted")

	// Assert new file still exists
	_, err = os.Stat(newFile)
	assert.False(t, os.IsNotExist(err), "New gallery file should not be deleted")
}


func TestEnqueueTimelapseJobs(t *testing.T) {
	originalCreateJob := jobs.CreateJob
	defer func() { jobs.CreateJob = originalCreateJob }()

	var calledJobTypes []string
	jobs.CreateJob = func(jobType string, payload interface{}) (int64, error) {
		calledJobTypes = append(calledJobTypes, jobType)
		return 1, nil
	}

	originalDaysOf24HourSnapshots := config.AppConfig.DaysOf24HourSnapshots
	config.AppConfig.DaysOf24HourSnapshots = 2 // Will enqueue 2 daily jobs
	defer func() { config.AppConfig.DaysOf24HourSnapshots = originalDaysOf24HourSnapshots }()

	originalTimelapseConfigsData := models.TimelapseConfigsData
	models.TimelapseConfigsData = []models.TimelapseConfig{
		{Name: "1_week"},
		{Name: "1_month"},
	} // Will enqueue 2 regular jobs
	defer func() { models.TimelapseConfigsData = originalTimelapseConfigsData }()

	EnqueueTimelapseJobs()

	// 2 daily + 2 regular + 4 cleanup jobs (snapshots, gallery, videos, logs)
	assert.Len(t, calledJobTypes, 2+2+4)

	expectedJobTypes := []string{
		"generate_timelapse", "generate_timelapse", "generate_timelapse", "generate_timelapse",
		"cleanup_snapshots", "cleanup_videos", "cleanup_logs", "cleanup_gallery",
	}
	assert.ElementsMatch(t, expectedJobTypes, calledJobTypes)
}

func TestGenerateSingleTimelapse_Daily(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	// Mock video generation functions
	originalRegenerateFullTimelapse := regenerateFullTimelapse
	originalWriteLastAppendedSnapshot := writeLastAppendedSnapshot
	originalReadLastAppendedSnapshot := readLastAppendedSnapshot
	originalCreateVideoSegment := createVideoSegment
	originalConcatenateVideos := concatenateVideos

	defer func() {
		regenerateFullTimelapse = originalRegenerateFullTimelapse
		writeLastAppendedSnapshot = originalWriteLastAppendedSnapshot
		readLastAppendedSnapshot = originalReadLastAppendedSnapshot
		createVideoSegment = originalCreateVideoSegment
		concatenateVideos = originalConcatenateVideos
	}()

	regenerateFullTimelapseCalled := false
	regenerateFullTimelapse = func(snapshotFiles []string, outputFileName string) error {
		regenerateFullTimelapseCalled = true
		// Assert on snapshotFiles if needed
		assert.NotEmpty(t, snapshotFiles)
		assert.Contains(t, outputFileName, "timelapse_24_hour_")
		return nil
	}
	writeLastAppendedSnapshot = func(timelapseName, snapshotPath string) error { return nil }
	readLastAppendedSnapshot = func(timelapseName string) (string, error) { return "", nil } // Force full regeneration
	createVideoSegment = func(imagePath, segmentPath string) error { return nil }
	concatenateVideos = func(existingVideoPath, newSegmentPath, outputVideoPath string) error { return nil }

	// Ensure there are snapshots for today
	testDay := time.Now().Truncate(24 * time.Hour)
	for hour := 0; hour < 24; hour++ {
		tm := testDay.Add(time.Duration(hour) * time.Hour)
		snapshotDir := filepath.Join(config.AppConfig.SnapshotsDir, tm.Format("2006-01"), tm.Format("02"), tm.Format("15"))
		os.MkdirAll(snapshotDir, 0755)
		dummyFile := filepath.Join(snapshotDir, tm.Format("2006-01-02-15-04-05")+".jpg")
		os.WriteFile(dummyFile, []byte("dummy"), 0644)
	}

	dailyTimelapseName := fmt.Sprintf("24_hour_%s", testDay.Format("2006-01-02"))
	err := GenerateSingleTimelapse(dailyTimelapseName)
	assert.NoError(t, err)
	assert.True(t, regenerateFullTimelapseCalled, "regenerateFullTimelapse should have been called for a new daily timelapse")

	// Test case for a non-existent daily timelapse date
	nonExistentDay := time.Now().AddDate(0, 0, -100).Truncate(24 * time.Hour)
	nonExistentTimelapseName := fmt.Sprintf("24_hour_%s", nonExistentDay.Format("2006-01-02"))
	regenerateFullTimelapseCalled = false // Reset
	err = GenerateSingleTimelapse(nonExistentTimelapseName)
	assert.NoError(t, err) // Should not error, just log no snapshots
	assert.False(t, regenerateFullTimelapseCalled, "regenerateFullTimelapse should NOT be called for a daily timelapse with no snapshots")
}
