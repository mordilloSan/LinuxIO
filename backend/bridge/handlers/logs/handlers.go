package logs

import (
	"net"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// RegisterStreamHandlers registers all logs stream handlers.
func RegisterStreamHandlers(handlers map[string]func(*session.Session, net.Conn, []string) error) {
	handlers[StreamTypeGeneralLogs] = HandleGeneralLogsStream
	handlers[StreamTypeServiceLogs] = HandleServiceLogsStream
}
