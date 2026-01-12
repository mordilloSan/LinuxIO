package logs

import (
	"net"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// RegisterStreamHandlers registers all logs stream handlers.
func RegisterStreamHandlers(handlers map[string]func(*session.Session, net.Conn, []string) error) {
	handlers[StreamTypeGeneralLogs] = HandleGeneralLogsStream
	handlers[StreamTypeServiceLogs] = HandleServiceLogsStream
}

// sendStreamClose sends a stream close frame
func sendStreamClose(stream net.Conn) {
	frame := &ipc.StreamFrame{
		Opcode:   ipc.OpStreamClose,
		StreamID: 1,
	}
	_ = ipc.WriteRelayFrame(stream, frame)
}
