package wireguard

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

var (
	runWGQuickCommand = runWGQuick
	isInterfaceUpFunc = isInterfaceUp
	interfaceGoneFunc = interfaceGone
	units             = unitControl{
		activeState: systemd.GetActiveState,
		start:       systemd.StartUnitAndWait,
		stop:        systemd.StopUnitAndWait,
		queueStop:   systemd.StopUnit,
		resetFailed: systemd.ResetFailedUnit,
		disable:     systemd.DisableUnit,
		output:      lastUnitOutput,
		direct:      runWGQuickDirect,
	}
)

// unitControl is how the package reaches an interface's unit and, for an
// interface its unit does not hold, wg-quick itself. Tests replace it.
type unitControl struct {
	activeState func(ctx context.Context, unit string) (string, error)
	// start and stop return when the unit's job is over.
	start, stop func(ctx context.Context, unit string) error
	// queueStop returns once the stop is queued.
	queueStop, resetFailed, disable func(ctx context.Context, unit string) error
	// output is what the unit wrote to the journal since a time.
	output func(ctx context.Context, unit string, since time.Time) string
	direct func(ctx context.Context, action, name string) (string, error)
}

// unitName is the unit wireguard-tools ships for an interface: wg-quick up on
// start and wg-quick down on stop.
func unitName(name string) string {
	return "wg-quick@" + name + ".service"
}

func UpInterface(ctx context.Context, req apischema.NameRequest) (any, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("usage: up_interface <name>")
	}
	name := req.Name
	if err := validateInterfaceName(name); err != nil {
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	out, err := runWGQuickCommand(ctx, "up", name)
	if err != nil {
		return nil, fmt.Errorf("bring up interface: %w", err)
	}

	slog.Info("WireGuard interface brought up", "interface", name)
	return map[string]any{
		"status": "on",
		"output": out,
	}, nil
}

func DownInterface(ctx context.Context, req apischema.NameRequest) (any, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("usage: down_interface <name>")
	}

	name := req.Name
	if err := validateInterfaceName(name); err != nil {
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	out, err := runWGQuickCommand(ctx, "down", name)
	if err != nil {
		return nil, fmt.Errorf("bring down interface: %w", err)
	}
	slog.Info("WireGuard interface brought down", "interface", name)
	return map[string]any{
		"status": "off",
		"output": out,
	}, nil
}

func bringUpInterface(ctx context.Context, name string) error {
	_, err := runWGQuickCommand(ctx, "up", name)
	if err != nil {
		return fmt.Errorf("bring up interface: %w", err)
	}
	return nil
}

// runWGQuick brings an interface up or down through its unit, so wg-quick
// runs as systemd starts it at boot and the unit's state follows the
// interface. Started from the job host instead, wg-quick inherits the host's
// NoNewPrivileges, under which the AppArmor profile Ubuntu confines it with
// cannot run ip and wg under theirs: every call fails with EPERM. Every up
// goes through here, so this is where forwarding is turned on for an
// interface that needs it.
func runWGQuick(ctx context.Context, action, name string) (string, error) {
	if action == "up" {
		if err := ensureForwarding(name); err != nil {
			return "", err
		}
		return "", startInterface(ctx, name)
	}
	return stopInterface(ctx, name)
}

func startInterface(ctx context.Context, name string) error {
	unit := unitName(name)
	state, err := units.activeState(ctx, unit)
	if err != nil {
		return err
	}
	if state == "active" {
		// Starting a unit that is already active does nothing: either the
		// interface is up, or the unit is left over from one that is gone.
		if !interfaceGoneFunc(name) {
			return nil
		}
		clearStaleUnit(ctx, name)
	}
	since := time.Now()
	err = units.start(ctx, unit)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		// The start is systemd's and carries on after a cancel, so it is
		// stopped. A stop that finds wg-quick up still running ends it with
		// SIGTERM, on which wg-quick deletes the link; PostUp lines that had
		// already run are not undone, as when wg-quick is interrupted by hand.
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if stopErr := units.queueStop(stopCtx, unit); stopErr != nil {
			slog.Warn("failed to stop interface unit after a cancelled start", "interface", name, "unit", unit, "error", stopErr)
		}
		return err
	}
	return withUnitOutput(ctx, unit, since, err)
}

// stopInterface takes the interface down. One that is already down, with a
// unit that does not hold it, is left as it is.
func stopInterface(ctx context.Context, name string) (string, error) {
	unit := unitName(name)
	state, err := units.activeState(ctx, unit)
	if err != nil {
		return "", err
	}
	switch state {
	case "inactive", "failed", "unknown":
	case "active":
		if interfaceGoneFunc(name) {
			clearStaleUnit(ctx, name)
			return "", nil
		}
		fallthrough
	default:
		since := time.Now()
		if err = units.stop(ctx, unit); err != nil {
			return "", withUnitOutput(ctx, unit, since, err)
		}
	}
	if !isInterfaceUpFunc(name) {
		return "", nil
	}
	// The unit does not hold it: an interface brought up with wg-quick by
	// hand or by a LinuxIO from before interfaces went through their unit,
	// or one a failed stop left up. Only wg-quick itself can take it down,
	// and where its profile refuses that from here, the administrator runs it.
	out, err := units.direct(ctx, "down", name)
	if err != nil {
		return out, fmt.Errorf("interface %s is up and %s, which is %s, does not hold it, so it could not be taken down from here, run \"wg-quick down %s\" as root: %w", name, unit, state, name, err)
	}
	return out, nil
}

// clearStaleUnit stops a unit that is active for an interface that is gone,
// as one taken down with wg-quick by hand. Its stop command fails, there
// being no interface to take down, and leaves the unit failed, which is
// cleared so that it does not stay listed as a failure.
func clearStaleUnit(ctx context.Context, name string) {
	unit := unitName(name)
	err := units.stop(ctx, unit)
	if err == nil {
		return
	}
	slog.Debug("stale interface unit stopped with an error", "component", "wireguard", "interface", name, "unit", unit, "error", err)
	if err := units.resetFailed(ctx, unit); err != nil {
		slog.Warn("failed to reset stale interface unit", "interface", name, "unit", unit, "error", err)
	}
}

// interfaceGone reports whether no network interface has the name. Unlike
// isInterfaceUp, which is also false when WireGuard cannot be asked, it is
// true only for an interface that is definitely not there, which is what a
// unit is stopped on.
func interfaceGone(name string) bool {
	_, err := os.Lstat("/sys/class/net/" + name)
	return errors.Is(err, fs.ErrNotExist)
}

// withUnitOutput adds what the unit wrote to the journal since the job was
// asked for to err: with systemd running wg-quick, that is where wg-quick
// says why it failed.
func withUnitOutput(ctx context.Context, unit string, since time.Time, err error) error {
	if out := units.output(ctx, unit, since); out != "" {
		return fmt.Errorf("%w: %s", err, out)
	}
	return err
}

// lastUnitOutput returns the last lines the unit's current invocation wrote
// to the journal since a time, and "" when they cannot be read. The time
// leaves out the lines of the start when a stop fails days later. --sync
// makes the lines just written readable.
func lastUnitOutput(ctx context.Context, unit string, since time.Time) string {
	id, err := systemd.GetInvocationID(ctx, unit)
	if err != nil || id == "" {
		slog.Debug("no invocation to read interface unit output from", "component", "wireguard", "unit", unit, "error", err)
		return ""
	}
	journalctl, err := hostcmd.LookPath("journalctl")
	if err != nil {
		slog.Debug("journalctl not found", "component", "wireguard", "unit", unit, "error", err)
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	syncOut, err := rootCommand(ctx, journalctl, "--sync").CombinedOutput()
	if err != nil {
		slog.Debug("journal sync failed", "component", "wireguard", "unit", unit, "error", hostcmd.OutputError("journalctl", []string{"--sync"}, syncOut, err))
	}
	// A second earlier: the journal's stamps and this clock need not agree
	// to the microsecond.
	sinceArg := "--since=@" + strconv.FormatInt(since.Unix()-1, 10)
	args := []string{"--no-pager", "--quiet", "--all", "-o", "cat", "-n", "40", sinceArg, "_SYSTEMD_INVOCATION_ID=" + id}
	out, err := rootCommand(ctx, journalctl, args...).CombinedOutput()
	if err != nil {
		slog.Warn("failed to read interface unit output", "unit", unit, "error", hostcmd.OutputError("journalctl", args, out, err))
		return ""
	}
	return strings.TrimSpace(string(out))
}

// runWGQuickDirect runs wg-quick from this process, for an interface its
// unit does not hold. It fails where wg-quick is confined (see runWGQuick).
func runWGQuickDirect(ctx context.Context, action, name string) (string, error) {
	wgQuickPath, err := exec.LookPath("wg-quick")
	if err != nil {
		return "", fmt.Errorf("wg-quick not found: %w", err)
	}

	cmd := rootCommand(ctx, wgQuickPath, action, name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), hostcmd.OutputError("wg-quick", []string{action, name}, out, err)
	}
	return string(out), nil
}

func rootCommand(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: 0, Gid: 0},
	}
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	cmd.Dir = "/"
	return cmd
}

func isInterfaceUp(name string) bool {
	client, err := wgctrl.New()
	if err != nil {
		slog.Debug("failed to create wgctrl client", "component", "wireguard", "interface", name, "error", err)
		return false
	}
	defer client.Close()

	_, err = client.Device(name)
	return err == nil
}
