package worker

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"time-machine/pkg/jobs"
	"time-machine/pkg/services/video"
)

func Start() {
	log.Println("Starting job worker...")
	// This is a simple, single-threaded worker.
	// Will need to expand this if we do more cameras

	for {
		job, err := jobs.GetPendingJob()
		if err != nil {
			log.Printf("Error getting pending job: %v", err)
			time.Sleep(10 * time.Second) // Wait before retrying
			continue
		}

		if job == nil {
			// No pending jobs, wait a bit
			time.Sleep(10 * time.Second)
			continue
		}

		log.Printf("Processing job %d: %s", job.ID, job.JobType)
		err = jobs.UpdateJobStatus(job.ID, "running", nil)
		if err != nil {
			log.Printf("Error updating job status to running: %v", err)
			continue
		}

		var jobErr error
		switch job.JobType {
		case "generate_timelapse":
			var payload struct {
				TimelapseName string `json:"timelapse_name"`
			}
			if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
				jobErr = err
			} else {
				jobErr = video.GenerateSingleTimelapse(payload.TimelapseName)
			}
		case "cleanup_snapshots":
			video.CleanupSnapshots()
		case "cleanup_videos":
			video.CleanOldVideos()
		default:
			jobErr = fmt.Errorf("unknown job type: %s", job.JobType)
			log.Println(jobErr)
		}

		if jobErr != nil {
			log.Printf("Error processing job %d: %v", job.ID, jobErr)
			err = jobs.UpdateJobStatus(job.ID, "failed", jobErr)
		} else {
			log.Printf("Job %d completed successfully", job.ID)
			err = jobs.UpdateJobStatus(job.ID, "completed", nil)
		}

		if err != nil {
			log.Printf("Error updating job status after completion/failure: %v", err)
		}

		// Clean up the job from the database
		// I think this will have weird issues
		err = jobs.DeleteJob(job.ID)
		if err != nil {
			log.Printf("Error deleting job %d: %v", job.ID, err)
		}
	}
}
