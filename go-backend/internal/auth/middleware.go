package auth

import (
	"net/http"

	"go-backend/internal/logger"
	"go-backend/internal/session"
	"go-backend/internal/utils"

	"github.com/gin-gonic/gin"
)

func CorsMiddleware() gin.HandlerFunc {
	devOrigin := "http://localhost:" + utils.GetDevPort()

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		if origin == devOrigin {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Content-Type")
			c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")

			logger.Debugf("CORS allowed: %s %s", c.Request.Method, origin)
		} else if origin != "" {
			logger.Debugf("CORS denied: %s %s", c.Request.Method, origin)
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	}
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		sess, err := session.ValidateFromRequest(c.Request)
		if err != nil || sess == nil {
			logger.Warnf("⚠️  Unauthorized request or expired session: %v", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Set("session", sess)
		c.Next()
	}
}

// Helper to validate session and handle unauthorized
func GetSessionOrAbort(c *gin.Context) *session.Session {
	sess, err := session.ValidateFromRequest(c.Request)
	if err != nil || sess == nil {
		logger.Warnf("Unauthorized docker access: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid session"})
		c.Abort()
		return nil
	}
	return sess
}
