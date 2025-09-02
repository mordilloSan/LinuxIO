package control

import (
	"github.com/mordilloSan/LinuxIO/internal/ipc"
	"github.com/mordilloSan/LinuxIO/internal/logger"
)

func ControlHandlers(shutdownChan chan string) map[string]ipc.HandlerFunc {
	return map[string]ipc.HandlerFunc{
		"shutdown": func(args []string) (any, error) {
			reason := "unknown"
			if len(args) > 0 {
				reason = args[0] // "logout" or "forced"
			}
			logger.Debugf("Received shutdown command: %s", reason)
			select {
			case shutdownChan <- reason:
			default:
			}
			return "Bridge shutting down", nil
		},
	}
}
