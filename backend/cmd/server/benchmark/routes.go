package benchmark

import (
	"github.com/gin-gonic/gin"
)

func RegisterDebugRoutes(router *gin.Engine, env string) {
	if env != "production" {
		router.GET("/debug/benchmark", benchmarkHandler(router))
	}
}
