// Package hwprobe holds the GPU/PCI hardware probing helpers and the ghw
// snapshot cache shared by the bridge and the monitoring daemon.
package hwprobe

import (
	"log/slog"
	"sync"

	"github.com/jaypipes/ghw"
	"github.com/jaypipes/ghw/pkg/gpu"
	"github.com/jaypipes/ghw/pkg/pci"
)

// SnapshotCache caches snapshots whose construction is expensive (ghw parses
// the PCI IDs database, ~250ms of CPU per call). A successful snapshot is
// reused for the process lifetime; failures are retried on the next call. The
// mutex is held across load so concurrent first callers load once. The zero
// value is ready to use.
type SnapshotCache[T any] struct {
	mu    sync.Mutex
	value T
	ok    bool
}

func (c *SnapshotCache[T]) Get(load func() (T, error)) (T, error) {
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

// Snapshots are read-only after load; callers must not mutate them.
var (
	pciInfoCache SnapshotCache[*pci.Info]
	gpuInfoCache SnapshotCache[*gpu.Info]
)

func CachedPCIInfo() (*pci.Info, error) {
	return pciInfoCache.Get(func() (*pci.Info, error) {
		return pci.New(ghw.WithLogger(slog.Default()))
	})
}

func CachedGPUInfo() (*gpu.Info, error) {
	return gpuInfoCache.Get(func() (*gpu.Info, error) {
		return gpu.New(ghw.WithLogger(slog.Default()))
	})
}
