package appupdate

import (
	"context"
	"fmt"
	"strings"
	"time"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/transientunit"
)

const (
	updaterRuntimeLimit = 10 * time.Minute
	updaterStopTimeout  = 30 * time.Second
)

var errUpdaterUnitNotFound = transientunit.ErrNotFound

type transientUnitProperty = transientunit.Property
type transientExecCommand = transientunit.ExecCommand

type updaterLaunch struct {
	OperationID   string
	UID           uint32
	Unit          string
	Description   string
	ScriptPath    string
	ResultPath    string
	InstallerArgs []string
	RestartAfter  bool
}

type updaterUnitState = transientunit.State

type updaterExecutor interface {
	Start(context.Context, updaterLaunch) error
	Inspect(context.Context, string, string) (updaterUnitState, error)
	Stop(context.Context, string) error
	Collect(context.Context, string)
}

type systemdUpdaterExecutor struct{}

// updaterUnitRunner is fixed code owned by LinuxIO. All request-derived values
// are passed as positional arguments, never interpolated into the shell text.
// The external unit writes the typed result after installation and any service
// restart, so a replacement webserver or bridge can reconcile completion
// independently of the bridge process that accepted the request.
const updaterUnitRunner = `set -u
script_path=$1
result_path=$2
restart_after=$3
operation_id=$4
shift 4

set +e
/bin/bash "$script_path" "$@"
exit_code=$?
if [ "$exit_code" -eq 0 ] && [ "$restart_after" = 1 ]; then
  systemctl daemon-reload
  systemctl_status=$?
  if [ "$systemctl_status" -eq 0 ]; then
    sleep 0.5
    systemctl restart linuxio.service || systemctl restart linuxio.target
    systemctl_status=$?
  fi
  if [ "$systemctl_status" -ne 0 ]; then
    exit_code=$systemctl_status
  fi
fi
set -e

state=completed
error_message=
if [ "$exit_code" -ne 0 ]; then
  state=failed
  error_message="updater exited with status $exit_code"
fi
finished_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
result_dir=$(dirname -- "$result_path")
tmp=$(mktemp "$result_dir/.executor-result-XXXXXX")
trap 'rm -f -- "$tmp"' EXIT
printf '{"id":"%s","state":"%s","exit_code":%d,"finished_at":"%s","error":"%s"}\n' \
  "$operation_id" "$state" "$exit_code" "$finished_at" "$error_message" >"$tmp"
chmod 0600 "$tmp"
sync -d "$tmp"
mv -f -- "$tmp" "$result_path"
sync -f "$result_dir"
trap - EXIT
exit "$exit_code"`

func appUpdateUnitName(operationID string) string {
	return "linuxio-app-update-" + strings.ReplaceAll(operationID, "-", "") + ".service"
}

func appUpdateUnitDescription(operationID string, uid uint32) string {
	return fmt.Sprintf("LinuxIO app update %s owned by UID %d", operationID, uid)
}

func (systemdUpdaterExecutor) Start(ctx context.Context, launch updaterLaunch) error {
	properties := buildUpdaterUnitProperties(launch)
	err := transientunit.Start(ctx, launch.Unit, properties, transientunit.Options{Subsystem: "app-update", NoRetry: true})
	if err != nil {
		return err
	}
	return nil
}

func (systemdUpdaterExecutor) Inspect(ctx context.Context, unitName, expectedDescription string) (updaterUnitState, error) {
	state, err := transientunit.Inspect(ctx, unitName, expectedDescription, transientunit.Options{Subsystem: "app-update"})
	if err != nil {
		return updaterUnitState{}, err
	}
	return state, nil
}

func (systemdUpdaterExecutor) Stop(ctx context.Context, unitName string) error {
	return transientunit.Stop(ctx, unitName)
}

func (systemdUpdaterExecutor) Collect(ctx context.Context, unitName string) {
	transientunit.Collect(ctx, unitName, transientunit.Options{Subsystem: "app-update"})
}

func buildUpdaterUnitProperties(launch updaterLaunch) []transientUnitProperty {
	restart := "0"
	if launch.RestartAfter {
		restart = "1"
	}
	arguments := []string{
		"/bin/bash",
		"-c",
		updaterUnitRunner,
		"linuxio-app-update",
		launch.ScriptPath,
		launch.ResultPath,
		restart,
		launch.OperationID,
	}
	arguments = append(arguments, launch.InstallerArgs...)

	return []transientUnitProperty{
		{Name: "Description", Value: godbus.MakeVariant(launch.Description)},
		{Name: "Type", Value: godbus.MakeVariant("exec")},
		{Name: "ExecStart", Value: godbus.MakeVariant([]transientExecCommand{{Path: "/bin/bash", Arguments: arguments}})},
		{Name: "User", Value: godbus.MakeVariant("root")},
		{Name: "Group", Value: godbus.MakeVariant("root")},
		{Name: "Environment", Value: godbus.MakeVariant([]string{"TERM=dumb", "NO_COLOR=1", "CLICOLOR=0", "LC_ALL=C.UTF-8"})},
		{Name: "ProtectSystem", Value: godbus.MakeVariant("full")},
		{Name: "ReadWritePaths", Value: godbus.MakeVariant(systemdReadWritePaths())},
		{Name: "PrivateTmp", Value: godbus.MakeVariant(false)},
		{Name: "NoNewPrivileges", Value: godbus.MakeVariant(false)},
		{Name: "RuntimeMaxUSec", Value: godbus.MakeVariant(uint64(updaterRuntimeLimit / time.Microsecond))},
		{Name: "TimeoutStopUSec", Value: godbus.MakeVariant(uint64(updaterStopTimeout / time.Microsecond))},
		{Name: "StandardOutput", Value: godbus.MakeVariant("journal")},
		{Name: "StandardError", Value: godbus.MakeVariant("journal")},
		{Name: "SyslogIdentifier", Value: godbus.MakeVariant("linuxio-app-update")},
	}
}
