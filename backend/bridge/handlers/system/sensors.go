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

	coreIndex := 0
	mbIndex := 0
	driveIndex := 0

	for _, group := range groups {
		adapter := strings.ToLower(group.Adapter)
		for _, r := range group.Readings {
			unit := strings.ToLower(r.Unit)
			label := strings.ToLower(r.Label)

			if unit != "c" && unit != "°c" {
				continue
			}

			switch {
			case strings.HasPrefix(label, "core "):
				key := fmt.Sprintf("core%d", coreIndex)
				temps[key] = r.Value
				coreIndex++

			case strings.Contains(label, "package id") || strings.Contains(label, "tctl"):
				temps["package"] = r.Value

			case strings.Contains(adapter, "nvme") ||
				strings.Contains(adapter, "hdd") ||
				strings.Contains(adapter, "ssd") ||
				strings.Contains(adapter, "drive") ||
				strings.Contains(label, "composite"):
				key := fmt.Sprintf("drive%d", driveIndex)
				temps[key] = r.Value
				driveIndex++

			case strings.Contains(adapter, "acpitz") ||
				strings.Contains(label, "mb") ||
				strings.Contains(label, "board") ||
				strings.Contains(label, "system") ||
				strings.Contains(label, "systin") ||
				strings.Contains(label, "temp1"):
				key := fmt.Sprintf("mb%d", mbIndex)
				temps[key] = r.Value
				mbIndex++
			}
		}
	}

	return temps
}
