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
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

var (
	runWGQuickCommand    = runWGQuick
	isInterfaceUpFunc    = isInterfaceUp
	interfaceGoneFunc    = interfaceGone
	deviceRunsConfigFunc = deviceRunsConfig
	units                = unitControl{
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
		if err := startInterface(ctx, name); err != nil {
			return "", err
		}
		if err := ensureInterfaceForwards(name); err != nil {
			// An up leaves a working interface or none: callers read an
			// error as "it is down".
			stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
			defer cancel()
			if _, stopErr := stopInterface(stopCtx, name); stopErr != nil {
				slog.Warn("failed to take down an interface that does not forward", "interface", name, "error", stopErr)
			}
			return "", err
		}
		return "", nil
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
		// Starting a unit that is already active does nothing, whatever the
		// unit holds: the interface, up on this config; nothing, the
		// interface having been taken away by hand; or an interface under
		// another key, left running when its config was deleted or replaced.
		switch {
		case interfaceGoneFunc(name):
			clearStaleUnit(ctx, name)
		case deviceRunsConfigFunc(name):
			return nil
		default:
			if err = stopOtherKeysInterface(ctx, name); err != nil {
				return err
			}
		}
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

// stopOtherKeysInterface takes down the interface a unit holds under another
// key than its config's, so that the start which follows runs the config on
// disk. The unit's stop runs wg-quick down, which deletes the link and then
// runs the hooks of the config on disk: the PostDown lines of the config that
// interface came up on are gone with it and never run. A stop that failed
// after the link was deleted, in a PostDown line, has done what is needed
// here, and the unit it left failed is reset.
func stopOtherKeysInterface(ctx context.Context, name string) error {
	unit := unitName(name)
	since := time.Now()
	if err := units.stop(ctx, unit); err != nil {
		if !interfaceGoneFunc(name) {
			return withUnitOutput(ctx, unit, since, fmt.Errorf("take down the interface running as %s under another key than its config: %w", name, err))
		}
		slog.Debug("the stop of an interface under another key failed after it took the interface down", "component", "wireguard", "interface", name, "unit", unit, "error", err)
		if resetErr := units.resetFailed(ctx, unit); resetErr != nil {
			slog.Warn("failed to reset interface unit", "interface", name, "unit", unit, "error", resetErr)
		}
	}
	slog.Info("took down a WireGuard interface that ran under another key than its config", "interface", name, "unit", unit)
	return nil
}

// deviceRunsConfig reports whether the WireGuard interface that is up under a
// name is the one its config describes: its public key is the one the
// config's private key gives. An interface with another key was left running
// when its config was deleted or replaced by hand or by a LinuxIO from before
// a delete refused an interface it could not take down; its unit is still
// active, and no client of the config on disk completes a handshake with it.
// Where either side cannot be read the answer is true: an interface that
// works is not taken down on a guess.
func deviceRunsConfig(name string) bool {
	cfg, err := ParseWireGuardConfig(configPath(name))
	if err != nil {
		slog.Debug("no config to compare a running interface with", "component", "wireguard", "interface", name, "error", err)
		return true
	}
	private, err := wgtypes.ParseKey(cfg.PrivateKey)
	if err != nil {
		slog.Debug("no private key to compare a running interface with", "component", "wireguard", "interface", name, "error", err)
		return true
	}
	client, err := wgctrl.New()
	if err != nil {
		slog.Debug("failed to create wgctrl client", "component", "wireguard", "interface", name, "error", err)
		return true
	}
	defer client.Close()
	dev, err := client.Device(name)
	if err != nil {
		slog.Debug("failed to read a running interface's key", "component", "wireguard", "interface", name, "error", err)
		return true
	}
	return keyMatches(private, dev.PublicKey)
}

// keyMatches reports whether public is the public key of private. An
// interface with no private key has the zero public key, which is no key's.
func keyMatches(private, public wgtypes.Key) bool {
	return private.PublicKey() == public
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
