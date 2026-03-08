package system

import (
	"fmt"
	"os/exec"
	"strings"
)

type MemoryModule struct {
	ID         string `json:"id"`
	Technology string `json:"technology"`
	Type       string `json:"type"`
	Size       string `json:"size"`
	State      string `json:"state"`
	Rank       string `json:"rank"`
	Speed      string `json:"speed"`
}

func FetchMemoryModules() ([]MemoryModule, error) {
	out, err := exec.Command("dmidecode", "-t", "memory").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run dmidecode: %w", err)
	}

	return parseDMIMemory(string(out)), nil
}

func parseDMIMemory(output string) []MemoryModule {
	// Split on "Memory Device" sections (skip "Physical Memory Array" blocks)
	sections := strings.Split(output, "Memory Device")
	var modules []MemoryModule

	for _, section := range sections[1:] { // skip preamble before first "Memory Device"
		fields := parseDMIFields(section)

		bankLocator := fields["Bank Locator"]
		locator := fields["Locator"]
		id := locator
		if bankLocator != "" && locator != "" {
			id = bankLocator + ": " + locator
		}

		size := fields["Size"]
		installed := size != "" && size != "No Module Installed"

		state := "Absent"
		if installed {
			state = "Present"
		}

		modules = append(modules, MemoryModule{
			ID:         id,
			Technology: valueOrDefault(fields["Memory Technology"], installed),
			Type:       valueOrDefault(fields["Type"], installed),
			Size:       valueOrDefault(size, installed),
			State:      state,
			Rank:       valueOrDefault(fields["Rank"], installed),
			Speed:      valueOrDefault(fields["Speed"], installed),
		})
	}

	return modules
}

func parseDMIFields(section string) map[string]string {
	fields := make(map[string]string)
	for line := range strings.SplitSeq(section, "\n") {
		line = strings.TrimSpace(line)
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			fields[key] = val
		}
	}
	return fields
}

func valueOrDefault(val string, installed bool) string {
	if !installed {
		return "Unknown"
	}
	if val == "" || val == "Not Specified" {
		return "Unknown"
	}
	return val
}
