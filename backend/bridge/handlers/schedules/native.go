package schedules

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

type unitState struct {
	Exists       bool
	InvocationID string
	ActiveState  string
	Result       string
	ExitCode     int32
	ExitStatus   int32
	ExitedAt     uint64
	PendingJob   bool
	NextRunAt    *string
	LastRunAt    *string
}

type hostOps struct {
	inspect        func(context.Context, string) (unitState, error)
	stopInvocation func(context.Context, string, string) error
	start          func(context.Context, string) error
	stop           func(context.Context, string) error
	enable         func(context.Context, string) error
	disable        func(context.Context, string) error
	reload         func(context.Context) error
	calendar       func(context.Context, string) error
	lookupUser     func(string) (*user.User, error)
	available      func() error
}

func nativeHost() hostOps {
	return hostOps{
		inspect:        inspectUnit,
		stopInvocation: stopInvocation,
		start:          startUnit,
		stop:           systemd.StopUnit,
		enable:         systemd.EnableUnit,
		disable:        systemd.DisableUnit,
		reload:         systemd.DaemonReload,
		calendar:       validateCalendar,
		lookupUser:     user.Lookup,
		available:      nativeAvailable,
	}
}

func serviceName(id string) string { return "linuxio-schedule-" + id + ".service" }
func timerName(id string) string   { return "linuxio-schedule-" + id + ".timer" }

func validInvocationID(id string) bool {
	if len(id) != 32 || strings.ToLower(id) != id || id == strings.Repeat("0", 32) {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func timestamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func timestampUSec(value uint64) *string {
	if value == 0 || value == ^uint64(0) || value > uint64(1<<63-1)/1000 {
		return nil
	}
	result := timestamp(time.UnixMicro(int64(value)))
	return &result
}

func active(state unitState) bool {
	return state.PendingJob || state.ActiveState == "activating" || state.ActiveState == "active" ||
		state.ActiveState == "reloading" || state.ActiveState == "deactivating"
}

func nativeAvailable() error {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return fmt.Errorf("systemd is unavailable: %w", err)
	}
	if _, err := exec.LookPath("systemd-analyze"); err != nil {
		return fmt.Errorf("systemd calendar validation is unavailable: %w", err)
	}
	if _, err := os.Stat("/usr/bin/bash"); err != nil {
		return fmt.Errorf("bash is unavailable: %w", err)
	}
	return nil
}

func validateCalendar(ctx context.Context, expression string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	args := []string{"calendar", "--iterations=1", "--", expression}
	output, err := exec.CommandContext(ctx, "systemd-analyze", args...).CombinedOutput()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return utils.CommandOutputError("systemd-analyze", args, output, err)
}

func normalizeOptions(ctx context.Context, options apischema.ScheduleOptions, host hostOps) (apischema.ScheduleOptions, *user.User, error) {
	options.Name = strings.TrimSpace(options.Name)
	options.User = strings.TrimSpace(options.User)
	options.OnCalendar = strings.TrimSpace(options.OnCalendar)
	options.Timezone = strings.TrimSpace(options.Timezone)
	options.WorkingDirectory = strings.TrimSpace(options.WorkingDirectory)
	if options.Timezone == "" {
		options.Timezone = "UTC"
	}
	if err := validateOptions(options); err != nil {
		return options, nil, err
	}
	if options.Arguments == nil {
		options.Arguments = []string{}
	}
	account, err := host.lookupUser(options.User)
	if err != nil {
		return options, nil, fmt.Errorf("look up task execution user: %w", err)
	}
	if _, err := strconv.ParseUint(account.Uid, 10, 32); err != nil {
		return options, nil, fmt.Errorf("invalid execution UID: %w", err)
	}
	if _, err := strconv.ParseUint(account.Gid, 10, 32); err != nil {
		return options, nil, fmt.Errorf("invalid execution GID: %w", err)
	}
	if err := host.calendar(ctx, options.OnCalendar+" "+options.Timezone); err != nil {
		return options, nil, fmt.Errorf("validate schedule: %w", err)
	}
	return options, account, nil
}

func validateOptions(options apischema.ScheduleOptions) error {
	for _, value := range []string{options.Name, options.User, options.OnCalendar, options.Timezone, options.WorkingDirectory} {
		if strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return errors.New("scheduled task settings cannot contain control characters")
		}
	}
	if options.Name == "" || len(options.Name) > 128 || options.User == "" || len(options.User) > 64 {
		return errors.New("a task name and execution user are required")
	}
	if strings.TrimSpace(options.Script) == "" || len(options.Script) > 64<<10 || strings.ContainsRune(options.Script, 0) {
		return errors.New("script must contain between 1 and 65536 bytes without NUL characters")
	}
	if options.OnCalendar == "" || len(options.OnCalendar) > 256 || len(options.Timezone) > 128 {
		return errors.New("a valid calendar expression and timezone are required")
	}
	if _, err := time.LoadLocation(options.Timezone); err != nil {
		return fmt.Errorf("invalid schedule timezone: %w", err)
	}
	if options.TimeoutSeconds < 1 || options.TimeoutSeconds > 7*24*60*60 {
		return errors.New("task timeout must be between 1 second and 7 days")
	}
	if options.WorkingDirectory != "" && (!filepath.IsAbs(options.WorkingDirectory) || len(options.WorkingDirectory) > 4096 || strings.HasSuffix(options.WorkingDirectory, `\`)) {
		return errors.New("working directory must be an absolute path")
	}
	if err := validateArguments(options.Arguments); err != nil {
		return err
	}
	return nil
}

func validateArguments(arguments []string) error {
	if len(arguments) > 64 {
		return errors.New("a scheduled task supports at most 64 arguments")
	}
	total := 0
	for _, arg := range arguments {
		total += len(arg)
		if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
			return errors.New("invalid script argument")
		}
	}
	if total > 16<<10 {
		return errors.New("script arguments exceed 16384 bytes")
	}
	return nil
}

func definitionRevision(def apischema.ScheduleDefinition) string {
	data, _ := json.Marshal(def.Options)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// ExecStart uses ':' to disable environment substitution; percent specifiers
// still require escaping. Each script argument remains a separate argv entry.
func unitWord(value string) string {
	return strings.ReplaceAll(strconv.Quote(strings.ReplaceAll(value, "%", "%%")), ";", `\x3b`)
}

func (s store) scriptPath(def apischema.ScheduleDefinition) string {
	return filepath.Join(s.configDir, "scripts", def.ID+"-"+definitionRevision(def)+".sh")
}

func (s store) renderService(def apischema.ScheduleDefinition, account *user.User) []byte {
	var body strings.Builder
	// The calendar controls frequency; the default start limit would reject
	// legitimate second-level activations, including successful oneshot runs.
	fmt.Fprintf(&body, "[Unit]\nDescription=LinuxIO scheduled task %s\nStartLimitIntervalSec=0\n\n[Service]\nType=oneshot\nUser=%s\nGroup=%s\n",
		def.ID, account.Uid, account.Gid)
	fmt.Fprintf(&body, "TimeoutStartSec=%ds\nTimeoutStopSec=15s\nKillMode=control-group\nNoNewPrivileges=yes\nUMask=0077\n",
		def.Options.TimeoutSeconds)
	fmt.Fprintf(&body, "ExecStart=:/usr/bin/bash %s", unitWord(s.scriptPath(def)))
	for _, arg := range def.Options.Arguments {
		fmt.Fprintf(&body, " %s", unitWord(arg))
	}
	fmt.Fprintf(&body, "\nStandardOutput=journal\nStandardError=journal\nSyslogIdentifier=linuxio-schedule\n")
	if def.Options.WorkingDirectory != "" {
		fmt.Fprintf(&body, "WorkingDirectory=%s\n", strings.ReplaceAll(def.Options.WorkingDirectory, "%", "%%"))
	}
	return []byte(body.String())
}

func renderTimer(def apischema.ScheduleDefinition) []byte {
	return fmt.Appendf(nil, "[Unit]\nDescription=Timer for LinuxIO scheduled task %s\n\n[Timer]\nOnCalendar=%s %s\nAccuracySec=1ms\nPersistent=%t\nUnit=%s\n\n[Install]\nWantedBy=timers.target\n",
		def.ID, strings.ReplaceAll(def.Options.OnCalendar, "%", "%%"), def.Options.Timezone, def.Options.Persistent, serviceName(def.ID))
}

func startUnit(ctx context.Context, name string) error {
	return dbusclient.SystemdManager.UseSessionWithOptions(ctx,
		dbusclient.SystemBusOptions{Subsystem: "schedules", Timeout: 10 * time.Second, NoRetry: true},
		func(session dbusclient.SystemSession) error {
			var job godbus.ObjectPath
			return session.CallStore(dbusclient.SystemdManagerIface+".StartUnit", dbusclient.CallPolicy{}, []any{name, "fail"}, &job)
		})
}

func missingUnit(err error) bool {
	var busErr godbus.Error
	return errors.As(err, &busErr) && (busErr.Name == "org.freedesktop.systemd1.NoSuchUnit" ||
		busErr.Name == "org.freedesktop.systemd1.NoUnitForInvocationID" || busErr.Name == "org.freedesktop.DBus.Error.UnknownObject")
}

func inspectUnit(ctx context.Context, name string) (unitState, error) {
	var state unitState
	err := dbusclient.SystemdManager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		var path godbus.ObjectPath
		if err := session.CallStore(dbusclient.SystemdManagerIface+".GetUnit", dbusclient.CallPolicy{}, []any{name}, &path); err != nil {
			if missingUnit(err) {
				state.ActiveState = "inactive"
				return nil
			}
			return err
		}
		var err error
		state, err = readUnitState(session, session.ObjectAt(path), name)
		return err
	})
	return state, err
}

func invocationObject(session dbusclient.SystemSession, scheduleID, invocationID string) (godbus.BusObject, error) {
	if !validID(scheduleID) || !validInvocationID(invocationID) {
		return nil, errors.New("invalid scheduled run identity")
	}
	id, _ := hex.DecodeString(invocationID)
	var path godbus.ObjectPath
	if err := session.CallStore(dbusclient.SystemdManagerIface+".GetUnitByInvocationID", dbusclient.CallPolicy{}, []any{id}, &path); err != nil {
		return nil, err
	}
	unit := session.ObjectAt(path)
	name, err := dbusclient.GetProperty[string](session, unit, dbusclient.SystemdUnitIface, "Id")
	if err != nil {
		return nil, err
	}
	if name != serviceName(scheduleID) {
		return nil, errors.New("scheduled invocation belongs to another unit")
	}
	return unit, nil
}

func stopInvocation(ctx context.Context, scheduleID, invocationID string) error {
	return dbusclient.SystemdManager.UseSessionWithOptions(ctx,
		dbusclient.SystemBusOptions{Subsystem: "schedules", Timeout: 10 * time.Second, NoRetry: true},
		func(session dbusclient.SystemSession) error {
			unit, err := invocationObject(session, scheduleID, invocationID)
			if err != nil {
				return err
			}
			// This object path expires when the invocation changes. Never fall back
			// to StopUnit(name), which could stop a subsequent timer activation.
			var job godbus.ObjectPath
			return unit.CallWithContext(session.Context(), dbusclient.SystemdUnitIface+".Stop", 0, "replace").Store(&job)
		})
}

func readUnitState(session dbusclient.SystemSession, unit godbus.BusObject, name string) (unitState, error) {
	state := unitState{Exists: true}
	// systemd GetAll("") returns all interfaces in one reply, so job, activation,
	// invocation and exit properties cannot come from different executions.
	var properties map[string]godbus.Variant
	if err := unit.CallWithContext(session.Context(), "org.freedesktop.DBus.Properties.GetAll", 0, "").Store(&properties); err != nil {
		return state, err
	}
	var job []any
	var id []byte
	var next, last uint64
	fields := map[string]any{"ActiveState": &state.ActiveState, "Job": &job}
	isTimer := strings.HasSuffix(name, ".timer")
	if isTimer {
		fields["NextElapseUSecRealtime"], fields["LastTriggerUSec"] = &next, &last
	} else {
		fields["InvocationID"], fields["Result"] = &id, &state.Result
		fields["ExecMainCode"], fields["ExecMainStatus"] = &state.ExitCode, &state.ExitStatus
		fields["ExecMainExitTimestamp"] = &state.ExitedAt
	}
	for property, target := range fields {
		value, ok := properties[property]
		if !ok {
			return state, fmt.Errorf("missing systemd property %s", property)
		}
		if err := value.Store(target); err != nil {
			return state, fmt.Errorf("invalid systemd property %s: %w", property, err)
		}
	}
	if state.ActiveState == "" || len(job) != 2 {
		return state, errors.New("invalid systemd state or job property")
	}
	jobID, ok := job[0].(uint32)
	if !ok {
		return state, errors.New("invalid systemd job identity")
	}
	state.PendingJob = jobID != 0
	if isTimer {
		state.NextRunAt, state.LastRunAt = timestampUSec(next), timestampUSec(last)
	} else {
		state.InvocationID = hex.EncodeToString(id)
	}
	return state, nil
}
