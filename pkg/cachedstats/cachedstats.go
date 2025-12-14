package cachedstats

import (
	"sync"
	"time"

	"time-machine/pkg/services/snapshot"
	"time-machine/pkg/stats"

	"github.com/gin-gonic/gin"
)

// CachedStats holds the cached statistics data
// This probably will struggler and need a more robust caching solution as the app grows, larger data, or support for multiple instances, cameras etc

type CachedStats struct {
	sync.RWMutex
	Data gin.H
}

var Cache = &CachedStats{
	Data: make(gin.H),
}

func (cs *CachedStats) RunUpdater() {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for {
			cs.Update()
			<-ticker.C
		}
	}()
}

func (cs *CachedStats) Update() {
	cs.Lock()
	defer cs.Unlock()

	defaultDate := time.Now().Format("2006-01-02")
	cs.Data = gin.H{
		"total_images":         stats.GetTotalImagesCount(),
		"image_size":           stats.GetImagesDiskUsage(),
		"last_image_time":      stats.GetLastImageTime(),
		"last_processed_image": stats.GetLastProcessedImageName(),
		"available_dates":      stats.GetAvailableImageDates(),
		"system_info":          stats.GetSystemInfo(),
		"camera_status":        snapshot.GetFormattedCameraStatus(),
		"daily_gallery":        stats.GetDailyGallery(defaultDate),
	}
}

func (cs *CachedStats) GetData() gin.H {
	cs.RLock()
	defer cs.RUnlock()
	return cs.Data
}
