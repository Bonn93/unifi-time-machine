package handlers

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"time-machine/pkg/config"
	"time-machine/pkg/database"
	"time-machine/pkg/jobs"
	"time-machine/pkg/models"
)

func setupTestApp(t *testing.T) *gin.Engine {
	gin.SetMode(gin.TestMode)

	// Setup temporary directories for config
	config.AppConfig.DataDir = t.TempDir()
	config.AppConfig.SnapshotsDir = filepath.Join(config.AppConfig.DataDir, "snapshots")
	config.AppConfig.GalleryDir = filepath.Join(config.AppConfig.DataDir, "gallery")
	os.MkdirAll(config.AppConfig.SnapshotsDir, 0755)
	os.MkdirAll(config.AppConfig.GalleryDir, 0755)

	// Init DB
	database.InitDB()
	jobs.InitJobs(database.GetDB())

	// Setup router and templates
	r := gin.Default()
	tempDir := t.TempDir()
	templateDir := filepath.Join(tempDir, "templates")
	os.MkdirAll(templateDir, 0755)
	dummyTemplates := []string{"login.html", "index.html", "log.html", "admin.html", "error.html"}
	for _, tmpl := range dummyTemplates {
		filePath := filepath.Join(templateDir, tmpl)
		// Provide minimal valid templates
		content := `<!DOCTYPE html><html><body>{{.}}</body></html>`
		if tmpl == "admin.html" {
			// A more representative template for admin page to allow for better testing
			content = `
				<!DOCTYPE html><html><body>
				{{range .Users}}
					<div class="user">{{.Username}}</div>
				{{end}}
				<div class="message">{{.message}}</div>
				</body></html>`
		}
		os.WriteFile(filePath, []byte(content), 0644)
	}
	r.LoadHTMLGlob(templateDir + "/*")

	return r
}

func TestHandleLoginGet(t *testing.T) {
	r := setupTestApp(t)
	r.GET("/login", HandleLoginGet)

	req, _ := http.NewRequest("GET", "/login", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleUnauthorized(t *testing.T) {
	r := setupTestApp(t)
	r.GET("/unauthorized", HandleUnauthorized)

	req, _ := http.NewRequest("GET", "/unauthorized", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleAdminPage(t *testing.T) {
	r := setupTestApp(t)
	r.GET("/admin", func(c *gin.Context) {
		c.Set("user", &models.User{Username: "admin", IsAdmin: true})
		HandleAdminPage(c)
	})

	req, _ := http.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleSystemStatsJSON(t *testing.T) {
	r := setupTestApp(t)
	r.GET("/stats/system", HandleSystemStatsJSON)

	req, _ := http.NewRequest("GET", "/stats/system", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleImageStats(t *testing.T) {
	r := setupTestApp(t)
	r.GET("/stats/images", HandleImageStats)

	req, _ := http.NewRequest("GET", "/stats/images", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleDailyGallery(t *testing.T) {
	r := setupTestApp(t)
	r.GET("/gallery", HandleDailyGallery)

	// Test with no date (should default to today)
	req, _ := http.NewRequest("GET", "/gallery", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), time.Now().Format("2006-01-02"))

	// Test with a specific date
	req, _ = http.NewRequest("GET", "/gallery?date=2023-01-01", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "2023-01-01")
}

func TestHandleLog(t *testing.T) {
	r := setupTestApp(t)
	os.WriteFile(filepath.Join(config.AppConfig.DataDir, "ffmpeg_log_2023-01-01.txt"), []byte("log content"), 0644)
	r.GET("/log", func(c *gin.Context) {
		c.Set("user", &models.User{Username: "test"})
		HandleLog(c)
	})

	req, _ := http.NewRequest("GET", "/log", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleDashboard(t *testing.T) {
	r := setupTestApp(t)
	r.GET("/", func(c *gin.Context) {
		c.Set("user", &models.User{Username: "test"})
		HandleDashboard(c)
	})

	req, _ := http.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleForceGenerate(t *testing.T) {
	r := setupTestApp(t)
	r.POST("/force-generate", HandleForceGenerate)

	req, _ := http.NewRequest("POST", "/force-generate", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/", w.Header().Get("Location"))

	// Allow the background job to complete
	time.Sleep(100 * time.Millisecond)
}

func TestHandleCreateUser(t *testing.T) {
	r := setupTestApp(t)
	r.POST("/create-user", func(c *gin.Context) {
		c.Set("user", &models.User{Username: "admin", IsAdmin: true})
		HandleCreateUser(c)
	})

	// Test successful creation
	form := "username=newuser&password=newpassword&isAdmin=on"
	req, _ := http.NewRequest("POST", "/create-user", strings.NewReader(form))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/admin?success=User+created+successfully", w.Header().Get("Location"))

	// Test empty username
	form = "username=&password=newpassword&isAdmin=on"
	req, _ = http.NewRequest("POST", "/create-user", strings.NewReader(form))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleDashboard_VideoGrouping(t *testing.T) {

	// 1. Setup temp data directory
	r := setupTestApp(t)

	// 2. Create dummy video files
	today := time.Now()
	yesterday := today.AddDate(0, 0, -1)
	os.WriteFile(filepath.Join(config.AppConfig.DataDir, "timelapse_24_hour_"+today.Format("2006-01-02")+".webm"), []byte(""), 0644)
	os.WriteFile(filepath.Join(config.AppConfig.DataDir, "timelapse_24_hour_"+yesterday.Format("2006-01-02")+".webm"), []byte(""), 0644)
	os.WriteFile(filepath.Join(config.AppConfig.DataDir, "timelapse_1_week.webm"), []byte(""), 0644)
	os.WriteFile(filepath.Join(config.AppConfig.DataDir, "timelapse_1_week_20251026_120000.webm"), []byte(""), 0644)
	os.WriteFile(filepath.Join(config.AppConfig.DataDir, "timelapse_1_month.webm"), []byte(""), 0644)

	// 3. Setup router and special template
	r.SetFuncMap(template.FuncMap{
		"marshal": func(v interface{}) (template.HTML, error) {
			a, err := json.Marshal(v)
			return template.HTML(a), err
		},
	})


	// 4. Setup handler and execute request
	r.GET("/", func(c *gin.Context) {
		c.Set("user", &models.User{Username: "test"})
		config.AppConfig.DaysOf24HourSnapshots = 2
		HandleDashboard(c)
	})

	req, _ := http.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAdminPageWithUsers(t *testing.T) {
	r := setupTestApp(t)
	database.CreateUser("user1", "pass1", false)
	database.CreateUser("user2", "pass2", true)

	r.GET("/admin", func(c *gin.Context) {
		c.Set("user", &models.User{Username: "admin", IsAdmin: true})
		HandleAdminPage(c)
	})

	req, _ := http.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "user1")
	assert.Contains(t, w.Body.String(), "user2")
}

func TestHandleDeleteUser(t *testing.T) {
	r := setupTestApp(t)
	database.CreateUser("user-to-delete", "password", false)
	database.CreateUser("admin-user", "password", true)

	r.POST("/admin/users/delete", func(c *gin.Context) {
		c.Set("user", &models.User{Username: "admin-user", IsAdmin: true})
		HandleDeleteUser(c)
	})

	// Test deleting a user
	form := "username=user-to-delete"
	req, _ := http.NewRequest("POST", "/admin/users/delete", strings.NewReader(form))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/admin", w.Header().Get("Location"))
	exists, _ := database.UserExists("user-to-delete")
	assert.False(t, exists)

	// Test deleting self
	form = "username=admin-user"
	req, _ = http.NewRequest("POST", "/admin/users/delete", strings.NewReader(form))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	exists, _ = database.UserExists("admin-user")
	assert.True(t, exists)
}

func TestHandleChangePassword(t *testing.T) {
	r := setupTestApp(t)
	database.CreateUser("user-to-change-password", "oldpassword", false)

	r.POST("/admin/users/password", func(c *gin.Context) {
		c.Set("user", &models.User{Username: "admin", IsAdmin: true})
		HandleChangePassword(c)
	})

	// Test changing password
	form := "username=user-to-change-password&newPassword=newpassword"
	req, _ := http.NewRequest("POST", "/admin/users/password", strings.NewReader(form))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	_, authenticated := database.CheckUserCredentials("user-to-change-password", "newpassword")
	assert.True(t, authenticated)

	// Test empty password
	form = "username=user-to-change-password&newPassword="
	req, _ = http.NewRequest("POST", "/admin/users/password", strings.NewReader(form))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleShareLink(t *testing.T) {
	r := setupTestApp(t)
	r.POST("/share", func(c *gin.Context) {
		c.Set("user", &models.User{Username: "admin", IsAdmin: true})
		HandleShareLink(c)
	})

	// Test successful creation
	form := "filePath=/data/test.mp4"
	req, _ := http.NewRequest("POST", "/share", strings.NewReader(form))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "shareLink")
	assert.Contains(t, w.Body.String(), "expiresAt")

	// Test unlimited expiry
	config.AppConfig.ShareLinkExpiryHours = 0
	req, _ = http.NewRequest("POST", "/share", strings.NewReader(form))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"expiresAt":"Never"`)

	// Test missing filePath
	form = "filePath="
	req, _ = http.NewRequest("POST", "/share", strings.NewReader(form))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandlePublicLink(t *testing.T) {
	r := setupTestApp(t)
	r.GET("/public/:token", HandlePublicLink)

	// Create a dummy file
	dummyFilePath := filepath.Join(config.AppConfig.DataDir, "test.mp4")
	os.WriteFile(dummyFilePath, []byte("dummy content"), 0644)

	// Create a share link
	token, err := database.CreateShareLink("test.mp4", time.Hour)
	assert.NoError(t, err)

	// Test valid link
	req, _ := http.NewRequest("GET", "/public/"+token, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "dummy content", w.Body.String())

	// Test invalid link
	req, _ = http.NewRequest("GET", "/public/invalidtoken", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

