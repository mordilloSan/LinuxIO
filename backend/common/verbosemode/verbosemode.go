// Package verbosemode owns the systemd drop-ins that persist debug logging
// across daemon restarts.
package verbosemode

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/logging"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const reloadTimeout = 30 * time.Second

var dropinRoot = "/etc/systemd/system"

// reloadUnit is a variable so tests can observe the reload order.
var reloadUnit = systemd.ReloadOrTryRestartUnit

type dropin struct {
	unit    string
	content string
}

var dropins = []dropin{
	{
		unit: "linuxio-webserver.service",
		content: `[Service]
ExecStart=
ExecStart=` + version.BinDir + `/linuxio-webserver run -verbose
`,
	},
	{
		unit: "linuxio-indexer.service",
		content: `[Service]
ExecStart=
ExecStart=` + version.BinDir + `/linuxio-indexer --config-file ` + version.ConfigDir + `/indexer/config.yaml --verbose
`,
	},
	{
		unit: "linuxio-monitoring.service",
		content: `[Service]
ExecStart=
ExecStart=` + version.BinDir + `/linuxio-monitoring run --config ` + version.ConfigDir + `/monitoring/config.yaml --verbose
`,
	},
}

func (d dropin) path() string {
	return filepath.Join(dropinRoot, d.unit+".d", "verbose.conf")
}

// Paths lists the drop-in files.
func Paths() []string {
	paths := make([]string, len(dropins))
	for i, d := range dropins {
		paths[i] = d.path()
	}
	return paths
}

// Enabled reports whether every drop-in is present.
func Enabled() bool {
	for _, d := range dropins {
		if _, err := os.Stat(d.path()); err != nil {
			return false
		}
	}
	return true
}

// Partial reports whether any drop-in is present.
func Partial() bool {
	for _, d := range dropins {
		if _, err := os.Stat(d.path()); err == nil {
			return true
		}
	}
	return false
}

// Enable writes the drop-ins and reloads the systemd manager.
func Enable(ctx context.Context) error {
	for _, d := range dropins {
		if err := os.MkdirAll(filepath.Dir(d.path()), 0o755); err != nil {
			return fmt.Errorf("create drop-in directory: %w", err)
		}
		if err := os.WriteFile(d.path(), []byte(d.content), 0o644); err != nil {
			return fmt.Errorf("write drop-in %s: %w", d.path(), err)
		}
	}
	return daemonReload(ctx)
}

// Disable removes the drop-ins and reloads the systemd manager.
func Disable(ctx context.Context) error {
	for _, d := range dropins {
		if err := os.Remove(d.path()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove drop-in %s: %w", d.path(), err)
		}
	}
	return daemonReload(ctx)
}

// ReloadDaemons signals each running daemon to re-read the drop-ins (SIGHUP via
// ExecReload). Socket-activated daemons that are stopped pick the state up at
// their next start. Every daemon is signalled even when one fails, and the
// webserver goes last: the caller may be a bridge it would tear down if its
// unit has to restart.
func ReloadDaemons(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, reloadTimeout)
	defer cancel()
	var errs []error
	for _, d := range slices.Backward(dropins) { // dropins lists the webserver first
		if err := reloadUnit(ctx, d.unit); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func daemonReload(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, reloadTimeout)
	defer cancel()
	return systemd.DaemonReload(ctx)
}

// ReloadOnSIGHUP re-reads the persisted state into the logger on every SIGHUP
// until ctx is cancelled. The handler is registered before it returns, so an
// early SIGHUP never takes the default action.
func ReloadOnSIGHUP(ctx context.Context) {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		defer signal.Stop(hup)
		for {
			select {
			case <-ctx.Done():
				return
			case <-hup:
				logging.SetDebug(Enabled())
			}
		}
	}()
}
