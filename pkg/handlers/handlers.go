package handlers

import (
	"fmt"
	"io"
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

// HandleLoginGet renders the login page.
func HandleLoginGet(c *gin.Context) {
	c.HTML(http.StatusOK, "login.html", gin.H{})
}

func HandleDashboard(c *gin.Context) {
	models.VideoStatusData.RLock()
	defer models.VideoStatusData.RUnlock()

	// --- New Timelapse Data Structure ---
	// map[TIMELAPSE_TYPE] -> list of videos
	availableTimelapses := make(map[string][]gin.H)
	timelapseOrder := []string{"Daily", "Weekly", "Monthly", "Yearly"}

	// Initialize all timelapse types to ensure they appear in the UI
	for _, typeName := range timelapseOrder {
		availableTimelapses[typeName] = []gin.H{}
	}

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
				var isoDate string
				if err != nil {
					isoDate = datePart // Fallback to raw string
				} else {
					isoDate = parsedTime.Format(time.RFC3339)
				}
				otherVideos = append(otherVideos, gin.H{
					"Date": isoDate,
					"Path": "/data/" + fileName,
				})
			}
		}

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
			availableTimelapses[typeName] = otherVideos
		}
	}

	currentVideoStatus := gin.H{
		"IsRunning":           models.VideoStatusData.IsRunning,
		"LastRun":             "N/A",
		"Error":               models.VideoStatusData.Error,
		"CurrentlyGenerating": models.VideoStatusData.CurrentlyGenerating,
		"CurrentFile":         models.VideoStatusData.CurrentFile,
	}
	if models.VideoStatusData.LastRun != nil {
		currentVideoStatus["LastRun"] = models.VideoStatusData.LastRun.Format(time.RFC3339)
	}

	user, _ := c.Get("user")
	cachedData := cachedstats.Cache.GetData()

	data := gin.H{
		"Now":                  time.Now().Format(time.RFC3339),
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
		"DateFormat":           config.AppConfig.DateFormat,
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
	if selectedDate == "" {
		selectedDate = logDates[0]
	}

	user, _ := c.Get("user")
	c.HTML(http.StatusOK, "log.html", gin.H{
		"User":           user.(*models.User),
		"AvailableDates": logDates,
		"SelectedDate":   selectedDate,
	})
}

func HandleLogStream(c *gin.Context) {
	selectedDate := c.Query("date")
	if selectedDate == "" {
		c.String(http.StatusBadRequest, "date query parameter is required")
		return
	}

	logPath := filepath.Join(config.AppConfig.DataDir, fmt.Sprintf("ffmpeg_log_%s.txt", selectedDate))
	if !util.FileExists(logPath) {
		c.String(http.StatusNotFound, "Log file not found.")
		return
	}

	file, err := os.Open(logPath)
	if err != nil {
		c.String(http.StatusInternalServerError, "Error opening log file.")
		return
	}
	defer file.Close()

	c.Header("Content-Type", "text/plain; charset=utf-8")
	_, err = io.Copy(c.Writer, file)
	if err != nil {
		c.String(http.StatusInternalServerError, "Error streaming log file.")
		return
	}
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
	users, err := database.GetAllUsers()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "admin.html", gin.H{
			"User":        user.(*models.User),
			"message":     fmt.Sprintf("Error fetching users: %v", err),
			"messageType": "error",
		})
		return
	}

	successMessage := c.Query("success")
	data := gin.H{
		"User":  user.(*models.User),
		"Users": users,
	}
	if successMessage != "" {
		data["message"] = successMessage
		data["messageType"] = "success"
	}

	c.HTML(http.StatusOK, "admin.html", data)
}

func HandleCreateUser(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")
	isAdmin := c.PostForm("isAdmin") == "on" // Checkbox value

	user, _ := c.Get("user")
	templateData := gin.H{"User": user.(*models.User)}

	if username == "" || password == "" {
		users, _ := database.GetAllUsers()
		templateData["Users"] = users
		templateData["message"] = "Username and password cannot be empty."
		templateData["messageType"] = "error"
		c.HTML(http.StatusBadRequest, "admin.html", templateData)
		return
	}

	err := database.CreateUser(username, password, isAdmin)
	if err != nil {
		users, _ := database.GetAllUsers()
		templateData["Users"] = users
		templateData["message"] = fmt.Sprintf("Error creating user: %v", err)
		templateData["messageType"] = "error"
		c.HTML(http.StatusInternalServerError, "admin.html", templateData)
		return
	}

	c.Redirect(http.StatusFound, "/admin?success=User+created+successfully")
}

func HandleDeleteUser(c *gin.Context) {
	username := c.PostForm("username")
	user, _ := c.Get("user")
	loggedInUser := user.(*models.User)

	// Prevent a user from deleting themselves
	if loggedInUser.Username == username {
		users, err := database.GetAllUsers()
		if err != nil {
			// Handle error fetching users, perhaps render an error page
			c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Message": "Failed to fetch user list."})
			return
		}
		c.HTML(http.StatusBadRequest, "admin.html", gin.H{
			"User":        loggedInUser,
			"Users":       users,
			"message":     "You cannot delete your own account.",
			"messageType": "error",
		})
		return
	}

	err := database.DeleteUser(username)
	if err != nil {
		users, _ := database.GetAllUsers()
		c.HTML(http.StatusInternalServerError, "admin.html", gin.H{
			"User":        loggedInUser,
			"Users":       users,
			"message":     fmt.Sprintf("Error deleting user: %v", err),
			"messageType": "error",
		})
		return
	}

	// Redirect to the admin page with a success message
	// Note: Using query parameters for flash messages is simple but has limitations.
	// A more robust solution would use session-based flash messages.
	c.Redirect(http.StatusFound, "/admin")
}

func HandleChangePassword(c *gin.Context) {
	username := c.PostForm("username")
	newPassword := c.PostForm("newPassword")
	user, _ := c.Get("user")
	loggedInUser := user.(*models.User)

	if newPassword == "" {
		users, _ := database.GetAllUsers()
		c.HTML(http.StatusBadRequest, "admin.html", gin.H{
			"User":        loggedInUser,
			"Users":       users,
			"message":     "Password cannot be empty.",
			"messageType": "error",
		})
		return
	}

	err := database.UpdateUserPassword(username, newPassword)
	if err != nil {
		users, _ := database.GetAllUsers()
		c.HTML(http.StatusInternalServerError, "admin.html", gin.H{
			"User":        loggedInUser,
			"Users":       users,
			"message":     fmt.Sprintf("Error changing password: %v", err),
			"messageType": "error",
		})
		return
	}

	c.Redirect(http.StatusFound, "/admin")
}

// HandleUnauthorized renders a user-friendly unauthorized error page.
func HandleUnauthorized(c *gin.Context) {
	c.HTML(http.StatusForbidden, "error.html", gin.H{"Message": "Unauthorized Action"})
}

func HandleShareLink(c *gin.Context) {
	filePath := c.PostForm("filePath")
	if filePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "filePath is required"})
		return
	}

	// Make sure the file path is within the data directory to prevent directory traversal
	absFilePath, err := filepath.Abs(filepath.Join(config.AppConfig.DataDir, filepath.Base(filePath)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to resolve file path"})
		return
	}
	if !strings.HasPrefix(absFilePath, config.AppConfig.DataDir) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	var expiry time.Duration
	if config.AppConfig.ShareLinkExpiryHours > 0 {
		expiry = time.Hour * time.Duration(config.AppConfig.ShareLinkExpiryHours)
	} else {
		expiry = 0 // Unlimited
	}

	token, err := database.CreateShareLink(filePath, expiry)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create share link"})
		return
	}

	shareLink := fmt.Sprintf("%s/public/%s", c.Request.Host, token)
	response := gin.H{"shareLink": shareLink}
	if expiry > 0 {
		response["expiresAt"] = time.Now().Add(expiry).Format(time.RFC3339)
	} else {
		response["expiresAt"] = "Never"
	}

	c.JSON(http.StatusOK, response)
}

func HandlePublicLink(c *gin.Context) {
	token := c.Param("token")
	filePath, err := database.GetSharedFilePath(token)
	if err != nil {
		c.String(http.StatusInternalServerError, "Error retrieving file path")
		return
	}

	if filePath == "" {
		c.String(http.StatusNotFound, "Link not found or expired")
		return
	}

	// Again, ensure the path is safe
	absFilePath, err := filepath.Abs(filepath.Join(config.AppConfig.DataDir, filepath.Base(filePath)))
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to resolve file path")
		return
	}
	if !strings.HasPrefix(absFilePath, config.AppConfig.DataDir) {
		c.String(http.StatusForbidden, "Access denied")
		return
	}

	c.File(absFilePath)
}
