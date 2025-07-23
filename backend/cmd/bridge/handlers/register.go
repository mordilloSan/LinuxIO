package handlers

import (
	"backend/cmd/bridge/handlers/control"
	"backend/cmd/bridge/handlers/dbus"
	"backend/cmd/bridge/handlers/docker"
	"backend/cmd/bridge/handlers/drive"
	"backend/cmd/bridge/handlers/types"
	"backend/cmd/bridge/handlers/wireguard"
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
