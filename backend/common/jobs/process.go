package jobs

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ProcessStart returns the process start time (field 22 of /proc/<pid>/stat,
// in clock ticks since boot), which together with the pid identifies a process
// across pid reuse.
func ProcessStart(pid int32) (uint64, error) {
	_, start, err := processStat(pid)
	return start, err
}

// processStat returns the state (field 3 of /proc/<pid>/stat) and the start
// time of pid.
func processStat(pid int32) (string, uint64, error) {
	if pid <= 0 {
		return "", 0, fmt.Errorf("invalid pid %d", pid)
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(int(pid)) + "/stat")
	if err != nil {
		return "", 0, err
	}
	// The comm field may contain spaces and parentheses; fields resume after the last ')'.
	_, after, ok := bytes.CutLast(data, []byte{')'})
	if !ok {
		return "", 0, errors.New("malformed /proc stat: no comm terminator")
	}
	// The remainder starts at field 3, so field 22 is index 19.
	fields := strings.Fields(string(after))
	if len(fields) < 20 {
		return "", 0, errors.New("malformed /proc stat: too few fields")
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("parse start time of pid %d: %w", pid, err)
	}
	return fields[0], start, nil
}

// BootID returns the kernel's id of the running boot.
func BootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// ProcessAlive reports whether the executor's pid exists with the recorded
// start time and has not exited: a zombie waiting for its parent is dead.
func ProcessAlive(e Executor) bool {
	state, start, err := processStat(e.PID)
	return err == nil && start == e.PIDStart && state != "Z" && state != "X"
}
