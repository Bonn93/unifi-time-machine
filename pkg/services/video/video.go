package video

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"time-machine/pkg/config"
	"time-machine/pkg/jobs"
	"time-machine/pkg/models"
	"time-machine/pkg/util"
)

// heavy AI assist here, review carefully... since FFPMEG, AV1 and WEBM is tricky

var (
	PreferredVideoCodec string
	ffmpegThreads       int
	onceDetectCapabilities sync.Once
)

func detectFFmpegCapabilities() {
	onceDetectCapabilities.Do(func() {
		log.Println("Detecting FFmpeg capabilities...")

		// Determine CPU core count
		ffmpegThreads = runtime.NumCPU()
		if ffmpegThreads > 8 { // Cap threads to 8 to avoid excessive resource usage for FFmpeg
			ffmpegThreads = 8
		}
		if ffmpegThreads < 1 {
			ffmpegThreads = 1 // Ensure at least one thread
		}
		log.Printf("Detected %d CPU cores, setting FFmpeg threads to %d.", runtime.NumCPU(), ffmpegThreads)

		// Check for libaom-av1
		cmd := exec.Command("ffmpeg", "-hide_banner", "-encoders")
		output, err := cmd.Output()
		if err != nil {
			log.Printf("WARNING: Could not run ffmpeg -encoders to detect codecs: %v. Falling back to libvpx-vp9.", err)
			PreferredVideoCodec = "libvpx-vp9"
			return
		}

		// Check for libsvtav1, then libaom-av1
		if strings.Contains(string(output), "libsvtav1") {
			PreferredVideoCodec = "libsvtav1"
			log.Println("Detected libsvtav1 encoder. Will use SVT-AV1 for timelapses.")
		} else if strings.Contains(string(output), "libaom-av1") {
			PreferredVideoCodec = "libaom-av1"
			log.Println("Detected libaom-av1 encoder. Will use AOM-AV1 for timelapses.")
		} else {
			PreferredVideoCodec = "libvpx-vp9"
			log.Println("Neither SVT-AV1 nor AOM-AV1 detected. Falling back to libvpx-vp9 for timelapses.")
		}
	})
}

var createVideoSegment = func(imagePath, segmentPath string) error {
	log.Printf("Creating video segment for %s using codec %s with %d threads...", filepath.Base(imagePath), PreferredVideoCodec, ffmpegThreads)

	// FFmpeg command to create a single-frame WebM segment.
	// Parameters are aligned with regenerateFullTimelapse to ensure concat compatibility.
	// We use a video filter to force the conversion from JPEG (Full Range) to Video (TV Range)
	// scale=out_color_matrix=bt709:out_range=tv forces the math conversion.
	// format=yuv420p ensures the pixel format is compatible with WebM/AV1.
	videoFilter := "scale=out_color_matrix=bt709:out_range=tv,format=yuv420p"

	var cmd *exec.Cmd
	if PreferredVideoCodec == "libsvtav1" {
		cmd = exec.Command("ffmpeg",
			"-hide_banner",
			"-loglevel", "error",
			"-loop", "1",
			"-i", imagePath,
			"-t", "0.0333", // 1 frame at 30fps
			"-vf", videoFilter, // <--- CRITICAL FIX
			"-c:v", PreferredVideoCodec,
			"-preset", "8",
			"-threads", fmt.Sprintf("%d", ffmpegThreads),
			"-g", "1", // Force Intra frame
			"-keyint_min", "1",
			"-crf", config.AppConfig.GetCRFValue(),
			"-an",
			"-f", "webm",
			"-y", segmentPath,
		)
	} else {
		// Apply the same fix to the fallback block
		cmd = exec.Command("ffmpeg",
			"-hide_banner",
			"-loglevel", "error",
			"-loop", "1",
			"-i", imagePath,
			"-t", "0.0333",
			"-vf", videoFilter, // <--- CRITICAL FIX
			"-c:v", PreferredVideoCodec,
			"-threads", fmt.Sprintf("%d", ffmpegThreads),
			"-g", "1",
			"-keyint_min", "1",
			"-crf", config.AppConfig.GetCRFValue(),
			"-an",
			"-f", "webm",
			"-y", segmentPath,
		)
	}

	logFile, err := os.OpenFile(config.GetFFmpegLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open FFmpeg log file: %w", err)
	}
	defer logFile.Close()
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg (create segment) execution failed for %s: %w", imagePath, err)
	}
	log.Printf("Successfully created segment: %s", segmentPath)
	return nil
}

// used .txt extension for concat list to some issues as ffprobes was doing weird things with frame counts

var concatenateVideos = func(existingVideoPath, newSegmentPath, outputVideoPath string) error {
	log.Printf("Concatenating %s and %s into %s...", filepath.Base(existingVideoPath), filepath.Base(newSegmentPath), filepath.Base(outputVideoPath))

	concatListPath := "concat_list.txt" // Relative to DataDir
	fullConcatListPath := filepath.Join(config.AppConfig.DataDir, concatListPath)
	listFile, err := os.Create(fullConcatListPath)
	if err != nil {
		return fmt.Errorf("failed to create concat list: %w", err)
	}
	defer os.Remove(fullConcatListPath) // Clean up list file

	// Using ToSlash for cross-platform compatibility in the list file.
	_, err = listFile.WriteString(fmt.Sprintf("file '%s'\n", filepath.ToSlash(filepath.Base(existingVideoPath))))
	if err != nil {
		listFile.Close()
		return fmt.Errorf("failed to write existing video to concat list: %w", err)
	}
	_, err = listFile.WriteString(fmt.Sprintf("file '%s'\n", filepath.ToSlash(filepath.Base(newSegmentPath))))
	if err != nil {
		listFile.Close()
		return fmt.Errorf("failed to write new segment to concat list: %w", err)
	}
	listFile.Close() // Close before FFmpeg tries to read it

	tempOutput := filepath.Base(outputVideoPath)

	// Use stream copy (-c copy) for concatenation. This is extremely fast and avoids re-encoding.
	// It requires that all segments are perfectly compatible, which our createVideoSegment function now ensures.
	cmd := exec.Command("ffmpeg",
		"-f", "concat",
		"-safe", "0",
		"-i", concatListPath,
		"-c", "copy", // Stream copy, not re-encode
		"-threads", fmt.Sprintf("%d", ffmpegThreads),
		"-y", tempOutput,
	)
	cmd.Dir = config.AppConfig.DataDir

	logFile, err := os.OpenFile(config.GetFFmpegLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open FFmpeg log file: %w", err)
	}
	defer logFile.Close()
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg (concatenate) execution failed: %w", err)
	}
	log.Printf("Successfully concatenated videos into: %s", outputVideoPath)
	return nil
}

// Helper to get the path of the sidecar file
func getLastSnapshotTrackerPath(timelapseName string) string {
	return filepath.Join(config.AppConfig.DataDir, fmt.Sprintf("timelapse_%s.last_snapshot.txt", timelapseName))
}

// readLastAppendedSnapshot reads the path of the last snapshot appended to a timelapse from its tracker file.
var readLastAppendedSnapshot = func(timelapseName string) (string, error) {
	trackerPath := getLastSnapshotTrackerPath(timelapseName)
	content, err := os.ReadFile(trackerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // File not found is not an error, just means no snapshot tracked yet
		}
		return "", fmt.Errorf("failed to read last snapshot tracker for %s: %w", timelapseName, err)
	}
	return strings.TrimSpace(string(content)), nil
}

// writeLastAppendedSnapshot writes the path of the last snapshot appended to a timelapse to its tracker file.
var writeLastAppendedSnapshot = func(timelapseName, snapshotPath string) error {
	trackerPath := getLastSnapshotTrackerPath(timelapseName)
	err := os.WriteFile(trackerPath, []byte(snapshotPath), 0644)
	if err != nil {
		return fmt.Errorf("failed to write last snapshot tracker for %s: %w", timelapseName, err)
	}
	return nil
}

// --- VIDEO GENERATION AND CLEANUP IMPLEMENTATION ---

func StartVideoGeneratorScheduler() {
	detectFFmpegCapabilities() // Detect capabilities once at startup
	for {
		time.Sleep(time.Duration(config.AppConfig.VideoCronIntervalSec) * time.Second)
		EnqueueTimelapseJobs()
	}
}

func EnqueueTimelapseJobs() {
	log.Println("Enqueuing timelapse generation jobs...")

	// Dynamically enqueue jobs for daily 24-hour snapshots
	for i := 0; i < config.AppConfig.DaysOf24HourSnapshots; i++ {
		targetDate := time.Now().AddDate(0, 0, -i)
		timelapseName := fmt.Sprintf("24_hour_%s", targetDate.Format("2006-01-02"))
		payload := map[string]string{"timelapse_name": timelapseName}
		_, err := jobs.CreateJob("generate_timelapse", payload)
		if err != nil {
			log.Printf("Error enqueuing job for daily timelapse %s: %v", timelapseName, err)
		}
	}

	for _, cfg := range models.TimelapseConfigsData {
		payload := map[string]string{"timelapse_name": cfg.Name}
		_, err := jobs.CreateJob("generate_timelapse", payload)
		if err != nil {
			log.Printf("Error enqueuing job for timelapse %s: %v", cfg.Name, err)
		}
	}

	if _, err := jobs.CreateJob("cleanup_snapshots", nil); err != nil {
		log.Printf("Error enqueuing cleanup_snapshots job: %v", err)
	}
	if _, err := jobs.CreateJob("cleanup_videos", nil); err != nil {
		log.Printf("Error enqueuing cleanup_videos job: %v", err)
	}
	if _, err := jobs.CreateJob("cleanup_logs", nil); err != nil {
		log.Printf("Error enqueuing cleanup_logs job: %v", err)
	}
	if _, err := jobs.CreateJob("cleanup_gallery", nil); err != nil {
		log.Printf("Error enqueuing cleanup_gallery job: %v", err)
	}
}


var GenerateSingleTimelapse = func(timelapseName string) error {
	log.Printf("--- Processing timelapse: %s ---", timelapseName)
	detectFFmpegCapabilities()

	var cfg models.TimelapseConfig
	var targetDate = time.Now()

	if strings.HasPrefix(timelapseName, "24_hour_") {
		// This is a dynamically generated 24-hour daily timelapse
		dateStr := strings.TrimPrefix(timelapseName, "24_hour_")
		parsedDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			return fmt.Errorf("invalid date format in timelapse name %s: %w", timelapseName, err)
		}
		targetDate = parsedDate
		cfg = models.TimelapseConfig{
			Name:         timelapseName,
			Duration:     24 * time.Hour,
			FramePattern: "all",
		}
	} else {
		// This is one of the pre-defined timelapses (1_week, 1_month, 1_year)
		for _, c := range models.TimelapseConfigsData {
			if c.Name == timelapseName {
				cfg = c
				break
			}
		}
		if cfg.Name == "" {
			return fmt.Errorf("no timelapse configuration found for name: %s", timelapseName)
		}
	}

	allSnapshots := util.GetSnapshotFiles()
	if len(allSnapshots) == 0 {
		log.Println("No snapshots available to generate videos.")
		return nil
	}

	outputFileName := fmt.Sprintf("timelapse_%s.webm", cfg.Name)
	finalVideoPath := filepath.Join(config.AppConfig.DataDir, outputFileName)

	snapshotsForTimelapse := filterSnapshots(allSnapshots, cfg, targetDate)

	if len(snapshotsForTimelapse) == 0 {
		log.Printf("No snapshots available for %s timelapse, skipping.", cfg.Name)
		return nil
	}

	lastAppendedSnapshotPath, err := readLastAppendedSnapshot(cfg.Name)
	if err != nil {
		log.Printf("ERROR reading last appended snapshot for %s: %v. Forcing full regeneration.", cfg.Name, err)
		lastAppendedSnapshotPath = "" // Force full regeneration
	}

	startIndex := 0
	if lastAppendedSnapshotPath != "" {
		for i, s := range snapshotsForTimelapse {
			if s == lastAppendedSnapshotPath {
				startIndex = i + 1
				break
			}
		}
	}

	if !util.FileExists(finalVideoPath) || util.IsFileEmpty(finalVideoPath) || startIndex == 0 {
		log.Printf("Initial generation or regeneration for %s timelapse (video missing/empty or tracker invalid).", cfg.Name)
		err := regenerateFullTimelapse(snapshotsForTimelapse, outputFileName)
		if err != nil {
			return fmt.Errorf("error generating %s timelapse: %w", cfg.Name, err)
		}
		log.Printf("✅ Successfully generated initial/regenerated %s timelapse.", cfg.Name)
		if len(snapshotsForTimelapse) > 0 {
			if err := writeLastAppendedSnapshot(cfg.Name, snapshotsForTimelapse[len(snapshotsForTimelapse)-1]); err != nil {
				log.Printf("ERROR writing last appended snapshot for %s after full regeneration: %v", cfg.Name, err)
			}
		}
	} else if startIndex < len(snapshotsForTimelapse) {
		newSnapshotsToAppend := snapshotsForTimelapse[startIndex:]
		log.Printf("Incremental update for %s: appending %d new snapshots.", cfg.Name, len(newSnapshotsToAppend))

		for i, newSnapshot := range newSnapshotsToAppend {
			log.Printf("Appending snapshot %d/%d: %s", i+1, len(newSnapshotsToAppend), filepath.Base(newSnapshot))
			tempSegmentPath := filepath.Join(config.AppConfig.DataDir, fmt.Sprintf("temp_segment_%s_%d.webm", cfg.Name, i))
			tempConcatenatedVideoPath := filepath.Join(config.AppConfig.DataDir, fmt.Sprintf("temp_concat_video_%s_%d.webm", cfg.Name, i))

			err := createVideoSegment(newSnapshot, tempSegmentPath)
			if err != nil {
				return fmt.Errorf("error creating segment for %s: %w", newSnapshot, err)
			}

			err = concatenateVideos(finalVideoPath, tempSegmentPath, tempConcatenatedVideoPath)
			os.Remove(tempSegmentPath)
			if err != nil {
				return fmt.Errorf("error concatenating for %s: %w", finalVideoPath, err)
			}

			if err := os.Rename(tempConcatenatedVideoPath, finalVideoPath); err != nil {
				return fmt.Errorf("error renaming new video %s to %s: %w", tempConcatenatedVideoPath, finalVideoPath, err)
			}
			time.Sleep(100 * time.Millisecond)
			log.Printf("✅ Appended %s to %s.", filepath.Base(newSnapshot), cfg.Name)

			if err := writeLastAppendedSnapshot(cfg.Name, newSnapshot); err != nil {
				log.Printf("ERROR writing last appended snapshot for %s: %v", cfg.Name, err)
			}
		}
	} else {
		log.Printf("No new snapshots to append for %s timelapse.", cfg.Name)
	}

	return nil
}

var filterSnapshots = func(allFiles []string, config models.TimelapseConfig, targetTime time.Time) []string {
	var filtered []string

	// Determine the start and end of the filtering window
	var windowStart, windowEnd time.Time

	if strings.HasPrefix(config.Name, "24_hour_") {
		// For daily 24-hour snapshots, filter for the entire target day
		windowStart = targetTime.Truncate(24 * time.Hour)
		windowEnd = windowStart.Add(24 * time.Hour)
	} else {
		// For other timelapses, filter backwards from the targetTime for the specified duration
		windowStart = targetTime.Add(-config.Duration)
		windowEnd = targetTime
	}

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

		// Check if the file's timestamp is within the window [windowStart, windowEnd)
		if !fileTime.Before(windowStart) && fileTime.Before(windowEnd) {
			recentFiles = append(recentFiles, file)
		}
	}

	switch config.FramePattern {
	case "all":
		filtered = recentFiles
	case "hourly":
		var lastHour string
		for _, file := range recentFiles {
			fileName := filepath.Base(file)
			if len(fileName) >= 13 {
				hourKey := fileName[:13]
				if hourKey != lastHour {
					filtered = append(filtered, file)
					lastHour = hourKey
				}
			}
		}
	case "daily":
		var lastDay string
		for _, file := range recentFiles {
			fileName := filepath.Base(file)
			if len(fileName) >= 10 {
				dayKey := fileName[:10]
				if dayKey != lastDay {
					filtered = append(filtered, file)
					lastDay = dayKey
				}
			}
		}
	}

	sort.Strings(filtered) // Ensure chronological order
	return filtered
}
var regenerateFullTimelapse = func(snapshotFiles []string, outputFileName string) error {
	listFileName := fmt.Sprintf("image_list_%s.txt", strings.TrimSuffix(outputFileName, ".webm"))
	imageListPath := filepath.Join(config.AppConfig.DataDir, listFileName)
	tempVideoPath := filepath.Join(config.AppConfig.DataDir, "temp_"+outputFileName)
	finalVideoPath := filepath.Join(config.AppConfig.DataDir, outputFileName)

	// Create image list file
	listFile, err := os.Create(imageListPath)
	if err != nil {
		return fmt.Errorf("failed to create image list: %w", err)
	}
	defer os.Remove(imageListPath) // Clean up list file afterward

	for _, file := range snapshotFiles {
		relativePath, err := filepath.Rel(config.AppConfig.DataDir, file)
		if err != nil {
			log.Printf("Warning: could not create relative path for %s: %v", file, err)
			continue
		}
		relativePath = filepath.ToSlash(relativePath)
		listFile.WriteString(fmt.Sprintf("file '%s'\n", relativePath))
		listFile.WriteString(fmt.Sprintf("duration %f\n", 0.0333)) // Duration for 30 FPS
	}
	// Add last image again to ensure full duration
	if len(snapshotFiles) > 0 {
		relativePath, _ := filepath.Rel(config.AppConfig.DataDir, snapshotFiles[len(snapshotFiles)-1])
		listFile.WriteString(fmt.Sprintf("file '%s'\n", filepath.ToSlash(relativePath)))
	}
	listFile.Close()

	// FFmpeg command
	cmd := exec.Command("ffmpeg",
		"-f", "concat",
		"-safe", "0",
		"-i", listFileName,
		"-vf", "scale=out_color_matrix=bt709:out_range=tv,format=yuv420p",
		"-r", "30", // Set output framerate to 30 FPS
		"-c:v", PreferredVideoCodec, // Use the detected preferred codec
		"-threads", fmt.Sprintf("%d", ffmpegThreads),
		"-b:v", "0", // Use CRF for quality
		"-crf", config.AppConfig.GetCRFValue(), // Good balance of quality and size
		"-y", "temp_"+outputFileName,
	)
	cmd.Dir = config.AppConfig.DataDir

	// Capture FFmpeg output to main log
	logFile, err := os.OpenFile(config.GetFFmpegLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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
	if util.FileExists(finalVideoPath) {
		archiveFileName := fmt.Sprintf("%s_%s.webm", strings.TrimSuffix(outputFileName, ".webm"), time.Now().Format("20060102_150405"))
		archiveVideoPath := filepath.Join(config.AppConfig.DataDir, archiveFileName)
		log.Printf("Archiving existing video to: %s", archiveVideoPath)
		if err := os.Rename(finalVideoPath, archiveVideoPath); err != nil {
			log.Printf("Warning: failed to archive video %s: %v", finalVideoPath, err)
		}
	}

	if err := os.Rename(tempVideoPath, finalVideoPath); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond) // Give OS time to update file metadata
	return nil
}

var CleanupSnapshots = func() {
	log.Println("Starting snapshot cleanup...")
	allSnapshots := util.GetSnapshotFiles()
	if len(allSnapshots) == 0 {
		log.Println("No snapshot files found to cleanup.")
		return
	}

	retentionDays := config.AppConfig.SnapshotRetentionDays
	retentionCutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	log.Printf("Snapshot retention is %d days. Deleting files older than %s", retentionDays, retentionCutoff.Format("2006-01-02 15:04:05"))

	filesToDelete := 0
	filesKept := 0

	for _, file := range allSnapshots {
		// Extract timestamp from filename
		parts := strings.Split(strings.TrimSuffix(filepath.Base(file), ".jpg"), "-")
		if len(parts) != 6 {
			log.Printf("Skipping malformed snapshot filename: %s", file)
			continue // Skip malformed filenames
		}
		fileTime, err := time.Parse("2006-01-02-15-04-05", strings.Join(parts, "-"))
		if err != nil {
			log.Printf("Skipping snapshot with unparsable time: %s", file)
			continue
		}

		if fileTime.Before(retentionCutoff) {
			if err := os.Remove(file); err != nil {
				log.Printf("Warning: failed to remove snapshot %s: %v", file, err)
			} else {
				filesToDelete++
			}
		} else {
			filesKept++
		}
	}

	log.Printf("Snapshot cleanup finished. Kept %d files, removed %d files.", filesKept, filesToDelete)
}

// This function is now called from GenerateSingleTimelapse
var CleanOldVideos = func() {
	log.Printf("Starting video cleanup...")

	// Clean up dynamically generated daily 24-hour timelapses
	log.Printf("Cleaning up daily 24-hour timelapses (retaining last %d days)...", config.AppConfig.DaysOf24HourSnapshots)
	files, err := os.ReadDir(config.AppConfig.DataDir)
	if err != nil {
		log.Printf("Error reading data directory for daily video cleanup: %v", err)
		return
	}

	// Calculate the cutoff date for daily videos
	cutoffDate := time.Now().AddDate(0, 0, -config.AppConfig.DaysOf24HourSnapshots).Truncate(24 * time.Hour)
	dailyVideosRemoved := 0

	for _, file := range files {
		fileName := file.Name()
		if strings.HasPrefix(fileName, "timelapse_24_hour_") && strings.HasSuffix(fileName, ".webm") {
			// Extract date from filename: timelapse_24_hour_YYYY-MM-DD.webm
			dateStr := fileName[len("timelapse_24_hour_") : len("timelapse_24_hour_")+10] // "YYYY-MM-DD"
			fileDate, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				log.Printf("Warning: could not parse date from daily timelapse video %s: %v", fileName, err)
				continue
			}

			// If the video's date is before the cutoff date, delete it
			if fileDate.Before(cutoffDate) {
				filePath := filepath.Join(config.AppConfig.DataDir, fileName)
				if err := os.Remove(filePath); err != nil {
					log.Printf("Error removing old daily timelapse video %s: %v", fileName, err)
				} else {
					dailyVideosRemoved++
				}
			}
		}
	}
	log.Printf("Finished cleaning up daily 24-hour timelapses. Removed %d old daily videos.", dailyVideosRemoved)

	// Clean up other pre-defined timelapses (1_week, 1_month, 1_year)
	log.Printf("Cleaning up other timelapses (retaining up to %d archives of each type)...", config.AppConfig.VideoArchivesToKeep)
	for _, cfg := range models.TimelapseConfigsData {
		prefix := fmt.Sprintf("timelapse_%s_", cfg.Name) // This prefix will correctly match
		files, err := os.ReadDir(config.AppConfig.DataDir)
		if err != nil {
			log.Printf("Error reading data directory for video cleanup: %v", err)
			continue
		}

		var videoArchives []string
		for _, file := range files {
			if strings.HasPrefix(file.Name(), prefix) && strings.HasSuffix(file.Name(), ".webm") {
				videoArchives = append(videoArchives, file.Name())
			}
		}

		if len(videoArchives) <= config.AppConfig.VideoArchivesToKeep {
			continue
		}

		sort.Strings(videoArchives)

		filesToDeleteCount := len(videoArchives) - config.AppConfig.VideoArchivesToKeep
		filesToDelete := videoArchives[:filesToDeleteCount]
		removedCount := 0

		for _, fileName := range filesToDelete {
			filePath := filepath.Join(config.AppConfig.DataDir, fileName)
			if err := os.Remove(filePath); err != nil {
				log.Printf("Error removing old video archive %s: %v", fileName, err)
			} else {
				removedCount++
			}
		}
		log.Printf("Finished cleanup for %s. Removed %d archive(s).", cfg.Name, removedCount)
	}
}

var CleanupGallery = func() {
	log.Println("Starting gallery cleanup...")
	galleryPath := config.AppConfig.GalleryDir
	files, err := filepath.Glob(filepath.Join(galleryPath, "*.jpg"))
	if err != nil {
		log.Printf("Error finding gallery files for cleanup: %v", err)
		return
	}

	retentionCutoff := time.Now().Add(-time.Duration(config.AppConfig.SnapshotRetentionDays) * 24 * time.Hour)
	filesToDelete := 0

	for _, file := range files {
		name := filepath.Base(file)
		// Name is YYYY-MM-DD-HH.jpg
		dateStr := strings.TrimSuffix(name, ".jpg")
		fileTime, err := time.Parse("2006-01-02-15", dateStr)
		if err != nil {
			log.Printf("Warning: could not parse date from gallery file %s: %v", name, err)
			continue
		}

		if fileTime.Before(retentionCutoff) {
			if err := os.Remove(file); err != nil {
				log.Printf("Warning: failed to remove gallery file %s: %v", file, err)
			} else {
				filesToDelete++
			}
		}
	}

	if filesToDelete > 0 {
		log.Printf("Gallery cleanup complete. Removed %d old files.", filesToDelete)
	} else {
		log.Println("No old gallery files to clean up.")
	}
}

var CleanupLogFiles = func() {
	log.Println("Starting log file cleanup...")
	files, err := filepath.Glob(filepath.Join(config.AppConfig.DataDir, "ffmpeg_log_*.txt"))
	if err != nil {
		log.Printf("Error finding log files for cleanup: %v", err)
		return
	}

	retentionDuration := 7 * 24 * time.Hour
	cutoff := time.Now().Add(-retentionDuration)
	filesToDelete := 0

	for _, file := range files {
		name := filepath.Base(file)
		dateStr := strings.TrimSuffix(strings.TrimPrefix(name, "ffmpeg_log_"), ".txt")
		fileDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			log.Printf("Warning: could not parse date from log file %s: %v", name, err)
			continue
		}

		if fileDate.Before(cutoff) {
			if err := os.Remove(file); err != nil {
				log.Printf("Warning: failed to remove log file %s: %v", file, err)
			} else {
				filesToDelete++
			}
		}
	}

	if filesToDelete > 0 {
		log.Printf("Log file cleanup complete. Removed %d old log(s).", filesToDelete)
	} else {
		log.Println("No old log files to clean up.")
	}
}		