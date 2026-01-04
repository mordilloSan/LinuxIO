package handlers

import (
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/control"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/dbus"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/drive"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/generic"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/modules"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/terminal"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/wireguard"
	"github.com/mordilloSan/LinuxIO/backend/common/middleware"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// JsonHandlers are functions that return JSON-serializable data.
// Usage: JsonHandlers[handlerType][command](args)
var JsonHandlers = map[string]map[string]func([]string) (any, error){}

// StreamHandlers is the registry for yamux stream handlers.
// Usage: StreamHandlers[streamType](sess, conn, args)
var StreamHandlers = map[string]func(*session.Session, net.Conn, []string) error{}

func RegisterAllHandlers(shutdownChan chan string, sess *session.Session) {
	// Register JSON handlers
	// Frontend calls linuxio.useCall() -> opens "json" stream -> HandleJSONStream
	JsonHandlers["dbus"] = dbus.DbusHandlers()
	JsonHandlers["drives"] = drive.DriveHandlers()
	JsonHandlers["docker"] = docker.DockerHandlers()
	JsonHandlers["control"] = control.ControlHandlers(shutdownChan)
	JsonHandlers["config"] = config.ThemeHandlers(sess)
	JsonHandlers["system"] = system.SystemHandlers()
	JsonHandlers["filebrowser"] = filebrowser.FilebrowserHandlers()
	JsonHandlers["terminal"] = terminal.TerminalHandlers(sess)
	JsonHandlers["wireguard"] = middleware.RequirePrivilegedAll(sess, wireguard.WireguardHandlers()) //require administrator privileges
	// Register Stream handlers
	// Frontend calls linuxio.useStream() -> opens specific stream type -> handler below
	terminal.RegisterStreamHandlers(StreamHandlers)
	filebrowser.RegisterStreamHandlers(StreamHandlers)
	dbus.RegisterStreamHandlers(StreamHandlers)
	//Provides JSON commands: GetModules, InstallModule, UninstallModule, GetModuleDetails, ValidateModule
	JsonHandlers["modules"] = modules.ModuleHandlers(sess, JsonHandlers, StreamHandlers)
	// Modules can register into both JsonHandlers and StreamHandlers
	generic.RegisterStreamHandlers(StreamHandlers, JsonHandlers)
	// Load modules from YAML files - log errors but don't fail
	_ = modules.LoadModules(JsonHandlers, StreamHandlers)
}
