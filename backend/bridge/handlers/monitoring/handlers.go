package monitoring

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type monitoringRegistration struct {
	command string
	handler ipc.HandlerFunc
}

// RegisterHandlers registers PCP-backed monitoring handlers.
func RegisterHandlers() {
	registerMonitoringHandlers([]monitoringRegistration{
		{command: "get_cpu_series", handler: handleGetCPUSeries},
		{command: "get_memory_series", handler: handleGetMemorySeries},
		{command: "get_gpu_series", handler: handleGetGPUSeries},
		{command: "get_network_series", handler: handleGetNetworkSeries},
		{command: "get_disk_io_series", handler: handleGetDiskIOSeries},
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

func handleGetNetworkSeries(ctx context.Context, args []string, emit ipc.Events) error {
	return emit.Result(GetNetworkSeries(ctx, firstArg(args), secondArg(args)))
}

func handleGetDiskIOSeries(ctx context.Context, args []string, emit ipc.Events) error {
	return emit.Result(GetDiskIOSeries(ctx, firstArg(args), secondArg(args)))
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func secondArg(args []string) string {
	if len(args) < 2 {
		return ""
	}
	return args[1]
}
