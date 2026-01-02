package handlers

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/control"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/dbus"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/drive"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/generic"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/terminal"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/wireguard"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// HandlersByType is the registry for bridge request handlers.
// Usage: HandlersByType[handlerType][command](args)
var HandlersByType = map[string]map[string]func([]string) (any, error){}

func RegisterAllHandlers(shutdownChan chan string, sess *session.Session) {
	HandlersByType["dbus"] = dbus.DbusHandlers()
	HandlersByType["drives"] = drive.DriveHandlers()
	HandlersByType["docker"] = docker.DockerHandlers()
	HandlersByType["control"] = control.ControlHandlers(shutdownChan)
	HandlersByType["wireguard"] = wireguard.WireguardHandlers()
	HandlersByType["config"] = config.ThemeHandlers(sess)
	HandlersByType["system"] = system.SystemHandlers()
	HandlersByType["filebrowser"] = filebrowser.FilebrowserHandlers()
	HandlersByType["terminal"] = terminal.TerminalHandlers(sess)

	// Generic handlers for modules
	HandlersByType["command"] = generic.CommandHandlers()
	HandlersByType["generic_dbus"] = generic.DbusHandlers()
}
