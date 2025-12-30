package stats

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"

	"time-machine/pkg/config"
	"time-machine/pkg/models"
	"time-machine/pkg/services/video" // Import the video package
	"time-machine/pkg/util"
)

// SystemStats holds the CPU and memory usage data.
type SystemStats struct {
	mu          sync.RWMutex
	CPUUsage    string `json:"cpu_usage"`
	MemoryUsage string `json:"memory_usage"`
	OS          string `json:"os"`
	IsReady     bool   `json:"is_ready"`
}

var currentStats = &SystemStats{
	CPUUsage:    "Loading...",
	MemoryUsage: "Loading...",
	OS:          runtime.GOOS,
	IsReady:     false,
}

// StartStatsCollector starts a goroutine to periodically fetch system stats.
func StartStatsCollector() {
	go func() {
		for {
			cpuPercent, err := cpu.Percent(time.Second, false)
			if err != nil {
				log.Printf("Error getting CPU usage: %v", err)
			}

			memInfo, err := mem.VirtualMemory()
			if err != nil {
				log.Printf("Error getting memory usage: %v", err)
			}

			currentStats.mu.Lock()
			if len(cpuPercent) > 0 {
				currentStats.CPUUsage = fmt.Sprintf("%.2f%%", cpuPercent[0])
			}
			if memInfo != nil {
				currentStats.MemoryUsage = fmt.Sprintf("%.2f%%", memInfo.UsedPercent)
			}
			currentStats.IsReady = true
			currentStats.mu.Unlock()

			time.Sleep(5 * time.Second) // Update every 5 seconds
		}
	}()
}

// needs good wrapping with go routines and caching later, leverage the dB and make the UI more async for faster loads.

func HandleImageStatsData() gin.H {
	return gin.H{
		"total_images":         GetTotalImagesCount(),
		"image_size":           GetImagesDiskUsage(),
		"last_image_time":      GetLastImageTime(),
		"last_processed_image": GetLastProcessedImageName(),
		"available_dates":      GetAvailableImageDates(),
	}
}

var GetTotalImagesCount = func() int {
	// This now counts unprocessed images waiting for the next timelapse generation.
	return len(GetSnapshotFiles())
}

var GetImagesDiskUsage = func() string {
	var totalSize int64
	err := filepath.Walk(config.AppConfig.DataDir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return err
	})

	if err != nil {
		log.Printf("Error calculating disk usage: %v", err)
		return "N/A"
	}

	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)

	switch {
	case totalSize >= gb:
		return fmt.Sprintf("%.2f GB", float64(totalSize)/float64(gb))
	case totalSize >= mb:
		return fmt.Sprintf("%.2f MB", float64(totalSize)/float64(mb))
	case totalSize >= kb:
		return fmt.Sprintf("%.2f KB", float64(totalSize)/float64(kb))
	default:
		return fmt.Sprintf("%d Bytes", totalSize)
	}
}

var GetLastImageTime = func() string {
	// This now reflects the most recent snapshot taken for the timelapse.
	files := GetSnapshotFiles()
	if len(files) == 0 {
		return "N/A"
	}

	lastFilePath := files[len(files)-1]
	lastFileName := filepath.Base(lastFilePath)
	timeStr := strings.TrimSuffix(lastFileName, ".jpg")

	t, err := time.Parse("2006-01-02-15-04-05", timeStr)
	if err != nil {
		return "N/A (Parse Error)"
	}
	return t.Format("2006-01-02 15:04:05")
}

var GetLastProcessedImageName = func() string {
	models.VideoStatusData.RLock()
	lastRun := models.VideoStatusData.LastRun
	models.VideoStatusData.RUnlock()

	if lastRun == nil {
		return "N/A"
	}
	return lastRun.Format("2006-01-02-15-04-05") + ".jpg"
}

var GetSystemInfo = func() gin.H {
	currentStats.mu.RLock()
	defer currentStats.mu.RUnlock()

	return gin.H{
		"os_type":      currentStats.OS,
		"cpu_usage":    currentStats.CPUUsage,
		"memory_usage": currentStats.MemoryUsage,
		"av1_status":   fmt.Sprintf("Available (%s)", video.PreferredVideoCodec),
	}
}

// GetAvailableImageDates now scans the flat gallery directory.
var GetAvailableImageDates = func() []string {
	files, err := os.ReadDir(config.AppConfig.GalleryDir)
	if err != nil {
		log.Printf("Error reading gallery directory: %v", err)
		return []string{}
	}

	dateSet := make(map[string]struct{})
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".jpg") {
			// Filename: YYYY-MM-DD-HH.jpg
			fileName := file.Name()
			if len(fileName) >= 13 {
				dateStr := fileName[:10]
				dateSet[dateStr] = struct{}{}
			}
		}
	}

	var dates []string
	for date := range dateSet {
		dates = append(dates, date)
	}

	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	return dates
}


// GetDailyGallery now uses the dedicated, retained gallery images.
var GetDailyGallery = func(dateStr string) []map[string]string {
	gallery := make([]map[string]string, 24)

	for i := 0; i < 24; i++ {
		hour := fmt.Sprintf("%02d", i)
		timeLabel := fmt.Sprintf("%s:00", hour)

		// Look for a specific file like 'YYYY-MM-DD-HH.jpg'
		galleryFileName := fmt.Sprintf("%s-%s.jpg", dateStr, hour)
		galleryFilePath := filepath.Join(config.AppConfig.GalleryDir, galleryFileName)

		url := ""
		available := "false"

		if util.FileExists(galleryFilePath) {
			available = "true"
			// URL needs to be relative to the DataDir root for serving
			url = "/data/gallery/" + galleryFileName
		}

		gallery[i] = map[string]string{
			"time":      timeLabel,
			"url":       url,
			"available": available,
		}
	}
	return gallery
}

// GetSnapshotFiles recursively finds all snapshot files in the structured directory.
func GetSnapshotFiles() []string {
	var files []string
	err := filepath.WalkDir(config.AppConfig.SnapshotsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".jpg") {
			files = append(files, path)
		}
		return nil
	})

	if err != nil {
		log.Printf("Error walking snapshot directory: %v", err)
		return []string{}
	}

	sort.Strings(files) // Sorts chronologically due to file path structure
	return files
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}
