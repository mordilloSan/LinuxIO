package control

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers control handlers with the new handler system
func RegisterHandlers(_ runtime.Runtime) {
	ipc.RegisterFunc("control", "version", func(ctx context.Context, args []string, emit ipc.Events) error {
		info, err := getVersionInfo()
		if err != nil {
			return err
		}
		return emit.Result(info)
	})
}
