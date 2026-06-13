package config

import (
	"encoding/base64"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Config holds bootstrap configuration that must be available before the database
// is initialised (credentials, paths).  All operational tuning lives in the
// settings DB table managed by pkg/services/settings.
type Config struct {
	UFPHost        string
	UFPAPIKey      string
	TargetCameraID string
	DataDir        string
	SnapshotsDir   string
	GalleryDir     string
	AppKey         string
	AdminPassword  string
}

// AppConfig is the global application configuration.
var AppConfig Config

// LoadConfig loads bootstrap configuration from environment variables.
// Operational settings (intervals, quality, retention, etc.) are managed by
// pkg/services/settings and live in the database.
func LoadConfig() {
	AppConfig = Config{
		UFPAPIKey:      getEnv("UFP_API_KEY", ""),
		TargetCameraID: getEnv("TARGET_CAMERA_ID", ""),
		DataDir:        getEnv("DATA_DIR", "data"),
		AppKey:         getEnv("APP_KEY", ""),
		AdminPassword:  getEnv("ADMIN_PASSWORD", ""),
		SnapshotsDir:   getEnv("SNAPSHOTS_DIR", "snapshots"),
		GalleryDir:     getEnv("GALLERY_DIR", "gallery"),
	}

	if AppConfig.AppKey == "" {
		log.Fatal("FATAL: APP_KEY environment variable must be set.")
	}
	if _, err := base64.StdEncoding.DecodeString(AppConfig.AppKey); err != nil {
		log.Fatalf("FATAL: APP_KEY is not a valid base64 encoded string: %v", err)
	}

	var err error
	AppConfig.DataDir, err = filepath.Abs(AppConfig.DataDir)
	if err != nil {
		log.Fatalf("FATAL: Could not get absolute path for DataDir: %v", err)
	}
	AppConfig.SnapshotsDir = filepath.Join(AppConfig.DataDir, AppConfig.SnapshotsDir)
	AppConfig.GalleryDir = filepath.Join(AppConfig.DataDir, AppConfig.GalleryDir)

	AppConfig.UFPHost = getEnv("UFP_HOST", "")
	if AppConfig.UFPHost != "" && !strings.Contains(AppConfig.UFPHost, "://") {
		AppConfig.UFPHost = "https://" + AppConfig.UFPHost
	}

	log.Println("--- Bootstrap Configuration ---")
	log.Printf("UFP Host: %s", AppConfig.UFPHost)
	log.Printf("Target Camera ID: %s", AppConfig.TargetCameraID)
	log.Printf("Data Directory: %s", AppConfig.DataDir)
	log.Println("(Operational settings loaded from DB via settings service)")
	log.Println("--------------------------------")
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

