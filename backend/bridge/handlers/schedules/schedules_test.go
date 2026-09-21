package schedules

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
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
	units     map[string]unitState
	calls     []string
	reloadErr error
	lookupErr error
}

func testManager(t *testing.T) (manager, *testNative) {
	t.Helper()
	dir := t.TempDir()
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
	}, start: operation("start"), stop: operation("stop"), enable: operation("enable"), disable: operation("disable"), reload: func(context.Context) error { native.calls = append(native.calls, "reload"); return native.reloadErr }, stopInvocation: func(_ context.Context, id, invocation string) error {
		native.calls = append(native.calls, "stop-invocation:"+id+":"+invocation)
		return nil
	}}
	return manager{store{configDir: filepath.Join(dir, "config"), unitDir: filepath.Join(dir, "units")}, host}, native
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
