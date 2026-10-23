package capabilities

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/monitoring"
	"github.com/mordilloSan/LinuxIO/backend/common/hwprobe"
)

var errAvahiUnavailable = fmt.Errorf("avahi-daemon is not running")

var memoryInventoryLookPath = exec.LookPath

var memoryInventoryRunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func checkDependencyCommand(command, dependencyName string) (bool, error) {
	if _, err := exec.LookPath(command); err != nil {
		return false, fmt.Errorf("%s not found (missing %s dependency)", command, dependencyName)
	}
	return true, nil
}

// checkMonitoringHealth is a test seam over the monitoring daemon client. The
// daemon ships with LinuxIO, so detection only asks whether it is running.
var checkMonitoringHealth = monitoring.CheckHealth

func checkMonitoringAvailability(ctx context.Context) (bool, error) {
	if err := checkMonitoringHealth(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func checkedCapability(ok bool, err error) (bool, string) {
	return checkedCapabilityErr(ok, err, nil)
}

func checkedCapabilityErr(ok bool, err error, unavailable error) (bool, string) {
	if err != nil {
		return false, err.Error()
	}
	if !ok && unavailable != nil {
		return false, unavailable.Error()
	}
	return ok, ""
}

// checkMemoryModuleInventoryAvailability reports whether memory module data is
// readable, from udev's DMI properties or from dmidecode.
func checkMemoryModuleInventoryAvailability(ctx context.Context) (bool, error) {
	available, err := hasUdevMemoryInventory(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
	} else if available {
		return true, nil
	}

	if _, err := memoryInventoryLookPath("dmidecode"); err == nil {
		return true, nil
	}
	return false, fmt.Errorf("udev DMI memory data is unavailable and dmidecode is not installed")
}

func hasUdevMemoryInventory(ctx context.Context) (bool, error) {
	out, err := memoryInventoryRunCommand(ctx, "udevadm", "info", "--query=property", "--path=/sys/class/dmi/id")
	if err != nil {
		return false, err
	}
	props := make(map[string]string)
	for line := range strings.SplitSeq(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && key != "" {
			props[key] = value
		}
	}
	return hwprobe.HasUdevMemoryDeviceData(props), nil
}
