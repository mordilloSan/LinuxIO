package handlers

import (
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/control"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/dbus"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/generic"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/modules"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/terminal"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/wireguard"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// JsonHandlers are functions that return JSON-serializable data.
// Used by dynamic module system for YAML-defined handlers.
var JsonHandlers = map[string]map[string]func([]string) (any, error){}

// StreamHandlers is the registry for yamux stream handlers.
// Used for the bridge protocol and dynamic module streams.
var StreamHandlers = map[string]func(*session.Session, net.Conn, []string) error{}

func RegisterAllHandlers(shutdownChan chan string, sess *session.Session) {
	// Register the universal bridge stream handler
	// Frontend calls linuxio.call("system", "get_drive_info") -> opens "bridge" stream
	StreamHandlers["bridge"] = func(s *session.Session, conn net.Conn, args []string) error {
		return generic.HandleBridgeStream(s, conn, args)
	}

	// Register all handlers using the handler.Register() system
	system.RegisterHandlers()
	docker.RegisterHandlers()
	filebrowser.RegisterHandlers()
	config.RegisterHandlers(sess)
	control.RegisterHandlers(shutdownChan)
	dbus.RegisterHandlers()
	terminal.RegisterHandlers(sess)
	wireguard.RegisterHandlers()
	modules.RegisterHandlers(sess, StreamHandlers)

	// Register stream handlers for yamux streams (terminal, filebrowser, etc.)
	generic.RegisterStreamHandlers(StreamHandlers, JsonHandlers)
	terminal.RegisterStreamHandlers(StreamHandlers)
	filebrowser.RegisterStreamHandlers(StreamHandlers)
	dbus.RegisterStreamHandlers(StreamHandlers)
	docker.RegisterStreamHandlers(StreamHandlers)

	// Load modules from YAML files - log errors but don't fail
	_ = modules.LoadModules(StreamHandlers)
}
