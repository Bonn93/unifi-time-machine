package cachedstats

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"time-machine/pkg/services/snapshot"
	"time-machine/pkg/stats"
)

func TestUpdateAndGetData(t *testing.T) {
	// Overwrite the original functions with mock implementations
	originalGetTotalImagesCount := stats.GetTotalImagesCount
	stats.GetTotalImagesCount = func() int { return 100 }
	defer func() { stats.GetTotalImagesCount = originalGetTotalImagesCount }()

	originalGetImagesDiskUsage := stats.GetImagesDiskUsage
	stats.GetImagesDiskUsage = func() gin.H {
		return gin.H{
			"image_usage_gb":    "10.00 GB",
			"disk_total_gb":     "100.00 GB",
			"disk_used_gb":      "50.00 GB",
			"disk_used_percent": "50.00%",
		}
	}
	defer func() { stats.GetImagesDiskUsage = originalGetImagesDiskUsage }()

	originalGetLastImageTime := stats.GetLastImageTime
	stats.GetLastImageTime = func() string { return "2023-10-27 10:00:00" }
	defer func() { stats.GetLastImageTime = originalGetLastImageTime }()

	originalGetLastProcessedImageName := stats.GetLastProcessedImageName
	stats.GetLastProcessedImageName = func() string { return "image.jpg" }
	defer func() { stats.GetLastProcessedImageName = originalGetLastProcessedImageName }()

	originalGetAvailableImageDates := stats.GetAvailableImageDates
	stats.GetAvailableImageDates = func() []string { return []string{"2023-10-27"} }
	defer func() { stats.GetAvailableImageDates = originalGetAvailableImageDates }()

	originalGetSystemInfo := stats.GetSystemInfo
	stats.GetSystemInfo = func() gin.H {
		return gin.H{"cpu": "50%"}
	}
	defer func() { stats.GetSystemInfo = originalGetSystemInfo }()

	originalGetFormattedCameraStatus := snapshot.GetFormattedCameraStatus
	snapshot.GetFormattedCameraStatus = func() map[string]string {
		return map[string]string{"status": "active"}
	}
	defer func() { snapshot.GetFormattedCameraStatus = originalGetFormattedCameraStatus }()

	originalGetDailyGallery := stats.GetDailyGallery
	stats.GetDailyGallery = func(date string) []map[string]string {
		return []map[string]string{{"images": "5"}, {"videos": "1"}}
	}
	defer func() { stats.GetDailyGallery = originalGetDailyGallery }()

	// Create a new CachedStats instance
	cs := &CachedStats{
		Data: make(gin.H),
	}

	// Run the updater
	cs.Update()

	// Get the data
	data := cs.GetData()

	// Assertions
	assert.Equal(t, 100, data["total_images"])
	assert.Equal(t, "10.00 GB", data["image_size"].(gin.H)["image_usage_gb"])
	assert.Equal(t, "2023-10-27 10:00:00", data["last_image_time"])
	assert.Equal(t, "image.jpg", data["last_processed_image"])
	assert.Equal(t, []string{"2023-10-27"}, data["available_dates"])
	assert.Equal(t, gin.H{"cpu": "50%"}, data["system_info"])
	assert.Equal(t, map[string]string{"status": "active"}, data["camera_status"])
	assert.Equal(t, []map[string]string{{"images": "5"}, {"videos": "1"}}, data["daily_gallery"])
}

func TestGetDataLoading(t *testing.T) {
	cs := &CachedStats{
		Data:          make(gin.H),
		isInitialized: false,
	}
	data := cs.GetData()
	assert.True(t, data["is_loading"].(bool))
	assert.Equal(t, "Loading...", data["total_images"])
	assert.Equal(t, "Loading...", data["image_size"])
}

func TestRunUpdater(t *testing.T) {
	// This test is to ensure RunUpdater runs without panicking.
	// A more comprehensive test would involve checking if the data is updated periodically.
	cs := &CachedStats{
		Data: make(gin.H),
	}
	// just test the first update
	go cs.Update()
	time.Sleep(1 * time.Second) // Let the updater run once
}
