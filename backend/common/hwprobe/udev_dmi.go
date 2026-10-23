package hwprobe

import (
	"strconv"
	"strings"
)

// HasUdevMemoryDeviceData reports whether udev's DMI properties describe at
// least one memory device.
func HasUdevMemoryDeviceData(props map[string]string) bool {
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
