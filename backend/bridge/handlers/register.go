package handlers

import (
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/accounts"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/control"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/dbus"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/generic"
	jobhandlers "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/jobs"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/logs"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/power"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/shares"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/storage"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/terminal"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/wireguard"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// streamHandlers is the registry for yamux stream handlers.
// Populated during RegisterAllHandlers; read-only after that.
var streamHandlers = map[string]func(*session.Session, net.Conn, []string) error{}

// GetStreamHandler returns the handler for the given stream type.
func GetStreamHandler(streamType string) (func(*session.Session, net.Conn, []string) error, bool) {
	h, ok := streamHandlers[streamType]
	return h, ok
}

func RegisterAllHandlers(sess *session.Session) {
	// Register the universal RPC stream handler.
	// Typed frontend calls like linuxio.storage.get_drive_info.call()
	// open a "bridge" stream and dispatch through ipc.RegisterFunc handlers.
	streamHandlers["bridge"] = func(s *session.Session, conn net.Conn, args []string) error {
		return generic.HandleBridgeStream(s, conn, args)
	}

	// Register all handlers using the handler.Register() system
	system.RegisterHandlers(sess)
	accounts.RegisterHandlers()
	docker.RegisterHandlers(sess)
	filebrowser.RegisterHandlers()
	jobhandlers.RegisterHandlers()
	config.RegisterHandlers(sess)
	control.RegisterHandlers()
	power.RegisterHandlers(sess)
	dbus.RegisterHandlers()
	terminal.RegisterHandlers(sess)
	wireguard.RegisterHandlers()
	storage.RegisterHandlers()
	shares.RegisterHandlers()

	// Register stream handlers for yamux streams (terminal, jobs, logs, etc.)
	control.RegisterStreamHandlers(streamHandlers)
	terminal.RegisterStreamHandlers(streamHandlers)
	jobhandlers.RegisterStreamHandlers(streamHandlers)
	logs.RegisterStreamHandlers(streamHandlers)
}
