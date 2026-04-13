package system

import (
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jaypipes/ghw/pkg/gpu"
	"github.com/jaypipes/ghw/pkg/pci"
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

func FetchGPUInfo() ([]map[string]any, error) {
	info, err := gpu.New()
	if err != nil || info == nil {
		return nil, fmt.Errorf("failed to retrieve GPU information: %w", err)
	}

	nvidiaStats := readNvidiaSMIStats()
	gpus := make([]map[string]any, 0, len(info.GraphicsCards))
	for _, card := range info.GraphicsCards {
		entry := buildGPUEntry(card)
		enrichGPUFromSysfs(card.Address, entry)
		if stats, ok := nvidiaStats[normalizePCIAddress(card.Address)]; ok {
			mergeNvidiaStats(entry, stats)
		}
		gpus = append(gpus, entry)
	}

	return gpus, nil
}

func buildGPUEntry(card *gpu.GraphicsCard) map[string]any {
	entry := map[string]any{
		"address": normalizePCIAddress(card.Address),
	}
	if card.DeviceInfo != nil {
		populateDeviceInfo(entry, card.DeviceInfo)
	}
	if card.Node != nil {
		entry["numa_node"] = card.Node.ID
	}
	return entry
}

func populateDeviceInfo(entry map[string]any, di *pci.Device) {
	setIfNonEmpty(entry, "revision", di.Revision)
	setIfNonEmpty(entry, "driver", di.Driver)
	if di.Vendor != nil {
		setIfNonEmpty(entry, "vendor", di.Vendor.Name)
		setIfNonEmpty(entry, "vendor_id", di.Vendor.ID)
	}
	if di.Product != nil {
		setIfNonEmpty(entry, "model", di.Product.Name)
		setIfNonEmpty(entry, "device_id", di.Product.ID)
	}
	if di.Subsystem != nil {
		setIfNonEmpty(entry, "subsystem", di.Subsystem.Name)
		setIfNonEmpty(entry, "subsystem_id", di.Subsystem.ID)
	}
	if di.Class != nil {
		setIfNonEmpty(entry, "class_name", di.Class.Name)
	}
	if di.Subclass != nil {
		setIfNonEmpty(entry, "subclass_name", di.Subclass.Name)
	}
	if di.ProgrammingInterface != nil {
		setIfNonEmpty(entry, "programming_interface", di.ProgrammingInterface.Name)
	}
}

func mergeNvidiaStats(entry map[string]any, stats nvidiaGPUStats) {
	setIfNonEmpty(entry, "driver_version", stats.DriverVersion)
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

func enrichGPUFromSysfs(pciAddr string, entry map[string]any) {
	cardName, cardDir, pciDir, ok := findGPUCardDir(pciAddr)
	if !ok {
		return
	}

	entry["drm_card"] = cardName
	setIfNonEmpty(entry, "runtime_status", readSysfsString(filepath.Join(pciDir, "power", "runtime_status")))
	setIfNonEmpty(entry, "power_state", readSysfsString(filepath.Join(pciDir, "power_state")))
	setIfNonEmpty(entry, "link_speed", sanitizeUnknown(readSysfsString(filepath.Join(pciDir, "current_link_speed"))))
	setIfNonEmpty(entry, "link_width", sanitizeZero(readSysfsString(filepath.Join(pciDir, "current_link_width"))))
	setIfNonEmpty(entry, "max_link_speed", sanitizeUnknown(readSysfsString(filepath.Join(pciDir, "max_link_speed"))))
	setIfNonEmpty(entry, "max_link_width", sanitizeZero(readSysfsString(filepath.Join(pciDir, "max_link_width"))))
	setIfNonEmpty(entry, "driver_module", filepath.Base(readlink(filepath.Join(pciDir, "driver", "module"))))
	setIfNonEmpty(entry, "raw_class", readSysfsString(filepath.Join(pciDir, "class")))

	if v, ok := readSysfsBool(filepath.Join(pciDir, "boot_vga")); ok {
		entry["boot_vga"] = v
	}

	if v, ok := readSysfsIntAny(
		filepath.Join(cardDir, "gt_cur_freq_mhz"),
		filepath.Join(cardDir, "gt", "gt0", "rps_cur_freq_mhz"),
	); ok {
		setIntIfPositive(entry, "current_freq_mhz", v)
	}
	if v, ok := readSysfsIntAny(
		filepath.Join(cardDir, "gt_act_freq_mhz"),
		filepath.Join(cardDir, "gt", "gt0", "rps_act_freq_mhz"),
	); ok {
		setIntIfPositive(entry, "actual_freq_mhz", v)
	}
	if v, ok := readSysfsIntAny(
		filepath.Join(cardDir, "gt_min_freq_mhz"),
		filepath.Join(cardDir, "gt", "gt0", "rps_min_freq_mhz"),
	); ok {
		setIntIfPositive(entry, "min_freq_mhz", v)
	}
	if v, ok := readSysfsIntAny(
		filepath.Join(cardDir, "gt_max_freq_mhz"),
		filepath.Join(cardDir, "gt", "gt0", "rps_max_freq_mhz"),
	); ok {
		setIntIfPositive(entry, "max_freq_mhz", v)
	}
	if v, ok := readSysfsIntAny(
		filepath.Join(cardDir, "gt_boost_freq_mhz"),
		filepath.Join(cardDir, "gt", "gt0", "rps_boost_freq_mhz"),
	); ok {
		setIntIfPositive(entry, "boost_freq_mhz", v)
	}
	if v, ok := readSysfsIntAny(filepath.Join(cardDir, "gt", "gt0", "punit_req_freq_mhz")); ok {
		setIntIfPositive(entry, "requested_freq_mhz", v)
	}
	if v, ok := readSysfsIntAny(
		filepath.Join(cardDir, "gt_RP0_freq_mhz"),
		filepath.Join(cardDir, "gt", "gt0", "rps_RP0_freq_mhz"),
	); ok {
		setIntIfPositive(entry, "rp0_freq_mhz", v)
	}
	if v, ok := readSysfsIntAny(
		filepath.Join(cardDir, "gt_RP1_freq_mhz"),
		filepath.Join(cardDir, "gt", "gt0", "rps_RP1_freq_mhz"),
	); ok {
		setIntIfPositive(entry, "rp1_freq_mhz", v)
	}
	if v, ok := readSysfsIntAny(
		filepath.Join(cardDir, "gt_RPn_freq_mhz"),
		filepath.Join(cardDir, "gt", "gt0", "rps_RPn_freq_mhz"),
	); ok {
		setIntIfPositive(entry, "rpn_freq_mhz", v)
	}

	if util, ok := readGPUUtilization(cardDir, pciDir); ok {
		entry["utilization_percent"] = util
	}
	if rc6, ok := readSysfsUint64Any(
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

func findGPUCardDir(pciAddr string) (string, string, string, bool) {
	drmBase := "/sys/class/drm"
	entries, err := os.ReadDir(drmBase)
	if err != nil {
		return "", "", "", false
	}

	normalizedPCI := normalizePCIAddress(pciAddr)
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "card") || strings.Contains(e.Name(), "-") {
			continue
		}

		cardDir := filepath.Join(drmBase, e.Name())
		if !pathIsDir(cardDir) {
			continue
		}
		deviceLink := readlink(filepath.Join(cardDir, "device"))
		if normalizePCIAddress(filepath.Base(deviceLink)) != normalizedPCI {
			continue
		}

		return e.Name(), cardDir, filepath.Join(cardDir, "device"), true
	}

	return "", "", "", false
}

func enrichGPUFromHwmon(pciDir string, entry map[string]any) {
	hwmonDirs, err := filepath.Glob(filepath.Join(pciDir, "hwmon", "hwmon*"))
	if err != nil {
		return
	}

	for _, hwmonDir := range hwmonDirs {
		if tempMilli, ok := readSysfsIntAny(filepath.Join(hwmonDir, "temp1_input")); ok {
			entry["temperature_c"] = round1(float64(tempMilli) / 1000)
		}
		if fanRPM, ok := readSysfsIntAny(filepath.Join(hwmonDir, "fan1_input")); ok {
			entry["fan_rpm"] = fanRPM
		}
		if pwm, ok := readSysfsFloatAny(filepath.Join(hwmonDir, "pwm1")); ok {
			entry["fan_percent"] = round1((pwm / 255) * 100)
		}
		if powerMicroW, ok := readSysfsFloatAny(
			filepath.Join(hwmonDir, "power1_average"),
			filepath.Join(hwmonDir, "power1_input"),
		); ok {
			entry["power_draw_watts"] = round1(powerMicroW / 1_000_000)
		}
		if powerCapMicroW, ok := readSysfsFloatAny(filepath.Join(hwmonDir, "power1_cap")); ok {
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
		if !pathIsDir(connectorDir) {
			continue
		}

		status := strings.ToLower(readSysfsString(filepath.Join(connectorDir, "status")))
		if status != "connected" {
			continue
		}

		name := strings.TrimPrefix(e.Name(), cardName+"-")
		mode := firstLine(readSysfsString(filepath.Join(connectorDir, "modes")))
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

func readGPUUtilization(cardDir, pciDir string) (float64, bool) {
	if util, ok := readSysfsFloatAny(
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

	return estimateBusyPercentFromRC6(rc6Path, 120*time.Millisecond)
}

func estimateBusyPercentFromRC6(path string, sample time.Duration) (float64, bool) {
	startRC6, ok := readSysfsUint64Any(path)
	if !ok {
		return 0, false
	}
	start := time.Now()
	time.Sleep(sample)
	endRC6, ok := readSysfsUint64Any(path)
	if !ok {
		return 0, false
	}

	elapsedMs := float64(time.Since(start).Milliseconds())
	if elapsedMs <= 0 {
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

func readNvidiaSMIStats() map[string]nvidiaGPUStats {
	nvidiaSMIPath, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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

		pciAddr := normalizePCIAddress(row[0])
		stats[pciAddr] = nvidiaGPUStats{
			DriverVersion:       sanitizeValue(row[1]),
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
	if value, ok := readSysfsUint64Any(path); ok {
		entry[key] = value
	}
}

func normalizePCIAddress(addr string) string {
	addr = strings.TrimSpace(strings.ToLower(addr))
	parts := strings.Split(addr, ":")
	if len(parts) == 3 && len(parts[0]) > 4 {
		parts[0] = parts[0][len(parts[0])-4:]
		return strings.Join(parts, ":")
	}
	return addr
}

func setIfNonEmpty(entry map[string]any, key, value string) {
	value = sanitizeValue(value)
	if value != "" {
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

func readSysfsString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readSysfsBool(path string) (bool, bool) {
	value := readSysfsString(path)
	switch value {
	case "1", "Y", "y", "true":
		return true, true
	case "0", "N", "n", "false":
		return false, true
	default:
		return false, false
	}
}

func readSysfsIntAny(paths ...string) (int, bool) {
	for _, path := range paths {
		if path == "" {
			continue
		}
		value, err := strconv.Atoi(readSysfsString(path))
		if err == nil {
			return value, true
		}
	}
	return 0, false
}

func readSysfsUint64Any(paths ...string) (uint64, bool) {
	for _, path := range paths {
		if path == "" {
			continue
		}
		value, err := strconv.ParseUint(readSysfsString(path), 10, 64)
		if err == nil {
			return value, true
		}
	}
	return 0, false
}

func readSysfsFloatAny(paths ...string) (float64, bool) {
	for _, path := range paths {
		if path == "" {
			continue
		}
		value, err := strconv.ParseFloat(readSysfsString(path), 64)
		if err == nil {
			return value, true
		}
	}
	return 0, false
}

func readlink(path string) string {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return target
}

func sanitizeUnknown(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "unknown") {
		return ""
	}
	return sanitizeValue(value)
}

func sanitizeZero(value string) string {
	value = sanitizeValue(value)
	if value == "0" {
		return ""
	}
	return value
}

func sanitizeValue(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "n/a", "[not supported]", "[not available]":
		return ""
	default:
		return value
	}
}

func parseOptionalFloat(value string) float64 {
	value = sanitizeValue(value)
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

func pathIsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func firstLine(value string) string {
	value = sanitizeValue(value)
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
