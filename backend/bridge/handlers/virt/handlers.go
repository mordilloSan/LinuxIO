package virt

import (
	"context"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Routes = routeBindings(runtime.Runtime{}).Routes()

func routeBindings(_ runtime.Runtime) apischema.BindingSet {
	return apischema.Bindings(
		apischema.Query[apischema.NoRequest, []apischema.VirtualMachine]("virt.list", apischema.Privileged()).Handle(handleList),
		apischema.Query[apischema.NameRequest, apischema.VirtualMachine]("virt.get", apischema.Privileged()).Handle(handleGet),
		apischema.Query[apischema.VMPreflightRequest, apischema.VMPreflight]("virt.preflight", apischema.Privileged()).Handle(handlePreflight),
		apischema.Job[apischema.NameRequest, apischema.NoResponse]("virt.start", apischema.Privileged()).Handle(handleStart),
		apischema.Job[apischema.NameRequest, apischema.NoResponse]("virt.shutdown", apischema.Privileged()).Handle(handleShutdown),
		apischema.Job[apischema.NameRequest, apischema.NoResponse]("virt.reboot", apischema.Privileged()).Handle(handleReboot),
		apischema.Job[apischema.NameRequest, apischema.NoResponse]("virt.force_off", apischema.Privileged()).Handle(handleForceOff),
		apischema.Job[apischema.NameRequest, apischema.NoResponse]("virt.suspend", apischema.Privileged()).Handle(handleSuspend),
		apischema.Job[apischema.NameRequest, apischema.NoResponse]("virt.resume", apischema.Privileged()).Handle(handleResume),
		apischema.Job[apischema.VMDeleteRequest, apischema.VMDeleteResult]("virt.delete", apischema.Privileged()).Handle(handleDelete),
		apischema.Job[apischema.VMCreateRequest, apischema.VirtualMachine]("virt.create", apischema.Privileged()).Handle(handleCreate),
		apischema.DuplexRoute[apischema.NameRequest, apischema.NoResponse]("virt.console_open", apischema.Privileged(), apischema.NoEndpoint()).Duplex(
			func(ctx context.Context, stream net.Conn, req apischema.NameRequest) error {
				return HandleConsoleSession(ctx, stream, req)
			},
		),
	)
}

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings(rt).Register(router)
}

func handleList(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := ListVMs(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGet(ctx context.Context, req apischema.NameRequest, emit bridgeipc.Events) error {
	result, err := GetVM(ctx, req.Name)
	return bridgeipc.EmitResult(emit, result, err)
}

func handlePreflight(ctx context.Context, req apischema.VMPreflightRequest, emit bridgeipc.Events) error {
	result, err := Preflight(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleStart(ctx context.Context, req apischema.NameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, apischema.NoResponse{}, StartVM(ctx, req.Name))
}

func handleShutdown(ctx context.Context, req apischema.NameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, apischema.NoResponse{}, ShutdownVM(ctx, req.Name))
}

func handleReboot(ctx context.Context, req apischema.NameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, apischema.NoResponse{}, RebootVM(ctx, req.Name))
}

func handleForceOff(ctx context.Context, req apischema.NameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, apischema.NoResponse{}, ForceOffVM(ctx, req.Name))
}

func handleSuspend(ctx context.Context, req apischema.NameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, apischema.NoResponse{}, SuspendVM(ctx, req.Name))
}

func handleResume(ctx context.Context, req apischema.NameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, apischema.NoResponse{}, ResumeVM(ctx, req.Name))
}

func handleDelete(ctx context.Context, req apischema.VMDeleteRequest, emit bridgeipc.Events) error {
	result, err := DeleteVM(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleCreate(ctx context.Context, req apischema.VMCreateRequest, emit bridgeipc.Events) error {
	report := func(progress apischema.VMCreateProgress) {
		_ = emit.Progress(progress)
	}
	result, err := CreateVMWithProgress(ctx, req, report)
	return bridgeipc.EmitResult(emit, result, err)
}
