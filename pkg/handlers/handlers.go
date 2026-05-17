package handlers

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"time-machine/pkg/cachedstats"
	"time-machine/pkg/config"
	"time-machine/pkg/database"
	"time-machine/pkg/models"
	"time-machine/pkg/services/settings"
	"time-machine/pkg/services/video"
	"time-machine/pkg/stats"
	"time-machine/pkg/util"

	"github.com/gin-gonic/gin"
)

// HandleForceGenerate enqueues all timelapse jobs to be processed by the worker.
func HandleForceGenerate(c *gin.Context) {
	go video.EnqueueTimelapseJobs()
	c.Redirect(http.StatusFound, "/")
}

func HandleHealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "healthy"})
}

func HandleLoginGet(c *gin.Context) {
	c.HTML(http.StatusOK, "login.html", gin.H{})
}

// findTimelapseFile looks for a timelapse video in the preferred format, then falls back to WebM.
// Returns (diskPath, webPath, format) — all empty strings if nothing found.
func findTimelapseFile(name, preferredFormat string) (string, string, string) {
	for _, fmt := range []string{preferredFormat, "webm", "mp4"} {
		dp := video.DiskPath(name, fmt)
		if util.FileExists(dp) {
			return dp, video.TimelapseWebPath(name, fmt), fmt
		}
	}
	return "", "", ""
}

func HandleDashboard(c *gin.Context) {
	models.VideoStatusData.RLock()
	defer models.VideoStatusData.RUnlock()

	format := settings.Get("video.format", "webm")
	dataDir := config.AppConfig.DataDir

	availableTimelapses := make(map[string][]gin.H)
	timelapseOrder := []string{"Daily", "Weekly", "Monthly", "Yearly"}
	for _, t := range timelapseOrder {
		availableTimelapses[t] = []gin.H{}
	}

	// Daily 24-hour timelapses
	var dailyVideos []gin.H
	for i := 0; i < settings.GetInt("video.daily_days", 30); i++ {
		targetDate := time.Now().AddDate(0, 0, -i)
		dateStr := targetDate.Format("2006-01-02")
		timelapseName := fmt.Sprintf("24_hour_%s", dateStr)
		_, webPath, usedFmt := findTimelapseFile(timelapseName, format)
		if webPath != "" {
			dailyVideos = append(dailyVideos, gin.H{
				"Date":        dateStr,
				"DateDisplay": util.FormatDate(targetDate),
				"Path":        webPath,
				"Format":      usedFmt,
			})
		}
	}
	if len(dailyVideos) > 0 {
		sort.Slice(dailyVideos, func(i, j int) bool {
			return dailyVideos[i]["Date"].(string) > dailyVideos[j]["Date"].(string)
		})
		availableTimelapses["Daily"] = dailyVideos
	}

	// Collect weekly/monthly/yearly names from all formats present on disk
	nameSet := collectTimelapseNames(dataDir)

	for timelapseName := range nameSet {
		_, webPath, usedFmt := findTimelapseFile(timelapseName, format)
		if webPath == "" {
			continue
		}

		switch {
		case strings.HasPrefix(timelapseName, "week_"):
			dateStr := strings.TrimPrefix(timelapseName, "week_")
			weekStart, err := time.Parse("2006-01-02", dateStr)
			displayDate := "Week of " + dateStr
			if err == nil {
				displayDate = "Week of " + util.FormatDate(weekStart)
			}
			availableTimelapses["Weekly"] = append(availableTimelapses["Weekly"], gin.H{
				"Date":        dateStr,
				"DateDisplay": displayDate,
				"Path":        webPath,
				"Format":      usedFmt,
			})

		case strings.HasPrefix(timelapseName, "month_"):
			monthStr := strings.TrimPrefix(timelapseName, "month_")
			monthStart, err := time.Parse("2006-01", monthStr)
			displayDate := monthStr
			if err == nil {
				displayDate = monthStart.Format("January 2006")
			}
			availableTimelapses["Monthly"] = append(availableTimelapses["Monthly"], gin.H{
				"Date":        monthStr,
				"DateDisplay": displayDate,
				"Path":        webPath,
				"Format":      usedFmt,
			})

		case strings.HasPrefix(timelapseName, "year_"):
			yearStr := strings.TrimPrefix(timelapseName, "year_")
			availableTimelapses["Yearly"] = append(availableTimelapses["Yearly"], gin.H{
				"Date":        yearStr,
				"DateDisplay": yearStr,
				"Path":        webPath,
				"Format":      usedFmt,
			})
		}
	}

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
		"VideoFormat":               format,
	}

	c.HTML(http.StatusOK, "index.html", data)
}

// collectTimelapseNames returns a set of timelapse names found in any format on disk.
// It covers weekly/monthly/yearly only (not daily, which is date-iterated).
func collectTimelapseNames(dataDir string) map[string]bool {
	names := make(map[string]bool)

	for _, ext := range []string{"*.webm", "*.mp4"} {
		files, _ := filepath.Glob(filepath.Join(dataDir, "timelapse_"+ext))
		for _, f := range files {
			base := filepath.Base(f)
			for _, suf := range []string{".webm", ".mp4"} {
				base = strings.TrimSuffix(base, suf)
			}
			name := strings.TrimPrefix(base, "timelapse_")
			// Include weekly/monthly/yearly but not daily (handled by date-iteration)
			if strings.HasPrefix(name, "week_") ||
				strings.HasPrefix(name, "month_") ||
				strings.HasPrefix(name, "year_") {
				names[name] = true
			}
		}
	}

	// HLS directories
	hlsBase := filepath.Join(dataDir, "hls")
	entries, _ := filepath.Glob(filepath.Join(hlsBase, "timelapse_*/master.m3u8"))
	for _, e := range entries {
		dir := filepath.Base(filepath.Dir(e))
		name := strings.TrimPrefix(dir, "timelapse_")
		if strings.HasPrefix(name, "week_") ||
			strings.HasPrefix(name, "month_") ||
			strings.HasPrefix(name, "year_") {
			names[name] = true
		}
	}

	return names
}

func HandleLog(c *gin.Context) {
	dates, err := database.GetFFmpegLogDates()
	if err != nil {
		c.String(http.StatusInternalServerError, "Error fetching log dates: %v", err)
		return
	}

	if len(dates) == 0 {
		c.HTML(http.StatusOK, "log.html", gin.H{
			"Message": "No log files found.",
		})
		return
	}

	var logDates []map[string]string
	for _, dateStr := range dates {
		logDates = append(logDates, map[string]string{
			"value":   dateStr,
			"display": util.FormatDateForDisplay(dateStr),
		})
	}

	selectedDate := c.Query("date")
	if selectedDate == "" {
		selectedDate = dates[0]
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

	content, err := database.GetFFmpegLogContent(selectedDate)
	if err != nil {
		c.String(http.StatusInternalServerError, "Error fetching log content.")
		return
	}
	if content == "" {
		c.String(http.StatusNotFound, "No log entries found for this date.")
		return
	}

	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusOK, "%s", content)
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

	allSettings, _ := settings.GetAll()

	successMessage := c.Query("success")
	data := gin.H{
		"User":     user.(*models.User),
		"Users":    users,
		"Settings": allSettings,
	}
	if successMessage != "" {
		data["SettingsSuccess"] = successMessage
	}

	c.HTML(http.StatusOK, "admin.html", data)
}

func HandleCreateUser(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")
	isAdmin := c.PostForm("isAdmin") == "on"

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

	if loggedInUser.Username == username {
		users, err := database.GetAllUsers()
		if err != nil {
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

func HandleUnauthorized(c *gin.Context) {
	c.HTML(http.StatusForbidden, "error.html", gin.H{"Message": "Unauthorized Action"})
}

func HandleShareLink(c *gin.Context) {
	filePath := c.PostForm("filePath")
	if filePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "filePath is required"})
		return
	}

	absFilePath, err := filepath.Abs(filepath.Join(config.AppConfig.DataDir, filepath.Base(filePath)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to resolve file path"})
		return
	}
	if !strings.HasPrefix(absFilePath, config.AppConfig.DataDir) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	expiryHours := settings.GetInt("share.link_expiry_hours", 4)
	var expiry time.Duration
	if expiryHours > 0 {
		expiry = time.Hour * time.Duration(expiryHours)
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

// knownSettingKeys lists all keys that HandleSaveSettings will accept.
var knownSettingKeys = func() []string {
	keys := make([]string, len(settings.KnownSettings))
	for i, e := range settings.KnownSettings {
		keys[i] = e.Key
	}
	return keys
}()

// integerSettingKeys are keys that must parse as integers.
var integerSettingKeys = map[string]bool{
	"snapshot.interval_sec":      true,
	"video.cron_interval_sec":    true,
	"video.hls_segment_sec":      true,
	"video.daily_days":           true,
	"snapshot.retention_days":    true,
	"gallery.retention_days":     true,
	"share.link_expiry_hours":    true,
	"video.daylight_start_hour":  true,
	"video.daylight_end_hour":    true,
	"video.daylight_target_hour": true,
	"video.weekly_keep":          true,
	"video.monthly_keep":         true,
}

// HandleDataFile serves files from DataDir with correct MIME types for HLS segments and playlists.
func HandleDataFile(c *gin.Context) {
	fp := c.Param("filepath")
	absPath := filepath.Join(config.AppConfig.DataDir, filepath.FromSlash(fp))
	// Prevent path traversal
	if !strings.HasPrefix(absPath, config.AppConfig.DataDir) {
		c.Status(http.StatusForbidden)
		return
	}
	switch {
	case strings.HasSuffix(fp, ".m3u8"):
		c.Header("Content-Type", "application/x-mpegURL")
	case strings.HasSuffix(fp, ".ts"):
		c.Header("Content-Type", "video/MP2T")
	}
	c.File(absPath)
}

// HandleSaveSettings validates and persists admin-submitted settings.
func HandleSaveSettings(c *gin.Context) {
	user, _ := c.Get("user")

	for _, key := range knownSettingKeys {
		val := c.PostForm(key)
		if val == "" {
			continue // unchanged / not submitted
		}
		if integerSettingKeys[key] {
			if _, err := fmt.Sscanf(val, "%d", new(int)); err != nil {
				c.HTML(http.StatusBadRequest, "admin.html", gin.H{
					"User":        user.(*models.User),
					"message":     fmt.Sprintf("Invalid value for %s: must be an integer", key),
					"messageType": "error",
				})
				return
			}
		}
		if err := settings.Set(key, val); err != nil {
			c.HTML(http.StatusInternalServerError, "admin.html", gin.H{
				"User":        user.(*models.User),
				"message":     fmt.Sprintf("Failed to save setting %s: %v", key, err),
				"messageType": "error",
			})
			return
		}
	}

	// Kick off a background regeneration so format/quality changes take effect
	// immediately rather than waiting for the next scheduled cron cycle.
	go video.EnqueueTimelapseJobs()

	c.Redirect(http.StatusFound, "/admin?success=Settings+saved.+Timelapse+regeneration+has+been+queued.")
}
