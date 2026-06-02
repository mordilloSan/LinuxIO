package system

import (
	"context"
	"fmt"
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

	basePath := "/sys/class/dmi/id"

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
	tempMap := getTemperatureMap(ctx)
	mbTemps := make(map[string]float64)
	for key, value := range tempMap {
		if !strings.HasPrefix(key, "core") && key != "package" {
			mbTemps[key] = value
		}
	}
	info.Temperatures = &apischema.MotherboardTemperatures{Sensors: mbTemps}

	// If everything is empty, signal an error
	if info.Baseboard.Manufacturer == "" &&
		info.Baseboard.Model == "" &&
		info.BIOS.Vendor == "" &&
		info.BIOS.Version == "" &&
		len(info.Temperatures.Sensors) == 0 {
		return apischema.MotherboardInfo{}, fmt.Errorf("unable to read motherboard info")
	}

	return info, nil
}
