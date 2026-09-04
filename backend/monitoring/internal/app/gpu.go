package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os/exec"
	"regexp"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/utils"
)

const (
	// Commands
	nvidiaSmiCmd  string = "nvidia-smi"
	rocmSmiCmd    string = "rocm-smi"
	tegraStatsCmd string = "tegrastats"
	nvtopCmd      string = "nvtop"
	noGPUFoundMsg string = "no GPU found"

	// Command retry and timeout constants
	retryWaitTime     time.Duration = 5 * time.Second
	maxFailureRetries int           = 5

	// Unit Conversions
	mebibytesInAMegabyte float64 = 1.024  // nvidia-smi reports memory in MiB
	milliwattsInAWatt    float64 = 1000.0 // tegrastats reports power in mW
)

// GPUManager manages data collection for GPUs (either Nvidia or AMD)
type GPUManager struct {
	sync.Mutex
	GpuDataMap             map[string]*system.GPUData
	processSamples         map[string][]GPUProcessSample
	processSourceReady     map[string]bool
	processSourcePreferred string
	nvtopDeviceNames       map[string]string
	// lastAvgData stores the last calculated averages for each GPU
	// Used when a collection happens before new data arrives (Count == 0)
	lastAvgData map[string]system.GPUData
	// Per-cache-key tracking for delta calculations
	// cacheKey -> gpuId -> snapshot of last count/usage/power values
	lastSnapshots map[uint16]map[string]*gpuSnapshot
	caps          gpuCapabilities
	lifecycleMu   sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

// gpuSnapshot stores the last observed incremental values for delta tracking
type gpuSnapshot struct {
	count    uint32
	usage    float64
	power    float64
	powerPkg float64
	engines  map[string]float64
}

// RocmSmiJson represents the JSON structure of rocm-smi output
type RocmSmiJson struct {
	ID           string `json:"GUID"`
	Name         string `json:"Card series"`
	Temperature  string `json:"Temperature (Sensor edge) (C)"`
	MemoryUsed   string `json:"VRAM Total Used Memory (B)"`
	MemoryTotal  string `json:"VRAM Total Memory (B)"`
	Usage        string `json:"GPU use (%)"`
	PowerPackage string `json:"Average Graphics Package Power (W)"`
	PowerSocket  string `json:"Current Socket Graphics Package Power (W)"`
}

// gpuCollector defines a collector for a specific GPU management utility (nvidia-smi or rocm-smi)
type gpuCollector struct {
	name    string
	cmdArgs []string
	parse   func([]byte) bool // returns true if valid data was found
	buf     []byte
	bufSize uint16
}

var errNoValidData = fmt.Errorf("no valid GPU data found") // Error for missing data

func sleepUntilDone(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func runRetryingCollector(ctx context.Context, interval time.Duration, collect func() error, handleErr func(error) bool) {
	failures := 0
	for ctx.Err() == nil {
		err := collect()
		if err == nil {
			failures = 0
			if !sleepUntilDone(ctx, interval) {
				return
			}
			continue
		}
		if ctx.Err() != nil {
			return
		}
		failures++
		if failures > maxFailureRetries {
			return
		}
		if handleErr != nil && !handleErr(err) {
			return
		}
		if !sleepUntilDone(ctx, retryWaitTime) {
			return
		}
	}
}

func cleanupStartedCommand(cmd *exec.Cmd, stdout io.Closer, err *error) {
	if stdout != nil {
		_ = stdout.Close()
	}
	if *err != nil && cmd.Process != nil && cmd.ProcessState == nil {
		_ = cmd.Process.Kill()
	}
	if cmd.Process == nil {
		return
	}
	if waitErr := cmd.Wait(); *err == nil && waitErr != nil {
		*err = waitErr
	}
}

// startCollector runs fn on a managed goroutine tagged with pprof labels. The
// labels reach tracebacks and the goroutineleak profile, so a wedged vendor
// collector names itself instead of showing up as an anonymous stack.
func (gm *GPUManager) startCollector(ctx context.Context, name string, fn func()) {
	gm.wg.Go(func() {
		pprof.Do(ctx, pprof.Labels("component", "gpu", "collector", name), func(context.Context) {
			fn()
		})
	})
}

func (gm *GPUManager) Stop() {
	if gm == nil {
		return
	}
	gm.lifecycleMu.Lock()
	cancel := gm.cancel
	gm.cancel = nil
	gm.ctx = nil
	gm.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	gm.wg.Wait()
}

// collectorSource identifies a selectable GPU collector in GPU_COLLECTOR.
type collectorSource string

const (
	collectorSourceNVTop       collectorSource = collectorSource(nvtopCmd)
	collectorSourceNVML        collectorSource = "nvml"
	collectorSourceNvidiaSMI   collectorSource = collectorSource(nvidiaSmiCmd)
	collectorSourceIntelGpuTop collectorSource = collectorSource(intelGpuStatsCmd)
	collectorSourceAmdSysfs    collectorSource = "amd_sysfs"
	collectorSourceRocmSMI     collectorSource = collectorSource(rocmSmiCmd)
	collectorGroupNvidia       string          = "nvidia"
	collectorGroupIntel        string          = "intel"
	collectorGroupAmd          string          = "amd"
)

func isValidCollectorSource(source collectorSource) bool {
	switch source {
	case collectorSourceNVTop,
		collectorSourceNVML,
		collectorSourceNvidiaSMI,
		collectorSourceIntelGpuTop,
		collectorSourceAmdSysfs,
		collectorSourceRocmSMI:
		return true
	}
	return false
}

// gpuCapabilities describes detected GPU tooling and sysfs support on the host.
type gpuCapabilities struct {
	hasNvidiaSmi   bool
	hasNVML        bool
	hasRocmSmi     bool
	hasAmdSysfs    bool
	hasTegrastats  bool
	hasIntelGpuTop bool
	hasNvtop       bool
}

type collectorDefinition struct {
	group              string
	available          bool
	start              func(onFailure func()) bool
	deprecationWarning string
}

// starts and manages the ongoing collection of GPU data for the specified GPU management utility
func (c *gpuCollector) start(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		err := c.collect(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if err == errNoValidData {
				slog.Warn(c.name + " found no valid GPU data, stopping")
				break
			}
			slog.Warn(c.name+" failed, restarting", "err", err)
			if !sleepUntilDone(ctx, retryWaitTime) {
				return
			}
			continue
		}
		if !sleepUntilDone(ctx, retryWaitTime) {
			return
		}
	}
}

// collect executes the command, parses output with the assigned parser function.
// Reaps the child and releases the stdout pipe on every return path so early
// exits (errNoValidData, scanner error) cannot leak processes or FDs.
func (c *gpuCollector) collect(ctx context.Context) (err error) {
	cmd := exec.CommandContext(ctx, c.name, c.cmdArgs...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if startErr := cmd.Start(); startErr != nil {
		return startErr
	}
	defer cleanupStartedCommand(cmd, stdout, &err)

	scanner := bufio.NewScanner(stdout)
	if c.buf == nil {
		c.buf = make([]byte, 0, c.bufSize)
	}
	scanner.Buffer(c.buf, bufio.MaxScanTokenSize)

	for scanner.Scan() {
		hasValidData := c.parse(scanner.Bytes())
		if !hasValidData {
			return errNoValidData
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return fmt.Errorf("scanner error: %w", scanErr)
	}
	return nil
}

// getJetsonParser returns a function to parse the output of tegrastats and update the GPUData map
func (gm *GPUManager) getJetsonParser() func(output []byte) bool {
	// use closure to avoid recompiling the regex
	ramPattern := regexp.MustCompile(`RAM (\d+)/(\d+)MB`)
	gr3dPattern := regexp.MustCompile(`GR3D_FREQ (\d+)%`)
	tempPattern := regexp.MustCompile(`(?:tj|GPU)@(\d+\.?\d*)C`)
	// Orin Nano / NX do not have GPU specific power monitor
	// TODO: Maybe use VDD_IN for Nano / NX and add a total system power chart
	powerPattern := regexp.MustCompile(`(GPU_SOC|CPU_GPU_CV)\s+(\d+)mW|VDD_SYS_GPU\s+(\d+)/\d+`)

	// jetson devices have only one gpu so we'll just initialize here
	gpuData := &system.GPUData{Name: "GPU"}
	gm.GpuDataMap["0"] = gpuData

	return func(output []byte) bool {
		gm.Lock()
		defer gm.Unlock()
		// Parse RAM usage
		ramMatches := ramPattern.FindSubmatch(output)
		if ramMatches != nil {
			gpuData.MemoryUsed, _ = strconv.ParseFloat(string(ramMatches[1]), 64)
			gpuData.MemoryTotal, _ = strconv.ParseFloat(string(ramMatches[2]), 64)
		}
		// Parse GR3D (GPU) usage
		gr3dMatches := gr3dPattern.FindSubmatch(output)
		if gr3dMatches != nil {
			gr3dUsage, _ := strconv.ParseFloat(string(gr3dMatches[1]), 64)
			gpuData.Usage += gr3dUsage
		}
		// Parse temperature
		tempMatches := tempPattern.FindSubmatch(output)
		if tempMatches != nil {
			gpuData.Temperature, _ = strconv.ParseFloat(string(tempMatches[1]), 64)
		}
		// Parse power usage
		powerMatches := powerPattern.FindSubmatch(output)
		if powerMatches != nil {
			// powerMatches[2] is the "(GPU_SOC|CPU_GPU_CV) <N>mW" capture
			// powerMatches[3] is the "VDD_SYS_GPU <N>/<N>" capture
			powerStr := string(powerMatches[2])
			if powerStr == "" {
				powerStr = string(powerMatches[3])
			}
			power, _ := strconv.ParseFloat(powerStr, 64)
			gpuData.Power += power / milliwattsInAWatt
		}
		gpuData.Count++
		return true
	}
}

// parseNvidiaData parses the output of nvidia-smi and updates the GPUData map
func (gm *GPUManager) parseNvidiaData(output []byte) bool {
	gm.Lock()
	defer gm.Unlock()
	scanner := bufio.NewScanner(bytes.NewReader(output))
	var valid bool
	for scanner.Scan() {
		line := scanner.Text() // Or use scanner.Bytes() for []byte
		fields := strings.Split(strings.TrimSpace(line), ", ")
		if len(fields) < 7 {
			continue
		}
		valid = true
		id := fields[0]
		temp, _ := strconv.ParseFloat(fields[2], 64)
		memoryUsage, _ := strconv.ParseFloat(fields[3], 64)
		totalMemory, _ := strconv.ParseFloat(fields[4], 64)
		usage, _ := strconv.ParseFloat(fields[5], 64)
		power, _ := strconv.ParseFloat(fields[6], 64)
		// add gpu if not exists
		if _, ok := gm.GpuDataMap[id]; !ok {
			name := strings.TrimPrefix(fields[1], "NVIDIA ")
			gm.GpuDataMap[id] = &system.GPUData{Name: strings.TrimSuffix(name, " Laptop GPU")}
		}
		// update gpu data
		gpu := gm.GpuDataMap[id]
		gpu.Temperature = temp
		gpu.MemoryUsed = memoryUsage / mebibytesInAMegabyte
		gpu.MemoryTotal = totalMemory / mebibytesInAMegabyte
		gpu.Usage += usage
		gpu.Power += power
		gpu.Count++
	}
	return valid
}

// parseAmdData parses the output of rocm-smi and updates the GPUData map
func (gm *GPUManager) parseAmdData(output []byte) bool {
	var rocmSmiInfo map[string]RocmSmiJson
	if err := json.Unmarshal(output, &rocmSmiInfo); err != nil || len(rocmSmiInfo) == 0 {
		return false
	}
	gm.Lock()
	defer gm.Unlock()
	for _, v := range rocmSmiInfo {
		var power float64
		if v.PowerPackage != "" {
			power, _ = strconv.ParseFloat(v.PowerPackage, 64)
		} else {
			power, _ = strconv.ParseFloat(v.PowerSocket, 64)
		}
		memoryUsage, _ := strconv.ParseFloat(v.MemoryUsed, 64)
		totalMemory, _ := strconv.ParseFloat(v.MemoryTotal, 64)
		usage, _ := strconv.ParseFloat(v.Usage, 64)

		id := v.ID
		if _, ok := gm.GpuDataMap[id]; !ok {
			gm.GpuDataMap[id] = &system.GPUData{Name: v.Name}
		}
		gpu := gm.GpuDataMap[id]
		gpu.Temperature, _ = strconv.ParseFloat(v.Temperature, 64)
		gpu.MemoryUsed = utils.BytesToMegabytes(memoryUsage)
		gpu.MemoryTotal = utils.BytesToMegabytes(totalMemory)
		gpu.Usage += usage
		gpu.Power += power
		gpu.Count++
	}
	return true
}

// GetCurrentData returns GPU utilization data averaged since the last call with this cacheKey
func (gm *GPUManager) GetCurrentData(cacheKey uint16) map[string]system.GPUData {
	gm.Lock()
	defer gm.Unlock()

	gm.initializeSnapshots(cacheKey)
	nameCounts := gm.countGPUNames()

	gpuData := make(map[string]system.GPUData, len(gm.GpuDataMap))
	for id, gpu := range gm.GpuDataMap {
		gpuAvg := gm.calculateGPUAverage(id, gpu, cacheKey)
		gm.updateInstantaneousValues(&gpuAvg, gpu)
		gm.storeSnapshot(id, gpu, cacheKey)

		// Append id to name if there are multiple GPUs with the same name
		if nameCounts[gpu.Name] > 1 {
			gpuAvg.Name = fmt.Sprintf("%s %s", gpu.Name, id)
		}
		gpuData[id] = gpuAvg
	}
	if len(gpuData) > 0 {
		slog.Debug("GPU data collected", "count", len(gpuData))
	}
	return gpuData
}

// initializeSnapshots ensures snapshot maps are initialized for the given cache key
func (gm *GPUManager) initializeSnapshots(cacheKey uint16) {
	if gm.lastAvgData == nil {
		gm.lastAvgData = make(map[string]system.GPUData)
	}
	if gm.lastSnapshots == nil {
		gm.lastSnapshots = make(map[uint16]map[string]*gpuSnapshot)
	}
	if gm.lastSnapshots[cacheKey] == nil {
		gm.lastSnapshots[cacheKey] = make(map[string]*gpuSnapshot)
	}
}

// countGPUNames returns a map of GPU names to their occurrence count
func (gm *GPUManager) countGPUNames() map[string]int {
	nameCounts := make(map[string]int)
	for _, gpu := range gm.GpuDataMap {
		nameCounts[gpu.Name]++
	}
	return nameCounts
}

// calculateGPUAverage computes the average GPU metrics since the last snapshot for this cache key
func (gm *GPUManager) calculateGPUAverage(id string, gpu *system.GPUData, cacheKey uint16) system.GPUData {
	lastSnapshot := gm.lastSnapshots[cacheKey][id]
	currentCount := uint32(gpu.Count)
	deltaCount := gm.calculateDeltaCount(currentCount, lastSnapshot)

	// If no new data arrived
	if deltaCount == 0 {
		// If GPU appears suspended (instantaneous values are 0), return zero values
		// Otherwise return last known average for temporary collection gaps
		if gpu.Temperature == 0 && gpu.MemoryUsed == 0 {
			return system.GPUData{Name: gpu.Name}
		}
		return gm.lastAvgData[id] // zero value if not found
	}

	// Calculate new average
	gpuAvg := *gpu
	deltaUsage, deltaPower, deltaPowerPkg := gm.calculateDeltas(gpu, lastSnapshot)

	gpuAvg.Power = utils.TwoDecimals(deltaPower / float64(deltaCount))

	if gpu.Engines != nil {
		// make fresh map for averaged engine metrics to avoid mutating
		// the accumulator map stored in gm.GpuDataMap
		gpuAvg.Engines = make(map[string]float64, len(gpu.Engines))
		gpuAvg.Usage = gm.calculateIntelGPUUsage(&gpuAvg, gpu, lastSnapshot, deltaCount)
		gpuAvg.PowerPkg = utils.TwoDecimals(deltaPowerPkg / float64(deltaCount))
	} else {
		gpuAvg.Usage = utils.TwoDecimals(deltaUsage / float64(deltaCount))
	}

	gm.lastAvgData[id] = gpuAvg
	return gpuAvg
}

// calculateDeltaCount returns the change in count since the last snapshot
func (gm *GPUManager) calculateDeltaCount(currentCount uint32, lastSnapshot *gpuSnapshot) uint32 {
	if lastSnapshot != nil {
		return currentCount - lastSnapshot.count
	}
	return currentCount
}

// calculateDeltas computes the change in usage, power, and powerPkg since the last snapshot
func (gm *GPUManager) calculateDeltas(gpu *system.GPUData, lastSnapshot *gpuSnapshot) (deltaUsage, deltaPower, deltaPowerPkg float64) {
	if lastSnapshot != nil {
		return gpu.Usage - lastSnapshot.usage,
			gpu.Power - lastSnapshot.power,
			gpu.PowerPkg - lastSnapshot.powerPkg
	}
	return gpu.Usage, gpu.Power, gpu.PowerPkg
}

// calculateIntelGPUUsage computes Intel GPU usage from engine metrics and returns max engine usage
func (gm *GPUManager) calculateIntelGPUUsage(gpuAvg, gpu *system.GPUData, lastSnapshot *gpuSnapshot, deltaCount uint32) float64 {
	maxEngineUsage := 0.0
	for name, engine := range gpu.Engines {
		var deltaEngine float64
		if lastSnapshot != nil && lastSnapshot.engines != nil {
			deltaEngine = engine - lastSnapshot.engines[name]
		} else {
			deltaEngine = engine
		}
		gpuAvg.Engines[name] = utils.TwoDecimals(deltaEngine / float64(deltaCount))
		maxEngineUsage = max(maxEngineUsage, deltaEngine/float64(deltaCount))
	}
	return utils.TwoDecimals(maxEngineUsage)
}

// updateInstantaneousValues updates values that should reflect current state, not averages
func (gm *GPUManager) updateInstantaneousValues(gpuAvg *system.GPUData, gpu *system.GPUData) {
	gpuAvg.Temperature = utils.TwoDecimals(gpu.Temperature)
	gpuAvg.MemoryUsed = utils.TwoDecimals(gpu.MemoryUsed)
	gpuAvg.MemoryTotal = utils.TwoDecimals(gpu.MemoryTotal)
}

// storeSnapshot saves the current GPU state for this cache key
func (gm *GPUManager) storeSnapshot(id string, gpu *system.GPUData, cacheKey uint16) {
	snapshot := &gpuSnapshot{
		count:    uint32(gpu.Count),
		usage:    gpu.Usage,
		power:    gpu.Power,
		powerPkg: gpu.PowerPkg,
	}
	if gpu.Engines != nil {
		snapshot.engines = make(map[string]float64, len(gpu.Engines))
		maps.Copy(snapshot.engines, gpu.Engines)
	}
	gm.lastSnapshots[cacheKey][id] = snapshot
}

// discoverGpuCapabilities checks for available GPU tooling and sysfs support.
// It only reports capability presence and does not apply policy decisions.
func (gm *GPUManager) discoverGpuCapabilities() gpuCapabilities {
	caps := gpuCapabilities{
		hasAmdSysfs: gm.hasAmdSysfs(),
		hasNVML:     detectNVMLAvailability(),
	}
	if _, err := exec.LookPath(nvidiaSmiCmd); err == nil {
		caps.hasNvidiaSmi = true
	}
	if _, err := exec.LookPath(rocmSmiCmd); err == nil {
		caps.hasRocmSmi = true
	}
	if _, err := exec.LookPath(tegraStatsCmd); err == nil {
		caps.hasTegrastats = true
	}
	if _, err := exec.LookPath(intelGpuStatsCmd); err == nil {
		caps.hasIntelGpuTop = true
	}
	if _, err := exec.LookPath(nvtopCmd); err == nil {
		caps.hasNvtop = true
	}
	return caps
}

func hasAnyGpuCollector(caps gpuCapabilities) bool {
	return caps.hasNvidiaSmi || caps.hasNVML || caps.hasRocmSmi || caps.hasAmdSysfs || caps.hasTegrastats || caps.hasIntelGpuTop || caps.hasNvtop
}

func availableCollectorSources(caps gpuCapabilities) []collectorSource {
	available := make([]collectorSource, 0, 9)
	if caps.hasTegrastats {
		available = append(available, collectorSource(tegraStatsCmd))
	}
	if caps.hasNvidiaSmi {
		available = append(available, collectorSourceNvidiaSMI)
	}
	if caps.hasNVML {
		available = append(available, collectorSourceNVML)
	}
	if caps.hasAmdSysfs {
		available = append(available, collectorSourceAmdSysfs)
	}
	if caps.hasRocmSmi {
		available = append(available, collectorSourceRocmSMI)
	}
	if caps.hasIntelGpuTop {
		available = append(available, collectorSourceIntelGpuTop)
	}
	if caps.hasNvtop {
		available = append(available, collectorSourceNVTop)
	}
	return available
}

func collectorSourceStrings(sources []collectorSource) []string {
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		names = append(names, string(source))
	}
	return names
}

func logGPUDiscovery(mode string, caps gpuCapabilities, requested, selected []collectorSource, reason string) {
	args := []any{
		"mode", mode,
		"available", collectorSourceStrings(availableCollectorSources(caps)),
	}
	if len(requested) > 0 {
		args = append(args, "requested", collectorSourceStrings(requested))
	}
	if len(selected) > 0 {
		args = append(args, "selected", collectorSourceStrings(selected))
	}
	if reason != "" {
		args = append(args, "reason", reason)
	}
	slog.Info(gpuDiscoveryLogMessage(mode, selected, reason), args...)
}

func gpuDiscoveryLogMessage(mode string, selected []collectorSource, reason string) string {
	if len(selected) > 1 {
		return "Detected GPU collectors"
	}
	if len(selected) == 1 {
		return "Detected GPU collector"
	}
	if mode == "disabled" && reason == "SKIP_GPU=true" {
		return "GPU monitoring disabled"
	}
	return "No GPU collector detected"
}

func (gm *GPUManager) startIntelCollector(ctx context.Context) {
	gm.startCollector(ctx, "intel", func() {
		runRetryingCollector(ctx, retryWaitTime, func() error {
			return gm.collectIntelStats(ctx)
		}, func(err error) bool {
			slog.Warn("Error collecting Intel GPU data", "err", err)
			return true
		})
	})
}

func (gm *GPUManager) startNvidiaSmiCollector(ctx context.Context, intervalSeconds string) {
	collector := gpuCollector{
		name:    nvidiaSmiCmd,
		bufSize: 10 * 1024,
		cmdArgs: []string{
			"-l", intervalSeconds,
			"--query-gpu=index,name,temperature.gpu,memory.used,memory.total,utilization.gpu,power.draw",
			"--format=csv,noheader,nounits",
		},
		parse: gm.parseNvidiaData,
	}
	gm.startCollector(ctx, "nvidia-smi", func() {
		collector.start(ctx)
	})
}

func (gm *GPUManager) startTegraStatsCollector(ctx context.Context, intervalMilliseconds string) {
	collector := gpuCollector{
		name:    tegraStatsCmd,
		bufSize: 10 * 1024,
		cmdArgs: []string{"--interval", intervalMilliseconds},
		parse:   gm.getJetsonParser(),
	}
	gm.startCollector(ctx, "tegrastats", func() {
		collector.start(ctx)
	})
}

func (gm *GPUManager) startRocmSmiCollector(ctx context.Context, pollInterval time.Duration) {
	collector := gpuCollector{
		name:    rocmSmiCmd,
		bufSize: 10 * 1024,
		cmdArgs: []string{"--showid", "--showtemp", "--showuse", "--showpower", "--showproductname", "--showmeminfo", "vram", "--json"},
		parse:   gm.parseAmdData,
	}
	gm.startCollector(ctx, "rocm-smi", func() {
		failures := 0
		for {
			if ctx.Err() != nil {
				return
			}
			if err := collector.collect(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				failures++
				if failures > maxFailureRetries {
					break
				}
				slog.Warn("Error collecting AMD GPU data via rocm-smi", "err", err)
			}
			if !sleepUntilDone(ctx, pollInterval) {
				return
			}
		}
	})
}

func (gm *GPUManager) collectorDefinitions(ctx context.Context, caps gpuCapabilities) map[collectorSource]collectorDefinition {
	return map[collectorSource]collectorDefinition{
		collectorSourceNVML: {
			group:     collectorGroupNvidia,
			available: true,
			start: func(_ func()) bool {
				return gm.startNvmlCollector(ctx)
			},
		},
		collectorSourceNvidiaSMI: {
			group:     collectorGroupNvidia,
			available: caps.hasNvidiaSmi,
			start: func(_ func()) bool {
				gm.startNvidiaSmiCollector(ctx, "4") // seconds
				return true
			},
		},
		collectorSourceIntelGpuTop: {
			group:     collectorGroupIntel,
			available: caps.hasIntelGpuTop,
			start: func(_ func()) bool {
				gm.startIntelCollector(ctx)
				return true
			},
		},
		collectorSourceAmdSysfs: {
			group:     collectorGroupAmd,
			available: caps.hasAmdSysfs,
			start: func(_ func()) bool {
				return gm.startAmdSysfsCollector(ctx)
			},
		},
		collectorSourceRocmSMI: {
			group:              collectorGroupAmd,
			available:          caps.hasRocmSmi,
			deprecationWarning: "rocm-smi may be removed in a future release",
			start: func(_ func()) bool {
				gm.startRocmSmiCollector(ctx, 4300*time.Millisecond)
				return true
			},
		},
		collectorSourceNVTop: {
			available: caps.hasNvtop,
			start: func(onFailure func()) bool {
				gm.startNvtopCollector(ctx, "30", onFailure) // tens of milliseconds
				return true
			},
		},
	}
}

// parseCollectorPriority parses GPU_COLLECTOR and returns valid ordered entries.
func parseCollectorPriority(value string) []collectorSource {
	parts := strings.Split(value, ",")
	priorities := make([]collectorSource, 0, len(parts))
	for _, raw := range parts {
		name := collectorSource(strings.TrimSpace(strings.ToLower(raw)))
		if !isValidCollectorSource(name) {
			if name != "" {
				slog.Warn("Ignoring unknown GPU collector", "collector", name)
			}
			continue
		}
		priorities = append(priorities, name)
	}
	return priorities
}

// startNvmlCollector initializes NVML and starts its polling loop.
func (gm *GPUManager) startNvmlCollector(ctx context.Context) bool {
	collector := &nvmlCollector{gm: gm}
	//nolint:staticcheck // SA4023: init always fails in the non-glibc stub build; the glibc build can return nil.
	if err := collector.init(); err != nil {
		slog.Warn("Failed to initialize NVML", "err", err)
		return false
	}
	gm.startCollector(ctx, "nvml", func() {
		collector.start(ctx)
	})
	return true
}

// startNvmlProcessCollector uses NVML only for process attribution when the
// aggregate NVIDIA collector is nvidia-smi. This preserves the selected
// aggregate backend while still exposing PID-level GPU samples.
func (gm *GPUManager) startNvmlProcessCollector(ctx context.Context) bool {
	collector := &nvmlCollector{gm: gm, processOnly: true}
	//nolint:staticcheck // SA4023: init always fails in the non-glibc stub build; the glibc build can return nil.
	if err := collector.init(); err != nil {
		slog.Warn("Failed to initialize NVML GPU process collection", "err", err)
		return false
	}
	gm.startCollector(ctx, "nvml-process", func() {
		collector.startProcesses(ctx)
	})
	return true
}

// startAmdSysfsCollector starts AMD GPU collection via sysfs.
func (gm *GPUManager) startAmdSysfsCollector(ctx context.Context) bool {
	gm.startCollector(ctx, "amd-sysfs", func() {
		if err := gm.collectAmdStats(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("Error collecting AMD GPU data via sysfs", "err", err)
		}
	})
	return true
}

// startCollectorsByPriority starts collectors in order with one source per vendor group.
func (gm *GPUManager) startCollectorsByPriority(ctx context.Context, priorities []collectorSource, caps gpuCapabilities) []collectorSource {
	definitions := gm.collectorDefinitions(ctx, caps)
	selectedGroups := make(map[string]bool, 3)
	started := make([]collectorSource, 0, len(priorities))
	for i, source := range priorities {
		definition, ok := definitions[source]
		if !ok || !definition.available {
			continue
		}
		// nvtop is not a vendor-specific collector, so should only be used if no other collectors are selected or it is first in GPU_COLLECTOR.
		if source == collectorSourceNVTop {
			if len(selectedGroups) > 0 {
				slog.Warn("Skipping nvtop because other collectors are selected")
				continue
			}
			// if nvtop fails, fall back to remaining collectors.
			remaining := append([]collectorSource(nil), priorities[i+1:]...)
			if definition.start(func() {
				gm.startCollectorsByPriority(ctx, remaining, caps)
			}) {
				started = append(started, source)
				return started
			}
		}
		group := definition.group
		if group == "" || selectedGroups[group] {
			continue
		}
		if definition.deprecationWarning != "" {
			slog.Warn(definition.deprecationWarning)
		}
		if definition.start(nil) {
			selectedGroups[group] = true
			started = append(started, source)
		}
	}
	return started
}

// resolveAutoCollectorPriority builds the default collector order when GPU_COLLECTOR is unset.
func (gm *GPUManager) resolveAutoCollectorPriority(caps gpuCapabilities) []collectorSource {
	priorities := make([]collectorSource, 0, 4)

	if !caps.hasTegrastats {
		if caps.hasNvidiaSmi {
			priorities = append(priorities, collectorSourceNvidiaSMI)
		}
		if caps.hasNVML {
			priorities = append(priorities, collectorSourceNVML)
		}
	}

	if caps.hasAmdSysfs {
		priorities = append(priorities, collectorSourceAmdSysfs)
	}
	if caps.hasRocmSmi {
		priorities = append(priorities, collectorSourceRocmSMI)
	}

	if caps.hasIntelGpuTop {
		priorities = append(priorities, collectorSourceIntelGpuTop)
	}

	// Keep nvtop as a last resort only when no vendor collector exists.
	if len(priorities) == 0 && caps.hasNvtop {
		priorities = append(priorities, collectorSourceNVTop)
	}
	return priorities
}

// NewGPUManager creates a GPUManager. Start begins the long-lived collectors.
func NewGPUManager() (*GPUManager, error) {
	if skipGPU, _ := utils.GetEnv("SKIP_GPU"); skipGPU == "true" {
		logGPUDiscovery("disabled", gpuCapabilities{}, nil, nil, "SKIP_GPU=true")
		return nil, nil
	}
	var gm GPUManager
	caps := gm.discoverGpuCapabilities()
	gm.GpuDataMap = make(map[string]*system.GPUData)
	gm.processSamples = make(map[string][]GPUProcessSample)
	gm.processSourceReady = make(map[string]bool)
	gm.nvtopDeviceNames = make(map[string]string)
	gm.caps = caps

	return &gm, nil
}

// Start begins GPU collectors and binds their lifetime to ctx.
func (gm *GPUManager) Start(ctx context.Context) error {
	if gm == nil {
		return nil
	}
	gm.lifecycleMu.Lock()
	if gm.cancel != nil {
		gm.lifecycleMu.Unlock()
		return nil
	}
	gm.ctx, gm.cancel = context.WithCancel(ctx)
	collectorCtx := gm.ctx
	gm.lifecycleMu.Unlock()
	caps := gm.caps

	// Jetson devices should always use tegrastats (ignore GPU_COLLECTOR).
	if caps.hasTegrastats {
		gm.startTegraStatsCollector(collectorCtx, "3700")
		if caps.hasNvtop {
			gm.setGPUProcessSourcePreference(nvtopCmd)
			gm.startNvtopProcessCollector(collectorCtx, "30")
		}
		logGPUDiscovery("auto", caps, nil, []collectorSource{collectorSource(tegraStatsCmd)}, "tegrastats detected")
		return nil
	}

	// Respect explicit collector selection before capability auto-detection.
	if collectorConfig, ok := utils.GetEnv("GPU_COLLECTOR"); ok && strings.TrimSpace(collectorConfig) != "" {
		priorities := parseCollectorPriority(collectorConfig)
		started := gm.startCollectorsByPriority(collectorCtx, priorities, caps)
		if len(started) == 0 {
			logGPUDiscovery("configured", caps, priorities, nil, "no configured GPU collectors started")
			gm.Stop()
			return fmt.Errorf("no configured GPU collectors are available")
		}
		gm.selectConfiguredGPUProcessSource(started)
		logGPUDiscovery("configured", caps, priorities, started, "")
		return nil
	}

	if !hasAnyGpuCollector(caps) {
		logGPUDiscovery("disabled", caps, nil, nil, "no GPU collector detected")
		gm.Stop()
		return errors.New(noGPUFoundMsg)
	}

	// auto-detect and start collectors when GPU_COLLECTOR is unset.
	autoPriorities := gm.resolveAutoCollectorPriority(caps)
	started := gm.startCollectorsByPriority(collectorCtx, autoPriorities, caps)
	if len(started) == 0 {
		logGPUDiscovery("auto", caps, autoPriorities, nil, "no GPU collectors started")
		gm.Stop()
		return errors.New(noGPUFoundMsg)
	}
	gm.startAutoGPUProcessCollector(collectorCtx, caps, started)
	logGPUDiscovery("auto", caps, autoPriorities, started, "")

	return nil
}

func (gm *GPUManager) selectConfiguredGPUProcessSource(started []collectorSource) {
	if slices.Contains(started, collectorSourceNVTop) {
		gm.setGPUProcessSourcePreference(nvtopCmd)
		return
	}
	if slices.Contains(started, collectorSourceNVML) {
		gm.setGPUProcessSourcePreference(string(collectorSourceNVML))
		return
	}
}

func (gm *GPUManager) startAutoGPUProcessCollector(ctx context.Context, caps gpuCapabilities, started []collectorSource) {
	if slices.Contains(started, collectorSourceNVTop) {
		gm.setGPUProcessSourcePreference(nvtopCmd)
		return
	}
	if caps.hasNvtop {
		gm.setGPUProcessSourcePreference(nvtopCmd)
		gm.startNvtopProcessCollector(ctx, "30") // nvtop delay is in deciseconds.
		return
	}
	if slices.Contains(started, collectorSourceNVML) {
		gm.setGPUProcessSourcePreference(string(collectorSourceNVML))
		return
	}
	for _, source := range started {
		if source == collectorSourceNvidiaSMI && caps.hasNVML && gm.startNvmlProcessCollector(ctx) {
			gm.setGPUProcessSourcePreference(string(collectorSourceNVML))
			return
		}
	}
}
