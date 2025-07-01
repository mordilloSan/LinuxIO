package auth

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"

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

type ctxKey string

const proxyPathKey ctxKey = "proxyPath"

func FilebrowserReverseProxy(secret string) gin.HandlerFunc {
	target, _ := url.Parse("http://127.0.0.1:8090")
	proxy := httputil.NewSingleHostReverseProxy(target)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host

		// Extract session_id cookie manually
		cookie, err := req.Cookie("session_id")
		if err == nil && cookie.Value != "" {
			sess, err := session.Get(cookie.Value)
			if err == nil && sess != nil {
				// Set the header using the secret as header name
				req.Header.Set(secret, sess.User.Name)
			}
		}

	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("X-Frame-Options", "SAMEORIGIN")
		return nil
	}

	return func(c *gin.Context) {
		proxyPath := c.Param("proxyPath")
		c.Request = c.Request.WithContext(
			context.WithValue(c.Request.Context(), proxyPathKey, proxyPath),
		)

		defer func() {
			if rec := recover(); rec != nil {
				if err, ok := rec.(error); ok && err == http.ErrAbortHandler {
					// client closed connection — safe to ignore
					return
				}
				if str, ok := rec.(string); ok && str == "net/http: abort Handler" {
					return
				}
				// unexpected panic, rethrow
				panic(rec)
			}
		}()

		proxy.ServeHTTP(c.Writer, c.Request)
	}

}
