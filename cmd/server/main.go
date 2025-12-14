package main

import (
	"log"
	"os"

	"time-machine/pkg/config"
	"time-machine/pkg/database"
	"time-machine/pkg/jobs"
	"time-machine/pkg/server"
	"time-machine/pkg/services/snapshot"
	"time-machine/pkg/services/video"
	"time-machine/pkg/worker"
	"time-machine/pkg/cachedstats"
)

func main() {
	config.LoadConfig()

	// Ensure data directories exist
	if err := os.MkdirAll(config.AppConfig.SnapshotsDir, 0755); err != nil {
		log.Fatalf("Failed to create snapshots directory: %v", err)
	}
	if err := os.MkdirAll(config.AppConfig.GalleryDir, 0755); err != nil {
		log.Fatalf("Failed to create gallery directory: %v", err)
	}

	// Initialize Database
	database.InitDB()
	jobs.InitJobs(database.GetDB())

	// Create initial admin user if it doesn't exist
	// Can probably make a nicer GUI and set this up and remove a cleartext password in env var 
	adminUserExists, err := database.UserExists("admin")
	if err != nil {
		log.Fatalf("Failed to check if admin user exists: %v", err)
	}
	if !adminUserExists {
		if config.AppConfig.AdminPassword == "" {
			log.Fatal("FATAL: ADMIN_PASSWORD environment variable must be set to create the initial admin user.")
		}
		if err := database.CreateUser("admin", config.AppConfig.AdminPassword, true); err != nil {
			log.Fatalf("Failed to create initial admin user: %v", err)
		}
	}

	// Start background workers and schedulers
	cachedstats.Cache.RunUpdater()
	go worker.Start()
	go snapshot.StartSnapshotScheduler()
	go video.StartVideoGeneratorScheduler()
	log.Printf("✅ Snapshot Scheduler started with interval: %d seconds", config.AppConfig.SnapshotIntervalSec)
	log.Printf("✅ Video Generation Scheduler started with interval: %d seconds", config.AppConfig.VideoCronIntervalSec)

	server.StartServer()
}
