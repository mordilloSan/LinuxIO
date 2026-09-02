package system

import (
	"context"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

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
