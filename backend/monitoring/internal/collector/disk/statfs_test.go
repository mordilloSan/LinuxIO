package disk

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	psutilDisk "github.com/shirou/gopsutil/v4/disk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

// A tracked filesystem whose statfs does not return (a dead NFS server) costs
// the collection that started the call one wait, keeps its last figures, and
// costs later collections nothing until the call returns, when its answer is
// served; the root's figures are written every time.
func TestUpdateUsageDoesNotWaitOnADeadMount(t *testing.T) {
	release := make(chan struct{})
	var nfsCalls atomic.Int32
	oldUsage, oldWait := diskUsage, collectorStatfsWait
	diskUsage = func(_ context.Context, mountpoint string) (*psutilDisk.UsageStat, error) {
		if mountpoint == "/mnt/nfs" {
			nfsCalls.Add(1)
			<-release
			return &psutilDisk.UsageStat{Total: 4 << 30, Used: 3 << 30, UsedPercent: 75}, nil
		}
		return &psutilDisk.UsageStat{Total: 2 << 30, Used: 1 << 30, UsedPercent: 50}, nil
	}
	collectorStatfsWait = 50 * time.Millisecond
	t.Cleanup(func() { diskUsage, collectorStatfsWait = oldUsage, oldWait })

	lastSeen := time.Now().Add(-time.Minute)
	m := NewManager()
	m.fsStats["sda"] = &systemmodel.FsStats{Root: true, Mountpoint: "/"}
	m.fsStats["nfs"] = &systemmodel.FsStats{Mountpoint: "/mnt/nfs"}
	m.fsStats["nfs"].DiskTotal, m.fsStats["nfs"].UsageAt = 1, lastSeen

	var system systemmodel.Stats
	require.NoError(t, m.UpdateUsage(context.Background(), &system))
	assert.InDelta(t, 2.0, system.DiskTotal, 1e-9)
	assert.InDelta(t, 50.0, system.DiskPct, 1e-9)
	assert.InDelta(t, 1.0, m.fsStats["nfs"].DiskTotal, 1e-9, "a mount that did not answer keeps its last figures")
	assert.Equal(t, lastSeen, m.fsStats["nfs"].UsageAt)

	start := time.Now()
	require.NoError(t, m.UpdateUsage(context.Background(), &system))
	assert.Less(t, time.Since(start), collectorStatfsWait, "a collection waited on a call already running")
	assert.EqualValues(t, 1, nfsCalls.Load())

	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for m.fsStats["nfs"].DiskTotal != 4 {
		require.True(t, time.Now().Before(deadline), "the mount's late answer was never served")
		time.Sleep(10 * time.Millisecond)
		require.NoError(t, m.UpdateUsage(context.Background(), &system))
	}
	assert.EqualValues(t, 1, nfsCalls.Load())
	assert.True(t, m.fsStats["nfs"].UsageAt.After(lastSeen))
}

// A late answer is taken by a collection a minute later, the default
// collector interval, and its time is the answer's; one past the caller's
// maximum age starts a new call instead.
func TestStatfsCallsServeALateAnswerWithinTheCallersAge(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	statfs := func(context.Context, string) (*psutilDisk.UsageStat, error) {
		if calls.Add(1) == 1 {
			<-release
		}
		return &psutilDisk.UsageStat{Total: 1}, nil
	}
	var c StatfsCalls
	_, _, err := c.Usage(context.Background(), "/mnt/slow", 10*time.Millisecond, time.Minute, statfs)
	require.ErrorIs(t, err, ErrStatfsSlow)
	_, _, err = c.Usage(context.Background(), "/mnt/slow", 10*time.Millisecond, time.Minute, statfs)
	require.ErrorIs(t, err, ErrStatfsPending)
	close(release)
	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		select {
		case <-c.calls["/mnt/slow"].done:
			return true
		default:
			return false
		}
	}, 5*time.Second, 5*time.Millisecond)
	c.mu.Lock()
	answered := c.calls["/mnt/slow"].at
	c.calls["/mnt/slow"].at = answered.Add(-59 * time.Second)
	c.mu.Unlock()

	usage, at, err := c.Usage(context.Background(), "/mnt/slow", 10*time.Millisecond, time.Minute, statfs)
	require.NoError(t, err)
	assert.EqualValues(t, 1, usage.Total)
	assert.Equal(t, answered.Add(-59*time.Second), at)
	assert.EqualValues(t, 1, calls.Load(), "a late answer within the age starts no new call")
}
