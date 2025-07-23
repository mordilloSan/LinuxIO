package system

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

func FetchBaseboardInfo() (map[string]any, error) {
	basePath := "/sys/class/dmi/id"
	fields := map[string]string{
		"board_name":    "model",
		"board_vendor":  "manufacturer",
		"board_version": "version",
		"board_serial":  "serial",
		"bios_vendor":   "bios_vendor",
		"bios_version":  "bios_version",
		"bios_date":     "bios_date",
	}

	info := make(map[string]string)
	for file, label := range fields {
		content, err := os.ReadFile(filepath.Join(basePath, file))
		if err == nil {
			info[label] = strings.TrimSpace(string(content))
		}
	}

	if len(info) == 0 {
		return nil, fmt.Errorf("unable to read motherboard info")
	}

	// Include motherboard temperatures
	tempMap := getTemperatureMap()
	var socketTemps []float64
	for key, value := range tempMap {
		if strings.HasPrefix(key, "mb") {
			socketTemps = append(socketTemps, value)
		}
	}

	return map[string]any{
		"baseboard": map[string]string{
			"manufacturer": info["manufacturer"],
			"model":        info["model"],
			"version":      info["version"],
			"serial":       info["serial"],
		},
		"bios": map[string]string{
			"vendor":  info["bios_vendor"],
			"version": info["bios_version"],
			"date":    info["bios_date"],
		},
		"temperatures": map[string]any{
			"socket": socketTemps,
		},
	}, nil
}

func getBaseboardInfo(c *gin.Context) {
	data, err := FetchBaseboardInfo()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}
