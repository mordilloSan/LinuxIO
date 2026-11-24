package process

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	processmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/process"
)

// A name may hold spaces and parentheses; the fields start after the last ')'.
const fixtureStat = "4242 (my (odd) name) S 1 4242 4242 0 -1 4194560 100 0 0 0 250 50 0 0 20 -5 3 0 12345 1000 200 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 2 0 0 7 0 0\n"

const fixtureStatus = "Name:\tmy (odd) name\nUmask:\t0022\nState:\tS (sleeping)\nUid:\t1000\t1000\t1000\t1000\nGid:\t1000\t1000\t1000\t1000\n" +
	"VmPeak:\t   20000 kB\nVmSize:\t   10000 kB\nVmLck:\t       4 kB\nVmHWM:\t    3000 kB\nVmRSS:\t    2048 kB\nVmData:\t     500 kB\nVmStk:\t     132 kB\nVmSwap:\t      16 kB\nThreads:\t3\n"

const fixtureIO = "rchar: 100\nwchar: 200\nsyscr: 3\nsyscw: 4\nread_bytes: 4096\nwrite_bytes: 8192\ncancelled_write_bytes: 0\n"

func writeProcFixture(t *testing.T, root string, pid string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, pid)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
}

func TestReadProcReadsEachFileOnce(t *testing.T) {
	root := t.TempDir()
	writeProcFixture(t, root, "4242", map[string]string{
		"stat":    fixtureStat,
		"status":  fixtureStatus,
		"cmdline": "/usr/bin/odd\x00--flag\x00value\x00",
		"io":      fixtureIO,
	})

	got, err := readProc(root, 4242)
	require.NoError(t, err)
	assert.Equal(t, procSample{
		pid:        4242,
		name:       "my (odd) name",
		cmdline:    []string{"/usr/bin/odd", "--flag", "value"},
		state:      stateSleep,
		threads:    3,
		nice:       -5,
		startTicks: 12345,
		cpuSeconds: 3, // (250 + 50) ticks at 100 Hz
		uid:        1000,
		hasUID:     true,
		memory: processmodel.MemoryInfo{
			RSS: 2048 * 1024, VMS: 10000 * 1024, HWM: 3000 * 1024, Data: 500 * 1024,
			Stack: 132 * 1024, Locked: 4 * 1024, Swap: 16 * 1024,
		},
		io:    processmodel.IOCounters{ReadBytes: 100, WriteBytes: 200, ReadCount: 3, WriteCount: 4, DiskReadBytes: 4096, DiskWriteBytes: 8192},
		hasIO: true,
	}, got)
}

func TestReadProcKernelThreadAndMissingFiles(t *testing.T) {
	root := t.TempDir()
	// A kernel thread: empty cmdline, no Vm lines, no io file readable.
	writeProcFixture(t, root, "2", map[string]string{
		"stat":    "2 (kthreadd) S 0 0 0 0 -1 2129984 0 0 0 0 0 7 0 0 20 0 1 0 2 0 0 18446744073709551615 0 0 0 0 0 0 0 2147483647 0 0 0 0 17 3 0 0 0 0 0\n",
		"status":  "Name:\tkthreadd\nState:\tS (sleeping)\nUid:\t0\t0\t0\t0\nThreads:\t1\n",
		"cmdline": "",
	})

	got, err := readProc(root, 2)
	require.NoError(t, err)
	assert.Equal(t, "kthreadd", got.name)
	assert.Nil(t, got.cmdline)
	assert.Equal(t, processmodel.MemoryInfo{}, got.memory)
	assert.False(t, got.hasIO)
	assert.True(t, got.hasUID)
	assert.InDelta(t, 0.07, got.cpuSeconds, 1e-9)

	_, err = readProc(root, 3)
	assert.Error(t, err, "a process without a stat file has exited")
}

func TestReadProcRestoresATruncatedName(t *testing.T) {
	root := t.TempDir()
	stat := "77 (systemd-journal) S 1 77 77 0 -1 4194560 0 0 0 0 1 1 0 0 19 -1 1 0 300 0 0 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0\n"
	writeProcFixture(t, root, "77", map[string]string{"stat": stat, "cmdline": "/usr/lib/systemd/systemd-journald\x00"})
	got, err := readProc(root, 77)
	require.NoError(t, err)
	assert.Equal(t, "systemd-journald", got.name)

	// A comm that is not the start of the program's file name stays as it is.
	writeProcFixture(t, root, "78", map[string]string{"stat": "78 (renamed-worker1) S" + stat[len("77 (systemd-journal) S"):], "cmdline": "/usr/bin/python3\x00"})
	got, err = readProc(root, 78)
	require.NoError(t, err)
	assert.Equal(t, "renamed-worker1", got.name)
}

func TestParseStatRejectsMalformedContent(t *testing.T) {
	for _, content := range []string{"", "1 noparens S", "1 (short) S 1 2 3", "1 (x) S 1 1 1 0 -1 0 0 0 0 0 nope 1 0 0 20 0 1 0 5 0 0"} {
		var sample procSample
		assert.ErrorIs(t, parseStat([]byte(content), &sample), errMalformedStat, content)
	}
}

func TestReadPIDsSkipsNonProcessEntries(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"1", "42", "self", "sys", "0", "9x"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, name), 0o755))
	}
	pids, err := readPIDs(root)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int32{1, 42}, pids)
}

func TestProcessItemUsesTheSample(t *testing.T) {
	manager := NewManager()
	now := time.UnixMilli(1_000_000_000_000)
	bootMs := now.Add(-time.Hour).UnixMilli()
	sample := procSample{
		pid: 9, name: "worker", state: stateRunning, threads: 4, nice: 5,
		startTicks: 100 * 60 * 50, // 50 minutes after boot, so 10 minutes old
		cpuSeconds: 60, uid: 0, hasUID: true,
		memory: processmodel.MemoryInfo{RSS: 1 << 30},
	}
	users := map[uint32]string{}
	count := &processmodel.Count{}

	item := manager.processItem(sample, bootMs, now, 4<<30, users, count)

	assert.Equal(t, bootMs+50*60*1000, item.CreateTime)
	assert.Equal(t, int32(5), item.Nice)
	assert.Equal(t, "running", item.Status)
	assert.InDelta(t, 25.0, item.MemoryPercent, 1e-9)
	assert.InDelta(t, 10.0, item.CPUPercent, 1e-9, "60 s of CPU over a 600 s life")
	assert.Equal(t, "root", item.Username)
	assert.Equal(t, map[uint32]string{0: "root"}, users)
	assert.Equal(t, processmodel.Count{Total: 1, Running: 1, Thread: 4}, *count)
}

func TestLookupUsernameFallsBackToTheNumber(t *testing.T) {
	users := map[uint32]string{}
	assert.Equal(t, "4000000000", lookupUsername(users, 4_000_000_000))
	assert.Equal(t, "4000000000", users[4_000_000_000])
}
