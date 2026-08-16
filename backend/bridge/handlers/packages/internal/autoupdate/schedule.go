package autoupdate

import (
	"fmt"
	"path/filepath"
	"strings"
)

func onCalendarFor(freq string) (string, error) {
	switch freq {
	case "hourly":
		return "hourly", nil
	case "daily":
		return "daily", nil
	case "weekly":
		return "weekly", nil
	default:
		return "", fmt.Errorf("invalid frequency: %s", freq)
	}
}

func writeTimerDropIn(host backendHost, timer, onCalendar string) error {
	path := filepath.Join("/etc/systemd/system", timer+".d", "linuxio.conf")
	body := "[Timer]\nOnCalendar=\nOnCalendar=" + onCalendar + "\nRandomizedDelaySec=30m\n"
	return host.writeFileAtomic(path, []byte(body), 0o644)
}

func readTimerFrequency(host backendHost, timer string) string {
	path := filepath.Join("/etc/systemd/system", timer+".d", "linuxio.conf")
	data, err := host.readFile(path)
	if err != nil {
		return "daily"
	}
	frequency := "daily"
	for line := range strings.SplitSeq(string(data), "\n") {
		value, ok := strings.CutPrefix(strings.TrimSpace(line), "OnCalendar=")
		if ok && strings.TrimSpace(value) != "" {
			frequency = strings.TrimSpace(value)
		}
	}
	return frequency
}
