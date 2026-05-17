package models

import (
	"database/sql"
	"sync"
	"time"
)

// VideoStatus represents the status of the video generation process.
type VideoStatus struct {
	sync.RWMutex
	IsRunning           bool
	LastRun             *time.Time
	Error               string
	CurrentlyGenerating string
	CurrentFile         string
}

// TimelapseConfig represents the configuration for a timelapse.
type TimelapseConfig struct {
	Name         string
	Duration     time.Duration
	FramePattern string    // "all", "hourly", "daily", "N_hourly"
	WindowStart  time.Time // fixed window start; zero means use Duration relative to targetTime
	WindowEnd    time.Time // fixed window end
}

// Job represents a job in the database job queue.
type Job struct {
	ID        int64
	JobType   string
	Payload   string
	Status    string
	Error     sql.NullString
	CreatedAt time.Time
	UpdatedAt time.Time
}

// User represents a user account in the database.
type User struct {
	ID       int64
	Username string
	IsAdmin  bool
}

var VideoStatusData = &VideoStatus{
	IsRunning:           false,
	Error:               "",
	CurrentlyGenerating: "",
	CurrentFile:         "",
}

