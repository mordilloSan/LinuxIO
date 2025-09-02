package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
)

func CorsMiddleware(vitePort int) gin.HandlerFunc {
	devLocalhost := fmt.Sprintf("http://localhost:%d", vitePort)
	dev127 := fmt.Sprintf("http://127.0.0.1:%d", vitePort)

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin == devLocalhost || origin == dev127 {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
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
			sess, err := session.GetSession(cookie.Value)
			if err == nil && sess != nil {
				// Set the header using the secret as header name
				req.Header.Set(secret, sess.User.Username)
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

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		sess, err := session.ValidateSessionFromRequest(c.Request)
		if err != nil || sess == nil {
			logger.Warnf("⚠️  Unauthorized request or expired session: %v", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Set("session", sess)
		c.Next()
	}
}
