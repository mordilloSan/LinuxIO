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
		apischema.Call[apischema.NoRequest, []apischema.VirtualMachine]("virt.list", apischema.RetrySafe(), apischema.Privileged()).Handle(handleList),
		apischema.Call[apischema.NameRequest, apischema.VirtualMachine]("virt.get", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGet),
		apischema.Call[apischema.VMPreflightRequest, apischema.VMPreflight]("virt.preflight", apischema.RetrySafe(), apischema.Privileged()).Handle(handlePreflight),
		apischema.Call[apischema.NameRequest, apischema.NoResponse]("virt.start", apischema.Privileged()).HandleVoid(handleStart),
		apischema.Call[apischema.NameRequest, apischema.NoResponse]("virt.shutdown", apischema.Privileged()).HandleVoid(handleShutdown),
		apischema.Call[apischema.NameRequest, apischema.NoResponse]("virt.reboot", apischema.Privileged()).HandleVoid(handleReboot),
		apischema.Call[apischema.NameRequest, apischema.NoResponse]("virt.force_off", apischema.Privileged()).HandleVoid(handleForceOff),
		apischema.Call[apischema.NameRequest, apischema.NoResponse]("virt.suspend", apischema.Privileged()).HandleVoid(handleSuspend),
		apischema.Call[apischema.NameRequest, apischema.NoResponse]("virt.resume", apischema.Privileged()).HandleVoid(handleResume),
		apischema.Call[apischema.VMDeleteRequest, apischema.VMDeleteResult]("virt.delete", apischema.Privileged()).Handle(handleDelete),
		apischema.TaskRunner[apischema.VMCreateRequest, apischema.VirtualMachine]("virt.create", apischema.Privileged(), apischema.SessionTask(), apischema.WithTaskProgress[apischema.VMCreateProgress]()).Run(handleCreate, bridgeipc.TaskDefault),
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

func handleCreate(ctx context.Context, task *bridgeipc.Task, req apischema.VMCreateRequest) (apischema.VirtualMachine, error) {
	report := func(progress apischema.VMCreateProgress) {
		task.ReportProgress(progress)
	}
	return CreateVMWithProgress(ctx, req, report)
}
