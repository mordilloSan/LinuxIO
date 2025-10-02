package system

import (
	"github.com/shirou/gopsutil/v4/host"
)

func FetchHostInfo() (*host.InfoStat, error) {
	return host.Info()
}

func FetchUptimeSeconds() (uint64, error) {
	return host.Uptime()
}
