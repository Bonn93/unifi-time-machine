package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"github.com/gin-gonic/gin"
	"time-machine/pkg/cachedstats"
	"time-machine/pkg/config"
	"time-machine/pkg/database"
	"time-machine/pkg/models"
	"time-machine/pkg/services/video"
	"time-machine/pkg/stats"
	"time-machine/pkg/util"
)

// HandleForceGenerate enqueues all timelapse jobs to be processed by the worker.
func HandleForceGenerate(c *gin.Context) {
	go video.EnqueueTimelapseJobs() // Run in a goroutine to not block the UI
	c.Redirect(http.StatusFound, "/")
}

// --- HANDLERS ---

// HandleLogout clears the session cookie and redirects to the login page.
func HandleLogout(c *gin.Context) {
	c.SetCookie("session_token", "", -1, "/", "", false, true) // Clear the session cookie
	c.Redirect(http.StatusFound, "/login")
}

// HandleLoginGet renders the login page.
func HandleLoginGet(c *gin.Context) {
	c.HTML(http.StatusOK, "login.html", gin.H{})
}

// HandleLoginPost processes the login form submission.
func HandleLoginPost(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")

	user, authenticated := database.CheckUserCredentials(username, password)

	if !authenticated {
		c.HTML(http.StatusUnauthorized, "login.html", gin.H{"Error": "Invalid username or password"})
		return
	}

	// this needs to be more robust...
	sessionToken := username + ":" + fmt.Sprintf("%d", time.Now().Unix()) // Simple token
	c.SetCookie("session_token", sessionToken, 3600, "/", "", false, true)

	// Save the user in the context for subsequent middleware (e.g., admin check)
	c.Set("user", user)

	c.Redirect(http.StatusFound, "/")
}

func HandleDashboard(c *gin.Context) {
	models.VideoStatusData.RLock()
	defer models.VideoStatusData.RUnlock()

	// --- New Timelapse Data Structure ---
	// map[TIMELAPSE_TYPE] -> list of videos
	availableTimelapses := make(map[string][]gin.H)

	// --- Daily 24-Hour Timelapses ---
	var dailyVideos []gin.H
	for i := 0; i < config.AppConfig.DaysOf24HourSnapshots; i++ {
		targetDate := time.Now().AddDate(0, 0, -i)
		dateStr := targetDate.Format("2006-01-02")
		fileName := fmt.Sprintf("timelapse_24_hour_%s.webm", dateStr)
		filePath := filepath.Join(config.AppConfig.DataDir, fileName)

		if util.FileExists(filePath) {
			dailyVideos = append(dailyVideos, gin.H{
				"Date": dateStr,
				"Path": "/data/" + fileName,
			})
		}
	}
	if len(dailyVideos) > 0 {
		sort.Slice(dailyVideos, func(i, j int) bool {
			return dailyVideos[i]["Date"].(string) > dailyVideos[j]["Date"].(string)
		})
		availableTimelapses["Daily"] = dailyVideos
	}

	// --- Other Timelapse Info (Weekly, Monthly, Yearly) ---
	allVideoFiles, err := filepath.Glob(filepath.Join(config.AppConfig.DataDir, "timelapse_*.webm"))
	if err != nil {
		// Log the error but don't crash the page
		fmt.Printf("Error globbing video files: %v\n", err)
	}

	for _, cfg := range models.TimelapseConfigsData {
		var otherVideos []gin.H
		baseName := fmt.Sprintf("timelapse_%s.webm", cfg.Name)
		archivePrefix := fmt.Sprintf("timelapse_%s_", cfg.Name)

		// Check for the main file
		if util.FileExists(filepath.Join(config.AppConfig.DataDir, baseName)) {
			otherVideos = append(otherVideos, gin.H{
				"Date": "Latest",
				"Path": "/data/" + baseName,
			})
		}

		// Find archives
		for _, file := range allVideoFiles {
			fileName := filepath.Base(file)
			if strings.HasPrefix(fileName, archivePrefix) && strings.HasSuffix(fileName, ".webm") {
				// Extract date from "timelapse_1_week_20231027_150405.webm"
				datePart := strings.TrimSuffix(strings.TrimPrefix(fileName, archivePrefix), ".webm")
				parsedTime, err := time.Parse("20060102_150405", datePart)
				var displayDate string
				if err != nil {
					displayDate = datePart // Fallback to raw string
				} else {
					displayDate = parsedTime.Format("2006-01-02 15:04:05")
				}
				otherVideos = append(otherVideos, gin.H{
					"Date": displayDate,
					"Path": "/data/" + fileName,
				})
			}
		}

		if len(otherVideos) > 0 {
			sort.Slice(otherVideos, func(i, j int) bool {
				// Simple sort: "Latest" always comes first, then by date string descending
				if otherVideos[i]["Date"] == "Latest" {
					return true
				}
				if otherVideos[j]["Date"] == "Latest" {
					return false
				}
				return otherVideos[i]["Date"].(string) > otherVideos[j]["Date"].(string)
			})
			var typeName string
			switch cfg.Name {
			case "1_week":
				typeName = "Weekly"
			case "1_month":
				typeName = "Monthly"
			case "1_year":
				typeName = "Yearly"
			default:
				typeName = strings.Title(strings.ReplaceAll(cfg.Name, "_", " "))
			}
			availableTimelapses[typeName] = otherVideos
		}
	}

	videoExists := len(availableTimelapses) > 0

	currentVideoStatus := gin.H{
		"IsRunning":           models.VideoStatusData.IsRunning,
		"LastRun":             "N/A",
		"Error":               models.VideoStatusData.Error,
		"CurrentlyGenerating": models.VideoStatusData.CurrentlyGenerating,
		"CurrentFile":         models.VideoStatusData.CurrentFile,
	}
	if models.VideoStatusData.LastRun != nil {
		currentVideoStatus["LastRun"] = models.VideoStatusData.LastRun.Format("2006-01-02 15:04:05")
	}

	user, _ := c.Get("user")
	cachedData := cachedstats.Cache.GetData()
	timelapseOrder := []string{"Daily", "Weekly", "Monthly", "Yearly"}

	data := gin.H{
		"Now":                  time.Now().Format("2006-01-02 15:04:05"),
		"VideoExists":          videoExists,
		"AvailableTimelapses":  availableTimelapses,
		"TimelapseOrder":       timelapseOrder,
		"VideoStatus":          currentVideoStatus,
		"ImageStats":           cachedData,
		"SystemInfo":           cachedData["system_info"],
		"CameraStatus":         cachedData["camera_status"],
		"DefaultGalleryDate":   cachedData["default_date"],
		"DefaultGalleryImages": cachedData["daily_gallery"],
		"AvailableDates":       cachedData["available_dates"],
		"User":                 user.(*models.User),
	}

	c.HTML(http.StatusOK, "index.html", data)
}

func HandleLog(c *gin.Context) {
	logFiles, err := filepath.Glob(filepath.Join(config.AppConfig.DataDir, "ffmpeg_log_*.txt"))
	if err != nil {
		c.String(http.StatusInternalServerError, "Error finding log files: %v", err)
		return
	}

	if len(logFiles) == 0 {
		c.HTML(http.StatusOK, "log.html", gin.H{
			"Message": "No log files found.",
		})
		return
	}

	// Sort files by name to get the most recent one last
	sort.Sort(sort.Reverse(sort.StringSlice(logFiles)))

	var logDates []string
	for _, file := range logFiles {
		// Extract YYYY-MM-DD from the filename
		name := filepath.Base(file)
		dateStr := strings.TrimSuffix(strings.TrimPrefix(name, "ffmpeg_log_"), ".txt")
		logDates = append(logDates, dateStr)
	}

	// Determine which log to display
	selectedDate := c.Query("date")
	var logToShowPath string
	if selectedDate != "" {
		logToShowPath = filepath.Join(config.AppConfig.DataDir, fmt.Sprintf("ffmpeg_log_%s.txt", selectedDate))
	} else {
		logToShowPath = logFiles[0] // Default to the latest
		selectedDate = logDates[0]
	}

	content, err := os.ReadFile(logToShowPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.HTML(http.StatusNotFound, "log.html", gin.H{
				"Message":      fmt.Sprintf("Log file for date %s not found.", selectedDate),
				"AvailableDates": logDates,
			})
			return
		}
		c.String(http.StatusInternalServerError, "Error reading log file: %v", err)
		return
	}

	user, _ := c.Get("user")
	c.HTML(http.StatusOK, "log.html", gin.H{
		"User":           user.(*models.User),
		"LogContent":     string(content),
		"AvailableDates": logDates,
		"SelectedDate":   selectedDate,
	})
}

func HandleSystemStatsJSON(c *gin.Context) {
	c.JSON(http.StatusOK, stats.GetSystemInfo())
}

func HandleImageStats(c *gin.Context) {
	c.JSON(http.StatusOK, cachedstats.Cache.GetData())
}

func HandleDailyGallery(c *gin.Context) {
	dateStr := c.Query("date")
	if dateStr == "" {
		dateStr = time.Now().Format("2006-01-02")
	}

	// For simplicity, we'll just refetch if a different date is requested.
	// Caching daily galleries for all possible dates is more complex.
	// cache needs work...
	if dateStr == time.Now().Format("2006-01-02") {
		c.JSON(http.StatusOK, gin.H{
			"date":   dateStr,
			"images": cachedstats.Cache.GetData()["daily_gallery"],
		})
		return
	}

	images := stats.GetDailyGallery(dateStr)
	c.JSON(http.StatusOK, gin.H{
		"date":   dateStr,
		"images": images,
	})
}

func HandleAdminPage(c *gin.Context) {
	user, _ := c.Get("user")
	c.HTML(http.StatusOK, "admin.html", gin.H{
		"User": user.(*models.User),
	})
}

func HandleCreateUser(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")
	isAdmin := c.PostForm("isAdmin") == "on" // Checkbox value

	user, _ := c.Get("user")
	templateData := gin.H{"User": user.(*models.User)}

	if username == "" || password == "" {
		templateData["message"] = "Username and password cannot be empty."
		templateData["messageType"] = "error"
		c.HTML(http.StatusBadRequest, "admin.html", templateData)
		return
	}

	err := database.CreateUser(username, password, isAdmin)
	if err != nil {
		templateData["message"] = fmt.Sprintf("Error creating user: %v", err)
		templateData["messageType"] = "error"
		c.HTML(http.StatusInternalServerError, "admin.html", templateData)
		return
	}

	templateData["message"] = fmt.Sprintf("Successfully created user: %s", username)
	templateData["messageType"] = "success"
	c.HTML(http.StatusOK, "admin.html", templateData)
}

// HandleUnauthorized renders a user-friendly unauthorized error page.
func HandleUnauthorized(c *gin.Context) {
	c.HTML(http.StatusForbidden, "error.html", gin.H{"Message": "Unauthorized Action"})
}
