package memory

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

// util-linux's mkswap, which writes a swap header and, where SELinux is on,
// labels a file swapfile_t, and swapon, which systemd runs itself to start a
// swap unit.
const (
	mkswapCommand = "mkswap"
	swaponCommand = "swapon"
)

// swapOn runs "swapon -- path" and returns when the command has. options is
// the fourth field of the file's /etc/fstab line, or empty for a file
// without one: a pri= or discard option in it becomes swapon's -p or
// --discard, as systemd passes them at boot, and every other option is
// ignored. A pri= that is not a priority, or a discard policy swapon does not
// have, is refused rather than passed on. The path is given to swapon only
// after every folder on it, and the file, are found to be root's alone
// (openRootOnlyFolder, statSwapFileAt), so that its own lookup lands where
// the check did. The caller waits for path in /proc/swaps.
func (h hostRoot) swapOn(ctx context.Context, path, options string) error {
	if err := checkSwapFilePath(path); err != nil {
		return err
	}
	flags, err := swapOnFlags(path, options)
	if err != nil {
		return err
	}
	dir, err := h.openRootOnlyFolder(filepath.Dir(path))
	if err != nil {
		return err
	}
	_, err = h.statSwapFileAt(dir, filepath.Base(path), path)
	_ = dir.Close()
	if err != nil {
		return err
	}
	return runSwapTool(ctx, swaponCommand, append(flags, "--", h.path(path))...)
}

// swapOnFlags turns the options of the fstab line of path into swapon's
// flags: pri=<n> into -p <n>, discard into --discard, and discard=once and
// discard=pages into --discard=<policy>.
func swapOnFlags(path, options string) ([]string, error) {
	var priority, discard string
	for option := range strings.SplitSeq(options, ",") {
		key, value, hasValue := strings.Cut(option, "=")
		switch key {
		case "pri":
			// The kernel takes -1, the default, to 32767.
			n, err := strconv.Atoi(value)
			if !hasValue || err != nil || n < -1 || n > 32767 || priority != "" {
				return nil, fmt.Errorf("%w: the fstab line of %s has %q, which is not one swap priority", bridgeipc.ErrConflict, path, option)
			}
			priority = strconv.Itoa(n)
		case "discard":
			if (hasValue && value != "once" && value != "pages") || discard != "" {
				return nil, fmt.Errorf("%w: the fstab line of %s has %q, which is not one discard policy swapon has", bridgeipc.ErrConflict, path, option)
			}
			discard = "--discard"
			if hasValue {
				discard += "=" + value
			}
		}
	}
	var flags []string
	if priority != "" {
		flags = append(flags, "-p", priority)
	}
	if discard != "" {
		flags = append(flags, discard)
	}
	return flags, nil
}

// runSwapTool runs one of the swap commands, found as hostcmd.LookPath finds
// it, with args as separate arguments, and returns what it printed with its
// failure. A cancelled ctx kills it and is what is returned.
func runSwapTool(ctx context.Context, command string, args ...string) error {
	name, err := hostcmd.LookPath(command)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = swapoffWaitDelay
	output, err := cmd.CombinedOutput()
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return hostcmd.OutputError(command, args, output, err)
}
