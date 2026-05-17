package video

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
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
	PreferredVideoCodec    string
	ffmpegThreads          int
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
	// 1. Input Validation
	info, err := os.Stat(imagePath)
	if err != nil || info.Size() == 0 {
		return fmt.Errorf("invalid snapshot file (not found or zero size): %s", imagePath)
	}

	log.Printf("Creating video segment for %s using codec %s with %d threads...", filepath.Base(imagePath), PreferredVideoCodec, ffmpegThreads)

	// 2. Process Control (Timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	videoFilter := "scale=out_color_matrix=bt709:out_range=tv,format=yuv420p"
	var cmd *exec.Cmd
	if PreferredVideoCodec == "libsvtav1" {
		cmd = exec.CommandContext(ctx, "ffmpeg",
			"-hide_banner",
			"-loglevel", "error",
			"-loop", "1",
			"-i", imagePath,
			"-t", "0.0333", // 1 frame at 30fps
			"-vf", videoFilter,
			"-c:v", PreferredVideoCodec,
			"-preset", "10",
			"-threads", fmt.Sprintf("%d", ffmpegThreads),
			"-g", "1", // Force Intra frame
			"-keyint_min", "1",
			"-crf", config.AppConfig.GetCRFValue(),
			"-an",
			"-f", "webm",
			"-y", segmentPath,
		)
	} else {
		cmd = exec.CommandContext(ctx, "ffmpeg",
			"-hide_banner",
			"-loglevel", "error",
			"-loop", "1",
			"-i", imagePath,
			"-t", "0.0333",
			"-vf", videoFilter,
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

	// 3. Safe Logging
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		// Only log the output if the command actually failed
		log.Printf("FFmpeg failed: %v. Output: %s", err, stderr.String())
		// Also write to the daily log file for optional debugging
		logFile, logErr := os.OpenFile(config.GetFFmpegLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if logErr == nil {
			logFile.WriteString(fmt.Sprintf("--- FFmpeg Error: %s ---\n%s\n", time.Now(), stderr.String()))
			logFile.Close()
		}
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

// calendarWeekMonday returns the Monday of the week containing t, at midnight.
func calendarWeekMonday(t time.Time) time.Time {
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday = 7 in ISO week
	}
	return t.AddDate(0, 0, -(weekday - 1)).Truncate(24 * time.Hour)
}

func EnqueueTimelapseJobs() {
	log.Println("Enqueuing timelapse generation jobs...")
	now := time.Now()

	// Daily 24-hour timelapses
	for i := 0; i < config.AppConfig.DaysOf24HourSnapshots; i++ {
		targetDate := now.AddDate(0, 0, -i)
		timelapseName := fmt.Sprintf("24_hour_%s", targetDate.Format("2006-01-02"))
		if _, err := jobs.CreateJob("generate_timelapse", map[string]string{"timelapse_name": timelapseName}); err != nil {
			log.Printf("Error enqueuing job for daily timelapse %s: %v", timelapseName, err)
		}
	}

	// Calendar-week timelapses: last WeeklyLapsesToKeep Mondays
	currentMonday := calendarWeekMonday(now)
	for i := 0; i < config.AppConfig.WeeklyLapsesToKeep; i++ {
		monday := currentMonday.AddDate(0, 0, -7*i)
		timelapseName := fmt.Sprintf("week_%s", monday.Format("2006-01-02"))
		if _, err := jobs.CreateJob("generate_timelapse", map[string]string{"timelapse_name": timelapseName}); err != nil {
			log.Printf("Error enqueuing job for weekly timelapse %s: %v", timelapseName, err)
		}
	}

	// Calendar-month timelapses: last MonthlyLapsesToKeep months
	for i := 0; i < config.AppConfig.MonthlyLapsesToKeep; i++ {
		monthStart := time.Date(now.Year(), now.Month()-time.Month(i), 1, 0, 0, 0, 0, now.Location())
		timelapseName := fmt.Sprintf("month_%s", monthStart.Format("2006-01"))
		if _, err := jobs.CreateJob("generate_timelapse", map[string]string{"timelapse_name": timelapseName}); err != nil {
			log.Printf("Error enqueuing job for monthly timelapse %s: %v", timelapseName, err)
		}
	}

	// Year-to-date timelapse
	yearName := fmt.Sprintf("year_%d", now.Year())
	if _, err := jobs.CreateJob("generate_timelapse", map[string]string{"timelapse_name": yearName}); err != nil {
		log.Printf("Error enqueuing job for yearly timelapse %s: %v", yearName, err)
	}

	for _, jobType := range []string{"cleanup_snapshots", "cleanup_videos", "cleanup_logs", "cleanup_gallery"} {
		if _, err := jobs.CreateJob(jobType, nil); err != nil {
			log.Printf("Error enqueuing %s job: %v", jobType, err)
		}
	}
}

var GenerateSingleTimelapse = func(timelapseName string) error {
	log.Printf("--- Processing timelapse: %s ---", timelapseName)
	detectFFmpegCapabilities()

	var cfg models.TimelapseConfig
	var targetDate = time.Now()
	var useGallery bool

	switch {
	case strings.HasPrefix(timelapseName, "24_hour_"):
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

	case strings.HasPrefix(timelapseName, "week_"):
		// Calendar week: Monday to Sunday, fixed window, sourced from gallery
		dateStr := strings.TrimPrefix(timelapseName, "week_")
		monday, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			return fmt.Errorf("invalid date format in timelapse name %s: %w", timelapseName, err)
		}
		// Skip if outside the active retention window — stale queue jobs can reference old weeks.
		currentMonday := calendarWeekMonday(time.Now())
		keepWeeks := config.AppConfig.WeeklyLapsesToKeep
		if keepWeeks < 1 {
			keepWeeks = 1
		}
		oldestAllowedWeek := currentMonday.AddDate(0, 0, -7*(keepWeeks-1))
		if monday.Before(oldestAllowedWeek) {
			log.Printf("Skipping %s: outside retention window (oldest allowed: %s).", timelapseName, oldestAllowedWeek.Format("2006-01-02"))
			return nil
		}
		cfg = models.TimelapseConfig{
			Name:         timelapseName,
			FramePattern: "hourly",
			WindowStart:  monday,
			WindowEnd:    monday.AddDate(0, 0, 7),
		}
		useGallery = true

	case strings.HasPrefix(timelapseName, "month_"):
		// Calendar month: fixed window, one noon image per day, sourced from gallery
		monthStr := strings.TrimPrefix(timelapseName, "month_")
		monthStart, err := time.Parse("2006-01", monthStr)
		if err != nil {
			return fmt.Errorf("invalid month format in timelapse name %s: %w", timelapseName, err)
		}
		// Skip if outside the active retention window.
		now := time.Now()
		keepMonths := config.AppConfig.MonthlyLapsesToKeep
		if keepMonths < 1 {
			keepMonths = 1
		}
		oldestAllowedMonth := time.Date(now.Year(), now.Month()-time.Month(keepMonths-1), 1, 0, 0, 0, 0, now.Location())
		if monthStart.Before(oldestAllowedMonth) {
			log.Printf("Skipping %s: outside retention window (oldest allowed: %s).", timelapseName, oldestAllowedMonth.Format("2006-01"))
			return nil
		}
		nextMonth := time.Date(monthStart.Year(), monthStart.Month()+1, 1, 0, 0, 0, 0, monthStart.Location())
		cfg = models.TimelapseConfig{
			Name:         timelapseName,
			FramePattern: "daily",
			WindowStart:  monthStart,
			WindowEnd:    nextMonth,
		}
		useGallery = true

	case strings.HasPrefix(timelapseName, "year_"):
		// Year-to-date: a few images per day via 3_hourly, sourced from gallery
		yearStr := strings.TrimPrefix(timelapseName, "year_")
		year, err := strconv.Atoi(yearStr)
		if err != nil {
			return fmt.Errorf("invalid year format in timelapse name %s: %w", timelapseName, err)
		}
		loc := time.Now().Location()
		cfg = models.TimelapseConfig{
			Name:         timelapseName,
			FramePattern: "3_hourly",
			WindowStart:  time.Date(year, time.January, 1, 0, 0, 0, 0, loc),
			WindowEnd:    time.Date(year+1, time.January, 1, 0, 0, 0, 0, loc),
		}
		useGallery = true

	default:
		return fmt.Errorf("no timelapse configuration found for name: %s", timelapseName)
	}

	var allFiles []string
	if useGallery {
		allFiles = util.GetGalleryFiles()
	} else {
		allFiles = util.GetSnapshotFiles()
	}
	if len(allFiles) == 0 {
		log.Println("No source files available to generate timelapse.")
		return nil
	}

	outputFileName := fmt.Sprintf("timelapse_%s.webm", cfg.Name)
	finalVideoPath := filepath.Join(config.AppConfig.DataDir, outputFileName)

	snapshotsForTimelapse := filterSnapshots(allFiles, cfg, targetDate)

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
		switch {
		case !util.FileExists(finalVideoPath):
			log.Printf("Full regeneration for %s: video file missing.", cfg.Name)
		case util.IsFileEmpty(finalVideoPath):
			log.Printf("Full regeneration for %s: video file is empty.", cfg.Name)
		default:
			log.Printf("Full regeneration for %s: tracker missing or references a snapshot not in current set (startIndex=0).", cfg.Name)
		}
		// Calendar-based timelapses (week/month/year/24-hour) have unique names per period;
		// no need to archive old copies — just replace in place.
		err := regenerateFullTimelapse(snapshotsForTimelapse, outputFileName, false)
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
				log.Printf("ERROR creating segment for %s: %v. Moving to quarantine.", newSnapshot, err)

				quarantineDir := filepath.Join(config.AppConfig.DataDir, "quarantine")
				if err := os.MkdirAll(quarantineDir, 0755); err != nil {
					log.Printf("ERROR creating quarantine directory %s: %v", quarantineDir, err)
				}

				quarantinedFile := filepath.Join(quarantineDir, filepath.Base(newSnapshot))
				log.Printf("Moving corrupted snapshot from %s to %s", newSnapshot, quarantinedFile)
				if err := os.Rename(newSnapshot, quarantinedFile); err != nil {
					log.Printf("ERROR moving corrupted snapshot %s to %s: %v", newSnapshot, quarantinedFile, err)
				}

				// continue to next snapshot
				continue
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

// parseFileTime parses a timestamp from a snapshot or gallery filename basename.
// Supports YYYY-MM-DD-HH-MM-SS (snapshot) and YYYY-MM-DD-HH (gallery) formats.
func parseFileTime(filename string) (time.Time, error) {
	base := strings.TrimSuffix(filepath.Base(filename), ".jpg")
	parts := strings.Split(base, "-")
	switch len(parts) {
	case 6:
		return time.Parse("2006-01-02-15-04-05", base)
	case 4:
		return time.Parse("2006-01-02-15", base)
	default:
		return time.Time{}, fmt.Errorf("unrecognized filename format: %s", filename)
	}
}

// pickClosestToHour returns the file from files whose hour is closest to targetHour.
func pickClosestToHour(files []string, targetHour int) string {
	best := files[0]
	bestDiff := 25
	for _, f := range files {
		t, err := parseFileTime(f)
		if err != nil {
			continue
		}
		diff := t.Hour() - targetHour
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			bestDiff = diff
			best = f
		}
	}
	return best
}

var filterSnapshots = func(allFiles []string, cfg models.TimelapseConfig, targetTime time.Time) []string {
	var filtered []string

	// Determine window: fixed (calendar) or rolling (legacy/24-hour)
	var windowStart, windowEnd time.Time
	if !cfg.WindowStart.IsZero() {
		windowStart = cfg.WindowStart
		windowEnd = cfg.WindowEnd
	} else if strings.HasPrefix(cfg.Name, "24_hour_") {
		windowStart = targetTime.Truncate(24 * time.Hour)
		windowEnd = windowStart.Add(24 * time.Hour)
	} else {
		windowStart = targetTime.Add(-cfg.Duration)
		windowEnd = targetTime
	}

	// Filter to the time window; handle both snapshot (6-part) and gallery (4-part) filenames
	var recentFiles []string
	for _, file := range allFiles {
		fileTime, err := parseFileTime(file)
		if err != nil {
			continue
		}
		if !fileTime.Before(windowStart) && fileTime.Before(windowEnd) {
			recentFiles = append(recentFiles, file)
		}
	}

	// Apply daylight-hours filter for all non-24-hour timelapses
	if !strings.HasPrefix(cfg.Name, "24_hour_") {
		startHour := config.AppConfig.DaylightStartHour
		endHour := config.AppConfig.DaylightEndHour
		if startHour > 0 || endHour < 24 {
			var daytimeFiles []string
			for _, file := range recentFiles {
				t, err := parseFileTime(file)
				if err != nil {
					continue
				}
				if t.Hour() >= startHour && t.Hour() < endHour {
					daytimeFiles = append(daytimeFiles, file)
				}
			}
			recentFiles = daytimeFiles
		}
	}

	switch cfg.FramePattern {
	case "all":
		filtered = recentFiles

	case "hourly":
		// One file per hour bucket (first encountered in sorted order)
		var lastHour string
		for _, file := range recentFiles {
			base := strings.TrimSuffix(filepath.Base(file), ".jpg")
			if len(base) >= 13 {
				hourKey := base[:13] // YYYY-MM-DD-HH
				if hourKey != lastHour {
					filtered = append(filtered, file)
					lastHour = hourKey
				}
			}
		}

	case "daily":
		// One file per day: the image whose hour is closest to DaylightTargetHour (default noon)
		dayGroups := make(map[string][]string)
		var dayOrder []string
		for _, file := range recentFiles {
			base := strings.TrimSuffix(filepath.Base(file), ".jpg")
			if len(base) >= 10 {
				dayKey := base[:10] // YYYY-MM-DD
				if _, ok := dayGroups[dayKey]; !ok {
					dayOrder = append(dayOrder, dayKey)
				}
				dayGroups[dayKey] = append(dayGroups[dayKey], file)
			}
		}
		for _, day := range dayOrder {
			if best := pickClosestToHour(dayGroups[day], config.AppConfig.DaylightTargetHour); best != "" {
				filtered = append(filtered, best)
			}
		}

	default:
		// Custom N_hourly pattern: one file per N-hour bucket per day
		if strings.HasSuffix(cfg.FramePattern, "_hourly") {
			intervalStr := strings.TrimSuffix(cfg.FramePattern, "_hourly")
			if interval, err := strconv.Atoi(intervalStr); err == nil && interval > 0 {
				var lastInterval = -1
				var lastDay string
				for _, file := range recentFiles {
					base := strings.TrimSuffix(filepath.Base(file), ".jpg")
					if len(base) >= 13 {
						dayKey := base[:10]
						hourStr := base[11:13]
						if hour, err := strconv.Atoi(hourStr); err == nil {
							bucket := hour / interval
							if dayKey != lastDay || bucket != lastInterval {
								filtered = append(filtered, file)
								lastDay = dayKey
								lastInterval = bucket
							}
						}
					}
				}
				break
			}
		}
		// Fallback: one noon-closest image per day
		dayGroups := make(map[string][]string)
		var dayOrder []string
		for _, file := range recentFiles {
			base := strings.TrimSuffix(filepath.Base(file), ".jpg")
			if len(base) >= 10 {
				dayKey := base[:10]
				if _, ok := dayGroups[dayKey]; !ok {
					dayOrder = append(dayOrder, dayKey)
				}
				dayGroups[dayKey] = append(dayGroups[dayKey], file)
			}
		}
		for _, day := range dayOrder {
			if best := pickClosestToHour(dayGroups[day], config.AppConfig.DaylightTargetHour); best != "" {
				filtered = append(filtered, best)
			}
		}
	}

	sort.Strings(filtered)
	return filtered
}
var regenerateFullTimelapse = func(snapshotFiles []string, outputFileName string, archive bool) error {
	if len(snapshotFiles) == 0 {
		log.Println("No snapshots to generate timelapse.")
		return nil
	}

	tempVideoPath := filepath.Join(config.AppConfig.DataDir, "temp_"+outputFileName)
	finalVideoPath := filepath.Join(config.AppConfig.DataDir, outputFileName)

	// Validate files with a fast stat check — no FFmpeg call per frame.
	var validSnapshots []string
	for _, snapshot := range snapshotFiles {
		info, err := os.Stat(snapshot)
		if err != nil || info.Size() == 0 {
			log.Printf("Skipping missing or empty snapshot: %s", snapshot)
			continue
		}
		validSnapshots = append(validSnapshots, snapshot)
	}
	if len(validSnapshots) == 0 {
		log.Printf("No valid snapshots found to generate timelapse %s.", outputFileName)
		return nil
	}

	// Write an ffconcat list so FFmpeg processes all frames in a single pass instead
	// of one FFmpeg invocation per frame (which was causing extreme CPU usage on large sets).
	concatListPath := filepath.Join(config.AppConfig.DataDir, "regen_concat_list.txt")
	listFile, err := os.Create(concatListPath)
	if err != nil {
		return fmt.Errorf("failed to create concat list: %w", err)
	}
	fmt.Fprintln(listFile, "ffconcat version 1.0")
	for _, snapshot := range validSnapshots {
		// duration 0.0333 ≈ 30 fps
		fmt.Fprintf(listFile, "file '%s'\nduration 0.0333\n", filepath.ToSlash(snapshot))
	}
	// ffconcat requires the last entry to be repeated without a duration to avoid a missing final frame.
	fmt.Fprintf(listFile, "file '%s'\n", filepath.ToSlash(validSnapshots[len(validSnapshots)-1]))
	listFile.Close()
	defer os.Remove(concatListPath)

	log.Printf("Starting batch timelapse generation for %s (%d frames)...", outputFileName, len(validSnapshots))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	videoFilter := "scale=out_color_matrix=bt709:out_range=tv,format=yuv420p"
	var cmd *exec.Cmd
	if PreferredVideoCodec == "libsvtav1" {
		cmd = exec.CommandContext(ctx, "ffmpeg",
			"-hide_banner", "-loglevel", "error",
			"-f", "concat", "-safe", "0", "-i", concatListPath,
			"-vf", videoFilter,
			"-c:v", PreferredVideoCodec, "-preset", "10",
			"-threads", fmt.Sprintf("%d", ffmpegThreads),
			"-crf", config.AppConfig.GetCRFValue(),
			"-an", "-f", "webm", "-y", tempVideoPath,
		)
	} else {
		cmd = exec.CommandContext(ctx, "ffmpeg",
			"-hide_banner", "-loglevel", "error",
			"-f", "concat", "-safe", "0", "-i", concatListPath,
			"-vf", videoFilter,
			"-c:v", PreferredVideoCodec,
			"-threads", fmt.Sprintf("%d", ffmpegThreads),
			"-crf", config.AppConfig.GetCRFValue(),
			"-an", "-f", "webm", "-y", tempVideoPath,
		)
	}
	cmd.Dir = config.AppConfig.DataDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		os.Remove(tempVideoPath)
		logFile, logErr := os.OpenFile(config.GetFFmpegLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if logErr == nil {
			logFile.WriteString(fmt.Sprintf("--- FFmpeg Batch Error for %s: %s ---\n%s\n", outputFileName, time.Now(), stderr.String()))
			logFile.Close()
		}
		return fmt.Errorf("ffmpeg batch encode failed for %s: %w", outputFileName, err)
	}

	// Replace the old video; archive only if requested (legacy rolling-window timelapses).
	if util.FileExists(finalVideoPath) {
		if archive {
			archiveFileName := fmt.Sprintf("%s_%s.webm", strings.TrimSuffix(outputFileName, ".webm"), time.Now().Format("20060102_150405"))
			archiveVideoPath := filepath.Join(config.AppConfig.DataDir, archiveFileName)
			log.Printf("Archiving existing video to: %s", archiveVideoPath)
			if err := os.Rename(finalVideoPath, archiveVideoPath); err != nil {
				log.Printf("Warning: failed to archive video %s: %v", finalVideoPath, err)
			}
		} else {
			os.Remove(finalVideoPath)
		}
	}

	if err := os.Rename(tempVideoPath, finalVideoPath); err != nil {
		return fmt.Errorf("failed to rename temp video to final video: %w", err)
	}

	time.Sleep(100 * time.Millisecond)
	log.Printf("Successfully completed batch timelapse generation for %s (%d frames).", outputFileName, len(validSnapshots))
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
	corruptFiles := 0

	for _, file := range allSnapshots {
		// Check for 0-byte files
		info, err := os.Stat(file)
		if err == nil && info.Size() == 0 {
			log.Printf("Found zero-byte snapshot, deleting: %s", file)
			if err := os.Remove(file); err != nil {
				log.Printf("Warning: failed to remove zero-byte snapshot %s: %v", file, err)
			} else {
				corruptFiles++
				continue // Don't process further
			}
		}

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

	log.Printf("Snapshot cleanup finished. Kept %d files, removed %d old files, and removed %d corrupt (zero-byte) files.", filesKept, filesToDelete, corruptFiles)
}

// cleanVideosByCount keeps only the newest `keep` files matching prefix+".webm" in DataDir.
// cleanVideosByCount removes the oldest videos for a given prefix, keeping only the N newest.
// Use only for timelapses without a natural date in the filename (e.g. yearly).
func cleanVideosByCount(prefix string, keep int) {
	files, err := os.ReadDir(config.AppConfig.DataDir)
	if err != nil {
		log.Printf("Error reading data directory during video cleanup: %v", err)
		return
	}
	var matches []string
	for _, f := range files {
		if strings.HasPrefix(f.Name(), prefix) && strings.HasSuffix(f.Name(), ".webm") {
			matches = append(matches, f.Name())
		}
	}
	sort.Strings(matches) // chronological because date is in the name
	if len(matches) <= keep {
		return
	}
	toDelete := matches[:len(matches)-keep]
	for _, name := range toDelete {
		if err := os.Remove(filepath.Join(config.AppConfig.DataDir, name)); err != nil {
			log.Printf("Error removing old video %s: %v", name, err)
		}
	}
	log.Printf("Cleaned up %d old video(s) with prefix %q.", len(toDelete), prefix)
}

// cleanWeeklyVideos deletes weekly timelapse files whose Monday date is older than the
// retention window. This is date-based rather than count-based so that in-window videos
// are never deleted, preventing the rebuild-then-delete loop that count-based cleanup caused.
func cleanWeeklyVideos() {
	now := time.Now()
	keepWeeks := config.AppConfig.WeeklyLapsesToKeep
	if keepWeeks < 1 {
		keepWeeks = 1
	}
	currentMonday := calendarWeekMonday(now)
	oldestAllowed := currentMonday.AddDate(0, 0, -7*(keepWeeks-1))

	files, err := os.ReadDir(config.AppConfig.DataDir)
	if err != nil {
		log.Printf("Error reading data directory during weekly video cleanup: %v", err)
		return
	}
	deleted := 0
	for _, f := range files {
		name := f.Name()
		if !strings.HasPrefix(name, "timelapse_week_") || !strings.HasSuffix(name, ".webm") {
			continue
		}
		dateStr := name[len("timelapse_week_") : len("timelapse_week_")+10]
		monday, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if monday.Before(oldestAllowed) {
			if err := os.Remove(filepath.Join(config.AppConfig.DataDir, name)); err != nil {
				log.Printf("Error removing old weekly video %s: %v", name, err)
			} else {
				deleted++
			}
		}
	}
	if deleted > 0 {
		log.Printf("Cleaned up %d old weekly timelapse video(s) older than %s.", deleted, oldestAllowed.Format("2006-01-02"))
	}
}

// cleanMonthlyVideos deletes monthly timelapse files whose month is older than the retention window.
func cleanMonthlyVideos() {
	now := time.Now()
	keepMonths := config.AppConfig.MonthlyLapsesToKeep
	if keepMonths < 1 {
		keepMonths = 1
	}
	oldestAllowedYear := now.Year()
	oldestAllowedMonth := now.Month() - time.Month(keepMonths-1)
	for oldestAllowedMonth <= 0 {
		oldestAllowedMonth += 12
		oldestAllowedYear--
	}
	oldestAllowed := time.Date(oldestAllowedYear, oldestAllowedMonth, 1, 0, 0, 0, 0, now.Location())

	files, err := os.ReadDir(config.AppConfig.DataDir)
	if err != nil {
		log.Printf("Error reading data directory during monthly video cleanup: %v", err)
		return
	}
	deleted := 0
	for _, f := range files {
		name := f.Name()
		if !strings.HasPrefix(name, "timelapse_month_") || !strings.HasSuffix(name, ".webm") {
			continue
		}
		dateStr := name[len("timelapse_month_") : len("timelapse_month_")+7]
		monthStart, err := time.Parse("2006-01", dateStr)
		if err != nil {
			continue
		}
		if monthStart.Before(oldestAllowed) {
			if err := os.Remove(filepath.Join(config.AppConfig.DataDir, name)); err != nil {
				log.Printf("Error removing old monthly video %s: %v", name, err)
			} else {
				deleted++
			}
		}
	}
	if deleted > 0 {
		log.Printf("Cleaned up %d old monthly timelapse video(s) older than %s.", deleted, oldestAllowed.Format("2006-01"))
	}
}

var CleanOldVideos = func() {
	log.Printf("Starting video cleanup...")

	// Daily 24-hour timelapses: remove by cutoff date
	cutoffDate := time.Now().AddDate(0, 0, -config.AppConfig.DaysOf24HourSnapshots).Truncate(24 * time.Hour)
	files, err := os.ReadDir(config.AppConfig.DataDir)
	if err != nil {
		log.Printf("Error reading data directory for daily video cleanup: %v", err)
		return
	}
	dailyRemoved := 0
	for _, f := range files {
		name := f.Name()
		if strings.HasPrefix(name, "timelapse_24_hour_") && strings.HasSuffix(name, ".webm") {
			dateStr := name[len("timelapse_24_hour_") : len("timelapse_24_hour_")+10]
			fileDate, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				log.Printf("Warning: could not parse date from daily timelapse video %s: %v", name, err)
				continue
			}
			if fileDate.Before(cutoffDate) {
				if err := os.Remove(filepath.Join(config.AppConfig.DataDir, name)); err != nil {
					log.Printf("Error removing old daily timelapse video %s: %v", name, err)
				} else {
					dailyRemoved++
				}
			}
		}
	}
	log.Printf("Removed %d old daily 24-hour timelapse video(s).", dailyRemoved)

	// Weekly timelapses: date-based — never deletes in-window videos
	cleanWeeklyVideos()

	// Monthly timelapses: date-based — never deletes in-window videos
	cleanMonthlyVideos()

	// Yearly timelapses: keep current + previous year
	cleanVideosByCount("timelapse_year_", 2)
}

var CleanupGallery = func() {
	log.Println("Starting gallery cleanup...")
	galleryPath := config.AppConfig.GalleryDir
	files, err := filepath.Glob(filepath.Join(galleryPath, "*.jpg"))
	if err != nil {
		log.Printf("Error finding gallery files for cleanup: %v", err)
		return
	}

	retentionCutoff := time.Now().Add(-time.Duration(config.AppConfig.GalleryRetentionDays) * 24 * time.Hour)
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
