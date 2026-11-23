package wireguard

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestUpAndDownInterfaceRunWGQuick(t *testing.T) {
	env := newWireGuardTestEnv(t)

	up, err := UpInterface(context.Background(), apischema.NameRequest{Name: "wgtest"})
	if err != nil {
		t.Fatalf("UpInterface returned error: %v", err)
	}
	upResult, ok := up.(map[string]any)
	if !ok || upResult["status"] != "on" {
		t.Fatalf("UpInterface result = %#v, want status on", up)
	}

	down, err := DownInterface(context.Background(), apischema.NameRequest{Name: "wgtest"})
	if err != nil {
		t.Fatalf("DownInterface returned error: %v", err)
	}
	downResult, ok := down.(map[string]any)
	if !ok || downResult["status"] != "off" {
		t.Fatalf("DownInterface result = %#v, want status off", down)
	}

	want := []wgQuickCall{{action: "up", name: "wgtest"}, {action: "down", name: "wgtest"}}
	if !slices.Equal(env.wgQuick, want) {
		t.Fatalf("wg-quick calls = %v, want %v", env.wgQuick, want)
	}
}

// fakeUnit stands for one interface's unit and the interface itself: a start
// or stop that succeeds moves both, as wg-quick does when systemd runs it.
type fakeUnit struct {
	state       string
	interfaceUp bool
	startErr    error
	stopErr     error
	calls       []string
}

func (u *fakeUnit) install(t *testing.T) {
	t.Helper()
	isInterfaceUpFunc = func(string) bool { return u.interfaceUp }
	interfaceGoneFunc = func(string) bool { return !u.interfaceUp }
	units = unitControl{
		activeState: func(context.Context, string) (string, error) { return u.state, nil },
		start: func(_ context.Context, unit string) error {
			u.calls = append(u.calls, "start "+unit)
			if u.startErr != nil {
				u.state = "failed"
				return u.startErr
			}
			u.state, u.interfaceUp = "active", true
			return nil
		},
		stop: func(_ context.Context, unit string) error {
			u.calls = append(u.calls, "stop "+unit)
			if u.stopErr != nil {
				u.state = "failed"
				return u.stopErr
			}
			u.state, u.interfaceUp = "inactive", false
			return nil
		},
		queueStop: func(_ context.Context, unit string) error {
			u.calls = append(u.calls, "queue-stop "+unit)
			return nil
		},
		resetFailed: func(_ context.Context, unit string) error {
			u.calls = append(u.calls, "reset-failed "+unit)
			u.state = "inactive"
			return nil
		},
		disable: func(_ context.Context, unit string) error {
			u.calls = append(u.calls, "disable "+unit)
			return nil
		},
		output: func(context.Context, string, time.Time) string { return "wg-quick: said why" },
		direct: func(_ context.Context, action, name string) (string, error) {
			u.calls = append(u.calls, "wg-quick "+action+" "+name)
			u.interfaceUp = false
			return "", nil
		},
	}
}

// Up and down go through the interface's unit, so wg-quick runs as systemd
// starts it at boot and never as a child of the job host.
func TestInterfacePowerGoesThroughTheUnit(t *testing.T) {
	const unit = "wg-quick@wgtest.service"
	for _, tc := range []struct {
		name   string
		unit   fakeUnit
		action string
		want   []string
	}{
		{name: "up starts the unit", unit: fakeUnit{state: "inactive"}, action: "up", want: []string{"start " + unit}},
		{name: "up of an interface that is up does nothing", unit: fakeUnit{state: "active", interfaceUp: true}, action: "up"},
		{name: "up after a failed start", unit: fakeUnit{state: "failed"}, action: "up", want: []string{"start " + unit}},
		{
			// A start of an active unit does nothing, so the unit an
			// interface was taken away from by hand is stopped first.
			name: "up clears a unit whose interface is gone", unit: fakeUnit{state: "active", stopErr: errors.New("not a WireGuard interface")}, action: "up",
			want: []string{"stop " + unit, "reset-failed " + unit, "start " + unit},
		},
		{name: "down stops the unit", unit: fakeUnit{state: "active", interfaceUp: true}, action: "down", want: []string{"stop " + unit}},
		{name: "down ends a start in progress", unit: fakeUnit{state: "activating"}, action: "down", want: []string{"stop " + unit}},
		{name: "down of an interface that is down does nothing", unit: fakeUnit{state: "inactive"}, action: "down"},
		{
			name: "down clears a unit whose interface is gone", unit: fakeUnit{state: "active", stopErr: errors.New("not a WireGuard interface")}, action: "down",
			want: []string{"stop " + unit, "reset-failed " + unit},
		},
		{
			// Brought up with wg-quick by hand: the unit does not hold it.
			name: "down of an interface outside its unit runs wg-quick", unit: fakeUnit{state: "inactive", interfaceUp: true}, action: "down",
			want: []string{"wg-quick down wgtest"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newWireGuardTestEnv(t)
			tc.unit.install(t)
			if _, err := runWGQuick(context.Background(), tc.action, "wgtest"); err != nil {
				t.Fatalf("runWGQuick returned error: %v", err)
			}
			if !slices.Equal(tc.unit.calls, tc.want) {
				t.Fatalf("calls = %v, want %v", tc.unit.calls, tc.want)
			}
		})
	}
}

// systemd only says a unit failed; what wg-quick wrote is read from the
// unit's journal and returned with the error.
func TestInterfacePowerFailureCarriesTheUnitOutput(t *testing.T) {
	newWireGuardTestEnv(t)
	runWGQuickCommand = runWGQuick
	failed := errors.New("start unit wg-quick@wgtest.service: the unit is failed")
	unit := fakeUnit{state: "inactive", startErr: failed}
	unit.install(t)

	_, err := UpInterface(context.Background(), apischema.NameRequest{Name: "wgtest"})
	if !errors.Is(err, failed) || !strings.Contains(err.Error(), "wg-quick: said why") {
		t.Fatalf("UpInterface error = %v, want the unit's failure with its output", err)
	}

	unit = fakeUnit{state: "active", interfaceUp: true, stopErr: errors.New("stop unit wg-quick@wgtest.service: the unit is failed")}
	unit.install(t)
	_, err = DownInterface(context.Background(), apischema.NameRequest{Name: "wgtest"})
	if err == nil || !strings.Contains(err.Error(), "wg-quick: said why") {
		t.Fatalf("DownInterface error = %v, want the unit's failure with its output", err)
	}
}

// Where wg-quick is confined, the job host cannot run it for an interface its
// unit does not hold: the error says what the administrator runs instead.
func TestDownOutsideTheUnitSaysWhatToRun(t *testing.T) {
	newWireGuardTestEnv(t)
	runWGQuickCommand = runWGQuick
	unit := fakeUnit{state: "inactive", interfaceUp: true}
	unit.install(t)
	refused := errors.New("wg-quick down wgtest: exit status 1: /usr/bin/wg: Operation not permitted")
	units.direct = func(context.Context, string, string) (string, error) { return "", refused }

	_, err := DownInterface(context.Background(), apischema.NameRequest{Name: "wgtest"})
	if !errors.Is(err, refused) || !strings.Contains(err.Error(), `run "wg-quick down wgtest" as root`) {
		t.Fatalf("DownInterface error = %v, want wg-quick's refusal with what to run", err)
	}
}

// A cancelled start is systemd's and would carry on, so the unit is stopped.
func TestCancelledUpStopsTheUnit(t *testing.T) {
	newWireGuardTestEnv(t)
	runWGQuickCommand = runWGQuick
	ctx, cancel := context.WithCancel(context.Background())
	unit := fakeUnit{state: "inactive"}
	unit.install(t)
	units.start = func(context.Context, string) error {
		cancel()
		return ctx.Err()
	}
	units.queueStop = func(stopCtx context.Context, name string) error {
		if stopCtx.Err() != nil {
			t.Errorf("the stop was queued with the cancelled context")
		}
		unit.calls = append(unit.calls, "queue-stop "+name)
		return nil
	}

	_, err := UpInterface(ctx, apischema.NameRequest{Name: "wgtest"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("UpInterface error = %v, want context.Canceled", err)
	}
	if want := []string{"queue-stop wg-quick@wgtest.service"}; !slices.Equal(unit.calls, want) {
		t.Fatalf("calls = %v, want %v", unit.calls, want)
	}
}

// Removing an interface that cannot be taken down keeps its config, without
// which nothing could take it down later; one that is down loses its config
// and its unit is disabled, so it does not fail at every boot.
func TestRemoveInterfaceKeepsTheConfigOfAnInterfaceStillUp(t *testing.T) {
	env := newWireGuardTestEnv(t)
	writeTestInterface(t, "wgtest", "10.50.0.1/24")
	env.interfaceUp = true
	runWGQuickCommand = func(context.Context, string, string) (string, error) {
		return "", errors.New("PostDown failed")
	}

	_, err := RemoveInterface(context.Background(), apischema.NameRequest{Name: "wgtest"})
	if err == nil || !strings.Contains(err.Error(), "still up") {
		t.Fatalf("RemoveInterface error = %v, want a refusal for an interface still up", err)
	}
	assertPathExists(t, configPath("wgtest"))
	if len(env.disabled) != 0 {
		t.Fatalf("disabled units = %v, want none for an interface that stays", env.disabled)
	}

	// Down now, with a unit a failed stop left failed: the failure goes with
	// the interface.
	env.interfaceUp = false
	env.unitState = "failed"
	if _, err := RemoveInterface(context.Background(), apischema.NameRequest{Name: "wgtest"}); err != nil {
		t.Fatalf("RemoveInterface returned error: %v", err)
	}
	assertPathMissing(t, configPath("wgtest"))
	if !slices.Equal(env.disabled, []string{"wg-quick@wgtest.service"}) {
		t.Fatalf("disabled units = %v, want the interface's unit", env.disabled)
	}
	if !slices.Equal(env.unitResets, []string{"wg-quick@wgtest.service"}) {
		t.Fatalf("reset units = %v, want the interface's failed unit", env.unitResets)
	}
}

// A name is an argument of wg-quick and part of a unit name, so it holds only
// letters, digits, "_" and "-", and does not start with "-".
func TestValidateInterfaceName(t *testing.T) {
	for name, ok := range map[string]bool{
		"wg0": true, "wg-home_2": true, "_wg": true,
		"": false, "-wg0": false, "--help": false, "wg0.conf": false, "wg/0": false, "wg@0": false, "abcdefghijklmnop": false,
	} {
		if err := validateInterfaceName(name); (err == nil) != ok {
			t.Errorf("validateInterfaceName(%q) = %v, want ok=%v", name, err, ok)
		}
	}
}

// An up that is cancelled before anything was started stops nothing: the
// interface was already up, and a stop would take a running tunnel down.
func TestCancelledUpLeavesARunningInterfaceAlone(t *testing.T) {
	newWireGuardTestEnv(t)
	runWGQuickCommand = runWGQuick
	unit := fakeUnit{state: "active", interfaceUp: true}
	unit.install(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := UpInterface(ctx, apischema.NameRequest{Name: "wgtest"}); err != nil {
		t.Fatalf("UpInterface returned error: %v", err)
	}
	if len(unit.calls) != 0 {
		t.Fatalf("calls = %v, want none for an interface that is up", unit.calls)
	}
}
