package benchmark

import (
	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/config"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// RegisterDebugRoutes wires the benchmark endpoint only in non-production envs.
func RegisterDebugRoutes(router *gin.Engine, envMode string, sm *session.Manager) {
	if envMode != config.EnvProduction {
		router.GET("/debug/benchmark", benchmarkHandler(router, sm))
	}
}
