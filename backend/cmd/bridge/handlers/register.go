package handlers

import (
	"github.com/mordilloSan/LinuxIO/backend/cmd/bridge/handlers/control"
	"github.com/mordilloSan/LinuxIO/backend/cmd/bridge/handlers/dbus"
	"github.com/mordilloSan/LinuxIO/backend/cmd/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/cmd/bridge/handlers/drive"
	"github.com/mordilloSan/LinuxIO/backend/cmd/bridge/handlers/types"
	"github.com/mordilloSan/LinuxIO/backend/cmd/bridge/handlers/wireguard"
)

// Map of type -> (command -> handler)
var HandlersByType = map[string]map[string]types.HandlerFunc{}

func RegisterAllHandlers(shutdownChan chan string) {
	HandlersByType["dbus"] = dbus.DbusHandlers()
	HandlersByType["system"] = drive.DriveHandlers()
	HandlersByType["docker"] = docker.DockerHandlers()
	HandlersByType["control"] = control.ControlHandlers(shutdownChan)
	HandlersByType["wireguard"] = wireguard.WireguardHandlers()
}
