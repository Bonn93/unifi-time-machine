package jobs

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time-machine/pkg/models"
)

var db *sql.DB

func InitJobs(database *sql.DB) {
	db = database
}

// CreateJob creates a new job in the database.
func CreateJob(jobType string, payload interface{}) (int64, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal job payload: %w", err)
	}

	res, err := db.Exec("INSERT INTO jobs (job_type, payload) VALUES (?, ?)", jobType, string(payloadBytes))
	if err != nil {
		return 0, fmt.Errorf("failed to insert job: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	return id, nil
}

// GetPendingJob retrieves the oldest pending job from the database.
func GetPendingJob() (*models.Job, error) {
	row := db.QueryRow("SELECT id, job_type, payload, status, error, created_at, updated_at FROM jobs WHERE status = 'pending' ORDER BY created_at ASC LIMIT 1")

	var job models.Job
	err := row.Scan(&job.ID, &job.JobType, &job.Payload, &job.Status, &job.Error, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No pending jobs
		}
		return nil, fmt.Errorf("failed to get pending job: %w", err)
	}

	return &job, nil
}

// DeleteJob removes a job from the database.
func DeleteJob(id int64) error {
	_, err := db.Exec("DELETE FROM jobs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete job %d: %w", id, err)
	}
	return nil
}

// UpdateJobStatus updates the status and error of a job.
func UpdateJobStatus(id int64, status string, jobErr error) error {
	var errStr sql.NullString
	if jobErr != nil {
		errStr.String = jobErr.Error()
		errStr.Valid = true
	}
	_, err := db.Exec("UPDATE jobs SET status = ?, error = ? WHERE id = ?", status, errStr, id)
	if err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}
	return nil
}
