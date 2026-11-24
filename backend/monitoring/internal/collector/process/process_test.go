package process

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	containermodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	processmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/process"
)

func TestGroupProgramStats(t *testing.T) {
	processes := []processmodel.Process{
		{PID: 2, Name: "nginx", CPUPercent: 1.2, MemoryPercent: 0.5, MemoryInfo: processmodel.MemoryInfo{RSS: 100}},
		{PID: 1, Name: "nginx", CPUPercent: 2.3, MemoryPercent: 0.7, MemoryInfo: processmodel.MemoryInfo{RSS: 200}},
		{PID: 3, Name: "postgres", CPUPercent: 0.4, MemoryPercent: 2, MemoryInfo: processmodel.MemoryInfo{RSS: 300}},
	}

	programs := groupProgramStats(processes)

	require.Len(t, programs, 2)
	assert.Equal(t, "nginx", programs[0].Name)
	assert.Equal(t, 2, programs[0].Count)
	assert.InDelta(t, 3.5, programs[0].CPUPercent, 0.0001)
	assert.InDelta(t, 1.2, programs[0].MemoryPercent, 0.0001)
	assert.Equal(t, uint64(300), programs[0].MemoryRSSBytes)
	assert.Equal(t, []int32{1, 2}, programs[0].PIDs)
}

const fullContainerID = "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

func TestProcessAttributionResolverCgroupForms(t *testing.T) {
	identity := containermodel.Identity{ID: "abcdef123456", FullID: fullContainerID, Name: "web"}
	resolver := newProcessAttributionResolver([]containermodel.Identity{identity})
	for _, content := range []string{
		"0::/docker/" + fullContainerID + "\n",
		"1:name=systemd:/system.slice/docker-" + fullContainerID + ".scope\n",
		"0::/system.slice/docker-" + fullContainerID + ".scope/init.scope\n",
		"0::/machine.slice/libpod-" + fullContainerID + ".scope/container\n",
		"0::/libpod_parent/libpod-" + fullContainerID + "\n",
	} {
		got, ok := resolver.resolveCgroup(content)
		require.True(t, ok, content)
		assert.Equal(t, "web", got.Name)
	}
}

// A local user can name a cgroup of their own after a container; only the
// place a root runtime creates it counts, with the full ID.
func TestProcessAttributionResolverIgnoresCgroupsOutsideTheRuntimesPlace(t *testing.T) {
	resolver := newProcessAttributionResolver([]containermodel.Identity{{ID: "abcdef123456", FullID: fullContainerID, Name: "web"}})
	for _, content := range []string{
		"0::/user.slice/user-1000.slice/user@1000.service/app.slice/docker-" + fullContainerID + ".scope\n",
		"0::/user.slice/user-1000.slice/user@1000.service/user.slice/libpod-" + fullContainerID + ".scope\n",
		"0::/user.slice/docker/" + fullContainerID + "\n",
		"0::/docker/abcdef123456\n",
		"0::/machine.slice/libpod-conmon-" + fullContainerID + ".scope\n",
		"0::/system.slice/docker-" + fullContainerID + "\n",
		"0::/system.slice/" + fullContainerID + ".scope\n",
	} {
		_, ok := resolver.resolveCgroup(content)
		assert.False(t, ok, content)
	}
}

func TestProcessAttributionResolverRejectsTwoContainers(t *testing.T) {
	other := "0000000000000000000000000000000000000000000000000000000000000000"
	resolver := newProcessAttributionResolver([]containermodel.Identity{
		{FullID: fullContainerID, Name: "one"},
		{FullID: other, Name: "two"},
	})
	_, ok := resolver.resolveCgroup("2:cpu:/docker/" + fullContainerID + "\n1:name=systemd:/docker/" + other + "\n")
	assert.False(t, ok)
	_, ok = newProcessAttributionResolver([]containermodel.Identity{{ID: "abcdef123456", Name: "short only"}}).resolveCgroup("0::/docker/" + fullContainerID + "\n")
	assert.False(t, ok, "an identity without its full ID is never attributed")
}

func TestProcessIOPerSecond(t *testing.T) {
	manager := NewManager()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	read, write := manager.processIOPerSecond(42, 100, 100, 200, now)
	assert.Zero(t, read)
	assert.Zero(t, write)
	read, write = manager.processIOPerSecond(42, 100, 300, 600, now.Add(2*time.Second))
	assert.Equal(t, uint64(100), read)
	assert.Equal(t, uint64(200), write)
	read, write = manager.processIOPerSecond(42, 100, 1, 2, now.Add(3*time.Second))
	assert.Zero(t, read)
	assert.Zero(t, write)
	read, write = manager.processIOPerSecond(42, 100, 101, 1, now.Add(4*time.Second))
	assert.Equal(t, uint64(100), read)
	assert.Zero(t, write)
	read, write = manager.processIOPerSecond(42, 101, 500, 700, now.Add(5*time.Second))
	assert.Zero(t, read)
	assert.Zero(t, write)
}

func TestUpdateProcessCount(t *testing.T) {
	var count processmodel.Count

	updateProcessCount(&count, "running")
	updateProcessCount(&count, "sleep")
	updateProcessCount(&count, "zombie")
	updateProcessCount(&count, "blocked")
	updateProcessCount(&count, "")

	assert.Equal(t, 1, count.Running)
	assert.Equal(t, 1, count.Sleeping)
	assert.Equal(t, 1, count.Zombie)
	assert.Equal(t, 1, count.Blocked)
}

func TestProcessCPUPercent(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	t.Run("first sample uses process age when create time is available", func(t *testing.T) {
		manager := NewManager()
		createTime := now.Add(-100 * time.Second).UnixMilli()

		got := manager.processCPUPercent(123, createTime, 10, now)

		assert.InDelta(t, 10.0, got, 0.0001)
	})

	t.Run("first sample without create time seeds baseline", func(t *testing.T) {
		manager := NewManager()

		got := manager.processCPUPercent(123, 0, 10, now)

		assert.Zero(t, got)
		assert.Contains(t, manager.processCPUPrev, int32(123))
	})

	t.Run("subsequent sample uses total delta over elapsed time", func(t *testing.T) {
		manager := NewManager()
		createTime := now.Add(-100 * time.Second).UnixMilli()
		manager.processCPUPrev[123] = prevProcessCPU{
			createTime: createTime,
			total:      10,
			readTime:   now.Add(-2 * time.Second),
		}

		got := manager.processCPUPercent(123, createTime, 11, now)

		assert.InDelta(t, 50.0, got, 0.0001)
	})

	t.Run("counter reset returns zero and reseeds", func(t *testing.T) {
		manager := NewManager()
		createTime := now.Add(-100 * time.Second).UnixMilli()
		manager.processCPUPrev[123] = prevProcessCPU{
			createTime: createTime,
			total:      10,
			readTime:   now.Add(-time.Second),
		}

		got := manager.processCPUPercent(123, createTime, 1, now)

		assert.Zero(t, got)
		assert.InDelta(t, 1.0, manager.processCPUPrev[123].total, 0.0001)
	})

	t.Run("pid reuse is treated as a new baseline", func(t *testing.T) {
		manager := NewManager()
		oldCreateTime := now.Add(-time.Hour).UnixMilli()
		newCreateTime := now.Add(-10 * time.Second).UnixMilli()
		manager.processCPUPrev[123] = prevProcessCPU{
			createTime: oldCreateTime,
			total:      100,
			readTime:   now.Add(-time.Second),
		}

		got := manager.processCPUPercent(123, newCreateTime, 1, now)

		assert.InDelta(t, 10.0, got, 0.0001)
		assert.Equal(t, newCreateTime, manager.processCPUPrev[123].createTime)
	})

	t.Run("non increasing clock returns zero", func(t *testing.T) {
		manager := NewManager()
		createTime := now.Add(-100 * time.Second).UnixMilli()
		manager.processCPUPrev[123] = prevProcessCPU{
			createTime: createTime,
			total:      10,
			readTime:   now,
		}

		got := manager.processCPUPercent(123, createTime, 11, now)

		assert.Zero(t, got)
	})
}

func TestPruneProcessCPUCache(t *testing.T) {
	manager := &Manager{
		processCPUPrev: map[int32]prevProcessCPU{
			1: {},
			2: {},
		},
	}

	manager.pruneProcessCPUCache(map[int32]struct{}{2: {}})

	assert.NotContains(t, manager.processCPUPrev, int32(1))
	assert.Contains(t, manager.processCPUPrev, int32(2))
}

func TestMemoryPercentMatchesGopsutilExpression(t *testing.T) {
	const total = uint64(16 << 30)
	for _, rss := range []uint64{0, 123456789, 1 << 30, total} {
		assert.InDelta(t, 100*float32(rss)/float32(total), memoryPercent(rss, total), 0)
	}
	assert.InDelta(t, 6.25, memoryPercent(1<<30, total), 0.0001)
}
