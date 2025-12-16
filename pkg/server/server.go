package server

import (
	"encoding/json"
	"html/template"
	"log"

	"github.com/gin-gonic/gin"

	"time-machine/pkg/auth"
	"time-machine/pkg/config"
	"time-machine/pkg/handlers"
)

func SetupRouter() *gin.Engine {
	r := gin.Default()

	// --- Template and Authentication Setup ---
	r.SetFuncMap(template.FuncMap{
		"js": func(v interface{}) (template.JS, error) {
			j, err := json.Marshal(v)
			return template.JS(j), err
		},
	})
	r.LoadHTMLFiles("index.html", "admin.html", "login.html", "error.html", "log.html")

	// Login page route (GET) - serves the login HTML
	r.GET("/login", handlers.HandleLoginGet)
	// Login API endpoint (POST) - handles login logic and JWT issuance
	r.POST("/api/login", auth.LoginHandler)
	r.GET("/unauthorized", handlers.HandleUnauthorized)

	// --- Authenticated Route Group ---
	authorized := r.Group("/")
	authorized.Use(auth.AuthMiddleware()) // Use new JWT-based auth middleware
	{
		// Dashboard
		authorized.GET("/", handlers.HandleDashboard)

		// Static Files
		authorized.Static("/data", config.AppConfig.DataDir)

		// Actions
		authorized.GET("/log", handlers.HandleLog)

		// API Endpoints
		authorized.GET("/api/status", handlers.HandleSystemStats)
		authorized.GET("/api/images", handlers.HandleImageStats)
		authorized.GET("/api/gallery", handlers.HandleDailyGallery)

		// --- Admin-Only Route Group ---
		adminRoutes := authorized.Group("/")
		adminRoutes.Use(auth.AdminOnlyMiddleware())
		{
			adminRoutes.POST("/generate", handlers.HandleForceGenerate)
			adminRoutes.GET("/admin", handlers.HandleAdminPage) // Note: removed trailing slash for consistency
			adminRoutes.POST("/admin/users", handlers.HandleCreateUser)
		}
		// Logout endpoint (authenticated)
		authorized.GET("/logout", auth.LogoutHandler)
	}

	return r
}

func StartServer() {
	r := SetupRouter()
	log.Println("Gin server starting on port 8080...")

	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Gin server failed to start: %v", err)
	}
}
