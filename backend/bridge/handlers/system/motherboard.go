package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ==== Types ====

type Baseboard struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Version      string `json:"version"`
	Serial       string `json:"serial"`
}

type BIOS struct {
	Vendor  string `json:"vendor"`
	Version string `json:"version"`
	Date    string `json:"date"`
}

type MotherboardTemperatures struct {
	Sensors map[string]float64 `json:"sensors"`
}

type MotherboardInfo struct {
	Baseboard    Baseboard               `json:"baseboard"`
	BIOS         BIOS                    `json:"bios"`
	Temperatures MotherboardTemperatures `json:"temperatures"`
}

// ==== Logic ====

func FetchBaseboardInfo() (MotherboardInfo, error) {
	basePath := "/sys/class/dmi/id"

	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(basePath, name))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}

	info := MotherboardInfo{
		Baseboard: Baseboard{
			Model:        read("board_name"),
			Manufacturer: read("board_vendor"),
			Version:      read("board_version"),
			Serial:       read("board_serial"),
		},
		BIOS: BIOS{
			Vendor:  read("bios_vendor"),
			Version: read("bios_version"),
			Date:    read("bios_date"),
		},
	}

	// Include all temperature sensors except CPU-specific ones
	tempMap := getTemperatureMap()
	mbTemps := make(map[string]float64)
	for key, value := range tempMap {
		if !strings.HasPrefix(key, "core") && key != "package" {
			mbTemps[key] = value
		}
	}
	info.Temperatures = MotherboardTemperatures{Sensors: mbTemps}

	// If everything is empty, signal an error
	if info.Baseboard.Manufacturer == "" &&
		info.Baseboard.Model == "" &&
		info.Baseboard.Version == "" &&
		info.Baseboard.Serial == "" &&
		info.BIOS.Vendor == "" &&
		info.BIOS.Version == "" &&
		info.BIOS.Date == "" &&
		len(info.Temperatures.Sensors) == 0 {
		return MotherboardInfo{}, fmt.Errorf("unable to read motherboard info")
	}

	return info, nil
}
