package control

import (
	"github.com/mordilloSan/LinuxIO/common/ipc"
	"github.com/mordilloSan/LinuxIO/common/logger"
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
		"ping": func(_ []string) (any, error) { //nolint:unused
			return map[string]string{"type": "pong"}, nil
		},
	}
}
