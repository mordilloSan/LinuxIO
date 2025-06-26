package handlers

import (
	"go-backend/cmd/bridge/handlers/dbus"
	"go-backend/cmd/bridge/handlers/docker"
	"go-backend/cmd/bridge/handlers/system"
	"go-backend/cmd/bridge/handlers/types"
	"go-backend/cmd/bridge/handlers/wireguard"
)

// Map of type -> (command -> handler)
var HandlersByType = map[string]map[string]types.HandlerFunc{}

func RegisterAllHandlers(shutdownChan chan string) {
	ShutdownChan = shutdownChan
	HandlersByType["dbus"] = dbus.DbusHandlers()
	HandlersByType["system"] = system.SystemHandlers()
	HandlersByType["docker"] = docker.DockerHandlers()
	HandlersByType["control"] = ControlHandlers()
	HandlersByType["wireguard"] = wireguard.WireguardHandlers()
}
