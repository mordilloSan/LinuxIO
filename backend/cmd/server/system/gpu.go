package system

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/jaypipes/ghw/pkg/gpu"
)

// Cached GPU info and lock
var (
	cachedGPUInfo *gpu.Info
	gpuInitErr    error
	gpuCacheLock  sync.RWMutex
)

// Called once at server start to populate GPU info
func InitGPUInfo() {
	info, err := gpu.New()
	gpuCacheLock.Lock()
	cachedGPUInfo = info
	gpuInitErr = err
	gpuCacheLock.Unlock()
}

// Reusable function to fetch GPU data
func FetchGPUInfo() ([]map[string]any, error) {
	gpuCacheLock.RLock()
	info := cachedGPUInfo
	err := gpuInitErr
	gpuCacheLock.RUnlock()

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

// HTTP handler
func getGPUInfo(c *gin.Context) {
	data, err := FetchGPUInfo()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to retrieve GPU information",
			"details": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"gpus": data})
}
