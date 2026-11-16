package memory

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

// swapoffCommand is util-linux's swapoff, the command systemd runs itself
// to stop a swap unit.
const swapoffCommand = "swapoff"

// swapoffWaitDelay bounds how long a killed swapoff's output is waited for.
const swapoffWaitDelay = 5 * time.Second

// swapOff runs swapoff on the absolute path of a swap device and returns
// when the kernel has taken the device away. The kernel reads every page on
// the device back first, which takes as long as the disk needs: nothing here
// bounds it, and ctx is the only way out.
//
// It is a process of its own and not a swapoff(2) call from this one for
// what the kernel does to the caller: while the pages come back it is the
// first process the kernel kills when memory runs out (set_current_oom_origin
// in mm/swapfile.c). That has to be a helper and not the job host, which
// runs every job. A helper killed that way, or by ctx ending, leaves the
// device swapping: swapoff(2) gives up when a signal is pending and the
// kernel puts the device back, with the pages it had read in memory.
func swapOff(ctx context.Context, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%q is not the absolute path of a swap device", path)
	}
	name, err := hostcmd.LookPath(swapoffCommand)
	if err != nil {
		return err
	}
	args := []string{"--", path}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = swapoffWaitDelay
	output, err := cmd.CombinedOutput()
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return hostcmd.OutputError(swapoffCommand, args, output, err)
}
