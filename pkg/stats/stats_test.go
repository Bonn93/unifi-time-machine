package stats

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/mem"
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
	assert.IsType(t, gin.H{}, usage)
	assert.Contains(t, usage, "image_usage_gb")
	assert.Contains(t, usage, "disk_total_gb")
	assert.Contains(t, usage, "disk_used_gb")
	assert.Contains(t, usage, "disk_used_percent")
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
	// Manually set stats to test formatting
	currentStats.mu.Lock()
	currentStats.IsReady = true
	currentStats.CPUUsage = 85.555
	currentStats.Memory = &mem.VirtualMemoryStat{
		Total:       16 * 1024 * 1024 * 1024, // 16 GB
		Used:        4 * 1024 * 1024 * 1024,  // 4 GB
		UsedPercent: 25.0,
	}
	currentStats.OS = "TestOS"
	currentStats.mu.Unlock()

	info := GetSystemInfo()
	assert.NotNil(t, info)
	assert.Equal(t, "TestOS", info["os_type"])
	assert.Equal(t, "85.56%", info["cpu_usage"])
	assert.Equal(t, "4.00 GB / 16.00 GB (25.00%)", info["memory_usage"])
	assert.Equal(t, 85.555, info["cpu_usage_raw"])
	assert.Equal(t, 25.0, info["memory_usage_raw"])
	assert.Contains(t, info["av1_encoder"], "Available")

	// Test "Loading..." state
	currentStats.mu.Lock()
	currentStats.IsReady = false
	currentStats.mu.Unlock()
	info = GetSystemInfo()
	assert.Equal(t, "Loading...", info["cpu_usage"])
	assert.Equal(t, "Loading...", info["memory_usage"])
}

// Test for getOSPrettyName (simple case)
func TestGetOSPrettyName(t *testing.T) {
	// This test mainly ensures it doesn't crash and returns a string.
	// Mocking /etc/os-release is overly complex for this.
	name := getOSPrettyName()
	if runtime.GOOS == "linux" {
		// Can't be sure what it will be, but it shouldn't be empty
		assert.NotEmpty(t, name)
	} else {
		assert.Equal(t, runtime.GOOS, name)
	}
}

func TestGetAvailableImageDates(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	dates := GetAvailableImageDates()
	// The dummy file format is YYYY-MM-DD-HH.jpg, so we get the date part
	expectedDates := []string{
		time.Now().Format("2006-01-02"),
		time.Now().Add(-24 * time.Hour).Format("2006-01-02"),
		time.Now().Add(-48 * time.Hour).Format("2006-01-02"),
	}
	// The function returns unique dates, so remove duplicates from expected
	uniqueExpected := make(map[string]struct{})
	for _, d := range expectedDates {
		uniqueExpected[d] = struct{}{}
	}
	var finalExpected []string
	for d := range uniqueExpected {
		finalExpected = append(finalExpected, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(finalExpected)))

	assert.ElementsMatch(t, finalExpected, dates)
}

func TestGetDailyGallery(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	today := time.Now().Format("2006-01-02")
	gallery := GetDailyGallery(today)
	assert.Len(t, gallery, 24)

	// Find the created gallery image and check its data
	hour := time.Now().Format("15")
	found := false
	for _, item := range gallery {
		if item["time"] == hour+":00" {
			found = true
			assert.Equal(t, "true", item["available"])
			expectedURL := fmt.Sprintf("/data/gallery/%s-%s.jpg", today, hour)
			assert.Equal(t, expectedURL, item["url"])
		}
	}
	assert.True(t, found, "Did not find gallery item for the current hour")
}

func TestGetSnapshotFiles(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	files := GetSnapshotFiles()
	assert.Len(t, files, 5)
	assert.True(t, sort.StringsAreSorted(files))
}
