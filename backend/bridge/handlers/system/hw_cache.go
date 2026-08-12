package system

import (
	"sync"

	"github.com/jaypipes/ghw/pkg/gpu"
	"github.com/jaypipes/ghw/pkg/pci"
)

// hwSnapshotCache caches ghw snapshots whose construction parses the PCI IDs
// database (~250ms of CPU per call). PCI topology only changes on hotplug and
// the bridge lives for one login session, so a successful snapshot is reused
// for the process lifetime; failures are retried on the next call. The mutex
// is held across load so concurrent first callers parse the database once.
type hwSnapshotCache[T any] struct {
	mu    sync.Mutex
	value T
	ok    bool
}

func (c *hwSnapshotCache[T]) get(load func() (T, error)) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ok {
		return c.value, nil
	}
	value, err := load()
	if err != nil {
		var zero T
		return zero, err
	}
	c.value = value
	c.ok = true
	return value, nil
}

// Snapshots are read-only after load; handlers must not mutate them.
var (
	pciInfoCache hwSnapshotCache[*pci.Info]
	gpuInfoCache hwSnapshotCache[*gpu.Info]
)

func cachedPCIInfo() (*pci.Info, error) {
	return pciInfoCache.get(func() (*pci.Info, error) { return pci.New() })
}

func cachedGPUInfo() (*gpu.Info, error) {
	return gpuInfoCache.get(func() (*gpu.Info, error) { return gpu.New() })
}
