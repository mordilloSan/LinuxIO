package system

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// ==== Logic ====

func FetchBaseboardInfo(ctx context.Context) (apischema.MotherboardInfo, error) {
	return fetchBaseboardInfo(ctx, "/sys/class/dmi/id", getTemperatureMap)
}

func fetchBaseboardInfo(
	ctx context.Context,
	basePath string,
	fetchTemperatures func(context.Context) map[string]float64,
) (apischema.MotherboardInfo, error) {
	if err := ctx.Err(); err != nil {
		return apischema.MotherboardInfo{}, err
	}

	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(basePath, name))
		if err != nil {
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

	// Include all temperature sensors except CPU-specific ones
	tempMap := fetchTemperatures(ctx)
	if err := ctx.Err(); err != nil {
		return apischema.MotherboardInfo{}, err
	}
	mbTemps := make(map[string]float64)
	for key, value := range tempMap {
		if !strings.HasPrefix(key, "core") && key != "package" {
			mbTemps[key] = value
		}
	}
	info.Temperatures = &apischema.MotherboardTemperatures{Sensors: mbTemps}

	return info, nil
}
