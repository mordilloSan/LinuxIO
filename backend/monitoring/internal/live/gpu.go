package live

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/hwprobe"
	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

type nvidiaGPUStats struct {
	DriverVersion       string
	TemperatureC        float64
	FanPercent          float64
	UtilizationPercent  float64
	MemoryTotalBytes    uint64
	MemoryUsedBytes     uint64
	PowerDrawWatts      float64
	PowerLimitWatts     float64
	CurrentGraphicsMHz  int
	MaxGraphicsClockMHz int
}

func fetchGPUInfo(ctx context.Context) (map[string]monitoringapi.LiveGPU, error) {
	// Cached: the card list is static topology, and building it parses the
	// whole PCI IDs database. The live data the dashboard polls for
	// (utilization, temperatures, clocks) comes from the sysfs/nvidia-smi
	// enrichment below, which still runs on every call.
	info, err := hwprobe.CachedGPUInfo()
	if err != nil || info == nil {
		return nil, fmt.Errorf("failed to retrieve GPU information: %w", err)
	}

	nvidiaStats := readNvidiaSMIStats(ctx)
	gpus := make(map[string]monitoringapi.LiveGPU, len(info.GraphicsCards))
	for _, card := range info.GraphicsCards {
		entry := hwprobe.BuildGPUEntry(card)
		enrichGPUFromSysfs(ctx, card.Address, entry)
		if stats, ok := nvidiaStats[hwprobe.NormalizePCIAddress(card.Address)]; ok {
			mergeNvidiaStats(entry, stats)
		}
		device, err := liveGPUFromEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("build GPU response: %w", err)
		}
		gpus[hwprobe.NormalizePCIAddress(card.Address)] = device
	}

	return gpus, nil
}

// GPUs lists the host GPUs and overlays the figures the collector sampled.
func GPUs(ctx context.Context, sampled map[string]systemmodel.GPUData) map[string]monitoringapi.LiveGPU {
	result, err := fetchGPUInfo(ctx)
	if err != nil {
		result = make(map[string]monitoringapi.LiveGPU)
	}
	if sampled == nil {
		return result
	}
	for id, data := range sampled {
		mergeCollectorGPU(result, id, data)
	}
	return result
}

func mergeCollectorGPU(result map[string]monitoringapi.LiveGPU, id string, data systemmodel.GPUData) {
	key := hwprobe.NormalizePCIAddress(data.Address)
	if key == "" {
		key = hwprobe.NormalizePCIAddress(id)
	}
	value := liveGPUFromStats(data)
	if existing, ok := result[key]; ok {
		mergeLiveGPU(&existing, value)
		result[key] = existing
		return
	}
	if data.Address != "" {
		result[key] = value
	} else {
		result[id] = value
	}
}

func liveGPUFromStats(data systemmodel.GPUData) monitoringapi.LiveGPU {
	value := monitoringapi.LiveGPU{}
	if data.Temperature > 0 {
		value.TemperatureC = &data.Temperature
	}
	if data.MemoryUsed > 0 {
		bytes := uint64(data.MemoryUsed * 1048576)
		value.MemoryUsedBytes = &bytes
	}
	if data.MemoryTotal > 0 {
		bytes := uint64(data.MemoryTotal * 1048576)
		value.MemoryTotalBytes = &bytes
		if value.MemoryUsedBytes != nil && *value.MemoryUsedBytes <= bytes {
			free := bytes - *value.MemoryUsedBytes
			value.MemoryFreeBytes = &free
		}
	}
	if data.Usage > 0 {
		value.UtilizationPercent = &data.Usage
	}
	if data.Power > 0 {
		value.PowerDrawWatts = &data.Power
	}
	return value
}

func mergeLiveGPU(dst *monitoringapi.LiveGPU, src monitoringapi.LiveGPU) {
	if src.TemperatureC != nil {
		dst.TemperatureC = src.TemperatureC
	}
	if src.MemoryUsedBytes != nil {
		dst.MemoryUsedBytes = src.MemoryUsedBytes
	}
	if src.MemoryTotalBytes != nil {
		dst.MemoryTotalBytes = src.MemoryTotalBytes
	}
	if src.MemoryFreeBytes != nil {
		dst.MemoryFreeBytes = src.MemoryFreeBytes
	}
	if src.UtilizationPercent != nil {
		dst.UtilizationPercent = src.UtilizationPercent
	}
	if src.PowerDrawWatts != nil {
		dst.PowerDrawWatts = src.PowerDrawWatts
	}
}

func liveGPUFromEntry(entry map[string]any) (monitoringapi.LiveGPU, error) {
	var device monitoringapi.LiveGPU
	data, err := json.Marshal(entry)
	if err != nil {
		return device, err
	}
	return device, json.Unmarshal(data, &device)
}

func mergeNvidiaStats(entry map[string]any, stats nvidiaGPUStats) {
	hwprobe.SetIfNonEmpty(entry, "driver_version", stats.DriverVersion)
	setFloatIfPositive(entry, "temperature_c", stats.TemperatureC)
	setFloatIfPositive(entry, "fan_percent", stats.FanPercent)
	setFloatIfPositive(entry, "utilization_percent", stats.UtilizationPercent)
	setUint64IfPositive(entry, "memory_total_bytes", stats.MemoryTotalBytes)
	setUint64IfPositive(entry, "memory_used_bytes", stats.MemoryUsedBytes)
	if stats.MemoryTotalBytes > 0 && stats.MemoryUsedBytes <= stats.MemoryTotalBytes {
		entry["memory_free_bytes"] = stats.MemoryTotalBytes - stats.MemoryUsedBytes
	}
	setFloatIfPositive(entry, "power_draw_watts", stats.PowerDrawWatts)
	setFloatIfPositive(entry, "power_limit_watts", stats.PowerLimitWatts)
	setIntIfPositive(entry, "current_freq_mhz", stats.CurrentGraphicsMHz)
	setIntIfPositive(entry, "max_freq_mhz", stats.MaxGraphicsClockMHz)
}

func enrichGPUFromSysfs(ctx context.Context, pciAddr string, entry map[string]any) {
	cardName, cardDir, pciDir, ok := hwprobe.FindGPUCardDir(pciAddr)
	if !ok {
		return
	}

	entry["drm_card"] = cardName
	hwprobe.SetIfNonEmpty(entry, "runtime_status", hwprobe.ReadSysfsString(filepath.Join(pciDir, "power", "runtime_status")))
	hwprobe.SetIfNonEmpty(entry, "power_state", hwprobe.ReadSysfsString(filepath.Join(pciDir, "power_state")))
	hwprobe.SetIfNonEmpty(entry, "link_speed", hwprobe.SanitizeUnknown(hwprobe.ReadSysfsString(filepath.Join(pciDir, "current_link_speed"))))
	hwprobe.SetIfNonEmpty(entry, "link_width", hwprobe.SanitizeZero(hwprobe.ReadSysfsString(filepath.Join(pciDir, "current_link_width"))))
	hwprobe.SetIfNonEmpty(entry, "max_link_speed", hwprobe.SanitizeUnknown(hwprobe.ReadSysfsString(filepath.Join(pciDir, "max_link_speed"))))
	hwprobe.SetIfNonEmpty(entry, "max_link_width", hwprobe.SanitizeZero(hwprobe.ReadSysfsString(filepath.Join(pciDir, "max_link_width"))))
	hwprobe.SetIfNonEmpty(entry, "driver_module", hwprobe.DriverModule(pciDir))
	hwprobe.SetIfNonEmpty(entry, "raw_class", hwprobe.ReadSysfsString(filepath.Join(pciDir, "class")))

	if v, ok := hwprobe.ReadSysfsBool(filepath.Join(pciDir, "boot_vga")); ok {
		entry["boot_vga"] = v
	}

	if v, ok := hwprobe.ReadSysfsIntAny(
		filepath.Join(cardDir, "gt_cur_freq_mhz"),
		filepath.Join(cardDir, "gt", "gt0", "rps_cur_freq_mhz"),
	); ok {
		setIntIfPositive(entry, "current_freq_mhz", v)
	}
	if v, ok := hwprobe.ReadSysfsIntAny(
		filepath.Join(cardDir, "gt_act_freq_mhz"),
		filepath.Join(cardDir, "gt", "gt0", "rps_act_freq_mhz"),
	); ok {
		setIntIfPositive(entry, "actual_freq_mhz", v)
	}
	if v, ok := hwprobe.ReadSysfsIntAny(
		filepath.Join(cardDir, "gt_min_freq_mhz"),
		filepath.Join(cardDir, "gt", "gt0", "rps_min_freq_mhz"),
	); ok {
		setIntIfPositive(entry, "min_freq_mhz", v)
	}
	if v, ok := hwprobe.ReadSysfsIntAny(
		filepath.Join(cardDir, "gt_max_freq_mhz"),
		filepath.Join(cardDir, "gt", "gt0", "rps_max_freq_mhz"),
	); ok {
		setIntIfPositive(entry, "max_freq_mhz", v)
	}
	if v, ok := hwprobe.ReadSysfsIntAny(
		filepath.Join(cardDir, "gt_boost_freq_mhz"),
		filepath.Join(cardDir, "gt", "gt0", "rps_boost_freq_mhz"),
	); ok {
		setIntIfPositive(entry, "boost_freq_mhz", v)
	}
	if v, ok := hwprobe.ReadSysfsIntAny(filepath.Join(cardDir, "gt", "gt0", "punit_req_freq_mhz")); ok {
		setIntIfPositive(entry, "requested_freq_mhz", v)
	}
	if v, ok := hwprobe.ReadSysfsIntAny(
		filepath.Join(cardDir, "gt_RP0_freq_mhz"),
		filepath.Join(cardDir, "gt", "gt0", "rps_RP0_freq_mhz"),
	); ok {
		setIntIfPositive(entry, "rp0_freq_mhz", v)
	}
	if v, ok := hwprobe.ReadSysfsIntAny(
		filepath.Join(cardDir, "gt_RP1_freq_mhz"),
		filepath.Join(cardDir, "gt", "gt0", "rps_RP1_freq_mhz"),
	); ok {
		setIntIfPositive(entry, "rp1_freq_mhz", v)
	}
	if v, ok := hwprobe.ReadSysfsIntAny(
		filepath.Join(cardDir, "gt_RPn_freq_mhz"),
		filepath.Join(cardDir, "gt", "gt0", "rps_RPn_freq_mhz"),
	); ok {
		setIntIfPositive(entry, "rpn_freq_mhz", v)
	}

	if util, ok := readGPUUtilization(ctx, cardDir, pciDir); ok {
		entry["utilization_percent"] = util
	}
	if rc6, ok := hwprobe.ReadSysfsUint64Any(
		filepath.Join(cardDir, "power", "rc6_residency_ms"),
		filepath.Join(cardDir, "gt", "gt0", "rc6_residency_ms"),
	); ok {
		entry["rc6_residency_ms"] = rc6
	}

	addMemoryMetric(entry, "memory_total_bytes", filepath.Join(pciDir, "mem_info_vram_total"))
	addMemoryMetric(entry, "memory_used_bytes", filepath.Join(pciDir, "mem_info_vram_used"))
	addMemoryMetric(entry, "visible_memory_total_bytes", filepath.Join(pciDir, "mem_info_vis_vram_total"))
	addMemoryMetric(entry, "visible_memory_used_bytes", filepath.Join(pciDir, "mem_info_vis_vram_used"))
	addMemoryMetric(entry, "gtt_total_bytes", filepath.Join(pciDir, "mem_info_gtt_total"))
	addMemoryMetric(entry, "gtt_used_bytes", filepath.Join(pciDir, "mem_info_gtt_used"))
	if total, ok := getUint64(entry, "memory_total_bytes"); ok {
		if used, ok := getUint64(entry, "memory_used_bytes"); ok && used <= total {
			entry["memory_free_bytes"] = total - used
		}
	}

	enrichGPUFromHwmon(pciDir, entry)
	enrichConnectedDisplays(cardName, cardDir, entry)
}

func enrichGPUFromHwmon(pciDir string, entry map[string]any) {
	hwmonDirs, err := filepath.Glob(filepath.Join(pciDir, "hwmon", "hwmon*"))
	if err != nil {
		return
	}

	for _, hwmonDir := range hwmonDirs {
		if tempMilli, ok := hwprobe.ReadSysfsIntAny(filepath.Join(hwmonDir, "temp1_input")); ok {
			entry["temperature_c"] = round1(float64(tempMilli) / 1000)
		}
		if fanRPM, ok := hwprobe.ReadSysfsIntAny(filepath.Join(hwmonDir, "fan1_input")); ok {
			entry["fan_rpm"] = fanRPM
		}
		if pwm, ok := hwprobe.ReadSysfsFloatAny(filepath.Join(hwmonDir, "pwm1")); ok {
			entry["fan_percent"] = round1((pwm / 255) * 100)
		}
		if powerMicroW, ok := hwprobe.ReadSysfsFloatAny(
			filepath.Join(hwmonDir, "power1_average"),
			filepath.Join(hwmonDir, "power1_input"),
		); ok {
			entry["power_draw_watts"] = round1(powerMicroW / 1_000_000)
		}
		if powerCapMicroW, ok := hwprobe.ReadSysfsFloatAny(filepath.Join(hwmonDir, "power1_cap")); ok {
			entry["power_limit_watts"] = round1(powerCapMicroW / 1_000_000)
		}
	}
}

func enrichConnectedDisplays(cardName, cardDir string, entry map[string]any) {
	entries, err := os.ReadDir(cardDir)
	if err != nil {
		return
	}

	connected := make([]string, 0, 4)
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), cardName+"-") {
			continue
		}
		connectorDir := filepath.Join(cardDir, e.Name())
		if !hwprobe.PathIsDir(connectorDir) {
			continue
		}

		status := strings.ToLower(hwprobe.ReadSysfsString(filepath.Join(connectorDir, "status")))
		if status != "connected" {
			continue
		}

		name := strings.TrimPrefix(e.Name(), cardName+"-")
		mode := firstLine(hwprobe.ReadSysfsString(filepath.Join(connectorDir, "modes")))
		if mode != "" {
			name = fmt.Sprintf("%s (%s)", name, mode)
		}
		connected = append(connected, name)
	}

	entry["connected_displays"] = len(connected)
	if len(connected) > 0 {
		entry["display_names"] = connected
	}
}

func readGPUUtilization(ctx context.Context, cardDir, pciDir string) (float64, bool) {
	if util, ok := hwprobe.ReadSysfsFloatAny(
		filepath.Join(cardDir, "gpu_busy_percent"),
		filepath.Join(pciDir, "gpu_busy_percent"),
	); ok {
		return round1(util), true
	}

	rc6Path := firstExistingPath(
		filepath.Join(cardDir, "power", "rc6_residency_ms"),
		filepath.Join(cardDir, "gt", "gt0", "rc6_residency_ms"),
	)
	if rc6Path == "" {
		return 0, false
	}

	return estimateBusyPercentFromRC6(ctx, rc6Path, 120*time.Millisecond)
}

func estimateBusyPercentFromRC6(ctx context.Context, path string, sample time.Duration) (float64, bool) {
	if err := ctx.Err(); err != nil {
		return 0, false
	}
	startRC6, ok := hwprobe.ReadSysfsUint64Any(path)
	if !ok {
		return 0, false
	}
	start := time.Now()
	timer := time.NewTimer(sample)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return 0, false
	case <-timer.C:
	}
	endRC6, ok := hwprobe.ReadSysfsUint64Any(path)
	if !ok {
		return 0, false
	}

	elapsedMs := float64(time.Since(start).Milliseconds())
	if elapsedMs <= 0 {
		return 0, false
	}

	if endRC6 < startRC6 {
		return 0, false
	}
	deltaRC6 := float64(endRC6 - startRC6)
	busy := 100 * (1 - (deltaRC6 / elapsedMs))
	if math.IsNaN(busy) || math.IsInf(busy, 0) {
		return 0, false
	}
	if busy < 0 {
		busy = 0
	}
	if busy > 100 {
		busy = 100
	}

	return round1(busy), true
}

func readNvidiaSMIStats(parent context.Context) map[string]nvidiaGPUStats {
	nvidiaSMIPath, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		nvidiaSMIPath,
		"--query-gpu=pci.bus_id,driver_version,temperature.gpu,fan.speed,utilization.gpu,memory.total,memory.used,power.draw,power.limit,clocks.current.graphics,clocks.max.graphics",
		"--format=csv,noheader,nounits",
	)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	reader := csv.NewReader(strings.NewReader(string(output)))
	reader.TrimLeadingSpace = true
	rows, err := reader.ReadAll()
	if err != nil {
		return nil
	}

	stats := make(map[string]nvidiaGPUStats, len(rows))
	for _, row := range rows {
		if len(row) < 11 {
			continue
		}

		pciAddr := hwprobe.NormalizePCIAddress(row[0])
		stats[pciAddr] = nvidiaGPUStats{
			DriverVersion:       hwprobe.SanitizeValue(row[1]),
			TemperatureC:        parseOptionalFloat(row[2]),
			FanPercent:          parseOptionalFloat(row[3]),
			UtilizationPercent:  parseOptionalFloat(row[4]),
			MemoryTotalBytes:    parseOptionalMiB(row[5]),
			MemoryUsedBytes:     parseOptionalMiB(row[6]),
			PowerDrawWatts:      parseOptionalFloat(row[7]),
			PowerLimitWatts:     parseOptionalFloat(row[8]),
			CurrentGraphicsMHz:  int(parseOptionalFloat(row[9])),
			MaxGraphicsClockMHz: int(parseOptionalFloat(row[10])),
		}
	}

	return stats
}

func addMemoryMetric(entry map[string]any, key, path string) {
	if value, ok := hwprobe.ReadSysfsUint64Any(path); ok {
		entry[key] = value
	}
}

func setIntIfPositive(entry map[string]any, key string, value int) {
	if value > 0 {
		entry[key] = value
	}
}

func setUint64IfPositive(entry map[string]any, key string, value uint64) {
	if value > 0 {
		entry[key] = value
	}
}

func setFloatIfPositive(entry map[string]any, key string, value float64) {
	if value > 0 {
		entry[key] = round1(value)
	}
}

func getUint64(entry map[string]any, key string) (uint64, bool) {
	value, ok := entry[key]
	if !ok {
		return 0, false
	}
	typed, ok := value.(uint64)
	return typed, ok
}

func parseOptionalFloat(value string) float64 {
	value = hwprobe.SanitizeValue(value)
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func parseOptionalMiB(value string) uint64 {
	parsed := parseOptionalFloat(value)
	if parsed <= 0 {
		return 0
	}
	return uint64(parsed * 1024 * 1024)
}

func firstExistingPath(paths ...string) string {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func firstLine(value string) string {
	value = hwprobe.SanitizeValue(value)
	if value == "" {
		return ""
	}
	if before, _, ok := strings.Cut(value, "\n"); ok {
		return strings.TrimSpace(before)
	}
	return value
}

func round1(value float64) float64 {
	return math.Round(value*10) / 10
}
