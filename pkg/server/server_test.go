package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time-machine/pkg/auth"
	"time-machine/pkg/config"
	"time-machine/pkg/database"
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

	// Initialise the database
	database.InitDB()
	// Run tests
	os.Exit(m.Run())
}

func TestSetupRouter(t *testing.T) {
	// Create dummy template files in the current directory for the test
	os.Create("index.html")
	os.Create("admin.html")
	os.Create("login.html")
	os.Create("error.html")
	os.Create("log.html")
	defer os.Remove("index.html")
	defer os.Remove("admin.html")
	defer os.Remove("login.html")
	defer os.Remove("error.html")
	defer os.Remove("log.html")

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
}
