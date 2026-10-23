package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const btrfsTimerBinary = version.BinDir + "/linuxio-btrfs-timer"

// shortcut: hosts upgraded from earlier releases still have units/drop-ins that call these verbs, remove after one release
// btrfsTimerArgs maps the old hidden verbs onto linuxio-btrfs-timer arguments.
func btrfsTimerArgs(verb string, args []string) []string {
	sub := map[string]string{"btrfs-snapshot": "snapshot", "btrfs-health": "health"}[verb]
	return append([]string{btrfsTimerBinary, sub}, args...)
}

func runBtrfsCompat(verb string, args []string) int {
	argv := btrfsTimerArgs(verb, args)
	fmt.Fprintf(os.Stderr, "linuxio %s is deprecated; use %s (re-save the btrfs schedule or auto-update notify setting to rewrite the unit)\n", verb, btrfsTimerBinary)
	err := syscall.Exec(argv[0], argv, os.Environ())
	fmt.Fprintf(os.Stderr, "exec %s: %v\n", argv[0], err)
	return 1
}
