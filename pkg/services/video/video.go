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

func createVideoSegment(imagePath, segmentPath string) error {
	log.Printf("Creating video segment for %s using codec %s with %d threads...", filepath.Base(imagePath), PreferredVideoCodec, ffmpegThreads)

	// FFmpeg command to create a single-frame WebM segment.
	// Parameters are aligned with regenerateFullTimelapse to ensure concat compatibility.
	var cmd *exec.Cmd
	if PreferredVideoCodec == "libsvtav1" {
		cmd = exec.Command("ffmpeg",
			"-loop", "1",
			"-i", imagePath,
			"-t", "0.0333", // Duration for a single frame at 30 FPS
			"-r", "30", // Set segment framerate to 30
			"-c:v", PreferredVideoCodec,
			"-preset", "8",
			"-threads", fmt.Sprintf("%d", ffmpegThreads),
			"-g", "1",
			"-keyint_min", "1",
			"-crf", config.AppConfig.GetCRFValue(), // Matched with regenerateFullTimelapse
			"-pix_fmt", "yuv420p", // Ensure consistent pixel format
			"-an",
			"-f", "webm",
			"-y", segmentPath,
		)
	} else {
		cmd = exec.Command("ffmpeg",
			"-loop", "1",
			"-i", imagePath,
			"-t", "0.0333", // Duration for a single frame at 30 FPS
			"-r", "30", // Set segment framerate to 30
			"-c:v", PreferredVideoCodec,
			"-threads", fmt.Sprintf("%d", ffmpegThreads),
			"-g", "1",
			"-keyint_min", "1",
			"-crf", config.AppConfig.GetCRFValue(), // Matched with regenerateFullTimelapse
			"-pix_fmt", "yuv420p", // Ensure consistent pixel format
			"-an",
			"-f", "webm",
			"-y", segmentPath,
		)
	}

	logFile, err := os.OpenFile(config.AppConfig.FFmpegLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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

func concatenateVideos(existingVideoPath, newSegmentPath, outputVideoPath string) error {
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

	// Re-encode instead of stream copying for robustness. This avoids errors if segments have minor differences.
	cmd := exec.Command("ffmpeg",
		"-f", "concat",
		"-safe", "0",
		"-i", concatListPath,
		"-c:v", PreferredVideoCodec,
		"-b:v", "0",
		"-crf", config.AppConfig.GetCRFValue(),
		"-pix_fmt", "yuv420p",
		"-r", "30", // Set output framerate to 30 FPS
		"-y", tempOutput,
	)
	cmd.Dir = config.AppConfig.DataDir

	logFile, err := os.OpenFile(config.AppConfig.FFmpegLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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
func readLastAppendedSnapshot(timelapseName string) (string, error) {
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
func writeLastAppendedSnapshot(timelapseName, snapshotPath string) error {
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
}


func GenerateSingleTimelapse(timelapseName string) error {
	log.Printf("--- Processing timelapse: %s ---", timelapseName)

	var cfg models.TimelapseConfig
	for _, c := range models.TimelapseConfigsData {
		if c.Name == timelapseName {
			cfg = c
			break
		}
	}
	if cfg.Name == "" {
		return fmt.Errorf("no timelapse configuration found for name: %s", timelapseName)
	}

	allSnapshots := util.GetSnapshotFiles()
	if len(allSnapshots) == 0 {
		log.Println("No snapshots available to generate videos.")
		return nil
	}

	outputFileName := fmt.Sprintf("timelapse_%s.webm", cfg.Name)
	finalVideoPath := filepath.Join(config.AppConfig.DataDir, outputFileName)

	snapshotsForTimelapse := filterSnapshots(allSnapshots, cfg)

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

			if err := os.Remove(finalVideoPath); err != nil {
				log.Printf("WARNING: Failed to remove old video %s during update: %v", finalVideoPath, err)
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

// filterSnapshots selects files based on the timelapse configuration
func filterSnapshots(allFiles []string, config models.TimelapseConfig) []string {
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

func regenerateFullTimelapse(snapshotFiles []string, outputFileName string) error {
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
		"-r", "30", // Set output framerate to 30 FPS
		"-c:v", PreferredVideoCodec, // Use the detected preferred codec
		"-b:v", "0",          // Use CRF for quality
		"-crf", config.AppConfig.GetCRFValue(),         // Good balance of quality and size
		"-pix_fmt", "yuv420p",
		"-y", "temp_"+outputFileName,
	)
	cmd.Dir = config.AppConfig.DataDir

	// Capture FFmpeg output to main log
	logFile, err := os.OpenFile(config.AppConfig.FFmpegLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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

func CleanupSnapshots() {
	log.Println("Starting snapshot cleanup...")
	allSnapshots := util.GetSnapshotFiles()

	// Determine the maximum duration we need to keep snapshots for.
	// This is the duration of the longest timelapse.
	maxDuration := time.Duration(0)
	for _, cfg := range models.TimelapseConfigsData {
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

// This function is now called from GenerateSingleTimelapse
func CleanOldVideos() {
	log.Printf("Starting video archive cleanup (retaining up to %d of each type)...", config.AppConfig.VideoArchivesToKeep)

	for _, cfg := range models.TimelapseConfigsData {
		prefix := fmt.Sprintf("timelapse_%s_", cfg.Name)
		files, err := os.ReadDir(config.AppConfig.DataDir)
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

		if len(videoArchives) <= config.AppConfig.VideoArchivesToKeep {
			// log.Printf("No old archives to remove for %s.", cfg.Name)
			continue
		}

		// Sort files by name - the timestamp ensures chronological order
		sort.Strings(videoArchives)

		// The first N files in the sorted list are the oldest
		filesToDeleteCount := len(videoArchives) - config.AppConfig.VideoArchivesToKeep
		filesToDelete := videoArchives[:filesToDeleteCount]

		for _, fileName := range filesToDelete {
			filePath := filepath.Join(config.AppConfig.DataDir, fileName)
			if err := os.Remove(filePath); err != nil {
				log.Printf("Error removing old video archive %s: %v", fileName, err)
			}
		}
		log.Printf("Finished cleanup for %s. Removed %d archive(s).", cfg.Name, len(filesToDelete))
	}
}