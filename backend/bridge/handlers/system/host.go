package system

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/host"
)

func FetchHostInfo() (*host.InfoStat, error) {
	return host.Info()
}

func FetchUptimeSeconds() (uint64, error) {
	return host.Uptime()
}

func GetTimezones() ([]string, error) {
	const root = "/usr/share/zoneinfo"
	var zones []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := strings.TrimPrefix(path, root+"/")
		if _, zErr := time.LoadLocation(name); zErr == nil {
			zones = append(zones, name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(zones)
	return zones, nil
}
