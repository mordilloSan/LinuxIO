package system

import (
	"context"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/host"
)

func FetchHostInfo(ctx context.Context) (*host.InfoStat, error) {
	return host.InfoWithContext(ctx)
}

func FetchUptimeSeconds(ctx context.Context) (uint64, error) {
	return host.UptimeWithContext(ctx)
}

func GetCurrentServerTime(ctx context.Context) string {
	return time.Now().Format(time.RFC3339)
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
