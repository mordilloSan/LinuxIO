package benchmark

import (
	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/common/session"
)

// RegisterDebugRoutes wires the benchmark endpoint only in non-production envs.
func RegisterDebugRoutes(router *gin.Engine, env string, sm *session.Manager) {
	if env != "production" {
		router.GET("/debug/benchmark", benchmarkHandler(router, sm))
	}
}
