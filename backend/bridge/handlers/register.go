package handlers

import (
	"github.com/mordilloSan/LinuxIO/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/bridge/handlers/control"
	"github.com/mordilloSan/LinuxIO/bridge/handlers/dbus"
	"github.com/mordilloSan/LinuxIO/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/bridge/handlers/drive"
	"github.com/mordilloSan/LinuxIO/bridge/handlers/wireguard"
	"github.com/mordilloSan/LinuxIO/common/ipc"
)

// Map of type -> (command -> handler)
var HandlersByType = map[string]map[string]ipc.HandlerFunc{}

func RegisterAllHandlers(shutdownChan chan string) {
	HandlersByType["dbus"] = dbus.DbusHandlers()
	HandlersByType["system"] = drive.DriveHandlers()
	HandlersByType["docker"] = docker.DockerHandlers()
	HandlersByType["control"] = control.ControlHandlers(shutdownChan)
	HandlersByType["wireguard"] = wireguard.WireguardHandlers()
	HandlersByType["config"] = config.ThemeHandlers()
}
