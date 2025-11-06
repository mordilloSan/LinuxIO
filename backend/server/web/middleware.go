package web

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/go_logger/logger"
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
