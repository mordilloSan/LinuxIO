package system

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"sort"
	"strings"
)

var sensorsCommand = exec.Command

type SensorReadingKind string

const (
	SensorReadingKindNumber  SensorReadingKind = "number"
	SensorReadingKindBoolean SensorReadingKind = "boolean"
)

type SensorReading struct {
	Label string            `json:"label"`
	Value any               `json:"value"`
	Kind  SensorReadingKind `json:"kind"`
	Unit  string            `json:"unit"`
	field string
}

type SensorGroup struct {
	Adapter  string          `json:"adapter"`
	Readings []SensorReading `json:"readings"`
}

// FetchSensorsInfo returns sensor readings parsed from `sensors -j`.
// Each SensorGroup.Adapter is the chip name (e.g. "coretemp-isa-0000").
func FetchSensorsInfo() []SensorGroup {
	out, err := sensorsCommand("sensors", "-j").Output()
	if err != nil {
		return nil
	}

	var raw map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil
	}

	chipNames := sortedSensorKeys(raw)
	var groups []SensorGroup
	for _, chipName := range chipNames {
		chip, ok := raw[chipName].(map[string]any)
		if !ok {
			continue
		}
		if group := parseSensorChip(chipName, chip); len(group.Readings) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

func parseSensorChip(chipName string, chip map[string]any) SensorGroup {
	group := SensorGroup{Adapter: chipName}
	featureNames := sortedSensorKeys(chip)
	for _, featureName := range featureNames {
		if featureName == "Adapter" {
			continue
		}

		leaves := collectSensorLeaves(chip[featureName], nil)
		if len(leaves) == 0 {
			continue
		}

		sort.Slice(leaves, func(i, j int) bool {
			return compareSensorLeaves(leaves[i], leaves[j])
		})

		for _, leaf := range leaves {
			label := featureName
			if len(leaves) > 1 {
				label = fmt.Sprintf("%s (%s)", featureName, sensorLeafLabel(leaf.path))
			}

			group.Readings = append(group.Readings, SensorReading{
				Label: label,
				Value: leaf.value,
				Kind:  leaf.kind,
				Unit:  leaf.unit,
				field: leaf.field,
			})
		}
	}
	return group
}

type sensorLeaf struct {
	path  []string
	value any
	kind  SensorReadingKind
	unit  string
	field string
}

func collectSensorLeaves(value any, path []string) []sensorLeaf {
	switch typed := value.(type) {
	case map[string]any:
		keys := sortedSensorKeys(typed)
		var leaves []sensorLeaf
		for _, key := range keys {
			if key == "Adapter" {
				continue
			}
			leaves = append(leaves, collectSensorLeaves(typed[key], appendSensorPath(path, key))...)
		}
		return leaves
	case float64:
		field := sensorLeafField(path)
		return []sensorLeaf{{
			path:  cloneSensorPath(path),
			value: typed,
			kind:  SensorReadingKindNumber,
			unit:  sensorUnitForPath(path),
			field: field,
		}}
	case bool:
		field := sensorLeafField(path)
		return []sensorLeaf{{
			path:  cloneSensorPath(path),
			value: typed,
			kind:  SensorReadingKindBoolean,
			field: field,
		}}
	default:
		return nil
	}
}

func appendSensorPath(path []string, key string) []string {
	next := make([]string, len(path)+1)
	copy(next, path)
	next[len(path)] = key
	return next
}

func cloneSensorPath(path []string) []string {
	return append([]string(nil), path...)
}

func sortedSensorKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func compareSensorLeaves(left, right sensorLeaf) bool {
	leftRank := sensorLeafRank(left.field)
	rightRank := sensorLeafRank(right.field)
	if leftRank != rightRank {
		return leftRank < rightRank
	}
	return sensorLeafLabel(left.path) < sensorLeafLabel(right.path)
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

func sensorLeafField(path []string) string {
	if len(path) == 0 {
		return ""
	}
	return path[len(path)-1]
}

func sensorLeafLabel(path []string) string {
	if len(path) == 0 {
		return "value"
	}

	parts := make([]string, 0, len(path))
	for idx, part := range path {
		if idx == len(path)-1 {
			parts = append(parts, sensorLeafSegmentLabel(part))
			continue
		}
		parts = append(parts, strings.ReplaceAll(part, "_", " "))
	}
	return strings.Join(parts, " / ")
}

func sensorLeafSegmentLabel(part string) string {
	if idx := strings.IndexByte(part, '_'); idx >= 0 && idx+1 < len(part) {
		part = part[idx+1:]
	}
	return strings.ReplaceAll(part, "_", " ")
}

func sensorUnitForPath(path []string) string {
	for _, v := range slices.Backward(path) {
		prefix := sensorFieldPrefix(v)
		if unit := sensorUnit(prefix); unit != "" {
			return unit
		}
	}
	return ""
}

func sensorFieldPrefix(field string) string {
	head := field
	if before, _, ok := strings.Cut(field, "_"); ok {
		head = before
	}
	return strings.TrimRight(head, "0123456789")
}

func sensorUnit(prefix string) string {
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
	default:
		return ""
	}
}

func getTemperatureMap() map[string]float64 {
	groups := FetchSensorsInfo()
	temps := make(map[string]float64)
	indices := temperatureIndexes{}

	for _, group := range groups {
		adapter := strings.ToLower(group.Adapter)
		for _, r := range group.Readings {
			value, ok := sensorNumberValue(r)
			if !ok || !sensorReadingIsInput(r) {
				continue
			}

			key, ok := classifyTemperatureReading(adapter, r, &indices)
			if !ok {
				continue
			}
			temps[key] = value
		}
	}

	return temps
}

func sensorNumberValue(reading SensorReading) (float64, bool) {
	if reading.Kind != SensorReadingKindNumber {
		return 0, false
	}

	value, ok := reading.Value.(float64)
	return value, ok
}

func sensorReadingIsInput(reading SensorReading) bool {
	if reading.field == "" {
		return true
	}
	return reading.field == "input" || strings.HasSuffix(reading.field, "_input")
}

type temperatureIndexes struct {
	core  int
	board int
	drive int
}

func classifyTemperatureReading(adapter string, reading SensorReading, indices *temperatureIndexes) (string, bool) {
	unit := strings.ToLower(reading.Unit)
	if unit != "c" && unit != "°c" {
		return "", false
	}

	label := strings.ToLower(reading.Label)
	switch {
	case strings.HasPrefix(label, "core "):
		key := fmt.Sprintf("core%d", indices.core)
		indices.core++
		return key, true
	case strings.Contains(label, "package id") || strings.Contains(label, "tctl"):
		return "package", true
	case isDriveTemperature(adapter, label):
		key := fmt.Sprintf("drive%d", indices.drive)
		indices.drive++
		return key, true
	case isBoardTemperature(adapter, label):
		key := fmt.Sprintf("mb%d", indices.board)
		indices.board++
		return key, true
	default:
		return "", false
	}
}

func isDriveTemperature(adapter, label string) bool {
	return strings.Contains(adapter, "nvme") ||
		strings.Contains(adapter, "hdd") ||
		strings.Contains(adapter, "ssd") ||
		strings.Contains(adapter, "drive") ||
		strings.Contains(label, "composite")
}

func isBoardTemperature(adapter, label string) bool {
	return strings.Contains(adapter, "acpitz") ||
		strings.Contains(label, "mb") ||
		strings.Contains(label, "board") ||
		strings.Contains(label, "system") ||
		strings.Contains(label, "systin") ||
		strings.Contains(label, "temp1")
}
