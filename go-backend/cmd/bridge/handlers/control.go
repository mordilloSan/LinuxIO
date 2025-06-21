package handlers

import (
	"go-backend/cmd/bridge/handlers/types"
	"go-backend/internal/logger"
)

var ShutdownChan chan string

func ControlHandlers() map[string]types.HandlerFunc {
	return map[string]types.HandlerFunc{
		"shutdown": func(args []string) (any, error) {
			logger.Infof("Received shutdown command, exiting bridge")
			select {
			case ShutdownChan <- "Bridge received shutdown command":
			default:
			}
			return "Bridge shutting down", nil
		},
	}
}
