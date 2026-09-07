package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

var sensorsCommand = exec.CommandContext

const liveSensorsTimeout = 5 * time.Second

func (a *App) fetchLiveSensorsInfo(ctx context.Context) []monitoringapi.SensorGroup {
	timeout := liveSensorsTimeout
	if a.sensorConfig != nil && a.sensorConfig.timeout > 0 {
		timeout = a.sensorConfig.timeout
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return fetchSensorsInfo(commandCtx)
}

func fetchSensorsInfo(ctx context.Context) []monitoringapi.SensorGroup {
	out, err := sensorsCommand(ctx, "sensors", "-j").Output()
	if err != nil {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil
	}

	groups := make([]monitoringapi.SensorGroup, 0, len(raw))
	for _, chipName := range sortedSensorKeys(raw) {
		chip, ok := raw[chipName].(map[string]any)
		if !ok {
			continue
		}
		group := parseSensorChip(chipName, chip)
		if len(group.Readings) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

type sensorLeaf struct {
	path  []string
	value float64
	kind  monitoringapi.SensorReadingKind
	unit  string
	field string
}

func parseSensorChip(chipName string, chip map[string]any) monitoringapi.SensorGroup {
	group := monitoringapi.SensorGroup{Adapter: chipName}
	for _, featureName := range sortedSensorKeys(chip) {
		if featureName == "Adapter" {
			continue
		}
		leaves := collectSensorLeaves(chip[featureName], nil)
		sort.Slice(leaves, func(i, j int) bool {
			left, right := sensorLeafRank(leaves[i].field), sensorLeafRank(leaves[j].field)
			return left < right || (left == right && sensorLeafLabel(leaves[i].path) < sensorLeafLabel(leaves[j].path))
		})
		for _, leaf := range leaves {
			label := featureName
			if len(leaves) > 1 {
				label = fmt.Sprintf("%s (%s)", featureName, sensorLeafLabel(leaf.path))
			}
			group.Readings = append(group.Readings, monitoringapi.SensorReading{
				Label: label, Value: leaf.value, Kind: leaf.kind, Unit: leaf.unit, Field: leaf.field,
			})
		}
	}
	return group
}

func collectSensorLeaves(value any, path []string) []sensorLeaf {
	switch typed := value.(type) {
	case map[string]any:
		var leaves []sensorLeaf
		for _, key := range sortedSensorKeys(typed) {
			if key != "Adapter" {
				leaves = append(leaves, collectSensorLeaves(typed[key], appendSensorPath(path, key))...)
			}
		}
		return leaves
	case float64:
		return []sensorLeaf{{path: slices.Clone(path), value: typed, kind: monitoringapi.SensorReadingKindNumber, unit: sensorUnitForPath(path), field: sensorLeafField(path)}}
	case bool:
		value := 0.0
		if typed {
			value = 1
		}
		return []sensorLeaf{{path: slices.Clone(path), value: value, kind: monitoringapi.SensorReadingKindBoolean, field: sensorLeafField(path)}}
	default:
		return nil
	}
}

func sortedSensorKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func appendSensorPath(path []string, key string) []string {
	return append(slices.Clone(path), key)
}

func sensorLeafField(path []string) string {
	if len(path) == 0 {
		return ""
	}
	return path[len(path)-1]
}

func sensorLeafRank(field string) int {
	switch sensorLeafSegmentLabel(field) {
	case "input":
		return 0
	case "min":
		return 1
	case "max":
		return 2
	case "crit":
		return 3
	case "alarm":
		return 4
	default:
		return 5
	}
}

func sensorLeafLabel(path []string) string {
	if len(path) == 0 {
		return "value"
	}
	parts := make([]string, 0, len(path))
	for index, part := range path {
		if index == len(path)-1 {
			parts = append(parts, sensorLeafSegmentLabel(part))
		} else {
			parts = append(parts, strings.ReplaceAll(part, "_", " "))
		}
	}
	return strings.Join(parts, " / ")
}

func sensorLeafSegmentLabel(part string) string {
	if index := strings.IndexByte(part, '_'); index >= 0 && index+1 < len(part) {
		part = part[index+1:]
	}
	return strings.ReplaceAll(part, "_", " ")
}

func sensorUnitForPath(path []string) string {
	for _, part := range slices.Backward(path) {
		prefix := part
		if before, _, ok := strings.Cut(part, "_"); ok {
			prefix = before
		}
		prefix = strings.TrimRight(prefix, "0123456789")
		switch prefix {
		case "temp":
			return "°C"
		case "fan":
			return "RPM"
		case "in":
			return "V"
		case "power":
			return "W"
		case "curr":
			return "A"
		case "humidity":
			return "%"
		}
	}
	return ""
}

func cpuTemperatures(groups []monitoringapi.SensorGroup) map[string]float64 {
	temps := make(map[string]float64)
	core := 0
	for _, group := range groups {
		for _, reading := range group.Readings {
			if !isCPUTemperature(reading) {
				continue
			}
			name, isCore := cpuTemperatureName(reading.Label, core)
			if name == "" {
				continue
			}
			temps[name] = reading.Value
			if isCore {
				core++
			}
		}
	}
	return temps
}

func isCPUTemperature(reading monitoringapi.SensorReading) bool {
	if reading.Kind != monitoringapi.SensorReadingKindNumber {
		return false
	}
	if reading.Field != "" && reading.Field != "input" && !strings.HasSuffix(reading.Field, "_input") {
		return false
	}
	unit := strings.ToLower(reading.Unit)
	return unit == "c" || unit == "°c"
}

func cpuTemperatureName(label string, fallbackCore int) (string, bool) {
	label = strings.ToLower(label)
	if strings.HasPrefix(label, "core ") {
		coreID := fallbackCore
		fields := strings.Fields(strings.TrimPrefix(label, "core "))
		if len(fields) > 0 {
			if parsed, err := strconv.Atoi(fields[0]); err == nil && parsed >= 0 {
				coreID = parsed
			}
		}
		return fmt.Sprintf("core%d", coreID), true
	}
	if strings.Contains(label, "package id") || strings.Contains(label, "tctl") {
		return "package", false
	}
	return "", false
}
