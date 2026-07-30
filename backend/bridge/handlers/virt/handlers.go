package virt

import (
	"context"

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
		// These previously emitted apischema.NoResponse{}, so their job snapshots
		// carried "result":{} while the other 59 NoResponse routes omitted the key
		// entirely. HandleVoid emits nil, matching them and the generated `void`.
		apischema.Job[apischema.NameRequest, apischema.NoResponse]("virt.start", apischema.Privileged()).HandleVoid(handleStart),
		apischema.Job[apischema.NameRequest, apischema.NoResponse]("virt.shutdown", apischema.Privileged()).HandleVoid(handleShutdown),
		apischema.Job[apischema.NameRequest, apischema.NoResponse]("virt.reboot", apischema.Privileged()).HandleVoid(handleReboot),
		apischema.Job[apischema.NameRequest, apischema.NoResponse]("virt.force_off", apischema.Privileged()).HandleVoid(handleForceOff),
		apischema.Job[apischema.NameRequest, apischema.NoResponse]("virt.suspend", apischema.Privileged()).HandleVoid(handleSuspend),
		apischema.Job[apischema.NameRequest, apischema.NoResponse]("virt.resume", apischema.Privileged()).HandleVoid(handleResume),
		apischema.Job[apischema.VMDeleteRequest, apischema.VMDeleteResult]("virt.delete", apischema.Privileged()).Handle(handleDelete),
		apischema.Job[apischema.VMCreateRequest, apischema.VirtualMachine]("virt.create", apischema.Privileged()).HandleEvents(handleCreate),
		apischema.DuplexRoute[apischema.NameRequest, apischema.NoResponse]("virt.console_open", apischema.Privileged(), apischema.NoEndpoint()).Duplex(
			HandleConsoleSession,
		),
	)
}

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings(rt).Register(router)
}

func handleList(ctx context.Context, _ apischema.NoRequest) ([]apischema.VirtualMachine, error) {
	return ListVMs(ctx)
}

func handleGet(ctx context.Context, req apischema.NameRequest) (apischema.VirtualMachine, error) {
	return GetVM(ctx, req.Name)
}

func handlePreflight(ctx context.Context, req apischema.VMPreflightRequest) (apischema.VMPreflight, error) {
	return Preflight(ctx, req)
}

func handleStart(ctx context.Context, req apischema.NameRequest) error {
	return StartVM(ctx, req.Name)
}

func handleShutdown(ctx context.Context, req apischema.NameRequest) error {
	return ShutdownVM(ctx, req.Name)
}

func handleReboot(ctx context.Context, req apischema.NameRequest) error {
	return RebootVM(ctx, req.Name)
}

func handleForceOff(ctx context.Context, req apischema.NameRequest) error {
	return ForceOffVM(ctx, req.Name)
}

func handleSuspend(ctx context.Context, req apischema.NameRequest) error {
	return SuspendVM(ctx, req.Name)
}

func handleResume(ctx context.Context, req apischema.NameRequest) error {
	return ResumeVM(ctx, req.Name)
}

func handleDelete(ctx context.Context, req apischema.VMDeleteRequest) (apischema.VMDeleteResult, error) {
	return DeleteVM(ctx, req)
}

func handleCreate(ctx context.Context, req apischema.VMCreateRequest, emit bridgeipc.Events) error {
	report := func(progress apischema.VMCreateProgress) {
		_ = emit.Progress(progress)
	}
	result, err := CreateVMWithProgress(ctx, req, report)
	return bridgeipc.EmitResult(emit, result, err)
}
