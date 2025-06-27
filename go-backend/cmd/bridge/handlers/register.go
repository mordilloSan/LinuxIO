package handlers

import (
	"go-backend/cmd/bridge/handlers/control"
	"go-backend/cmd/bridge/handlers/dbus"
	"go-backend/cmd/bridge/handlers/docker"
	"go-backend/cmd/bridge/handlers/drive"
	"go-backend/cmd/bridge/handlers/types"
	"go-backend/cmd/bridge/handlers/wireguard"
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
