package jobs

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"time-machine/pkg/models"
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

	InitJobs(db)
	return db
}

func TestCreateJob(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	payload := map[string]string{"file": "test.mp4"}
	id, err := CreateJob("video_processing", payload)
	assert.NoError(t, err)
	assert.Greater(t, id, int64(0))

	var job models.Job
	var payloadStr string
	err = db.QueryRow("SELECT id, job_type, payload, status FROM jobs WHERE id = ?", id).Scan(&job.ID, &job.JobType, &payloadStr, &job.Status)
	assert.NoError(t, err)
	assert.Equal(t, id, job.ID)
	assert.Equal(t, "video_processing", job.JobType)
	assert.Equal(t, "pending", job.Status)

	var returnedPayload map[string]string
	err = json.Unmarshal([]byte(payloadStr), &returnedPayload)
	assert.NoError(t, err)
	assert.Equal(t, payload, returnedPayload)
}

func TestGetPendingJob(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Test when no pending jobs
	job, err := GetPendingJob()
	assert.NoError(t, err)
	assert.Nil(t, job)

	// Create a job
	payload := map[string]string{"file": "test.mp4"}
	id, err := CreateJob("video_processing", payload)
	assert.NoError(t, err)

	// Test getting the pending job
	job, err = GetPendingJob()
	assert.NoError(t, err)
	assert.NotNil(t, job)
	assert.Equal(t, id, job.ID)
	assert.Equal(t, "video_processing", job.JobType)
}

func TestDeleteJob(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	id, err := CreateJob("test_job", nil)
	assert.NoError(t, err)

	err = DeleteJob(id)
	assert.NoError(t, err)

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM jobs WHERE id = ?", id).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestUpdateJobStatus(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	id, err := CreateJob("test_job", nil)
	assert.NoError(t, err)

	// Test updating to "processing"
	err = UpdateJobStatus(id, "processing", nil)
	assert.NoError(t, err)

	var status string
	var errorStr sql.NullString
	err = db.QueryRow("SELECT status, error FROM jobs WHERE id = ?", id).Scan(&status, &errorStr)
	assert.NoError(t, err)
	assert.Equal(t, "processing", status)
	assert.False(t, errorStr.Valid)

	// Test updating to "failed" with an error
	jobErr := errors.New("something went wrong")
	err = UpdateJobStatus(id, "failed", jobErr)
	assert.NoError(t, err)

	err = db.QueryRow("SELECT status, error FROM jobs WHERE id = ?", id).Scan(&status, &errorStr)
	assert.NoError(t, err)
	assert.Equal(t, "failed", status)
	assert.True(t, errorStr.Valid)
	assert.Equal(t, "something went wrong", errorStr.String)
}
