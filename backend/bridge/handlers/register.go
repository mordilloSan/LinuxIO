package handlers

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/control"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/dbus"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/drive"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/terminal"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/wireguard"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// Map of type -> (command -> handler)
var HandlersByType = map[string]map[string]ipc.HandlerFunc{}

func RegisterAllHandlers(shutdownChan chan string) {
	HandlersByType["dbus"] = dbus.DbusHandlers()
	HandlersByType["drives"] = drive.DriveHandlers()
	HandlersByType["docker"] = docker.DockerHandlers()
	HandlersByType["control"] = control.ControlHandlers(shutdownChan)
	HandlersByType["wireguard"] = wireguard.WireguardHandlers()
	HandlersByType["config"] = config.ThemeHandlers()
	HandlersByType["system"] = system.SystemHandlers()
}

// RegisterTerminalHandlers attaches terminal handlers that require the session context.
func RegisterTerminalHandlers(sess *session.Session) {
	HandlersByType["terminal"] = terminal.TerminalHandlers(sess)
}
