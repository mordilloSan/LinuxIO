// Package process collects per-process and per-program CPU, memory and I/O.
package process

import (
	"context"
	"log/slog"
	"os/user"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	psutilHost "github.com/shirou/gopsutil/v4/host"
	psutilMem "github.com/shirou/gopsutil/v4/mem"

	"github.com/mordilloSan/LinuxIO/backend/common/hwprobe"
	containermodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	processmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/process"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/units"
)

type prevProcessCPU struct {
	createTime int64
	total      float64
	readTime   time.Time
}

type prevProcessIO struct {
	createTime            int64
	readBytes, writeBytes uint64
	readTime              time.Time
}

// Manager owns per-process CPU state used to compute CPU percentages between collection cycles.
type Manager struct {
	sync.Mutex
	processCPUPrev map[int32]prevProcessCPU
	processIOPrev  map[int32]prevProcessIO
	resolver       *processAttributionResolver
	bootMs         int64 // boot time in Unix milliseconds; 0 until read
}

// NewManager returns a Manager with empty CPU and I/O baselines.
func NewManager() *Manager {
	return &Manager{
		processCPUPrev: make(map[int32]prevProcessCPU),
		processIOPrev:  make(map[int32]prevProcessIO),
	}
}

// CollectStats samples every process, attributing each to a container where
// the identities allow it.
func (m *Manager) CollectStats(ctx context.Context, identities []containermodel.Identity) (*processmodel.Count, []processmodel.Process, []processmodel.Program, error) {
	m.Lock()
	defer m.Unlock()

	m.resolver = newProcessAttributionResolver(identities)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	pids, err := readPIDs(procRoot)
	if err != nil {
		slog.Warn("process collection failed", "error", err)
		return &processmodel.Count{}, nil, nil, nil
	}

	// Read once per tick; a failure leaves totalMem 0, so MemoryPercent stays 0 as before.
	var totalMem uint64
	if vm, memErr := psutilMem.VirtualMemoryWithContext(ctx); memErr == nil {
		totalMem = vm.Total
	}
	bootMs := m.bootTimeMs(ctx)

	now := time.Now()
	count := &processmodel.Count{PIDMax: readPIDMax()}
	items := make([]processmodel.Process, 0, len(pids))
	seen := make(map[int32]struct{}, len(pids))
	users := make(map[uint32]string)

	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		sample, err := readProc(procRoot, pid)
		if err != nil {
			continue // exited between the listing and the read
		}
		items = append(items, m.processItem(sample, bootMs, now, totalMem, users, count))
		seen[pid] = struct{}{}
	}
	m.pruneProcessCPUCache(seen)
	m.pruneProcessIOCache(seen)

	sort.Slice(items, func(i, j int) bool {
		if items[i].CPUPercent != items[j].CPUPercent {
			return items[i].CPUPercent > items[j].CPUPercent
		}
		if items[i].MemoryPercent != items[j].MemoryPercent {
			return items[i].MemoryPercent > items[j].MemoryPercent
		}
		return items[i].PID < items[j].PID
	})

	programs := groupProgramStats(items)
	return count, items, programs, nil
}

// processItem turns one /proc sample into the payload record, counting it.
// users caches the name of each uid for the rest of the scan.
func (m *Manager) processItem(sample procSample, bootMs int64, now time.Time, totalMem uint64, users map[uint32]string, count *processmodel.Count) processmodel.Process {
	item := processmodel.Process{
		PID:        sample.pid,
		Name:       sample.name,
		Cmdline:    sample.cmdline,
		Status:     sample.state,
		NumThreads: sample.threads,
		Nice:       sample.nice,
		MemoryInfo: sample.memory,
	}
	if item.Name == "" {
		item.Name = strconv.Itoa(int(sample.pid))
	}
	// Without the boot time a start time is unknown, and the first sample
	// only seeds the CPU baseline.
	var createTime int64
	if bootMs > 0 {
		createTime = bootMs + int64(sample.startTicks*1000/userHZ)
		item.CreateTime = createTime
	}
	if sample.hasUID {
		item.Username = lookupUsername(users, sample.uid)
	}
	updateProcessCount(count, sample.state)
	count.Thread += int(sample.threads)
	if totalMem > 0 {
		item.MemoryPercent = units.TwoDecimals(float64(memoryPercent(sample.memory.RSS, totalMem)))
	}
	if sample.hasIO {
		item.IOCounters = sample.io
		item.IOCounters.DiskReadBytesPerSecond, item.IOCounters.DiskWriteBytesPerSecond = m.processIOPerSecond(sample.pid, createTime, sample.io.DiskReadBytes, sample.io.DiskWriteBytes, now)
	}
	if m.resolver != nil {
		if identity, ok := m.resolver.resolve(sample.pid); ok {
			item.ContainerID, item.ContainerName = identity.ID, identity.Name
			if item.ContainerID == "" {
				item.ContainerID = identity.FullID
			}
		}
	}
	item.CPUPercent = units.TwoDecimals(m.processCPUPercent(sample.pid, createTime, sample.cpuSeconds, now))
	count.Total++
	return item
}

// lookupUsername names a uid once per scan; an unknown uid keeps its number,
// as ps shows it, and a failed lookup is not retried within the scan.
func lookupUsername(users map[uint32]string, uid uint32) string {
	if name, ok := users[uid]; ok {
		return name
	}
	id := strconv.FormatUint(uint64(uid), 10)
	name := id
	if u, err := user.LookupId(id); err == nil {
		name = u.Username
	}
	users[uid] = name
	return name
}

// bootTimeMs is the boot time in Unix milliseconds, read once for the
// manager's life so a step of the wall clock cannot move every process's
// start time and reset its CPU baseline. Zero means it could not be read.
func (m *Manager) bootTimeMs(ctx context.Context) int64 {
	if m.bootMs == 0 {
		if boot, err := psutilHost.BootTimeWithContext(ctx); err == nil && boot > 0 {
			m.bootMs = int64(boot) * 1000
		}
	}
	return m.bootMs
}

// memoryPercent mirrors gopsutil's Process.MemoryPercent without re-reading /proc/meminfo per process.
func memoryPercent(rss, total uint64) float32 {
	return 100 * float32(rss) / float32(total)
}

func (m *Manager) processIOPerSecond(pid int32, createTime int64, readBytes, writeBytes uint64, now time.Time) (uint64, uint64) {
	if m.processIOPrev == nil {
		m.processIOPrev = make(map[int32]prevProcessIO)
	}
	prev, ok := m.processIOPrev[pid]
	m.processIOPrev[pid] = prevProcessIO{createTime: createTime, readBytes: readBytes, writeBytes: writeBytes, readTime: now}
	if !ok || prev.createTime != createTime || prev.readTime.IsZero() {
		return 0, 0
	}
	seconds := now.Sub(prev.readTime).Seconds()
	if seconds <= 0 {
		return 0, 0
	}
	var readRate, writeRate uint64
	if readBytes >= prev.readBytes {
		readRate = uint64(float64(readBytes-prev.readBytes) / seconds)
	}
	if writeBytes >= prev.writeBytes {
		writeRate = uint64(float64(writeBytes-prev.writeBytes) / seconds)
	}
	return readRate, writeRate
}

func (m *Manager) processCPUPercent(pid int32, createTime int64, total float64, now time.Time) float64 {
	if m.processCPUPrev == nil {
		m.processCPUPrev = make(map[int32]prevProcessCPU)
	}

	prev, ok := m.processCPUPrev[pid]
	m.processCPUPrev[pid] = prevProcessCPU{
		createTime: createTime,
		total:      total,
		readTime:   now,
	}
	if !ok || prev.createTime != createTime || prev.readTime.IsZero() {
		if createTime <= 0 {
			return 0
		}
		elapsed := now.Sub(time.UnixMilli(createTime)).Seconds()
		if elapsed <= 0 {
			return 0
		}
		return total / elapsed * 100
	}

	elapsed := now.Sub(prev.readTime).Seconds()
	if elapsed <= 0 || total < prev.total {
		return 0
	}
	return (total - prev.total) / elapsed * 100
}

func (m *Manager) pruneProcessCPUCache(seen map[int32]struct{}) {
	for pid := range m.processCPUPrev {
		if _, ok := seen[pid]; !ok {
			delete(m.processCPUPrev, pid)
		}
	}
}

func (m *Manager) pruneProcessIOCache(seen map[int32]struct{}) {
	for pid := range m.processIOPrev {
		if _, ok := seen[pid]; !ok {
			delete(m.processIOPrev, pid)
		}
	}
}

func updateProcessCount(count *processmodel.Count, state string) {
	switch state {
	case stateRunning:
		count.Running++
	case stateSleep:
		count.Sleeping++
	case stateIdle:
		count.Idle++
	case stateStop:
		count.Stopped++
	case stateZombie:
		count.Zombie++
	case stateBlocked, stateWait:
		count.Blocked++
	}
}

func groupProgramStats(processes []processmodel.Process) []processmodel.Program {
	groups := make(map[string]*processmodel.Program)
	for _, proc := range processes {
		name := proc.Name
		if name == "" {
			name = "unknown"
		}
		group := groups[name]
		if group == nil {
			group = &processmodel.Program{Name: name}
			groups[name] = group
		}
		group.Count++
		group.CPUPercent += proc.CPUPercent
		group.MemoryPercent += proc.MemoryPercent
		group.MemoryRSSBytes += proc.MemoryInfo.RSS
		group.PIDs = append(group.PIDs, proc.PID)
	}

	items := make([]processmodel.Program, 0, len(groups))
	for _, group := range groups {
		group.CPUPercent = units.TwoDecimals(group.CPUPercent)
		group.MemoryPercent = units.TwoDecimals(group.MemoryPercent)
		slices.Sort(group.PIDs)
		items = append(items, *group)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CPUPercent != items[j].CPUPercent {
			return items[i].CPUPercent > items[j].CPUPercent
		}
		if items[i].MemoryPercent != items[j].MemoryPercent {
			return items[i].MemoryPercent > items[j].MemoryPercent
		}
		return items[i].Name < items[j].Name
	})
	return items
}

func readPIDMax() int {
	raw := hwprobe.ReadSysfsString("/proc/sys/kernel/pid_max")
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return value
}
