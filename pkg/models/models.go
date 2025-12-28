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
	FramePattern string // "all", "hourly", "daily"
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

// might change this to "year to date" rather than a full year for performance as its more clear

var TimelapseConfigsData = []TimelapseConfig{
	{Name: "1_week", Duration: 7 * 24 * time.Hour, FramePattern: "hourly"},
	{Name: "1_month", Duration: 30 * 24 * time.Hour, FramePattern: "daily"},
	{Name: "1_year", Duration: 365 * 24 * time.Hour, FramePattern: "daily"}, // Using daily for year as well for simplicity
}
