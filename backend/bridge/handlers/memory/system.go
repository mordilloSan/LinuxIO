package memory

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// hostSystem is every side effect the memory writes have, so tests drive
// them with a fake: systemd unit jobs and unit files, swapoff, files under
// /etc, kernel parameters under /proc/sys and /sys, and a fresh status
// read. Unit starts and stops queue a job and return; callers verify the
// result in /proc/swaps.
type hostSystem interface {
	status(ctx context.Context) (apischema.SwapStatus, error)
	startUnit(ctx context.Context, name string) error
	stopUnit(ctx context.Context, name string) error
	// restartUnit stops and starts a unit as one job; on a unit that is not
	// running it is a start.
	restartUnit(ctx context.Context, name string) error
	// resetZram frees the zram device name, which nothing may swap on or
	// hold open, so that its size can be set again; it tries once, and the
	// kernel refuses with EBUSY while something holds the device.
	resetZram(ctx context.Context, name string) error
	// swapOff takes the swap file or partition at device, a path from
	// /proc/swaps, away with swapoff and returns when it has left. It ends
	// early only when ctx does, and the device then swaps on.
	swapOff(ctx context.Context, device string) error
	enableUnit(ctx context.Context, name string) error
	disableUnit(ctx context.Context, name string) error
	maskUnit(ctx context.Context, name string) error
	unmaskUnit(ctx context.Context, name string) error
	unitFileState(ctx context.Context, name string) (string, error)
	// reload makes systemd run its generators again and returns when it has.
	reload(ctx context.Context) error
	activeState(ctx context.Context, name string) (string, error)
	writeFile(name string, data []byte, mode fs.FileMode) error
	removeFile(name string) error
	writeParam(name, value string) error
	readParam(name string) (string, error)
}

// liveSystem is the host: systemd over D-Bus and files under root.
type liveSystem struct {
	root hostRoot
}

func (s liveSystem) status(ctx context.Context) (apischema.SwapStatus, error) {
	return s.root.status(ctx)
}

func (liveSystem) startUnit(ctx context.Context, name string) error {
	return systemd.StartUnit(ctx, name)
}

func (liveSystem) stopUnit(ctx context.Context, name string) error {
	return systemd.StopUnit(ctx, name)
}

func (liveSystem) restartUnit(ctx context.Context, name string) error {
	return systemd.RestartUnit(ctx, name)
}

// zramResetWait is how long a reset of zram0 is tried again while something
// holds the device open, as udev does for a moment after a swapoff.
const zramResetWait = 5 * time.Second

// resetZram writes the device's reset attribute once. The kernel refuses it
// with EBUSY while the device is open; the executor tries again. A device
// that does not exist needs no reset.
func (s liveSystem) resetZram(_ context.Context, name string) error {
	err := s.writeParam(filepath.Join(sysBlockDir, name, "reset"), "1")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (s liveSystem) swapOff(ctx context.Context, device string) error {
	return swapOff(ctx, s.root.path(device))
}

func (liveSystem) enableUnit(ctx context.Context, name string) error {
	return systemd.EnableUnit(ctx, name)
}

func (liveSystem) disableUnit(ctx context.Context, name string) error {
	return systemd.DisableUnit(ctx, name)
}

func (liveSystem) maskUnit(ctx context.Context, name string) error {
	return systemd.MaskUnit(ctx, name)
}

// unmaskUnit also reloads the manager, which brings back the unit
// systemd-fstab-generator makes for a swap entry.
func (liveSystem) unmaskUnit(ctx context.Context, name string) error {
	return systemd.UnmaskUnit(ctx, name)
}

func (liveSystem) unitFileState(ctx context.Context, name string) (string, error) {
	return systemd.GetUnitFileState(ctx, name)
}

func (liveSystem) reload(ctx context.Context) error {
	return systemd.DaemonReload(ctx)
}

func (liveSystem) activeState(ctx context.Context, name string) (string, error) {
	return systemd.GetActiveState(ctx, name)
}

func (s liveSystem) writeFile(name string, data []byte, mode fs.FileMode) error {
	return s.root.writeFile(name, data, mode)
}

func (s liveSystem) removeFile(name string) error {
	return s.root.removeFile(name)
}

// writeParam writes a kernel parameter in place; it never creates one.
func (s liveSystem) writeParam(name, value string) error {
	f, err := os.OpenFile(s.root.path(name), os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(value); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func (s liveSystem) readParam(name string) (string, error) {
	data, err := os.ReadFile(s.root.path(name))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
