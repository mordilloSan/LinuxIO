package monitoring

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type monitoringRegistration struct {
	command string
	handler ipc.HandlerFunc
}

// RegisterHandlers registers Prometheus-backed monitoring handlers.
func RegisterHandlers() {
	registerMonitoringHandlers([]monitoringRegistration{
		{command: "get_cpu_series", handler: handleGetCPUSeries},
		{command: "get_memory_series", handler: handleGetMemorySeries},
		{command: "get_gpu_series", handler: handleGetGPUSeries},
	})
}

func registerMonitoringHandlers(registrations []monitoringRegistration) {
	for _, registration := range registrations {
		ipc.RegisterFunc("monitoring", registration.command, registration.handler)
	}
}

func handleGetCPUSeries(ctx context.Context, args []string, emit ipc.Events) error {
	return emit.Result(GetCPUSeries(ctx, firstArg(args)))
}

func handleGetMemorySeries(ctx context.Context, args []string, emit ipc.Events) error {
	return emit.Result(GetMemorySeries(ctx, firstArg(args)))
}

func handleGetGPUSeries(ctx context.Context, args []string, emit ipc.Events) error {
	return emit.Result(GetGPUSeries(ctx, firstArg(args)))
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
