package auth

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"time-machine/pkg/config"
	"time-machine/pkg/database"
	"time-machine/pkg/models"
)

// UserClaims defines the claims for the JWT.
type UserClaims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
	jwt.RegisteredClaims
}

// jwtSecret is the secret key used for signing JWTs.
var jwtSecret = []byte(config.AppConfig.AppKey)

// GenerateJWT generates a new JWT for the given user.
func GenerateJWT(user *models.User) (string, error) {
	expirationTime := time.Now().Add(24 * time.Hour) // Token valid for 24 hours
	claims := &UserClaims{
		UserID:   user.ID,
		Username: user.Username,
		IsAdmin:  user.IsAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}
	return tokenString, nil
}

// ValidateJWT validates the JWT string and returns the claims if valid.
func ValidateJWT(tokenString string) (*UserClaims, error) {
	claims := &UserClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return claims, nil
}

// AuthMiddleware provides JWT-based authentication middleware.
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		var tokenString string

		// Try to get token from Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			tokenString = strings.TrimPrefix(authHeader, "Bearer ")
		} else {
			// Fallback: Try to get token from cookie
			cookieToken, err := c.Cookie("jwt_token")
			if err == nil {
				tokenString = cookieToken
			}
		}

		if tokenString == "" {
			c.Redirect(http.StatusFound, "/login") // Redirect to login if no token
			c.Abort()
			return
		}

		claims, err := ValidateJWT(tokenString)
		if err != nil {
			log.Printf("JWT validation failed: %v", err)
			c.SetCookie("jwt_token", "", -1, "/", "", false, true) // Clear invalid cookie
			c.Redirect(http.StatusFound, "/login") // Redirect to login if token is invalid or expired
			c.Abort()
			return
		}

		// Store user info in context
		user := &models.User{
			ID:       claims.UserID,
			Username: claims.Username,
			IsAdmin:  claims.IsAdmin,
		}
		c.Set("user", user)
		c.Next()
	}
}

// AdminOnlyMiddleware checks for admin privileges.
func AdminOnlyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userVal, exists := c.Get("user")
		if !exists {
			// This should not happen if AuthMiddleware is run first
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied: User information not found in context"})
			c.Abort()
			return
		}

		user, ok := userVal.(*models.User)
		if !ok || !user.IsAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied: Admin privileges required"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// LoginHandler handles user login requests.
func LoginHandler(c *gin.Context) {
	var login struct {
		Username string `form:"username" binding:"required"`
		Password string `form:"password" binding:"required"`
	}

	if err := c.ShouldBind(&login); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, authenticated := database.CheckUserCredentials(login.Username, login.Password)
	if !authenticated {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	tokenString, err := GenerateJWT(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	// Set JWT as an HttpOnly cookie
	c.SetCookie("jwt_token", tokenString, int(24*time.Hour.Seconds()), "/", "", false, true)
	c.Redirect(http.StatusFound, "/") // Redirect to dashboard
}

// LogoutHandler handles user logout requests by clearing the JWT cookie.
func LogoutHandler(c *gin.Context) {
	c.SetCookie("jwt_token", "", -1, "/", "", false, true) // Clear the cookie
	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}