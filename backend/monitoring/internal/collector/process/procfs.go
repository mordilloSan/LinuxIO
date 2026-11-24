package process

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	processmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/process"
)

// procRoot is where the process table is read.
const procRoot = "/proc"

// userHZ is the unit of the time fields in /proc/<pid>/stat. The kernel fixes
// it at 100 for user space on every architecture Go supports.
const userHZ = 100

// The status words a process reports, as gopsutil named them; the processes
// payload and the process counts keep these strings.
const (
	stateRunning = "running"
	stateSleep   = "sleep"
	stateBlocked = "blocked"
	stateZombie  = "zombie"
	stateStop    = "stop"
	stateIdle    = "idle"
	stateWait    = "wait"
)

// procSample is what one pass over /proc/<pid> found. Only the stat file is
// required; the others are best-effort and leave their fields empty.
type procSample struct {
	pid        int32
	name       string
	cmdline    []string
	state      string
	threads    int32
	nice       int32
	startTicks uint64  // starttime, in userHZ ticks since boot
	cpuSeconds float64 // utime + stime
	uid        uint32
	hasUID     bool
	memory     processmodel.MemoryInfo
	io         processmodel.IOCounters
	hasIO      bool
}

var errMalformedStat = errors.New("malformed stat file")

// readPIDs lists the numeric entries of the process table.
func readPIDs(root string) ([]int32, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	pids := make([]int32, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == "" || name[0] < '0' || name[0] > '9' {
			continue
		}
		pid, err := strconv.ParseInt(name, 10, 32)
		if err != nil || pid <= 0 {
			continue
		}
		pids = append(pids, int32(pid))
	}
	return pids, nil
}

// readProc reads one process, opening each of its stat, status, cmdline and
// io files once. An error means the stat file could not be read or parsed,
// most often because the process has exited.
func readProc(root string, pid int32) (procSample, error) {
	dir := filepath.Join(root, strconv.Itoa(int(pid)))
	sample := procSample{pid: pid}

	stat, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return sample, err
	}
	if err := parseStat(stat, &sample); err != nil {
		return sample, err
	}
	if status, err := os.ReadFile(filepath.Join(dir, "status")); err == nil {
		parseStatus(status, &sample)
	}
	if cmdline, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil {
		sample.cmdline = parseCmdline(cmdline)
	}
	// comm holds 15 bytes at most; a longer name is the start of the
	// program's own file name, which gopsutil restored the same way.
	if len(sample.name) >= 15 && len(sample.cmdline) > 0 {
		if base := filepath.Base(sample.cmdline[0]); strings.HasPrefix(base, sample.name) {
			sample.name = base
		}
	}
	if io, err := os.ReadFile(filepath.Join(dir, "io")); err == nil {
		sample.io, sample.hasIO = parseIO(io), true
	}
	return sample, nil
}

// parseStat reads the fields of /proc/<pid>/stat that the collector uses. The
// name is between the first '(' and the last ')', since it may hold either.
func parseStat(content []byte, sample *procSample) error {
	open := bytes.IndexByte(content, '(')
	end := bytes.LastIndexByte(content, ')')
	if open < 0 || end < open || end+2 > len(content) {
		return errMalformedStat
	}
	sample.name = string(content[open+1 : end])
	// rest[0] is field 3 (state) of proc_pid_stat(5), so field n is rest[n-3].
	rest := strings.Fields(string(content[end+2:]))
	if len(rest) < 20 {
		return errMalformedStat
	}
	if len(rest[0]) > 0 {
		sample.state = stateWord(rest[0][0])
	}
	utime, err1 := strconv.ParseUint(rest[11], 10, 64)
	stime, err2 := strconv.ParseUint(rest[12], 10, 64)
	nice, err3 := strconv.ParseInt(rest[16], 10, 32)
	threads, err4 := strconv.ParseInt(rest[17], 10, 32)
	start, err5 := strconv.ParseUint(rest[19], 10, 64)
	if err := errors.Join(err1, err2, err3, err4, err5); err != nil {
		return errors.Join(errMalformedStat, err)
	}
	sample.cpuSeconds = float64(utime+stime) / userHZ
	sample.nice = int32(nice)
	sample.threads = int32(threads)
	sample.startTicks = start
	return nil
}

// parseStatus reads the real uid and the memory figures of /proc/<pid>/status.
// A kernel thread has no Vm lines, and its memory stays zero.
func parseStatus(content []byte, sample *procSample) {
	for line := range strings.SplitSeq(string(content), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "Uid":
			realUID, _, _ := strings.Cut(value, "\t")
			if uid, err := strconv.ParseUint(realUID, 10, 32); err == nil {
				sample.uid, sample.hasUID = uint32(uid), true
			}
		case "VmRSS":
			sample.memory.RSS = statusKiB(value)
		case "VmSize":
			sample.memory.VMS = statusKiB(value)
		case "VmHWM":
			sample.memory.HWM = statusKiB(value)
		case "VmData":
			sample.memory.Data = statusKiB(value)
		case "VmStk":
			sample.memory.Stack = statusKiB(value)
		case "VmLck":
			sample.memory.Locked = statusKiB(value)
		case "VmSwap":
			sample.memory.Swap = statusKiB(value)
		}
	}
}

// statusKiB converts a "1234 kB" status value to bytes; anything else is 0.
func statusKiB(value string) uint64 {
	n, err := strconv.ParseUint(strings.TrimSpace(strings.TrimSuffix(value, "kB")), 10, 64)
	if err != nil {
		return 0
	}
	return n * 1024
}

// parseCmdline splits the NUL-separated arguments; a kernel thread has none.
func parseCmdline(content []byte) []string {
	content = bytes.TrimRight(content, "\x00")
	if len(content) == 0 {
		return nil
	}
	parts := bytes.Split(content, []byte{0})
	args := make([]string, len(parts))
	for i, part := range parts {
		args[i] = string(part)
	}
	return args
}

func parseIO(content []byte) processmodel.IOCounters {
	var counters processmodel.IOCounters
	for line := range strings.SplitSeq(string(content), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "rchar":
			counters.ReadBytes = n
		case "wchar":
			counters.WriteBytes = n
		case "syscr":
			counters.ReadCount = n
		case "syscw":
			counters.WriteCount = n
		case "read_bytes":
			counters.DiskReadBytes = n
		case "write_bytes":
			counters.DiskWriteBytes = n
		}
	}
	return counters
}

func stateWord(letter byte) string {
	switch letter {
	case 'R':
		return stateRunning
	case 'S':
		return stateSleep
	case 'D':
		return stateBlocked
	case 'Z':
		return stateZombie
	case 'T', 't':
		return stateStop
	case 'I':
		return stateIdle
	case 'W':
		return stateWait
	}
	return ""
}
