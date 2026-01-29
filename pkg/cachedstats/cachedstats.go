package cachedstats

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"time-machine/pkg/services/snapshot"
	"time-machine/pkg/stats"
)

// CachedStats holds the cached statistics data.
type CachedStats struct {
	sync.RWMutex
	Data          gin.H
	isInitialized bool
}

// Cache is the global instance of our statistics cache.
var Cache = &CachedStats{
	Data: make(gin.H),
}

// getLoadingData returns a map of placeholder values for the initial page load.
func getLoadingData() gin.H {
	return gin.H{
		"total_images":         "Loading...",
		"image_size":           "Loading...",
		"last_image_time":      "Loading...",
		"last_processed_image": "Loading...",
		"available_dates":      []string{},
		"system_info":          stats.GetSystemInfo(), // This is fast and has its own "Loading..." state
		"camera_status":        gin.H{"Name": "Loading...", "Model": "Loading...", "Status": "Loading...", "Connected": "false"},
		"daily_gallery":        []map[string]string{},
		"is_loading":           true, // Flag for the frontend to know the data is temporary
	}
}

// RunUpdater starts the background process to update the cache periodically.
// The first update is run in a separate goroutine to avoid blocking server startup.
func (cs *CachedStats) RunUpdater() {
	go func() {
		// Perform the first update.
		cs.Update()

		// After the first update, start the ticker for regular updates.
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			cs.Update()
		}
	}()
}

// Update fetches the latest stats and updates the cache.
func (cs *CachedStats) Update() {
	defaultDate := time.Now().Format("2006-01-02")
	newData := gin.H{
		"total_images":         stats.GetTotalImagesCount(),
		"image_size":           stats.GetImagesDiskUsage(),
		"last_image_time":      stats.GetLastImageTime(),
		"last_processed_image": stats.GetLastProcessedImageName(),
		"available_dates":      stats.GetAvailableImageDates(),
		"system_info":          stats.GetSystemInfo(),
		"camera_status":        snapshot.GetFormattedCameraStatus(),
		"daily_gallery":        stats.GetDailyGallery(defaultDate),
		"is_loading":           false,
	}

	cs.Lock()
	defer cs.Unlock()

	cs.Data = newData
	if !cs.isInitialized {
		cs.isInitialized = true
	}
}

// GetData returns the cached data. If the cache is not yet initialized,
// it returns a set of placeholder "Loading..." values.
func (cs *CachedStats) GetData() gin.H {
	cs.RLock()
	defer cs.RUnlock()

	if !cs.isInitialized {
		return getLoadingData()
	}
	return cs.Data
}
