package memory

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
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
//
// A path someone other than root could redirect is refused (rootOnlyPath).
func swapOff(ctx context.Context, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%q is not the absolute path of a swap device", path)
	}
	if err := rootOnlyPath(path); err != nil {
		return err
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

// errOthersCanRedirect says that someone other than root can put another
// entry at the path of a swap device.
var errOthersCanRedirect = errors.New("users other than root can change a folder on its path")

// rootOnlyPath refuses the path of a swap device that someone other than
// root could put another entry at. swapoff(2) looks the path up again, so in
// a folder another user can write, a link at the path, or at the name a
// deleted file is listed under, sends the call to another active swap
// device, which then leaves without the room check that was made for this
// one. Every folder on the path must be a folder, not a link, that root owns
// and no group or everyone may write: /proc/swaps names a device by its real
// path, so a link among them was put there since. A folder that is missing
// ends the walk: nobody but root can make it, and swapoff reports it.
func rootOnlyPath(path string) error {
	return foldersOnlyOf(0, "/", path)
}

// foldersOnlyOf is rootOnlyPath for the folders from base down to the one
// path sits in, with uid as their owner.
func foldersOnlyOf(uid uint32, base, path string) error {
	rel, err := filepath.Rel(base, filepath.Dir(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("%q is not below %q", path, base)
	}
	folders := []string{base}
	if rel != "." {
		for name := range strings.SplitSeq(rel, "/") {
			folders = append(folders, filepath.Join(folders[len(folders)-1], name))
		}
	}
	for _, dir := range folders {
		info, err := os.Lstat(dir)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !info.IsDir() || !ok || st.Uid != uid || info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("%w: %s is left swapping: %w (%s), so swapoff could take another device away", bridgeipc.ErrConflict, path, errOthersCanRedirect, dir)
		}
	}
	return nil
}
