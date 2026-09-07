package system

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/host"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

var hostInfoCache hwSnapshotCache[apischema.HostInfo]

func FetchHostInfo(ctx context.Context) (apischema.HostInfo, error) {
	if err := ctx.Err(); err != nil {
		return apischema.HostInfo{}, err
	}
	return hostInfoCache.get(func() (apischema.HostInfo, error) {
		value, err := host.InfoWithContext(ctx)
		if err != nil {
			return apischema.HostInfo{}, err
		}
		if value == nil {
			return apischema.HostInfo{}, fmt.Errorf("host information unavailable")
		}
		result := hostInfoToAPI(value)
		if result == (apischema.HostInfo{}) {
			return apischema.HostInfo{}, fmt.Errorf("host information unavailable")
		}
		return result, nil
	})
}

func GetCurrentServerTime(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return time.Now().Format(time.RFC3339), nil
}

func GetTimezones(ctx context.Context) ([]string, error) {
	const root = "/usr/share/zoneinfo"
	var zones []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
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
