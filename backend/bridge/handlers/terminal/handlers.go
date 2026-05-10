package terminal

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers all terminal handlers with the global registry
func RegisterHandlers(_ runtime.Runtime) {
	ipc.RegisterFunc("terminal", "list_shells", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return emit.Result([]string{})
		}
		shells, err := ListContainerShells(args[0])
		if err != nil {
			return err
		}
		return emit.Result(shells)
	})
}
