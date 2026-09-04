package app

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	psutilCPU "github.com/shirou/gopsutil/v4/cpu"
	psutilProcess "github.com/shirou/gopsutil/v4/process"

	container "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	procmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/process"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/utils"
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

// processManager owns per-process CPU state used to compute CPU percentages between collection cycles.
type processManager struct {
	sync.Mutex
	processCPUPrev map[int32]prevProcessCPU
	processIOPrev  map[int32]prevProcessIO
	resolver       *processAttributionResolver
}

func newProcessManager() *processManager {
	return &processManager{
		processCPUPrev: make(map[int32]prevProcessCPU),
		processIOPrev:  make(map[int32]prevProcessIO),
	}
}

func (m *processManager) collectProcessStats(ctx context.Context, identities []container.Identity) (*procmodel.Count, []procmodel.Process, []procmodel.Program, error) {
	m.Lock()
	defer m.Unlock()

	m.resolver = newProcessAttributionResolver(identities)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	procs, err := psutilProcess.ProcessesWithContext(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, nil, ctxErr
		}
		slog.Warn("process collection failed", "err", err)
		return &procmodel.Count{}, nil, nil, nil
	}

	now := time.Now()
	count := &procmodel.Count{PIDMax: readPIDMax()}
	items := make([]procmodel.Process, 0, len(procs))
	seen := make(map[int32]struct{}, len(procs))

	for _, proc := range procs {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		if proc == nil {
			continue
		}
		item, ok, err := m.collectOneProcess(ctx, proc, now, count)
		if err != nil {
			return nil, nil, nil, err
		}
		if !ok {
			continue
		}
		items = append(items, item)
		seen[item.PID] = struct{}{}
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

//nolint:gocognit // Process collection is a sequence of independent best-effort procfs fields.
func (m *processManager) collectOneProcess(ctx context.Context, proc *psutilProcess.Process, now time.Time, count *procmodel.Count) (procmodel.Process, bool, error) {
	item := procmodel.Process{PID: proc.Pid}
	createTime, err := proc.CreateTimeWithContext(ctx)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return item, false, ctxErr
	}
	if err != nil && errors.Is(err, psutilProcess.ErrorProcessNotRunning) {
		return item, false, nil
	}
	if err == nil {
		item.CreateTime = createTime
	}

	if name, err := proc.NameWithContext(ctx); err == nil {
		item.Name = name
	}
	if item.Name == "" {
		item.Name = strconv.Itoa(int(proc.Pid))
	}
	if cmdline, err := proc.CmdlineSliceWithContext(ctx); err == nil {
		item.Cmdline = cmdline
	}
	if username, err := proc.UsernameWithContext(ctx); err == nil {
		item.Username = username
	}
	if statuses, err := proc.StatusWithContext(ctx); err == nil {
		item.Status = strings.Join(statuses, ",")
		updateProcessCount(count, statuses)
	}
	if threads, err := proc.NumThreadsWithContext(ctx); err == nil {
		item.NumThreads = threads
		count.Thread += int(threads)
	}
	if nice, err := proc.NiceWithContext(ctx); err == nil {
		item.Nice = nice
	}
	if memInfo, err := proc.MemoryInfoWithContext(ctx); err == nil && memInfo != nil {
		item.MemoryInfo = procmodel.MemoryInfo{
			RSS:    memInfo.RSS,
			VMS:    memInfo.VMS,
			HWM:    memInfo.HWM,
			Data:   memInfo.Data,
			Stack:  memInfo.Stack,
			Locked: memInfo.Locked,
			Swap:   memInfo.Swap,
		}
	}
	if memPercent, err := proc.MemoryPercentWithContext(ctx); err == nil {
		item.MemoryPercent = utils.TwoDecimals(float64(memPercent))
	}
	if ioCounters, err := proc.IOCountersWithContext(ctx); err == nil && ioCounters != nil {
		item.IOCounters = procmodel.IOCounters{
			ReadCount:      ioCounters.ReadCount,
			WriteCount:     ioCounters.WriteCount,
			ReadBytes:      ioCounters.ReadBytes,
			WriteBytes:     ioCounters.WriteBytes,
			DiskReadBytes:  ioCounters.DiskReadBytes,
			DiskWriteBytes: ioCounters.DiskWriteBytes,
		}
		item.IOCounters.DiskReadBytesPerSecond, item.IOCounters.DiskWriteBytesPerSecond = m.processIOPerSecond(proc.Pid, createTime, ioCounters.DiskReadBytes, ioCounters.DiskWriteBytes, now)
	}
	if m.resolver != nil {
		if identity, ok := m.resolver.resolve(proc.Pid); ok {
			item.ContainerID, item.ContainerName = identity.ID, identity.Name
			if item.ContainerID == "" {
				item.ContainerID = identity.FullID
			}
		}
	}
	if times, err := proc.TimesWithContext(ctx); err == nil && times != nil {
		item.CPUPercent = utils.TwoDecimals(m.processCPUPercent(proc.Pid, createTime, processTimesTotal(times), now))
	}
	if err := ctx.Err(); err != nil {
		return item, false, err
	}

	count.Total++
	return item, true, nil
}

func (m *processManager) processIOPerSecond(pid int32, createTime int64, readBytes, writeBytes uint64, now time.Time) (uint64, uint64) {
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

func (m *processManager) processCPUPercent(pid int32, createTime int64, total float64, now time.Time) float64 {
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

func (m *processManager) pruneProcessCPUCache(seen map[int32]struct{}) {
	for pid := range m.processCPUPrev {
		if _, ok := seen[pid]; !ok {
			delete(m.processCPUPrev, pid)
		}
	}
}

func (m *processManager) pruneProcessIOCache(seen map[int32]struct{}) {
	for pid := range m.processIOPrev {
		if _, ok := seen[pid]; !ok {
			delete(m.processIOPrev, pid)
		}
	}
}

func updateProcessCount(count *procmodel.Count, statuses []string) {
	if len(statuses) == 0 {
		return
	}
	for _, status := range statuses {
		switch status {
		case psutilProcess.Running:
			count.Running++
		case psutilProcess.Sleep:
			count.Sleeping++
		case psutilProcess.Idle:
			count.Idle++
		case psutilProcess.Stop:
			count.Stopped++
		case psutilProcess.Zombie:
			count.Zombie++
		case psutilProcess.Blocked, psutilProcess.Lock, psutilProcess.Wait:
			count.Blocked++
		}
	}
}

func groupProgramStats(processes []procmodel.Process) []procmodel.Program {
	groups := make(map[string]*procmodel.Program)
	for _, proc := range processes {
		name := proc.Name
		if name == "" {
			name = "unknown"
		}
		group := groups[name]
		if group == nil {
			group = &procmodel.Program{Name: name}
			groups[name] = group
		}
		group.Count++
		group.CPUPercent += proc.CPUPercent
		group.MemoryPercent += proc.MemoryPercent
		group.MemoryRSSBytes += proc.MemoryInfo.RSS
		group.PIDs = append(group.PIDs, proc.PID)
	}

	items := make([]procmodel.Program, 0, len(groups))
	for _, group := range groups {
		group.CPUPercent = utils.TwoDecimals(group.CPUPercent)
		group.MemoryPercent = utils.TwoDecimals(group.MemoryPercent)
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

func processTimesTotal(times *psutilCPU.TimesStat) float64 {
	return times.User + times.System + times.Idle + times.Nice + times.Iowait + times.Irq +
		times.Softirq + times.Steal + times.Guest + times.GuestNice
}

func readPIDMax() int {
	raw := utils.ReadStringFile("/proc/sys/kernel/pid_max")
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return value
}
