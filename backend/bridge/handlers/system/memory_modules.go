package system

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

var memoryModulesRunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

var memoryModulesLookPath = exec.LookPath

func FetchMemoryModules(ctx context.Context) ([]apischema.MemoryModule, error) {
	if modules, err := fetchUdevMemoryModules(ctx); err == nil && len(modules) > 0 {
		return modules, nil
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}

	out, err := memoryModulesRunCommand(ctx, "dmidecode", "-t", "memory")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return []apischema.MemoryModule{}, nil
	}

	return parseDMIMemory(string(out)), nil
}

func CheckMemoryModuleInventoryAvailability(ctx context.Context) (bool, error) {
	available, err := hasUdevMemoryInventory(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
	} else if available {
		return true, nil
	}

	if _, err := memoryModulesLookPath("dmidecode"); err == nil {
		return true, nil
	}
	return false, fmt.Errorf("udev DMI memory data is unavailable and dmidecode is not installed")
}

func hasUdevMemoryInventory(ctx context.Context) (bool, error) {
	out, err := memoryModulesRunCommand(ctx, "udevadm", "info", "--export-db")
	if err != nil {
		return false, err
	}
	return hasUdevMemoryDeviceData(string(out)), nil
}

func fetchUdevMemoryModules(ctx context.Context) ([]apischema.MemoryModule, error) {
	out, err := memoryModulesRunCommand(ctx, "udevadm", "info", "--export-db")
	if err != nil {
		return nil, err
	}
	return parseUdevMemoryModules(string(out)), nil
}

func parseUdevMemoryModules(output string) []apischema.MemoryModule {
	props := udevDMIProperties(output)
	if !hasUdevMemoryDeviceDataFromProps(props) {
		return nil
	}

	devices, _ := strconv.Atoi(props["MEMORY_ARRAY_NUM_DEVICES"])
	modules := make([]apischema.MemoryModule, 0, devices)
	for slot := range devices {
		prefix := fmt.Sprintf("MEMORY_DEVICE_%d_", slot)
		sizeBytes := props[prefix+"SIZE"]
		present := props[prefix+"PRESENT"] != "0" && (sizeBytes != "" || props[prefix+"TOTAL_WIDTH"] != "")

		id := memoryModuleID(slot, props[prefix+"BANK_LOCATOR"], props[prefix+"LOCATOR"])
		size := "Unknown"
		if present && sizeBytes != "" {
			size = formatBytesIEC(sizeBytes)
		}

		state := "Absent"
		if present {
			state = "Present"
		}

		modules = append(modules, apischema.MemoryModule{
			ID:         id,
			Technology: valueOrDefault(props[prefix+"MEMORY_TECHNOLOGY"], present),
			Type:       valueOrDefault(props[prefix+"TYPE"], present),
			Size:       size,
			State:      state,
			Rank:       valueOrDefault(props[prefix+"RANK"], present),
			Speed:      memoryModuleSpeed(props[prefix+"SPEED_MTS"], props[prefix+"CONFIGURED_SPEED_MTS"], present),
		})
	}

	return modules
}

func hasUdevMemoryDeviceData(output string) bool {
	return hasUdevMemoryDeviceDataFromProps(udevDMIProperties(output))
}

func hasUdevMemoryDeviceDataFromProps(props map[string]string) bool {
	devices, err := strconv.Atoi(props["MEMORY_ARRAY_NUM_DEVICES"])
	if err != nil || devices <= 0 {
		return false
	}
	for key := range props {
		if strings.HasPrefix(key, "MEMORY_DEVICE_") {
			return true
		}
	}
	return false
}

func udevDMIProperties(output string) map[string]string {
	for paragraph := range strings.SplitSeq(output, "\n\n") {
		lines := strings.Split(strings.TrimSpace(paragraph), "\n")
		if len(lines) == 0 {
			continue
		}

		props := make(map[string]string)
		isDMI := false
		for _, line := range lines {
			if strings.HasPrefix(line, "P: ") && strings.TrimSpace(strings.TrimPrefix(line, "P: ")) == "/devices/virtual/dmi/id" {
				isDMI = true
				continue
			}
			if after, ok := strings.CutPrefix(line, "E: "); ok {
				key, value, ok := strings.Cut(after, "=")
				if ok {
					props[key] = value
				}
			}
		}
		if isDMI {
			return props
		}
	}
	return map[string]string{}
}

func memoryModuleID(slot int, bankLocator, locator string) string {
	bankLocator = strings.TrimSpace(bankLocator)
	locator = strings.TrimSpace(locator)
	if bankLocator != "" && locator != "" {
		return bankLocator + ": " + locator
	}
	if locator != "" {
		return locator
	}
	if bankLocator != "" {
		return bankLocator
	}
	return fmt.Sprintf("Slot %d", slot)
}

func memoryModuleSpeed(primary, configured string, installed bool) string {
	if !installed {
		return "Unknown"
	}
	speed := strings.TrimSpace(primary)
	if speed == "" || speed == "0" {
		speed = strings.TrimSpace(configured)
	}
	if speed == "" || speed == "0" {
		return "Unknown"
	}
	if strings.Contains(speed, " ") {
		return speed
	}
	return speed + " MT/s"
}

func formatBytesIEC(value string) string {
	bytes, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil || bytes == 0 {
		return "Unknown"
	}

	const unit = 1024
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	size := float64(bytes)
	idx := 0
	for size >= unit && idx < len(units)-1 {
		size /= unit
		idx++
	}
	if size == float64(uint64(size)) {
		return fmt.Sprintf("%.0f %s", size, units[idx])
	}
	return fmt.Sprintf("%.1f %s", size, units[idx])
}

func parseDMIMemory(output string) []apischema.MemoryModule {
	// Split on "Memory Device" sections (skip "Physical Memory Array" blocks)
	sections := strings.Split(output, "Memory Device")
	var modules []apischema.MemoryModule

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

		modules = append(modules, apischema.MemoryModule{
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
	if val == "" || val == "Not Specified" || val == "<OUT OF SPEC>" {
		return "Unknown"
	}
	return val
}
