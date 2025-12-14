package config

import (
	"encoding/base64" // Uncommented
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds the application configuration.
type Config struct {
	UFPHost              string
	UFPAPIKey            string
	TargetCameraID       string
	DataDir              string
	SnapshotsDir         string
	GalleryDir           string
	SnapshotIntervalSec  int
	VideoCronIntervalSec int
	VideoArchivesToKeep  int
	FFmpegLogPath        string
	AppKey               string // Uncommented
	AdminPassword        string
	VideoQuality         string
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
		UFPAPIKey:            getEnv("UFP_API_KEY", ""),
		TargetCameraID:       getEnv("TARGET_CAMERA_ID", ""),
		DataDir:              getEnv("DATA_DIR", "data"),
		SnapshotIntervalSec:  getEnvAsInt("TIMELAPSE_INTERVAL", 3600),
		VideoCronIntervalSec: getEnvAsInt("VIDEO_CRON_INTERVAL", 300),
		VideoArchivesToKeep:  getEnvAsInt("VIDEO_ARCHIVES_TO_KEEP", 3),
		AppKey:               getEnv("APP_KEY", ""),
		AdminPassword:        getEnv("ADMIN_PASSWORD", ""),
		VideoQuality:         getEnv("VIDEO_QUALITY", "medium"),
		SnapshotsDir:         getEnv("SNAPSHOTS_DIR", "snapshots"),
		GalleryDir:           getEnv("GALLERY_DIR", "gallery"),
		FFmpegLogPath:        getEnv("FFMPEG_LOG_PATH", "ffmpeg_log.txt"),
	}

	// Validate APP_KEY
	if AppConfig.AppKey == "" {
		log.Fatal("FATAL: APP_KEY environment variable must be set.")
	}
	_, err := base64.StdEncoding.DecodeString(AppConfig.AppKey) 
	if err != nil {
		log.Fatalf("FATAL: APP_KEY is not a valid base64 encoded string: %v", err) 
	}

	AppConfig.SnapshotsDir = filepath.Join(AppConfig.DataDir, AppConfig.SnapshotsDir)
	AppConfig.GalleryDir = filepath.Join(AppConfig.DataDir, AppConfig.GalleryDir)
	AppConfig.FFmpegLogPath = filepath.Join(AppConfig.DataDir, AppConfig.FFmpegLogPath)

	// Ensure UFP_HOST has a protocol scheme
	AppConfig.UFPHost = getEnv("UFP_HOST", "")
	if AppConfig.UFPHost != "" && !strings.Contains(AppConfig.UFPHost, "://") {
		AppConfig.UFPHost = "https://" + AppConfig.UFPHost
	}

	log.Printf("UFP Host set to: %s", AppConfig.UFPHost)
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
