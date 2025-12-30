package stats

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"time-machine/pkg/config"
	"time-machine/pkg/models"
)

func setupTest(t *testing.T) (string, func()) {
	tempDir, err := os.MkdirTemp("", "stats-test")
	assert.NoError(t, err)

	config.AppConfig.DataDir = tempDir
	config.AppConfig.SnapshotsDir = filepath.Join(tempDir, "snapshots")
	config.AppConfig.GalleryDir = filepath.Join(tempDir, "gallery")
	os.MkdirAll(config.AppConfig.SnapshotsDir, 0755)
	os.MkdirAll(config.AppConfig.GalleryDir, 0755)

	// Create some dummy snapshot files
	for i := 0; i < 5; i++ {
		now := time.Now().Add(-time.Duration(i) * time.Hour)
		snapshotDir := filepath.Join(config.AppConfig.SnapshotsDir, now.Format("2006-01"), now.Format("02"), now.Format("15"))
		os.MkdirAll(snapshotDir, 0755)
		dummyFile := filepath.Join(snapshotDir, now.Format("2006-01-02-15-04-05")+".jpg")
		os.WriteFile(dummyFile, []byte("dummy"), 0644)
	}

	// Create some dummy gallery files
	for i := 0; i < 3; i++ {
		now := time.Now().Add(-time.Duration(i*24) * time.Hour)
		dummyFile := filepath.Join(config.AppConfig.GalleryDir, now.Format("2006-01-02-15")+".jpg")
		os.WriteFile(dummyFile, []byte("dummy"), 0644)
	}

	return tempDir, func() {
		os.RemoveAll(tempDir)
	}
}

func TestGetTotalImagesCount(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	count := GetTotalImagesCount()
	assert.Equal(t, 5, count)
}

func TestGetImagesDiskUsage(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	usage := GetImagesDiskUsage()
	assert.NotEqual(t, "N/A", usage)
}

func TestGetLastImageTime(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	lastTime := GetLastImageTime()
	assert.NotEqual(t, "N/A", lastTime)
	// We can't easily assert the exact time, but we can check the format
	_, err := time.Parse("2006-01-02 15:04:05", lastTime)
	assert.NoError(t, err)
}

func TestGetLastProcessedImageName(t *testing.T) {
	now := time.Now()
	models.VideoStatusData.LastRun = &now
	name := GetLastProcessedImageName()
	assert.Equal(t, now.Format("2006-01-02-15-04-05")+".jpg", name)

	models.VideoStatusData.LastRun = nil
	name = GetLastProcessedImageName()
	assert.Equal(t, "N/A", name)
}

func TestGetSystemInfo(t *testing.T) {
	// Start the collector
	StartStatsCollector()

	// Give it a moment to run
	time.Sleep(2 * time.Second)

	info := GetSystemInfo()
	assert.NotNil(t, info)
	assert.Contains(t, info, "os_type")
	assert.NotEqual(t, "Loading...", info["cpu_usage"])
	assert.NotEqual(t, "Loading...", info["memory_usage"])
}

func TestGetAvailableImageDates(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	dates := GetAvailableImageDates()
	assert.Len(t, dates, 3)
	// Check if sorted in reverse
	sorted := sort.SliceIsSorted(dates, func(i, j int) bool {
		return dates[i] > dates[j]
	})
	assert.True(t, sorted)
}

func TestGetDailyGallery(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	today := time.Now().Format("2006-01-02")
	gallery := GetDailyGallery(today)
	assert.Len(t, gallery, 24)

	// Find the created gallery image and check its data
	hour := time.Now().Format("15")
	for _, item := range gallery {
		if item["time"] == hour+":00" {
			assert.Equal(t, "true", item["available"])
			expectedURL := fmt.Sprintf("/data/gallery/%s-%s.jpg", today, hour)
			assert.Equal(t, expectedURL, item["url"])
		}
	}
}

func TestGetSnapshotFiles(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	files := GetSnapshotFiles()
	assert.Len(t, files, 5)
	assert.True(t, sort.StringsAreSorted(files))
}
