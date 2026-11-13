package schedules

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

const testID = "123e4567-e89b-12d3-a456-426614174000"
const testInvocation = "1234567890abcdef1234567890abcdef"

func TestScheduleDirectoryModesUnderUmask(t *testing.T) {
	if os.Getenv("LINUXIO_TEST_SCHEDULE_UMASK") != "1" {
		command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestScheduleDirectoryModesUnderUmask$")
		command.Env = append(os.Environ(), "LINUXIO_TEST_SCHEDULE_UMASK=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("restrictive bridge umask: %v\n%s", err, output)
		}
		return
	}
	// Umask is process-wide: exercise the bridge's setting in a child process.
	syscall.Umask(0o077)
	base := t.TempDir()
	for name, mode := range map[string]os.FileMode{"schedules": 0o755, "scripts": 0o755, "definitions": 0o700} {
		path := filepath.Join(base, name)
		for range 2 { // Creation and repair of existing directories.
			if err := secureDirectory(path, mode); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != mode {
				t.Errorf("%s mode = %o, want %o", name, info.Mode().Perm(), mode)
			}
			if err := os.Chmod(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func testOptions() apischema.ScheduleOptions {
	return apischema.ScheduleOptions{Name: "Backup", Script: "#!/bin/bash\nprintf '%s\\n' \"$@\"\n", User: "runner", OnCalendar: "daily", Timezone: "UTC", TimeoutSeconds: 60}
}

type testNative struct {
	units       map[string]unitState
	calls       []string
	reloadErr   error
	lookupErr   error
	markErr     error
	stopErr     error
	withdrawErr error
	notifyErr   error // fails every write of a choice
	clearErr    error // fails the clearing of one
}

// testManager keeps the notification settings in a temporary directory: the
// choices are written to a real settings file, and each write is a call.
func testManager(t *testing.T) (manager, *testNative) {
	t.Helper()
	dir := t.TempDir()
	settings := alerts.Path
	alerts.Path = filepath.Join(dir, "alerts.json")
	t.Cleanup(func() { alerts.Path = settings })
	native := &testNative{units: map[string]unitState{}}
	operation := func(action string) func(context.Context, string) error {
		return func(_ context.Context, name string) error {
			native.calls = append(native.calls, action+":"+name)
			state := native.units[name]
			state.Exists = true
			if action == "start" {
				state.ActiveState = "active"
			}
			if action == "stop" {
				state.ActiveState = "inactive"
				state.PendingJob = false
			}
			native.units[name] = state
			return nil
		}
	}
	host := hostOps{available: func() error { return nil }, calendar: func(ctx context.Context, _ string) error { return ctx.Err() }, lookupUser: func(string) (*user.User, error) {
		return &user.User{Uid: strconv.Itoa(os.Geteuid()), Gid: strconv.Itoa(os.Getegid())}, native.lookupErr
	}, inspect: func(_ context.Context, name string) (unitState, error) {
		state := native.units[name]
		if state.ActiveState == "" {
			state.ActiveState = "inactive"
		}
		return state, nil
	}, markCanceled: func(_ context.Context, unit, invocation string) error {
		native.calls = append(native.calls, "mark:"+unit+":"+invocation)
		return native.markErr
	}, withdrawCancel: func(_ context.Context, unit, invocation string) error {
		native.calls = append(native.calls, "withdraw:"+unit+":"+invocation)
		return native.withdrawErr
	}, start: operation("start"), stop: operation("stop"), enable: operation("enable"), disable: operation("disable"), reload: func(context.Context) error { native.calls = append(native.calls, "reload"); return native.reloadErr }, stopInvocation: func(_ context.Context, id, invocation string) error {
		native.calls = append(native.calls, "stop-invocation:"+id+":"+invocation)
		return native.stopErr
	}, notifyChoices: notifyChoices, setNotifyChoice: func(ctx context.Context, id, choice string) error {
		native.calls = append(native.calls, "notify:"+id+":"+choice)
		if native.notifyErr != nil {
			return native.notifyErr
		}
		if choice == "" && native.clearErr != nil {
			return native.clearErr
		}
		return setNotifyChoice(ctx, id, choice)
	}}
	return manager{store{configDir: filepath.Join(dir, "config"), unitDir: filepath.Join(dir, "units")}, host}, native
}

func TestStopMarksTheRunCanceledFirst(t *testing.T) {
	m, native := testManager(t)
	ctx := t.Context()
	created, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions()})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Definition.ID
	req := apischema.ScheduleStopRequest{ID: id, InvocationID: testInvocation}
	mark := "mark:" + serviceName(id) + ":" + testInvocation
	stop := "stop-invocation:" + id + ":" + testInvocation

	// A stop that succeeds leaves the mark of the run it named.
	if err := m.stop(ctx, req); err != nil {
		t.Fatal(err)
	}
	if got, want := native.calls[len(native.calls)-2:], []string{mark, stop}; !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want them to end with %v", got, want)
	}

	native.markErr = errors.New("store unavailable")
	native.calls = nil
	if err := m.stop(ctx, req); err != nil {
		t.Fatalf("a failed mark stopped the stop: %v", err)
	}
	if want := []string{mark, stop}; !slices.Equal(native.calls, want) {
		t.Fatalf("calls = %v, want %v", native.calls, want)
	}
}

func TestStopThatFailsWithdrawsTheMark(t *testing.T) {
	m, native := testManager(t)
	ctx := t.Context()
	created, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions()})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Definition.ID
	req := apischema.ScheduleStopRequest{ID: id, InvocationID: testInvocation}
	want := []string{
		"mark:" + serviceName(id) + ":" + testInvocation,
		"stop-invocation:" + id + ":" + testInvocation,
		"withdraw:" + serviceName(id) + ":" + testInvocation,
	}
	native.stopErr = errors.New("unit is not running this invocation")

	for _, withdrawErr := range []error{nil, errors.New("store unavailable")} {
		native.withdrawErr = withdrawErr
		native.calls = nil
		if err := m.stop(ctx, req); !errors.Is(err, native.stopErr) {
			t.Fatalf("stop = %v, want the stop's error (withdraw error %v)", err, withdrawErr)
		}
		if !slices.Equal(native.calls, want) {
			t.Fatalf("calls = %v, want %v (withdraw error %v)", native.calls, want, withdrawErr)
		}
	}
}

func TestNativeScriptLifecycle(t *testing.T) {
	m, native := testManager(t)
	ctx := t.Context()
	created, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions()})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Definition.ID
	if created.Definition.Enabled || !validID(id) {
		t.Fatalf("new task should be disabled with a valid ID: %+v", created)
	}
	script := m.store.scriptPath(created.Definition)
	info, err := os.Stat(script)
	if err != nil || info.Mode().Perm() != 0o440 {
		t.Fatalf("script permissions: %v %v", info, err)
	}
	enabled := true
	if enableErr := m.change(ctx, id, nil, &enabled, false); enableErr != nil {
		t.Fatal(enableErr)
	}
	if native.units[timerName(id)].ActiveState != "active" {
		t.Fatal("timer was not started")
	}
	native.units[serviceName(id)] = unitState{Exists: true, ActiveState: "activating", PendingJob: true, InvocationID: testInvocation}
	edit := testOptions()
	edit.Script = "echo changed\n"
	if err := m.change(ctx, id, &edit, nil, false); err == nil {
		t.Fatal("edited a running script")
	}
	if err := m.change(ctx, id, nil, nil, true); err == nil {
		t.Fatal("deleted a running script")
	}
	if native.units[timerName(id)].ActiveState != "active" {
		t.Fatal("rejecting active edit did not restore timer")
	}
	enabled = false
	if enableErr := m.change(ctx, id, nil, &enabled, false); enableErr != nil {
		t.Fatal(enableErr)
	}
	if native.units[serviceName(id)].ActiveState != "activating" {
		t.Fatal("disable stopped the script")
	}
	if err := m.stop(ctx, apischema.ScheduleStopRequest{ID: id, InvocationID: testInvocation}); err != nil {
		t.Fatal(err)
	}
	if got := native.calls[len(native.calls)-1]; got != "stop-invocation:"+id+":"+testInvocation {
		t.Fatal(got)
	}
	native.units[serviceName(id)] = unitState{Exists: true, ActiveState: "inactive"}
	if err := m.change(ctx, id, &edit, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old immutable script was not removed")
	}
	if err := m.runNow(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := m.runNow(ctx, id); err == nil {
		t.Fatal("accepted overlapping manual run")
	}
	native.units[serviceName(id)] = unitState{Exists: true, ActiveState: "inactive"}
	if err := m.change(ctx, id, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.definition(id); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("definition still exists: %v", err)
	}
}

func TestRunNowRecreatesMissingRevisionScript(t *testing.T) {
	m, native := testManager(t)
	ctx := t.Context()
	created, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions()})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Definition.ID
	script := m.store.scriptPath(created.Definition)
	if err := os.Remove(script); err != nil {
		t.Fatal(err)
	}
	if err := m.runNow(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("script was not recreated: %v", err)
	}
	if got := native.calls[len(native.calls)-1]; got != "start:"+serviceName(id) {
		t.Fatal(got)
	}
}

func TestFailedReloadLeavesDisabledRepairableDefinition(t *testing.T) {
	m, n := testManager(t)
	ctx := t.Context()
	created, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions()})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Definition.ID
	enabled := true
	if enableErr := m.change(ctx, id, nil, &enabled, false); enableErr != nil {
		t.Fatal(enableErr)
	}
	n.reloadErr = errors.New("unavailable")
	edit := testOptions()
	edit.Script = "echo replacement\n"
	if changeErr := m.change(ctx, id, &edit, nil, false); !errors.Is(changeErr, n.reloadErr) {
		t.Fatalf("lost reload failure: %v", changeErr)
	}
	saved, err := m.store.definition(id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Enabled || saved.Options.Script != edit.Script {
		t.Fatalf("unsafe recovery definition: %+v", saved)
	}
	if n.units[timerName(id)].ActiveState != "inactive" {
		t.Fatal("failed update left timer active")
	}
	n.reloadErr = nil
	if enableErr := m.change(ctx, id, nil, &enabled, false); enableErr != nil {
		t.Fatal(enableErr)
	}
}

func TestValidationAndProtectedDefinitions(t *testing.T) {
	m, _ := testManager(t)
	invalid := []apischema.ScheduleOptions{}
	for _, edit := range []func(*apischema.ScheduleOptions){func(o *apischema.ScheduleOptions) { o.User = "runner\nUser=root" }, func(o *apischema.ScheduleOptions) { o.Script = "bad\x00" }, func(o *apischema.ScheduleOptions) { o.Arguments = []string{"bad\x00"} }, func(o *apischema.ScheduleOptions) { o.WorkingDirectory = "relative" }, func(o *apischema.ScheduleOptions) { o.WorkingDirectory = "/tmp/\\" }, func(o *apischema.ScheduleOptions) { o.TimeoutSeconds = 0 }, func(o *apischema.ScheduleOptions) { o.OnCalendar = "daily\nUnit=evil" }, func(o *apischema.ScheduleOptions) { o.Timezone = "No/SuchZone" }} {
		o := testOptions()
		edit(&o)
		invalid = append(invalid, o)
	}
	for _, opts := range invalid {
		if _, _, err := normalizeOptions(t.Context(), opts, m.host); err == nil {
			t.Fatalf("accepted invalid options: %+v", opts)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := normalizeOptions(ctx, testOptions(), m.host); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	if _, err := m.store.definition("../../outside"); err == nil {
		t.Fatal("accepted traversal")
	}
	if err := m.store.save(apischema.ScheduleDefinition{ID: testID, Options: testOptions()}); err != nil {
		t.Fatal(err)
	}
	path, _ := m.store.definitionPath(testID)
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.definition(testID); err == nil {
		t.Fatal("read unsafe definition")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "other.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.definition(testID); err == nil {
		t.Fatal("followed definition symlink")
	}
}

func TestRenderedUnitsUseOnlyNativeExecution(t *testing.T) {
	m, _ := testManager(t)
	opts := testOptions()
	opts.Arguments = []string{"two words", "$HOME", "%n", `quote"slash\`, ";", "line\nbreak"}
	opts.WorkingDirectory = "/tmp/a b%"
	def := apischema.ScheduleDefinition{ID: testID, Options: opts}
	unit := string(m.store.renderService(def, &user.User{Uid: "1001", Gid: "1002"}))
	for _, want := range []string{"StartLimitIntervalSec=0\n", "User=1001\nGroup=1002\n", "ExecStart=:/usr/bin/bash ", "TimeoutStartSec=60s", "KillMode=control-group", "StandardOutput=journal", "StandardError=journal", "WorkingDirectory=/tmp/a b%%", "%%n", `\x3b`} {
		if !strings.Contains(unit, want) {
			t.Errorf("missing %q in %s", want, unit)
		}
	}
	for _, forbidden := range []string{"ExecStartPre=", "ExecStopPost=", "schedule-worker", opts.Script} {
		if strings.Contains(unit, forbidden) {
			t.Errorf("unexpected %q in service", forbidden)
		}
	}
	timer := string(renderTimer(def))
	if !strings.Contains(timer, "OnCalendar=daily UTC") || !strings.Contains(timer, "Persistent=false") || !strings.Contains(timer, "AccuracySec=1ms\n") {
		t.Fatal(timer)
	}
	def.Options.Persistent = true
	if !strings.Contains(string(renderTimer(def)), "Persistent=true") {
		t.Fatal("missed-run policy lost")
	}
	// The native parser provides a useful check without requiring PID 1/system
	// bus access or installing any unit on the host.
	if _, err := exec.LookPath("systemd-analyze"); err != nil {
		t.Skip("systemd-analyze unavailable")
	}
	path := filepath.Join(t.TempDir(), serviceName(testID))
	if err := os.WriteFile(path, []byte(unit), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "systemd-analyze", "verify", path).CombinedOutput()
	if err != nil {
		t.Fatalf("native unit verification: %v\n%s", err, output)
	}
}

func TestUnavailableHistoryNeverBecomesSuccess(t *testing.T) {
	m, n := testManager(t)
	def := apischema.ScheduleDefinition{ID: testID, Options: testOptions()}
	if status := m.status(t.Context(), def); status.Result != "" || status.ExitStatus != nil || status.CanStop {
		t.Fatalf("invented native result: %+v", status)
	}
	n.units[serviceName(testID)] = unitState{Exists: true, ActiveState: "failed", Result: "exit-code", ExitedAt: 100, ExitCode: 1, ExitStatus: 7}
	if status := m.status(t.Context(), def); status.Result != "exit-code" || status.ExitStatus == nil || *status.ExitStatus != 7 {
		t.Fatalf("lost native result: %+v", status)
	}
}

func TestScheduledRunReadsTheScheduleName(t *testing.T) {
	m, _ := testManager(t)
	opts := testOptions()
	opts.Name = "Nightly backup"
	if err := m.store.save(apischema.ScheduleDefinition{ID: testID, Options: opts}); err != nil {
		t.Fatal(err)
	}
	kind, ok := scheduledrun.KindOf(serviceName(testID), m.store.configDir)
	if !ok || kind.Route != scheduledrun.RouteScript || kind.Label != "Nightly backup" || kind.Instance != testID {
		t.Errorf("KindOf = %+v, %v", kind, ok)
	}
}

// The run-record hooks are a packaged drop-in (packaging/systemd/
// linuxio-run-record.conf), never part of the rendered unit: ensureInstalled
// compares the unit on disk with renderService, so a hook line there would
// make every schedule look out of date.
func TestScheduleUnitCarriesNoRunHooks(t *testing.T) {
	m, _ := testManager(t)
	def := apischema.ScheduleDefinition{ID: testID, Options: testOptions()}
	unit := string(m.store.renderService(def, &user.User{Uid: "1001", Gid: "1002"}))
	if strings.Contains(unit, "scheduled-run") {
		t.Errorf("rendered unit carries a run hook:\n%s", unit)
	}
}

func settingsFile() string { return filepath.Join(filepath.Dir(alerts.Path), "notifications.json") }

// storedChoices are the schedules' own choices in the settings file.
func storedChoices(t *testing.T) map[string]string {
	t.Helper()
	n, err := alerts.LoadNotifications()
	if err != nil {
		t.Fatal(err)
	}
	return n.Scripts
}

func notifyCalls(calls []string) []string {
	return slices.DeleteFunc(slices.Clone(calls), func(call string) bool { return !strings.HasPrefix(call, "notify:") })
}

// installedPaths are the definition, script and units of schedule id.
func installedPaths(t *testing.T, m manager, id string) []string {
	t.Helper()
	def, err := m.store.definition(id)
	if err != nil {
		t.Fatal(err)
	}
	path, err := m.store.definitionPath(id)
	if err != nil {
		t.Fatal(err)
	}
	return []string{path, m.store.scriptPath(def), filepath.Join(m.store.unitDir, serviceName(id)), filepath.Join(m.store.unitDir, timerName(id))}
}

func installedFiles(t *testing.T, m manager, id string) []os.FileInfo {
	t.Helper()
	var infos []os.FileInfo
	for _, path := range installedPaths(t, m, id) {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		infos = append(infos, info)
	}
	return infos
}

// requireUntouched fails when a file was written again: every write renames a
// new file into place.
func requireUntouched(t *testing.T, before, after []os.FileInfo) {
	t.Helper()
	for i := range before {
		if !os.SameFile(before[i], after[i]) || !before[i].ModTime().Equal(after[i].ModTime()) {
			t.Errorf("%s was written again", before[i].Name())
		}
	}
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func TestCreateStoresTheNotifyChoice(t *testing.T) {
	m, _ := testManager(t)
	ctx := t.Context()
	plain, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions()})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Notify != "" {
		t.Errorf("a task created without a choice has %q", plain.Notify)
	}
	if _, statErr := os.Stat(settingsFile()); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a task created without a choice wrote the settings: %v", statErr)
	}

	created, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions(), Notify: alerts.ReportEveryRun})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Definition.ID
	if created.Notify != alerts.ReportEveryRun {
		t.Errorf("created task has %q, want %q", created.Notify, alerts.ReportEveryRun)
	}
	if want := map[string]string{id: alerts.ReportEveryRun}; !reflect.DeepEqual(storedChoices(t), want) {
		t.Errorf("stored choices = %v, want %v", storedChoices(t), want)
	}
	if got := alerts.NotificationsOrDefaults().ReportForScript(id); got != alerts.ReportEveryRun {
		t.Errorf("the task's runs report %q, want %q", got, alerts.ReportEveryRun)
	}
}

func TestGetReturnsTheNotifyChoice(t *testing.T) {
	m, _ := testManager(t)
	ctx := t.Context()
	chosen, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions(), Notify: alerts.ReportFailures})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions()})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{chosen.Definition.ID: alerts.ReportFailures, plain.Definition.ID: ""}
	for id, choice := range want {
		got, getErr := m.get(ctx, id)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if got.Notify != choice {
			t.Errorf("get %s: notify = %q, want %q", id, got.Notify, choice)
		}
	}
	list, err := m.list(ctx)
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]string{}
	for _, status := range list.Schedules {
		listed[status.Definition.ID] = status.Notify
	}
	if !reflect.DeepEqual(listed, want) {
		t.Errorf("listed choices = %v, want %v", listed, want)
	}
}

func TestUpdateOfOnlyTheNotifyChoiceLeavesTheUnitAlone(t *testing.T) {
	m, native := testManager(t)
	ctx := t.Context()
	created, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions()})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Definition.ID
	enabled := true
	if err := m.change(ctx, id, nil, &enabled, false); err != nil {
		t.Fatal(err)
	}
	// The script runs: a change of its options would be refused.
	native.units[serviceName(id)] = unitState{Exists: true, ActiveState: "activating", PendingJob: true, InvocationID: testInvocation}
	before := installedFiles(t, m, id)
	for _, choice := range []string{alerts.ReportEveryRun, ""} {
		native.calls = nil
		if err := m.update(ctx, apischema.ScheduleUpdateRequest{ID: id, Options: created.Definition.Options, Notify: &choice}); err != nil {
			t.Fatalf("update to %q: %v", choice, err)
		}
		if want := []string{"notify:" + id + ":" + choice}; !slices.Equal(native.calls, want) {
			t.Errorf("update to %q: calls = %v, want only %v", choice, native.calls, want)
		}
		if got := storedChoices(t)[id]; got != choice {
			t.Errorf("stored choice = %q, want %q", got, choice)
		}
		requireUntouched(t, before, installedFiles(t, m, id))
	}
	if native.units[timerName(id)].ActiveState != "active" || native.units[serviceName(id)].ActiveState != "activating" {
		t.Errorf("units changed: %+v", native.units)
	}
}

func TestUpdateOfOnlyTheNotifyChoiceRepairsAnIncompleteSchedule(t *testing.T) {
	m, _ := testManager(t)
	ctx := t.Context()
	created, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions()})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Definition.ID
	enabled := true
	if err := m.change(ctx, id, nil, &enabled, false); err != nil {
		t.Fatal(err)
	}
	service := filepath.Join(m.store.unitDir, serviceName(id))
	if err := os.Remove(service); err != nil {
		t.Fatal(err)
	}
	choice := alerts.ReportEveryRun
	if err := m.update(ctx, apischema.ScheduleUpdateRequest{ID: id, Options: created.Definition.Options, Notify: &choice}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(service); err != nil {
		t.Errorf("the first save did not repair the unit: %v", err)
	}
	if got := storedChoices(t)[id]; got != choice {
		t.Errorf("stored choice = %q, want %q", got, choice)
	}
}

func TestUpdateWithoutNotifyKeepsTheChoice(t *testing.T) {
	m, native := testManager(t)
	ctx := t.Context()
	created, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions(), Notify: alerts.ReportEveryRun})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Definition.ID
	edit := testOptions()
	edit.Script = "echo changed\n"
	native.calls = nil
	if err := m.update(ctx, apischema.ScheduleUpdateRequest{ID: id, Options: edit}); err != nil {
		t.Fatal(err)
	}
	// A choice equal to the stored one writes nothing either.
	same := alerts.ReportEveryRun
	edit.Script = "echo again\n"
	if err := m.update(ctx, apischema.ScheduleUpdateRequest{ID: id, Options: edit, Notify: &same}); err != nil {
		t.Fatal(err)
	}
	if calls := notifyCalls(native.calls); len(calls) != 0 {
		t.Errorf("updates wrote the settings: %v", calls)
	}
	if got := storedChoices(t)[id]; got != alerts.ReportEveryRun {
		t.Errorf("stored choice = %q, want %q", got, alerts.ReportEveryRun)
	}
	if saved, err := m.store.definition(id); err != nil || saved.Options.Script != edit.Script {
		t.Fatalf("options were not applied: %+v %v", saved, err)
	}
}

// The choice is stored after the checks that can refuse an update and before
// the schedule changes, so a refused update or a failed write changes nothing.
func TestUpdateOfOptionsAndNotifyChoice(t *testing.T) {
	m, native := testManager(t)
	ctx := t.Context()
	created, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions(), Notify: alerts.ReportEveryRun})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Definition.ID
	enabled := true
	if err := m.change(ctx, id, nil, &enabled, false); err != nil {
		t.Fatal(err)
	}
	edit := testOptions()
	edit.Script = "echo changed\n"
	failures := alerts.ReportFailures
	update := apischema.ScheduleUpdateRequest{ID: id, Options: edit, Notify: &failures}
	before := installedFiles(t, m, id)

	native.units[serviceName(id)] = unitState{Exists: true, ActiveState: "activating", PendingJob: true, InvocationID: testInvocation}
	if err := m.update(ctx, update); err == nil {
		t.Fatal("edited a running script")
	}
	native.units[serviceName(id)] = unitState{Exists: true, ActiveState: "inactive"}
	native.notifyErr = errors.New("settings are locked")
	if err := m.update(ctx, update); !errors.Is(err, native.notifyErr) {
		t.Fatalf("update = %v, want the settings error", err)
	}
	if got := storedChoices(t)[id]; got != alerts.ReportEveryRun {
		t.Errorf("stored choice = %q, want %q", got, alerts.ReportEveryRun)
	}
	requireUntouched(t, before, installedFiles(t, m, id))
	if native.units[timerName(id)].ActiveState != "active" {
		t.Error("the timer was not resumed")
	}

	native.notifyErr = nil
	if err := m.update(ctx, update); err != nil {
		t.Fatal(err)
	}
	if got := storedChoices(t)[id]; got != alerts.ReportFailures {
		t.Errorf("stored choice = %q, want %q", got, alerts.ReportFailures)
	}
	if saved, err := m.store.definition(id); err != nil || saved.Options.Script != edit.Script || !saved.Enabled {
		t.Fatalf("options were not applied: %+v %v", saved, err)
	}
}

func TestDeleteRemovesTheNotifyChoice(t *testing.T) {
	m, native := testManager(t)
	ctx := t.Context()
	logs := captureLogs(t)
	chosen, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions(), Notify: alerts.ReportEveryRun})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions()})
	if err != nil {
		t.Fatal(err)
	}
	native.calls = nil
	if deleteErr := m.change(ctx, plain.Definition.ID, nil, nil, true); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	if calls := notifyCalls(native.calls); len(calls) != 0 {
		t.Errorf("deleting a task without a choice wrote the settings: %v", calls)
	}
	if deleteErr := m.change(ctx, chosen.Definition.ID, nil, nil, true); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	if choices := storedChoices(t); len(choices) != 0 {
		t.Errorf("stored choices = %v, want none", choices)
	}

	// A choice that cannot be cleared does not stop the delete.
	kept, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions(), Notify: alerts.ReportEveryRun})
	if err != nil {
		t.Fatal(err)
	}
	native.notifyErr = errors.New("settings are locked")
	if err := m.change(ctx, kept.Definition.ID, nil, nil, true); err != nil {
		t.Fatalf("delete = %v, want it to carry on", err)
	}
	if _, err := m.store.definition(kept.Definition.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("definition still exists: %v", err)
	}
	requireOneWarning(t, logs, "could not clear the notification choice of a deleted scheduled task")
}

// The notice of a deleted schedule's completed runs links to a task that is
// gone, so the delete resolves it; other alerts stay.
func TestDeleteResolvesTheFinishedNotice(t *testing.T) {
	m, _ := testManager(t)
	ctx := t.Context()
	logs := captureLogs(t)
	var ids []string
	for range 2 {
		created, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions(), Notify: alerts.ReportEveryRun})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.Definition.ID)
	}
	// What the end hook raises for a completed run, and for a failed one.
	for _, obs := range []alerts.Observation{
		{Source: "jobs", Key: "finished-" + ids[0], Severity: alerts.SeverityInfo, Title: "Backup finished", OccurrenceID: "run-1"},
		{Source: "jobs", Key: "finished-" + ids[1], Severity: alerts.SeverityInfo, Title: "Backup finished", OccurrenceID: "run-2"},
		{Source: "jobs", Key: "12345678-90ab-cdef-1234-567890abcdef", Severity: alerts.SeverityError, Title: "Backup failed"},
	} {
		if err := alerts.Raise(ctx, obs); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.change(ctx, ids[0], nil, nil, true); err != nil {
		t.Fatal(err)
	}
	doc, err := alerts.Load()
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, a := range doc.Alerts {
		left = append(left, a.ID)
	}
	slices.Sort(left)
	if want := []string{"jobs/12345678-90ab-cdef-1234-567890abcdef", "jobs/finished-" + ids[1]}; !slices.Equal(left, want) {
		t.Errorf("alerts = %v, want %v", left, want)
	}
	if logs.Len() != 0 {
		t.Errorf("logs = %q, want none", logs.String())
	}

	// A notice that cannot be resolved does not stop the delete.
	if err := os.WriteFile(alerts.Path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.change(ctx, ids[1], nil, nil, true); err != nil {
		t.Fatalf("delete = %v, want it to carry on", err)
	}
	if _, err := m.store.definition(ids[1]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("definition still exists: %v", err)
	}
	requireOneWarning(t, logs, "could not resolve the finished notice of a deleted scheduled task")
}

func TestNotifyChoiceMustBeKnown(t *testing.T) {
	m, native := testManager(t)
	ctx := t.Context()
	for _, choice := range []string{alerts.ReportChanges, "always", "Failures"} {
		if _, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions(), Notify: choice}); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
			t.Errorf("create with %q = %v, want ErrInvalidArgs", choice, err)
		}
	}
	if defs, err := m.store.definitions(ctx); err != nil || len(defs) != 0 {
		t.Fatalf("definitions = %v %v, want none", defs, err)
	}
	created, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions()})
	if err != nil {
		t.Fatal(err)
	}
	native.calls = nil
	changes := alerts.ReportChanges
	if err := m.update(ctx, apischema.ScheduleUpdateRequest{ID: created.Definition.ID, Options: created.Definition.Options, Notify: &changes}); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Errorf("update with %q = %v, want ErrInvalidArgs", changes, err)
	}
	if len(native.calls) != 0 {
		t.Errorf("a refused choice changed something: %v", native.calls)
	}
	if _, err := os.Stat(settingsFile()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused choice wrote the settings: %v", err)
	}
}

func TestAnOverrideForAMissingScheduleIsHarmless(t *testing.T) {
	m, _ := testManager(t)
	ctx := t.Context()
	created, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions()})
	if err != nil {
		t.Fatal(err)
	}
	before, err := m.list(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// What a delete that could not clear the choice leaves.
	if _, settingsErr := alerts.UpdateNotifications(ctx, func(n *alerts.NotificationSettings) error {
		n.Scripts = map[string]string{testID: alerts.ReportEveryRun}
		return nil
	}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	after, err := m.list(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Errorf("list = %+v, want %+v", after, before)
	}
	// The settings page rewrites every setting it owns and keeps the choices.
	saved, err := alerts.RewriteNotifications(ctx, func(n *alerts.NotificationSettings) error {
		n.Report[alerts.KindScripts] = alerts.ReportEveryRun
		return nil
	})
	if err != nil {
		t.Fatalf("saving the settings: %v", err)
	}
	if saved.Scripts[testID] != alerts.ReportEveryRun {
		t.Errorf("saved choices = %v", saved.Scripts)
	}
	if got, err := m.get(ctx, created.Definition.ID); err != nil || got.Notify != "" {
		t.Errorf("get = %q %v, want no choice of its own", got.Notify, err)
	}
}

// The choice lives in the notification settings only: a member in the
// definition would change its revision, script path and unit text, and an
// older build refuses a definition with a member it does not know.
func TestScheduleDefinitionFileIsUnchanged(t *testing.T) {
	m, _ := testManager(t)
	ctx := t.Context()
	created, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions(), Notify: alerts.ReportEveryRun})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Definition.ID
	read := func() [][]byte {
		var files [][]byte
		for _, path := range installedPaths(t, m, id) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, data)
		}
		return files
	}
	with := read()
	// Clear the choice, then write the schedule's files again without one.
	none := ""
	if err := m.update(ctx, apischema.ScheduleUpdateRequest{ID: id, Options: created.Definition.Options, Notify: &none}); err != nil {
		t.Fatal(err)
	}
	opts := created.Definition.Options
	if err := m.change(ctx, id, &opts, nil, false); err != nil {
		t.Fatal(err)
	}
	without := read()
	for i, path := range installedPaths(t, m, id) {
		if !bytes.Equal(with[i], without[i]) {
			t.Errorf("%s differs with a choice:\n%s\nwithout:\n%s", filepath.Base(path), with[i], without[i])
		}
	}
	if bytes.Contains(with[0], []byte(alerts.ReportEveryRun)) || bytes.Contains(with[0], []byte("notify")) {
		t.Errorf("definition carries the choice:\n%s", with[0])
	}
	account := &user.User{Uid: strconv.Itoa(os.Geteuid()), Gid: strconv.Itoa(os.Getegid())}
	if !bytes.Equal(with[2], m.store.renderService(created.Definition, account)) {
		t.Errorf("unit is not the rendering of the definition alone:\n%s", with[2])
	}
}

var unparsableSettings = []byte(`{"scripts": `)

// breakSettings replaces the notification settings with a file that does not
// parse, and checks when the test ends that nothing wrote over it and that
// all it logged are warnings with the error. The file names the schedules in
// named, as the choices it lost would. It returns what is logged.
func breakSettings(t *testing.T, named ...string) *bytes.Buffer {
	t.Helper()
	broken := append(slices.Clone(unparsableSettings), strings.Join(named, ",")...)
	if err := os.WriteFile(settingsFile(), broken, 0o600); err != nil {
		t.Fatal(err)
	}
	logs := captureLogs(t)
	t.Cleanup(func() {
		if data, err := os.ReadFile(settingsFile()); err != nil || !bytes.Equal(data, broken) {
			t.Errorf("settings file = %q %v, want it untouched", data, err)
		}
		for line := range strings.Lines(logs.String()) {
			if !strings.Contains(line, "level=WARN") || !strings.Contains(line, "error=") {
				t.Errorf("log line %q is not a warning with the error", line)
			}
		}
	})
	return logs
}

// requireOneWarning fails unless logs hold one line: a warning saying message,
// with the error.
func requireOneWarning(t *testing.T, logs *bytes.Buffer, message string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "level=WARN") || !strings.Contains(lines[0], `msg="`+message+`"`) || !strings.Contains(lines[0], "error=") {
		t.Errorf("logs = %q, want one warning %q with the error", logs.String(), message)
	}
}

func TestUnreadableSettingsShowNoNotifyChoice(t *testing.T) {
	m, _ := testManager(t)
	ctx := t.Context()
	for _, choice := range []string{alerts.ReportEveryRun, ""} {
		if _, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions(), Notify: choice}); err != nil {
			t.Fatal(err)
		}
	}
	logs := breakSettings(t)
	list, err := m.list(ctx)
	if err != nil || len(list.Schedules) != 2 {
		t.Fatalf("list = %+v %v", list, err)
	}
	for _, listed := range list.Schedules {
		if listed.Notify != "" {
			t.Errorf("listed %s with %q", listed.Definition.ID, listed.Notify)
		}
		if got, getErr := m.get(ctx, listed.Definition.ID); getErr != nil || got.Notify != "" {
			t.Errorf("get = %q %v, want no choice", got.Notify, getErr)
		}
	}
	// The Schedules tab polls both; the settings page warns once per load.
	if logs.Len() != 0 {
		t.Errorf("logs = %q, want nothing at info or above", logs.String())
	}
}

// requireSettingsConflict fails unless err is the unreadable settings as a
// conflict the admin repairs on the settings page, not a server fault.
func requireSettingsConflict(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, alerts.ErrUnparsableSettings) || bridgeipc.StatusCode(err) != 409 || !strings.Contains(err.Error(), "Notifications settings page") {
		t.Fatalf("err = %v, want the settings error as a 409 saying where they are repaired", err)
	}
}

func TestUnreadableSettingsRefuseANotifyChoice(t *testing.T) {
	m, native := testManager(t)
	ctx := t.Context()
	created, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions()})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Definition.ID
	enabled := true
	if enableErr := m.change(ctx, id, nil, &enabled, false); enableErr != nil {
		t.Fatal(enableErr)
	}
	breakSettings(t)
	units, err := os.ReadDir(m.store.unitDir)
	if err != nil {
		t.Fatal(err)
	}
	_, createErr := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions(), Notify: alerts.ReportFailures})
	requireSettingsConflict(t, createErr)
	if defs, defsErr := m.store.definitions(ctx); defsErr != nil || len(defs) != 1 {
		t.Fatalf("definitions = %d %v, want 1", len(defs), defsErr)
	}
	if after, readErr := os.ReadDir(m.store.unitDir); readErr != nil || len(after) != len(units) {
		t.Fatalf("units = %d %v, want %d", len(after), readErr, len(units))
	}

	// A choice to store or to clear, with other options and with the same
	// ones: what is stored cannot be read, so even a clear is refused.
	before := installedFiles(t, m, id)
	edit := testOptions()
	edit.Script = "echo changed\n"
	for _, choice := range []string{alerts.ReportFailures, ""} {
		for _, options := range []apischema.ScheduleOptions{edit, created.Definition.Options} {
			requireSettingsConflict(t, m.update(ctx, apischema.ScheduleUpdateRequest{ID: id, Options: options, Notify: &choice}))
		}
	}
	requireUntouched(t, before, installedFiles(t, m, id))
	if saved, defErr := m.store.definition(id); defErr != nil || saved.Options.Script != created.Definition.Options.Script || !saved.Enabled {
		t.Fatalf("definition changed: %+v %v", saved, defErr)
	}
	if native.units[timerName(id)].ActiveState != "active" {
		t.Error("the timer was not resumed")
	}

	// An update without a choice reads and writes no settings.
	if updateErr := m.update(ctx, apischema.ScheduleUpdateRequest{ID: id, Options: edit}); updateErr != nil {
		t.Fatal(updateErr)
	}
	if saved, defErr := m.store.definition(id); defErr != nil || saved.Options.Script != edit.Script {
		t.Fatalf("options were not applied: %+v %v", saved, defErr)
	}
}

func TestUnreadableNotifySettingsDoNotStopADelete(t *testing.T) {
	m, _ := testManager(t)
	ctx := t.Context()
	created, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions(), Notify: alerts.ReportEveryRun})
	if err != nil {
		t.Fatal(err)
	}
	logs := breakSettings(t, created.Definition.ID)
	if deleteErr := m.change(ctx, created.Definition.ID, nil, nil, true); deleteErr != nil {
		t.Fatalf("delete = %v, want it to carry on", deleteErr)
	}
	if _, defErr := m.store.definition(created.Definition.ID); !errors.Is(defErr, os.ErrNotExist) {
		t.Fatalf("definition still exists: %v", defErr)
	}
	requireOneWarning(t, logs, "could not clear the notification choice of a deleted scheduled task")
}

func TestUnreadableNotifySettingsDoNotWarnForATaskWithNoChoice(t *testing.T) {
	m, _ := testManager(t)
	ctx := t.Context()
	plain, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions()})
	if err != nil {
		t.Fatal(err)
	}
	logs := breakSettings(t)
	if deleteErr := m.change(ctx, plain.Definition.ID, nil, nil, true); deleteErr != nil {
		t.Fatalf("delete = %v, want it to carry on", deleteErr)
	}
	if logs.Len() != 0 {
		t.Errorf("logs = %q, want none: the task had no choice to clear", logs.String())
	}
}

func TestFailedCreateLeavesNoNotifyChoice(t *testing.T) {
	m, native := testManager(t)
	ctx := t.Context()
	// A definitions directory others can write is refused: no definition is saved.
	dir := filepath.Join(m.store.configDir, "definitions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions(), Notify: alerts.ReportEveryRun}); err == nil {
		t.Fatal("created a task in an unsafe directory")
	}
	if choices := storedChoices(t); len(choices) != 0 {
		t.Errorf("stored choices = %v, want none", choices)
	}

	// A choice that cannot be taken back is a warning; the create fails with
	// its own error.
	logs := captureLogs(t)
	native.clearErr = errors.New("settings are locked")
	if _, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions(), Notify: alerts.ReportEveryRun}); err == nil || errors.Is(err, native.clearErr) {
		t.Fatalf("create = %v, want the definition error", err)
	}
	requireOneWarning(t, logs, "could not take back the notification choice of a scheduled task that was not created")
	native.clearErr = nil

	// A create that fails after saving its definition leaves a disabled task,
	// saved again to repair it; it keeps its choice.
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	native.reloadErr = errors.New("unavailable")
	if _, err := m.create(ctx, apischema.ScheduleCreateRequest{Options: testOptions(), Notify: alerts.ReportEveryRun}); !errors.Is(err, native.reloadErr) {
		t.Fatalf("create = %v, want the reload error", err)
	}
	defs, err := m.store.definitions(ctx)
	if err != nil || len(defs) != 1 {
		t.Fatalf("definitions = %v %v, want the disabled task", defs, err)
	}
	if got := storedChoices(t)[defs[0].ID]; got != alerts.ReportEveryRun {
		t.Errorf("stored choice = %q, want %q", got, alerts.ReportEveryRun)
	}
}

// A stored value that validation refuses (a hand edit) is repaired on the
// settings page too, so storing a choice over it is a conflict, not a fault.
func TestInvalidStoredSettingsRefuseANotifyChoice(t *testing.T) {
	m, _ := testManager(t)
	if err := os.WriteFile(settingsFile(), []byte(`{"dockerUpdates":"sometimes"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := m.create(t.Context(), apischema.ScheduleCreateRequest{Options: testOptions(), Notify: alerts.ReportFailures})
	if !errors.Is(err, alerts.ErrInvalidSettings) || bridgeipc.StatusCode(err) != 409 || !strings.Contains(err.Error(), "Notifications settings page") {
		t.Fatalf("err = %v, want the settings error as a 409 saying where they are repaired", err)
	}
}
