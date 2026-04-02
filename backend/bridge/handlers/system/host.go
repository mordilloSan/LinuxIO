package system

import (
	"os/exec"
	"strings"

	"github.com/shirou/gopsutil/v4/host"
)

func FetchHostInfo() (*host.InfoStat, error) {
	return host.Info()
}

func FetchUptimeSeconds() (uint64, error) {
	return host.Uptime()
}

func GetTimezones() ([]string, error) {
	out, err := exec.Command("timedatectl", "list-timezones", "--no-pager").Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			result = append(result, l)
		}
	}
	return result, nil
}
