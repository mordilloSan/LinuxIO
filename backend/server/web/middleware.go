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

// FilebrowserReverseProxy proxies to the FileBrowser service and
// injects a per-request header (name = secret) with the LinuxIO username.
// It reads the session cookie and resolves the session via the provided Manager.
func FilebrowserReverseProxy(secret string, sm *session.Manager) gin.HandlerFunc {
	target, _ := url.Parse("http://127.0.0.1:8090")
	proxy := httputil.NewSingleHostReverseProxy(target)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
		// Set a header named by 'secret' with the username.
		if s, err := sm.ValidateFromRequest(req); err == nil && s != nil {
			req.Header.Set(secret, s.User.Username)
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
					return // client closed connection — ignore
				}
				if str, ok := rec.(string); ok && str == "net/http: abort Handler" {
					return
				}
				panic(rec) // unexpected
			}
		}()

		proxy.ServeHTTP(c.Writer, c.Request)
	}
}
