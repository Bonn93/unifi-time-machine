package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time-machine/pkg/auth"
	"time-machine/pkg/config"
	"time-machine/pkg/database"
	"time-machine/pkg/jobs"
	"time-machine/pkg/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Set up a temporary directory for data
	tempDir, err := os.MkdirTemp("", "test-data")
	if err != nil {
		panic("Failed to create temp dir")
	}
	defer os.RemoveAll(tempDir)
	config.AppConfig.DataDir = tempDir

	// Initialise the database and jobs
	database.InitDB()
	jobs.InitJobs(database.GetDB())
	// Run tests
	os.Exit(m.Run())
}

func TestSetupRouter(t *testing.T) {
	// Create dummy template and static files for the test
	os.MkdirAll("web/templates", 0755)
	os.Create("web/templates/index.html")
	os.Create("web/templates/admin.html")
	os.Create("web/templates/login.html")
	os.Create("web/templates/error.html")
	os.Create("web/templates/log.html")
	os.MkdirAll("web/static/css", 0755)
	os.Create("web/static/css/style.css")
	os.MkdirAll("web/static/js", 0755)
	os.Create("web/static/js/main.js")

	defer os.RemoveAll("web")

	config.AppConfig.AppKey = "test-secret"
	router := SetupRouter()
	assert.NotNil(t, router)

	// Test unauthenticated routes
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/login", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/unauthorized", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// Test static file routes
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/static/css/style.css", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/static/js/main.js", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Test authenticated routes without auth
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/login", w.Header().Get("Location"))

	// Test authenticated routes with valid auth
	user := &models.User{ID: 1, Username: "test", IsAdmin: false}
	token, _ := auth.GenerateJWT(user)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Test admin routes with non-admin user
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// Test admin routes with admin user
	adminUser := &models.User{ID: 2, Username: "admin", IsAdmin: true}
	adminToken, _ := auth.GenerateJWT(adminUser)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Test admin POST routes with non-admin user
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/force-generate", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// Test admin POST routes with admin user
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/force-generate", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusFound, w.Code) // Expecting a redirect
}
