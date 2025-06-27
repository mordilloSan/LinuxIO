package control

import (
	"go-backend/cmd/bridge/handlers/types"
	"go-backend/internal/logger"
)

func ControlHandlers(shutdownChan chan string) map[string]types.HandlerFunc {
	return map[string]types.HandlerFunc{
		"shutdown": func(args []string) (any, error) {
			reason := "unknown"
			if len(args) > 0 {
				reason = args[0] // "logout" or "forced"
			}
			logger.Infof("Received shutdown command: %s", reason)
			select {
			case shutdownChan <- reason:
			default:
			}
			return "Bridge shutting down", nil
		},
	}
}
