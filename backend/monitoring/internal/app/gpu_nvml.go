//go:build amd64 && glibc

package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

// NVML constants and types
const (
	nvmlSuccess               int    = 0
	nvmlErrorInsufficientSize int    = 7
	nvmlValueNotAvailable     uint64 = ^uint64(0)
)

type nvmlDevice uintptr

type nvmlReturn int

type nvmlMemoryV1 struct {
	Total uint64
	Free  uint64
	Used  uint64
}

type nvmlMemoryV2 struct {
	Version  uint32
	Total    uint64
	Reserved uint64
	Free     uint64
	Used     uint64
}

type nvmlUtilization struct {
	Gpu    uint32
	Memory uint32
}

// NVML process structures are part of the C ABI. Keep explicit amd64 padding;
// purego passes these arrays as opaque pointers to NVML.
type nvmlProcessInfoV1 struct {
	PID           uint32
	_             uint32
	UsedGpuMemory uint64
}
type nvmlProcessInfoV2 struct {
	PID               uint32
	_                 uint32
	UsedGpuMemory     uint64
	GPUInstanceID     uint32
	ComputeInstanceID uint32
}
type nvmlProcessUtilization struct {
	PID       uint32
	_         uint32
	TimeStamp uint64
	SMUtil    uint32
	MemUtil   uint32
	EncUtil   uint32
	DecUtil   uint32
}

type nvmlPciInfo struct {
	BusId          [16]byte
	Domain         uint32
	Bus            uint32
	Device         uint32
	PciDeviceId    uint32
	PciSubSystemId uint32
}

// NVML function signatures
var (
	nvmlInit                                func() nvmlReturn
	nvmlShutdown                            func() nvmlReturn
	nvmlDeviceGetCount                      func(count *uint32) nvmlReturn
	nvmlDeviceGetHandleByIndex              func(index uint32, device *nvmlDevice) nvmlReturn
	nvmlDeviceGetName                       func(device nvmlDevice, name *byte, length uint32) nvmlReturn
	nvmlDeviceGetMemoryInfo                 func(device nvmlDevice, memory uintptr) nvmlReturn
	nvmlDeviceGetUtilizationRates           func(device nvmlDevice, utilization *nvmlUtilization) nvmlReturn
	nvmlDeviceGetTemperature                func(device nvmlDevice, sensorType int, temp *uint32) nvmlReturn
	nvmlDeviceGetPowerUsage                 func(device nvmlDevice, power *uint32) nvmlReturn
	nvmlDeviceGetPciInfo                    func(device nvmlDevice, pci *nvmlPciInfo) nvmlReturn
	nvmlErrorString                         func(result nvmlReturn) string
	nvmlDeviceGetComputeRunningProcessesV3  func(device nvmlDevice, count *uint32, infos uintptr) nvmlReturn
	nvmlDeviceGetComputeRunningProcessesV2  func(device nvmlDevice, count *uint32, infos uintptr) nvmlReturn
	nvmlDeviceGetComputeRunningProcessesV1  func(device nvmlDevice, count *uint32, infos uintptr) nvmlReturn
	nvmlDeviceGetGraphicsRunningProcessesV3 func(device nvmlDevice, count *uint32, infos uintptr) nvmlReturn
	nvmlDeviceGetGraphicsRunningProcessesV2 func(device nvmlDevice, count *uint32, infos uintptr) nvmlReturn
	nvmlDeviceGetGraphicsRunningProcessesV1 func(device nvmlDevice, count *uint32, infos uintptr) nvmlReturn
	nvmlDeviceGetProcessUtilization         func(device nvmlDevice, samples uintptr, count *uint32, lastSeen uint64) nvmlReturn
)

type nvmlCollector struct {
	gm              *GPUManager
	lib             uintptr
	devices         []nvmlDevice
	bdfs            []string
	names           []string
	processLastSeen map[string]uint64
	processOnly     bool
	isV2            bool
}

func (c *nvmlCollector) init() error {
	slog.Debug("NVML: Initializing")
	libPath := getNVMLPath()

	lib, err := openLibrary(libPath)
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", libPath, err)
	}
	c.lib = lib

	purego.RegisterLibFunc(&nvmlInit, lib, "nvmlInit")
	purego.RegisterLibFunc(&nvmlShutdown, lib, "nvmlShutdown")
	purego.RegisterLibFunc(&nvmlDeviceGetCount, lib, "nvmlDeviceGetCount")
	purego.RegisterLibFunc(&nvmlDeviceGetHandleByIndex, lib, "nvmlDeviceGetHandleByIndex")
	purego.RegisterLibFunc(&nvmlDeviceGetName, lib, "nvmlDeviceGetName")
	// Try to get v2 memory info, fallback to v1 if not available
	if hasSymbol(lib, "nvmlDeviceGetMemoryInfo_v2") {
		c.isV2 = true
		purego.RegisterLibFunc(&nvmlDeviceGetMemoryInfo, lib, "nvmlDeviceGetMemoryInfo_v2")
	} else {
		purego.RegisterLibFunc(&nvmlDeviceGetMemoryInfo, lib, "nvmlDeviceGetMemoryInfo")
	}
	purego.RegisterLibFunc(&nvmlDeviceGetUtilizationRates, lib, "nvmlDeviceGetUtilizationRates")
	purego.RegisterLibFunc(&nvmlDeviceGetTemperature, lib, "nvmlDeviceGetTemperature")
	purego.RegisterLibFunc(&nvmlDeviceGetPowerUsage, lib, "nvmlDeviceGetPowerUsage")
	purego.RegisterLibFunc(&nvmlDeviceGetPciInfo, lib, "nvmlDeviceGetPciInfo")
	purego.RegisterLibFunc(&nvmlErrorString, lib, "nvmlErrorString")
	registerNVMLProcessFunctions(lib)

	if ret := nvmlInit(); ret != nvmlReturn(nvmlSuccess) {
		return fmt.Errorf("nvmlInit failed: %v", ret)
	}

	var count uint32
	if ret := nvmlDeviceGetCount(&count); ret != nvmlReturn(nvmlSuccess) {
		return fmt.Errorf("nvmlDeviceGetCount failed: %v", ret)
	}

	for i := uint32(0); i < count; i++ {
		var device nvmlDevice
		if ret := nvmlDeviceGetHandleByIndex(i, &device); ret == nvmlReturn(nvmlSuccess) {
			c.devices = append(c.devices, device)
			c.names = append(c.names, readNVMLDeviceName(device))
			// Get BDF for power state check
			var pci nvmlPciInfo
			if ret := nvmlDeviceGetPciInfo(device, &pci); ret == nvmlReturn(nvmlSuccess) {
				busID := string(pci.BusId[:])
				if idx := strings.Index(busID, "\x00"); idx != -1 {
					busID = busID[:idx]
				}
				c.bdfs = append(c.bdfs, strings.ToLower(busID))
			} else {
				c.bdfs = append(c.bdfs, "")
			}
		}
	}
	c.processLastSeen = make(map[string]uint64, len(c.devices))
	if c.processOnly && !hasNVMLProcessListingAPI() {
		_ = nvmlShutdown()
		return errors.New("nvml process listing APIs are unavailable")
	}

	return nil
}

func hasNVMLProcessListingAPI() bool {
	return nvmlDeviceGetComputeRunningProcessesV3 != nil ||
		nvmlDeviceGetComputeRunningProcessesV2 != nil ||
		nvmlDeviceGetComputeRunningProcessesV1 != nil ||
		nvmlDeviceGetGraphicsRunningProcessesV3 != nil ||
		nvmlDeviceGetGraphicsRunningProcessesV2 != nil ||
		nvmlDeviceGetGraphicsRunningProcessesV1 != nil
}

func readNVMLDeviceName(device nvmlDevice) string {
	var nameBuf [64]byte
	if ret := nvmlDeviceGetName(device, &nameBuf[0], uint32(len(nameBuf))); ret != nvmlReturn(nvmlSuccess) {
		return ""
	}
	name := string(nameBuf[:])
	if end := strings.IndexByte(name, 0); end >= 0 {
		name = name[:end]
	}
	name = strings.TrimPrefix(name, "NVIDIA ")
	return strings.TrimSuffix(name, " Laptop GPU")
}

func (c *nvmlCollector) start(ctx context.Context) {
	defer nvmlShutdown()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collect()
		}
	}
}

func (c *nvmlCollector) startProcesses(ctx context.Context) {
	defer nvmlShutdown()
	c.collectProcesses()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collectProcesses()
		}
	}
}

func (c *nvmlCollector) collectProcesses() {
	c.gm.Lock()
	defer c.gm.Unlock()
	processesByDevice := make(map[string][]GPUProcessSample, len(c.devices))
	for i, device := range c.devices {
		id := fmt.Sprintf("%d", i)
		processesByDevice[id] = c.collectProcessSamples(id, c.names[i], device)
	}
	c.gm.replaceGPUProcessSourceLocked(string(collectorSourceNVML), processesByDevice, true)
}

func (c *nvmlCollector) collect() {
	c.gm.Lock()
	defer c.gm.Unlock()

	processesByDevice := make(map[string][]GPUProcessSample, len(c.devices))
	for i, device := range c.devices {
		id := fmt.Sprintf("%d", i)
		bdf := c.bdfs[i]
		processesByDevice[id] = c.collectProcessSamples(id, c.names[i], device)

		// Update GPUDataMap
		if _, ok := c.gm.GpuDataMap[id]; !ok {
			if c.names[i] == "" {
				continue
			}
			c.gm.GpuDataMap[id] = &system.GPUData{Name: c.names[i]}
		}
		gpu := c.gm.GpuDataMap[id]

		if bdf != "" && !c.isGPUActive(bdf) {
			slog.Debug("NVML: GPU is suspended, skipping", "bdf", bdf)
			gpu.Temperature = 0
			gpu.MemoryUsed = 0
			continue
		}

		// Utilization
		var utilization nvmlUtilization
		if ret := nvmlDeviceGetUtilizationRates(device, &utilization); ret != nvmlReturn(nvmlSuccess) {
			slog.Debug("NVML: Utilization failed (GPU likely suspended)", "bdf", bdf, "ret", ret)
			gpu.Temperature = 0
			gpu.MemoryUsed = 0
			continue
		}

		slog.Debug("NVML: Collecting data for GPU", "bdf", bdf)

		// Temperature
		var temp uint32
		nvmlDeviceGetTemperature(device, 0, &temp) // 0 is NVML_TEMPERATURE_GPU

		// Memory: only poll if GPU is active to avoid leaving D3cold state (#1522)
		if utilization.Gpu > 0 {
			var usedMem, totalMem uint64
			if c.isV2 {
				var memory nvmlMemoryV2
				memory.Version = 0x02000028 // (2 << 24) | 40 bytes
				if ret := nvmlDeviceGetMemoryInfo(device, uintptr(unsafe.Pointer(&memory))); ret != nvmlReturn(nvmlSuccess) {
					slog.Debug("NVML: MemoryInfo_v2 failed", "bdf", bdf, "ret", ret)
				} else {
					usedMem = memory.Used
					totalMem = memory.Total
				}
			} else {
				var memory nvmlMemoryV1
				if ret := nvmlDeviceGetMemoryInfo(device, uintptr(unsafe.Pointer(&memory))); ret != nvmlReturn(nvmlSuccess) {
					slog.Debug("NVML: MemoryInfo failed", "bdf", bdf, "ret", ret)
				} else {
					usedMem = memory.Used
					totalMem = memory.Total
				}
			}
			if totalMem > 0 {
				gpu.MemoryUsed = float64(usedMem) / 1024 / 1024 / mebibytesInAMegabyte
				gpu.MemoryTotal = float64(totalMem) / 1024 / 1024 / mebibytesInAMegabyte
			}
		} else {
			slog.Debug("NVML: Skipping memory info (utilization=0)", "bdf", bdf)
		}

		// Power
		var power uint32
		nvmlDeviceGetPowerUsage(device, &power)

		gpu.Temperature = float64(temp)
		gpu.Usage += float64(utilization.Gpu)
		gpu.Power += float64(power) / 1000.0
		gpu.Count++
		slog.Debug("NVML: Collected data", "gpu", gpu)
	}
	c.gm.replaceGPUProcessSourceLocked(string(collectorSourceNVML), processesByDevice, true)
}

func registerNVMLProcessFunctions(lib uintptr) {
	nvmlDeviceGetComputeRunningProcessesV3 = nil
	nvmlDeviceGetComputeRunningProcessesV2 = nil
	nvmlDeviceGetComputeRunningProcessesV1 = nil
	nvmlDeviceGetGraphicsRunningProcessesV3 = nil
	nvmlDeviceGetGraphicsRunningProcessesV2 = nil
	nvmlDeviceGetGraphicsRunningProcessesV1 = nil
	nvmlDeviceGetProcessUtilization = nil
	if hasSymbol(lib, "nvmlDeviceGetComputeRunningProcesses_v3") {
		purego.RegisterLibFunc(&nvmlDeviceGetComputeRunningProcessesV3, lib, "nvmlDeviceGetComputeRunningProcesses_v3")
	} else if hasSymbol(lib, "nvmlDeviceGetComputeRunningProcesses_v2") {
		purego.RegisterLibFunc(&nvmlDeviceGetComputeRunningProcessesV2, lib, "nvmlDeviceGetComputeRunningProcesses_v2")
	} else if hasSymbol(lib, "nvmlDeviceGetComputeRunningProcesses") {
		purego.RegisterLibFunc(&nvmlDeviceGetComputeRunningProcessesV1, lib, "nvmlDeviceGetComputeRunningProcesses")
	}
	if hasSymbol(lib, "nvmlDeviceGetGraphicsRunningProcesses_v3") {
		purego.RegisterLibFunc(&nvmlDeviceGetGraphicsRunningProcessesV3, lib, "nvmlDeviceGetGraphicsRunningProcesses_v3")
	} else if hasSymbol(lib, "nvmlDeviceGetGraphicsRunningProcesses_v2") {
		purego.RegisterLibFunc(&nvmlDeviceGetGraphicsRunningProcessesV2, lib, "nvmlDeviceGetGraphicsRunningProcesses_v2")
	} else if hasSymbol(lib, "nvmlDeviceGetGraphicsRunningProcesses") {
		purego.RegisterLibFunc(&nvmlDeviceGetGraphicsRunningProcessesV1, lib, "nvmlDeviceGetGraphicsRunningProcesses")
	}
	if hasSymbol(lib, "nvmlDeviceGetProcessUtilization") {
		purego.RegisterLibFunc(&nvmlDeviceGetProcessUtilization, lib, "nvmlDeviceGetProcessUtilization")
	}
}

type nvmlProcessInfo struct {
	pid    int32
	memory *uint64
}

func readNVMLProcesses(device nvmlDevice, fn func(nvmlDevice, *uint32, uintptr) nvmlReturn, version int) []nvmlProcessInfo {
	if fn == nil {
		return nil
	}
	count := uint32(0)
	ret := fn(device, &count, 0)
	if (ret != nvmlReturn(nvmlSuccess) && ret != nvmlReturn(nvmlErrorInsufficientSize)) || count == 0 {
		return nil
	}
	if count > 65536 {
		return nil
	}
	if version >= 2 {
		items := make([]nvmlProcessInfoV2, count)
		if fn(device, &count, uintptr(unsafe.Pointer(&items[0]))) != nvmlReturn(nvmlSuccess) {
			return nil
		}
		if count > uint32(len(items)) {
			return nil
		}
		out := make([]nvmlProcessInfo, 0, count)
		for _, item := range items[:count] {
			if item.PID > 0 {
				out = append(out, nvmlProcessInfo{int32(item.PID), nvmlProcessMemory(item.UsedGpuMemory)})
			}
		}
		return out
	}
	items := make([]nvmlProcessInfoV1, count)
	if fn(device, &count, uintptr(unsafe.Pointer(&items[0]))) != nvmlReturn(nvmlSuccess) {
		return nil
	}
	if count > uint32(len(items)) {
		return nil
	}
	out := make([]nvmlProcessInfo, 0, count)
	for _, item := range items[:count] {
		if item.PID > 0 {
			out = append(out, nvmlProcessInfo{int32(item.PID), nvmlProcessMemory(item.UsedGpuMemory)})
		}
	}
	return out
}

func nvmlProcessMemory(value uint64) *uint64 {
	if value == nvmlValueNotAvailable {
		return nil
	}
	v := value
	return &v
}

func (c *nvmlCollector) collectProcessSamples(id, name string, device nvmlDevice) []GPUProcessSample {
	if c.processLastSeen == nil {
		c.processLastSeen = make(map[string]uint64)
	}
	merged := make(map[int32]GPUProcessSample)
	add := func(items []nvmlProcessInfo) {
		for _, item := range items {
			sample := merged[item.pid]
			sample.DeviceID = id
			sample.DeviceName = name
			sample.PID = item.pid
			sample.Source = string(collectorSourceNVML)
			if item.memory != nil {
				sample.MemoryBytes = item.memory
			}
			merged[item.pid] = sample
		}
	}
	for _, pair := range []struct {
		fn      func(nvmlDevice, *uint32, uintptr) nvmlReturn
		version int
	}{{nvmlDeviceGetComputeRunningProcessesV3, 3}, {nvmlDeviceGetComputeRunningProcessesV2, 2}, {nvmlDeviceGetComputeRunningProcessesV1, 1}, {nvmlDeviceGetGraphicsRunningProcessesV3, 3}, {nvmlDeviceGetGraphicsRunningProcessesV2, 2}, {nvmlDeviceGetGraphicsRunningProcessesV1, 1}} {
		add(readNVMLProcesses(device, pair.fn, pair.version))
	}
	lastSeen := c.processLastSeen[id]
	utilization, nextLastSeen := readNVMLProcessUtilization(device, nvmlDeviceGetProcessUtilization, lastSeen)
	if nextLastSeen > lastSeen {
		c.processLastSeen[id] = nextLastSeen
	}
	for _, item := range utilization {
		if sample, ok := merged[int32(item.PID)]; ok {
			sample.GPUUtilization = nvmlProcessPercent(item.SMUtil)
			sample.EncodeUtilization = nvmlProcessPercent(item.EncUtil)
			sample.DecodeUtilization = nvmlProcessPercent(item.DecUtil)
			merged[int32(item.PID)] = sample
		}
	}
	items := make([]GPUProcessSample, 0, len(merged))
	for _, item := range merged {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].PID < items[j].PID })
	return items
}

func readNVMLProcessUtilization(
	device nvmlDevice,
	fn func(nvmlDevice, uintptr, *uint32, uint64) nvmlReturn,
	lastSeen uint64,
) ([]nvmlProcessUtilization, uint64) {
	if fn == nil {
		return nil, lastSeen
	}
	count := uint32(0)
	ret := fn(device, 0, &count, lastSeen)
	if (ret != nvmlReturn(nvmlSuccess) && ret != nvmlReturn(nvmlErrorInsufficientSize)) || count == 0 || count > 65536 {
		return nil, lastSeen
	}
	items := make([]nvmlProcessUtilization, count)
	if fn(device, uintptr(unsafe.Pointer(&items[0])), &count, lastSeen) != nvmlReturn(nvmlSuccess) || count > uint32(len(items)) {
		return nil, lastSeen
	}

	latest := make(map[uint32]nvmlProcessUtilization, count)
	nextLastSeen := lastSeen
	for _, item := range items[:count] {
		if previous, ok := latest[item.PID]; !ok || item.TimeStamp > previous.TimeStamp {
			latest[item.PID] = item
		}
		if item.TimeStamp > nextLastSeen {
			nextLastSeen = item.TimeStamp
		}
	}
	result := make([]nvmlProcessUtilization, 0, len(latest))
	for _, item := range latest {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PID < result[j].PID })
	return result, nextLastSeen
}

func nvmlProcessPercent(value uint32) *float64 {
	if value > 100 {
		return nil
	}
	percent := float64(value)
	return &percent
}
