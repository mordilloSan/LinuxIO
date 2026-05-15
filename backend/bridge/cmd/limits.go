package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// logBridgeResourceLimits records the process and cgroup limits that shape how
// much work this bridge instance can safely take on.
func logBridgeResourceLimits() {
	logResourceLimit("nofile", syscall.RLIMIT_NOFILE, "files")
	logCgroupLimit("tasks", "pids.max", "processes")
	logCgroupLimit("memory", "memory.max", "bytes")
}

// logResourceLimit reads and logs one POSIX rlimit for the bridge process.
func logResourceLimit(name string, resource int, units string) {
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(resource, &limit); err != nil {
		slog.Debug("failed to read bridge resource limit",
			"resource", name,
			"error", err)
		return
	}
	soft := formatResourceLimit(limit.Cur)
	hard := formatResourceLimit(limit.Max)
	slog.Info(fmt.Sprintf("bridge resource limit resource=%s soft=%s hard=%s units=%s", name, soft, hard, units),
		"resource", name,
		"soft", soft,
		"hard", hard,
		"units", units)
}

// formatResourceLimit formats an rlimit value for logs, including infinity.
func formatResourceLimit(value uint64) string {
	if value == ^uint64(0) {
		return "infinity"
	}
	return strconv.FormatUint(value, 10)
}

// logCgroupLimit reads and logs one cgroup v2 limit for the bridge process.
func logCgroupLimit(name, filename, units string) {
	value, err := readUnifiedCgroupLimit(filename)
	if err != nil {
		slog.Debug("failed to read bridge cgroup limit",
			"resource", name,
			"file", filename,
			"error", err)
		return
	}
	limit := formatCgroupLimit(value)
	slog.Info(fmt.Sprintf("bridge cgroup limit resource=%s limit=%s units=%s", name, limit, units),
		"resource", name,
		"limit", limit,
		"units", units)
}

// readUnifiedCgroupLimit reads a limit file from the current unified cgroup.
func readUnifiedCgroupLimit(filename string) (string, error) {
	cgroupPath, err := unifiedCgroupPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join("/sys/fs/cgroup", cgroupPath, filename))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// unifiedCgroupPath returns the cgroup v2 path for the current bridge process.
func unifiedCgroupPath() (string, error) {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.SplitN(line, ":", 3)
		if len(fields) == 3 && fields[1] == "" {
			cleaned := filepath.Clean("/" + fields[2])
			return strings.TrimPrefix(cleaned, "/"), nil
		}
	}
	return "", errors.New("unified cgroup entry not found")
}

// formatCgroupLimit formats cgroup limit values for logs, including max.
func formatCgroupLimit(value string) string {
	if value == "max" {
		return "infinity"
	}
	return value
}
