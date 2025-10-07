package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
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

// FilebrowserReverseProxy proxies to the FileBrowser service.
// The target base URL is resolved on each request via getBaseURL()
// (e.g., "http://172.18.0.2:80" - Docker network IP, no published ports).
func FilebrowserReverseProxy(secret string, sm *session.Manager, getBaseURL func() string) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					return
				}
				if str, ok := rec.(string); ok && str == "net/http: abort Handler" {
					return
				}
				panic(rec)
			}
		}()

		base := strings.TrimSpace(getBaseURL())
		if base == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "filebrowser not ready"})
			return
		}

		target, err := url.Parse(base)
		if err != nil {
			logger.Warnf("invalid FileBrowser target URL %q: %v", base, err)
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}

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

		proxyPath := c.Param("proxyPath")
		c.Request = c.Request.WithContext(
			context.WithValue(c.Request.Context(), proxyPathKey, proxyPath),
		)

		proxy.ServeHTTP(c.Writer, c.Request)
	}
}
