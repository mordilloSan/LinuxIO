package system

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/disk"
)

func FetchFileSystemInfo() ([]map[string]any, error) {
	parts, err := disk.Partitions(true)
	if err != nil {
		return nil, fmt.Errorf("failed to get disk partitions: %w", err)
	}

	var results []map[string]any
	for _, p := range parts {
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil {
			continue
		}
		results = append(results, map[string]any{
			"device":      p.Device,
			"mountpoint":  p.Mountpoint,
			"fstype":      p.Fstype,
			"total":       usage.Total,
			"used":        usage.Used,
			"free":        usage.Free,
			"usedPercent": usage.UsedPercent,
		})
	}
	return results, nil
}

func getFsInfo(c *gin.Context) {
	data, err := FetchFileSystemInfo()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, data)
}
