package system

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type systemRegistration struct {
	command string
	handler ipc.HandlerFunc
}

// RegisterHandlers registers all system handlers with the global registry
func RegisterHandlers() {
	onceSampler.Do(func() {
		go runSimpleNetInfoSampler()
	})
	onceDiskSampler.Do(func() {
		go runDiskThroughputSampler()
	})

	registerCapabilitiesHandlers()
	registerSystemHandlers([]systemRegistration{
		{command: "get_cpu_info", handler: handleGetCPUInfo},
		{command: "get_sensor_info", handler: handleGetSensorInfo},
		{command: "get_motherboard_info", handler: handleGetMotherboardInfo},
		{command: "get_memory_info", handler: handleGetMemoryInfo},
		{command: "get_host_info", handler: handleGetHostInfo},
		{command: "get_uptime", handler: handleGetUptime},
		{command: "get_fs_info", handler: handleGetFilesystemInfo},
		{command: "get_processes", handler: handleGetProcesses},
		{command: "get_services", handler: handleGetServices},
		{command: "get_gpu_info", handler: handleGetGPUInfo},
		{command: "get_updates_fast", handler: handleGetUpdatesFast},
		{command: "get_network_info", handler: handleGetNetworkInfo},
		{command: "get_disk_throughput", handler: handleGetDiskThroughput},
		{command: "get_system_info", handler: handleGetSystemInfo},
		{command: "get_pci_devices", handler: handleGetPCIDevices},
		{command: "get_memory_modules", handler: handleGetMemoryModules},
	})
}

func registerSystemHandlers(registrations []systemRegistration) {
	for _, registration := range registrations {
		ipc.RegisterFunc("system", registration.command, registration.handler)
	}
}

func handleGetCPUInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchCPUInfo)
}

func handleGetSensorInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emit.Result(FetchSensorsInfo())
}

func handleGetMotherboardInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchBaseboardInfo)
}

func handleGetMemoryInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchMemoryInfo)
}

func handleGetHostInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchHostInfo)
}

func handleGetUptime(ctx context.Context, args []string, emit ipc.Events) error {
	uptimeSeconds, err := FetchUptimeSeconds()
	if err != nil {
		return err
	}
	return emit.Result(uptimeSeconds)
}

func handleGetFilesystemInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemArgCall(emit, parseIncludeAllArg(args), FetchFileSystemInfo)
}

func handleGetProcesses(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchProcesses)
}

func handleGetServices(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchServices)
}

func handleGetGPUInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchGPUInfo)
}

func handleGetUpdatesFast(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, GetUpdatesFast)
}

func handleGetNetworkInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchNetworks)
}

func handleGetDiskThroughput(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchDiskThroughput)
}

func handleGetSystemInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchSystemInfo)
}

func handleGetPCIDevices(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchPCIDevices)
}

func handleGetMemoryModules(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchMemoryModules)
}

func parseIncludeAllArg(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func emitSystemResult(emit ipc.Events, result any, err error) error {
	if err != nil {
		return err
	}
	return emit.Result(result)
}

func emitSystemCall[T any](emit ipc.Events, fn func() (T, error)) error {
	result, err := fn()
	return emitSystemResult(emit, result, err)
}

func emitSystemArgCall[A any, T any](emit ipc.Events, arg A, fn func(A) (T, error)) error {
	result, err := fn(arg)
	return emitSystemResult(emit, result, err)
}
