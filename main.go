package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// --- CONFIGURATION AND GLOBAL STATE ---

type Config struct {
	UFPHost             string
	UFPAPIKey           string
	TargetCameraID      string
	DataDir             string
	SnapshotsDir        string
	GalleryDir          string
	SnapshotIntervalSec int
	VideoCronIntervalSec int
	VideoArchivesToKeep int
	FFmpegLogPath       string
}

var AppConfig Config

type VideoStatus struct {
	sync.RWMutex
	IsRunning bool
	LastRun   *time.Time
	Error     string
}

var videoStatus = VideoStatus{
	IsRunning: false,
	Error:     "",
}

type TimelapseConfig struct {
	Name         string
	Duration     time.Duration
	FramePattern string // "all", "hourly", "daily"
}

var timelapseConfigs = []TimelapseConfig{
	{Name: "24_hour", Duration: 24 * time.Hour, FramePattern: "all"},
	{Name: "1_week", Duration: 7 * 24 * time.Hour, FramePattern: "hourly"},
	{Name: "1_month", Duration: 30 * 24 * time.Hour, FramePattern: "daily"},
	{Name: "1_year", Duration: 365 * 24 * time.Hour, FramePattern: "daily"}, // Using daily for year as well for simplicity
}

// --- INITIALIZATION ---

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := getEnv(key, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultValue
}

func loadConfig() {
	AppConfig = Config{
		UFPAPIKey:           getEnv("UFP_API_KEY", ""),
		TargetCameraID:      getEnv("TARGET_CAMERA_ID", ""),
		DataDir:             getEnv("DATA_DIR", "data"),
		SnapshotIntervalSec: getEnvAsInt("TIMELAPSE_INTERVAL", 3600),
		VideoCronIntervalSec: getEnvAsInt("VIDEO_CRON_INTERVAL", 300),
		VideoArchivesToKeep: getEnvAsInt("VIDEO_ARCHIVES_TO_KEEP", 3),
	}

	AppConfig.SnapshotsDir = filepath.Join(AppConfig.DataDir, "snapshots")
	AppConfig.GalleryDir = filepath.Join(AppConfig.DataDir, "gallery")

	// Ensure UFP_HOST has a protocol scheme
	AppConfig.UFPHost = getEnv("UFP_HOST", "")
	if AppConfig.UFPHost != "" && !strings.Contains(AppConfig.UFPHost, "://") {
		AppConfig.UFPHost = "https://" + AppConfig.UFPHost
	}

	log.Printf("UFP Host set to: %s", AppConfig.UFPHost)

	AppConfig.FFmpegLogPath = filepath.Join(AppConfig.DataDir, "ffmpeg_log.txt")
}

func main() {
	loadConfig()

	// Ensure data directories exist
	if err := os.MkdirAll(AppConfig.SnapshotsDir, 0755); err != nil {
		log.Fatalf("Failed to create snapshots directory: %v", err)
	}
	if err := os.MkdirAll(AppConfig.GalleryDir, 0755); err != nil {
		log.Fatalf("Failed to create gallery directory: %v", err)
	}

	// Start background schedulers
	go startSnapshotScheduler()
	go startVideoGeneratorScheduler()
	log.Printf("✅ Snapshot Scheduler started with interval: %d seconds", AppConfig.SnapshotIntervalSec)
	log.Printf("✅ Video Generation Scheduler started with interval: %d seconds", AppConfig.VideoCronIntervalSec)

	// Setup Gin router
	r := gin.Default()

	// --- Dashboard Template Rendering ---
	r.SetFuncMap(template.FuncMap{
		"js": func(v interface{}) (template.JS, error) {
			j, err := json.Marshal(v)
			return template.JS(j), err
		},
	})
	r.LoadHTMLFiles("index.html")
	r.GET("/", handleDashboard)

	// --- Static File & API Routes ---
	r.Static("/data", AppConfig.DataDir)
	r.POST("/generate", handleForceGenerate)
	r.GET("/log", handleLog)
	r.GET("/api/status", handleSystemStats)
	r.GET("/api/images", handleImageStats)
	r.GET("/api/gallery", handleDailyGallery)

	log.Println("Gin server starting on port 8080...")

	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Gin server failed to start: %v", err)
	}
}

// --- HANDLERS ---

func handleDashboard(c *gin.Context) {
	// --- New Timelapse Info ---
	var availableTimelapses []gin.H
	var firstAvailableVideo string
	videoExists := false
	for _, cfg := range timelapseConfigs {
		fileName := fmt.Sprintf("timelapse_%s.webm", cfg.Name)
		filePath := filepath.Join(AppConfig.DataDir, fileName)
		if fileExists(filePath) {
			if !videoExists {
				firstAvailableVideo = "/data/" + fileName
				videoExists = true
			}
			availableTimelapses = append(availableTimelapses, gin.H{
				"Name":     strings.ReplaceAll(cfg.Name, "_", " "),
				"FileName": fileName,
				"Path":     "/data/" + fileName,
			})
		}
	}

	// Gather all data points
	videoStatus.RLock()
	currentVideoStatus := gin.H{
		"IsRunning": videoStatus.IsRunning,
		"LastRun":   "N/A",
		"Error":     videoStatus.Error,
	}
	if videoStatus.LastRun != nil {
		currentVideoStatus["LastRun"] = videoStatus.LastRun.Format("2006-01-02 15:04:05")
	}
	videoStatus.RUnlock()

	defaultDate := time.Now().Format("2006-01-02")

	// Consolidate data into a single map for the template
	data := gin.H{
		"Now":                  time.Now().Format("2006-01-02 15:04:05"),
		"VideoExists":          videoExists, // True if any video exists
		"FirstAvailableVideo":  firstAvailableVideo,
		"AvailableTimelapses":  availableTimelapses,
		"VideoStatus":          currentVideoStatus,
		"ImageStats":           handleImageStatsData(),
		"SystemInfo":           getSystemInfo(),
		"CameraStatus":         getFormattedCameraStatus(),
		"AvailableDates":       getAvailableImageDates(),
		"DefaultGalleryDate":   defaultDate,
		"DefaultGalleryImages": getDailyGallery(defaultDate),
	}

	c.HTML(http.StatusOK, "index.html", data)
}

func handleForceGenerate(c *gin.Context) {
	videoStatus.RLock()
	isRunning := videoStatus.IsRunning
	videoStatus.RUnlock()

	if !isRunning {
		// Execute in a goroutine so the HTTP request completes immediately
		go generateAllTimelapses()
	}

	c.Redirect(http.StatusFound, "/")
}

func handleLog(c *gin.Context) {
	content, err := os.ReadFile(AppConfig.FFmpegLogPath)
	if err != nil {
		// Attempt to create an empty log file if it doesn't exist
		if os.IsNotExist(err) {
			c.String(http.StatusOK, "FFmpeg log file does not exist yet.")
			return
		}
		c.String(http.StatusInternalServerError, "Error reading log file: %v", err)
		return
	}
	// Use pre-formatted text for log output
	c.String(http.StatusOK, "<pre>%s</pre>", string(content)) 
}

func handleSystemStats(c *gin.Context) {
	c.JSON(http.StatusOK, getSystemInfo())
}

func handleImageStatsData() gin.H {
	return gin.H{
		"total_images":         getTotalImagesCount(),
		"image_size":           getImagesDiskUsage(),
		"last_image_time":      getLastImageTime(),
		"last_processed_image": getLastProcessedImageName(),
		"available_dates":      getAvailableImageDates(),
	}
}

func handleImageStats(c *gin.Context) {
	c.JSON(http.StatusOK, handleImageStatsData())
}

func handleDailyGallery(c *gin.Context) {
	dateStr := c.Query("date")
	if dateStr == "" {
		dateStr = time.Now().Format("2006-01-02")
	}

	images := getDailyGallery(dateStr)
	c.JSON(http.StatusOK, gin.H{
		"date":   dateStr,
		"images": images,
	})
}

// --- CORE LOGIC (Scheduler and API calls) ---

func startSnapshotScheduler() {
	for {
		takeSnapshot()
		time.Sleep(time.Duration(AppConfig.SnapshotIntervalSec) * time.Second)
	}
}

func takeSnapshot() {
	if AppConfig.UFPHost == "" || AppConfig.UFPAPIKey == "" || AppConfig.TargetCameraID == "" {
		log.Println("Snapshot Error: UniFi Protect credentials missing.")
		return
	}

	apiURL := fmt.Sprintf("%s/proxy/protect/integration/v1/cameras/%s/snapshot", AppConfig.UFPHost, AppConfig.TargetCameraID)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: tr,
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Printf("Error creating snapshot request: %v", err)
		return
	}
	req.Header.Set("X-Api-Key", AppConfig.UFPAPIKey)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Snapshot API request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("UniFi API returned status code %d: %s", resp.StatusCode, string(bodyBytes))
		return
	}

	now := time.Now()

	// --- New Directory Structure Logic ---
	// Path: snapshots/YYYY-MM/DD/HH/
	snapshotDir := filepath.Join(AppConfig.SnapshotsDir, now.Format("2006-01"), now.Format("02"), now.Format("15"))
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		log.Printf("Error creating snapshot directory %s: %v", snapshotDir, err)
		return
	}

	// Save the snapshot for the timelapse
	fileName := now.Format("2006-01-02-15-04-05") + ".jpg"
	snapshotPath := filepath.Join(snapshotDir, fileName)
	out, err := os.Create(snapshotPath)
	if err != nil {
		log.Printf("Error creating file %s: %v", snapshotPath, err)
		return
	}
	defer out.Close()

	// Tee the response body to write to multiple places if needed
	if _, err = io.Copy(out, resp.Body); err != nil {
		log.Printf("Error saving snapshot to file %s: %v", snapshotPath, err)
		return
	}
	log.Printf("Snapshot saved: %s", snapshotPath)

	// --- New Gallery Logic ---
	// Save the first snapshot of the hour to the gallery
	galleryFileName := now.Format("2006-01-02-15") + ".jpg"
	galleryPath := filepath.Join(AppConfig.GalleryDir, galleryFileName)

	if !fileExists(galleryPath) {
		if err := copyFile(snapshotPath, galleryPath); err != nil {
			log.Printf("Error copying snapshot to gallery %s: %v", galleryPath, err)
		} else {
			log.Printf("Saved new gallery image: %s", galleryPath)
		}
	}

	// Update the latest_snapshot.jpg for the video player poster
	latestPath := filepath.Join(AppConfig.DataDir, "latest_snapshot.jpg")
	if err := copyFile(snapshotPath, latestPath); err != nil {
		log.Printf("Error copying snapshot to latest_snapshot.jpg: %v", err)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func getCameraStatus() map[string]interface{} {
	if AppConfig.UFPHost == "" || AppConfig.UFPAPIKey == "" || AppConfig.TargetCameraID == "" {
		return map[string]interface{}{"error": "UniFi Protect credentials missing from environment."}
	}

	apiURL := fmt.Sprintf("%s/proxy/protect/integration/v1/cameras/%s", AppConfig.UFPHost, AppConfig.TargetCameraID)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: tr,
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Printf("Error creating camera status request: %v", err)
		return map[string]interface{}{"error": fmt.Sprintf("Request creation error: %v", err)}
	}

	req.Header.Set("X-Api-Key", AppConfig.UFPAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Camera Status API request failed: %v", err)
		return map[string]interface{}{"error": fmt.Sprintf("API request failed: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("UniFi API returned status code %d: %s", resp.StatusCode, string(bodyBytes))
		return map[string]interface{}{"error": fmt.Sprintf("API returned status code %d", resp.StatusCode)}
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Error decoding camera status JSON: %v", err)
		return map[string]interface{}{"error": "Failed to decode API response."}
	}

	return result
}

func getFormattedCameraStatus() map[string]string {
	rawStatus := getCameraStatus()

	if rawStatus == nil {
		return map[string]string{"Status": "ERROR: Connection Failed"}
	}
	if errMsg, ok := rawStatus["error"]; ok {
		return map[string]string{"Status": fmt.Sprintf("API ERROR: %s", errMsg)}
	}

	status := "Unknown"
	if state, ok := rawStatus["state"].(string); ok {
		status = state
	}

	uptimeStr := "N/A"
	if uptimeFloat, ok := rawStatus["upSince"].(float64); ok {
		upSince := time.Unix(int64(uptimeFloat/1000), 0)
		uptimeStr = upSince.Format("2006-01-02 15:04:05")
	}

	model := "N/A"
	if modelStr, ok := rawStatus["modelKey"].(string); ok {
		model = strings.ReplaceAll(modelStr, "UVC G", "G")
	}

	name := "N/A"
	if nameStr, ok := rawStatus["name"].(string); ok {
		name = nameStr
	}

	return map[string]string{
		"Name":      name,
		"Model":     model,
		"Status":    status,
		"UpSince":   uptimeStr,
		"Connected": strconv.FormatBool(status == "CONNECTED"),
	}
}

// --- VIDEO GENERATION AND CLEANUP IMPLEMENTATION ---

func startVideoGeneratorScheduler() {
	for {
		if !videoStatus.IsRunning {
			// This single function will handle all timelapse generations and cleanup
			generateAllTimelapses()
		} else {
			log.Println("Video generation skipped, another job is running.")
		}
		time.Sleep(time.Duration(AppConfig.VideoCronIntervalSec) * time.Second)
	}
}

func generateAllTimelapses() {
	log.Println("Starting all timelapse generations...")

	// 1. Set running status
	videoStatus.Lock()
	videoStatus.IsRunning = true
	videoStatus.Error = ""
	videoStatus.Unlock()

	defer func() {
		currentTime := time.Now()
		videoStatus.Lock()
		videoStatus.IsRunning = false
		videoStatus.LastRun = &currentTime
		videoStatus.Unlock()
		log.Println("All timelapse generations finished.")
	}()

	allSnapshots := getSnapshotFiles()
	if len(allSnapshots) < 2 {
		log.Println("Not enough snapshots to generate videos.")
		return
	}

	for _, cfg := range timelapseConfigs {
		log.Printf("--- Generating timelapse: %s ---", cfg.Name)
		snapshotsForTimelapse := filterSnapshots(allSnapshots, cfg)

		if len(snapshotsForTimelapse) < 2 {
			log.Printf("Not enough snapshots for %s timelapse, skipping.", cfg.Name)
			continue
		}

		outputFileName := fmt.Sprintf("timelapse_%s.webm", cfg.Name)
		err := generateTimelapse(snapshotsForTimelapse, outputFileName)
		if err != nil {
			log.Printf("ERROR generating %s timelapse: %v", cfg.Name, err)
			videoStatus.Lock()
			videoStatus.Error = fmt.Sprintf("Error on %s: %v", cfg.Name, err)
			videoStatus.Unlock()
			// Decide if we should stop or continue with other timelapses
			// For now, we'll continue
		} else {
			log.Printf("✅ Successfully generated %s timelapse.", cfg.Name)
		}
	}

	// 2. After all videos are generated, perform cleanup.
	cleanupSnapshots()

	// 3. Clean up old archived videos (this function was unused before)
	cleanOldVideos()
}

// filterSnapshots selects files based on the timelapse configuration
func filterSnapshots(allFiles []string, config TimelapseConfig) []string {
	var filtered []string
	now := time.Now()
	cutoff := now.Add(-config.Duration)

	// Pre-filter files that are within the duration
	var recentFiles []string
	for _, file := range allFiles {
		// Extract timestamp from filename: snapshots/YYYY-MM/DD/HH/YYYY-MM-DD-HH-MM-SS.jpg
		parts := strings.Split(strings.TrimSuffix(filepath.Base(file), ".jpg"), "-")
		if len(parts) != 6 {
			continue // Invalid filename format
		}
		fileTime, err := time.Parse("2006-01-02-15-04-05", strings.Join(parts, "-"))
		if err != nil {
			continue
		}

		if fileTime.After(cutoff) {
			recentFiles = append(recentFiles, file)
		}
	}

	switch config.FramePattern {
	case "all":
		return recentFiles
	case "hourly":
		// Keep the first snapshot of every hour
		hourlyMap := make(map[string]string) // Key: YYYY-MM-DD-HH
		for _, file := range recentFiles {
			fileName := filepath.Base(file) // YYYY-MM-DD-HH-MM-SS.jpg
			if len(fileName) >= 13 {
				hourKey := fileName[:13]
				if _, exists := hourlyMap[hourKey]; !exists {
					hourlyMap[hourKey] = file
				}
			}
		}
		for _, file := range hourlyMap {
			filtered = append(filtered, file)
		}
	case "daily":
		// Keep the first snapshot of every day
		dailyMap := make(map[string]string) // Key: YYYY-MM-DD
		for _, file := range recentFiles {
			fileName := filepath.Base(file) // YYYY-MM-DD-HH-MM-SS.jpg
			if len(fileName) >= 10 {
				dayKey := fileName[:10]
				if _, exists := dailyMap[dayKey]; !exists {
					dailyMap[dayKey] = file
				}
			}
		}
		for _, file := range dailyMap {
			filtered = append(filtered, file)
		}
	}

	sort.Strings(filtered) // Ensure chronological order
	return filtered
}

func generateTimelapse(snapshotFiles []string, outputFileName string) error {
	listFileName := fmt.Sprintf("image_list_%s.txt", strings.TrimSuffix(outputFileName, ".webm"))
	imageListPath := filepath.Join(AppConfig.DataDir, listFileName)
	tempVideoPath := filepath.Join(AppConfig.DataDir, "temp_"+outputFileName)
	finalVideoPath := filepath.Join(AppConfig.DataDir, outputFileName)

	// Create image list file
	listFile, err := os.Create(imageListPath)
	if err != nil {
		return fmt.Errorf("failed to create image list: %w", err)
	}
	defer os.Remove(imageListPath) // Clean up list file afterward

	for _, file := range snapshotFiles {
		relativePath, err := filepath.Rel(AppConfig.DataDir, file)
		if err != nil {
			log.Printf("Warning: could not create relative path for %s: %v", file, err)
			continue
		}
		relativePath = filepath.ToSlash(relativePath)
		listFile.WriteString(fmt.Sprintf("file '%s'\n", relativePath))
		listFile.WriteString("duration 0.04\n") // 25fps
	}
	// Add last image again to ensure full duration
	if len(snapshotFiles) > 0 {
		relativePath, _ := filepath.Rel(AppConfig.DataDir, snapshotFiles[len(snapshotFiles)-1])
		listFile.WriteString(fmt.Sprintf("file '%s'\n", filepath.ToSlash(relativePath)))
	}
	listFile.Close()

	// FFmpeg command
	cmd := exec.Command("ffmpeg",
		"-f", "concat",
		"-safe", "0",
		"-i", imageListPath,
		"-r", "25",
		"-c:v", "libvpx-vp9", // Changed to VP9 for broader compatibility
		"-b:v", "0",          // Use CRF for quality
		"-crf", "35",         // Good balance of quality and size
		"-pix_fmt", "yuv420p",
		"-y", tempVideoPath,
	)

	// Capture FFmpeg output to main log
	logFile, err := os.OpenFile(AppConfig.FFmpegLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer logFile.Close()
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	log.Printf("Running FFmpeg for %s...", outputFileName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg execution failed: %w", err)
	}

	// Atomically replace the old video with the new one, after archiving the old one
	if fileExists(finalVideoPath) {
		archiveFileName := fmt.Sprintf("%s_%s.webm", strings.TrimSuffix(outputFileName, ".webm"), time.Now().Format("20060102_150405"))
		archiveVideoPath := filepath.Join(AppConfig.DataDir, archiveFileName)
		log.Printf("Archiving existing video to: %s", archiveVideoPath)
		if err := os.Rename(finalVideoPath, archiveVideoPath); err != nil {
			log.Printf("Warning: failed to archive video %s: %v", finalVideoPath, err)
		}
	}

	return os.Rename(tempVideoPath, finalVideoPath)
}

func cleanupSnapshots() {
	log.Println("Starting snapshot cleanup...")
	allSnapshots := getSnapshotFiles()

	// Determine the maximum duration we need to keep snapshots for.
	// This is the duration of the longest timelapse.
	maxDuration := time.Duration(0)
	for _, cfg := range timelapseConfigs {
		if cfg.Duration > maxDuration {
			maxDuration = cfg.Duration
		}
	}
	// Add a small buffer
	retentionCutoff := time.Now().Add(-maxDuration).Add(-1 * time.Hour)

	filesToDelete := 0
	for _, file := range allSnapshots {
		// Extract timestamp from filename
		parts := strings.Split(strings.TrimSuffix(filepath.Base(file), ".jpg"), "-")
		if len(parts) != 6 {
			continue // Skip malformed filenames
		}
		fileTime, err := time.Parse("2006-01-02-15-04-05", strings.Join(parts, "-"))
		if err != nil {
			continue
		}

		// If the file is older than our longest timelapse, it's a candidate for deletion.
		if fileTime.Before(retentionCutoff) {
			// A simpler rule: just delete anything older than the max duration.
			// The hourly/daily snapshots for the timelapses are selected from the pool,
			// so we can't delete other files from within the retention period.
			if err := os.Remove(file); err != nil {
				log.Printf("Warning: failed to remove snapshot %s: %v", file, err)
			} else {
				filesToDelete++
			}
		}
	}

	if filesToDelete > 0 {
		log.Printf("Snapshot cleanup complete. Removed %d old files.", filesToDelete)
	} else {
		log.Println("No old snapshots to clean up.")
	}
}

// This function is now called from generateAllTimelapses
func cleanOldVideos() {
	log.Printf("Starting video archive cleanup (retaining up to %d of each type)...", AppConfig.VideoArchivesToKeep)

	for _, cfg := range timelapseConfigs {
		prefix := fmt.Sprintf("timelapse_%s_", cfg.Name)
		files, err := os.ReadDir(AppConfig.DataDir)
		if err != nil {
			log.Printf("Error reading data directory for video cleanup: %v", err)
			return
		}

		var videoArchives []string
		for _, file := range files {
			if strings.HasPrefix(file.Name(), prefix) && strings.HasSuffix(file.Name(), ".webm") {
				videoArchives = append(videoArchives, file.Name())
			}
		}

		if len(videoArchives) <= AppConfig.VideoArchivesToKeep {
			// log.Printf("No old archives to remove for %s.", cfg.Name)
			continue
		}

		// Sort files by name - the timestamp ensures chronological order
		sort.Strings(videoArchives)

		// The first N files in the sorted list are the oldest
		filesToDeleteCount := len(videoArchives) - AppConfig.VideoArchivesToKeep
		filesToDelete := videoArchives[:filesToDeleteCount]

		for _, fileName := range filesToDelete {
			filePath := filepath.Join(AppConfig.DataDir, fileName)
			if err := os.Remove(filePath); err != nil {
				log.Printf("Error removing old video archive %s: %v", fileName, err)
			}
		}
		log.Printf("Finished cleanup for %s. Removed %d archive(s).", cfg.Name, len(filesToDelete))
	}
}

// --- UTILITY FUNCTIONS IMPLEMENTATION ---

// getSnapshotFiles recursively finds all snapshot files in the structured directory.
func getSnapshotFiles() []string {
	var files []string
	err := filepath.WalkDir(AppConfig.SnapshotsDir, func(path string, d os.DirEntry, err error) error {
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

func getTotalImagesCount() int {
	// This now counts unprocessed images waiting for the next timelapse generation.
	return len(getSnapshotFiles())
}

func getImagesDiskUsage() string {
	var totalSize int64
	err := filepath.Walk(AppConfig.DataDir, func(_ string, info os.FileInfo, err error) error {
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

func getLastImageTime() string {
	// This now reflects the most recent snapshot taken for the timelapse.
	files := getSnapshotFiles()
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

func getLastProcessedImageName() string {
	videoStatus.RLock()
	lastRun := videoStatus.LastRun
	videoStatus.RUnlock()

	if lastRun == nil {
		return "N/A"
	}
	return lastRun.Format("2006-01-02-15-04-05") + ".jpg"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func getSystemInfo() gin.H {
	// NOTE: This remains a placeholder for system-specific metrics.
	return gin.H{
		"os_type":      "Linux",
		"cpu_usage":    "0.2%",
		"memory_usage": "10.1%",
		"av1_status":   "Available (libvpx-vp9)",
	}
}

// getAvailableImageDates now scans the flat gallery directory.
func getAvailableImageDates() []string {
	files, err := os.ReadDir(AppConfig.GalleryDir)
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

// getDailyGallery now uses the dedicated, retained gallery images.
func getDailyGallery(dateStr string) []map[string]string {
	gallery := make([]map[string]string, 24)

	for i := 0; i < 24; i++ {
		hour := fmt.Sprintf("%02d", i)
		timeLabel := fmt.Sprintf("%s:00", hour)
		
		// Look for a specific file like 'YYYY-MM-DD-HH.jpg'
		galleryFileName := fmt.Sprintf("%s-%s.jpg", dateStr, hour)
		galleryFilePath := filepath.Join(AppConfig.GalleryDir, galleryFileName)
		
		url := ""
		available := "false"

		if fileExists(galleryFilePath) {
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