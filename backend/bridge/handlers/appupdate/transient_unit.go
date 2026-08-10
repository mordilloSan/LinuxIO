package appupdate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	godbus "github.com/godbus/dbus/v5"

	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

const (
	updaterRuntimeLimit = 10 * time.Minute
	updaterStopTimeout  = 30 * time.Second
)

var errUpdaterUnitNotFound = errors.New("updater unit not found")

type transientUnitProperty struct {
	Name  string
	Value godbus.Variant
}

type transientAuxUnit struct {
	Name       string
	Properties []transientUnitProperty
}

type transientExecCommand struct {
	Path          string
	Arguments     []string
	IgnoreFailure bool
}

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

type updaterUnitState struct {
	ActiveState   string
	SubState      string
	ServiceResult string
	ExitCode      int
}

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
result_dir=${result_path%/*}
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
	var job dbusclient.ObjectPath
	err := dbusclient.SystemdManager.UseSessionWithOptions(
		ctx,
		dbusclient.SystemBusOptions{Subsystem: "app-update", Timeout: 15 * time.Second, NoRetry: true},
		func(session dbusclient.SystemSession) error {
			if err := session.RequireAvailable(); err != nil {
				return err
			}
			return session.CallStore(
				dbusclient.SystemdManagerIface+".StartTransientUnit",
				dbusclient.CallPolicy{},
				[]any{launch.Unit, "fail", properties, []transientAuxUnit{}},
				&job,
			)
		},
	)
	if err != nil {
		return fmt.Errorf("start transient updater unit %s: %w", launch.Unit, err)
	}
	return nil
}

func (systemdUpdaterExecutor) Inspect(ctx context.Context, unitName, expectedDescription string) (updaterUnitState, error) {
	var state updaterUnitState
	err := dbusclient.SystemdManager.UseSessionWithOptions(
		ctx,
		dbusclient.SystemBusOptions{Subsystem: "app-update", Timeout: 10 * time.Second},
		func(session dbusclient.SystemSession) error {
			found, inspectErr := inspectUpdaterUnit(session, unitName, expectedDescription)
			state = found
			return inspectErr
		},
	)
	if err != nil {
		return updaterUnitState{}, fmt.Errorf("inspect updater unit %s: %w", unitName, err)
	}
	return state, nil
}

func inspectUpdaterUnit(session dbusclient.SystemSession, unitName, expectedDescription string) (updaterUnitState, error) {
	var path dbusclient.ObjectPath
	if err := session.CallStore(
		dbusclient.SystemdManagerIface+".GetUnit",
		dbusclient.CallPolicy{},
		[]any{unitName},
		&path,
	); err != nil {
		if session.Context().Err() != nil {
			return updaterUnitState{}, session.Context().Err()
		}
		if isNoSuchUnitError(err) {
			return updaterUnitState{}, errUpdaterUnitNotFound
		}
		return updaterUnitState{}, err
	}

	unit := session.ObjectAt(path)
	id, err := dbusclient.GetProperty[string](session.Context(), unit, dbusclient.SystemdUnitIface, "Id")
	if err != nil {
		return updaterUnitState{}, err
	}
	description, err := dbusclient.GetProperty[string](session.Context(), unit, dbusclient.SystemdUnitIface, "Description")
	if err != nil {
		return updaterUnitState{}, err
	}
	transient, err := dbusclient.GetProperty[bool](session.Context(), unit, dbusclient.SystemdUnitIface, "Transient")
	if err != nil {
		return updaterUnitState{}, err
	}
	if id != unitName || description != expectedDescription || !transient {
		return updaterUnitState{}, fmt.Errorf("updater unit identity mismatch for %s", unitName)
	}
	return readUpdaterUnitState(session, unit)
}

func readUpdaterUnitState(session dbusclient.SystemSession, unit godbus.BusObject) (updaterUnitState, error) {
	var state updaterUnitState
	var err error
	state.ActiveState, err = dbusclient.GetProperty[string](session.Context(), unit, dbusclient.SystemdUnitIface, "ActiveState")
	if err != nil {
		return updaterUnitState{}, err
	}
	state.SubState, _ = dbusclient.GetProperty[string](session.Context(), unit, dbusclient.SystemdUnitIface, "SubState")
	state.ServiceResult, _ = dbusclient.GetProperty[string](session.Context(), unit, dbusclient.SystemdServiceIface, "Result")
	if exitCode, getErr := dbusclient.GetProperty[int32](session.Context(), unit, dbusclient.SystemdServiceIface, "ExecMainStatus"); getErr == nil {
		state.ExitCode = int(exitCode)
	}
	return state, nil
}

func (systemdUpdaterExecutor) Stop(ctx context.Context, unitName string) error {
	return systemdapi.StopUnit(ctx, unitName)
}

func (systemdUpdaterExecutor) Collect(ctx context.Context, unitName string) {
	properties := []transientUnitProperty{{Name: "CollectMode", Value: godbus.MakeVariant("inactive-or-failed")}}
	_ = dbusclient.SystemdManager.UseSessionWithOptions(
		ctx,
		dbusclient.SystemBusOptions{Subsystem: "app-update", Timeout: 5 * time.Second},
		func(session dbusclient.SystemSession) error {
			return session.Call(
				dbusclient.SystemdManagerIface+".SetUnitProperties",
				dbusclient.CallPolicy{},
				unitName,
				true,
				properties,
			)
		},
	)
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

func isNoSuchUnitError(err error) bool {
	var dbusErr godbus.Error
	if !errors.As(err, &dbusErr) {
		return false
	}
	return dbusErr.Name == "org.freedesktop.systemd1.NoSuchUnit" || dbusErr.Name == "org.freedesktop.systemd1.LoadFailed"
}
