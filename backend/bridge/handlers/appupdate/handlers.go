package appupdate

import (
	"context"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers control handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime) {
	rpc.Register("control", rt, []rpc.Command{
		{Name: "version", Handler: handleVersion},
	})
}

// RegisterStreamHandlers registers the app-update stream handler.
func RegisterStreamHandlers(handlers map[string]func(runtime.Runtime, net.Conn, []string) error) {
	handlers[streamTypeAppUpdate] = HandleAppUpdateStream
}

func handleVersion(ctx context.Context, args []string, emit ipc.Events) error {
	info, err := getVersionInfo()
	return rpc.EmitResult(emit, info, err)
}
