package handlers

import (
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/accounts"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/appupdate"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/control"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/datetime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/hostname"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	jobhandlers "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/jobs"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/logs"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/network"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/power"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/shares"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/storage"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/terminal"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/updates"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/wireguard"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/generic"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
)

// streamHandlers is the registry for yamux stream handlers.
// Populated during RegisterAllHandlers; read-only after that.
var streamHandlers = map[string]func(runtime.Runtime, net.Conn, []string) error{}

// GetStreamHandler returns the handler for the given stream type.
func GetStreamHandler(streamType string) (func(runtime.Runtime, net.Conn, []string) error, bool) {
	h, ok := streamHandlers[streamType]
	return h, ok
}

func RegisterAllHandlers(rt runtime.Runtime) {
	// Register the universal RPC stream handler.
	// Typed frontend calls like linuxio.storage.get_drive_info.call()
	// open a "bridge" stream and dispatch through ipc.RegisterFunc handlers.
	streamHandlers["bridge"] = func(rt runtime.Runtime, conn net.Conn, args []string) error {
		return generic.HandleBridgeStream(rt.Session, conn, args)
	}

	// Register all handlers using the handler.Register() system
	appupdate.RegisterHandlers(rt)
	system.RegisterHandlers(rt)
	accounts.RegisterHandlers(rt)
	docker.RegisterHandlers(rt)
	filebrowser.RegisterHandlers(rt)
	indexer.RegisterHandlers(rt)
	jobhandlers.RegisterHandlers(rt)
	config.RegisterHandlers(rt)
	control.RegisterHandlers(rt)
	power.RegisterHandlers(rt)
	systemd.RegisterHandlers(rt)
	hostname.RegisterHandlers(rt)
	datetime.RegisterHandlers(rt)
	network.RegisterHandlers(rt)
	updates.RegisterHandlers(rt)
	terminal.RegisterHandlers(rt)
	wireguard.RegisterHandlers(rt)
	storage.RegisterHandlers(rt)
	shares.RegisterHandlers(rt)

	// Register stream handlers for yamux streams (terminal, jobs, logs, etc.)
	appupdate.RegisterStreamHandlers(streamHandlers)
	terminal.RegisterStreamHandlers(streamHandlers)
	jobhandlers.RegisterStreamHandlers(streamHandlers)
	logs.RegisterStreamHandlers(streamHandlers)
	docker.RegisterStreamHandlers(streamHandlers)
}
