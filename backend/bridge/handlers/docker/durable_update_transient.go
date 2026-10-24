package docker

import (
	"context"
	"time"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/transientunit"
	"github.com/mordilloSan/LinuxIO/backend/common/dockerupdate"
)

type systemdDockerUpdateExecutor struct{}

func (systemdDockerUpdateExecutor) Start(ctx context.Context, launch dockerUpdateLaunch) error {
	return transientunit.Start(ctx, launch.Unit, buildDockerUpdateUnitProperties(launch), transientunit.Options{
		Subsystem: "docker-update",
		NoRetry:   true,
	})
}

func (systemdDockerUpdateExecutor) Inspect(ctx context.Context, unitName, expectedDescription string) (transientunit.State, error) {
	return transientunit.Inspect(ctx, unitName, expectedDescription, transientunit.Options{Subsystem: "docker-update"})
}

func (systemdDockerUpdateExecutor) Stop(ctx context.Context, unitName string) error {
	return transientunit.Stop(ctx, unitName)
}

func (systemdDockerUpdateExecutor) Collect(ctx context.Context, unitName string) {
	transientunit.Collect(ctx, unitName, transientunit.Options{Subsystem: "docker-update"})
}

// buildDockerUpdateUnitProperties derives the transient unit from
// dockerupdate.WorkerUnit, the same definition the scheduled service unit is
// rendered from.
func buildDockerUpdateUnitProperties(launch dockerUpdateLaunch) []transientunit.Property {
	unit := dockerupdate.WorkerUnit
	return []transientunit.Property{
		{Name: "Description", Value: godbus.MakeVariant(launch.Description)},
		{Name: "Type", Value: godbus.MakeVariant("exec")},
		{Name: "ExecStart", Value: godbus.MakeVariant([]transientunit.ExecCommand{{
			Path:      dockerupdate.RunnerPath,
			Arguments: append([]string{dockerupdate.RunnerPath}, dockerupdate.DurableArgs(launch.OperationID)...),
		}})},
		{Name: "User", Value: godbus.MakeVariant("root")},
		{Name: "Group", Value: godbus.MakeVariant("root")},
		{Name: "Requires", Value: godbus.MakeVariant(unit.Requires)},
		{Name: "After", Value: godbus.MakeVariant(unit.After)},
		{Name: "Wants", Value: godbus.MakeVariant(unit.Wants)},
		{Name: "ProtectSystem", Value: godbus.MakeVariant(unit.ProtectSystem)},
		{Name: "ProtectHome", Value: godbus.MakeVariant(unit.ProtectHome)},
		{Name: "ReadWritePaths", Value: godbus.MakeVariant(unit.ReadWritePaths)},
		{Name: "PrivateTmp", Value: godbus.MakeVariant(unit.PrivateTmp)},
		{Name: "NoNewPrivileges", Value: godbus.MakeVariant(unit.NoNewPrivileges)},
		{Name: "RuntimeMaxUSec", Value: godbus.MakeVariant(uint64(unit.RuntimeLimit / time.Microsecond))},
		{Name: "TimeoutStopUSec", Value: godbus.MakeVariant(uint64(unit.StopTimeout / time.Microsecond))},
		{Name: "StandardOutput", Value: godbus.MakeVariant("journal")},
		{Name: "StandardError", Value: godbus.MakeVariant("journal")},
		{Name: "SyslogIdentifier", Value: godbus.MakeVariant(unit.SyslogIdentifier)},
	}
}
