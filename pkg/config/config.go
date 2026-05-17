package config

import (
	"encoding/base64"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds the application configuration.
type Config struct {
	UFPHost               string
	UFPAPIKey             string
	TargetCameraID        string
	DataDir               string
	SnapshotsDir          string
	GalleryDir            string
	SnapshotIntervalSec   int
	VideoCronIntervalSec  int
	FFmpegLogPath         string
	AppKey                string
	AdminPassword         string
	VideoQuality          string
	HQSnapParams          string
	DaysOf24HourSnapshots int
	SnapshotRetentionDays int
	GalleryRetentionDays  int
	ShareLinkExpiryHours  int
	DateFormat            string
	TimeFormat            string
	DaylightStartHour     int
	DaylightEndHour       int
	DaylightTargetHour    int // target hour for "daily" frame selection (closest-to-noon picker)
	WeeklyLapsesToKeep    int // number of calendar-week timelapses to retain
	MonthlyLapsesToKeep   int // number of calendar-month timelapses to retain
	MaxBitrate            string
}

// AppConfig is the global application configuration.
var AppConfig Config

// GetCRFValue returns the CRF value based on the configured video quality.

func (c *Config) GetCRFValue() string {

	switch strings.ToLower(c.VideoQuality) {

	case "low":

		return "35"

	case "medium":

		return "28"

	case "high":

		return "20"

	case "ultra":

		return "15"

	default:

		return "28" // Default to medium

	}

}

// LoadConfig loads the configuration from environment variables.

func LoadConfig() {

	AppConfig = Config{

		UFPAPIKey: getEnv("UFP_API_KEY", ""),

		TargetCameraID: getEnv("TARGET_CAMERA_ID", ""),

		DataDir: getEnv("DATA_DIR", "data"),

		SnapshotIntervalSec: getEnvAsInt("TIMELAPSE_INTERVAL", 3600),

		VideoCronIntervalSec: getEnvAsInt("VIDEO_CRON_INTERVAL", 300),

		AppKey: getEnv("APP_KEY", ""),

		AdminPassword: getEnv("ADMIN_PASSWORD", ""),

		VideoQuality: getEnv("VIDEO_QUALITY", "medium"),

		SnapshotsDir: getEnv("SNAPSHOTS_DIR", "snapshots"),

		GalleryDir: getEnv("GALLERY_DIR", "gallery"),

		HQSnapParams: getEnv("HQSNAP", "auto"),

		DaysOf24HourSnapshots: getEnvAsInt("DAYS_OF_24_HOUR_SNAPSHOTS", 30),

		SnapshotRetentionDays:  getEnvAsInt("SNAPSHOT_RETENTION_DAYS", 30),
		GalleryRetentionDays:   getEnvAsInt("GALLERY_RETENTION_DAYS", 365),
		ShareLinkExpiryHours:   getEnvAsInt("SHARE_LINK_EXPIRY_HOURS", 4),
		DateFormat:             getEnv("DATE_FORMAT", "DD/MM/YYYY"),
		TimeFormat:             getEnv("TIME_FORMAT", "12h"),
		DaylightStartHour:   getEnvAsInt("DAYLIGHT_START_HOUR", 7),
		DaylightEndHour:     getEnvAsInt("DAYLIGHT_END_HOUR", 19),
		DaylightTargetHour:  getEnvAsInt("DAYLIGHT_TARGET_HOUR", 12),
		WeeklyLapsesToKeep:  getEnvAsInt("WEEKLY_LAPSES_TO_KEEP", 4),
		MonthlyLapsesToKeep: getEnvAsInt("MONTHLY_LAPSES_TO_KEEP", 3),
		MaxBitrate:          getEnv("VIDEO_MAX_BITRATE", "2M"),
	}

	// Validate APP_KEY

	if AppConfig.AppKey == "" {

		log.Fatal("FATAL: APP_KEY environment variable must be set.")

	}

	_, err := base64.StdEncoding.DecodeString(AppConfig.AppKey)

	if err != nil {

		log.Fatalf("FATAL: APP_KEY is not a valid base64 encoded string: %v", err)

	}

	// Convert DataDir to absolute path

	AppConfig.DataDir, err = filepath.Abs(AppConfig.DataDir)

	if err != nil {

		log.Fatalf("FATAL: Could not get absolute path for DataDir: %v", err)

	}

	AppConfig.SnapshotsDir = filepath.Join(AppConfig.DataDir, AppConfig.SnapshotsDir)

	AppConfig.GalleryDir = filepath.Join(AppConfig.DataDir, AppConfig.GalleryDir)

	// Ensure UFP_HOST has a protocol scheme

	AppConfig.UFPHost = getEnv("UFP_HOST", "")

	if AppConfig.UFPHost != "" && !strings.Contains(AppConfig.UFPHost, "://") {

		AppConfig.UFPHost = "https://" + AppConfig.UFPHost

	}

	log.Printf("UFP Host set to: %s", AppConfig.UFPHost)

	log.Println("--- Application Configuration ---")
	log.Printf("Target Camera ID: %s", AppConfig.TargetCameraID)
	log.Printf("Data Directory: %s", AppConfig.DataDir)
	log.Printf("Snapshot Interval: %d seconds", AppConfig.SnapshotIntervalSec)
	log.Printf("Video Cron Interval: %d seconds", AppConfig.VideoCronIntervalSec)
	log.Printf("Video Quality: %s (max bitrate: %s)", AppConfig.VideoQuality, AppConfig.MaxBitrate)
	log.Printf("High Quality Snapshots: %s", AppConfig.HQSnapParams)
	log.Printf("Days of 24-Hour Timelapses: %d", AppConfig.DaysOf24HourSnapshots)
	log.Printf("Snapshot Retention Days: %d", AppConfig.SnapshotRetentionDays)
	log.Printf("Gallery Retention Days: %d", AppConfig.GalleryRetentionDays)
	if AppConfig.ShareLinkExpiryHours > 0 {
		log.Printf("Share Link Expiry: %d hours", AppConfig.ShareLinkExpiryHours)
	} else {
		log.Printf("Share Link Expiry: Unlimited")
	}
	log.Printf("Date Format: %s", AppConfig.DateFormat)
	log.Printf("Time Format: %s", AppConfig.TimeFormat)
	log.Printf("Daylight Filter: %02d:00 to %02d:00 (target hour: %02d:00)", AppConfig.DaylightStartHour, AppConfig.DaylightEndHour, AppConfig.DaylightTargetHour)
	log.Printf("Weekly Lapses to Keep: %d", AppConfig.WeeklyLapsesToKeep)
	log.Printf("Monthly Lapses to Keep: %d", AppConfig.MonthlyLapsesToKeep)
	log.Println("---------------------------------")
}

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
