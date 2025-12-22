package worker

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time-machine/pkg/jobs"
	"time-machine/pkg/models"
	"time-machine/pkg/services/video"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	assert.NoError(t, err)

	createJobTableSQL := `CREATE TABLE IF NOT EXISTS jobs (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"job_type" TEXT NOT NULL,
		"payload" TEXT,
		"status" TEXT NOT NULL DEFAULT 'pending',
		"error" TEXT,
		"created_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
		"updated_at" DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	_, err = db.Exec(createJobTableSQL)
	assert.NoError(t, err)

	jobs.InitJobs(db)
	return db
}

func TestProcessJob(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Mock video service functions
	video.GenerateSingleTimelapse = func(timelapseName string) error { return nil }
	video.CleanupSnapshots = func() {}
	video.CleanOldVideos = func() {}
	video.CleanupLogFiles = func() {}

	// Test "generate_timelapse" job
	payload, _ := json.Marshal(map[string]string{"timelapse_name": "24_hour"})
	job := &models.Job{ID: 1, JobType: "generate_timelapse", Payload: string(payload)}
	processJob(job)

	var status string
	err := db.QueryRow("SELECT status FROM jobs WHERE id = ?", 1).Scan(&status)
	if err != nil && err != sql.ErrNoRows { // Job is deleted after processing
		t.Fatalf("Failed to query job status: %v", err)
	}

	// Test "cleanup_snapshots" job
	job = &models.Job{ID: 2, JobType: "cleanup_snapshots"}
	jobs.CreateJob(job.JobType, nil)
	processJob(job)
	err = db.QueryRow("SELECT status FROM jobs WHERE id = ?", 2).Scan(&status)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("Failed to query job status: %v", err)
	}

	// Test "cleanup_videos" job
	job = &models.Job{ID: 3, JobType: "cleanup_videos"}
	jobs.CreateJob(job.JobType, nil)
	processJob(job)
	err = db.QueryRow("SELECT status FROM jobs WHERE id = ?", 3).Scan(&status)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("Failed to query job status: %v", err)
	}

	// Test "cleanup_logs" job
	job = &models.Job{ID: 4, JobType: "cleanup_logs"}
	jobs.CreateJob(job.JobType, nil)
	processJob(job)
	err = db.QueryRow("SELECT status FROM jobs WHERE id = ?", 4).Scan(&status)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("Failed to query job status: %v", err)
	}

	// Test unknown job type
	job = &models.Job{ID: 5, JobType: "unknown_job"}
	jobs.CreateJob(job.JobType, nil)
	processJob(job)
	err = db.QueryRow("SELECT status FROM jobs WHERE id = ?", 5).Scan(&status)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("Failed to query job status: %v", err)
	}

	// Test invalid payload
	job = &models.Job{ID: 6, JobType: "generate_timelapse", Payload: "invalid payload"}
	jobs.CreateJob(job.JobType, "invalid payload")
	processJob(job)
	err = db.QueryRow("SELECT status FROM jobs WHERE id = ?", 6).Scan(&status)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("Failed to query job status: %v", err)
	}
}
