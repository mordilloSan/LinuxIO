package benchmark

import (
	"fmt"
	"github.com/mordilloSan/LinuxIO/backend/internal/utils"

	"github.com/gin-gonic/gin"
)

func RegisterDebugRoutes(router *gin.Engine, env string) {
	if env != "production" {
		router.GET("/debug/benchmark", benchmarkHandler(router))
	}
}

func benchmarkHandler(router *gin.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie("session_id")
		if err != nil {
			c.JSON(401, gin.H{"error": "unauthorized"})
			return
		}
		results := utils.RunBenchmark("http://localhost:8080", "session_id="+cookie, router, 8)
		var output []gin.H
		for _, r := range results {
			if r.Error != nil {
				output = append(output, gin.H{"endpoint": r.Endpoint, "error": r.Error.Error()})
			} else {
				output = append(output, gin.H{
					"endpoint": r.Endpoint,
					"status":   r.Status,
					"latency":  fmt.Sprintf("%.2fms", float64(r.Latency.Microseconds())/1000),
				})
			}
		}
		c.JSON(200, output)
	}
}
