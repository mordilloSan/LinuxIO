package terminal

import (
	"context"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers all terminal handlers with the global registry
func RegisterHandlers(rt runtime.Runtime) {
	rpc.Register("terminal", rt, []rpc.Command{
		{Name: "list_shells", Handler: handleListShells},
	})
}

// RegisterStreamHandlers registers all terminal stream handlers.
func RegisterStreamHandlers(handlers map[string]func(runtime.Runtime, net.Conn, []string) error) {
	handlers["terminal"] = HandleTerminalStream
	handlers["container"] = HandleContainerTerminalStream
}

func handleListShells(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		return emit.Result([]string{})
	}
	shells, err := ListContainerShells(args[0])
	return rpc.EmitResult(emit, shells, err)
}
