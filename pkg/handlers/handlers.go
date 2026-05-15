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
	"time-machine/pkg/cachedstats"
	"time-machine/pkg/config"
	"time-machine/pkg/database"
	"time-machine/pkg/models"
	"time-machine/pkg/services/video"
	"time-machine/pkg/stats"
	"time-machine/pkg/util"

	"github.com/gin-gonic/gin"
)

// HandleForceGenerate enqueues all timelapse jobs to be processed by the worker.
func HandleForceGenerate(c *gin.Context) {
	go video.EnqueueTimelapseJobs() // Run in a goroutine to not block the UI
	c.Redirect(http.StatusFound, "/")
}

// --- HANDLERS ---

// HandleHealthCheck provides a simple health check endpoint
func HandleHealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "healthy"})
}

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
				"Date":        dateStr,
				"DateDisplay": util.FormatDate(targetDate),
				"Path":        "/data/" + fileName,
			})
		}
	}
	if len(dailyVideos) > 0 {
		sort.Slice(dailyVideos, func(i, j int) bool {
			return dailyVideos[i]["Date"].(string) > dailyVideos[j]["Date"].(string)
		})
		availableTimelapses["Daily"] = dailyVideos
	}

	// --- Weekly, Monthly, Yearly timelapses: discovered from filesystem ---
	allVideoFiles, err := filepath.Glob(filepath.Join(config.AppConfig.DataDir, "timelapse_*.webm"))
	if err != nil {
		fmt.Printf("Error globbing video files: %v\n", err)
	}

	for _, file := range allVideoFiles {
		fileName := filepath.Base(file)
		switch {
		case strings.HasPrefix(fileName, "timelapse_week_"):
			dateStr := strings.TrimPrefix(strings.TrimSuffix(fileName, ".webm"), "timelapse_week_")
			weekStart, err := time.Parse("2006-01-02", dateStr)
			displayDate := "Week of " + dateStr
			if err == nil {
				displayDate = "Week of " + util.FormatDate(weekStart)
			}
			availableTimelapses["Weekly"] = append(availableTimelapses["Weekly"], gin.H{
				"Date":        dateStr,
				"DateDisplay": displayDate,
				"Path":        "/data/" + fileName,
			})

		case strings.HasPrefix(fileName, "timelapse_month_"):
			monthStr := strings.TrimPrefix(strings.TrimSuffix(fileName, ".webm"), "timelapse_month_")
			monthStart, err := time.Parse("2006-01", monthStr)
			displayDate := monthStr
			if err == nil {
				displayDate = monthStart.Format("January 2006")
			}
			availableTimelapses["Monthly"] = append(availableTimelapses["Monthly"], gin.H{
				"Date":        monthStr,
				"DateDisplay": displayDate,
				"Path":        "/data/" + fileName,
			})

		case strings.HasPrefix(fileName, "timelapse_year_"):
			yearStr := strings.TrimPrefix(strings.TrimSuffix(fileName, ".webm"), "timelapse_year_")
			availableTimelapses["Yearly"] = append(availableTimelapses["Yearly"], gin.H{
				"Date":        yearStr,
				"DateDisplay": yearStr,
				"Path":        "/data/" + fileName,
			})
		}
	}

	// Sort each category newest first
	for _, typeName := range []string{"Weekly", "Monthly", "Yearly"} {
		sort.Slice(availableTimelapses[typeName], func(i, j int) bool {
			return availableTimelapses[typeName][i]["Date"].(string) > availableTimelapses[typeName][j]["Date"].(string)
		})
	}

	currentVideoStatus := gin.H{
		"IsRunning":           models.VideoStatusData.IsRunning,
		"LastRun":             "N/A",
		"Error":               models.VideoStatusData.Error,
		"CurrentlyGenerating": models.VideoStatusData.CurrentlyGenerating,
		"CurrentFile":         models.VideoStatusData.CurrentFile,
	}
	if models.VideoStatusData.LastRun != nil {
		currentVideoStatus["LastRun"] = util.FormatDateTime(*models.VideoStatusData.LastRun)
	}

	user, _ := c.Get("user")
	cachedData := cachedstats.Cache.GetData()

	defaultDate := time.Now().Format("2006-01-02")
	defaultDateDisplay := util.FormatDateForDisplay(defaultDate)

	data := gin.H{
		"Now":                       util.FormatDateTime(time.Now()),
		"AvailableTimelapses":       availableTimelapses,
		"TimelapseOrder":            timelapseOrder,
		"VideoStatus":               currentVideoStatus,
		"ImageStats":                cachedData,
		"SystemInfo":                cachedData["system_info"],
		"CameraStatus":              cachedData["camera_status"],
		"DefaultGalleryDate":        defaultDate,
		"DefaultGalleryDateDisplay": defaultDateDisplay,
		"DefaultGalleryImages":      cachedData["daily_gallery"],
		"AvailableDates":            cachedData["available_dates"],
		"User":                      user.(*models.User),
		"DateFormat":                config.AppConfig.DateFormat,
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

	var logDates []map[string]string
	var firstDate string
	for i, file := range logFiles {
		// Extract YYYY-MM-DD from the filename
		name := filepath.Base(file)
		dateStr := strings.TrimSuffix(strings.TrimPrefix(name, "ffmpeg_log_"), ".txt")
		if i == 0 {
			firstDate = dateStr
		}
		logDates = append(logDates, map[string]string{
			"value":   dateStr,
			"display": util.FormatDateForDisplay(dateStr),
		})
	}

	// Determine which log to display
	selectedDate := c.Query("date")
	if selectedDate == "" {
		selectedDate = firstDate
	}
	selectedDateDisplay := util.FormatDateForDisplay(selectedDate)

	user, _ := c.Get("user")
	c.HTML(http.StatusOK, "log.html", gin.H{
		"User":                user.(*models.User),
		"AvailableDates":      logDates,
		"SelectedDate":        selectedDate,
		"SelectedDateDisplay": selectedDateDisplay,
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
		response["expiresAt"] = util.FormatDateTime(time.Now().Add(expiry))
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
