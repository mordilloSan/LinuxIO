package docker

import (
	"context"
	"time"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/transientunit"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
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

func buildDockerUpdateUnitProperties(launch dockerUpdateLaunch) []transientunit.Property {
	return []transientunit.Property{
		{Name: "Description", Value: godbus.MakeVariant(launch.Description)},
		{Name: "Type", Value: godbus.MakeVariant("exec")},
		{Name: "ExecStart", Value: godbus.MakeVariant([]transientunit.ExecCommand{{
			Path:      dockerUpdateRunnerPath,
			Arguments: []string{dockerUpdateRunnerPath, "run-operation", "--id", launch.OperationID},
		}})},
		{Name: "User", Value: godbus.MakeVariant("root")},
		{Name: "Group", Value: godbus.MakeVariant("root")},
		{Name: "Requires", Value: godbus.MakeVariant([]string{"docker.service"})},
		{Name: "After", Value: godbus.MakeVariant([]string{"docker.service", "network-online.target"})},
		{Name: "Wants", Value: godbus.MakeVariant([]string{"network-online.target"})},
		{Name: "ProtectSystem", Value: godbus.MakeVariant("strict")},
		{Name: "ProtectHome", Value: godbus.MakeVariant("read-only")},
		{Name: "ReadWritePaths", Value: godbus.MakeVariant([]string{version.DataDir, "/run"})},
		{Name: "PrivateTmp", Value: godbus.MakeVariant(true)},
		{Name: "NoNewPrivileges", Value: godbus.MakeVariant(true)},
		{Name: "RuntimeMaxUSec", Value: godbus.MakeVariant(uint64(dockerUpdateRuntimeLimit / time.Microsecond))},
		{Name: "TimeoutStopUSec", Value: godbus.MakeVariant(uint64(dockerUpdateStopTimeout / time.Microsecond))},
		{Name: "StandardOutput", Value: godbus.MakeVariant("journal")},
		{Name: "StandardError", Value: godbus.MakeVariant("journal")},
		{Name: "SyslogIdentifier", Value: godbus.MakeVariant("linuxio-docker-update")},
	}
}
