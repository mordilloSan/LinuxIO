package system

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// ==== Logic ====

func FetchBaseboardInfo(ctx context.Context) (apischema.MotherboardInfo, error) {
	if err := ctx.Err(); err != nil {
		return apischema.MotherboardInfo{}, err
	}
	return motherboardInfoCache.get(func() (apischema.MotherboardInfo, error) {
		return fetchBaseboardInfo(ctx, "/sys/class/dmi/id")
	})
}

var motherboardInfoCache hwSnapshotCache[apischema.MotherboardInfo]

func fetchBaseboardInfo(ctx context.Context, basePath string) (apischema.MotherboardInfo, error) {
	if err := ctx.Err(); err != nil {
		return apischema.MotherboardInfo{}, err
	}

	var firstOperationalErr error
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(basePath, name))
		if err != nil {
			if firstOperationalErr == nil && !errors.Is(err, os.ErrNotExist) {
				firstOperationalErr = err
			}
			return ""
		}
		return strings.TrimSpace(string(b))
	}

	info := apischema.MotherboardInfo{
		Baseboard: apischema.MotherboardBaseboard{
			Model:        read("board_name"),
			Manufacturer: read("board_vendor"),
		},
		BIOS: apischema.MotherboardBIOS{
			Vendor:  read("bios_vendor"),
			Version: read("bios_version"),
		},
	}

	if err := ctx.Err(); err != nil {
		return apischema.MotherboardInfo{}, err
	}
	if firstOperationalErr != nil {
		return info, firstOperationalErr
	}
	return info, nil
}
