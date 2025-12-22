package handlers

import (
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

func setupRouter() *gin.Engine {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	// Create dummy template files
	tempDir := os.TempDir()

	// Create a temporary directory for templates
	templateDir := filepath.Join(tempDir, "templates")
	os.MkdirAll(templateDir, 0755)

	// Dummy template files
	dummyTemplates := []string{"login.html", "index.html", "log.html", "admin.html", "error.html"}
	for _, tmpl := range dummyTemplates {
		filePath := filepath.Join(templateDir, tmpl)
		content := []byte("")
		if tmpl == "log.html" {
			content = []byte(`{{ .LogContent }}`)
		} else if tmpl == "admin.html" {
			content = []byte(`{{ .message }}`)
		} else if tmpl == "error.html" {
			content = []byte(`{{ .Message }}`)
		} else if tmpl == "login.html" {
			content = []byte(`{{ .Error }}`)
		} else if tmpl == "index.html" {
			content = []byte(`{{ .DefaultGalleryDate }}`)
		}
		os.WriteFile(filePath, content, 0644)
	}

	r.LoadHTMLGlob(templateDir + "/*")

	return r
}

func TestHandleLoginGet(t *testing.T) {
	r := setupRouter()
	r.GET("/login", HandleLoginGet)

	req, _ := http.NewRequest("GET", "/login", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleLogout(t *testing.T) {
	r := setupRouter()
	r.GET("/logout", HandleLogout)

	req, _ := http.NewRequest("GET", "/logout", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/login", w.Header().Get("Location"))
}

func TestHandleUnauthorized(t *testing.T) {
	r := setupRouter()
	r.GET("/unauthorized", HandleUnauthorized)

	req, _ := http.NewRequest("GET", "/unauthorized", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleAdminPage(t *testing.T) {
	r := setupRouter()
	r.GET("/admin", func(c *gin.Context) {
		c.Set("user", &models.User{Username: "admin", IsAdmin: true})
		HandleAdminPage(c)
	})

	req, _ := http.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleSystemStats(t *testing.T) {
	r := setupRouter()
	r.GET("/stats/system", HandleSystemStats)

	req, _ := http.NewRequest("GET", "/stats/system", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleImageStats(t *testing.T) {
	r := setupRouter()
	r.GET("/stats/images", HandleImageStats)

	req, _ := http.NewRequest("GET", "/stats/images", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleDailyGallery(t *testing.T) {
	r := setupRouter()
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
	r := setupRouter()
	config.AppConfig.DataDir = t.TempDir()
	os.WriteFile(filepath.Join(config.AppConfig.DataDir, "ffmpeg_log_2023-01-01.txt"), []byte("log content"), 0644)
	r.GET("/log", func(c *gin.Context) {
		c.Set("user", &models.User{Username: "test"})
		HandleLog(c)
	})

	req, _ := http.NewRequest("GET", "/log", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "log content")
}

func TestHandleDashboard(t *testing.T) {
	r := setupRouter()
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
	r := setupRouter()
	config.AppConfig.DataDir = t.TempDir()
	database.InitDB()
	jobs.InitJobs(database.GetDB())
	r.GET("/force-generate", HandleForceGenerate)

	req, _ := http.NewRequest("GET", "/force-generate", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/", w.Header().Get("Location"))
}

func TestHandleCreateUser(t *testing.T) {
	r := setupRouter()
	config.AppConfig.DataDir = t.TempDir()
	database.InitDB()
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

	assert.Equal(t, http.StatusOK, w.Code)

	// Test empty username
	form = "username=&password=newpassword&isAdmin=on"
	req, _ = http.NewRequest("POST", "/create-user", strings.NewReader(form))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleLoginPost(t *testing.T) {
	r := setupRouter()
	config.AppConfig.DataDir = t.TempDir()
	database.InitDB()
	database.CreateUser("testuser", "password123", false)
	r.POST("/login", HandleLoginPost)

	// Test successful login
	form := "username=testuser&password=password123"
	req, _ := http.NewRequest("POST", "/login", strings.NewReader(form))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/", w.Header().Get("Location"))

	// Test failed login
	form = "username=testuser&password=wrongpassword"
	req, _ = http.NewRequest("POST", "/login", strings.NewReader(form))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
