package handlers

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/accounts"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/appupdate"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/control"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/datetime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/hostname"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
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
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func RegisterAllHandlers(rt runtime.Runtime) *bridgeipc.Router {
	router := bridgeipc.NewRouter(bridgeipc.DefaultRegistry)

	appupdate.RegisterHandlers(rt, router)
	system.RegisterHandlers(rt, router)
	accounts.RegisterHandlers(rt, router)
	docker.RegisterHandlers(rt, router)
	filebrowser.RegisterHandlers(rt, router)
	indexer.RegisterHandlers(rt, router)
	config.RegisterHandlers(rt, router)
	control.RegisterHandlers(rt, router)
	power.RegisterHandlers(rt, router)
	systemd.RegisterHandlers(rt, router)
	hostname.RegisterHandlers(rt, router)
	datetime.RegisterHandlers(rt, router)
	network.RegisterHandlers(rt, router)
	updates.RegisterHandlers(rt, router)
	terminal.RegisterHandlers(rt, router)
	wireguard.RegisterHandlers(rt, router)
	storage.RegisterHandlers(rt, router)
	shares.RegisterHandlers(rt, router)
	logs.RegisterHandlers(rt, router)

	return router
}
