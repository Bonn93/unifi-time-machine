package stats

import (
	"bufio"
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
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"

	"time-machine/pkg/config"
	"time-machine/pkg/models"
	"time-machine/pkg/services/video" // Import the video package
	"time-machine/pkg/util"
)

var (
	osPrettyName  string
	osReleaseOnce sync.Once
)

// SystemStats holds the CPU and memory usage data.
type SystemStats struct {
	mu          sync.RWMutex
	CPUUsage    float64
	Memory      *mem.VirtualMemoryStat
	OS          string
	IsReady     bool
}

var currentStats = &SystemStats{
	OS:      getOSPrettyName(),
	IsReady: false,
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
				currentStats.CPUUsage = cpuPercent[0]
			}
			if memInfo != nil {
				currentStats.Memory = memInfo
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

var GetImagesDiskUsage = func() gin.H {
	var imageSize int64
	err := filepath.Walk(config.AppConfig.DataDir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			imageSize += info.Size()
		}
		return err
	})

	if err != nil {
		log.Printf("Error calculating image disk usage: %v", err)
		return gin.H{"error": "N/A"}
	}

	diskStat, err := disk.Usage(config.AppConfig.DataDir)
	if err != nil {
		log.Printf("Error getting disk usage stat: %v", err)
		return gin.H{"error": "N/A"}
	}

	return gin.H{
		"image_usage_gb":    fmt.Sprintf("%.2f GB", float64(imageSize)/1024/1024/1024),
		"disk_total_gb":     fmt.Sprintf("%.2f GB", float64(diskStat.Total)/1024/1024/1024),
		"disk_used_gb":      fmt.Sprintf("%.2f GB", float64(diskStat.Used)/1024/1024/1024),
		"disk_used_percent": fmt.Sprintf("%.2f%%", diskStat.UsedPercent),
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

// getOSPrettyName reads /etc/os-release and returns the PRETTY_NAME if available.
// It's cached after the first call.
func getOSPrettyName() string {
	osReleaseOnce.Do(func() {
		if runtime.GOOS != "linux" {
			osPrettyName = runtime.GOOS
			return
		}
		file, err := os.Open("/etc/os-release")
		if err != nil {
			osPrettyName = "linux"
			return
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				// The value is usually quoted, so trim quotes.
				osPrettyName = strings.Trim(strings.SplitN(line, "=", 2)[1], `"`)
				return
			}
		}
		// Fallback if PRETTY_NAME is not found
		osPrettyName = "linux"
	})
	return osPrettyName
}

var GetSystemInfo = func() gin.H {
	currentStats.mu.RLock()
	defer currentStats.mu.RUnlock()

	info := gin.H{
		"os_type":      currentStats.OS,
		"cpu_usage":    "Loading...",
		"memory_usage": "Loading...",
		"av1_encoder":  fmt.Sprintf("Available (%s)", video.PreferredVideoCodec),
	}

	if currentStats.IsReady {
		info["cpu_usage"] = fmt.Sprintf("%.2f%%", currentStats.CPUUsage)
		if currentStats.Memory != nil {
			info["memory_usage"] = fmt.Sprintf("%.2f GB / %.2f GB (%.2f%%)",
				float64(currentStats.Memory.Used)/1024/1024/1024,
				float64(currentStats.Memory.Total)/1024/1024/1024,
				currentStats.Memory.UsedPercent,
			)
			// Pass raw percentages for frontend coloring
			info["cpu_usage_raw"] = currentStats.CPUUsage
			info["memory_usage_raw"] = currentStats.Memory.UsedPercent
		}
	}

	return info
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

