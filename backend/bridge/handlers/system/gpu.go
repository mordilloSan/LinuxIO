package system

import (
	"fmt"
	"github.com/jaypipes/ghw/pkg/gpu"
)

func FetchGPUInfo() ([]map[string]any, error) {
	info, err := gpu.New()
	if err != nil || info == nil {
		return nil, fmt.Errorf("failed to retrieve GPU information: %w", err)
	}
	var gpus []map[string]any
	for _, card := range info.GraphicsCards {
		gpus = append(gpus, map[string]any{
			"address":      card.Address,
			"vendor":       card.DeviceInfo.Vendor.Name,
			"model":        card.DeviceInfo.Product.Name,
			"device_id":    card.DeviceInfo.Product.ID,
			"vendor_id":    card.DeviceInfo.Vendor.ID,
			"subsystem":    card.DeviceInfo.Subsystem.Name,
			"subsystem_id": card.DeviceInfo.Subsystem.ID,
			"revision":     card.DeviceInfo.Revision,
			"driver":       card.DeviceInfo.Driver,
		})
	}
	return gpus, nil
}
