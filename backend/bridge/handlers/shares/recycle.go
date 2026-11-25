package shares

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"slices"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// The recycle bins of the shared folders are emptied by linuxio-worker
// (docs/shared-folders.md, "Network"), run daily by the package unit
// recycleServiceName. Its timer is written and enabled here, as the snapshot
// timer is by the storage handlers, and runs only while a folder has a bin.

const (
	recycleTimerName   = "linuxio-shares-recycle.timer"
	recycleServiceName = "linuxio-shares-recycle.service"
)

// recycleTimerPath is a var so tests can write the timer to a temp dir.
var recycleTimerPath = "/etc/systemd/system/" + recycleTimerName

type recycleTimerOps struct {
	daemonReload func(context.Context) error
	enableUnit   func(context.Context, string) error
	startUnit    func(context.Context, string) error
	stopUnit     func(context.Context, string) error
	disableUnit  func(context.Context, string) error
}

// recycleTimer is a var so tests can stand in for systemd.
var recycleTimer = recycleTimerOps{
	daemonReload: systemd.DaemonReload,
	enableUnit:   systemd.EnableUnit,
	startUnit:    systemd.StartUnit,
	stopUnit:     systemd.StopUnit,
	disableUnit:  systemd.DisableUnit,
}

// renderRecycleTimer is the timer's text, which depends on no setting: once
// a day, at a time spread over an hour, and at the next start when the host
// was off at that time.
func renderRecycleTimer() []byte {
	return []byte("[Unit]\nDescription=Empty the recycle bins of LinuxIO's shared folders\n\n[Timer]\nOnCalendar=daily\nRandomizedDelaySec=1h\nPersistent=true\nUnit=" +
		recycleServiceName + "\n\n[Install]\nWantedBy=timers.target\n")
}

// syncRecycleTimer runs the timer while a folder of folders has its recycle
// bin on, and stops it when none has. A host that never had a bin has no
// timer written, and systemd is not asked anything.
func syncRecycleTimer(ctx context.Context, folders []apischema.SharedFolder) error {
	if slices.ContainsFunc(folders, func(f apischema.SharedFolder) bool { return f.RecycleBin }) {
		if err := atomicfile.Write(recycleTimerPath, renderRecycleTimer(), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", recycleTimerPath, err)
		}
		if err := recycleTimer.daemonReload(ctx); err != nil {
			return fmt.Errorf("reload systemd: %w", err)
		}
		if err := recycleTimer.enableUnit(ctx, recycleTimerName); err != nil {
			return fmt.Errorf("enable %s: %w", recycleTimerName, err)
		}
		if err := recycleTimer.startUnit(ctx, recycleTimerName); err != nil {
			return fmt.Errorf("start %s: %w", recycleTimerName, err)
		}
		return nil
	}
	if _, err := os.Stat(recycleTimerPath); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err := recycleTimer.stopUnit(ctx, recycleTimerName); err != nil {
		return fmt.Errorf("stop %s: %w", recycleTimerName, err)
	}
	if err := recycleTimer.disableUnit(ctx, recycleTimerName); err != nil {
		return fmt.Errorf("disable %s: %w", recycleTimerName, err)
	}
	return nil
}

// recycleBinsChanged brings the timer in step with the record after a
// folder was made, changed or deleted. The folder's change is done by then
// and is not undone when this fails: the bins work, and are emptied once a
// later change of a folder sets the timer right. The failure is logged.
func recycleBinsChanged(ctx context.Context) {
	folders, err := loadFolders()
	if err == nil {
		err = syncRecycleTimer(ctx, folders)
	}
	if err != nil {
		slog.Warn("failed to set the timer that empties the recycle bins", "error", err)
	}
}
