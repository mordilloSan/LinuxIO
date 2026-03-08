package system

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type SensorReading struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type SensorGroup struct {
	Adapter  string          `json:"adapter"`
	Readings []SensorReading `json:"readings"`
}

func FetchSensorsInfo() []SensorGroup {
	out, err := exec.Command("sensors").Output()
	if err != nil {
		return nil
	}

	var groups []SensorGroup
	lines := strings.Split(string(out), "\n")

	re := regexp.MustCompile(`(?i)^([\w\s().:+-]+?):\s*([+-]?[0-9]*\.?[0-9]+)\s*([A-Za-z°%]+)?`)
	var currentGroup *SensorGroup

	for _, raw := range lines {
		line := strings.TrimSpace(raw)

		if line == "" {
			continue
		}

		// New adapter block
		if !strings.Contains(line, ":") {
			if currentGroup != nil && len(currentGroup.Readings) > 0 {
				groups = append(groups, *currentGroup)
			}
			currentGroup = &SensorGroup{
				Adapter:  line,
				Readings: []SensorReading{},
			}
			continue
		}

		// Match label:value unit
		if currentGroup != nil {
			if matches := re.FindStringSubmatch(line); len(matches) >= 3 {
				value, err := strconv.ParseFloat(matches[2], 64)
				if err == nil {
					currentGroup.Readings = append(currentGroup.Readings, SensorReading{
						Label: strings.TrimSpace(matches[1]),
						Value: value,
						Unit:  strings.TrimSpace(matches[3]),
					})
				}
			}
		}
	}

	if currentGroup != nil && len(currentGroup.Readings) > 0 {
		groups = append(groups, *currentGroup)
	}

	return groups
}

func getTemperatureMap() map[string]float64 {
	groups := FetchSensorsInfo()
	temps := make(map[string]float64)
	indices := temperatureIndexes{}

	for _, group := range groups {
		adapter := strings.ToLower(group.Adapter)
		for _, r := range group.Readings {
			key, ok := classifyTemperatureReading(adapter, r, &indices)
			if !ok {
				continue
			}
			temps[key] = r.Value
		}
	}

	return temps
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
