package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// maxSwappiness is the kernel's ceiling since 5.8; above 100 the kernel
// prefers swapping out anonymous pages to dropping the file cache.
const maxSwappiness = 200

// SetSwappiness writes vm.swappiness live and into the sysctl drop-in.
func SetSwappiness(ctx context.Context, value int) (apischema.SwapStatus, error) {
	return newExecutor(nil).setSwappiness(ctx, value)
}

func (e *executor) setSwappiness(ctx context.Context, value int) (apischema.SwapStatus, error) {
	if value < 0 || value > maxSwappiness {
		return apischema.SwapStatus{}, fmt.Errorf("%w: swappiness must be from 0 to %d, got %d", bridgeipc.ErrInvalidArgs, maxSwappiness, value)
	}
	unlock, err := lockMutation()
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	defer unlock()
	status, err := e.sys.status(ctx)
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	if status.TunedActive {
		return apischema.SwapStatus{}, fmt.Errorf("%w: the TuneD profile %s sets swappiness", bridgeipc.ErrConflict, status.TunedProfile)
	}
	param := filepath.Join(vmSysctlDir, sysctlSwappiness)
	previous, err := e.sys.readParam(param)
	if err != nil {
		return apischema.SwapStatus{}, fmt.Errorf("read %s: %w", param, err)
	}
	// The drop-in is saved before anything changes: its write can land and
	// still fail, and putting it back is harmless when it did not land.
	savedDropIn, err := e.root.saveFile(sysctlDropInPath)
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	// A failed write may still have landed, from an error at close; putting
	// the previous value back is harmless when it did not.
	if err := e.sys.writeParam(param, strconv.Itoa(value)); err != nil {
		err = fmt.Errorf("write %s: %w", param, err)
		return apischema.SwapStatus{}, errors.Join(err, e.restoreSwappiness(param, previous))
	}
	if err := e.persistSwappiness(param, value); err != nil {
		return apischema.SwapStatus{}, errors.Join(err, e.restoreSwappiness(param, previous), savedDropIn.restore(e.sys))
	}
	slog.Info("swappiness changed", "swappiness", value, "previous", previous)
	return e.sys.status(ctx)
}

// persistSwappiness checks that the live value reads back, then writes it
// into the drop-in, keeping every other key the drop-in holds.
func (e *executor) persistSwappiness(param string, value int) error {
	got, err := e.sys.readParam(param)
	if err != nil {
		return fmt.Errorf("read %s: %w", param, err)
	}
	if want := strconv.Itoa(value); got != want {
		return fmt.Errorf("%s reads %s after writing %s", param, got, want)
	}
	entries, err := e.root.readSysctlDropIn()
	if err != nil {
		return err
	}
	data := renderSysctlDropIn(mergeSysctlDropIn(entries, []sysctlSetting{{name: sysctlSwappiness, value: value}}, nil))
	if err := e.sys.writeFile(sysctlDropInPath, data, managedFileMode); err != nil {
		return fmt.Errorf("write %s: %w", sysctlDropInPath, err)
	}
	return nil
}

// restoreSwappiness writes the previous live value back.
func (e *executor) restoreSwappiness(param, previous string) error {
	if err := e.sys.writeParam(param, previous); err != nil {
		return fmt.Errorf("restore %s to %s: %w", param, previous, err)
	}
	return nil
}
